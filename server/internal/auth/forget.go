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

// ErrAlreadyForgotten is returned for a placeholder account.
var ErrAlreadyForgotten = errors.New("this account was already forgotten")

// Forget erases the account with email. With apply false it reports what
// it would do and changes nothing. The actor on ctx is recorded in the
// site audit row, which names the account by id and placeholder only.
func (s *Service) Forget(ctx context.Context, email string, apply bool) (ForgetReport, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ForgetReport{}, invalid("Give the account's email.")
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
	if errors.Is(err, sql.ErrNoRows) {
		return ForgetReport{}, notFound("No account has that email.")
	}
	if err != nil {
		return ForgetReport{}, err
	}
	r.Placeholder = fmt.Sprintf("Removed user %d", r.UserID)
	r.PlaceholderEmail = fmt.Sprintf("removed-user-%d%s", r.UserID, forgottenDomain)
	steps := []struct {
		what, q string
		args    []any
	}{
		{"sessions", "DELETE FROM sessions WHERE user_id = ?", []any{r.UserID}},
		{"drafts", "DELETE FROM drafts WHERE user_id = ?", []any{r.UserID}},
		{"integrity events", "DELETE FROM integrity_events WHERE user_id = ?", []any{r.UserID}},
		{"contact messages", "DELETE FROM contact_messages WHERE user_id = ?", []any{r.UserID}},
		{"co-organiser places", "DELETE FROM contest_admins WHERE user_id = ?", []any{r.UserID}},
		{"browser hashes on submissions", "UPDATE submissions SET client_hash = '' WHERE user_id = ? AND client_hash <> ''", []any{r.UserID}},
		{"roster entries", `UPDATE contest_roster SET email = ?, display_name = ?, roll_number = '', section = '' WHERE email = ?`,
			[]any{r.PlaceholderEmail, r.Placeholder, r.Email}},
	}
	for _, st := range steps {
		res, err := tx.ExecContext(ctx, st.q, st.args...)
		if err != nil {
			return ForgetReport{}, fmt.Errorf("%s: %w", st.what, err)
		}
		n, _ := res.RowsAffected()
		r.Counts = append(r.Counts, ForgetCount{st.what, n})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email = ?, name = ?, avatar_url = '', google_sub = NULL, disabled = 1,
		role = 'student' WHERE id = ?`, r.PlaceholderEmail, r.Placeholder, r.UserID); err != nil {
		return ForgetReport{}, err
	}
	details := map[string]any{"user_id": r.UserID, "placeholder": r.Placeholder, "role": r.Role}
	for _, c := range r.Counts {
		details[strings.ReplaceAll(c.What, " ", "_")] = c.N
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
