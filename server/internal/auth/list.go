package auth

// The list of people (V4-15, V4-18): every row of users is one person who
// may sign in, unless disabled. Owners and admins add people one at a time
// or by pasting a list, remove them, and let in an address that was
// refused. Addresses are stored as typed and compared through canonical,
// so a Gmail address matches however its dots are placed.

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
)

// ErrNotListed refuses a verified Google email that is not on the list.
var ErrNotListed = errors.New("email not on the list")

// MaxImport is the most addresses one paste may add.
const MaxImport = 500

// MaxRefused is how many refused addresses are kept, the most recent.
const MaxRefused = 500

// Audit actions of the list.
const (
	AuditPersonAdded   = "person_added"
	AuditPersonRemoved = "person_removed"
	AuditListImported  = "list_imported"
	AuditLetIn         = "let_in"
)

// canonical is the form two addresses are compared in: trimmed and lower
// case, and for Gmail without the dots of the local part (Gmail ignores
// them) and with googlemail.com read as gmail.com. Nothing else is
// rewritten: a "+tag" is a different address.
func canonical(email string) string {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return e
	}
	if d := e[at+1:]; d == "gmail.com" || d == "googlemail.com" {
		return strings.ReplaceAll(e[:at], ".", "") + "@gmail.com"
	}
	return e
}

func isGmail(canon string) bool { return strings.HasSuffix(canon, "@gmail.com") }

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// listed finds the row for email: the same address first, then, for
// Gmail, any row whose canonical form is the same. found is false when
// nobody on the list has it.
func listed(ctx context.Context, q querier, email string) (id int64, stored string, found bool, err error) {
	err = q.QueryRowContext(ctx, "SELECT id, email FROM users WHERE email = ?", strings.ToLower(strings.TrimSpace(email))).Scan(&id, &stored)
	if err == nil {
		return id, stored, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, err
	}
	canon := canonical(email)
	if !isGmail(canon) {
		return 0, "", false, nil
	}
	// shortcut: reads every Gmail row and compares in Go; fine for a list of a few hundred, a canonical column if it grows to thousands.
	rows, err := q.QueryContext(ctx, "SELECT id, email FROM users WHERE email LIKE '%@gmail.com' OR email LIKE '%@googlemail.com'")
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&id, &stored); err != nil {
			return 0, "", false, err
		}
		if canonical(stored) == canon {
			return id, stored, true, nil
		}
	}
	return 0, "", false, rows.Err()
}

// recordRefusal notes a refused address in its own transaction (the
// sign-in's is rolled back) and keeps only the MaxRefused most recent.
func (s *Service) recordRefusal(ctx context.Context, email string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO refused_signins (email, count, first_at, last_at) VALUES (?, 1, ?, ?)
		ON CONFLICT (email) DO UPDATE SET count = count + 1, last_at = excluded.last_at`, limit(email, 254), now, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM refused_signins WHERE email NOT IN (
		SELECT email FROM refused_signins ORDER BY last_at DESC, email LIMIT ?)`, MaxRefused); err != nil {
		return err
	}
	return tx.Commit()
}

type txExecer interface {
	execer
	querier
}

// forgetRefusal removes the refused rows of an address now on the list.
func forgetRefusal(ctx context.Context, tx txExecer, email string) error {
	canon := canonical(email)
	rows, err := tx.QueryContext(ctx, "SELECT email FROM refused_signins")
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return err
		}
		if canonical(e) == canon {
			drop = append(drop, e)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range drop {
		if _, err := tx.ExecContext(ctx, "DELETE FROM refused_signins WHERE email = ?", e); err != nil {
			return err
		}
	}
	return nil
}

// validEmail reports whether v is one plain address, as the list stores it.
func validEmail(v string) bool {
	a, err := mail.ParseAddress(v)
	return err == nil && a.Address == v && len(v) <= 254 && strings.LastIndexByte(v, '@') > 0
}

// ImportResult is what a paste would do (a dry run) or did.
type ImportResult struct {
	Added      []string `json:"added"`
	Duplicates []string `json:"duplicates"`
	Invalid    []string `json:"invalid"`
}

