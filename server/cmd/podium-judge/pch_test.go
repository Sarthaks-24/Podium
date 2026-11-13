package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judge"
)

func pchDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bits"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bits", "stdc++.h.gch"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestUsePCH(t *testing.T) {
	good := func(context.Context, int) (bool, string, error) { return true, "", nil }
	cases := []struct {
		name   string
		dir    func(*testing.T) string
		probe  probeFunc
		want   string // runner's directory afterwards
		lines  int
		inLine string
	}{
		{"loads", pchDir, good, "dir", 1, "precompiled header in use"},
		{"missing", func(t *testing.T) string { return t.TempDir() }, good, "", 1, "compiling normally"},
		{"ignored by GCC", pchDir, func(context.Context, int) (bool, string, error) { return false, "`__VERSION__' defined as other", nil }, "", 1, "GCC ignored it: `__VERSION__'"},
		{"probe fails", pchDir, func(context.Context, int) (bool, string, error) { return false, "", errors.New("box busy") }, "", 1, "box busy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			r := &judge.Runner{}
			dir := c.dir(t)
			usePCH(context.Background(), r, dir, os.Getuid(), 0, c.probe, log)
			want := c.want
			if want == "dir" {
				want = dir
			}
			if r.PCH() != want {
				t.Errorf("runner uses %q, want %q", r.PCH(), want)
			}
			if n := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; n != c.lines || !strings.Contains(logs.String(), c.inLine) {
				t.Errorf("log (%d lines): %s", n, logs.String())
			}
			if r.OnPCHFault == nil {
				t.Error("no fault hook")
			}
		})
	}
}

func TestPCHFaultHookLogs(t *testing.T) {
	var logs bytes.Buffer
	r := &judge.Runner{}
	usePCH(context.Background(), r, t.TempDir(), 0, 0, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	logs.Reset()
	r.OnPCHFault("error: while reading precompiled header")
	if !strings.Contains(logs.String(), "unreadable") || !strings.Contains(logs.String(), "while reading precompiled header") {
		t.Errorf("log: %s", logs.String())
	}
}

func TestPCHBuildNeedsRootAndAVerb(t *testing.T) {
	if err := pch(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("no verb: %v", err)
	}
	if os.Geteuid() != 0 {
		if err := pch([]string{"build", "-dir", t.TempDir()}); err == nil || !strings.Contains(err.Error(), "must run as root") {
			t.Errorf("not root: %v", err)
		}
	}
}
