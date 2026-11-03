// Invite-only contests (migration 0009, v4.01 stage I): the students a
// contest whose audience is "invited" lets take part. Only students already
// on the sign-in list can be invited; managers pick them from the list or
// paste addresses, matched with the sign-in list's Gmail-aware rule.

package contests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// MaxInvites is the most students one add or one paste takes, the sign-in
// import's limit.
const MaxInvites = 500

// NotInvitable is the one reason every unmatched pasted line gets: staff,
// disabled people and unknown addresses read the same, so a paste does not
// tell who is on the list in another role.
const NotInvitable = "Not a student on the sign-in list"

// ErrInvitesLocked refuses removing an invite from the start until the
// final close (owner, Q7): an admin who must stop someone disables them.
var ErrInvitesLocked = errors.New("Invites cannot be removed while the contest runs.")

// Invite is one invited student.
type Invite struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	InvitedAt int64  `json:"invited_at"`
}

// InviteCandidate is an enabled student on the sign-in list, for the
// picker; Invited is true when they are already invited.
type InviteCandidate struct {
	UserID  int64  `json:"user_id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Invited bool   `json:"invited"`
}

// PasteMiss is one pasted line that matched no student.
type PasteMiss struct {
	Line   string `json:"line"`
	Reason string `json:"reason"`
}

// PasteResult is what a paste adds (or, as a dry run, would add).
type PasteResult struct {
	Added          []string    `json:"added"`
	AlreadyInvited []string    `json:"already_invited"`
	Unmatched      []PasteMiss `json:"unmatched"`
}

// Invites lists the contest's invited students by name.
func (s *Service) Invites(ctx context.Context, contestID int64) ([]Invite, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.name, u.email, i.invited_at FROM contest_invites i
		JOIN users u ON u.id = i.user_id WHERE i.contest_id = ?
		ORDER BY lower(u.name) = '', lower(u.name), u.email`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var v Invite
		if err := rows.Scan(&v.UserID, &v.Name, &v.Email, &v.InvitedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InviteCandidates lists every enabled student on the sign-in list. Staff,
// disabled people and sign-in times are left out.
func (s *Service) InviteCandidates(ctx context.Context, contestID int64) ([]InviteCandidate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.name, u.email,
		EXISTS (SELECT 1 FROM contest_invites i WHERE i.contest_id = ? AND i.user_id = u.id)
		FROM users u WHERE u.role = 'student' AND u.disabled = 0
		ORDER BY lower(u.name) = '', lower(u.name), u.email`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InviteCandidate{}
	for rows.Next() {
		var v InviteCandidate
		if err := rows.Scan(&v.UserID, &v.Name, &v.Email, &v.Invited); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AddInvites invites the enabled students among userIDs, audited for by,
// and returns how many were newly invited. Ids of anyone else are skipped.
func (s *Service) AddInvites(ctx context.Context, contestID int64, userIDs []int64, by Actor) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	if len(userIDs) > MaxInvites {
		return 0, FieldErrors{"user_ids": "Invite at most 500 students at a time."}
	}
	ids, _ := json.Marshal(userIDs)
	var added int
	err := s.audited(ctx, contestID, by, AuditInvitesAdded, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO contest_invites (contest_id, user_id, invited_by, invited_at)
			SELECT ?, id, ?, ? FROM users WHERE id IN (SELECT value FROM json_each(?)) AND role = 'student' AND disabled = 0`,
			contestID, by.ID, s.now().UnixMilli(), string(ids))
		if err != nil {
			return nil, false, err
		}
		n, _ := res.RowsAffected()
		added = int(n)
		return map[string]any{"count": added}, added > 0, nil
	})
	return added, err
}

