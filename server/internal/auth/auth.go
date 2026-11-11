// Package auth owns identity: Google sign-in (OpenID Connect with PKCE and
// a verified ID token), the list of people who may sign in (list.go),
// sessions stored by token hash, the four roles and who may change whose
// account (rules.go), and the owner bootstrapped from configuration. It
// also keeps the site audit
// log, the organisation's site settings, the run-mode record and account
// lookup, and erases a person (forget-user). Its HTTP handlers (http.go)
// serve the Dashboard's People, Sign-in and Audit tabs; sign-in and the session cookie stay with the
// middleware in httpapi.
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/retention"
)

// Role is a global role. The order is student < organiser < admin <
// owner (V4-16): an admin organises like an organiser and also manages
// students and organisers in the Dashboard (rules.go). Until v4 the stored
// value of an organiser was "admin" (migration 0002_v4_roles); site audit
// rows from before it are read with that in mind (siteaudit.go).
type Role string

const (
	Student   Role = "student"
	Organiser Role = "organiser"
	Admin     Role = "admin"
	Owner     Role = "owner"
)

func (r Role) rank() int {
	switch r {
	case Owner:
		return 3
	case Admin:
		return 2
	case Organiser:
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
	// Remembered marks a kept session (keep.go): the account menu offers to
	// turn it off.
	Remembered bool `json:"remembered"`
	// Theme, Verdicts, EditorColours and ThemeCustom are the display
	// preferences (preferences.go). ThemeCustom is the custom theme's three
	// inputs as a JSON object, or null when none was saved.
	Theme         string          `json:"theme"`
	Verdicts      string          `json:"verdicts"`
	EditorColours string          `json:"editor_colours"`
	ThemeCustom   json.RawMessage `json:"theme_custom"`
}

// SessionTTL is how long an ordinary session lasts: 12 hours from sign-in,
// however much it is used, so a session left open on a shared lab PC is gone
// by the next day at the latest. The cookie itself has no expiry, so closing
// the browser ends it sooner. A kept session (keep.go) lasts as an owner set.
const SessionTTL = retention.Sessions

// Sign-in refusals. Handlers map them to the /signin?error= codes.
var (
	ErrUnverified = errors.New("email not verified")
	ErrDisabled   = errors.New("account disabled")
	// ErrAccount: the email is bound to a different Google account (or this
	// Google account to a different email). An owner can reset the binding.
	ErrAccount   = errors.New("email bound to another google account")
	ErrNoSession = errors.New("no valid session")
)

// ChangeError is a refused change to people, with a message for people.
// Kind is "invalid", "not_found", "conflict" or "forbidden".
type ChangeError struct {
	Kind    string
	Message string
}

func (e *ChangeError) Error() string { return e.Message }

func invalid(msg string) error   { return &ChangeError{"invalid", msg} }
func notFound(msg string) error  { return &ChangeError{"not_found", msg} }
func conflict(msg string) error  { return &ChangeError{"conflict", msg} }
func forbidden(msg string) error { return &ChangeError{"forbidden", msg} }

// Service applies the identity rules against the database.
type Service struct {
	DB *sql.DB
	// OwnerEmail (lowercase) is always an owner and can always sign in.
	OwnerEmail string
	// Now is the clock; tests replace it.
	Now func() time.Time

	// OnSessionsEnded, when set, is called with the ids of sessions the
	// server has just ended, so the provider keys held for them are
	// forgotten. An id is the session token's hash.
	OnSessionsEnded func(ids []string)

	// org caches the site settings (site.go).
	org orgCache
	// keep caches the keep-signed-in setting (keep.go).
	keep keepCache
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Bootstrap makes OwnerEmail an owner, creating the user if needed and
// re-enabling them: the configured owner is always on the list, which is
// how an owner locked out of the Dashboard gets back in.
func (s *Service) Bootstrap(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A Gmail spelling with other dots is the same person, already listed.
	id, _, found, err := listed(ctx, tx, s.OwnerEmail)
	if err != nil {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	if found {
		_, err = tx.ExecContext(ctx, "UPDATE users SET role = 'owner', disabled = 0 WHERE id = ?", id)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES (?, 'owner', ?)", s.OwnerEmail, s.now().UnixMilli())
	}
	if err != nil {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	return tx.Commit()
}

// SignIn is SignInKeep for a person who did not tick "Keep me signed in".
// It returns the session token for the cookie and its expiry.
func (s *Service) SignIn(ctx context.Context, id Identity, ip, userAgent string) (User, string, time.Time, error) {
	u, token, st, err := s.SignInKeep(ctx, id, ip, userAgent, false)
	return u, token, st.Expires, err
}

// SignInKeep applies the sign-in rules to a verified identity and starts a
// session. The email may sign in only when it is on the list (list.go) and
// not disabled; an address that is not is recorded as refused. The row is
// bound to the first Google account that signs in with it. tick is the
// person's choice to stay signed in; the setting for their role decides
// whether it counts (keep.go). It returns the session token for the cookie
// and how the session began.
func (s *Service) SignInKeep(ctx context.Context, id Identity, ip, userAgent string, tick bool) (User, string, Started, error) {
	if !id.EmailVerified {
		return User{}, "", Started{}, ErrUnverified
	}
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if !validEmail(email) {
		return User{}, "", Started{}, ErrNotListed
	}
	policy, err := s.KeepPolicy(ctx)
	if err != nil {
		return User{}, "", Started{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", Started{}, err
	}
	defer tx.Rollback()

	userID, _, found, err := listed(ctx, tx, email)
	if err != nil {
		return User{}, "", Started{}, err
	}
	if !found {
		tx.Rollback()
		if err := s.recordRefusal(ctx, email); err != nil {
			return User{}, "", Started{}, err
		}
		return User{}, "", Started{}, ErrNotListed
	}
	var disabled bool
	var role Role
	var boundSub sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT role, disabled, google_sub FROM users WHERE id = ?", userID).Scan(&role, &disabled, &boundSub); err != nil {
		return User{}, "", Started{}, err
	}
	// The account is bound to the first Google account that signed in with
	// this email. Email addresses get reassigned; Google's subject id does
	// not, so a different subject means a different person.
	if id.Subject == "" || (boundSub.Valid && boundSub.String != id.Subject) {
		return User{}, "", Started{}, ErrAccount
	}
	var otherID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE google_sub = ? AND id <> ?", id.Subject, userID).Scan(&otherID)
	if err == nil {
		return User{}, "", Started{}, ErrAccount // this Google account is linked to another address
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", Started{}, err
	}
	if disabled {
		return User{}, "", Started{}, ErrDisabled
	}
	// A closed switch keeps everyone below admin out, so admins and owners
	// can always get in to open it again.
	if !role.AtLeast(Admin) {
		open, err := signInOpen(ctx, tx)
		if err != nil {
			return User{}, "", Started{}, err
		}
		if !open {
			return User{}, "", Started{}, ErrSignInClosed
		}
	}

	now := s.now()
	u := User{ID: userID, Name: limit(id.Name, 200), AvatarURL: safeAvatar(id.Picture)}
	var custom string
	// The row keeps the address as it was listed; a dotted Gmail spelling
	// from Google does not rename it.
	err = tx.QueryRowContext(ctx, `UPDATE users SET name = ?, avatar_url = ?, google_sub = ?, last_login_at = ? WHERE id = ?
		RETURNING email, role, theme, verdicts, editor_colours, theme_custom`, u.Name, u.AvatarURL, id.Subject, now.UnixMilli(), userID).
		Scan(&u.Email, &u.Role, &u.Theme, &u.Verdicts, &u.EditorColours, &custom)
	u.ThemeCustom = customJSON(custom)
	if err != nil {
		return User{}, "", Started{}, fmt.Errorf("record user: %w", err)
	}
	token, st, err := startSession(ctx, tx, policy, u.ID, u.Role, tick, now, ip, userAgent)
	if err != nil {
		return User{}, "", Started{}, fmt.Errorf("create session: %w", err)
	}
	u.Remembered = st.Remembered
	return u, token, st, tx.Commit()
}

// Authenticate resolves a session token to its user. An ordinary session
// never renews: it ends SessionTTL after sign-in (a session made before
// that rule, with a later expiry, ends at the same cap), when its user is
// disabled, or at sign-out, and renewed is zero. A kept session (keep.go)
// ends when it has gone unused for the setting's period, and a use at least
// an hour after the last renewal moves its expiry forward: renewed is then
// the new expiry, for the caller to put in a fresh cookie.
func (s *Service) Authenticate(ctx context.Context, token string) (u User, renewed time.Time, err error) {
	if token == "" || len(token) > 100 {
		return User{}, time.Time{}, ErrNoSession
	}
	id := hashToken(token)
	now := s.now()
	var created, expires, lastSeen int64
	var custom string
	err = s.DB.QueryRowContext(ctx, `SELECT u.id, u.email, u.name, u.avatar_url, u.role, u.disabled, u.theme, u.verdicts,
		u.editor_colours, u.theme_custom, s.created_at, s.expires_at, s.remember, coalesce(s.last_seen_at, s.created_at)
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.Role, &u.Disabled, &u.Theme, &u.Verdicts, &u.EditorColours, &custom, &created, &expires, &u.Remembered, &lastSeen)
	u.ThemeCustom = customJSON(custom)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, time.Time{}, ErrNoSession
	}
	if err != nil {
		return User{}, time.Time{}, err
	}
	var idle time.Duration
	end := min(expires, created+SessionTTL.Milliseconds())
	if u.Remembered {
		p, err := s.KeepPolicy(ctx)
		if err != nil {
			return User{}, time.Time{}, err
		}
		idle = p.Idle()
		end = min(expires, lastSeen+idle.Milliseconds())
		// A row on a role set to Nobody is not a session: a switch that
		// raced its write, a testing-mode re-role or Bootstrap can leave one.
		if p.For(u.Role) == KeepNobody {
			end = 0
		}
	}
	if now.UnixMilli() >= end || u.Disabled {
		if ended, err := deleteSessions(ctx, s.DB, "id = ?", id); err == nil {
			s.sessionsEnded(ended)
		}
		return User{}, time.Time{}, ErrNoSession
	}
	if u.Remembered && now.UnixMilli()-lastSeen >= renewEvery.Milliseconds() {
		// Best effort: a failed write leaves the session as it was, still
		// valid until its stored expiry.
		next := now.Add(idle)
		if _, err := s.DB.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ? AND remember = 1",
			now.UnixMilli(), next.UnixMilli(), id); err == nil {
			renewed = next
		}
	}
	return u, renewed, nil
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
	ended, err := deleteSessions(ctx, tx, "user_id = ?", id)
	if err != nil {
		return err
	}
	if err := s.recordSite(ctx, tx, AuditGoogleReset, email, map[string]any{"sessions_ended": len(ended)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.sessionsEnded(ended)
	return nil
}

// SignOut ends the session for token, if any. Sign-out needs no session,
// so a token that cannot be one, or names no session, costs only a read:
// a flood of made-up cookies must not queue writes behind a contest's
// submissions.
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
	ended, err := deleteSessions(ctx, s.DB, "id = ?", id)
	s.sessionsEnded(ended)
	return err
}

// DeleteExpiredSessions removes the sessions that have ended: an ordinary
// one past its expiry or SessionTTL after sign-in, a kept one past its
// expiry or the setting's period without use.
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	p, err := s.KeepPolicy(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	ended, err := deleteSessions(ctx, s.DB, `(remember = 0 AND (expires_at <= ?1 OR created_at <= ?2))
		OR (remember = 1 AND (expires_at <= ?1 OR coalesce(last_seen_at, created_at) <= ?3))`,
		now, now-SessionTTL.Milliseconds(), now-p.Idle().Milliseconds())
	s.sessionsEnded(ended)
	return int64(len(ended)), err
}

// Person is an organiser, admin or owner as listed on the Dashboard's People tab.
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

// Organisers lists everyone with the organiser, admin or owner role,
// owners first, then admins.
func (s *Service) Organisers(ctx context.Context) ([]Person, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, email, name, role, last_login_at, disabled FROM users
		WHERE role IN ('owner', 'admin', 'organiser') ORDER BY role = 'owner' DESC, role = 'admin' DESC, lower(name) = '', lower(name), email`)
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
		p.Locked = s.isConfiguredOwner(p.Email)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GrantRole gives email the organiser, admin or owner role, creating the
// account if the person has never signed in. The actor on ctx must be
// allowed the change (rules.go).
func (s *Service) GrantRole(ctx context.Context, email string, role Role) (Person, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		return Person{}, invalid("Enter a single email address, such as ta@college.edu.")
	}
	if role != Organiser && role != Admin && role != Owner {
		return Person{}, invalid("Choose organiser, admin or owner.")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()
	// The person as listed: a Gmail spelling with other dots is the same
	// row, so nobody can plant a second one beside a protected person.
	var current Role
	userID, stored, found, err := listed(ctx, tx, email)
	if err != nil {
		return Person{}, err
	}
	if found {
		email = stored
		if err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", userID).Scan(&current); err != nil {
			return Person{}, err
		}
	}
	if err := s.changeAllowed(ctx, email, current, role); err != nil {
		return Person{}, err
	}
	if s.isConfiguredOwner(email) {
		return Person{}, conflict("This owner is set in the server configuration and cannot be changed here.")
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
	var kept []string
	if current != role {
		from := string(current)
		if from == "" {
			from = "none"
		}
		if err := s.recordSite(ctx, tx, AuditRoleGranted, email, map[string]any{"role": string(role), "from_role": from}); err != nil {
			return Person{}, err
		}
		// A kept session does not carry into another role; an ordinary one
		// is read with the new role on its next request.
		if kept, err = deleteSessions(ctx, tx, "user_id = ? AND remember = 1", p.ID); err != nil {
			return Person{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Person{}, err
	}
	s.sessionsEnded(kept)
	return p, nil
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
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !role.AtLeast(Organiser)) {
		return notFound("That person is not an organiser.")
	}
	if err != nil {
		return err
	}
	if err := s.changeAllowed(ctx, email, role, Student); err != nil {
		return err
	}
	if s.isConfiguredOwner(email) {
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
	ended, err := deleteSessions(ctx, tx, "user_id = ?", id)
	if err != nil {
		return err
	}
	if err := s.recordSite(ctx, tx, AuditRoleRevoked, email, map[string]any{"from_role": string(role), "sessions_ended": len(ended)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.sessionsEnded(ended)
	return nil
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

// safeAvatar keeps only https avatar URLs from Google's image host, which
// is the one external image source the Content-Security-Policy allows.
func safeAvatar(u string) string {
	if strings.HasPrefix(u, "https://lh3.googleusercontent.com/") && len(u) <= 500 {
		return u
	}
	return ""
}
