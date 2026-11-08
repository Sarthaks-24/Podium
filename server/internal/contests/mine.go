package contests

// Your contests on the profile page (v4 M2): every contest the signed-in
// person entered, newest first, with their score, their place and the
// contest's date. The place is read from the board this person would see
// now: while the contest shows students a frozen board (the freeze, or the
// organisers holding the reveal) there is none, so the profile never says
// more than the standings page does. No new table: entering is a
// contest_starts row or a non-practice submission, as for the board. An
// archived contest is left out: students cannot open it any more. Each row
// costs a state, a score and a cached board read, which suits one page for
// one person, not a list of everyone.

import (
	"context"
	"errors"
	"net/http"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

type myContestJSON struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	StartsAt int64  `json:"starts_at"`
	State    string `json:"state"`
	Score    int    `json:"score"`
	MaxScore int    `json:"max_score"`
	// Place and Of are absent while the board students see is frozen, and
	// for someone the board leaves out (an organiser, or excluded).
	Place *int `json:"place,omitempty"`
	Of    *int `json:"of,omitempty"`
	// ToFinish lists the problems not finished once every window is over,
	// and Editorial says the editorial is open to read (v2.0.2 stage U).
	ToFinish  []toFinishJSON `json:"to_finish"`
	Editorial bool           `json:"editorial"`
}

// toFinishJSON is one problem a student has not finished: its pinned
// title, their best score (contest or practice) and whether practice is open.
type toFinishJSON struct {
	Label       string `json:"label"`
	Title       string `json:"title"`
	Score       int    `json:"score"`
	Max         int    `json:"max"`
	CanPractice bool   `json:"can_practice"`
}

// Entered lists the contests userID entered, newest start first.
func (s *Service) Entered(ctx context.Context, userID int64) ([]*Contest, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM contests WHERE id IN (
			SELECT contest_id FROM contest_starts WHERE user_id = ?
			UNION SELECT contest_id FROM submissions WHERE user_id = ? AND contest_id IS NOT NULL AND kind = 'submit' AND practice = 0)
		AND published = 1 AND archived = 0 ORDER BY starts_at DESC, id DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Contest, 0, len(ids))
	for _, id := range ids {
		c, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// MyContests answers GET /api/me/contests.
func (d *Handlers) MyContests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := auth.UserFrom(ctx).ID
	list, err := d.Contests.Entered(ctx, me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := make([]myContestJSON, 0, len(list))
	for _, c := range list {
		item, err := d.myContest(ctx, c, me)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		out = append(out, item)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contests": out, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d *Handlers) myContest(ctx context.Context, c *Contest, me int64) (myContestJSON, error) {
	item := myContestJSON{Slug: c.Slug, Title: c.Title, StartsAt: c.Schedule.StartsAt.UnixMilli(), ToFinish: []toFinishJSON{}}
	st, err := d.Contests.State(ctx, c, me)
	if err != nil {
		return item, err
	}
	item.State = c.StateFor(st, d.Contests.Clock())
	total, cells, err := d.Contests.MyScore(ctx, c, st, me)
	if err != nil {
		return item, err
	}
	item.Score = total
	for _, cell := range cells {
		item.MaxScore += cell.Max
	}
	if err := d.fillFinish(ctx, c, st, me, cells, &item); err != nil {
		return item, err
	}
	variant, err := d.Contests.StudentVariant(ctx, c, st)
	if err != nil || variant != VariantFull {
		return item, err
	}
	board, err := d.Contests.Board(ctx, c, VariantFull)
	if err != nil {
		return item, err
	}
	for _, row := range board.Rows {
		if row.UserID == me {
			place, of := row.Rank, len(board.Rows)
			item.Place, item.Of = &place, &of
		}
	}
	return item, nil
}

// fillFinish sets ToFinish and Editorial once every window is over and the
// student may still open the contest. A withdrawn invite or an archived
// contest leaves both empty, like the rest of the row's links.
func (d *Handlers) fillFinish(ctx context.Context, c *Contest, st StudentState, me int64, cells map[string]Cell, item *myContestJSON) error {
	now := d.Contests.Clock()
	if now.Before(c.Final()) {
		return nil
	}
	access, err := d.Contests.contestAccess(ctx, c, viewerOf(auth.UserFrom(ctx)))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if access != AccessParticipate {
		return nil
	}
	item.Editorial = c.EditorialVisible(st, now)
	practice, err := d.Contests.MyPracticeScore(ctx, c, me)
	if err != nil {
		return err
	}
	for _, p := range c.Problems {
		cell := cells[p.Label]
		best := cell.Score
		if pc := practice[p.Label]; pc.Score > best {
			best = pc.Score
		}
		if p.Deleted || cell.Max <= 0 || best >= cell.Max {
			continue
		}
		item.ToFinish = append(item.ToFinish, toFinishJSON{Label: p.Label, Title: p.Title, Score: best, Max: cell.Max,
			CanPractice: c.PracticeAfterEnd && p.Ready})
	}
	return nil
}
