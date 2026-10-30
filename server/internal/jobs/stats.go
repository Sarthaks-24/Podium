package jobs

import (
	"context"
	"sort"
	"time"
)

// finishedRing is how many closed jobs the service remembers for the
// throughput and latency figures in Stats. 4,096 covers more than ten
// minutes of a 700-student contest's end-of-contest spike.
const finishedRing = 4096

// finishedJob is one closed job as Stats sees it.
type finishedJob struct {
	kind     string
	failed   bool
	created  int64 // Unix ms
	started  int64 // Unix ms, first claim; 0 if never claimed
	finished int64 // Unix ms
}

// KindStats is the queue for one job kind.
type KindStats struct {
	Queued int `json:"queued"`
	Leased int `json:"leased"`
	// Finished counts jobs of this kind closed in the last minute and the
	// last five minutes (done or failed), since this server started.
	FinishedLastMinute int `json:"finished_last_minute"`
	FinishedLast5Min   int `json:"finished_last_5_min"`
}

// LeasedJob is a job a worker holds now.
type LeasedJob struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Worker   string `json:"worker"`
	Attempts int    `json:"attempts"`
	// RunningS is how long ago the first claim was, in seconds.
	RunningS float64 `json:"running_s"`
}

// Stats is a snapshot of the queue for the metrics route.
type Stats struct {
	Kinds  map[string]KindStats `json:"kinds"`
	Queued int                  `json:"queued"`
	Leased int                  `json:"leased"`
	// QueuedPaused counts queued jobs held back because their contest is
	// paused; they are included in Queued.
	QueuedPaused int `json:"queued_paused"`
	// OldestQueuedS is the age of the oldest queued job in seconds, 0
	// when nothing waits.
	OldestQueuedS float64     `json:"oldest_queued_s"`
	LeasedJobs    []LeasedJob `json:"leased_jobs"`
	// SubmitWait and SubmitTotal are percentiles, in milliseconds, of
	// submissions closed in the last ten minutes: from Submit to the
	// first claim, and from Submit to the verdict. Zero when none closed.
	SubmitWaitP50MS  int64 `json:"submit_wait_p50_ms"`
	SubmitWaitP95MS  int64 `json:"submit_wait_p95_ms"`
	SubmitTotalP50MS int64 `json:"submit_total_p50_ms"`
	SubmitTotalP95MS int64 `json:"submit_total_p95_ms"`
	// SubmitsJudged is how many submissions those percentiles cover.
	SubmitsJudged int `json:"submits_judged_last_10_min"`
	// FailedLast5Min counts jobs of any kind that failed (judge errors,
	// expired leases, deadlines) in the last five minutes.
	FailedLast5Min int `json:"failed_last_5_min"`
}

// recordFinished remembers a closed job for Stats. Called after the
// transaction that closed it has committed.
func (s *Service) recordFinished(j Job, now time.Time) {
	f := finishedJob{kind: j.Kind, failed: j.State == Failed, created: j.CreatedAt.UnixMilli(), finished: now.UnixMilli()}
	if !j.StartedAt.IsZero() {
		f.started = j.StartedAt.UnixMilli()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.done) < finishedRing {
		s.done = append(s.done, f)
		return
	}
	s.done[s.doneNext] = f
	s.doneNext = (s.doneNext + 1) % finishedRing
}

// Waiting counts the queued jobs and returns when the oldest was queued
// (the zero time when none waits), for the health check.
func (s *Service) Waiting(ctx context.Context) (int64, time.Time, error) {
	var queued, oldest int64
	err := s.DB.QueryRowContext(ctx, "SELECT count(*), coalesce(min(created_at), 0) FROM jobs WHERE state = 'queued'").
		Scan(&queued, &oldest)
	if err != nil || queued == 0 {
		return queued, time.Time{}, err
	}
	return queued, time.UnixMilli(oldest), nil
}

// Stats reads the queue: two indexed queries on the jobs table and the
// in-memory record of recently closed jobs.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	now := s.now()
	ms := now.UnixMilli()
	st := Stats{Kinds: map[string]KindStats{}, LeasedJobs: []LeasedJob{}}
	for k := range s.Kinds {
		st.Kinds[k] = KindStats{}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT kind, state, count(*), min(created_at),
		sum(CASE WHEN contest_id IS NOT NULL AND EXISTS (SELECT 1 FROM contests c WHERE c.id = jobs.contest_id AND c.paused = 1) THEN 1 ELSE 0 END)
		FROM jobs WHERE state IN ('queued', 'leased') GROUP BY kind, state`)
	if err != nil {
		return Stats{}, err
	}
	var oldest int64
	for rows.Next() {
		var kind, state string
		var n, paused int
		var first int64
		if err := rows.Scan(&kind, &state, &n, &first, &paused); err != nil {
			rows.Close()
			return Stats{}, err
		}
		ks := st.Kinds[kind]
		if state == Queued {
			ks.Queued += n
			st.Queued += n
			st.QueuedPaused += paused
			if oldest == 0 || first < oldest {
				oldest = first
			}
		} else {
			ks.Leased += n
			st.Leased += n
		}
		st.Kinds[kind] = ks
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Stats{}, err
	}
	if oldest > 0 {
		st.OldestQueuedS = float64(ms-oldest) / 1000
	}
	leased, err := s.DB.QueryContext(ctx, `SELECT id, kind, worker, attempts, coalesce(started_at, created_at) FROM jobs
		WHERE state = 'leased' ORDER BY id`)
	if err != nil {
		return Stats{}, err
	}
	for leased.Next() {
		var j LeasedJob
		var started int64
		if err := leased.Scan(&j.ID, &j.Kind, &j.Worker, &j.Attempts, &started); err != nil {
			leased.Close()
			return Stats{}, err
		}
		j.RunningS = float64(ms-started) / 1000
		st.LeasedJobs = append(st.LeasedJobs, j)
	}
	leased.Close()
	if err := leased.Err(); err != nil {
		return Stats{}, err
	}

	s.mu.Lock()
	done := append([]finishedJob(nil), s.done...)
	s.mu.Unlock()
	var wait, total []int64
	for _, f := range done {
		age := ms - f.finished
		if age <= 5*60_000 {
			ks := st.Kinds[f.kind]
			ks.FinishedLast5Min++
			if age <= 60_000 {
				ks.FinishedLastMinute++
			}
			st.Kinds[f.kind] = ks
			if f.failed {
				st.FailedLast5Min++
			}
		}
		if f.kind == "submit" && !f.failed && age <= 10*60_000 {
			total = append(total, f.finished-f.created)
			if f.started > 0 {
				wait = append(wait, f.started-f.created)
			}
		}
	}
	st.SubmitsJudged = len(total)
	st.SubmitWaitP50MS, st.SubmitWaitP95MS = percentile(wait, 50), percentile(wait, 95)
	st.SubmitTotalP50MS, st.SubmitTotalP95MS = percentile(total, 50), percentile(total, 95)
	return st, nil
}

// percentile is the nearest-rank percentile of v, 0 for an empty slice.
// It sorts v.
func percentile(v []int64, p int) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	rank := (p*len(v) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return v[rank-1]
}
