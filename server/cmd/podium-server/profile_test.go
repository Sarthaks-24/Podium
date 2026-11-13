package main

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func burn(d time.Duration) {
	end := time.Now().Add(d)
	x := 0
	for time.Now().Before(end) {
		for i := 0; i < 1e5; i++ {
			x += i * i
		}
	}
	_ = x
}

// Outside testing mode the profile is refused, names the variable, and the
// file is not even created.
func TestCPUProfileRefusedOutsideTestingMode(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeDev, config.ModeProduction} {
		path := filepath.Join(t.TempDir(), "run.pprof")
		stop, err := startCPUProfile(config.Server{Mode: mode, CPUProfile: path}, discardLog())
		if err == nil || !strings.Contains(err.Error(), "PODIUM_CPU_PROFILE") {
			t.Errorf("%s: error %v, want a message naming PODIUM_CPU_PROFILE", mode, err)
		}
		if stop != nil {
			stop()
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: the profile file was created", mode)
		}
	}
}

func TestCPUProfileNotSetDoesNothing(t *testing.T) {
	stop, err := startCPUProfile(config.Server{Mode: config.ModeProduction}, discardLog())
	if err != nil || stop == nil {
		t.Fatalf("unset: %v", err)
	}
	stop()
}

func TestCPUProfileUnwritablePathRefusedAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "run.pprof")
	_, err := startCPUProfile(config.Server{Mode: config.ModeTesting, CPUProfile: path}, discardLog())
	if err == nil || !strings.Contains(err.Error(), "PODIUM_CPU_PROFILE") {
		t.Errorf("error %v, want a message naming PODIUM_CPU_PROFILE", err)
	}
}

// A run leaves a closed, non-empty file readable only by its owner.
func TestCPUProfileWritesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.pprof")
	stop, err := startCPUProfile(config.Server{Mode: config.ModeTesting, CPUProfile: path}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	burn(150 * time.Millisecond)
	stop()
	stop() // twice is harmless
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("profile file: %v %v", fi, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	// A file still open cannot be removed on Windows; elsewhere this only
	// checks that the file can be replaced by a second run.
	if err := os.Remove(path); err != nil {
		t.Errorf("file not closed: %v", err)
	}
	stop2, err := startCPUProfile(config.Server{Mode: config.ModeTesting, CPUProfile: path}, discardLog())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	stop2()
}

// Profiles are written to a file; no HTTP endpoint serves them.
func TestNoHTTPProfileEndpoint(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "profile_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err == nil && strings.Contains(string(b), "net/http/"+"pprof") {
			t.Errorf("%s imports the HTTP profile endpoint", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
