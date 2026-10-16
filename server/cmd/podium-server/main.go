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

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/httpapi"
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

	// Request contexts derive from base, which is cancelled when shutdown
	// begins, so long-lived event streams end instead of holding Shutdown
	// open until its timeout.
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		BaseContext:       func(net.Listener) context.Context { return base },
		Addr:              cfg.Listen,
		Handler:           httpapi.New(httpapi.Deps{DB: database, Log: log, WebApp: webui.Handler()}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// No WriteTimeout: server-sent event streams stay open for hours.
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	srv.RegisterOnShutdown(cancelBase)
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "base_url", cfg.BaseURL)
		errc <- srv.ListenAndServe()
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
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("connections still open after 10 s; closing them", "err", err)
		srv.Close()
	}
	return nil
}
