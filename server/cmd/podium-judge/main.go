// Command podium-judge is the judge worker. It runs as the unprivileged
// judge user, never opens the database, and executes every compile and run
// inside isolate. Subcommands:
//
//	podium-judge calibrate [-reps N]   measure this host and print Markdown
//
// Stop the podium-judge service before calibrating: calibrate is a separate
// process with its own memory budget and would overlap the worker's boxes.
//
// The worker loop that claims submissions from the server arrives in M3.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judge/calib"
)

func main() {
	envFile := flag.String("env-file", "", "load variables from this file before reading the environment (local development)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: podium-judge [-env-file FILE] calibrate [-reps N]")
	}
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			fail(err)
		}
	}
	switch flag.Arg(0) {
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