// Import adds the addresses in text (separated by commas, semicolons,
// spaces or new lines) to the list as students. A duplicate, already on
// the list or earlier in the same paste (compared through canonical), is
// skipped; so is anything that is not one plain address. With dryRun
// nothing is saved, so the page can show the three counts first; the
// second call saves the same answer. Adding one person is a paste of one.
func (s *Service) Import(ctx context.Context, text string, dryRun bool) (ImportResult, error) {
	out := ImportResult{Added: []string{}, Duplicates: []string{}, Invalid: []string{}}
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if len(fields) > MaxImport {
		return out, invalid("Paste at most 500 addresses at a time.")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err := s.changeAllowed(ctx, "", "", Student); err != nil {
		return out, err
	}
	seen := map[string]bool{}
	now := s.now().UnixMilli()
	for _, f := range fields {
		email := strings.ToLower(strings.Trim(f, "<>\"'"))
		if !validEmail(email) {
			out.Invalid = append(out.Invalid, limit(f, 254))
			continue
		}
		canon := canonical(email)
		if seen[canon] {
			out.Duplicates = append(out.Duplicates, email)
			continue
		}
		seen[canon] = true
		_, _, found, err := listed(ctx, tx, email)
		if err != nil {
			return out, err
		}
		if found {
			out.Duplicates = append(out.Duplicates, email)
			continue
		}
		out.Added = append(out.Added, email)
		if dryRun {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES (?, 'student', ?)", email, now); err != nil {
			return out, err
		}
		if err := forgetRefusal(ctx, tx, email); err != nil {
			return out, err
		}
	}
	if dryRun || len(out.Added) == 0 {
		return out, nil
	}
	action, subject, details := AuditListImported, "", map[string]any{"added": len(out.Added), "duplicates": len(out.Duplicates), "invalid": len(out.Invalid)}
	if len(out.Added) == 1 {
		action, subject, details = AuditPersonAdded, out.Added[0], map[string]any{}
	}
	if err := s.recordSite(ctx, tx, action, subject, details); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// LetIn adds a refused address to the list as a student and forgets the
// refusal.
func (s *Service) LetIn(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return invalid("Enter a single email address, such as asha@college.edu.")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.changeAllowed(ctx, email, "", Student); err != nil {
		return err
	}
	_, _, found, err := listed(ctx, tx, email)
	if err != nil {
		return err
	}
	if !found {
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES (?, 'student', ?)", email, s.now().UnixMilli()); err != nil {
			return err
		}
		if err := s.recordSite(ctx, tx, AuditLetIn, email, nil); err != nil {
			return err
		}
	}
	if err := forgetRefusal(ctx, tx, email); err != nil {
		return err
	}
	return tx.Commit()
}

// Remove takes a person off the list. Someone who never signed in is
// deleted; someone who did is disabled and signed out instead, because
// their submissions point at the row. deleted says which happened.
func (s *Service) Remove(ctx context.Context, email string) (deleted bool, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	a, err := accountByEmail(ctx, tx, email)
	if err != nil {
		return false, err
	}
	if err := s.changeAllowed(ctx, a.Email, a.Role, a.Role); err != nil {
		return false, err
	}
	if a.Email == s.OwnerEmail {
		return false, conflict("This owner is set in the server configuration and cannot be removed.")
	}
	if actor, _ := ActorFrom(ctx); actor.ID == a.ID {
		return false, conflict("You cannot remove yourself.")
	}
	if a.Role == Owner {
		if err := lastOwnerGuard(ctx, tx); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ? AND google_sub IS NULL AND last_login_at IS NULL", a.ID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	deleted = n == 1
	var ended int64
	if !deleted {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET disabled = 1 WHERE id = ?", a.ID); err != nil {
			return false, err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", a.ID)
		if err != nil {
			return false, err
		}
		ended, _ = res.RowsAffected()
	}
	if err := s.recordSite(ctx, tx, AuditPersonRemoved, a.Email, map[string]any{"deleted": deleted, "sessions_ended": ended}); err != nil {
		return false, err
	}
	return deleted, tx.Commit()
}

// ListedPerson is one row of the list as People shows it.
type ListedPerson struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Role        Role   `json:"role"`
	LastLoginAt *int64 `json:"last_login_at"`
	Disabled    bool   `json:"disabled"`
	// Locked marks the configured owner, whose role cannot be changed.
	Locked bool `json:"locked"`
}

// ListCounts are the People page's three counts.
type ListCounts struct {
	OnList    int `json:"on_list"`
	SignedIn  int `json:"signed_in"`
	NeverCame int `json:"never_came"`
}

// Refusal is one refused address.
type Refusal struct {
	Email   string `json:"email"`
	Count   int    `json:"count"`
	FirstAt int64  `json:"first_at"`
	LastAt  int64  `json:"last_at"`
}

// List returns everyone on the list (owners, admins, organisers, then
// students, each by name), the counts, and the refused addresses, newest
// first. Disabled people are on it and counted apart from neither count.
func (s *Service) List(ctx context.Context) ([]ListedPerson, ListCounts, []Refusal, error) {
	var c ListCounts
	rows, err := s.DB.QueryContext(ctx, `SELECT id, email, name, role, last_login_at, disabled FROM users
		ORDER BY CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'organiser' THEN 2 ELSE 3 END, lower(name) = '', lower(name), email`)
	if err != nil {
		return nil, c, nil, err
	}
	people := []ListedPerson{}
	for rows.Next() {
		var p ListedPerson
		if err := rows.Scan(&p.ID, &p.Email, &p.Name, &p.Role, &p.LastLoginAt, &p.Disabled); err != nil {
			rows.Close()
			return nil, c, nil, err
		}
		p.Locked = p.Email == s.OwnerEmail
		people = append(people, p)
		if !p.Disabled {
			c.OnList++
			if p.LastLoginAt != nil {
				c.SignedIn++
			} else {
				c.NeverCame++
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, c, nil, err
	}
	refused := []Refusal{}
	rows, err = s.DB.QueryContext(ctx, "SELECT email, count, first_at, last_at FROM refused_signins ORDER BY last_at DESC, email")
	if err != nil {
		return nil, c, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Refusal
		if err := rows.Scan(&r.Email, &r.Count, &r.FirstAt, &r.LastAt); err != nil {
			return nil, c, nil, err
		}
		refused = append(refused, r)
	}
	return people, c, refused, rows.Err()
}
