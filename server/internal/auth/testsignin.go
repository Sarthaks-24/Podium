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
	u, token, st, err := s.TestSignInKeep(ctx, email, name, role, ip, userAgent, false)
	return u, token, st.Expires, err
}

// TestSignInKeep is TestSignIn with the person's choice to stay signed in,
// which the setting for their role decides on as it does at Google sign-in
// (keep.go).
func (s *Service) TestSignInKeep(ctx context.Context, email, name string, role Role, ip, userAgent string, tick bool) (User, string, Started, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return User{}, "", Started{}, invalid("Enter an email address, for example asha@college.edu.")
	}
	switch role {
	case Student, Organiser, Admin, Owner:
	default:
		return User{}, "", Started{}, invalid("Choose student, organiser, admin or owner.")
	}
	if s.isConfiguredOwner(email) {
		role = Owner
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
	// A disabled account stays disabled here too; an admin or an owner
	// re-enables it on the Dashboard's People tab.
	var disabled bool
	err = tx.QueryRowContext(ctx, "SELECT disabled FROM users WHERE email = ?", email).Scan(&disabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", Started{}, err
	}
	if disabled && !s.isConfiguredOwner(email) {
		return User{}, "", Started{}, ErrDisabled
	}
	now := s.now()
	u := User{Email: email, Name: limit(strings.TrimSpace(name), 200)}
	var custom string
	// A row with a Google link keeps its stored role: RecordMode leaves such
	// rows alone, so a role chosen here would outlive testing mode.
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, name, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET name = excluded.name, last_login_at = excluded.last_login_at,
			role = CASE WHEN users.google_sub IS NULL THEN excluded.role ELSE users.role END
		RETURNING id, role, theme, verdicts, editor_colours, theme_custom`, email, u.Name, string(role), now.UnixMilli(), now.UnixMilli()).
		Scan(&u.ID, &u.Role, &u.Theme, &u.Verdicts, &u.EditorColours, &custom)
	u.ThemeCustom = customJSON(custom)
	if err != nil {
		return User{}, "", Started{}, fmt.Errorf("record test user: %w", err)
	}
	token, st, err := startSession(ctx, tx, policy, u.ID, u.Role, tick, now, ip, userAgent)
	if err != nil {
		return User{}, "", Started{}, fmt.Errorf("create session: %w", err)
	}
	u.Remembered = st.Remembered
	return u, token, st, tx.Commit()
}
