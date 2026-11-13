//go:build sandbox && linux

package calib

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

const (
	// memory.events of the slice adds up the events of every box below it,
	// so a bomb stopped at its own box limit raises it. memory.events.local
	// counts only events where the slice's own limit was the one hit.
	sliceEventsLocal = "/sys/fs/cgroup/isolate.slice/memory.events.local"
	boxCgroups       = "/sys/fs/cgroup/isolate.slice/isolate.service"
)

// sliceEvents reads the oom and oom_kill counters of isolate.slice itself.
// It fails the test when the file cannot be read: a test that cannot see
// the counters proves nothing.
func sliceEvents(t *testing.T) (oom, oomKill int) {
	t.Helper()
	f, err := os.Open(sliceEventsLocal)
	if err != nil {
		t.Fatalf("cannot read %s, so the test cannot tell whether the kernel killed anything at the slice level: %v", sliceEventsLocal, err)
	}
	defer f.Close()
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			continue
		}
		switch key {
		case "oom":
			oom, seen = n, seen+1
		case "oom_kill":
			oomKill, seen = n, seen+1
		}
	}
	if seen != 2 {
		t.Fatalf("%s has no oom and oom_kill lines", sliceEventsLocal)
	}
	return oom, oomKill
}

// compileFor compiles a program in its own box and returns it.
func compileFor(t *testing.T, r *judge.Runner, box int, file, lang string) (judge.Program, string) {
	t.Helper()
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog")
	os.Mkdir(prog, 0o755)
	p, cr, err := r.Compile(context.Background(), box, judge.Languages[lang], Source(file), prog)
	if err != nil || cr.Verdict != judge.OK {
		t.Fatalf("compile %s in box %d: %v %s %s", file, box, err, cr.Verdict, cr.Output)
	}
	in := filepath.Join(dir, "in")
	os.WriteFile(in, nil, 0o644)
	return p, dir
}

// Two lanes memory-bomb at once and a third runs a correct program beside
// them. Each bomb is stopped at its own 256 MB box limit, the correct
// program is accepted, and the kernel never has to step in at the level of
// the whole sandbox slice: its own oom and oom_kill counts do not move.
func TestTwoMemoryBombsAndACorrectProgramAtOnce(t *testing.T) {
	boxes := [3]int{testBox + 2, testBox + 3, testBox + 4}
	files := [3]string{"malloc_2g.cpp", "malloc_2g.cpp", "hello.cpp"}
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var progs [3]judge.Program
	var dirs [3]string
	for i, b := range boxes {
		if err := r.Iso.Cleanup(ctx, b); err != nil {
			t.Fatalf("clean box %d: %v", b, err)
		}
		progs[i], dirs[i] = compileFor(t, r, b, files[i], "cpp17")
	}
	oomBefore, killBefore := sliceEvents(t)
	var res [3]judge.ExecResult
	var errs [3]error
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(3)
	done.Add(3)
	for i := range boxes {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			res[i], errs[i] = r.Execute(ctx, boxes[i], progs[i], time.Second, 256, filepath.Join(dirs[i], "in"), filepath.Join(dirs[i], "out"))
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	oomAfter, killAfter := sliceEvents(t)
	for i := range boxes {
		if errs[i] != nil {
			t.Fatalf("box %d: %v", boxes[i], errs[i])
		}
		want := judge.MemoryLimit
		if i == 2 {
			want = judge.OK
		}
		if res[i].Verdict != want {
			t.Errorf("%s in box %d: got %s, want %s (%s)", files[i], boxes[i], res[i].Verdict, want, res[i].Stderr)
		}
	}
	if oomAfter != oomBefore || killAfter != killBefore {
		t.Errorf("isolate.slice own counters moved: oom %d to %d, oom_kill %d to %d; the slice's limit was hit", oomBefore, oomAfter, killBefore, killAfter)
	}
}

// boxCgroupProcs lists the processes still in the box's control group. A
// box that is cleaned up has no group at all, which is an empty list.
func boxCgroupProcs(t *testing.T, box int) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(boxCgroups, fmt.Sprintf("box-%d", box), "cgroup.procs"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("cannot read the control group of box %d: %v", box, err)
	}
	return strings.Fields(string(b))
}

// A Java program that starts sleeping threads without end is not accepted,
// leaves no process in the box, and the next correct Java program in the
// same box is accepted.
func TestJavaThreadBombLeavesTheBoxClean(t *testing.T) {
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var bomb Case
	for _, c := range Hostile {
		if c.File == "java_thread_bomb.java" {
			bomb = c
		}
	}
	if bomb.File == "" {
		t.Fatal("java_thread_bomb.java is not in the hostile list")
	}
	o, err := RunCase(ctx, r, testBox, t.TempDir(), bomb)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("thread bomb: %s (%s)", o.Got, o.Detail)
	if o.Got == judge.OK {
		t.Errorf("the thread bomb was accepted")
	}
	if left := boxCgroupProcs(t, testBox); len(left) > 0 {
		t.Errorf("processes left in box %d: %v", testBox, left)
	}
	next, err := RunCase(ctx, r, testBox, t.TempDir(), Case{Name: "hello", Lang: "java21", File: "hello.java", Want: []judge.Verdict{judge.OK}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Got != judge.OK {
		t.Errorf("the next correct program got %s (%s)", next.Got, next.Detail)
	}
}
