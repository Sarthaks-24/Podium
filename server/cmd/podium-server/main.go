// Command podium-server is the contest platform's web server: the JSON API,
// server-sent events, the embedded web app, and the judge queue. All
// configuration comes from environment variables (docs/OPERATIONS.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/buildinfo"
	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/retention"
	"github.com/Sarthaks-24/podium/internal/submissions"
	"github.com/Sarthaks-24/podium/internal/webui"
)

// version is the release, set at build time in package buildinfo.
var version = buildinfo.Version

func main() {
	envFile := flag.String("env-file", "", "load variables from this file before reading the environment (local development)")
	showVersion := flag.Bool("version", false, "print the version, commit, build date, schema and judge protocol, then exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: podium-server [-env-file FILE]           run the server")
		fmt.Fprintln(os.Stderr, "       podium-server -version                    print what this build is")
		fmt.Fprintln(os.Stderr, "       podium-server check [-db F] [-tests D] [-hash]  check a database and its test files")
		fmt.Fprintln(os.Stderr, "       podium-server deploy-check [-db F] [-force REASON -release TAG]  may a release be installed now?")
		fmt.Fprintln(os.Stderr, "       podium-server saw -contest SLUG -email E -label A [-at TIME]  what a student was shown of a problem")
		fmt.Fprintln(os.Stderr, "       podium-server runs-report [-db F]          runs with a custom input, per contest")
		fmt.Fprintln(os.Stderr, "       podium-server migrate -db COPY             rehearse an upgrade on a copy of the database")
	}
	flag.Parse()
	if *showVersion {
		buildinfo.Write(os.Stdout)
		return
	}
	if flag.Arg(0) == "check" {
		os.Exit(checkCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "deploy-check" {
		os.Exit(deployCheckCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "saw" {
		os.Exit(sawCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "migrate" {
		os.Exit(migrateCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "runs-report" {
		os.Exit(runsReportCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*envFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if config.IsProblems(err) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(envFile string) error {
	if envFile != "" {
		if err := config.LoadEnvFile(envFile); err != nil {
			return err
		}
	}
	cfg, err := config.LoadServer(os.Getenv)
	if err != nil {
		return err
	}
	if err := config.CheckPublicListen(cfg); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting podium-server", append([]any{"version", version}, config.Summary(os.Getenv, config.ServerVars)...)...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.TestsDir, 0o750); err != nil {
		return fmt.Errorf("create tests directory: %w", err)
	}
	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	applied, err := db.Migrate(ctx, database)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("applied migrations", "versions", applied)
	}

	authSvc := &auth.Service{DB: database, OwnerEmail: cfg.OwnerEmail}
	if err := authSvc.Bootstrap(ctx, cfg.AllowedDomains); err != nil {
		return err
	}
	// Nothing made in testing mode may survive into another mode (S2).
	if ch, err := authSvc.RecordMode(ctx, string(cfg.Mode)); err != nil {
		return fmt.Errorf("record run mode: %w", err)
	} else if ch.Changed {
		log.Warn("run mode differs from the database's last start: every session ended",
			"from", ch.Previous, "to", cfg.Mode, "sessions_ended", ch.SessionsEnded, "organisers_demoted", ch.Demoted)
	}
	go sweepSessions(ctx, authSvc, log)
	var provider httpapi.IdentityProvider
	if cfg.Mode == config.ModeTesting {
		log.Warn("testing mode: Google sign-in is off and anyone at this machine can sign in as anyone; listening on loopback only")
	} else {
		provider = auth.NewProvider(auth.GoogleEndpoints, cfg.GoogleClientID, cfg.GoogleClientSecret, httpapi.RedirectURL(cfg.BaseURL))
	}
	if cfg.JudgeToken == config.TestingJudgeToken {
		log.Warn("judge API uses the public testing token; set PODIUM_JUDGE_TOKEN before going to production")
	}

	// Request contexts derive from base, which is cancelled when shutdown
	// begins, so long-lived event streams end instead of holding Shutdown
	// open until its timeout.
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	blobs := problems.Blobs{Dir: cfg.TestsDir}
	// The kinds are set after the service exists: output and input
	// generation queue the next step of their chain through it.
	jobSvc := &jobs.Service{DB: database}
	jobSvc.Kinds = map[string]jobs.Kind{
		judgeapi.KindValidate:       problems.ValidateKind{},
		judgeapi.KindGenerate:       problems.GenerateKind{Store: blobs, Jobs: jobSvc},
		judgeapi.KindGenerateInputs: problems.GenerateInputsKind{Store: blobs, Jobs: jobSvc},
		judgeapi.KindSubmit:         submissions.SubmitKind{},
		judgeapi.KindRun:            submissions.RunKind{},
	}
	// Outputs a generate job's worker uploaded but nothing used are deleted
	// when the job closes or its lease is swept.
	jobSvc.Discard = func(shas []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		blobs.DiscardUnrecorded(ctx, database, shas)
	}
	problemSvc := &problems.Service{DB: database, Blobs: blobs, Jobs: jobSvc}
	hub := &events.Hub{}
	submissionSvc := &submissions.Service{DB: database, Jobs: jobSvc, Problems: problemSvc, Hub: hub, HashKey: cfg.SessionSecret}
	contestSvc := &contests.Service{DB: database, Problems: problemSvc, Hub: hub}
	submissionSvc.ContestView = contestSvc.ViewFor
	inputEvents := &httpapi.InputGenerationEvents{Problems: problemSvc, Hub: hub}
	jobSvc.OnChange = func(jobID int64, kind string) {
		submissionSvc.OnJobChange(jobID, kind)
		contestSvc.OnJobChange(jobID, kind)
		inputEvents.OnJobChange(jobID, kind)
	}
	// AI wizard keys live in this process's memory only (D50): no
	// variable, no file, gone on restart.
	aiKeys := &ai.Keys{}
	go sweepAIKeys(ctx, aiKeys)
	if cfg.Mode == config.ModeTesting {
		log.Warn("testing mode: the AI wizard offers the test provider with canned problems")
	}
	// Integrity checks run in one background goroutine on this server,
	// not on the judge (D49).
	integritySvc := &integrity.Service{DB: database, Problems: problemSvc, Log: log}
	if n, err := integritySvc.Recover(ctx); err != nil {
		return fmt.Errorf("integrity: mark interrupted checks: %w", err)
	} else if n > 0 {
		log.Warn("integrity checks interrupted by the last shutdown marked as failed", "count", n)
	}
	go sweepJobs(ctx, jobSvc, log)
	go retainIntegrity(ctx, contestSvc, integritySvc, log, func(ctx context.Context) {
		start := time.Now()
		res, err := blobs.GC(ctx, database, start)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("unused test files: collection failed", "err", err)
		case res.Rows+res.Files > 0:
			log.Info("unused test files removed", "blobs", res.Rows, "files", res.Files, "mb", res.Bytes>>20,
				"ms", time.Since(start).Milliseconds())
		}
	})
	// The retention periods of internal/retention that no other job
	// applies: run payloads, contact messages, unused revisions,
	// submissions and audit rows (B3.1). Once a day.
	go retainDaily(ctx, &retention.Job{DB: database, Log: log, ExportStandings: contestSvc.FinalStandings,
		Expired: contestSvc.Invalidate}, log)
	handler := httpapi.New(httpapi.Deps{
		DB: database, Log: log, WebApp: webui.Handler(webui.Options{BaseURL: cfg.BaseURL, Log: log, Organisation: func(r *http.Request) (webui.Organisation, error) {
			o, err := authSvc.Organisation(r.Context())
			return webui.Organisation{Name: o.Name, Email: o.ContactEmail, Phone: o.Phone}, err
		}}),
		Auth: authSvc, IdP: provider, BaseURL: cfg.BaseURL, SessionSecret: cfg.SessionSecret,
		Problems: problemSvc, Submissions: submissionSvc, Hub: hub, Contests: contestSvc, Integrity: integritySvc,
		AI:   &httpapi.AI{Keys: aiKeys, Slots: &ai.Slots{}},
		Mode: string(cfg.Mode), Version: version,
	})
	srv := apiServer(cfg.Listen, handler, base)
	srv.RegisterOnShutdown(cancelBase)
	judgeSrv := &http.Server{
		Addr:              cfg.JudgeListen,
		Handler:           httpapi.NewJudge(httpapi.JudgeDeps{Log: log, Jobs: jobSvc, Blobs: blobs, Token: cfg.JudgeToken}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute, // a 16 MB blob over a slow link
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	errc := make(chan error, 2)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "base_url", cfg.BaseURL)
		errc <- srv.ListenAndServe()
	}()
	go func() {
		log.Info("judge api listening", "addr", cfg.JudgeListen)
		if err := judgeSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("judge api: %w", err)
		}
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	stop() // a second interrupt now kills the process immediately
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	judgeSrv.Shutdown(shutdownCtx)
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("connections still open after 10 s; closing them", "err", err)
		srv.Close()
	}
	return nil
}

// apiServer is the public listener's server (behind Caddy). A request must
// arrive whole, headers and body, within ReadTimeout; the routes that take
// large uploads extend their own deadline (httpapi.SlowBodyTimeout). There
// is no WriteTimeout, because server-sent event streams stay open for
// hours; once a request is read, Go clears the read deadline, so the
// stream is not cut either (TestEventStreamOutlivesReadTimeout).
func apiServer(addr string, h http.Handler, base context.Context) *http.Server {
	return &http.Server{
		BaseContext:       func(net.Listener) context.Context { return base },
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// sweepJobs returns jobs whose judge went quiet to the queue every 10 s.
func sweepJobs(ctx context.Context, j *jobs.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		requeued, failed, err := j.Sweep(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("job sweep failed", "err", err)
		case requeued > 0 || failed > 0:
			log.Warn("judge leases expired", "requeued", requeued, "failed", failed)
		}
	}
}

// retainIntegrity applies the integrity retention rules at start-up and
// every 12 hours: contest records are deleted 180 days after a contest
// ends, except for students in an open case (D47), and a closed case's
// evidence and explanations are emptied 2 years after its decision. The
// same pass then removes test files and sources nothing refers to any more
// (gcBlobs, problems.Blobs.GC).
func retainIntegrity(ctx context.Context, c *contests.Service, cases *integrity.Service, log *slog.Logger, gcBlobs func(context.Context)) {
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	for {
		if events, subs, err := c.Retain(ctx); err != nil && ctx.Err() == nil {
			log.Warn("integrity retention failed", "err", err)
		} else if events > 0 || subs > 0 {
			log.Info("integrity data past retention removed", "events", events, "submissions", subs)
		}
		if n, err := cases.Retain(ctx); err != nil && ctx.Err() == nil {
			log.Warn("integrity case retention failed", "err", err)
		} else if n > 0 {
			log.Info("integrity case evidence past retention removed", "cases", n)
		}
		gcBlobs(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// retainDaily runs the retention job a minute after start-up (so a
// restart during a contest does not add its writes to the start) and then
// every 24 hours.
func retainDaily(ctx context.Context, j *retention.Job, log *slog.Logger) {
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 24 * time.Hour
		start := time.Now()
		res, err := j.Run(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("retention job failed; it runs again tomorrow", "err", err)
		case res != (retention.Result{}):
			log.Info("data past retention removed", "runs_emptied", res.RunsEmptied, "contact_messages", res.ContactDeleted,
				"revisions", res.RevisionsDeleted, "contests", res.ContestsExpired, "submissions", res.SubmissionsDeleted,
				"audit_rows", res.AuditDeleted, "ms", time.Since(start).Milliseconds())
		}
	}
}

// sweepAIKeys drops expired AI keys every 10 minutes, so a key whose
// session never comes back does not stay in memory past its two hours by
// more than that.
func sweepAIKeys(ctx context.Context, k *ai.Keys) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.Sweep()
		}
	}
}

// sweepSessions deletes expired sessions every hour until ctx ends.
func sweepSessions(ctx context.Context, a *auth.Service, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := a.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			log.Warn("session sweep failed", "err", err)
		} else if n > 0 {
			log.Debug("expired sessions removed", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
