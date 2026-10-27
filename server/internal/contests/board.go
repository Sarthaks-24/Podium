package contests

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Variants of the board.
const (
	VariantFull   = "full"
	VariantFrozen = "frozen"
)

// Standings is a computed board.
type Standings struct {
	ContestID int64     `json:"contest_id"`
	Variant   string    `json:"variant"`
	Version   int64     `json:"version"`
	Labels    []string  `json:"labels"`
	Rows      []Row     `json:"rows"`
	Computed  time.Time `json:"-"`
	// JudgingRemaining counts submissions made before the end that are
	// still waiting for the judge ("final in about 6 min").
	JudgingRemaining int `json:"judging_remaining"`
}

// DefaultBoardEvery is the shortest time between two recomputes of one
// contest's board.
const DefaultBoardEvery = 2 * time.Second

// boardKey names one cached board.
type boardKey struct {
	id      int64
	variant string
}

// boardEntry is one cached board and the computation that refreshes it.
type boardEntry struct {
	board      *Standings
	running    bool
	runVersion int64
	done       chan struct{}
	lastStart  time.Time
}

// boardCache keeps the last computed board per contest and variant, with a
// version per contest that every change bumps. A board whose version is
// behind is stale but still served while it is being recomputed.
type boardCache struct {
	mu       sync.Mutex
	entries  map[boardKey]*boardEntry
	version  map[int64]int64
	refresh  map[int64]bool  // a background refresh is scheduled
	notified map[int64]int64 // last version sent in a standings event
	computes atomic.Int64    // how many boards were computed (tests)
}

func (b *boardCache) init() {
	if b.entries == nil {
		b.entries, b.version = map[boardKey]*boardEntry{}, map[int64]int64{}
		b.refresh, b.notified = map[int64]bool{}, map[int64]int64{}
	}
}

func (b *boardCache) bump(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.init()
	b.version[id]++
}

// Version is the contest board's current version, for clients deciding
// whether to refetch.
func (s *Service) Version(id int64) int64 {
	s.board.mu.Lock()
	defer s.board.mu.Unlock()
	s.board.init()
	return s.board.version[id]
}

// Invalidate marks a contest's boards stale (a submission finished, a
// rejudge ran) and schedules a refresh, after which watchers are told.
func (s *Service) Invalidate(id int64) { s.changed(id) }

func (s *Service) boardEvery() time.Duration {
	if s.BoardEvery > 0 {
		return s.BoardEvery
	}
	return DefaultBoardEvery
}

