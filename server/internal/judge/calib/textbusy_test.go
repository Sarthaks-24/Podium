//go:build sandbox && linux

package calib

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// The checker of the pair-with-sum pack.
//
//go:embed testdata/pair_checker.cpp
var pairChecker []byte

// A right output is accepted by a custom checker every time, with two lanes
// checking and a third compiling. On the test VM (2026-11-14) one right
// output in about 3,900 jobs got "wrong answer": the checker had just been
// copied into its box, another lane forked while that file was still open
// for writing, and the sandbox could not start it ("text file busy", exit
// 127, which read as wrong answer). Before the fix this test failed about
// ten times in 3,000 runs on two CPUs. PODIUM_STRESS_N sets the runs a lane.
func TestARightOutputIsAcceptedWhileOtherLanesStartSandboxes(t *testing.T) {
	n := 750
	if v, err := strconv.Atoi(os.Getenv("PODIUM_STRESS_N")); err == nil && v > 0 {
		n = v
	}
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	in, ans, out := write("in", "4 3\n1 9 9 2\n"), write("ans", "1 4\n"), write("out", "1 4\n")
	lanes, loadBox := []int{testBox + 2, testBox + 3}, testBox+4
	for _, b := range append([]int{loadBox}, lanes...) {
		if err := r.Iso.Cleanup(ctx, b); err != nil {
			t.Fatalf("clean box %d: %v", b, err)
		}
	}
	cdir := filepath.Join(dir, "checker")
	os.Mkdir(cdir, 0o755)
	checker, cr, err := r.Compile(ctx, testBox, judge.CheckerLanguage(), pairChecker, cdir)
	if err != nil || cr.Verdict != judge.OK {
		t.Fatalf("compile the checker: %v %s %s", err, cr.Verdict, cr.Output)
	}
	var stop atomic.Bool
	var load sync.WaitGroup
	load.Add(1)
	go func() { // compiles beside the checkers, as at the end of a contest
		defer load.Done()
		for i := 0; !stop.Load(); i++ {
			d := filepath.Join(dir, "load"+strconv.Itoa(i))
			os.Mkdir(d, 0o755)
			r.Compile(ctx, loadBox, judge.Languages["cpp17"], Source("hello.cpp"), d)
			os.RemoveAll(d)
		}
	}()
	var bad atomic.Int64
	var wg sync.WaitGroup
	for _, box := range lanes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				res, err := r.RunChecker(ctx, box, checker, in, out, ans)
				if err != nil {
					bad.Add(1)
					t.Errorf("box %d run %d: %v", box, i, err)
					return
				}
				if res.Verdict != judge.Accepted {
					bad.Add(1)
					t.Errorf("box %d run %d: %s, message %q, exit %d, status %s", box, i, res.Verdict, res.Message, res.Meta.ExitCode, res.Meta.Status)
				}
			}
		}()
	}
	wg.Wait()
	stop.Store(true)
	load.Wait()
	t.Logf("%d checker runs in each of %d boxes: %d not accepted", n, len(lanes), bad.Load())
}
