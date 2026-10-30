package judge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for isolate when PODIUM_FAKE_ISOLATE names a
// directory: it appends its arguments to calls.txt there, and answers
// --init as "busy" until it has been called busy.txt's number of times
// (or always for -1), with any other error for "fail", and otherwise with
// the box root.
func TestMain(m *testing.M) {
	if dir := os.Getenv("PODIUM_FAKE_ISOLATE"); dir != "" {
		os.Exit(fakeIsolate(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeIsolate(dir string, args []string) int {
	f, err := os.OpenFile(filepath.Join(dir, "calls.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 3
	}
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	if !strings.Contains(strings.Join(args, " "), "--init") {
		return 0
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	inits := strings.Count(string(calls), "--init")
	mode, _ := os.ReadFile(filepath.Join(dir, "busy.txt"))
	switch m := strings.TrimSpace(string(mode)); m {
	case "fail":
		fmt.Fprintln(os.Stderr, "Cannot create control group")
		return 2
	default:
		busy, _ := strconv.Atoi(m)
		if busy < 0 || inits <= busy {
			fmt.Fprintln(os.Stderr, "This box is currently in use by another process")
			return 2
		}
	}
	fmt.Println(dir)
	return 0
}

// fakeIsolate returns an Isolate run by this test binary, in mode busy
// ("2", "-1" or "fail"), and a function reading the calls it got.
func newFakeIsolate(t *testing.T, busy string) (Isolate, func() []string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "busy.txt"), []byte(busy), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PODIUM_FAKE_ISOLATE", dir)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := initBackoff
	initBackoff = []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond}
	t.Cleanup(func() { initBackoff = old })
	calls := func() []string {
		b, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
		return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), " ", "_"))
	}
	return Isolate{Path: exe}, calls
}

func count(calls []string, flag string) int {
	n := 0
	for _, c := range calls {
		if strings.Contains(c, flag) {
			n++
		}
	}
	return n
}

// A box another process holds is waited for, not failed at once (walk
// 2026-10-21, D325); a box that stays busy fails clearly after a bounded
// number of tries; Init never cleans up a box it did not get.
func TestInitRetriesABusyBox(t *testing.T) {
	ctx := context.Background()

	iso, calls := newFakeIsolate(t, "2")
	dir, err := iso.Init(ctx, 7)
	if err != nil || !strings.HasSuffix(dir, "box") {
		t.Fatalf("busy twice, then free: %q %v", dir, err)
	}
	if n := count(calls(), "--init"); n != 3 {
		t.Errorf("%d inits, want 3", n)
	}
	if err := iso.Cleanup(ctx, 7); err != nil {
		t.Fatal(err)
	}

	iso, calls = newFakeIsolate(t, "-1")
	_, err = iso.Init(ctx, 7)
	if !errors.Is(err, ErrBoxBusy) || !strings.Contains(err.Error(), "after 5 tries") {
		t.Fatalf("always busy: %v", err)
	}
	if got := calls(); count(got, "--init") != len(initBackoff)+1 || count(got, "--cleanup") != 0 {
		t.Errorf("always busy: calls %v, want %d inits and no cleanup", got, len(initBackoff)+1)
	}
	// The failed Init gave up its claim: the box can be tried again.
	if _, err := iso.Init(ctx, 7); !errors.Is(err, ErrBoxBusy) {
		t.Errorf("second try after a busy failure: %v", err)
	}

	iso, calls = newFakeIsolate(t, "fail")
	if _, err := iso.Init(ctx, 7); err == nil || errors.Is(err, ErrBoxBusy) || count(calls(), "--init") != 1 {
		t.Errorf("another failure is not retried: %v, calls %v", err, calls())
	}

	// A cancelled job stops waiting.
	iso, _ = newFakeIsolate(t, "-1")
	initBackoff = []time.Duration{time.Hour}
	// Long enough for the first init to finish on a loaded machine, so the
	// deadline lands in the hour-long wait.
	cctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	start := time.Now()
	if _, err := iso.Init(cctx, 7); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("cancelled wait: %v after %v", err, time.Since(start))
	}
}

// This process never initialises a box it already holds: a second Init
// before Cleanup is refused without calling isolate, so a running test's
// box is never wiped under it.
func TestInitRefusesABoxThisJudgeHolds(t *testing.T) {
	ctx := context.Background()
	iso, calls := newFakeIsolate(t, "0")
	if _, err := iso.Init(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := iso.Init(ctx, 3); !errors.Is(err, ErrBoxClaimed) {
		t.Fatalf("second Init: %v", err)
	}
	if n := count(calls(), "--init"); n != 1 {
		t.Errorf("%d inits, want 1", n)
	}
	if _, err := iso.Init(ctx, 4); err != nil {
		t.Errorf("another box: %v", err)
	}
	iso.Cleanup(ctx, 3)
	iso.Cleanup(ctx, 4)
	if _, err := iso.Init(ctx, 3); err != nil {
		t.Errorf("Init after Cleanup: %v", err)
	}
	iso.Cleanup(ctx, 3)
}
