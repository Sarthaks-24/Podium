package submissions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// publisher sends run and submission updates to their authors' event
// streams. Updates for one job are sent in order, and only when what the
// author can see has changed: for students that is when a block of samples
// or a whole subtask finishes, never per hidden test, so the arrival time
// of an event says nothing about a single hidden test.
type publisher struct {
	mu    sync.Mutex
	last  map[int64]string
	locks map[int64]*sync.Mutex
}

func (p *publisher) lock(jobID int64) func() {
	p.mu.Lock()
	if p.locks == nil {
		p.locks, p.last = map[int64]*sync.Mutex{}, map[int64]string{}
	}
	l := p.locks[jobID]
	if l == nil {
		l = &sync.Mutex{}
		p.locks[jobID] = l
	}
	p.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// changed records sig for the job and reports whether it differs from the
// last one sent. done forgets the job.
func (p *publisher) changed(jobID int64, sig string, done bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	differs := p.last[jobID] != sig
	if done {
		delete(p.last, jobID)
		delete(p.locks, jobID)
	} else {
		p.last[jobID] = sig
	}
	return differs
}

// OnJobChange publishes the new state of the run or submission a job
// belongs to. It is the jobs service's OnChange hook and must not block.
func (s *Service) OnJobChange(jobID int64, kind string) {
	if s.Hub == nil || (kind != judgeapi.KindSubmit && kind != judgeapi.KindRun) {
		return
	}
	go func() {
		unlock := s.pub.lock(jobID)
		defer unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var id, user, contest int64
		var role, state string
		if err := s.DB.QueryRowContext(ctx, `SELECT s.id, s.user_id, coalesce(s.contest_id, 0), u.role, s.state
			FROM submissions s JOIN users u ON u.id = s.user_id WHERE s.job_id = ?`, jobID).Scan(&id, &user, &contest, &role, &state); err != nil {
			return
		}
		finished := state == "done" || state == "error"
		if !s.Hub.Has(user) {
			s.pub.changed(jobID, "", finished)
			return
		}
		opts := s.ViewFor(ctx, contest, user, role == "student")
		if kind == judgeapi.KindRun {
			v, err := s.GetRunWith(ctx, id, opts)
			if err == nil && s.pub.changed(jobID, fmt.Sprintf("%s/%d", v.State, runDone(v)), finished) {
				s.Hub.Publish(user, "run", v)
			}
			return
		}
		v, err := s.GetWith(ctx, id, opts)
		if err != nil {
			return
		}
		// Exam mode: the answer to Submit already said "Submitted". Any
		// later event would arrive when judging reached some point, and its
		// timing is a channel on hidden tests, so nothing is sent until the
		// window closes, except a judge failure the student must act on.
		if opts.Hide && v.State != "error" {
			s.pub.changed(jobID, "", finished)
			return
		}
		if s.pub.changed(jobID, signature(v, opts.Coarse), finished) {
			s.Hub.Publish(user, "submission", v)
		}
	}()
}

// ViewFor is how a viewer sees their own submission in a contest (0
// outside contests): organisers see every figure; students see the coarse
// view with their contest's rules on top.
func (s *Service) ViewFor(ctx context.Context, contestID, userID int64, student bool) ViewOptions {
	if !student {
		return ViewOptions{}
	}
	if contestID > 0 && s.ContestView != nil {
		return s.ContestView(ctx, contestID, userID)
	}
	return StudentView
}

// runDone counts a run's inputs that have a verdict.
func runDone(v *RunView) int {
	n := 0
	for _, r := range v.Results {
		if r.Verdict != "" {
			n++
		}
	}
	return n
}

// signature is what an update must change to be worth sending.
func signature(v *SubmissionView, coarse bool) string {
	done := 0
	samplesDone := true
	for _, t := range v.Samples {
		if t.Verdict == "" {
			samplesDone = false
		} else if !coarse {
			done++
		}
	}
	for _, st := range v.Subtasks {
		if coarse {
			if st.Verdict != "" {
				done++
			}
			continue
		}
		for _, t := range st.Tests {
			if t.Verdict != "" {
				done++
			}
		}
	}
	return fmt.Sprintf("%s/%s/%v/%d", v.State, v.Verdict, samplesDone, done)
}
