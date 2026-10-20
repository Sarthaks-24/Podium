// Package auth owns identity: Google sign-in (OpenID Connect with PKCE and
// a verified ID token), the server-side email-domain check, sessions stored
// by token hash, the owner/admin/student roles, the owner bootstrapped from
// configuration, and the allowed-domain list. HTTP handlers live in
// httpapi; this package holds the rules.
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/retention"
)

// Role is a global role. The order is student < admin < owner.
type Role string

const (
	Student Role = "student"
	Admin   Role = "admin"
	Owner   Role = "owner"
)

func (r Role) rank() int {
	switch r {
	case Owner:
		return 2
	case Admin:
		return 1
	}
	return 0
}

// AtLeast reports whether r grants everything min grants.
func (r Role) AtLeast(min Role) bool { return r.rank() >= min.rank() }

// User is a signed-in person.
type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Role      Role   `json:"role"`
	Disabled  bool   `json:"-"`
	// Theme and Verdicts are the display preferences (preferences.go).
	Theme    string `json:"theme"`
	Verdicts string `json:"verdicts"`
	// HideProgress is the person's choice to hide their own progress
	// page and Home summary (v1.4.0).
	HideProgress bool `json:"hide_progress"`
}

// SessionTTL is how long a session lasts: 12 hours from sign-in, however
// much it is used, so a session left open on a shared lab PC is gone by
// the next day at the latest (security council S5). The cookie itself has
// no expiry, so closing the browser ends it sooner.
const SessionTTL = retention.Sessions

// Sign-in refusals. Handlers map them to the /signin?error= codes.
var (
	ErrDomain     = errors.New("email domain not allowed")
	ErrUnverified = errors.New("email not verified")
	ErrDisabled   = errors.New("account disabled")
	// ErrAccount: the email is bound to a different Google account (or this
	// Google account to a different email). An owner can reset the binding.
	ErrAccount   = errors.New("email bound to another google account")
	ErrNoSession = errors.New("no valid session")
)

// ChangeError is a refused change to people or domains, with a message for
// people. Kind is "invalid", "not_found" or "conflict".
type ChangeError struct {
	Kind    string
	Message string
}

func (e *ChangeError) Error() string { return e.Message }

func invalid(msg string) error  { return &ChangeError{"invalid", msg} }
func notFound(msg string) error { return &ChangeError{"not_found", msg} }
func conflict(msg string) error { return &ChangeError{"conflict", msg} }

