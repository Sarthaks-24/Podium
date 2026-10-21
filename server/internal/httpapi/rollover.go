package httpapi

// The semester rollover (B3.3): an owner action that archives the term's
// contests and disables student accounts not seen for 180 days. A
// preview unless apply is true. docs/API.md, "POST /api/admin/rollover".

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
)

func (d Deps) semesterRollover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label       string `json:"label"`
		EndedBefore *int64 `json:"ended_before"`
		Apply       bool   `json:"apply"`
		// AsOf is the preview's server_now: an apply with it disables only
		// accounts that were idle at the preview, so nobody who crossed
		// the 180 days in between is disabled unseen (C2-033).
		AsOf *int64 `json:"as_of"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || utf8.RuneCountInString(label) > 80 || strings.IndexFunc(label, notOneLine) >= 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_request",
			Message: "Name the term in one line of at most 80 characters, such as 2026 odd semester.",
			Fields:  map[string]string{"label": "Name the term, such as 2026 odd semester."}}})
		return
	}
	now := d.now()
	before := now
	if req.EndedBefore != nil {
		before = time.UnixMilli(*req.EndedBefore)
	}
	ctx := r.Context()
	list := []contests.RolloverContest{}
	if d.Contests != nil {
		var err error
		if list, err = d.Contests.EndedBefore(ctx, before); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	seenBefore := now.Add(-auth.IdleStudentPeriod)
	if req.AsOf != nil {
		asOf := time.UnixMilli(*req.AsOf)
		if asOf.After(now) || now.Sub(asOf) > time.Hour {
			WriteError(w, http.StatusBadRequest, "bad_request", "This preview is more than an hour old. Preview the rollover again, then apply it.")
			return
		}
		seenBefore = asOf.Add(-auth.IdleStudentPeriod)
	}
	idle, err := d.Auth.IdleStudents(ctx, seenBefore)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if req.Apply {
		archived := make([]map[string]any, 0, len(list))
		for _, c := range list {
			if err := d.Contests.Archive(ctx, c.ID); err != nil {
				d.internalError(w, r, err)
				return
			}
			d.audit(r, c.ID, contests.AuditArchive, map[string]any{"rollover": label})
			archived = append(archived, map[string]any{"id": c.ID, "slug": c.Slug})
		}
		rep, err := d.Auth.DisableIdle(ctx, seenBefore, label, archived)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		idle = rep.Disabled
		if idle == nil {
			idle = []auth.Account{}
		}
		d.Log.Info("semester rollover", "by", userFrom(ctx).ID, "contests", len(list), "accounts", len(idle))
	}
	WriteJSON(w, http.StatusOK, map[string]any{"applied": req.Apply, "label": label, "ended_before": before.UnixMilli(),
		"contests": list, "accounts": idle, "idle_days": int(auth.IdleStudentPeriod / (24 * time.Hour)), "server_now": now.UnixMilli()})
}

// notOneLine reports a character a one-line label may not hold: a control
// character, a line or paragraph separator, or a hidden format character
// that could make the audit row read differently from what it says.
func notOneLine(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || auth.Hidden(r)
}
