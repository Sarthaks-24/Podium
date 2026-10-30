package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/retention"
	"github.com/Sarthaks-24/podium/internal/schedule"
	"github.com/Sarthaks-24/podium/internal/submissions"
	"github.com/Sarthaks-24/podium/internal/webui"
)

// services are the server's parts, each built from the ones it needs.
type services struct {
	db          *sql.DB
	log         *slog.Logger
	blobs       problems.Blobs
	auth        *auth.Service
	jobs        *jobs.Service
	problems    *problems.Service
	hub         *events.Hub
	submissions *submissions.Service
	contests    *contests.Service
	integrity   *integrity.Service
	aiKeys      *ai.Keys
	retention   *retention.Job
}

// newServices builds the services and applies the start-up rules that
// need them: the configured owner and domains, the run-mode record, and
// marking integrity checks a shutdown interrupted.
func newServices(ctx context.Context, cfg config.Server, database *sql.DB, log *slog.Logger) (*services, error) {
	s := &services{db: database, log: log, blobs: problems.Blobs{Dir: cfg.TestsDir}, hub: &events.Hub{},
		// AI wizard keys live in this process's memory only: no variable,
		// no file, gone on restart.
		aiKeys:    &ai.Keys{},
		retention: &retention.Job{DB: database, Log: log}}
	s.auth = &auth.Service{DB: database, OwnerEmail: cfg.OwnerEmail}
	if err := s.auth.Bootstrap(ctx); err != nil {
		return nil, err
	}
	// Nothing made in testing mode may survive into another mode.
	if ch, err := s.auth.RecordMode(ctx, string(cfg.Mode)); err != nil {
		return nil, fmt.Errorf("record run mode: %w", err)
	} else if ch.Changed {
		log.Warn("run mode differs from the database's last start: every session ended",
			"from", ch.Previous, "to", cfg.Mode, "sessions_ended", ch.SessionsEnded, "organisers_demoted", ch.Demoted, "testing_accounts_disabled", ch.Disabled)
	}

	// Outputs a generate job's worker uploaded but nothing used are deleted
	// when the job closes or its lease is swept.
	discard := func(shas []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.blobs.DiscardUnrecorded(ctx, database, shas)
	}
	s.jobs = jobs.New(database, discard, func(j *jobs.Service) map[string]jobs.Kind {
		return map[string]jobs.Kind{
			judgeapi.KindValidate:       problems.ValidateKind{},
			judgeapi.KindGenerate:       problems.GenerateKind{Store: s.blobs, Jobs: j},
			judgeapi.KindGenerateInputs: problems.GenerateInputsKind{Store: s.blobs, Jobs: j},
			judgeapi.KindSubmit:         submissions.SubmitKind{},
			judgeapi.KindRun:            submissions.RunKind{},
		}
	})
	s.problems = &problems.Service{DB: database, Blobs: s.blobs, Jobs: s.jobs}
	s.contests = &contests.Service{DB: database, Problems: s.problems, Hub: s.hub}
	s.submissions = submissions.New(database, s.jobs, s.problems, s.hub, cfg.SessionSecret, s.contests.ViewFor)
	inputEvents := &problems.InputGenerationEvents{Problems: s.problems, Hub: s.hub}
	s.jobs.Notify(s.submissions.OnJobChange, s.contests.OnJobChange, inputEvents.OnJobChange)

	// Integrity checks run in one background goroutine on this server,
	// not on the judge: nothing is executed, so the sandbox adds nothing.
	s.integrity = &integrity.Service{DB: database, Problems: s.problems, Contests: s.contests, Log: log}
	if n, err := s.integrity.Recover(ctx); err != nil {
		return nil, fmt.Errorf("integrity: mark interrupted checks: %w", err)
	} else if n > 0 {
		log.Warn("integrity checks interrupted by the last shutdown marked as failed", "count", n)
	}
	return s, nil
}

// handler is the public API and web app.
func (s *services) handler(cfg config.Server, provider httpapi.IdentityProvider) http.Handler {
	return httpapi.New(httpapi.Deps{
		DB: s.db, Log: s.log, WebApp: webui.Handler(),
		Auth: s.auth, IdP: provider, BaseURL: cfg.BaseURL, SessionSecret: cfg.SessionSecret,
		Problems: s.problems, Submissions: s.submissions, Hub: s.hub, Contests: s.contests, Integrity: s.integrity,
		AI:   &ai.Wizard{Keys: s.aiKeys, Slots: &ai.Slots{}},
		Mode: string(cfg.Mode), Version: version,
	})
}

// tasks is every background job of the server. Shutdown waits for a run in
// progress before the database closes.
func (s *services) tasks() []schedule.Task {
	return []schedule.Task{
		// Jobs whose judge went quiet go back to the queue.
		{Name: "job sweep", Every: 10 * time.Second, Delay: 10 * time.Second, Run: s.sweepJobs},
		{Name: "session sweep", Every: time.Hour, Run: s.sweepSessions},
		// A key whose session never comes back does not stay in memory
		// past its two hours by more than this.
		{Name: "AI key sweep", Every: 10 * time.Minute, Delay: 10 * time.Minute, Run: func(context.Context) { s.aiKeys.Sweep() }},
		// A minute after start-up, so a restart during a contest does not
		// add its writes to the start.
		{Name: "retention", Every: 12 * time.Hour, Delay: time.Minute, Run: s.retain},
	}
}

func (s *services) sweepJobs(ctx context.Context) {
	requeued, failed, err := s.jobs.Sweep(ctx)
	switch {
	case err != nil && ctx.Err() == nil:
		s.log.Warn("job sweep failed", "err", err)
	case requeued > 0 || failed > 0:
		s.log.Warn("judge leases expired", "requeued", requeued, "failed", failed)
	}
}

func (s *services) sweepSessions(ctx context.Context) {
	if n, err := s.auth.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
		s.log.Warn("session sweep failed", "err", err)
	} else if n > 0 {
		s.log.Debug("expired sessions removed", "count", n)
	}
}

// retain is the one retention pass: run payloads past
// retention.RunPayloads are emptied; a contest's integrity events and
// similarity checks are deleted retention.IntegrityEvents after it ends;
// then test files and sources nothing refers to any more are removed.
// Every step is idempotent, so a failed one is retried by the next pass.
func (s *services) retain(ctx context.Context) {
	failed := func(err error) bool { return err != nil && ctx.Err() == nil }
	start := time.Now()
	if res, err := s.retention.Run(ctx); failed(err) {
		s.log.Warn("run payload retention failed", "err", err)
	} else if res != (retention.Result{}) {
		s.log.Info("data past retention removed", "runs_emptied", res.RunsEmptied, "ms", time.Since(start).Milliseconds())
	}
	if events, subs, err := s.contests.Retain(ctx); failed(err) {
		s.log.Warn("integrity retention failed", "err", err)
	} else if events > 0 || subs > 0 {
		s.log.Info("integrity data past retention removed", "events", events, "submissions", subs)
	}
	if signals, runs, err := s.integrity.RetainSignals(ctx); failed(err) {
		s.log.Warn("integrity check retention failed", "err", err)
	} else if signals > 0 || runs > 0 {
		s.log.Info("integrity checks past retention removed", "signals", signals, "runs", runs)
	}
	start = time.Now()
	if res, err := s.blobs.GC(ctx, s.db, start); failed(err) {
		s.log.Warn("unused test files: collection failed", "err", err)
	} else if res.Rows+res.Files > 0 {
		s.log.Info("unused test files removed", "blobs", res.Rows, "files", res.Files, "mb", res.Bytes>>20,
			"ms", time.Since(start).Milliseconds())
	}
}
