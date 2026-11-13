package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	serverGroup  = "system.slice/podium-server.service"
	judgeGroup   = "system.slice/podium-judge.service"
	sandboxGroup = "isolate.slice"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) lines() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(s.b.String()), "\n") {
		if l == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			panic(err)
		}
		out = append(out, m)
	}
	return out
}

// fakeTree is a control group tree and a /proc/stat in a temp folder, with
// a reader that records every path it is asked to open.
type fakeTree struct {
	t      testing.TB
	root   string
	proc   string
	mu     sync.Mutex
	opened map[string]bool
	reads  int
	logBuf *syncBuf
	log    *slog.Logger
}

func newFakeTree(t testing.TB) *fakeTree {
	t.Helper()
	dir := t.TempDir()
	f := &fakeTree{t: t, root: filepath.Join(dir, "cgroup"), proc: filepath.Join(dir, "stat"), opened: map[string]bool{}, logBuf: &syncBuf{}}
	f.log = slog.New(slog.NewJSONHandler(f.logBuf, nil))
	return f
}

func (f *fakeTree) put(group, file, content string) {
	f.t.Helper()
	p := filepath.Join(f.root, filepath.FromSlash(group), file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeTree) cpu(group string, usec int64) {
	f.t.Helper()
	f.put(group, "cpu.stat", fmt.Sprintf("usage_usec %d\nuser_usec 1\nsystem_usec 2\nnr_periods 0\n", usec))
}

// stat writes a /proc/stat whose first line has total jiffies and steal.
func (f *fakeTree) stat(total, steal int64) {
	f.t.Helper()
	// user nice system idle iowait irq softirq steal guest guest_nice
	body := fmt.Sprintf("cpu  0 0 0 %d 0 0 0 %d 0 0\ncpu0 1 2 3 4 5 6 7 8\nintr 1\n", total-steal, steal)
	if err := os.WriteFile(f.proc, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeTree) sampler() *sampler {
	s := newSampler(f.log, f.root, f.proc)
	s.read = func(path string, buf []byte) (int, error) {
		f.mu.Lock()
		f.opened[path] = true
		f.reads++
		f.mu.Unlock()
		return readSmallFile(path, buf)
	}
	return s
}

func (f *fakeTree) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for p := range f.opened {
		rel, _ := filepath.Rel(filepath.Dir(f.root), p)
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

func fillAll(f *fakeTree, serverUs, judgeUs, sandboxUs int64) {
	f.cpu(serverGroup, serverUs)
	f.cpu(judgeGroup, judgeUs)
	f.cpu(sandboxGroup, sandboxUs)
	f.put(serverGroup, "memory.peak", "104857600\n")  // 100 MiB
	f.put(judgeGroup, "memory.peak", "52428800\n")    // 50 MiB
	f.put(sandboxGroup, "memory.peak", "314572800\n") // 300 MiB
}

func TestSamplerLineHasItsFields(t *testing.T) {
	f := newFakeTree(t)
	fillAll(f, 1_000_000, 2_000_000, 0)
	f.stat(1000, 0)
	s := f.sampler()
	t0 := time.Unix(1000, 0)
	s.tick(t0)
	if n := len(f.logBuf.lines()); n != 0 {
		t.Fatalf("first tick logged %d lines, want 0 (it only sets the baseline)", n)
	}
	// 10 s later: the server used 5 s of CPU, the judge 10 s, the sandbox
	// 2.5 s; 2 of 400 jiffies were stolen.
	fillAll(f, 6_000_000, 12_000_000, 2_500_000)
	f.stat(1400, 2)
	s.tick(t0.Add(10 * time.Second))
	lines := f.logBuf.lines()
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	l := lines[0]
	want := map[string]float64{
		"interval_s": 10, "server_cpu_pct": 50, "judge_cpu_pct": 100, "sandbox_cpu_pct": 25,
		"server_mem_peak_mb": 100, "judge_mem_peak_mb": 50, "sandbox_mem_peak_mb": 300, "steal_pct": 0.5,
	}
	if l["msg"] != "cpu sample" {
		t.Errorf("msg = %v", l["msg"])
	}
	for k, v := range want {
		if got, ok := l[k].(float64); !ok || got != v {
			t.Errorf("%s = %v, want %v", k, l[k], v)
		}
	}
	if len(l) != len(want)+3 { // time, level, msg
		t.Errorf("line has %d fields: %v", len(l), l)
	}
}

func TestSamplerReadsOnlyTheThreeGroups(t *testing.T) {
	f := newFakeTree(t)
	fillAll(f, 1, 1, 1)
	f.stat(10, 0)
	// Files that must never be opened.
	f.put(serverGroup, "memory.current", "1")
	f.put("system.slice/other.service", "cpu.stat", "usage_usec 1\n")
	f.put("", "cpu.stat", "usage_usec 1\n")
	s := f.sampler()
	s.tick(time.Unix(0, 0))
	s.tick(time.Unix(10, 0))
	want := []string{
		"cgroup/isolate.slice/cpu.stat",
		"cgroup/isolate.slice/memory.peak",
		"cgroup/system.slice/podium-judge.service/cpu.stat",
		"cgroup/system.slice/podium-judge.service/memory.peak",
		"cgroup/system.slice/podium-server.service/cpu.stat",
		"cgroup/system.slice/podium-server.service/memory.peak",
		"stat",
	}
	if got := f.paths(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("opened:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Without memory.peak (kernels before 6.5) the largest memory.current seen
// is reported.
func TestSamplerFallsBackToMemoryCurrent(t *testing.T) {
	f := newFakeTree(t)
	fillAll(f, 0, 0, 0)
	for _, g := range []string{serverGroup, judgeGroup, sandboxGroup} {
		os.Remove(filepath.Join(f.root, filepath.FromSlash(g), "memory.peak"))
		f.put(g, "memory.current", "10485760\n") // 10 MiB
	}
	f.put(serverGroup, "memory.current", "209715200\n") // 200 MiB
	f.stat(10, 0)
	s := f.sampler()
	s.tick(time.Unix(0, 0))
	f.put(serverGroup, "memory.current", "52428800\n") // fell to 50 MiB
	s.tick(time.Unix(10, 0))
	lines := f.logBuf.lines()
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	l := lines[0]
	if l["server_mem_peak_mb"] != float64(200) || l["judge_mem_peak_mb"] != float64(10) {
		t.Errorf("peaks = %v / %v, want 200 / 10", l["server_mem_peak_mb"], l["judge_mem_peak_mb"])
	}
}

func TestSamplerSurvivesBadReadings(t *testing.T) {
	f := newFakeTree(t)
	fillAll(f, 5_000_000, 5_000_000, 5_000_000)
	f.stat(1000, 0)
	s := f.sampler()
	s.tick(time.Unix(0, 0))
	f.cpu(serverGroup, 1_000_000)                        // counter went backwards
	os.RemoveAll(filepath.Join(f.root, "isolate.slice")) // group vanished
	f.put(judgeGroup, "cpu.stat", "garbage\n")           // unreadable
	f.stat(1400, 0)
	s.tick(time.Unix(10, 0))
	lines := f.logBuf.lines()
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	l := lines[0]
	for _, k := range []string{"server_cpu_pct", "judge_cpu_pct", "sandbox_cpu_pct", "sandbox_mem_peak_mb"} {
		if l[k] != float64(-1) {
			t.Errorf("%s = %v, want -1", k, l[k])
		}
	}
	if l["server_mem_peak_mb"] != float64(100) {
		t.Errorf("server_mem_peak_mb = %v, want 100", l["server_mem_peak_mb"])
	}
	// The next good tick measures from the last good reading.
	f.cpu(serverGroup, 3_000_000)
	f.stat(1800, 0)
	s.tick(time.Unix(20, 0))
	lines = f.logBuf.lines()
	if len(lines) != 2 || lines[1]["server_cpu_pct"] != float64(20) {
		t.Errorf("after recovery: %v, want server_cpu_pct 20", lines)
	}
}

func goroutinesSettle(base int) int {
	n := runtime.NumGoroutine()
	for i := 0; i < 100 && n > base; i++ {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

func TestSamplerIsOffWithoutTheFiles(t *testing.T) {
	f := newFakeTree(t) // nothing created
	base := runtime.NumGoroutine()
	stop := startSampler(f.sampler(), 5*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	stop()
	stop() // safe twice
	lines := f.logBuf.lines()
	if len(lines) != 1 || !strings.Contains(fmt.Sprint(lines[0]["msg"]), "sampling is off") {
		t.Fatalf("lines = %v, want one 'sampling is off' line", lines)
	}
	if n := goroutinesSettle(base); n > base {
		t.Errorf("%d goroutines, started with %d", n, base)
	}
	if f.reads > 7 { // the one probe: three cpu.stat, three memory files and /proc/stat
		t.Errorf("an off sampler kept reading: %d reads", f.reads)
	}
}

func TestSamplerStopsOnShutdown(t *testing.T) {
	f := newFakeTree(t)
	fillAll(f, 1, 1, 1)
	f.stat(10, 0)
	base := runtime.NumGoroutine()
	stop := startSampler(f.sampler(), 5*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for len(f.logBuf.lines()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(f.logBuf.lines()) < 2 {
		t.Fatal("a running sampler logged nothing")
	}
	stop()
	n := len(f.logBuf.lines())
	time.Sleep(50 * time.Millisecond)
	if after := len(f.logBuf.lines()); after != n {
		t.Errorf("logged %d more lines after stop", after-n)
	}
	if g := goroutinesSettle(base); g > base {
		t.Errorf("%d goroutines after stop, started with %d", g, base)
	}
}

// BenchmarkSamplerTick is what one tick costs: three cpu.stat reads, three
// memory.peak reads and /proc/stat, on a fake tree in a temp folder.
func BenchmarkSamplerTick(b *testing.B) {
	f := newFakeTree(b)
	fillAll(f, 123456789, 123456789, 123456789)
	f.stat(100000, 10)
	s := newSampler(slog.New(slog.NewJSONHandler(io.Discard, nil)), f.root, f.proc)
	now := time.Unix(0, 0)
	s.tick(now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(10 * time.Second)
		s.tick(now)
	}
}
