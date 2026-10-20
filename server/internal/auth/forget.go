package auth

// Erasure (v2 plan section 9, B3.2): podium-server forget-user. What a
// person did stays countable, so past standings remain correct, but
// nothing left names them: their sessions, drafts, integrity events,
// contact messages and the browser hashes on their submissions go; their
// name and email become a placeholder; the Google link is cleared and
// the account is disabled, so the next sign-in with that address makes a
// new account. Audit rows written before keep the address they recorded
// until the audit retention (4 years) removes them: they are append only
// by design.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AuditUserForgotten is the site audit action of an erasure.
const AuditUserForgotten = "user_forgotten"

// forgottenDomain ends every placeholder address: .invalid is reserved
// (RFC 2606), so no Google account can ever sign in with one.
const forgottenDomain = "@removed.invalid"

// ForgetReport is what an erasure changed, or would change.
type ForgetReport struct {
	UserID      int64
	Email, Name string
	Role        Role
	// Placeholder is the name the account takes; PlaceholderEmail its
	// address.
	Placeholder, PlaceholderEmail string
	// Counts maps what was deleted or cleared to how many rows.
	Counts []ForgetCount
	// Applied is false for a dry run, which changed nothing.
	Applied bool
}

// ForgetCount is one line of the report.
type ForgetCount struct {
	What string
	N    int64
}

// ErrForgetOwner refuses erasing PODIUM_OWNER_EMAIL, the recovery account.
var ErrForgetOwner = errors.New("the configured owner (PODIUM_OWNER_EMAIL) cannot be forgotten; change PODIUM_OWNER_EMAIL first")

// ErrForgetOwnerRole refuses erasing another owner without the caller's
// explicit say-so: take the role away first, or pass allowOwner.
var ErrForgetOwnerRole = errors.New("this account is an owner; remove the owner role on People first, or pass -allow-owner")

// ErrNoOwnerEmail: the configured owner is unknown, so the guard above
// cannot work (a shell without /etc/podium/server.env loaded).
var ErrNoOwnerEmail = errors.New("PODIUM_OWNER_EMAIL is not set; pass -owner with the configured owner address, or run with the server environment loaded")

// ErrAlreadyForgotten is returned for a placeholder account.
var ErrAlreadyForgotten = errors.New("this account was already forgotten")

// ForgetKept lists what an erasure leaves on purpose, for the report.
var ForgetKept = []string{
	"submissions, scores and code, under the placeholder (past standings stay correct)",
	"integrity cases, explanations and decisions: kept until the case retention (2 years after the decision)",
	"audit rows written before today, with the address and text they recorded: kept until the audit retention (4 years)",
	"backups: until they age out (14 days)",
}

// removedText replaces a forgotten person's clarification questions; the
// organisers' answers stay.
const removedText = "Removed at the request of the person who asked."

// Forget erases the account with email. With apply false it reports what
// it would do and changes nothing. The actor on ctx is recorded in the
// site audit row, which names the account by id and placeholder only. A
// person on a roster who never signed in has no account: then only the
// roster rows are erased. An owner account needs allowOwner.
func (s *Service) Forget(ctx context.Context, email string, apply, allowOwner bool) (ForgetReport, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ForgetReport{}, invalid("Give the account's email.")
	}
	if s.OwnerEmail == "" {
		return ForgetReport{}, ErrNoOwnerEmail
	}
	if email == s.OwnerEmail {
		return ForgetReport{}, ErrForgetOwner
	}
	if strings.HasSuffix(email, forgottenDomain) {
		return ForgetReport{}, ErrAlreadyForgotten
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ForgetReport{}, err
	}
	defer tx.Rollback()
	var r ForgetReport
	err = tx.QueryRowContext(ctx, "SELECT id, email, name, role FROM users WHERE email = ?", email).Scan(&r.UserID, &r.Email, &r.Name, &r.Role)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Roster only: a random placeholder, since there is no id.
		r.Email = email
		r.Placeholder = "Removed person"
		r.PlaceholderEmail = "removed-" + strings.ToLower(randomToken(9)) + forgottenDomain
	case err != nil:
		return ForgetReport{}, err
	case r.Role == Owner && !allowOwner:
		return ForgetReport{}, ErrForgetOwnerRole
	default:
		r.Placeholder = fmt.Sprintf("Removed user %d", r.UserID)
		r.PlaceholderEmail = fmt.Sprintf("removed-user-%d%s", r.UserID, forgottenDomain)
	}
	type step struct {
		what, q string
		args    []any
	}
	var steps []step
	if r.UserID > 0 {
		steps = []step{
			{"sessions", "DELETE FROM sessions WHERE user_id = ?", []any{r.UserID}},
			{"drafts", "DELETE FROM drafts WHERE user_id = ?", []any{r.UserID}},
			{"integrity events", "DELETE FROM integrity_events WHERE user_id = ?", []any{r.UserID}},
			{"contact messages", "DELETE FROM contact_messages WHERE user_id = ?", []any{r.UserID}},
			{"co-organiser places", "DELETE FROM contest_admins WHERE user_id = ?", []any{r.UserID}},
			{"browser hashes on submissions", "UPDATE submissions SET client_hash = '' WHERE user_id = ? AND client_hash <> ''", []any{r.UserID}},
			// An extension's reason can be an accommodation, which is
			// health data (security review 2026-10-20, finding 4).
			{"extension reasons", "UPDATE contest_extensions SET reason = '' WHERE user_id = ? AND reason <> ''", []any{r.UserID}},
			{"clarification questions", "UPDATE clarifications SET question = ? WHERE asked_by = ? AND question <> ?",
				[]any{removedText, r.UserID, removedText}},
		}
	}
	steps = append(steps, step{"roster entries", `UPDATE contest_roster SET email = ?, display_name = ?, roll_number = '', section = '' WHERE email = ?`,
		[]any{r.PlaceholderEmail, r.Placeholder, r.Email}})
	for _, st := range steps {
		res, err := tx.ExecContext(ctx, st.q, st.args...)
		if err != nil {
			return ForgetReport{}, fmt.Errorf("%s: %w", st.what, err)
		}
		n, _ := res.RowsAffected()
		r.Counts = append(r.Counts, ForgetCount{st.what, n})
	}
	if r.UserID == 0 && r.Counts[len(r.Counts)-1].N == 0 {
		return ForgetReport{}, notFound("No account and no roster entry has that email.")
	}
	if r.UserID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET email = ?, name = ?, avatar_url = '', google_sub = NULL, disabled = 1,
			role = 'student' WHERE id = ?`, r.PlaceholderEmail, r.Placeholder, r.UserID); err != nil {
			return ForgetReport{}, err
		}
	}
	details := map[string]any{"user_id": r.UserID, "placeholder": r.Placeholder, "role": r.Role}
	if r.UserID == 0 {
		details["roster_only"] = true
	}
	for _, c := range r.Counts {
		details[strings.NewReplacer(" ", "_", "-", "_").Replace(c.What)] = c.N
	}
	if err := s.recordSite(ctx, tx, AuditUserForgotten, "", details); err != nil {
		return ForgetReport{}, err
	}
	if !apply {
		return r, nil
	}
	if err := tx.Commit(); err != nil {
		return ForgetReport{}, err
	}
	r.Applied = true
	return r, nil
}
