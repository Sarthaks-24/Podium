package contests

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

type Organiser struct {
	UserID  int64  `json:"user_id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	AddedAt int64  `json:"added_at"`
}

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

func (d *Handlers) AddOrganiser(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
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
	now := d.Contests.Clock().UnixMilli()
	if _, err = d.Contests.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO contest_organisers(contest_id,user_id,added_by,added_at) VALUES(?,?,?,?)`, c.ID, u.ID, auth.UserFrom(r.Context()).ID, now); err != nil {
		d.internalError(w, r, err)
		return
	}
	if err = d.Contests.DB.QueryRowContext(r.Context(), `SELECT added_at FROM contest_organisers WHERE contest_id=? AND user_id=?`, c.ID, u.ID).Scan(&now); err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Organiser{UserID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role), AddedAt: now})
}

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
	if _, err = d.Contests.DB.ExecContext(r.Context(), `DELETE FROM contest_organisers WHERE contest_id=? AND user_id=?`, c.ID, id); err != nil {
		d.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
