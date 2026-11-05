// Per-contest organisers (migration 0008): the people who manage one
// contest besides admins and the owner.

package contests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// Organiser is one person listed as a manager of a contest.
type Organiser struct {
	UserID  int64  `json:"user_id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	AddedAt int64  `json:"added_at"`
}

// Organisers lists the contest's listed managers by email.
func (s *Service) Organisers(ctx context.Context, contestID int64) ([]Organiser, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,u.email,u.name,u.role,o.added_at FROM contest_organisers o JOIN users u ON u.id=o.user_id WHERE o.contest_id=? ORDER BY u.email COLLATE NOCASE`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Organiser{}
	for rows.Next() {
		var o Organiser
		if err := rows.Scan(&o.UserID, &o.Email, &o.Name, &o.Role, &o.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrganiserNames are the names the contest page shows as its organisers:
// listed people who are still enabled staff, in the order they were added
// (the creator first). Staff see each by name or, without one, by email.
// Students see names only: an organiser with no name is left out, so an
// account email never reaches a student.
func (s *Service) OrganiserNames(ctx context.Context, contestID int64, staff bool) ([]string, error) {
	nameOnly := ""
	if !staff {
		nameOnly = " AND trim(u.name) <> ''"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT coalesce(nullif(trim(u.name), ''), u.email) FROM contest_organisers o
		JOIN users u ON u.id = o.user_id
		WHERE o.contest_id = ? AND u.disabled = 0 AND u.role IN ('organiser', 'admin', 'owner')`+nameOnly+`
		ORDER BY o.added_at, o.user_id`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// AddOrganiser lists userID as a manager of the contest, audited for by.
// Adding someone already listed writes nothing and returns their
// original added_at.
func (s *Service) AddOrganiser(ctx context.Context, contestID, userID int64, by Actor) (int64, error) {
	var at int64
	err := s.audited(ctx, contestID, by, AuditOrganiserAdded, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO contest_organisers(contest_id,user_id,added_by,added_at) VALUES(?,?,?,?)`, contestID, userID, by.ID, s.now().UnixMilli())
		if err != nil {
			return nil, false, err
		}
		n, _ := res.RowsAffected()
		var email string
		if err := tx.QueryRowContext(ctx, `SELECT o.added_at, u.email FROM contest_organisers o JOIN users u ON u.id = o.user_id WHERE o.contest_id=? AND o.user_id=?`, contestID, userID).Scan(&at, &email); err != nil {
			return nil, false, err
		}
		return map[string]any{"user_id": userID, "email": email}, n > 0, nil
	})
	return at, err
}

// RemoveOrganiser takes userID off the contest's managers, audited for by.
// Removing someone not listed writes nothing.
func (s *Service) RemoveOrganiser(ctx context.Context, contestID, userID int64, by Actor) error {
	return s.audited(ctx, contestID, by, AuditOrganiserRemoved, func(tx *sql.Tx) (any, bool, error) {
		var email string
		if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id=?`, userID).Scan(&email); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM contest_organisers WHERE contest_id=? AND user_id=?`, contestID, userID)
		if err != nil {
			return nil, false, err
		}
		n, _ := res.RowsAffected()
		return map[string]any{"user_id": userID, "email": email}, n > 0, nil
	})
}

// ListOrganisers answers the contest's managers.
func (d *Handlers) ListOrganisers(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Organisers(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"organisers": list, "server_now": d.Contests.Clock().UnixMilli()})
}

// AddOrganiser lists a person whose role is organiser or above as a
// manager of the contest. Anyone else answers 404 with one message.
func (d *Handlers) AddOrganiser(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.Email == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "Enter a listed organiser email.")
		return
	}
	u, found, err := d.Auth.ListedPerson(r.Context(), body.Email)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if !found || !u.Role.AtLeast(auth.Organiser) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "That person is not a listed organiser.")
		return
	}
	at, err := d.Contests.AddOrganiser(r.Context(), c.ID, u.ID, actor(r))
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Organiser{UserID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role), AddedAt: at})
}

// RemoveOrganiser takes a person off the contest's managers; they drop to
// view on their next request.
func (d *Handlers) RemoveOrganiser(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil || id <= 0 {
		d.contestError(w, r, ErrNotFound)
		return
	}
	if err = d.Contests.RemoveOrganiser(r.Context(), c.ID, id, actor(r)); err != nil {
		d.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
