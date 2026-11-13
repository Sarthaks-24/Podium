//go:build sandbox && linux

package calib

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/sizing"
)

// These tests run under the sandbox cap a bigger machine gets, which
// tools/sandbox-test.sh sets for the run when PODIUM_SANDBOX_CAP_MB is
// given (make sandbox-test-bigcap). Without it they skip: the cap of
// isolate.slice belongs to the host and a test user cannot change it.
func bigCap(t *testing.T) int {
	t.Helper()
	want := os.Getenv("PODIUM_SANDBOX_CAP_MB")
	if want == "" {
		t.Skip("PODIUM_SANDBOX_CAP_MB is not set; run make sandbox-test-bigcap")
	}
	wantMB, err := strconv.Atoi(want)
	if err != nil {
		t.Fatalf("PODIUM_SANDBOX_CAP_MB=%q", want)
	}
	got, err := sizing.ReadSliceCapMB(sizing.SliceCapPath)
	if err != nil || got != wantMB {
		t.Fatalf("the sandbox slice allows %d MB (%v), the run was meant to be under %d MB", got, err, wantMB)
	}
	return got
}

func runProgram(t *testing.T, file, lang string, timeLimit time.Duration, memoryMB int) judge.ExecResult {
	t.Helper()
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog")
	os.Mkdir(prog, 0o755)
	p, cr, err := r.Compile(ctx, testBox, judge.Languages[lang], Source(file), prog)
	if err != nil || cr.Verdict != judge.OK {
		t.Fatalf("compile %s: %v %s %s", file, err, cr.Verdict, cr.Output)
	}
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	os.WriteFile(in, nil, 0o644)
	res, err := r.Execute(ctx, testBox, p, timeLimit, memoryMB, in, out)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The bug J1 fixes: a Java run on a problem with the largest memory limit
// is given 1,280 MB, more than the 1,100 MB cap of the 2 GB machine, so a
// correct program was killed by the slice, not by its own limit. Under the
// cap a machine with room gets, it finishes.
func TestJavaOnTheLargestMemoryLimitIsNotKilled(t *testing.T) {
	capMB := bigCap(t)
	res := runProgram(t, "java_large.java", "java21", 5*time.Second, 1024)
	t.Logf("verdict %s, %d MB used, cg limit %d MB, slice cap %d MB, meta %+v", res.Verdict, res.Meta.MemoryKB()/1024, res.Meta.CgMemKB/1024, capMB, res.Meta.Status)
	if res.Verdict != judge.OK {
		t.Fatalf("a correct program on a 1,024 MB problem got %s (%s)", res.Verdict, res.Stderr)
	}
	if used := res.Meta.MemoryKB() / 1024; used <= 1100 {
		t.Fatalf("the program used only %d MB, which fits the old cap of 1,100 MB; it does not show the bug", used)
	}
}

// A bigger cap must not loosen a problem's own limit: a program that goes
// past its problem's memory limit is killed at that limit, not at the cap.
func TestAMemoryBombIsStillKilledAtItsOwnLimit(t *testing.T) {
	bigCap(t)
	for _, limit := range []int{64, 256, 1024} {
		res := runProgram(t, "malloc_2g.cpp", "cpp17", time.Second, limit)
		if res.Verdict != judge.MemoryLimit {
			t.Errorf("limit %d MB: got %s, want a memory limit verdict", limit, res.Verdict)
		}
		if used := res.Meta.MemoryKB() / 1024; used > limit+limit/10 {
			t.Errorf("limit %d MB: the program held %d MB before it was stopped", limit, used)
		}
	}
	res := runProgram(t, "java_oom.java", "java21", 2*time.Second, 256)
	if res.Verdict != judge.MemoryLimit {
		t.Errorf("Java past -Xmx256m: got %s", res.Verdict)
	}
}
