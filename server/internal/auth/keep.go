package auth

// Keep me signed in (v2.1.0, PF-14). An owner chooses, for each role,
// whether people are signed out when the browser closes or after 12 hours
// ("nobody", today's behaviour and the default), may choose to stay signed in
// ("choose"), or always stay signed in ("everyone"), and how many idle days
// end a kept session. A kept (remembered) session has a cookie with a
// Max-Age and an expiry that moves forward on use, at most once an hour; an
// ordinary session is untouched by all of this. The setting is one row of
// the settings table, cached like the organisation (site.go) and read on
// every request.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// KeepMode is who of one role is kept signed in.
type KeepMode string

// The three settings of "Keep people signed in".
const (
	KeepNobody   KeepMode = "nobody"
	KeepChoose   KeepMode = "choose"
	KeepEveryone KeepMode = "everyone"
)

// Limits of a kept session.
const (
	// MaxKeepDays is the longest idle period an owner can set.
	MaxKeepDays = 365
	// untilSignOut is the expiry given when an owner chooses "Only when
	// they sign out": browsers keep no cookie much longer than 400 days.
	untilSignOut = 400 * 24 * time.Hour
	// renewEvery bounds the writes a kept session costs: one an hour.
	renewEvery = time.Hour
)

// AuditKeepChanged is the site audit action for a save of the setting.
const AuditKeepChanged = "keep_signed_in_changed"

const keepKey = "keep_signed_in"

// KeepPolicy is the owner's setting.
type KeepPolicy struct {
	Students   KeepMode `json:"students"`
	Organisers KeepMode `json:"organisers"`
	Admins     KeepMode `json:"admins"`
	Owner      KeepMode `json:"owner"`
	// Days is the idle period of a kept session; it is not used while
	// UntilSignOut is set.
	Days int `json:"days"`
	// UntilSignOut keeps a session until the person signs out.
	UntilSignOut bool `json:"until_sign_out"`
}

// DefaultKeepPolicy keeps nobody signed in, with a 30 day period for when
// an owner turns it on.
func DefaultKeepPolicy() KeepPolicy {
	return KeepPolicy{Students: KeepNobody, Organisers: KeepNobody, Admins: KeepNobody, Owner: KeepNobody, Days: 30}
}

// For is the setting for role r.
func (p KeepPolicy) For(r Role) KeepMode {
	switch r {
	case Owner:
		return p.Owner
	case Admin:
		return p.Admins
	case Organiser:
		return p.Organisers
	}
	return p.Students
}

// Idle is how long a kept session may go unused.
func (p KeepPolicy) Idle() time.Duration {
	if p.UntilSignOut {
		return untilSignOut
	}
	return time.Duration(p.Days) * 24 * time.Hour
}

// Any reports whether some role may be kept signed in.
func (p KeepPolicy) Any() bool {
	return p.Students != KeepNobody || p.Organisers != KeepNobody || p.Admins != KeepNobody || p.Owner != KeepNobody
}

// Started is how a new session began.
type Started struct {
	// Remembered is true for a kept session: its cookie needs a Max-Age.
	Remembered bool
	// Ignored is true when the person ticked "Keep me signed in" and their
	// role is not set to let them: they are told once.
	Ignored bool
	// Expires is when the session ends if it is not used.
	Expires time.Time
}

// KeepResult is what a save of the setting did.
type KeepResult struct {
	Policy    KeepPolicy
	SignedOut int
}

// KeepCounts is how many people hold a kept session, by role.
type KeepCounts struct {
	Students   int `json:"students"`
	Organisers int `json:"organisers"`
	Admins     int `json:"admins"`
	Owner      int `json:"owner"`
}

type keepCache struct {
	mu     sync.Mutex
	loaded bool
	p      KeepPolicy
}

// KeepPolicy returns the setting, the default until an owner saves one.
func (s *Service) KeepPolicy(ctx context.Context) (KeepPolicy, error) {
	s.keep.mu.Lock()
	defer s.keep.mu.Unlock()
	return s.loadKeep(ctx)
}

// loadKeep reads the setting once; keep.mu must be held.
func (s *Service) loadKeep(ctx context.Context) (KeepPolicy, error) {
	c := &s.keep
	if c.loaded {
		return c.p, nil
	}
	p := DefaultKeepPolicy()
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", keepKey).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return KeepPolicy{}, err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return KeepPolicy{}, err
		}
	}
	c.p, c.loaded = p, true
	return p, nil
}

func validKeepMode(m KeepMode) bool { return m == KeepNobody || m == KeepChoose || m == KeepEveryone }

// checkKeep returns what is wrong with p.
func checkKeep(p KeepPolicy, old KeepPolicy) (KeepPolicy, FieldErrors) {
	f := FieldErrors{}
	for field, m := range map[string]KeepMode{"students": p.Students, "organisers": p.Organisers, "admins": p.Admins, "owner": p.Owner} {
		if !validKeepMode(m) {
			f[field] = "Choose Nobody, Each person chooses or Everyone."
		}
	}
	if p.UntilSignOut {
		// The days are not used; keep what the owner had.
		p.Days = old.Days
	} else if p.Days < 1 || p.Days > MaxKeepDays {
		f["days"] = "Enter a whole number of days from 1 to 365."
	}
	if len(f) == 0 {
		return p, nil
	}
	return p, f
}

