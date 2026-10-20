package contests

import (
	"context"
	"time"
)

// RolloverContest is a contest the semester rollover archives.
type RolloverContest struct {
	ID     int64  `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	EndsAt int64  `json:"ends_at"`
}

// EndedBefore lists the unarchived contests whose last moment (the end
// plus every extension) is before t and before now: the term's contests.
func (s *Service) EndedBefore(ctx context.Context, t time.Time) ([]RolloverContest, error) {
	cut := min(t.UnixMilli(), s.now().UnixMilli())
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.slug, c.title, c.ends_at FROM contests c WHERE c.archived = 0
		AND c.ends_at + 60000 * (c.extended_minutes + coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) < ?
		ORDER BY c.ends_at, c.id`, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RolloverContest{}
	for rows.Next() {
		var c RolloverContest
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.EndsAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