// Service applies the identity rules against the database.
type Service struct {
	DB *sql.DB
	// OwnerEmail (lowercase) is always an owner and can always sign in.
	OwnerEmail string
	// Now is the clock; tests replace it.
	Now func() time.Time

	// org caches the site settings (site.go).
	org orgCache
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Bootstrap makes OwnerEmail an owner, creating the user if needed. On a
// fresh install (no users yet) it also seeds the allowed domains; after that
// owners manage the list in the web app and configuration no longer
// overrides it.
func (s *Service) Bootstrap(ctx context.Context, seedDomains []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var users int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil {
		return err
	}
	now := s.now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (email, role, created_at) VALUES (?, 'owner', ?)
		ON CONFLICT (email) DO UPDATE SET role = 'owner', disabled = 0`, s.OwnerEmail, now); err != nil {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	if users == 0 {
		for _, d := range seedDomains {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO allowed_domains (domain, created_at) VALUES (?, ?)", d, now); err != nil {
				return fmt.Errorf("seed domains: %w", err)
			}
		}
	}
	return tx.Commit()
}

// AllowedDomains lists the allowed email domains, sorted.
func (s *Service) AllowedDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT domain, created_at FROM allowed_domains ORDER BY domain")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.Domain, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SignIn applies the sign-in rules to a verified identity and starts a
// session. The email may sign in when its domain is allowed, when the
// account already holds the admin or owner role, or when it is OwnerEmail.
// It returns the session token for the cookie and its expiry.
func (s *Service) SignIn(ctx context.Context, id Identity, ip, userAgent string) (User, string, time.Time, error) {
	if !id.EmailVerified {
		return User{}, "", time.Time{}, ErrUnverified
	}
	email := strings.ToLower(id.Email)
	at := strings.LastIndexByte(email, '@')
	if at < 1 {
		return User{}, "", time.Time{}, ErrDomain
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	defer tx.Rollback()

	var role Role = Student
	var disabled bool
	var boundSub sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT role, disabled, google_sub FROM users WHERE email = ?", email).Scan(&role, &disabled, &boundSub)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, err
	}
	// The account is bound to the first Google account that signed in with
	// this email. Email addresses get reassigned; Google's subject id does
	// not, so a different subject means a different person.
	if id.Subject == "" || (boundSub.Valid && boundSub.String != id.Subject) {
		return User{}, "", time.Time{}, ErrAccount
	}
	var otherEmail string
	err = tx.QueryRowContext(ctx, "SELECT email FROM users WHERE google_sub = ? AND email <> ?", id.Subject, email).Scan(&otherEmail)
	if err == nil {
		return User{}, "", time.Time{}, ErrAccount // this Google account is linked to another address
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, err
	}
	if !(email == s.OwnerEmail || (exists && role.AtLeast(Admin))) {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM allowed_domains WHERE domain = ?", email[at+1:]).Scan(&n); err != nil {
			return User{}, "", time.Time{}, err
		}
		if n == 0 {
			return User{}, "", time.Time{}, ErrDomain
		}
	}
	if disabled {
		return User{}, "", time.Time{}, ErrDisabled
	}

	now := s.now()
	u := User{Email: email, Name: limit(id.Name, 200), AvatarURL: safeAvatar(id.Picture)}
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, name, avatar_url, google_sub, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET name = excluded.name, avatar_url = excluded.avatar_url,
			google_sub = excluded.google_sub, last_login_at = excluded.last_login_at
		RETURNING id, role, theme, verdicts, hide_progress`, email, u.Name, u.AvatarURL, id.Subject, now.UnixMilli(), now.UnixMilli()).Scan(&u.ID, &u.Role, &u.Theme, &u.Verdicts, &u.HideProgress)
	if err != nil {
		return User{}, "", time.Time{}, fmt.Errorf("record user: %w", err)
	}
	token := randomToken(32)
	expires := now.Add(SessionTTL)
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, user_id, created_at, expires_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), u.ID, now.UnixMilli(), expires.UnixMilli(), limit(ip, 64), limit(userAgent, 300)); err != nil {
		return User{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return u, token, expires, tx.Commit()
}

// Authenticate resolves a session token to its user. Sessions never
// renew: one ends SessionTTL after sign-in (a session made before that
// rule, with a later expiry, ends at the same cap), when its user is
// disabled, or at sign-out. renewed is always zero; it stays in the
// signature so callers that re-send a renewed cookie need no change.
func (s *Service) Authenticate(ctx context.Context, token string) (u User, renewed time.Time, err error) {
	if token == "" || len(token) > 100 {
		return User{}, time.Time{}, ErrNoSession
	}
	id := hashToken(token)
	now := s.now()
	var created, expires int64
	err = s.DB.QueryRowContext(ctx, `SELECT u.id, u.email, u.name, u.avatar_url, u.role, u.disabled, u.theme, u.verdicts,
		u.hide_progress, s.created_at, s.expires_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.Role, &u.Disabled, &u.Theme, &u.Verdicts, &u.HideProgress, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, time.Time{}, ErrNoSession
	}
	if err != nil {
		return User{}, time.Time{}, err
	}
	end := min(expires, created+SessionTTL.Milliseconds())
	if now.UnixMilli() >= end || u.Disabled {
		s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
		return User{}, time.Time{}, ErrNoSession
	}
	return u, time.Time{}, nil
}

// ResetGoogleAccount unbinds an account from its Google account, so the
// next person to sign in with that email (and a verified address) is bound
// instead. Used when an address legitimately moves to a new Google account.
// Their sessions end.
func (s *Service) ResetGoogleAccount(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, "UPDATE users SET google_sub = NULL WHERE email = ? RETURNING id", email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("Nobody with that email has an account.")
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id)
	if err != nil {
		return err
	}
	ended, _ := res.RowsAffected()
	if err := s.recordSite(ctx, tx, AuditGoogleReset, email, map[string]any{"sessions_ended": ended}); err != nil {
		return err
	}
	return tx.Commit()
}

// SignOut ends the session for token, if any. Sign-out needs no session,
// so a token that cannot be one, or names no session, costs only a read:
// a flood of made-up cookies must not queue writes behind a contest's
// submissions (P0 security review, finding 3).
func (s *Service) SignOut(ctx context.Context, token string) error {
	if token == "" || len(token) > 100 {
		return nil
	}
	id := hashToken(token)
	var one int
	err := s.DB.QueryRowContext(ctx, "SELECT 1 FROM sessions WHERE id = ?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	return err
}

// DeleteExpiredSessions removes sessions past their expiry or SessionTTL
// after sign-in.
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	now := s.now().UnixMilli()
	res, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ? OR created_at <= ?", now, now-SessionTTL.Milliseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Person is an organiser as listed on the People page.
type Person struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Role        Role   `json:"role"`
	LastLoginAt *int64 `json:"last_login_at"`
	// Locked marks the configured owner, whose role cannot be changed.
	Locked bool `json:"locked"`
	// Disabled marks an account an owner has disabled (accounts.go).
	Disabled bool `json:"disabled"`
}

// Domain is an allowed email domain.
type Domain struct {
	Domain    string `json:"domain"`
	CreatedAt int64  `json:"created_at"`
}

// Organisers lists everyone with the admin or owner role, owners first.
func (s *Service) Organisers(ctx context.Context) ([]Person, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, email, name, role, last_login_at, disabled FROM users
		WHERE role IN ('owner', 'admin') ORDER BY role = 'owner' DESC, lower(name) = '', lower(name), email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Email, &p.Name, &p.Role, &p.LastLoginAt, &p.Disabled); err != nil {
			return nil, err
		}
		p.Locked = p.Email == s.OwnerEmail
		out = append(out, p)
	}
	return out, rows.Err()
}

// GrantRole gives email the admin or owner role, creating the account if
// the person has never signed in.
func (s *Service) GrantRole(ctx context.Context, email string, role Role) (Person, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		return Person{}, invalid("Enter a single email address, such as ta@college.edu.")
	}
	if role != Admin && role != Owner {
		return Person{}, invalid("Choose admin or owner.")
	}
	if email == s.OwnerEmail {
		return Person{}, conflict("This owner is set in the server configuration and cannot be changed here.")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()
	var current Role
	err = tx.QueryRowContext(ctx, "SELECT role FROM users WHERE email = ?", email).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Person{}, err
	}
	if current == Owner && role != Owner {
		if err := lastOwnerGuard(ctx, tx); err != nil {
			return Person{}, err
		}
	}
	var p Person
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, role, created_at) VALUES (?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET role = excluded.role
		RETURNING id, email, name, role, last_login_at`, email, role, s.now().UnixMilli()).
		Scan(&p.ID, &p.Email, &p.Name, &p.Role, &p.LastLoginAt)
	if err != nil {
		return Person{}, err
	}
	if current != role {
		from := string(current)
		if from == "" {
			from = "none"
		}
		if err := s.recordSite(ctx, tx, AuditRoleGranted, email, map[string]any{"role": string(role), "from_role": from}); err != nil {
			return Person{}, err
		}
	}
	return p, tx.Commit()
}

// lastOwnerGuard refuses a change that would leave no owner.
func lastOwnerGuard(ctx context.Context, tx *sql.Tx) error {
	var owners int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role = 'owner'").Scan(&owners); err != nil {
		return err
	}
	if owners <= 1 {
		return conflict("At least one owner must remain.")
	}
	return nil
}

