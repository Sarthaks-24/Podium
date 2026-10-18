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

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
	"github.com/Sarthaks-24/podium/internal/webui"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	envFile := flag.String("env-file", "", "load variables from this file before reading the environment (local development)")
	flag.Parse()
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
	jobSvc := &jobs.Service{DB: database, Kinds: map[string]jobs.Kind{
		judgeapi.KindValidate: problems.ValidateKind{},
		judgeapi.KindGenerate: problems.GenerateKind{Store: blobs},
		judgeapi.KindSubmit:   submissions.SubmitKind{},
		judgeapi.KindRun:      submissions.RunKind{},
	}}
	problemSvc := &problems.Service{DB: database, Blobs: blobs, Jobs: jobSvc}
	hub := &events.Hub{}
	submissionSvc := &submissions.Service{DB: database, Jobs: jobSvc, Problems: problemSvc, Hub: hub, HashKey: cfg.SessionSecret}
	jobSvc.OnChange = submissionSvc.OnJobChange
	go sweepJobs(ctx, jobSvc, log)
	handler := httpapi.New(httpapi.Deps{
		DB: database, Log: log, WebApp: webui.Handler(),
		Auth: authSvc, IdP: provider, BaseURL: cfg.BaseURL, SessionSecret: cfg.SessionSecret,
		Problems: problemSvc, Submissions: submissionSvc, Hub: hub, Mode: string(cfg.Mode),
	})
	srv := &http.Server{
		BaseContext:       func(net.Listener) context.Context { return base },
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Long enough for a 32 MB pack on a slow link; headers are still
		// bounded by ReadHeaderTimeout.
		ReadTimeout: 120 * time.Second,
		// No WriteTimeout: server-sent event streams stay open for hours.
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
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
