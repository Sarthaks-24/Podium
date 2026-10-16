// Package calib measures a judge host: it runs hostile programs to prove the
// sandbox contains them, and times each language on hello world and a
// one-million-element sort. `podium-judge calibrate` prints the result as
// the Markdown tables kept in the calibration record, and the sandbox test
// suite asserts the same hostile verdicts.
package calib

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

//go:embed programs/*
var programs embed.FS

// Case is one hostile program and the verdicts that count as contained.
type Case struct {
	Name     string
	Lang     string
	File     string
	Want     []judge.Verdict
	Time     time.Duration
	MemoryMB int
	// Forbidden is output that proves the sandbox leaked.
	Forbidden string
}

// Hostile lists the PLAN.md section 7.6 programs plus a few extras.
var Hostile = []Case{
	{Name: "fork bomb (C++)", Lang: "cpp17", File: "fork_bomb.cpp", Want: []judge.Verdict{judge.RuntimeError, judge.TimeLimit}},
	{Name: "malloc 2 GB and touch pages (C++)", Lang: "cpp17", File: "malloc_2g.cpp", Want: []judge.Verdict{judge.MemoryLimit}},
	{Name: "infinite loop (C++)", Lang: "cpp17", File: "infinite_loop.cpp", Want: []judge.Verdict{judge.TimeLimit}},
	{Name: "sleep forever (C++)", Lang: "cpp17", File: "sleep_forever.cpp", Want: []judge.Verdict{judge.TimeLimit}},
	{Name: "infinite output (C++)", Lang: "cpp17", File: "infinite_print.cpp", Want: []judge.Verdict{judge.OutputLimit}},
	{Name: "open /etc/shadow (C++)", Lang: "cpp17", File: "read_shadow.cpp", Want: []judge.Verdict{judge.RuntimeError}, Forbidden: "OPENED"},
	{Name: "TCP connect to 1.1.1.1 (C++)", Lang: "cpp17", File: "socket_connect.cpp", Want: []judge.Verdict{judge.RuntimeError}, Forbidden: "CONNECTED"},
	{Name: "#include /dev/urandom (compile)", Lang: "cpp17", File: "include_urandom.cpp", Want: []judge.Verdict{judge.CompileError}},
	{Name: "new long[1<<28] (Java)", Lang: "java21", File: "java_oom.java", Want: []judge.Verdict{judge.MemoryLimit}},
	{Name: "unbounded recursion (Python)", Lang: "python3", File: "py_recursion.py", Want: []judge.Verdict{judge.RuntimeError, judge.MemoryLimit}},
	{Name: "100 worker threads (Node)", Lang: "node22", File: "node_threads.js", Want: []judge.Verdict{judge.RuntimeError, judge.OK}},
	// The judge's own environment always contains PATH=; the sandbox's
	// output here is empty, so seeing it means the judge followed a link.
	{Name: "output symlinked to /proc/self/environ (C++)", Lang: "cpp17", File: "symlink_output.cpp", Want: []judge.Verdict{judge.OK}, Forbidden: "PATH="},
	{Name: "output replaced by a FIFO (C++)", Lang: "cpp17", File: "fifo_output.cpp", Want: []judge.Verdict{judge.OK}},
	{Name: "endless output (Java)", Lang: "java21", File: "java_print.java", Want: []judge.Verdict{judge.OutputLimit}},
	{Name: "endless output (Python)", Lang: "python3", File: "py_print.py", Want: []judge.Verdict{judge.OutputLimit}},
	{Name: "fill /tmp with 16 MB files (C++)", Lang: "cpp17", File: "disk_fill.cpp", Want: []judge.Verdict{judge.MemoryLimit, judge.RuntimeError}, Time: 5 * time.Second},
}

// Outcome is the observed result of one hostile case.
type Outcome struct {
	Case   Case
	Got    judge.Verdict
	Meta   judge.Meta
	Detail string
	Wall   time.Duration
}

