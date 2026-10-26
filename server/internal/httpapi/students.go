package httpapi

// The organisers' students table: everyone who signed in as a student,
// with how many contests each took part in and their last submission.
// Owners and admins only. Results per contest live in each contest's
// standings and CSV export, not here.

import (
	"net/http"
	"strings"

	"github.com/Sarthaks-24/podium/internal/auth"
)

func (d Deps) studentsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/students", d.requireRole(auth.Admin, d.limitReads(d.listStudents)))
}

// StudentRow is one line of the students table.
type StudentRow struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	Contests         int    `json:"contests"`
	LastSubmissionAt *int64 `json:"last_submission_at"`
	Disabled         bool   `json:"disabled,omitempty"`
}

// listStudents answers the table, filtered by q (a part of the name or
// email), sorted by name.
func (d Deps) listStudents(w http.ResponseWriter, r *http.Request) {
	q := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))) + "%"
	rows, err := d.DB.QueryContext(r.Context(), `SELECT u.id, coalesce(nullif(u.name, ''), u.email), u.email, u.disabled,
		(SELECT count(DISTINCT contest_id) FROM (
			SELECT contest_id FROM contest_starts WHERE user_id = u.id
			UNION SELECT contest_id FROM submissions WHERE user_id = u.id AND contest_id IS NOT NULL AND kind = 'submit' AND practice = 0)),
		(SELECT max(created_at) FROM submissions WHERE user_id = u.id AND kind = 'submit')
		FROM users u WHERE u.role = 'student' AND u.email NOT LIKE '%@removed.invalid'
		AND (lower(u.name) LIKE ? ESCAPE '\' OR lower(u.email) LIKE ? ESCAPE '\')
		ORDER BY lower(coalesce(nullif(u.name, ''), u.email)), u.id LIMIT 2000`, q, q)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []StudentRow{}
	for rows.Next() {
		var s StudentRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.Disabled, &s.Contests, &s.LastSubmissionAt); err != nil {
			d.internalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"students": out})
}
