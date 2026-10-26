package auth

// The site audit (v2 plan B2.7, decision D62): one append-only row for
// every change to people, domains and the site, written in the same
// transaction as the change. A row names the actor (as they were at the
// time), the action, the person it was about and a small JSON object of
// fixed fields. Nothing from a request body, header or cookie is ever
// copied into it except normalised emails and domains, so no AI key,
// session token, cookie or OAuth code can land here (the leak test in
// httpapi proves it).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Site audit actions (docs/API.md, "Site audit: People History").
const (
	AuditRoleGranted     = "role_granted"
	AuditRoleRevoked     = "role_revoked"
	AuditDomainAdded     = "domain_added"
	AuditDomainRemoved   = "domain_removed"
	AuditAccountDisabled = "account_disabled"
	AuditAccountEnabled  = "account_enabled"
	AuditSessionsEnded   = "sessions_ended"
	AuditGoogleReset     = "google_reset"
	AuditModeChanged     = "mode_changed"
)

// Actor is the person an audit row names, as they were when they acted.
type Actor struct {
	ID    int64  `json:"user_id"`
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

type actorKey struct{}

// WithActor returns ctx carrying the person acting, for the audit rows the
// changes below write. Without one a row has no actor: the server itself.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor WithActor stored, if any.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok && a.ID > 0
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// recordSite appends a site audit row for the change tx makes.
func (s *Service) recordSite(ctx context.Context, tx execer, action, subject string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("site audit details: %w", err)
	}
	var id any
	var email, role string
	if a, ok := ActorFrom(ctx); ok {
		id, email, role = a.ID, a.Email, string(a.Role)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO site_audit (at, actor_id, actor_email, actor_role, action, subject_email, details)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, s.now().UnixMilli(), id, email, role, action, strings.ToLower(subject), string(b)); err != nil {
		return fmt.Errorf("record %s in the site audit: %w", action, err)
	}
	return nil
}

// SiteAuditEntry is one row of the site audit.
type SiteAuditEntry struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Action string `json:"action"`
	// Actor is nil for the server's own changes.
	Actor        *Actor          `json:"actor"`
	SubjectEmail string          `json:"subject_email"`
	Details      json.RawMessage `json:"details"`
}

// SiteAuditFilter narrows the People page's History.
type SiteAuditFilter struct {
	Action string
	// Q matches the actor's or the subject's email, case insensitive.
	Q            string
	Since, Until int64
	Page         int
}

// SiteAuditPage is how many rows one page holds.
const SiteAuditPage = 100

// SiteAudit lists site audit rows, newest first, a page at a time, and
// reports whether there are more.
func (s *Service) SiteAudit(ctx context.Context, f SiteAuditFilter) ([]SiteAuditEntry, bool, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	q := `SELECT id, at, action, coalesce(actor_id, 0), actor_email, actor_role, subject_email, details FROM site_audit WHERE 1 = 1`
	var args []any
	if f.Action != "" {
		q += " AND action = ?"
		args = append(args, f.Action)
	}
	if f.Q != "" {
		q += " AND (instr(lower(actor_email), ?) > 0 OR instr(lower(subject_email), ?) > 0)"
		args = append(args, strings.ToLower(f.Q), strings.ToLower(f.Q))
	}
	if f.Since > 0 {
		q += " AND at >= ?"
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		q += " AND at < ?"
		args = append(args, f.Until)
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, SiteAuditPage+1, (f.Page-1)*SiteAuditPage)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []SiteAuditEntry{}
	for rows.Next() {
		var e SiteAuditEntry
		var a Actor
		var details string
		if err := rows.Scan(&e.ID, &e.At, &e.Action, &a.ID, &a.Email, &a.Role, &e.SubjectEmail, &details); err != nil {
			return nil, false, err
		}
		if a.ID > 0 {
			e.Actor = &a
		}
		e.Details = json.RawMessage(details)
		out = append(out, e)
	}
	more := len(out) > SiteAuditPage
	if more {
		out = out[:SiteAuditPage]
	}
	return out, more, rows.Err()
}
