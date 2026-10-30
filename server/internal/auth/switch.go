package auth

import (
	"context"
	"database/sql"
	"errors"
)

// The sign-in switch (V4-17, stage O O6): an owner or an admin can close
// sign-in, for example once a test run is full. While it is closed nobody
// below admin starts a new session; sessions already open keep working.
// It is one row of the settings table, open when there is none.

// ErrSignInClosed refuses a new sign-in while the switch is closed.
var ErrSignInClosed = errors.New("sign-in is closed")

// Audit actions of the switch.
const (
	AuditSignInClosed = "signin_closed"
	AuditSignInOpened = "signin_opened"
)

const signInKey = "signin_open"

// SignInOpen reports whether new sign-ins are accepted.
func (s *Service) SignInOpen(ctx context.Context) (bool, error) {
	return signInOpen(ctx, s.DB)
}

func signInOpen(ctx context.Context, q rowQuerier) (bool, error) {
	var v string
	err := q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", signInKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return v != "0", err
}

// SetSignInOpen opens or closes sign-in and records the change. Setting it
// to what it already is changes nothing and writes no audit row.
func (s *Service) SetSignInOpen(ctx context.Context, open bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	was, err := signInOpen(ctx, tx)
	if err != nil || was == open {
		return err
	}
	v, action := "1", AuditSignInOpened
	if !open {
		v, action = "0", AuditSignInClosed
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, signInKey, v, s.now().UnixMilli()); err != nil {
		return err
	}
	if err := s.recordSite(ctx, tx, action, "", nil); err != nil {
		return err
	}
	return tx.Commit()
}
