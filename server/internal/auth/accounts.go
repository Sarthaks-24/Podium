package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Account is one person's account as the owner's account actions see it:
// ending someone's sessions, and disabling or
// re-enabling them.
type Account struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Role        Role   `json:"role"`
	Disabled    bool   `json:"disabled"`
	LastLoginAt *int64 `json:"last_login_at"`
}

// Account finds one account by its email, students included, matched as
// sign-in stores it (trimmed, lower case). Nobody with that email is a
// not_found ChangeError.
func (s *Service) Account(ctx context.Context, email string) (Account, error) {
	return accountByEmail(ctx, s.DB, email)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func accountByEmail(ctx context.Context, q rowQuerier, email string) (Account, error) {
	var a Account
	err := q.QueryRowContext(ctx, "SELECT id, email, name, role, disabled, last_login_at FROM users WHERE email = ?",
		strings.ToLower(strings.TrimSpace(email))).Scan(&a.ID, &a.Email, &a.Name, &a.Role, &a.Disabled, &a.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, notFound("Nobody with that email has an account.")
	}
	return a, err
}

// EndSessions signs a person out everywhere: every session of the account
// is deleted, so their next request and their open live streams (at the
// next clock tick) find no session. It returns how many ended.
func (s *Service) EndSessions(ctx context.Context, email string) (Account, int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, 0, err
	}
	defer tx.Rollback()
	a, err := accountByEmail(ctx, tx, email)
	if err != nil {
		return Account{}, 0, err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", a.ID)
	if err != nil {
		return Account{}, 0, err
	}
	n, _ := res.RowsAffected()
	if err := s.recordSite(ctx, tx, AuditSessionsEnded, a.Email, map[string]any{"count": n}); err != nil {
		return Account{}, 0, err
	}
	return a, n, tx.Commit()
}

// SetDisabled disables or re-enables an account. A disabled account cannot
// sign in, and disabling ends its sessions, so it is refused at every
// signed-in route from its next request. The configured owner cannot be
// disabled (it is the recovery path), and nobody can disable themselves.
// by is the owner acting. It returns the account and how many sessions
// ended.
func (s *Service) SetDisabled(ctx context.Context, email string, disabled bool, by int64) (Account, int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, 0, err
	}
	defer tx.Rollback()
	a, err := accountByEmail(ctx, tx, email)
	if err != nil {
		return Account{}, 0, err
	}
	if disabled && a.Email == s.OwnerEmail {
		return Account{}, 0, conflict("This owner is set in the server configuration and cannot be disabled.")
	}
	if disabled && a.ID == by {
		return Account{}, 0, conflict("You cannot disable your own account.")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET disabled = ? WHERE id = ?", disabled, a.ID); err != nil {
		return Account{}, 0, err
	}
	a.Disabled = disabled
	var ended int64
	if disabled {
		res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", a.ID)
		if err != nil {
			return Account{}, 0, err
		}
		ended, _ = res.RowsAffected()
	}
	action, details := AuditAccountEnabled, map[string]any{}
	if disabled {
		action, details = AuditAccountDisabled, map[string]any{"sessions_ended": ended}
	}
	if err := s.recordSite(ctx, tx, action, a.Email, details); err != nil {
		return Account{}, 0, err
	}
	return a, ended, tx.Commit()
}

// DisabledAccounts lists the disabled accounts, by email.
func (s *Service) DisabledAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, email, name, role, disabled, last_login_at FROM users WHERE disabled = 1 ORDER BY email")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Email, &a.Name, &a.Role, &a.Disabled, &a.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
