package judge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Compile limits apply to every compiler regardless of the problem.
const (
	compileCPU        = 10 * time.Second
	compileWall       = 20 * time.Second
	compileFileSizeKB = 64 * 1024
	// CompileOutputLimit truncates compiler messages stored with a submission.
	CompileOutputLimit = 16 * 1024
	stderrTailLimit    = 4096
	// maxArtifactBytes bounds what a compile may hand to later runs.
	maxArtifactBytes = 64 << 20
	// DefaultMemoryBudgetMB is the total --cg-mem the runner lets boxes hold
	// at once. It stays under the 1100 MB cap on isolate.slice, so one box's
	// usage can never get a different box OOM-killed.
	DefaultMemoryBudgetMB = 1024
)

var errNotRegular = errors.New("not a regular file")

// Runner compiles and executes programs. Every compile and every test gets
// a fresh box: files written by one test cannot reach the next, and the
// control group memory peak belongs to that test alone.
type Runner struct {
	Iso Isolate
	// MemoryBudgetKB caps the sum of memory limits of boxes running at the
	// same time; zero means DefaultMemoryBudgetMB.
	MemoryBudgetKB int

	// exclusive serialises g++ and every JVM invocation across all lanes so
	// that two lanes plus one JVM stay inside the 2 GB budget.
	exclusive sync.Mutex

	memOnce sync.Once
	memMu   sync.Mutex
	memCond *sync.Cond
	memUsed int
}

// reserve blocks until kb of the memory budget is free and returns the
// function that gives it back. A request larger than the whole budget
// waits for the budget to be empty and then takes all of it.
func (r *Runner) reserve(ctx context.Context, kb int) (func(), error) {
	r.memOnce.Do(func() { r.memCond = sync.NewCond(&r.memMu) })
	budget := r.MemoryBudgetKB
	if budget <= 0 {
		budget = DefaultMemoryBudgetMB * 1024
	}
	kb = min(kb, budget)
	stop := context.AfterFunc(ctx, func() {
		r.memMu.Lock()
		r.memCond.Broadcast()
		r.memMu.Unlock()
	})
	defer stop()
	r.memMu.Lock()
	defer r.memMu.Unlock()
	for r.memUsed+kb > budget {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.memCond.Wait()
	}
	r.memUsed += kb
	return func() {
		r.memMu.Lock()
		r.memUsed -= kb
		r.memCond.Broadcast()
		r.memMu.Unlock()
	}, nil
}

// Program is a compiled (or, for interpreted languages, stored) submission
// ready to run: the files in Dir are copied into each test's box.
type Program struct {
	Lang Language
	Dir  string
}

// CompileResult reports a compile step.
type CompileResult struct {
	// Verdict is OK or CompileError.
	Verdict Verdict
	// Output is the compiler's combined output, truncated.
	Output string
	Time   time.Duration
	Wall   time.Duration
	// Transient marks a compile error caused by load (the compiler ran out
	// of time or memory) rather than by the code, so it is not cached.
	Transient bool
}

// Compile writes source into a fresh box, compiles it, and copies the
// resulting regular files into outDir, which must exist and be empty.
func (r *Runner) Compile(ctx context.Context, box int, lang Language, source []byte, outDir string) (Program, CompileResult, error) {
	prog := Program{Lang: lang, Dir: outDir}
	if lang.Compile == nil {
		err := os.WriteFile(filepath.Join(outDir, lang.Source), source, 0o644)
		return prog, CompileResult{Verdict: OK}, err
	}
	if lang.CompileExclusive {
		r.exclusive.Lock()
		defer r.exclusive.Unlock()
	}
	lim := Limits{CPU: compileCPU, Wall: compileWall, MemoryKB: lang.CompileMemoryMB * 1024,
		Processes: lang.CompileProcesses, FileSizeKB: compileFileSizeKB}
	if lang.CompileCPU > 0 {
		lim.CPU, lim.Wall = lang.CompileCPU, 2*lang.CompileCPU
	}
	release, err := r.reserve(ctx, lim.MemoryKB)
	if err != nil {
		return prog, CompileResult{}, err
	}
	defer release()
	dir, err := r.Iso.Init(ctx, box)
	if err != nil {
		return prog, CompileResult{}, err
	}
	defer r.Iso.Cleanup(ctx, box)

	if err := os.WriteFile(filepath.Join(dir, lang.Source), source, 0o644); err != nil {
		return prog, CompileResult{}, err
	}
	meta, err := r.Iso.Run(ctx, RunRequest{
		Box: box, Limits: lim, Args: lang.Compile,
		Stdout: "compile.txt", StderrToStdout: true,
		Env: sandboxEnv, Dirs: lang.Dirs,
	})
	if err != nil {
		return prog, CompileResult{}, err
	}
	out, _ := readBoxHead(filepath.Join(dir, "compile.txt"), CompileOutputLimit)
	res := CompileResult{Verdict: OK, Output: string(out), Time: meta.Time, Wall: meta.WallTime}
	switch {
	case meta.Status == "XX":
		return prog, res, fmt.Errorf("compile box %d: isolate internal error: %s", box, meta.Message)
	case meta.OOMKilled:
		res.Verdict, res.Output, res.Transient = CompileError, appendNote(res.Output, "The compiler ran out of memory."), true
	case meta.Status == "TO":
		res.Verdict, res.Output, res.Transient = CompileError, appendNote(res.Output, fmt.Sprintf("Compilation took longer than %d seconds.", int(lim.CPU.Seconds()))), true
	case meta.Status != "" || meta.ExitCode != 0:
		res.Verdict = CompileError
	}
	if res.Verdict != OK {
		return prog, res, nil
	}
	if err := copyRegularFiles(dir, outDir, map[string]bool{"compile.txt": true}); err != nil {
		return prog, res, fmt.Errorf("collect compile artifacts: %w", err)
	}
	return prog, res, nil
}

