package judge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A precompiled header for <bits/stdc++.h> (J5). GCC reads the whole
// standard library for that one include, which is most of a small program's
// compile time. A header precompiled with the compile command's own options
// is loaded instead. It is built by root at install time from the system's
// own headers, lives in PCHDir (not /usr/include, which apt owns), and is
// offered to the compiler as -I PCHDir and as a read-only isolate directory.
//
// GCC uses it only when it is the first include and no macro defined before
// it conflicts with the header, and otherwise reads the real header: the
// verdict of a program never depends on it. If the header is missing or from
// another GCC the compiler ignores it; if the file is damaged GCC stops with
// an error, and Compile then compiles again without it.

// PCHDir is where install and upgrade build the header, owned by root.
const PCHDir = "/usr/local/lib/podium-pch"

const (
	pchFile   = "bits/stdc++.h.gch"
	pchSource = "#include <bits/stdc++.h>\n"
	// pchFaultMarker is what GCC says when it cannot read a header file it
	// has found.
	pchFaultMarker = "while reading precompiled header"
)

// SetPCH points the C++ compile at the precompiled header in dir, or turns
// it off with "". It is safe to call while lanes compile.
func (r *Runner) SetPCH(dir string) { r.pch.Store(&dir) }

// PCH is the directory set by SetPCH, or "".
func (r *Runner) PCH() string {
	if p := r.pch.Load(); p != nil {
		return *p
	}
	return ""
}

// compileCommand is the compile command and isolate directory rules for
// lang: its own, plus the header directory for a language that uses it
// while one is set.
func (r *Runner) compileCommand(lang Language) (args, dirs []string) {
	dir, out := r.PCH(), slices.Index(lang.Compile, "-o")
	if dir == "" || !lang.PCH || out < 0 {
		return lang.Compile, lang.Dirs
	}
	return slices.Insert(slices.Clone(lang.Compile), out, "-I", dir), append(slices.Clone(lang.Dirs), dir+":maybe")
}

// PCHBuildArgs is the g++ command that precompiles header into out: the
// language's compile options, so GCC accepts the result for that compile,
// then the input and output in place of "-o main main.cpp".
func PCHBuildArgs(lang Language, out, header string) []string {
	i := slices.Index(lang.Compile, "-o")
	return append(slices.Clone(lang.Compile[:i]), "-x", "c++-header", header, "-o", out)
}

// BuildPCH precompiles <bits/stdc++.h> into dir. It must run as root: the
// directory and file are root-owned and read-only to everyone else. The
// input is a one-line file written here that includes the system's header;
// nothing else reaches the compiler. The file appears atomically.
func BuildPCH(ctx context.Context, dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "bits"), 0o755); err != nil {
		return err
	}
	for _, d := range []string{dir, filepath.Join(dir, "bits")} {
		if err := os.Chmod(d, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.MkdirTemp(dir, ".build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	header := filepath.Join(tmp, "stdc++.h")
	if err := os.WriteFile(header, []byte(pchSource), 0o644); err != nil {
		return err
	}
	out := filepath.Join(tmp, "stdc++.h.gch")
	args := PCHBuildArgs(Languages["cpp17"], out, header)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if b, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("build the precompiled header: %w: %s", err, tail(string(b), 400))
	}
	if err := os.Chmod(out, 0o644); err != nil {
		return err
	}
	return os.Rename(out, filepath.Join(dir, pchFile))
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// CheckPCHFiles checks that the header exists and that only uid can change
// it: the directory, its bits folder and the file are owned by uid and not
// writable by the group or others. The judge refuses to use a header
// anyone else could replace.
func CheckPCHFiles(dir string, uid int) error {
	gch := filepath.Join(dir, pchFile)
	if _, err := os.Stat(gch); err != nil {
		return fmt.Errorf("precompiled header: %w", err)
	}
	for _, p := range []string{dir, filepath.Join(dir, "bits"), gch} {
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("precompiled header: %w", err)
		}
		if owner, ok := ownerOf(fi); ok && owner != uid {
			return fmt.Errorf("precompiled header: %s is owned by uid %d, not %d", p, owner, uid)
		}
		if sharedWritable(fi) {
			return fmt.Errorf("precompiled header: %s is writable by its group or others", p)
		}
	}
	return nil
}

// PCHUsed reads the output of g++ -H and says whether the precompiled
// header was loaded: -H marks a used one with a line "! path.gch".
func PCHUsed(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "! ") && strings.HasSuffix(strings.TrimSpace(l), "stdc++.h.gch") {
			return true
		}
	}
	return false
}

// PCHNotUsedBecause is the reason -Winvalid-pch gave for ignoring the
// header, or "".
func PCHNotUsedBecause(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if _, why, ok := strings.Cut(l, "not used because "); ok {
			why, _, _ = strings.Cut(why, " [-Winvalid-pch]")
			return why
		}
	}
	return ""
}

// ProbePCH compiles a program that includes <bits/stdc++.h> in box, with
// the exact command and directory rules a student compile gets plus -H and
// -Winvalid-pch, and reports whether the header was used (and if not, why).
// It is the proof that the build options match.
func (r *Runner) ProbePCH(ctx context.Context, box int) (used bool, why string, err error) {
	return r.ProbePCHSource(ctx, box, pchSource+"int main() {}\n")
}

// ProbePCHSource is ProbePCH for a program of the caller's choosing, to see
// whether the header is used for it.
func (r *Runner) ProbePCHSource(ctx context.Context, box int, source string) (used bool, why string, err error) {
	if r.PCH() == "" {
		return false, "", fmt.Errorf("no precompiled header is set")
	}
	lang := Languages["cpp17"]
	args, dirs := r.compileCommand(lang)
	args = slices.Insert(slices.Clone(args), 1, "-Winvalid-pch", "-H")
	r.exclusive.Lock()
	defer r.exclusive.Unlock()
	lim := r.compileLimits(lang)
	release, err := r.reserve(ctx, lim.MemoryKB)
	if err != nil {
		return false, "", err
	}
	defer release()
	dir, err := r.Iso.Init(ctx, box)
	if err != nil {
		return false, "", boxInitFailure(ctx, err)
	}
	defer r.cleanup(ctx, box)
	if err := os.WriteFile(filepath.Join(dir, lang.Source), []byte(source), 0o644); err != nil {
		return false, "", err
	}
	meta, err := r.Iso.Run(ctx, RunRequest{Box: box, Limits: lim, Args: args, Stdout: "compile.txt", StderrToStdout: true,
		Env: sandboxEnv, Dirs: dirs})
	if err != nil {
		return false, "", err
	}
	out, _ := readBoxHead(filepath.Join(dir, "compile.txt"), 1<<20)
	if meta.Status != "" || meta.ExitCode != 0 {
		return false, "", fmt.Errorf("the probe compile failed: %s", tail(string(out), 300))
	}
	return PCHUsed(string(out)), PCHNotUsedBecause(string(out)), nil
}
