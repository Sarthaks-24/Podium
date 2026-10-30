// Package judge compiles and runs untrusted programs inside isolate boxes,
// classifies the outcome into a verdict, and compares output with the
// expected answer. It is the security boundary of podium: nothing in here
// runs student code outside isolate, passes it to a shell, or touches the
// database. See docs/ARCHITECTURE.md, section "Judge".
package judge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Isolate drives the isolate binary in control-group mode.
type Isolate struct {
	// Path is the setuid isolate binary.
	Path string
}

// Limits bound one sandboxed run.
type Limits struct {
	// CPU is the CPU time limit; the program is killed after CPU+ExtraTime.
	CPU time.Duration
	// Wall guards against sleeping programs. Zero means 2*CPU + 1 s.
	Wall time.Duration
	// MemoryKB caps the whole control group (all processes and threads).
	MemoryKB int
	// Processes caps processes plus threads.
	Processes int
	// FileSizeKB caps every file the program writes, including stdout.
	FileSizeKB int
}

// DefaultFileSizeKB is the 16 MB cap on what a program may write.
const DefaultFileSizeKB = 16 * 1024

// extraTime lets isolate measure how far past the limit a program ran.
const extraTime = 500 * time.Millisecond

// RunRequest is one isolate --run invocation inside an initialised box.
type RunRequest struct {
	Box    int
	Limits Limits
	// Args is the command and its arguments, executed directly (no shell).
	Args []string
	// Stdin, Stdout and Stderr are file names inside the box; empty leaves
	// the stream unset. StderrToStdout merges stderr into Stdout.
	Stdin, Stdout, Stderr string
	StderrToStdout        bool
	// Env holds KEY=VALUE pairs; nothing else is inherited.
	Env []string
	// Dirs are extra --dir rules, for example "/etc/java-21-openjdk:maybe".
	Dirs []string
}

// Meta is the parsed isolate meta file.
type Meta struct {
	Time      time.Duration
	WallTime  time.Duration
	MaxRSSKB  int
	CgMemKB   int
	ExitCode  int
	ExitSig   int
	Killed    bool
	OOMKilled bool
	// Status is "", "RE", "SG", "TO" or "XX".
	Status  string
	Message string
}

// MemoryKB is the best available peak memory figure for one run in a fresh
// box: the control group peak when reported, otherwise the process RSS.
func (m Meta) MemoryKB() int {
	if m.CgMemKB > m.MaxRSSKB {
		return m.CgMemKB
	}
	return m.MaxRSSKB
}

// ErrBoxBusy is isolate refusing --init because another isolate process
// holds the box's lock right now. Init waits and tries again a few times
// before it returns this.
var ErrBoxBusy = errors.New("the isolate box is in use by another process")

// ErrBoxClaimed is Init on a box this judge already holds: a lane bug,
// refused rather than re-initialising a box a run still uses.
var ErrBoxClaimed = errors.New("the isolate box is already in use by this judge")

// boxBusyText is what isolate 2.x prints when it cannot take a box's lock.
const boxBusyText = "currently in use by another process"

// initBackoff is the wait before each retry of a busy --init: five tries
// over 3.75 s, enough for another process's isolate call (a run, an init
// or a cleanup) to finish, short against a job's lease.
var initBackoff = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

// claimed is the set of boxes this process holds between Init and
// Cleanup, keyed by isolate binary and box id.
var claimed = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

func boxKey(path string, box int) string { return path + "#" + strconv.Itoa(box) }

// Init creates box id and returns the host path of its /box directory.
//
// isolate's box lock is held only while an isolate process runs, so
// "in use by another process" means a call on that box is running right
// now. Init then waits (initBackoff) and tries again, and after the last
// try returns ErrBoxBusy, which fails the job as a judge error. It never
// cleans up a box it could not initialise. isolate itself cannot tell a
// box another process initialised and left idle between its calls from a
// free one, so box ids must belong to one judge per host (lane n is box
// n); within this process Init refuses a box that is already claimed.
func (iso Isolate) Init(ctx context.Context, box int) (string, error) {
	key := boxKey(iso.Path, box)
	claimed.Lock()
	if claimed.m[key] {
		claimed.Unlock()
		return "", fmt.Errorf("isolate init box %d: %w", box, ErrBoxClaimed)
	}
	claimed.m[key] = true
	claimed.Unlock()
	dir, err := iso.initBox(ctx, box)
	if err != nil {
		claimed.Lock()
		delete(claimed.m, key)
		claimed.Unlock()
	}
	return dir, err
}

func (iso Isolate) initBox(ctx context.Context, box int) (string, error) {
	for try := 0; ; try++ {
		out, err := iso.cmd(ctx, "--cg", "--box-id="+strconv.Itoa(box), "--init")
		if err == nil {
			root := strings.TrimSpace(string(out))
			if root == "" || !filepath.IsAbs(root) {
				return "", fmt.Errorf("isolate init box %d: unexpected output %q", box, root)
			}
			return filepath.Join(root, "box"), nil
		}
		if !strings.Contains(err.Error(), boxBusyText) || ctx.Err() != nil {
			return "", fmt.Errorf("isolate init box %d: %w", box, err)
		}
		if try == len(initBackoff) {
			return "", fmt.Errorf("isolate init box %d after %d tries: %w; is another judge or test suite using the same box ids on this host?", box, try+1, ErrBoxBusy)
		}
		t := time.NewTimer(initBackoff[try])
		select {
		case <-ctx.Done():
			t.Stop()
			return "", fmt.Errorf("isolate init box %d: %w", box, ctx.Err())
		case <-t.C:
		}
	}
}

