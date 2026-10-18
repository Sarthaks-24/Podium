package submissions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// compileTimeoutWait is how long a user's Run and Submit are refused after
// their program's compile ran out of time or memory (security council S4).
// C++ and Java compiles share one lock on the judge, so a student looping
// such a program would stall every other student's compile.
const compileTimeoutWait = 60 * time.Second

// compileTimeouts is shared by the job kinds, which see the report, and
// the Service, which refuses the next request; neither holds the other, so
// it is package-wide. It is in memory: a restart forgets it, which costs
// nothing. Its lock is held only for map updates, never across a query.
var compileTimeouts timeoutLog

type timeoutLog struct {
	mu sync.Mutex
	// jobs holds jobs whose program compile timed out, until they finish.
	jobs map[int64]time.Time
	// until holds when each user may run and submit again, per contest.
	until map[scopeKey]time.Time
}

type scopeKey struct{ user, contest int64 }

// noteCompileTimeout remembers a report's program compile that ran out of
// time or memory, so the job's Finish can slow its owner down.
func noteCompileTimeout(r judgeapi.Report, now time.Time) {
	for _, c := range r.Compile {
		if c.Role == judgeapi.RoleProgram && c.TimedOut {
			compileTimeouts.mu.Lock()
			if compileTimeouts.jobs == nil {
				compileTimeouts.jobs = map[int64]time.Time{}
			}
			compileTimeouts.jobs[r.JobID] = now
			if len(compileTimeouts.jobs) > 10000 {
				// Jobs cancelled by a rejudge never finish; drop old ones.
				for id, t := range compileTimeouts.jobs {
					if now.Sub(t) > time.Hour {
						delete(compileTimeouts.jobs, id)
					}
				}
			}
			compileTimeouts.mu.Unlock()
		}
	}
}

// finishCompileTimeout runs in a run or submit job's Finish. When the
// job's program compile timed out, it refuses the owner's Run and Submit
// in the same contest for compileTimeoutWait. A rejudge is the organisers'
// doing, so it does not count against the student.
func finishCompileTimeout(ctx context.Context, tx *sql.Tx, job *jobs.Job, submissionID int64, now time.Time) error {
	compileTimeouts.mu.Lock()
	_, timedOut := compileTimeouts.jobs[job.ID]
	delete(compileTimeouts.jobs, job.ID)
	compileTimeouts.mu.Unlock()
	if !timedOut || job.Priority == RejudgePriority {
		return nil
	}
	var k scopeKey
	err := tx.QueryRowContext(ctx, "SELECT user_id, coalesce(contest_id, 0) FROM submissions WHERE id = ?", submissionID).
		Scan(&k.user, &k.contest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	compileTimeouts.mu.Lock()
	defer compileTimeouts.mu.Unlock()
	if compileTimeouts.until == nil {
		compileTimeouts.until = map[scopeKey]time.Time{}
	}
	compileTimeouts.until[k] = now.Add(compileTimeoutWait)
	if len(compileTimeouts.until) > 20000 {
		for key, t := range compileTimeouts.until {
			if t.Before(now) {
				delete(compileTimeouts.until, key)
			}
		}
	}
	return nil
}

// checkCompileTimeout refuses a Run or Submit while the user waits after
// a compile that timed out. Compiles see no tests, so in exam mode the
// refusal reveals nothing hidden.
func checkCompileTimeout(user, contest int64, now time.Time) error {
	compileTimeouts.mu.Lock()
	until, ok := compileTimeouts.until[scopeKey{user, contest}]
	compileTimeouts.mu.Unlock()
	wait := until.Sub(now)
	if !ok || wait <= 0 {
		return nil
	}
	secs := max(1, int(math.Ceil(wait.Seconds())))
	return &RateError{fmt.Sprintf("Your last program took too long to compile. Try again in %d seconds; "+
		"a program that compiles for 10 seconds is usually a template or include problem.", secs), time.Duration(secs) * time.Second}
}