// ExecResult reports one run of a program on one input.
type ExecResult struct {
	Verdict Verdict
	Meta    Meta
	// Stderr is the tail of the program's stderr.
	Stderr []byte
}

// LimitsFor converts problem limits into sandbox limits for lang.
func (l Language) LimitsFor(timeLimit time.Duration, memoryMB int) Limits {
	cpu := time.Duration(math.Round(float64(timeLimit) * l.TimeFactor))
	return Limits{CPU: cpu, MemoryKB: (memoryMB + l.MemoryOverheadMB) * 1024, Processes: l.Processes,
		FileSizeKB: DefaultFileSizeKB}
}

// Execute runs prog in a fresh box with inputPath as stdin and copies its
// stdout to outputPath. timeLimit and memoryMB are the problem's limits
// before language adjustment. A missing or non-regular output file (the
// program replaced it with a symlink, FIFO or directory) counts as empty
// output.
func (r *Runner) Execute(ctx context.Context, box int, prog Program, timeLimit time.Duration, memoryMB int, inputPath, outputPath string) (ExecResult, error) {
	lang := prog.Lang
	lim := lang.LimitsFor(timeLimit, memoryMB)
	if lang.RunExclusive {
		r.exclusive.Lock()
		defer r.exclusive.Unlock()
	}
	release, err := r.reserve(ctx, lim.MemoryKB)
	if err != nil {
		return ExecResult{}, err
	}
	defer release()
	dir, err := r.Iso.Init(ctx, box)
	if err != nil {
		return ExecResult{}, err
	}
	defer r.Iso.Cleanup(ctx, box)

	if err := copyRegularFiles(prog.Dir, dir, nil); err != nil {
		return ExecResult{}, err
	}
	if err := copyFile(inputPath, filepath.Join(dir, "input.txt"), 0o644, -1); err != nil {
		return ExecResult{}, err
	}
	meta, err := r.Iso.Run(ctx, RunRequest{
		Box: box, Limits: lim, Args: lang.RunCommand(memoryMB),
		Stdin: "input.txt", Stdout: "output.txt", Stderr: "stderr.txt",
		Env: sandboxEnv, Dirs: lang.Dirs,
	})
	if err != nil {
		return ExecResult{}, err
	}
	tail, _ := readBoxTail(filepath.Join(dir, "stderr.txt"), stderrTailLimit)
	res := ExecResult{Verdict: Classify(meta, lim, lang, tail), Meta: meta, Stderr: tail}

	capBytes := int64(lim.FileSizeKB) * 1024
	n, err := copyBoxOutput(filepath.Join(dir, "output.txt"), outputPath, capBytes)
	if err != nil {
		return res, err
	}
	// Java and Python ignore SIGXFSZ and keep running with truncated
	// output (Java's PrintStream swallows the write error and loops until
	// the time limit), so an output at the cap is the reliable signal.
	if n >= capBytes && (res.Verdict == OK || res.Verdict == RuntimeError || res.Verdict == TimeLimit) {
		res.Verdict = OutputLimit
	}
	return res, nil
}

// copyBoxOutput copies at most capBytes of a box file to dst and returns the
// number of bytes copied. The box file belongs to the sandboxed program, so
// anything wrong with it is the program's output, never a judge failure: a
// missing, non-regular or unreadable source (the program may chmod its own
// stdout to 000) yields an empty dst, and a read that fails part way keeps
// what was read. Otherwise a program could turn a chosen test into a fatal
// job error, a one-bit oracle on hidden data. Only failures writing dst,
// on the judge's side, are errors.
func copyBoxOutput(src, dst string, capBytes int64) (int64, error) {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	in, err := openBoxFile(src)
	if err != nil {
		return 0, out.Close()
	}
	defer in.Close()
	n, err := io.Copy(out, &boxReader{r: io.LimitReader(in, capBytes)})
	if err != nil {
		out.Close()
		return n, err
	}
	return n, out.Close()
}

// boxReader turns a read error on a box file into the end of the file, so
// io.Copy reports only errors writing the destination.
type boxReader struct{ r io.Reader }

func (b *boxReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && err != io.EOF {
		return n, io.EOF
	}
	return n, err
}

// copyRegularFiles copies the regular files directly inside src into dst.
// Symlinks, devices and subdirectories are ignored: a sandboxed process
// must never be able to make the judge read or write outside the box.
func copyRegularFiles(src, dst string, skip map[string]bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range entries {
		if skip[e.Name()] || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maxArtifactBytes {
			return fmt.Errorf("artifacts exceed %d MB", maxArtifactBytes>>20)
		}
		err = copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), info.Mode().Perm()&0o755, maxArtifactBytes)
		if errors.Is(err, errNotRegular) {
			continue // replaced between ReadDir and open
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src to dst through openBoxFile; limit < 0 means no limit.
func copyFile(src, dst string, perm os.FileMode, limit int64) error {
	in, err := openBoxFile(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	var r io.Reader = in
	if limit >= 0 {
		r = io.LimitReader(in, limit)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func readBoxHead(path string, n int64) ([]byte, error) {
	f, err := openBoxFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

func readBoxTail(path string, n int64) ([]byte, error) {
	f, err := openBoxFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > n {
		if _, err := f.Seek(-n, io.SeekEnd); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(f, n))
}

func appendNote(output, note string) string {
	if output == "" {
		return note
	}
	return strings.TrimRight(output, "\n") + "\n" + note
}