// Cleanup destroys box id and everything in it, and gives up this
// process's claim on it.
func (iso Isolate) Cleanup(ctx context.Context, box int) error {
	defer func() {
		claimed.Lock()
		delete(claimed.m, boxKey(iso.Path, box))
		claimed.Unlock()
	}()
	// Cleanup must run even when the run's context was cancelled.
	ctx = context.WithoutCancel(ctx)
	if _, err := iso.cmd(ctx, "--cg", "--box-id="+strconv.Itoa(box), "--cleanup"); err != nil {
		return fmt.Errorf("isolate cleanup box %d: %w", box, err)
	}
	return nil
}

// Run executes req and returns the parsed meta file. A program that fails
// (non-zero exit, signal, timeout) is not an error; only a failure of
// isolate itself is.
func (iso Isolate) Run(ctx context.Context, req RunRequest) (Meta, error) {
	mf, err := os.CreateTemp("", "podium-meta-*")
	if err != nil {
		return Meta{}, err
	}
	metaPath := mf.Name()
	mf.Close()
	defer os.Remove(metaPath)

	cmd := iso.command(ctx, RunArgs(req, metaPath)...)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}
	runErr := cmd.Run()
	// isolate exits 0 when the program succeeded and 1 when it failed;
	// anything else is an internal error of the sandbox.
	var exitErr *exec.ExitError
	if runErr != nil && !(errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1) {
		return Meta{}, fmt.Errorf("isolate run box %d: %w: %s", req.Box, runErr, strings.TrimSpace(stderr.String()))
	}
	f, err := os.Open(metaPath)
	if err != nil {
		return Meta{}, fmt.Errorf("isolate run box %d: read meta: %w", req.Box, err)
	}
	defer f.Close()
	return ParseMeta(f)
}

// RunArgs builds the isolate argument list for req. It is separate from Run
// so the exact flags can be unit-tested on any platform.
func RunArgs(req RunRequest, metaPath string) []string {
	l := req.Limits
	wall := l.Wall
	if wall == 0 {
		wall = 2*l.CPU + time.Second
	}
	fsize := l.FileSizeKB
	if fsize == 0 {
		fsize = DefaultFileSizeKB
	}
	procs := l.Processes
	if procs < 1 {
		procs = 1
	}
	a := []string{
		"--cg", "--silent",
		"--box-id=" + strconv.Itoa(req.Box),
		"--time=" + seconds(l.CPU),
		"--wall-time=" + seconds(wall),
		"--extra-time=" + seconds(extraTime),
		"--cg-mem=" + strconv.Itoa(l.MemoryKB),
		"--fsize=" + strconv.Itoa(fsize),
		"--processes=" + strconv.Itoa(procs),
		"--meta=" + metaPath,
	}
	if req.Stdin != "" {
		a = append(a, "--stdin="+req.Stdin)
	}
	if req.Stdout != "" {
		a = append(a, "--stdout="+req.Stdout)
	}
	if req.StderrToStdout {
		a = append(a, "--stderr-to-stdout")
	} else if req.Stderr != "" {
		a = append(a, "--stderr="+req.Stderr)
	}
	for _, d := range req.Dirs {
		a = append(a, "--dir="+d)
	}
	for _, e := range req.Env {
		a = append(a, "--env="+e)
	}
	a = append(a, "--run", "--")
	return append(a, req.Args...)
}

func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}

// ParseMeta reads an isolate meta file (key:value lines).
func ParseMeta(r io.Reader) (Meta, error) {
	var m Meta
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		var err error
		switch key {
		case "time":
			m.Time, err = parseSeconds(val)
		case "time-wall":
			m.WallTime, err = parseSeconds(val)
		case "max-rss":
			m.MaxRSSKB, err = strconv.Atoi(val)
		case "cg-mem":
			m.CgMemKB, err = strconv.Atoi(val)
		case "exitcode":
			m.ExitCode, err = strconv.Atoi(val)
		case "exitsig":
			m.ExitSig, err = strconv.Atoi(val)
		case "killed":
			m.Killed = true
		case "cg-oom-killed":
			m.OOMKilled = true
		case "status":
			m.Status = val
		case "message":
			m.Message = val
		}
		if err != nil {
			return m, fmt.Errorf("meta %s: %w", key, err)
		}
	}
	return m, sc.Err()
}

func parseSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(f * float64(time.Second)), nil
}

// command builds an isolate invocation. On cancellation isolate gets
// SIGTERM first, so it can kill the sandboxed processes and tear down the
// control group itself; SIGKILL follows only if it has not exited after
// cancelGrace. Killing isolate outright would orphan the box.
func (iso Isolate) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, iso.Path, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = cancelGrace
	return cmd
}

// cancelGrace is how long isolate has to clean up after SIGTERM.
const cancelGrace = 5 * time.Second

func (iso Isolate) cmd(ctx context.Context, args ...string) ([]byte, error) {
	cmd := iso.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// limitedWriter keeps the first n bytes and discards the rest.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
