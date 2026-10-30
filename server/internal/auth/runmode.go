package auth

import (
	"context"
	"database/sql"
	"errors"
)

// ModeChange is what RecordMode found and did at start-up.
type ModeChange struct {
	// Previous is the mode the database last ran in; empty when it has no
	// record (a database from before the record existed).
	Previous string
	// Changed is true when Previous differs from the mode now.
	Changed bool
	// SessionsEnded and Demoted count what the change undid.
	SessionsEnded int64
	Demoted       int64
}

// RecordMode compares the run mode with the one the database last ran in
// and stores the new one. Testing mode signs anyone
// in as anyone, so nothing made in it may survive into another mode:
//
//   - On any change of mode (or no record yet) every session ends.
//   - On a change to dev or production, every organiser, admin and owner without a
//     linked Google account becomes a student again, except the configured
//     owner. Testing-mode sign-ins never link one; neither does a role
//     granted to someone who has not signed in with Google yet, so such a
//     grant has to be made again after the switch.
//
// Call it once at start-up, after Bootstrap.
func (s *Service) RecordMode(ctx context.Context, mode string) (ModeChange, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ModeChange{}, err
	}
	defer tx.Rollback()
	var ch ModeChange
	err = tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'mode'").Scan(&ch.Previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ModeChange{}, err
	}
	if err == nil && ch.Previous == mode {
		return ch, nil
	}
	ch.Changed = true
	res, err := tx.ExecContext(ctx, "DELETE FROM sessions")
	if err != nil {
		return ModeChange{}, err
	}
	ch.SessionsEnded, _ = res.RowsAffected()
	if mode != "testing" {
		res, err := tx.ExecContext(ctx, `UPDATE users SET role = 'student'
			WHERE role IN ('organiser', 'admin', 'owner') AND google_sub IS NULL AND email <> ?`, s.OwnerEmail)
		if err != nil {
			return ModeChange{}, err
		}
		ch.Demoted, _ = res.RowsAffected()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('mode', ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, mode, s.now().UnixMilli()); err != nil {
		return ModeChange{}, err
	}
	from := ch.Previous
	if from == "" {
		from = "none"
	}
	if err := s.recordSite(ctx, tx, AuditModeChanged, "", map[string]any{"from": from, "to": mode,
		"sessions_ended": ch.SessionsEnded, "demoted": ch.Demoted}); err != nil {
		return ModeChange{}, err
	}
	return ch, tx.Commit()
}
