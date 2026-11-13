package judge

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func indexOf(args []string, s string) int { return slices.Index(args, s) }

// Off, the compile command is the language's own. On, a C++ student
// compile gets the header directory on its include path and as a
// read-only isolate directory; nothing else changes.
func TestCompileCommandUsesThePCHOnlyWhenSet(t *testing.T) {
	r := &Runner{}
	cpp := Languages["cpp17"]
	args, dirs := r.compileCommand(cpp)
	if !slices.Equal(args, cpp.Compile) || !slices.Equal(dirs, cpp.Dirs) {
		t.Fatalf("off: %v %v", args, dirs)
	}
	r.SetPCH("/pch")
	args, dirs = r.compileCommand(cpp)
	i := indexOf(args, "-I")
	if i < 0 || args[i+1] != "/pch" || i > indexOf(args, "-o") {
		t.Fatalf("on: no -I /pch before -o in %v", args)
	}
	if !slices.Contains(dirs, "/pch:maybe") {
		t.Errorf("on: dirs %v lack the header directory", dirs)
	}
	// Everything but the added pair is the language's own command.
	rest := slices.Delete(slices.Clone(args), i, i+2)
	if !slices.Equal(rest, cpp.Compile) {
		t.Errorf("on: %v is not %v plus -I", args, cpp.Compile)
	}
	if !slices.Equal(Languages["cpp17"].Compile, cpp.Compile) || indexOf(Languages["cpp17"].Compile, "-I") >= 0 {
		t.Error("the language table was changed")
	}
	// Checkers, Java, Python and Node never get it.
	for _, l := range []Language{CheckerLanguage(), Languages["java21"], Languages["python3"], Languages["node22"]} {
		a, d := r.compileCommand(l)
		if !slices.Equal(a, l.Compile) || !slices.Equal(d, l.Dirs) {
			t.Errorf("%s: %v %v", l.ID, a, d)
		}
	}
	r.SetPCH("")
	if a, _ := r.compileCommand(cpp); !slices.Equal(a, cpp.Compile) {
		t.Errorf("after SetPCH(\"\"): %v", a)
	}
}

// The header is built with the compile command's own options, so GCC
// accepts it; only the input and output differ.
func TestPCHBuildArgsMatchTheCompileCommand(t *testing.T) {
	cpp := Languages["cpp17"]
	args := PCHBuildArgs(cpp, "/out/bits/stdc++.h.gch", "/w/stdc++.h")
	if args[0] != cpp.Compile[0] {
		t.Fatalf("compiler %q", args[0])
	}
	// The options before "-o main main.cpp".
	for _, opt := range cpp.Compile[1 : len(cpp.Compile)-3] {
		if !slices.Contains(args, opt) {
			t.Errorf("build lacks the compile option %s: %v", opt, args)
		}
	}
	for _, want := range [][]string{{"-x", "c++-header"}, {"-o", "/out/bits/stdc++.h.gch"}} {
		if i := indexOf(args, want[0]); i < 0 || args[i+1] != want[1] {
			t.Errorf("build lacks %v: %v", want, args)
		}
	}
	if !slices.Contains(args, "/w/stdc++.h") || slices.Contains(args, "main.cpp") {
		t.Errorf("input: %v", args)
	}
}

func TestPCHUsed(t *testing.T) {
	used := "! /usr/local/lib/podium-pch/bits/stdc++.h.gch\n main.cpp\n"
	notUsed := "main.cpp:1:25: warning: /usr/local/lib/podium-pch/bits/stdc++.h.gch: not used because `_GLIBCXX_DEBUG' is defined [-Winvalid-pch]\nx /usr/local/lib/podium-pch/bits/stdc++.h.gch\n. /usr/include/c++/13/x\n"
	if !PCHUsed(used) {
		t.Error("a used header was not recognised")
	}
	if PCHUsed(notUsed) || PCHUsed("") || PCHUsed(". /usr/include/c++/13/bits/stdc++.h\n") {
		t.Error("a header that was not used was recognised")
	}
	if why := PCHNotUsedBecause(notUsed); !strings.Contains(why, "`_GLIBCXX_DEBUG' is defined") {
		t.Errorf("reason %q", why)
	}
}

func TestCheckPCHFiles(t *testing.T) {
	dir := t.TempDir()
	uid := os.Getuid()
	if err := CheckPCHFiles(dir, uid); err == nil || !strings.Contains(err.Error(), "stdc++.h.gch") {
		t.Errorf("an empty directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bits"), 0o755); err != nil {
		t.Fatal(err)
	}
	gch := filepath.Join(dir, "bits", "stdc++.h.gch")
	if err := os.WriteFile(gch, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("owner and mode checks need Unix")
	}
	if err := CheckPCHFiles(dir, uid); err != nil {
		t.Errorf("a good header: %v", err)
	}
	if err := CheckPCHFiles(dir, uid+1); err == nil || !strings.Contains(err.Error(), "owned by") {
		t.Errorf("another owner: %v", err)
	}
	for name, path := range map[string]string{"file": gch, "bits": filepath.Dir(gch), "dir": dir} {
		st, _ := os.Stat(path)
		os.Chmod(path, st.Mode()|0o020)
		if err := CheckPCHFiles(dir, uid); err == nil || !strings.Contains(err.Error(), "writable") {
			t.Errorf("group-writable %s: %v", name, err)
		}
		os.Chmod(path, st.Mode())
	}
}
