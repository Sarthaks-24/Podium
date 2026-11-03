//go:build sandbox && linux

package calib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// TestDeepDirectories (audit A16, J3): a program that nests 1,500
// directories fails its own job with an error the worker reports as
// Fatal ("The judge could not finish this submission"), never as a box
// init failure, which would leave the job leased and requeue the same
// program. Once the box is cleaned with enough open files (what
// reset-boxes does as root before the judge restarts), the next job on
// the same box is judged normally.
func TestDeepDirectories(t *testing.T) {
	const box = testBox + 2
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &orig); err != nil {
		t.Fatal(err)
	}
	// Setting the limit here also makes child processes (isolate)
	// inherit it, instead of the soft limit Go restores for them.
	soft := func(n uint64) {
		lim := orig
		lim.Cur = min(n, orig.Max)
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig) })
	soft(orig.Max)
	if err := r.Iso.Cleanup(ctx, box); err != nil {
		t.Fatalf("clean box %d before the test: %v", box, err)
	}
	if orig.Max < 4096 {
		t.Skipf("hard open-file limit %d is too low to heal the box afterwards", orig.Max)
	}

	cpp := judge.Languages["cpp17"]
	compile := func(name string) judge.Program {
		t.Helper()
		prog := filepath.Join(t.TempDir(), "prog")
		os.Mkdir(prog, 0o755)
		p, cr, err := r.Compile(ctx, box, cpp, Source(name), prog)
		if err != nil || cr.Verdict != judge.OK {
			t.Fatalf("compile %s: %v %s %s", name, err, cr.Verdict, cr.Output)
		}
		return p
	}
	execute := func(p judge.Program) (judge.ExecResult, error) {
		dir := t.TempDir()
		in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
		os.WriteFile(in, nil, 0o644)
		return r.Execute(ctx, box, p, 5*time.Second, 256, in, out)
	}

	deep := compile("deep_dirs.cpp")
	hello := compile("hello.cpp")

	soft(1024) // podium-judge.service runs with the default soft limit
	res, err := execute(deep)
	if err == nil {
		t.Fatalf("deep directories ran with verdict %s; isolate 2.2.1 should fail its walk (J0)", res.Verdict)
	}
	if errors.Is(err, judge.ErrBoxInit) {
		t.Fatalf("the poisoning run is a box init failure, so its job would be requeued: %v", err)
	}
	t.Logf("poisoning run: %v", err)
	if !r.BoxBroken(box) {
		t.Error("the poisoned box is not marked broken, so its lane would claim another student's job")
	}

	soft(orig.Max)
	if err := r.Iso.Cleanup(ctx, box); err != nil {
		t.Fatalf("clean the deep box with a high open-file limit: %v", err)
	}
	if res, err := execute(hello); err != nil || res.Verdict != judge.OK {
		t.Fatalf("next job on the healed box: %v %s %s", err, res.Verdict, res.Stderr)
	}
}