// Contained reports whether the outcome is one of the accepted verdicts and
// nothing forbidden reached the output.
func (o Outcome) Contained() bool {
	return slices.Contains(o.Case.Want, o.Got) && !strings.Contains(o.Detail, "LEAK")
}

// Source returns an embedded program.
func Source(file string) []byte {
	b, err := programs.ReadFile("programs/" + file)
	if err != nil {
		panic(err)
	}
	return b
}

// RunCase compiles and runs one hostile case in box, using work as scratch.
func RunCase(ctx context.Context, r *judge.Runner, box int, work string, c Case) (Outcome, error) {
	lang, ok := judge.Languages[c.Lang]
	if !ok {
		return Outcome{}, fmt.Errorf("unknown language %s", c.Lang)
	}
	tl, mem := c.Time, c.MemoryMB
	if tl == 0 {
		tl = time.Second
	}
	if mem == 0 {
		mem = 256
	}
	dir, err := os.MkdirTemp(work, "case-*")
	if err != nil {
		return Outcome{}, err
	}
	defer os.RemoveAll(dir)
	start := time.Now()
	prog, cr, err := r.Compile(ctx, box, lang, Source(c.File), mustMkdir(dir, "prog"))
	if err != nil {
		return Outcome{}, err
	}
	o := Outcome{Case: c}
	if cr.Verdict != judge.OK {
		o.Got, o.Detail, o.Wall = cr.Verdict, firstLine(cr.Output), time.Since(start)
		return o, nil
	}
	in, out := filepath.Join(dir, "in.txt"), filepath.Join(dir, "out.txt")
	if err := os.WriteFile(in, nil, 0o644); err != nil {
		return o, err
	}
	res, err := r.Execute(ctx, box, prog, tl, mem, in, out)
	if err != nil {
		return o, err
	}
	o.Got, o.Meta, o.Wall = res.Verdict, res.Meta, time.Since(start)
	o.Detail = metaSummary(res.Meta)
	if c.Forbidden != "" {
		b, _ := os.ReadFile(out)
		if bytes.Contains(b, []byte(c.Forbidden)) || bytes.Contains(res.Stderr, []byte(c.Forbidden)) {
			o.Detail = "LEAK: judge saw " + c.Forbidden
		}
	}
	return o, nil
}

// Timing is the median measurement for one language.
type Timing struct {
	Lang      judge.Language
	Compile   time.Duration // median compile wall time (zero when not compiled)
	HelloRun  time.Duration // median CPU time of hello world inside the box
	HelloStep time.Duration // median wall time of a whole Execute step (box init, copy, run, cleanup)
	SortRun   time.Duration // median CPU time of the 1e6 sort
	SortRSSMB float64
	Output    string // sort output, identical across languages when correct
}