// RevokeRole makes an organiser a student again and ends their sessions, so
// a person admitted only by the role (another domain) is out at once and
// anyone else signs in again as a student. The configured owner and the
// last remaining owner cannot be demoted.
func (s *Service) RevokeRole(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	var role Role
	err = tx.QueryRowContext(ctx, "SELECT email, role FROM users WHERE id = ?", id).Scan(&email, &role)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !role.AtLeast(Admin)) {
		return notFound("That person is not an organiser.")
	}
	if err != nil {
		return err
	}
	if email == s.OwnerEmail {
		return conflict("This owner is set in the server configuration and cannot be removed here.")
	}
	if role == Owner {
		if err := lastOwnerGuard(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET role = 'student' WHERE id = ?", id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id)
	if err != nil {
		return err
	}
	ended, _ := res.RowsAffected()
	if err := s.recordSite(ctx, tx, AuditRoleRevoked, email, map[string]any{"from_role": string(role), "sessions_ended": ended}); err != nil {
		return err
	}
	return tx.Commit()
}

// AddDomain allows a domain; adding one already present returns it with
// created false.
func (s *Service) AddDomain(ctx context.Context, domain string) (Domain, bool, error) {
	d, ok := normaliseDomain(domain)
	if !ok {
		return Domain{}, false, invalid("Enter a domain such as college.edu.")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Domain{}, false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO allowed_domains (domain, created_at) VALUES (?, ?)", d, s.now().UnixMilli())
	if err != nil {
		return Domain{}, false, err
	}
	created, _ := res.RowsAffected()
	if created == 1 {
		if err := s.recordSite(ctx, tx, AuditDomainAdded, "", map[string]any{"domain": d}); err != nil {
			return Domain{}, false, err
		}
	}
	var out Domain
	if err := tx.QueryRowContext(ctx, "SELECT domain, created_at FROM allowed_domains WHERE domain = ?", d).Scan(&out.Domain, &out.CreatedAt); err != nil {
		return Domain{}, false, err
	}
	return out, created == 1, tx.Commit()
}

// RemoveDomain stops sign-ins from a domain and ends the sessions of
// students on it. Organisers on it keep access because their role, not the
// domain, admits them.
func (s *Service) RemoveDomain(ctx context.Context, domain string) error {
	d, _ := normaliseDomain(domain)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM allowed_domains WHERE domain = ?", d)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("That domain is not on the allowed list.")
	}
	// Compare the email's suffix exactly; LIKE would treat _ as a wildcard.
	res, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id IN (
		SELECT id FROM users WHERE role = 'student' AND email <> ? AND substr(lower(email), -length(?) - 1) = '@' || ?)`,
		s.OwnerEmail, d, d)
	if err != nil {
		return err
	}
	ended, _ := res.RowsAffected()
	if err := s.recordSite(ctx, tx, AuditDomainRemoved, "", map[string]any{"domain": d, "sessions_ended": ended}); err != nil {
		return err
	}
	return tx.Commit()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// limit truncates s to at most n bytes without splitting a character.
func limit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

var normaliseDomain = config.NormaliseDomain

// safeAvatar keeps only https avatar URLs from Google's image host, which
// is the one external image source the Content-Security-Policy allows.
func safeAvatar(u string) string {
	if strings.HasPrefix(u, "https://lh3.googleusercontent.com/") && len(u) <= 500 {
		return u
	}
	return ""
}