// PasteInvites matches one address per line to the enabled students on the
// sign-in list and, unless dryRun, invites the matches, audited for by.
// Nothing is written on a dry run.
func (s *Service) PasteInvites(ctx context.Context, contestID int64, text string, dryRun bool, by Actor) (PasteResult, error) {
	out := PasteResult{Added: []string{}, AlreadyInvited: []string{}, Unmatched: []PasteMiss{}}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > MaxInvites {
		return out, FieldErrors{"text": "Paste at most 500 lines at a time."}
	}
	// A dry run only reads, so it does not hold the write gate that every
	// submission needs.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: dryRun})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// The list's canonical forms and the invites, read once per paste.
	type person struct {
		id      int64
		student bool
	}
	// As sign-in matches: the exact address first, then its canonical form
	// (Gmail dots). Should two rows share a canonical form, the exact one
	// still wins for its own address.
	exact, people := map[string]person{}, map[string]person{}
	rows, err := tx.QueryContext(ctx, `SELECT id, email, role = 'student' AND disabled = 0 FROM users ORDER BY id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p person
		var email string
		if err := rows.Scan(&p.id, &email, &p.student); err != nil {
			rows.Close()
			return out, err
		}
		exact[email] = p
		if _, seen := people[auth.Canonical(email)]; !seen {
			people[auth.Canonical(email)] = p
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	invited := map[int64]bool{}
	rows, err = tx.QueryContext(ctx, `SELECT user_id FROM contest_invites WHERE contest_id = ?`, contestID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		invited[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	var add []int64
	for _, l := range lines {
		email := strings.ToLower(strings.Trim(l, "<>\"' ,;\t"))
		p, ok := exact[email]
		if !ok {
			p, ok = people[auth.Canonical(email)]
		}
		switch {
		case !ok || !p.student:
			out.Unmatched = append(out.Unmatched, PasteMiss{Line: limitStr(l, 254), Reason: NotInvitable})
		case invited[p.id]:
			out.AlreadyInvited = append(out.AlreadyInvited, email)
		default:
			invited[p.id] = true
			add = append(add, p.id)
			out.Added = append(out.Added, email)
		}
	}
	if dryRun || len(add) == 0 {
		return out, nil
	}
	now := s.now().UnixMilli()
	for _, id := range add {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO contest_invites (contest_id, user_id, invited_by, invited_at)
			VALUES (?, ?, ?, ?)`, contestID, id, by.ID, now); err != nil {
			return out, err
		}
	}
	if err := s.recordTx(ctx, tx, contestID, by, AuditInvitesAdded, map[string]any{"count": len(add), "pasted": true}); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// InvitesLocked reports whether removing invites is refused now: from the
// contest's start until its final close, extra time included.
func (s *Service) InvitesLocked(c *Contest) bool {
	now := s.now()
	return !now.Before(c.Schedule.StartsAt) && now.Before(c.Final())
}

// RemoveInvite takes back a student's invite, audited for by. It is
// refused from the contest's start until its final close.
func (s *Service) RemoveInvite(ctx context.Context, c *Contest, userID int64, by Actor) error {
	if s.InvitesLocked(c) {
		return ErrInvitesLocked
	}
	return s.audited(ctx, c.ID, by, AuditInviteRemoved, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM contest_invites WHERE contest_id = ? AND user_id = ?`, c.ID, userID)
		if err != nil {
			return nil, false, err
		}
		n, _ := res.RowsAffected()
		return map[string]int64{"user_id": userID}, n > 0, nil
	})
}

// ListInvites answers the contest's audience and invited students.
func (d *Handlers) ListInvites(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Invites(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"audience": c.Audience, "invites": list,
		"locked": d.Contests.InvitesLocked(c), "server_now": d.Contests.Clock().UnixMilli()})
}

// InviteCandidates answers every enabled student on the sign-in list, for
// the picker, which filters on the client.
func (d *Handlers) InviteCandidates(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.InviteCandidates(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"candidates": list})
}

// AddInvites invites the picked students: {"user_ids": [...]}.
func (d *Handlers) AddInvites(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var body struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	n, err := d.Contests.AddInvites(r.Context(), c.ID, body.UserIDs, actor(r))
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"added": n})
}

// PasteInvites matches pasted addresses: {"text", "dry_run"}.
func (d *Handlers) PasteInvites(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var body struct {
		Text   string `json:"text"`
		DryRun bool   `json:"dry_run"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	res, err := d.Contests.PasteInvites(r.Context(), c.ID, body.Text, body.DryRun, actor(r))
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// RemoveInvite takes back one student's invite.
func (d *Handlers) RemoveInvite(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil || id <= 0 {
		d.contestError(w, r, ErrNotFound)
		return
	}
	if err := d.Contests.RemoveInvite(r.Context(), c, id, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