// RunTiming measures every language reps times and returns medians.
func RunTiming(ctx context.Context, r *judge.Runner, box int, work string, reps int) ([]Timing, error) {
	input := filepath.Join(work, "sort-input.txt")
	if err := writeSortInput(input, 1_000_000); err != nil {
		return nil, err
	}
	ext := map[string]string{"cpp17": "cpp", "java21": "java", "python3": "py", "node22": "js"}
	var out []Timing
	for _, id := range []string{"cpp17", "java21", "python3", "node22"} {
		lang := judge.Languages[id]
		t := Timing{Lang: lang}
		var compiles, hellos, steps, sorts []time.Duration
		var rss []float64
		for i := 0; i < reps; i++ {
			dir, err := os.MkdirTemp(work, "t-*")
			if err != nil {
				return nil, err
			}
			hello, cr, err := r.Compile(ctx, box, lang, Source("hello."+ext[id]), mustMkdir(dir, "hello"))
			if err != nil || cr.Verdict != judge.OK {
				os.RemoveAll(dir)
				return nil, fmt.Errorf("%s hello compile: %v %s", id, err, cr.Output)
			}
			if lang.Compile != nil {
				compiles = append(compiles, cr.Wall)
			}
			start := time.Now()
			hr, err := r.Execute(ctx, box, hello, time.Second, 256, input, filepath.Join(dir, "hello.out"))
			if err != nil || hr.Verdict != judge.OK {
				os.RemoveAll(dir)
				return nil, fmt.Errorf("%s hello run: %v %s %s", id, err, hr.Verdict, hr.Stderr)
			}
			steps = append(steps, time.Since(start))
			hellos = append(hellos, hr.Meta.Time)

			srt, cr, err := r.Compile(ctx, box, lang, Source("sort."+ext[id]), mustMkdir(dir, "sort"))
			if err != nil || cr.Verdict != judge.OK {
				os.RemoveAll(dir)
				return nil, fmt.Errorf("%s sort compile: %v %s", id, err, cr.Output)
			}
			sr, err := r.Execute(ctx, box, srt, 10*time.Second, 256, input, filepath.Join(dir, "sort.out"))
			if err != nil || sr.Verdict != judge.OK {
				os.RemoveAll(dir)
				return nil, fmt.Errorf("%s sort run: %v %s %s", id, err, sr.Verdict, sr.Stderr)
			}
			sorts = append(sorts, sr.Meta.Time)
			rss = append(rss, float64(sr.Meta.MemoryKB())/1024)
			b, _ := os.ReadFile(filepath.Join(dir, "sort.out"))
			t.Output = strings.TrimSpace(string(b))
			os.RemoveAll(dir)
		}
		t.Compile, t.HelloRun, t.HelloStep, t.SortRun = median(compiles), median(hellos), median(steps), median(sorts)
		sort.Float64s(rss)
		t.SortRSSMB = rss[len(rss)/2]
		out = append(out, t)
	}
	return out, nil
}

// WriteMarkdown renders the results in the layout of the calibration record.
func WriteMarkdown(w io.Writer, host string, hostile []Outcome, timing []Timing, reps int) {
	fmt.Fprintf(w, "Host: %s\n\n", host)
	fmt.Fprintln(w, "## Sandbox hostile-program checks")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Program | Expected | Observed | Contained | Notes |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for _, o := range hostile {
		want := make([]string, len(o.Case.Want))
		for i, v := range o.Case.Want {
			want[i] = string(v)
		}
		ok := "yes"
		if !o.Contained() {
			ok = "**NO**"
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", o.Case.Name, strings.Join(want, " or "), o.Got, ok, o.Detail)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Timing per language, median of %d\n\n", reps)
	fmt.Fprintln(w, "| Language | Compile wall (ms) | Hello CPU (ms) | Hello whole step (ms) | Sort 1e6 CPU (ms) | Sort peak memory (MB) | Sort output |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
	for _, t := range timing {
		compile := "none"
		if t.Lang.Compile != nil {
			compile = ms(t.Compile)
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %.0f | `%s` |\n", t.Lang.Name, compile, ms(t.HelloRun), ms(t.HelloStep), ms(t.SortRun), t.SortRSSMB, t.Output)
	}
}

func writeSortInput(path string, n int) error {
	var b strings.Builder
	b.Grow(n * 11)
	fmt.Fprintf(&b, "%d\n", n)
	x := uint32(2463534242)
	for i := 0; i < n; i++ {
		// xorshift32: deterministic, so every host sorts the same data.
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		fmt.Fprintf(&b, "%d ", int32(x>>1)-(1<<29))
	}
	b.WriteByte('\n')
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return s[len(s)/2]
}

func ms(d time.Duration) string { return fmt.Sprintf("%d", d.Milliseconds()) }

func metaSummary(m judge.Meta) string {
	parts := []string{fmt.Sprintf("cpu %d ms, wall %d ms, mem %d MB", m.Time.Milliseconds(), m.WallTime.Milliseconds(), m.MemoryKB()/1024)}
	if m.ExitSig != 0 {
		parts = append(parts, fmt.Sprintf("signal %d", m.ExitSig))
	} else if m.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit %d", m.ExitCode))
	}
	if m.OOMKilled {
		parts = append(parts, "oom-killed")
	}
	return strings.Join(parts, ", ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "|", "/")
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

func mustMkdir(parent, name string) string {
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		panic(err)
	}
	return p
}
