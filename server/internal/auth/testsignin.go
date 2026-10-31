package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// TestSignIn starts a session as anyone, with the given role, without
// Google. It exists only for PODIUM_MODE=testing, where the
// server listens on loopback alone; the HTTP route is not registered in any
// other mode, and the handler also refuses anything that arrived through a
// proxy. The configured owner always keeps the owner role, and the
// person's Google link is left untouched so a later real sign-in still
// binds normally. A Google-linked person keeps their stored role whatever
// the form asked for; RecordMode undoes the roles it grants to everyone
// else when the server next starts in another mode.
func (s *Service) TestSignIn(ctx context.Context, email, name string, role Role, ip, userAgent string) (User, string, time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return User{}, "", time.Time{}, invalid("Enter an email address, for example asha@college.edu.")
	}
	switch role {
	case Student, Organiser, Admin, Owner:
	default:
		return User{}, "", time.Time{}, invalid("Choose student, organiser, admin or owner.")
	}
	if s.isConfiguredOwner(email) {
		role = Owner
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	defer tx.Rollback()
	// A disabled account stays disabled here too; an owner re-enables it on
	// the People page.
	var disabled bool
	err = tx.QueryRowContext(ctx, "SELECT disabled FROM users WHERE email = ?", email).Scan(&disabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, err
	}
	if disabled && !s.isConfiguredOwner(email) {
		return User{}, "", time.Time{}, ErrDisabled
	}
	now := s.now()
	u := User{Email: email, Name: limit(strings.TrimSpace(name), 200)}
	// A row with a Google link keeps its stored role: RecordMode leaves such
	// rows alone, so a role chosen here would outlive testing mode.
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, name, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET name = excluded.name, last_login_at = excluded.last_login_at,
			role = CASE WHEN users.google_sub IS NULL THEN excluded.role ELSE users.role END
		RETURNING id, role, theme, verdicts`, email, u.Name, string(role), now.UnixMilli(), now.UnixMilli()).Scan(&u.ID, &u.Role, &u.Theme, &u.Verdicts)
	if err != nil {
		return User{}, "", time.Time{}, fmt.Errorf("record test user: %w", err)
	}
	token := randomToken(32)
	expires := now.Add(SessionTTL)
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, user_id, created_at, expires_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), u.ID, now.UnixMilli(), expires.UnixMilli(), limit(ip, 64), limit(userAgent, 300)); err != nil {
		return User{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return u, token, expires, tx.Commit()
}
