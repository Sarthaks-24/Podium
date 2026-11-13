package calib

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// SelfTest runs the hostile suite and a hello world in every language on
// this host, in box, and prints one PASS or FAIL line per program to w
// (podium-judge selftest). It is the same check as the sandbox
// tests (go test -tags sandbox), shipped in the binary so it runs on the
// VM, where there is no Go toolchain. It returns how many failed; err is
// set only when the suite itself could not run.
func SelfTest(ctx context.Context, r *judge.Runner, box int, w io.Writer) (failed int, err error) {
	work, err := os.MkdirTemp("", "podium-selftest-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)
	if err := r.Iso.Cleanup(ctx, box); err != nil {
		return 0, fmt.Errorf("clean box %d: %w", box, err)
	}
	for _, c := range Hostile {
		o, err := RunCase(ctx, r, box, work, c)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(w, "FAIL %s: could not run: %v\n", c.Name, err)
		case o.Contained():
			fmt.Fprintf(w, "PASS %s: %s in %s\n", c.Name, o.Got, o.Wall.Round(time.Millisecond))
		default:
			failed++
			fmt.Fprintf(w, "FAIL %s: got %s, want one of %v; %s\n", c.Name, o.Got, c.Want, o.Detail)
		}
	}
	for _, id := range []string{"cpp17", "java21", "python3", "node22"} {
		if err := helloWorld(ctx, r, box, work, id); err != nil {
			failed++
			fmt.Fprintf(w, "FAIL hello world (%s): %v\n", judge.Languages[id].Name, err)
		} else {
			fmt.Fprintf(w, "PASS hello world (%s)\n", judge.Languages[id].Name)
		}
	}
	return failed, nil
}

var helloFile = map[string]string{"cpp17": "hello.cpp", "java21": "hello.java", "python3": "hello.py", "node22": "hello.js"}

// helloWorld compiles and runs the language's hello world and checks its
// output, proving the toolchain works inside the box.
func helloWorld(ctx context.Context, r *judge.Runner, box int, work, id string) error {
	dir, err := os.MkdirTemp(work, "hello-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	p, cr, err := r.Compile(ctx, box, judge.Languages[id], Source(helloFile[id]), mustMkdir(dir, "prog"))
	if err != nil {
		return err
	}
	if cr.Verdict != judge.OK {
		return fmt.Errorf("compile: %s %s", cr.Verdict, firstLine(cr.Output))
	}
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	if err := os.WriteFile(in, nil, 0o644); err != nil {
		return err
	}
	res, err := r.Execute(ctx, box, p, time.Second, 256, in, out)
	if err != nil {
		return err
	}
	if res.Verdict != judge.OK {
		return fmt.Errorf("run: %s %s", res.Verdict, firstLine(string(res.Stderr)))
	}
	f, err := os.Open(out)
	if err != nil {
		return err
	}
	defer f.Close()
	chk, err := judge.Check(judge.CheckTokens, strings.NewReader("hello\n"), f, 0)
	if err != nil || !chk.OK {
		return fmt.Errorf("output is not hello")
	}
	return nil
}

// PCHSelfTest is selftest's check of the precompiled header, run when
// PODIUM_JUDGE_PCH=on: the files in dir belong to uid (root in production)
// and no one else can write them, and a probe compile in the sandbox, with
// the student compile command, loads the header. It prints one PASS or FAIL
// line and returns 1 for a failure.
func PCHSelfTest(ctx context.Context, r *judge.Runner, dir string, uid, box int, w io.Writer) (failed int) {
	if err := judge.CheckPCHFiles(dir, uid); err != nil {
		fmt.Fprintf(w, "FAIL precompiled header: %v\n", err)
		return 1
	}
	r.SetPCH(dir)
	used, why, err := r.ProbePCH(ctx, box)
	switch {
	case err != nil:
		fmt.Fprintf(w, "FAIL precompiled header: %v\n", err)
	case !used:
		fmt.Fprintf(w, "FAIL precompiled header: g++ -Winvalid-pch did not use it: %s\n", why)
	default:
		fmt.Fprintln(w, "PASS precompiled header: g++ -Winvalid-pch loads it with the student compile command")
		return 0
	}
	return 1
}
