package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// probeFunc is judge.Runner.ProbePCH; tests replace it.
type probeFunc func(ctx context.Context, box int) (used bool, why string, err error)

// usePCH decides at start whether the runner compiles with the precompiled
// header (PODIUM_JUDGE_PCH=on). The header must exist, belong to uid and be
// writable by nobody else, and a probe compile with the exact student
// command must load it. Anything less writes one log line and leaves the
// runner compiling normally: the setting never stops the judge.
func usePCH(ctx context.Context, r *judge.Runner, dir string, uid, box int, probe probeFunc, log *slog.Logger) {
	r.OnPCHFault = func(output string) {
		log.Warn("precompiled header unreadable; compiling without it until it is rebuilt (podium-judge pch build)", "output", clip(output, 300))
	}
	if err := judge.CheckPCHFiles(dir, uid); err != nil {
		log.Warn("precompiled header not used; compiling normally", "reason", err.Error())
		return
	}
	r.SetPCH(dir)
	used, why, err := probe(ctx, box)
	switch {
	case err != nil:
		r.SetPCH("")
		log.Warn("precompiled header not used; compiling normally", "reason", err.Error())
	case !used:
		r.SetPCH("")
		log.Warn("precompiled header not used; compiling normally", "reason", "GCC ignored it: "+why)
	default:
		log.Info("precompiled header in use", "dir", dir)
	}
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// pch is "podium-judge pch build": precompile <bits/stdc++.h> into the
// header directory. install.sh runs it as root after installing the
// binaries, on a fresh install and on every upgrade.
func pch(args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return fmt.Errorf("usage: podium-judge pch build [-dir DIR]")
	}
	fs := flag.NewFlagSet("pch build", flag.ExitOnError)
	dir := fs.String("dir", judge.PCHDir, "directory for the header")
	fs.Parse(args[1:])
	if os.Geteuid() != 0 {
		return fmt.Errorf("pch build must run as root: the header is owned by root so no one else can replace it")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := judge.BuildPCH(ctx, *dir); err != nil {
		return err
	}
	fmt.Printf("precompiled header built in %s\n", *dir)
	return nil
}
