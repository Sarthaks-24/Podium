//go:build sandbox && linux

package calib

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// The precompiled header (J5) on the real compiler and the real sandbox.
// Each test builds its own header directory: the suite runs as the judge
// user, who owns what it builds here, where install makes root the owner.

const (
	withInclude  = "#include <bits/stdc++.h>\nusing namespace std;\nint main() { vector<int> v{3, 1, 2}; sort(v.begin(), v.end()); cout << v[0] << v[2] << endl; }\n"
	noInclude    = "#include <cstdio>\nint main() { printf(\"%d\\n\", 7); }\n"
	harmlessDef  = "#define MOD 1000000007\n" + withInclude
	conflictDef  = "#define _GLIBCXX_DEBUG\n" + withInclude
	pragmaFirst  = "#pragma GCC optimize(\"O3\")\n" + withInclude
	otherInclude = "#include <cstdio>\n" + withInclude
)

func pchRunner(t *testing.T) (*judge.Runner, string) {
	t.Helper()
	r := runner(t)
	// Not under the test's temp folder: isolate must be able to walk to the
	// directory it binds, which the judge user's private temp folder (mode
	// 0700) does not allow.
	parent, err := os.MkdirTemp("/var/tmp", "podium-pchtest-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	os.Chmod(parent, 0o755)
	dir := filepath.Join(parent, "pch")
	if err := judge.BuildPCH(context.Background(), dir); err != nil {
		t.Fatalf("build the header: %v", err)
	}
	if err := judge.CheckPCHFiles(dir, os.Getuid()); err != nil {
		t.Fatalf("the built header fails its own check: %v", err)
	}
	r.SetPCH(dir)
	return r, dir
}

// run compiles source with r and runs it on empty input.
func run(t *testing.T, r *judge.Runner, source string) (verdict judge.Verdict, out string, binary []byte) {
	t.Helper()
	dir := t.TempDir()
	prog := filepath.Join(dir, "p")
	os.Mkdir(prog, 0o755)
	p, cr, err := r.Compile(context.Background(), testBox, judge.Languages["cpp17"], []byte(source), prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cr.Verdict != judge.OK {
		return cr.Verdict, cr.Output, nil
	}
	in, o := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	os.WriteFile(in, nil, 0o644)
	res, err := r.Execute(context.Background(), testBox, p, time.Second, 256, in, o)
	if err != nil || res.Verdict != judge.OK {
		t.Fatalf("run: %v %s %s", err, res.Verdict, res.Stderr)
	}
	b, _ := os.ReadFile(o)
	bin, _ := os.ReadFile(filepath.Join(prog, "main"))
	return judge.OK, string(b), bin
}

// The options the header is built with are the compile command's: a probe
// compile in the sandbox, with -Winvalid-pch, loads it.
func TestThePrecompiledHeaderIsUsedByTheCompileCommand(t *testing.T) {
	r, _ := pchRunner(t)
	used, why, err := r.ProbePCH(context.Background(), testBox)
	if err != nil || !used {
		t.Fatalf("used = %v (%s), %v", used, why, err)
	}
}

// When GCC may use the header and when it must read the real one, and the
// program behaves the same either way.
func TestThePrecompiledHeaderChangesNoVerdict(t *testing.T) {
	r, _ := pchRunner(t)
	plain := runner(t)
	for _, c := range []struct {
		name, src string
		used      bool
		because   string
	}{
		{"first include", withInclude, true, ""},
		{"a harmless define before it", harmlessDef, true, ""},
		{"a conflicting define before it", conflictDef, false, "_GLIBCXX_DEBUG"},
		{"a pragma before it", pragmaFirst, false, ""},
		{"another include before it", otherInclude, false, ""},
		{"no bits/stdc++.h at all", noInclude, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			used, why, err := r.ProbePCHSource(context.Background(), testBox, c.src)
			if err != nil {
				t.Fatal(err)
			}
			if used != c.used || !strings.Contains(why, c.because) {
				t.Errorf("used = %v because %q; want %v %q", used, why, c.used, c.because)
			}
			v1, out1, bin1 := run(t, r, c.src)
			v2, out2, bin2 := run(t, plain, c.src)
			if v1 != judge.OK || v2 != judge.OK || out1 != out2 {
				t.Errorf("with the header: %s %q; without: %s %q", v1, out1, v2, out2)
			}
			if c.name == "no bits/stdc++.h at all" && !bytes.Equal(bin1, bin2) {
				t.Error("a program that does not include the header compiled to another binary")
			}
		})
	}
	// A compile error stays a compile error.
	bad := "#include <bits/stdc++.h>\nint main( { return 0 }\n"
	v1, _, _ := run(t, r, bad)
	v2, _, _ := run(t, plain, bad)
	if v1 != judge.CompileError || v2 != judge.CompileError {
		t.Errorf("compile error: %s, %s", v1, v2)
	}
}

// Inside the sandbox the header directory is a read-only bind mount: even a
// directory anyone may write to cannot be changed from a program.
func TestThePrecompiledHeaderIsReadOnlyInTheSandbox(t *testing.T) {
	r, dir := pchRunner(t)
	gch := filepath.Join(dir, "bits", "stdc++.h.gch")
	for _, d := range []string{dir, filepath.Join(dir, "bits")} {
		os.Chmod(d, 0o777)
	}
	os.Chmod(gch, 0o666)
	before, _ := os.ReadFile(gch)
	ctx := context.Background()
	for name, script := range map[string]string{
		"overwrite": "echo x > " + gch,
		"append":    "echo x >> " + gch,
		"delete":    "rm -f " + gch,
		"replace":   "echo x > " + gch + ".new && mv " + gch + ".new " + gch,
		"new file":  "echo x > " + dir + "/bits/other.h",
		"truncate":  ": > " + gch,
	} {
		t.Run(name, func(t *testing.T) {
			boxDir, err := r.Iso.Init(ctx, testBox)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Iso.Cleanup(ctx, testBox)
			meta, err := r.Iso.Run(ctx, judge.RunRequest{Box: testBox, Limits: judge.Limits{CPU: 5 * time.Second, MemoryKB: 64 * 1024, Processes: 8},
				Args: []string{"/bin/sh", "-c", script}, Stdout: "out.txt", StderrToStdout: true, Dirs: []string{dir + ":maybe"}})
			if err != nil {
				t.Fatal(err)
			}
			out, _ := os.ReadFile(filepath.Join(boxDir, "out.txt"))
			if meta.ExitCode == 0 || !strings.Contains(string(out), "Read-only file system") {
				t.Errorf("the write was not refused as read-only: exit %d, %q", meta.ExitCode, out)
			}
		})
	}
	after, _ := os.ReadFile(gch)
	if !bytes.Equal(before, after) {
		t.Error("the header changed")
	}
	if _, err := os.Stat(dir + "/bits/other.h"); err == nil {
		t.Error("a file was created in the header directory")
	}
}

// A header that is missing, from other options, from another GCC, or
// damaged never fails a student's compile.
func TestAnUnusableHeaderFallsBackToANormalCompile(t *testing.T) {
	ctx := context.Background()
	_, want, _ := run(t, runner(t), withInclude)

	t.Run("missing file", func(t *testing.T) {
		r, dir := pchRunner(t)
		os.Remove(filepath.Join(dir, "bits", "stdc++.h.gch"))
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
			t.Errorf("%s %q", v, out)
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		r, dir := pchRunner(t)
		os.RemoveAll(dir)
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
			t.Errorf("%s %q", v, out)
		}
	})
	t.Run("built with other options", func(t *testing.T) {
		r, dir := pchRunner(t)
		header := filepath.Join(t.TempDir(), "h.h")
		os.WriteFile(header, []byte("#include <bits/stdc++.h>\n"), 0o644)
		if b, err := exec.Command("/usr/bin/g++", "-O2", "-std=gnu++20", "-x", "c++-header", header, "-o", filepath.Join(dir, "bits", "stdc++.h.gch")).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		used, why, err := r.ProbePCH(ctx, testBox)
		if err != nil || used || why == "" {
			t.Errorf("probe: used %v, why %q, %v", used, why, err)
		}
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
			t.Errorf("%s %q", v, out)
		}
	})
	t.Run("from another GCC", func(t *testing.T) {
		r, dir := pchRunner(t)
		gch := filepath.Join(dir, "bits", "stdc++.h.gch")
		b, _ := os.ReadFile(gch)
		// __VERSION__ is recorded as "13.3.0"; make it 93.3.0.
		i := bytes.Index(b, []byte(`"13.`))
		if i < 0 {
			t.Fatal("no recorded GCC version in the header")
		}
		b[i+1] = '9'
		os.WriteFile(gch, b, 0o644)
		used, why, err := r.ProbePCH(ctx, testBox)
		if err != nil || used || !strings.Contains(why, "__VERSION__") {
			t.Errorf("probe: used %v, why %q, %v", used, why, err)
		}
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
			t.Errorf("%s %q", v, out)
		}
	})
	t.Run("damaged file", func(t *testing.T) {
		r, dir := pchRunner(t)
		var faults int
		r.OnPCHFault = func(string) { faults++ }
		gch := filepath.Join(dir, "bits", "stdc++.h.gch")
		b, _ := os.ReadFile(gch)
		os.WriteFile(gch, b[:1000], 0o644)
		for i := 0; i < 2; i++ {
			if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
				t.Errorf("compile %d: %s %q", i, v, out)
			}
		}
		if faults != 1 {
			t.Errorf("the fault hook ran %d times, want once", faults)
		}
	})
	t.Run("a confirmed fault switches the header off", func(t *testing.T) {
		r, dir := pchRunner(t)
		gch := filepath.Join(dir, "bits", "stdc++.h.gch")
		b, _ := os.ReadFile(gch)
		os.WriteFile(gch, b[:1000], 0o644)
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want {
			t.Fatalf("first compile: %s %q", v, out)
		}
		if got := r.PCH(); got != "" {
			t.Errorf("the header is still set after a confirmed fault: %q", got)
		}
		var faults int
		r.OnPCHFault = func(string) { faults++ }
		if v, out, _ := run(t, r, withInclude); v != judge.OK || out != want || faults != 0 {
			t.Errorf("second compile: %s %q, %d faults (it must not meet the damaged header again)", v, out, faults)
		}
	})
	t.Run("student prints the fault text", func(t *testing.T) {
		r, _ := pchRunner(t)
		faults := 0
		r.OnPCHFault = func(string) { faults++ }
		src := "#include <bits/stdc++.h>\n#error while reading precompiled header\nint main() {}\n"
		if v, _, _ := run(t, r, src); v != judge.CompileError {
			t.Errorf("verdict %s, want a compile error", v)
		}
		if faults != 0 {
			t.Errorf("the fault hook ran %d times for the student's own error", faults)
		}
	})
	// The header directory is in the box only for the compile that uses it,
	// so a program can fail with the fault text there and compile cleanly
	// in the retry. That must not switch a good header off for everyone.
	t.Run("a student who fakes the fault leaves the header on", func(t *testing.T) {
		r, dir := pchRunner(t)
		faults := 0
		r.OnPCHFault = func(string) { faults++ }
		src := "#if __has_include(\"" + dir + "/bits/stdc++.h.gch\")\n#error while reading precompiled header\n#endif\n" + noInclude
		if v, out, _ := run(t, r, src); v != judge.OK || out != "7\n" {
			t.Fatalf("the faking program: %s %q", v, out)
		}
		if got := r.PCH(); got != dir {
			t.Errorf("the header was switched off by a student's program: PCH is %q, want %q", got, dir)
		}
		if faults != 0 {
			t.Errorf("the fault hook ran %d times for a good header", faults)
		}
	})
}