// Board returns the contest's standings in the given variant. The work is
// bounded however many people ask at once (m4-security finding 1):
//
//   - A current board is returned from the cache.
//   - Only one computation per contest and variant runs at a time. Readers
//     arriving meanwhile get the last board; when there is none yet, or the
//     running computation is already for the current version, they wait for
//     it rather than start another.
//   - A stale board is recomputed at most once every BoardEvery; inside
//     that time the last board is served and a refresh is scheduled.
func (s *Service) Board(ctx context.Context, c *Contest, variant string) (*Standings, error) {
	key := boardKey{c.ID, variant}
	for {
		s.board.mu.Lock()
		s.board.init()
		e := s.board.entries[key]
		if e == nil {
			e = &boardEntry{}
			s.board.entries[key] = e
		}
		v := s.board.version[c.ID]
		if e.board != nil && e.board.Version == v {
			b := e.board
			s.board.mu.Unlock()
			return b, nil
		}
		if e.running {
			if e.board != nil && e.runVersion != v {
				b := e.board
				s.board.mu.Unlock()
				return b, nil
			}
			done := e.done
			s.board.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		now := time.Now()
		if e.board != nil && now.Sub(e.lastStart) < s.boardEvery() {
			b, wait := e.board, e.lastStart.Add(s.boardEvery()).Sub(now)
			s.board.mu.Unlock()
			s.scheduleRefresh(c.ID, wait)
			return b, nil
		}
		e.running, e.runVersion, e.done, e.lastStart = true, v, make(chan struct{}), now
		s.board.mu.Unlock()

		st, err := s.computeBoard(ctx, c, variant, v)

		s.board.mu.Lock()
		e.running = false
		close(e.done)
		if err == nil && (e.board == nil || st.Version >= e.board.Version) {
			e.board = st
		}
		behind := s.board.version[c.ID] != v
		s.board.mu.Unlock()
		if behind {
			s.scheduleRefresh(c.ID, s.boardEvery())
		}
		return st, err
	}
}

// computeBoard reads the participants and submissions and scores them.
func (s *Service) computeBoard(ctx context.Context, c *Contest, variant string, version int64) (*Standings, error) {
	s.board.computes.Add(1)
	b := s.scoringBoard(c)
	labels := make([]string, 0, len(c.Problems))
	for _, p := range c.Problems {
		labels = append(labels, p.Label)
	}
	people, err := s.participants(ctx, c)
	if err != nil {
		return nil, err
	}
	entries, remaining, err := s.entries(ctx, c, 0)
	if err != nil {
		return nil, err
	}
	return &Standings{ContestID: c.ID, Variant: variant, Version: version, Labels: labels,
		Rows: Compute(b, people, entries, variant == VariantFrozen), Computed: s.now(), JudgingRemaining: remaining}, nil
}

// scheduleRefresh recomputes the contest's cached boards after delay (one
// refresh per contest pending at a time, so a burst of changes costs one
// recompute) and then sends the standings event, so browsers refetch a
// board that is already in the cache.
func (s *Service) scheduleRefresh(id int64, delay time.Duration) {
	s.board.mu.Lock()
	s.board.init()
	if s.board.refresh[id] {
		s.board.mu.Unlock()
		return
	}
	s.board.refresh[id] = true
	s.board.mu.Unlock()
	time.AfterFunc(max(delay, 0), func() {
		s.board.mu.Lock()
		delete(s.board.refresh, id)
		var variants []string
		for k, e := range s.board.entries {
			if k.id == id && e.board != nil {
				variants = append(variants, k.variant)
			}
		}
		s.board.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := s.Get(ctx, id)
		if err != nil {
			return
		}
		for _, v := range variants {
			if _, err := s.Board(ctx, c, v); err != nil {
				return
			}
		}
		s.notifyStandings(ctx, c)
	})
}

// scoringBoard is the scoring view of the contest.
func (s *Service) scoringBoard(c *Contest) Board {
	b := Board{Scoring: c.Scoring, Window: c.Schedule.Mode == WindowStart, StartsAt: c.Schedule.StartsAt, Freeze: c.Schedule.Freeze}
	if c.Schedule.Mode == FixedStart && c.Schedule.Freeze > 0 {
		b.FreezeAt = c.Schedule.Close().Add(-c.Schedule.Freeze)
	}
	for _, p := range c.Problems {
		b.Problems = append(b.Problems, BoardProblem{Label: p.Label, ProblemID: p.ProblemID, Points: p.Points})
	}
	return b
}

// participants are students who opened the contest or submitted to it,
// with their own windows. The name shown is the roster's, then the
// account's, never the email: the board is public to the contest.
func (s *Service) participants(ctx context.Context, c *Contest) ([]Participant, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,
		coalesce(nullif(trim(r.display_name), ''), nullif(trim(u.name), ''), 'Student ' || u.id),
		coalesce(cs.started_at, 0), coalesce(ce.minutes, 0)
		FROM users u
		LEFT JOIN contest_starts cs ON cs.contest_id = ? AND cs.user_id = u.id
		LEFT JOIN contest_extensions ce ON ce.contest_id = ? AND ce.user_id = u.id
		LEFT JOIN contest_roster r ON r.contest_id = ? AND r.email = u.email
		WHERE u.id IN (SELECT user_id FROM contest_starts WHERE contest_id = ?
			UNION SELECT user_id FROM submissions WHERE contest_id = ? AND kind = 'submit' AND practice = 0)
		AND u.id NOT IN (SELECT user_id FROM integrity_cases WHERE contest_id = ? AND decision = 'excluded')
		AND u.role = 'student'`, c.ID, c.ID, c.ID, c.ID, c.ID, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Participant
	for rows.Next() {
		var p Participant
		var started int64
		var ext int
		if err := rows.Scan(&p.UserID, &p.Name, &started, &ext); err != nil {
			return nil, err
		}
		var startForWindow time.Time
		if c.Schedule.Mode == WindowStart && started > 0 {
			startForWindow = time.UnixMilli(started)
		}
		w, ok := c.Schedule.StudentWindow(startForWindow, time.Duration(ext)*time.Minute)
		if !ok {
			// Window mode without a start: they cannot have submitted.
			continue
		}
		p.Started, p.Ends = w.Opens, w.Ends
		out = append(out, p)
	}
	return out, rows.Err()
}

// entriesQuery reads only columns of the submissions_by_board index, so
// it never touches the rows with their inline code and full results.
const entriesQuery = `SELECT user_id, problem_id, created_at, state, score, earned
	FROM submissions INDEXED BY submissions_by_board
	WHERE contest_id = ? AND kind = 'submit' AND practice = 0 AND state <> 'error' AND (? = 0 OR user_id = ?)`

// entries are the contest's non-practice submissions (one user's when
// userID is set) with their earned points per subtask, and how many are
// still waiting for the judge.
func (s *Service) entries(ctx context.Context, c *Contest, userID int64) ([]Entry, int, error) {
	rows, err := s.DB.QueryContext(ctx, entriesQuery, c.ID, userID, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Entry
	remaining := 0
	for rows.Next() {
		var e Entry
		var created int64
		var state, earned string
		if err := rows.Scan(&e.UserID, &e.ProblemID, &created, &state, &e.Score, &earned); err != nil {
			return nil, 0, err
		}
		e.At = time.UnixMilli(created)
		e.Done = state == "done"
		if !e.Done {
			remaining++
		}
		// A score that cannot be read is an error, never a silent 0 (S1-08).
		if e.Done && earned != "" {
			if err := json.Unmarshal([]byte(earned), &e.Earned); err != nil {
				return nil, 0, fmt.Errorf("contest %d: earned of user %d on problem %d: %w", c.ID, e.UserID, e.ProblemID, err)
			}
		}
		out = append(out, e)
	}
	return out, remaining, rows.Err()
}

// BoardNow computes the full board from the database, bypassing the cache:
// a review decision reads the student's rank and score just before it.
func (s *Service) BoardNow(ctx context.Context, c *Contest) (*Standings, error) {
	return s.computeBoard(ctx, c, VariantFull, 0)
}