// SetKeepPolicy saves the setting for the actor on ctx. Every role set to
// Nobody has its kept sessions ended in the same transaction. A save that
// changes nothing writes nothing.
func (s *Service) SetKeepPolicy(ctx context.Context, p KeepPolicy) (KeepResult, error) {
	s.keep.mu.Lock()
	defer s.keep.mu.Unlock()
	old, err := s.loadKeep(ctx)
	if err != nil {
		return KeepResult{}, err
	}
	p, f := checkKeep(p, old)
	if f != nil {
		return KeepResult{}, f
	}
	if p == old {
		return KeepResult{Policy: p}, nil
	}
	body, _ := json.Marshal(p)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return KeepResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keepKey, string(body), s.now().UnixMilli()); err != nil {
		return KeepResult{}, err
	}
	var off []any
	for _, r := range []Role{Student, Organiser, Admin, Owner} {
		if p.For(r) == KeepNobody {
			off = append(off, string(r))
		}
	}
	var ended []string
	if len(off) > 0 {
		ended, err = deleteSessions(ctx, tx, "remember = 1 AND user_id IN (SELECT id FROM users WHERE role IN (?"+strings.Repeat(", ?", len(off)-1)+"))", off...)
		if err != nil {
			return KeepResult{}, err
		}
	}
	if err := s.recordSite(ctx, tx, AuditKeepChanged, "", map[string]any{"students": p.Students, "organisers": p.Organisers,
		"admins": p.Admins, "owner": p.Owner, "days": p.Days, "until_sign_out": p.UntilSignOut, "sessions_ended": len(ended)}); err != nil {
		return KeepResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return KeepResult{}, err
	}
	s.keep.p = p
	s.sessionsEnded(ended)
	return KeepResult{Policy: p, SignedOut: len(ended)}, nil
}

// KeepCounts says how many people, not sessions, hold a kept session for
// each role: what the Dashboard's confirm shows before a role is set to
// Nobody.
func (s *Service) KeepCounts(ctx context.Context) (KeepCounts, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.role, count(DISTINCT u.id) FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.remember = 1 GROUP BY u.role`)
	if err != nil {
		return KeepCounts{}, err
	}
	defer rows.Close()
	var c KeepCounts
	for rows.Next() {
		var role Role
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return KeepCounts{}, err
		}
		switch role {
		case Student:
			c.Students = n
		case Organiser:
			c.Organisers = n
		case Admin:
			c.Admins = n
		case Owner:
			c.Owner = n
		}
	}
	return c, rows.Err()
}

// SetRemember turns the session behind token into a kept session, or back
// into an ordinary one that ends 12 hours from now, for a person whose role
// is set to "Each person chooses".
func (s *Service) SetRemember(ctx context.Context, token string, keep bool) (Started, error) {
	if token == "" || len(token) > 100 {
		return Started{}, ErrNoSession
	}
	p, err := s.KeepPolicy(ctx)
	if err != nil {
		return Started{}, err
	}
	id := hashToken(token)
	var role Role
	var created int64
	err = s.DB.QueryRowContext(ctx, "SELECT u.role, s.created_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?", id).Scan(&role, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Started{}, ErrNoSession
	}
	if err != nil {
		return Started{}, err
	}
	if p.For(role) != KeepChoose {
		return Started{}, conflict("Podium does not let you choose whether to stay signed in.")
	}
	now := s.now()
	if keep {
		st := Started{Remembered: true, Expires: now.Add(p.Idle())}
		_, err := s.DB.ExecContext(ctx, "UPDATE sessions SET remember = 1, last_seen_at = ?, expires_at = ? WHERE id = ?", now.UnixMilli(), st.Expires.UnixMilli(), id)
		return st, err
	}
	// Back to an ordinary session, which ends 12 hours after sign-in:
	// toggling cannot buy a fresh 12 hours. One already past that ends.
	st := Started{Expires: time.UnixMilli(created).Add(SessionTTL)}
	if !st.Expires.After(now) {
		ended, err := deleteSessions(ctx, s.DB, "id = ?", id)
		s.sessionsEnded(ended)
		if err != nil {
			return Started{}, err
		}
		return Started{}, ErrNoSession
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE sessions SET remember = 0, last_seen_at = NULL, expires_at = ? WHERE id = ?", st.Expires.UnixMilli(), id)
	return st, err
}

// startSession inserts a session for a person of role and says how it
// began. A kept session is one the role's setting always keeps, or lets the
// person choose and they did; a tick the setting does not honour is
// reported as ignored.
func startSession(ctx context.Context, tx *sql.Tx, p KeepPolicy, userID int64, role Role, tick bool, now time.Time, ip, userAgent string) (string, Started, error) {
	mode := p.For(role)
	st := Started{Remembered: mode == KeepEveryone || (mode == KeepChoose && tick), Ignored: mode == KeepNobody && tick, Expires: now.Add(SessionTTL)}
	var seen any
	if st.Remembered {
		st.Expires = now.Add(p.Idle())
		seen = now.UnixMilli()
	}
	token := randomToken(32)
	_, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, user_id, created_at, expires_at, ip, user_agent, remember, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, now.UnixMilli(), st.Expires.UnixMilli(), limit(ip, 64), limit(userAgent, 300), st.Remembered, seen)
	return token, st, err
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// deleteSessions deletes the sessions matching where and returns their ids.
func deleteSessions(ctx context.Context, q queryer, where string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, "DELETE FROM sessions WHERE "+where+" RETURNING id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// sessionsEnded tells OnSessionsEnded, once the deletion is committed.
func (s *Service) sessionsEnded(ids []string) {
	if s.OnSessionsEnded != nil && len(ids) > 0 {
		s.OnSessionsEnded(ids)
	}
}

// Until is how long remains until t on the service's clock: the Max-Age of a
// kept session's cookie.
func (s *Service) Until(t time.Time) time.Duration { return t.Sub(s.now()) }