// selftest's line for the header: PASS for a good one, FAIL with the reason
// for one built with other options, and FAIL for one another user could
// change.
func TestPCHSelfTestReports(t *testing.T) {
	r, dir := pchRunner(t)
	ctx := context.Background()
	var out bytes.Buffer
	if failed := PCHSelfTest(ctx, r, dir, os.Getuid(), testBox, &out); failed != 0 || !strings.HasPrefix(out.String(), "PASS precompiled header") {
		t.Errorf("good header: %d %q", failed, out.String())
	}
	out.Reset()
	if failed := PCHSelfTest(ctx, r, dir, os.Getuid()+1, testBox, &out); failed != 1 || !strings.Contains(out.String(), "owned by") {
		t.Errorf("another owner: %d %q", failed, out.String())
	}
	out.Reset()
	os.Chmod(filepath.Join(dir, "bits"), 0o775)
	if failed := PCHSelfTest(ctx, r, dir, os.Getuid(), testBox, &out); failed != 1 || !strings.Contains(out.String(), "writable") {
		t.Errorf("group-writable: %d %q", failed, out.String())
	}
	os.Chmod(filepath.Join(dir, "bits"), 0o755)
	header := filepath.Join(t.TempDir(), "h.h")
	os.WriteFile(header, []byte("#include <bits/stdc++.h>\n"), 0o644)
	if b, err := exec.Command("/usr/bin/g++", "-O2", "-std=gnu++20", "-x", "c++-header", header, "-o", filepath.Join(dir, "bits", "stdc++.h.gch")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	out.Reset()
	if failed := PCHSelfTest(ctx, r, dir, os.Getuid(), testBox, &out); failed != 1 || !strings.Contains(out.String(), "did not use it") {
		t.Errorf("other options: %d %q", failed, out.String())
	}
}
