// Command podium-judge is the judge worker. It runs as the unprivileged
// judge user, never opens the database, and executes every compile and run
// inside isolate. Subcommands:
//
//	podium-judge work                  claim and run jobs from the server
//	podium-judge calibrate [-reps N]   measure this host and print Markdown
//
// Stop the podium-judge service before calibrating: calibrate is a separate
// process with its own memory budget and would overlap the worker's boxes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judge/calib"
	"github.com/Sarthaks-24/podium/internal/worker"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	envFile := flag.String("env-file", "", "load variables from this file before reading the environment (local development)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: podium-judge [-env-file FILE] work | calibrate [-reps N]")
	}
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			fail(err)
		}
	}
	switch flag.Arg(0) {
	case "work":
		fail(work())
	case "calibrate":
		fail(calibrate(flag.Args()[1:]))
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func calibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ExitOnError)
	reps := fs.Int("reps", 5, "repetitions per timing measurement")
	box := fs.Int("box", 80, "isolate box id to use (keep clear of lane ids)")
	fs.Parse(args)

	cfg, err := config.LoadJudge(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	work, err := os.MkdirTemp("", "podium-calibrate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	r := &judge.Runner{Iso: judge.Isolate{Path: cfg.IsolatePath}}
	// A box left behind by a killed run would make --init fail.
	if err := r.Iso.Cleanup(ctx, *box); err != nil {
		return err
	}
	var hostile []calib.Outcome
	for _, c := range calib.Hostile {
		fmt.Fprintf(os.Stderr, "hostile: %s\n", c.Name)
		o, err := calib.RunCase(ctx, r, *box, work, c)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		hostile = append(hostile, o)
	}
	fmt.Fprintln(os.Stderr, "timing: 4 languages")
	timing, err := calib.RunTiming(ctx, r, *box, work, *reps)
	if err != nil {
		return err
	}
	calib.WriteMarkdown(os.Stdout, hostDescription(), hostile, timing, *reps)
	for _, o := range hostile {
		if !o.Contained() {
			return fmt.Errorf("sandbox did not contain %q", o.Case.Name)
		}
	}
	return nil
}

// work runs the worker loop. The first SIGINT or SIGTERM stops claiming:
// every lane finishes and reports the job it holds, then the process exits
// 0 (systemd's TimeoutStopSec on podium-judge.service bounds the wait). A
// second signal abandons the running jobs at once; the server requeues
// them when their leases run out.
func work() error {
	cfg, err := config.LoadWorker(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting podium-judge", append([]any{"version", version, "lanes", cfg.Lanes}, config.Summary(os.Getenv, config.JudgeVars)...)...)
	stopCtx, stopClaims := context.WithCancel(context.Background())
	defer stopClaims()
	ctx, abandon := context.WithCancel(context.Background())
	defer abandon()
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		select {
		case <-sigs:
		case <-ctx.Done():
			return
		}
		log.Info("stopping: no new jobs; finishing the running ones (a second signal abandons them)")
		stopClaims()
		select {
		case <-sigs:
			log.Warn("second signal: abandoning running jobs; the server requeues them after their leases")
			abandon()
		case <-ctx.Done():
		}
	}()

	blobs := filepath.Join(cfg.CacheDir, "blobs")
	jobs := filepath.Join(cfg.CacheDir, "jobs")
	compiled := filepath.Join(cfg.CacheDir, "compiled")
	for _, dir := range []string{blobs, jobs, compiled} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("judge cache: %w", err)
		}
	}
	host, _ := os.Hostname()
	client := &worker.Client{BaseURL: cfg.ServerURL, Token: cfg.Token, HTTP: &http.Client{Timeout: 5 * time.Minute}}
	w := &worker.Worker{
		Runner:   &judge.Runner{Iso: judge.Isolate{Path: cfg.IsolatePath}},
		Client:   client,
		Cache:    &worker.Cache{Dir: blobs, Client: client, MaxBytes: int64(cfg.BlobCacheMB) << 20},
		Compiled: &worker.CompileCache{Dir: compiled, MaxBytes: worker.DefaultCompileCacheBytes},
		Log:      log,
		Lanes:    cfg.Lanes,
		WorkDir:  jobs,
		Name:     host,
		Version:  version,
		Isolate:  isolateVersion(ctx, cfg.IsolatePath),
	}
	w.Drain(stopCtx, ctx)
	log.Info("stopped")
	return nil
}

// isolateVersion is the first line of "isolate --version", for the log.
func isolateVersion(ctx context.Context, path string) string {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "unknown"
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}

func hostDescription() string {
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return fmt.Sprintf("%d CPUs, kernel %s, measured %s", runtime.NumCPU(),
		strings.TrimSpace(string(kernel)), time.Now().UTC().Format("2006-01-02 15:04 UTC"))
}

func fail(err error) {
	if err == nil {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, err)
	if config.IsProblems(err) {
		os.Exit(2)
	}
	os.Exit(1)
}
