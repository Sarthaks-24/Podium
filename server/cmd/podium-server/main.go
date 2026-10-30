// Command podium-server is the contest platform's web server: the JSON API,
// server-sent events, the embedded web app, and the judge queue. All
// configuration comes from environment variables (docs/OPERATIONS.md).
//
// With no argument it runs the server. The subcommands are operator tools
// that open the database themselves (usage in flag.Usage below, details in
// docs/OPERATIONS.md): check, runs-report, migrate and forget-user.
// forget-user is a dry run unless given -apply.
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
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/jobs/judgeserver"
	"github.com/Sarthaks-24/podium/internal/schedule"
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
		fmt.Fprintln(os.Stderr, "       podium-server runs-report [-db F]          runs with a custom input, per contest")
		fmt.Fprintln(os.Stderr, "       podium-server migrate -db COPY             rehearse an upgrade on a copy of the database")
		fmt.Fprintln(os.Stderr, "       podium-server forget-user [-db F] [-apply] EMAIL  erase one person (a dry run without -apply)")
	}
	flag.Parse()
	if *showVersion {
		buildinfo.Write(os.Stdout)
		return
	}
	if flag.Arg(0) == "check" {
		os.Exit(checkCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "migrate" {
		os.Exit(migrateCmd(flag.Args()[1:], os.Stdout))
	}
	if flag.Arg(0) == "forget-user" {
		os.Exit(forgetCmd(flag.Args()[1:], os.Stdout))
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

	svc, err := newServices(ctx, cfg, database, log)
	if err != nil {
		return err
	}
	var provider httpapi.IdentityProvider
	if cfg.Mode == config.ModeTesting {
		log.Warn("testing mode: Google sign-in is off and anyone at this machine can sign in as anyone; listening on loopback only")
		if ai.HasFake() {
			log.Warn("testing mode: the AI wizard offers the test provider with canned problems")
		} else {
			log.Warn("testing mode: this build has no test AI provider; build with -tags fakeai (make dev-test) to get it")
		}
	} else {
		provider = auth.NewProvider(auth.GoogleEndpoints, cfg.GoogleClientID, cfg.GoogleClientSecret, httpapi.RedirectURL(cfg.BaseURL))
	}
	if cfg.JudgeToken == config.TestingJudgeToken {
		log.Warn("judge API uses the public testing token; set PODIUM_JUDGE_TOKEN before going to production")
	}
	sched := schedule.Start(svc.tasks())

	// Request contexts derive from base, which is cancelled when shutdown
	// begins, so long-lived event streams end instead of holding Shutdown
	// open until its timeout.
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := apiServer(cfg.Listen, svc.handler(cfg, provider), base)
	srv.RegisterOnShutdown(cancelBase)
	judgeSrv := judgeServer(cfg.JudgeListen, judgeserver.New(judgeserver.Deps{Log: log, Jobs: svc.jobs, Blobs: svc.blobs, Token: cfg.JudgeToken}))
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
	var serveErr error
	select {
	case err := <-errc:
		serveErr = fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	stop() // a second interrupt now kills the process immediately
	log.Info("shutting down")
	shutdown(log, sched, judgeSrv, srv)
	return serveErr
}

// shutdown stops the servers, then the background jobs, waiting for a
// batch in progress to commit, all before the caller closes the database.
func shutdown(log *slog.Logger, sched *schedule.Scheduler, judgeSrv, srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	judgeSrv.Shutdown(ctx)
	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("connections still open after 10 s; closing them", "err", err)
		srv.Close()
	}
	if !sched.Stop(10 * time.Second) {
		log.Warn("a background job was still running after 10 s; closing the database under it")
	}
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

// judgeServer is the judge API's own listener.
func judgeServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute, // a 16 MB blob over a slow link
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
