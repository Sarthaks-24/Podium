//go:build sandbox && linux

// The hostile-program suite. It needs Linux with isolate installed and the
// test user in the isolate group:
//
//	go test -tags sandbox ./internal/judge/...
//
// Run it after every change to internal/judge or to isolate flags.
package calib

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// testBox is far from the lane ids a running judge uses (0, 1, ...).
const testBox = 90

func runner(t *testing.T) *judge.Runner {
	path := os.Getenv("PODIUM_ISOLATE_PATH")
	if path == "" {
		path = "/usr/bin/isolate"
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("isolate not found at %s: %v", path, err)
	}
	r := &judge.Runner{Iso: judge.Isolate{Path: path}}
	if err := r.Iso.Cleanup(context.Background(), testBox); err != nil {
		t.Fatalf("clean box %d: %v", testBox, err)
	}
	return r
}

func TestHostilePrograms(t *testing.T) {
	r := runner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, c := range Hostile {
		t.Run(c.Name, func(t *testing.T) {
			o, err := RunCase(ctx, r, testBox, t.TempDir(), c)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %s (%s) in %s", c.Name, o.Got, o.Detail, o.Wall.Round(time.Millisecond))
			if !o.Contained() {
				t.Errorf("not contained: got %s, want one of %v; %s", o.Got, c.Want, o.Detail)
			}
		})
	}
}

func TestEveryLanguageAccepts(t *testing.T) {
	r := runner(t)
	ctx := context.Background()
	ext := map[string]string{"cpp17": "cpp", "java21": "java", "python3": "py", "node22": "js"}
	for id, e := range ext {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			prog := filepath.Join(dir, "prog")
			os.Mkdir(prog, 0o755)
			p, cr, err := r.Compile(ctx, testBox, judge.Languages[id], Source("hello."+e), prog)
			if err != nil || cr.Verdict != judge.OK {
				t.Fatalf("compile: %v %s %s", err, cr.Verdict, cr.Output)
			}
			in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
			os.WriteFile(in, nil, 0o644)
			res, err := r.Execute(ctx, testBox, p, time.Second, 256, in, out)
			if err != nil || res.Verdict != judge.OK {
				t.Fatalf("run: %v %s %s", err, res.Verdict, res.Stderr)
			}
			chk, err := judge.Check(judge.CheckTokens, strings.NewReader("hello\n"), mustOpen(t, out), 0)
			if err != nil || !chk.OK {
				t.Fatalf("output check: %v %+v", err, chk)
			}
		})
	}
}

func TestBoxesDoNotShareFiles(t *testing.T) {
	// A file written in one test's box must be gone in the next.
	r := runner(t)
	ctx := context.Background()
	writer := []byte("with open('/box/leak.txt','w') as f: f.write('x')\n")
	reader := []byte("import os\nprint('LEAKED' if os.path.exists('/box/leak.txt') else 'clean')\n")
	py := judge.Languages["python3"]
	for i, src := range [][]byte{writer, reader} {
		dir := t.TempDir()
		prog := filepath.Join(dir, "p")
		os.Mkdir(prog, 0o755)
		p, _, err := r.Compile(ctx, testBox, py, src, prog)
		if err != nil {
			t.Fatal(err)
		}
		in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
		os.WriteFile(in, nil, 0o644)
		if _, err := r.Execute(ctx, testBox, p, time.Second, 64, in, out); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			b, _ := os.ReadFile(out)
			if strings.TrimSpace(string(b)) != "clean" {
				t.Errorf("second box saw the first box's file: %q", b)
			}
		}
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// A box another process is running in is waited for and only then
// initialised: Init must not return while that run still holds the box
// (walk 2026-10-21, D325).
func TestBusyBoxIsWaitedFor(t *testing.T) {
	const box = testBox + 1
	r := runner(t)
	ctx := context.Background()
	path := r.Iso.Path
	if err := r.Iso.Cleanup(ctx, box); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(path, "--cg", "--box-id="+strconv.Itoa(box), "--init").CombinedOutput(); err != nil {
		t.Fatalf("init the other process's box: %v %s", err, out)
	}
	other := exec.Command(path, "--cg", "--silent", "--box-id="+strconv.Itoa(box), "--time=5", "--wall-time=5", "--run", "--", "/bin/sleep", "1.5")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan time.Time, 1)
	go func() {
		other.Wait()
		ended <- time.Now()
	}()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	dir, err := r.Iso.Init(ctx, box)
	got := time.Now()
	if err != nil {
		t.Fatalf("Init of a box busy for about 1.2 s more: %v", err)
	}
	defer r.Iso.Cleanup(ctx, box)
	select {
	case at := <-ended:
		// at is taken just after Wait returns, so it can only be late.
		if got.Before(at.Add(-100 * time.Millisecond)) {
			t.Errorf("Init returned %v before the other run ended", at.Sub(got))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Init returned while the other run still held the box")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("box dir: %v", err)
	}
	t.Logf("waited %v for the busy box", got.Sub(start).Round(time.Millisecond))
}
