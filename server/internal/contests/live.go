package contests

import (
	"context"
	"time"
)

// LiveUntil reports whether the student is in a live contest at now: a
// contest whose rules they accepted (or whose clock they started) and
// whose window for them is still open. until is the latest such window's
// end, when the notices held for it may be shown (B4.4). Only contests
// running now are looked at, and each by its primary key, so the check is
// cheap enough for every page load.
func (s *Service) LiveUntil(ctx context.Context, userID int64) (until time.Time, live bool, err error) {
	now := s.now()
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id FROM contests c
		WHERE c.starts_at <= ? AND c.ends_at + 60000 * (c.extended_minutes +
			coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) > ?
		AND EXISTS (SELECT 1 FROM contest_starts cs WHERE cs.contest_id = c.id AND cs.user_id = ?)`,
		now.UnixMilli(), now.UnixMilli(), userID)
	if err != nil {
		return time.Time{}, false, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return time.Time{}, false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return time.Time{}, false, err
	}
	for _, id := range ids {
		c, err := s.Get(ctx, id)
		if err != nil {
			return time.Time{}, false, err
		}
		st, err := s.State(ctx, c, userID)
		if err != nil {
			return time.Time{}, false, err
		}
		if st.HasClock && !st.Over && st.Window.Ends.After(until) {
			until, live = st.Window.Ends, true
		}
	}
	return until, live, nil
}

// InLiveContest is LiveUntil without the time, for the inbox.
func (s *Service) InLiveContest(ctx context.Context, userID int64) (bool, error) {
	_, live, err := s.LiveUntil(ctx, userID)
	return live, err
}
