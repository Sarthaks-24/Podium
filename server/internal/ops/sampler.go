package ops

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// The CPU sampler answers, from a real contest, how the two vCPUs are split
// between the web server, the judge and the sandboxes. Every 10 s it logs
// one "cpu sample" line with the CPU each of the three groups used since the
// last line and the memory peak of each, read from the control group v2
// files systemd keeps for them, plus the share of CPU time the hypervisor
// took away (steal). It reads those seven files and nothing else, and does
// not run where they do not exist (a laptop, Windows, a container).

// SampleEvery is how often the sampler logs.
const SampleEvery = 10 * time.Second

// cgroupRoot and procStat are where Linux keeps the files. The unit and slice
// names are the ones deploy/install.sh installs: podium-server.service,
// podium-judge.service and isolate.slice, which holds every sandbox box.
const (
	cgroupRoot = "/sys/fs/cgroup"
	procStat   = "/proc/stat"
)

// group is one control group and the last reading of it.
type group struct {
	name              string
	cpuPath, peakPath string
	curPath           string
	noPeak            bool    // memory.peak is absent (kernels before 6.5): use memory.current
	maxCur            int64   // largest memory.current seen, when noPeak
	prevUsec          int64   // cpu.stat usage_usec at the last good reading, -1 before one
	cpuPct            float64 // this tick's result, -1 when unreadable
	peakMB            int64   // this tick's result, -1 when unreadable
	cpuReadable       bool
}

// sampler reads the three groups and /proc/stat. A sampler is used by one
// goroutine at a time.
type sampler struct {
	log    *slog.Logger
	root   string
	proc   string
	read   func(path string, buf []byte) (int, error)
	groups []*group
	buf    [512]byte

	primed               bool
	prevAt               time.Time
	prevTotal, prevSteal int64 // /proc/stat at the last good reading, -1 before one
}

func newSampler(log *slog.Logger, root, proc string) *sampler {
	s := &sampler{log: log, root: root, proc: proc, read: readSmallFile, prevTotal: -1, prevSteal: -1}
	for _, g := range []struct{ name, dir string }{
		{"server", "system.slice/podium-server.service"},
		{"judge", "system.slice/podium-judge.service"},
		{"sandbox", "isolate.slice"},
	} {
		dir := filepath.Join(root, filepath.FromSlash(g.dir))
		s.groups = append(s.groups, &group{name: g.name, prevUsec: -1,
			cpuPath: filepath.Join(dir, "cpu.stat"), peakPath: filepath.Join(dir, "memory.peak"), curPath: filepath.Join(dir, "memory.current")})
	}
	return s
}

// readSmallFile reads the start of a small file into buf.
func readSmallFile(path string, buf []byte) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := f.Read(buf)
	if err == io.EOF {
		err = nil
	}
	return n, err
}

// number reads one whole-number file such as memory.peak.
func (s *sampler) number(path string) (int64, error) {
	n, err := s.read(path, s.buf[:])
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(string(bytes.TrimSpace(s.buf[:n])), 10, 64)
	if err == nil && v < 0 {
		err = strconv.ErrRange
	}
	return v, err
}

// field finds the whole number after key at the start of a line of the
// bytes in b (the cpu.stat format, "usage_usec 123").
func field(b []byte, key string) (int64, bool) {
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		if len(line) > len(key) && string(line[:len(key)]) == key && line[len(key)] == ' ' {
			v, err := strconv.ParseInt(string(bytes.TrimSpace(line[len(key)+1:])), 10, 64)
			return v, err == nil && v >= 0
		}
	}
	return 0, false
}

// cpuTimes reads the first line of /proc/stat: all jiffies and the steal
// column.
func (s *sampler) cpuTimes() (total, steal int64, ok bool) {
	n, err := s.read(s.proc, s.buf[:])
	if err != nil {
		return 0, 0, false
	}
	b := s.buf[:n]
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	if !bytes.HasPrefix(b, []byte("cpu ")) {
		return 0, 0, false
	}
	b = b[4:]
	for i := 0; i < 8; i++ { // user nice system idle iowait irq softirq steal
		b = bytes.TrimLeft(b, " ")
		end := bytes.IndexByte(b, ' ')
		if end < 0 {
			end = len(b)
		}
		v, err := strconv.ParseInt(string(b[:end]), 10, 64)
		if err != nil || v < 0 {
			return 0, 0, false
		}
		total += v
		if i == 7 {
			steal = v
		}
		b = b[end:]
	}
	return total, steal, true
}

func (s *sampler) readGroup(g *group, secs float64) {
	g.cpuPct, g.peakMB, g.cpuReadable = -1, -1, false
	if n, err := s.read(g.cpuPath, s.buf[:]); err == nil {
		if usec, ok := field(s.buf[:n], "usage_usec"); ok {
			g.cpuReadable = true
			if g.prevUsec >= 0 && usec >= g.prevUsec && secs > 0 {
				g.cpuPct = pct(float64(usec-g.prevUsec) / 1e6 / secs * 100)
			}
			g.prevUsec = usec // after a counter that went backwards too
		}
	}
	if !g.noPeak {
		v, err := s.number(g.peakPath)
		if err == nil {
			g.peakMB = v >> 20
			return
		}
		g.noPeak = errors.Is(err, fs.ErrNotExist) && g.cpuReadable

	}
	if g.noPeak {
		if v, err := s.number(g.curPath); err == nil {
			g.maxCur = max(g.maxCur, v)
			g.peakMB = g.maxCur >> 20
		}
	}
}

// pct rounds a percentage to one decimal.
func pct(v float64) float64 { return math.Round(v*10) / 10 }

// tick reads every file once. The first call only records the starting
// readings; each later one logs a line. It reports whether any group's CPU
// counter could be read.
func (s *sampler) tick(now time.Time) bool {
	secs := now.Sub(s.prevAt).Seconds()
	any := false
	for _, g := range s.groups {
		s.readGroup(g, secs)
		any = any || g.cpuReadable
	}
	stealPct := -1.0
	if total, steal, ok := s.cpuTimes(); ok {
		if s.prevTotal >= 0 && total > s.prevTotal && steal >= s.prevSteal {
			stealPct = pct(float64(steal-s.prevSteal) / float64(total-s.prevTotal) * 100)
		}
		s.prevTotal, s.prevSteal = total, steal
	}
	first := !s.primed
	s.primed, s.prevAt = true, now
	if first {
		return any
	}
	s.log.Info("cpu sample", "interval_s", pct(secs),
		"server_cpu_pct", s.groups[0].cpuPct, "server_mem_peak_mb", s.groups[0].peakMB,
		"judge_cpu_pct", s.groups[1].cpuPct, "judge_mem_peak_mb", s.groups[1].peakMB,
		"sandbox_cpu_pct", s.groups[2].cpuPct, "sandbox_mem_peak_mb", s.groups[2].peakMB,
		"steal_pct", stealPct)
	return any
}

// StartSampler starts the production sampler and returns the function that
// stops it and waits for it to end. Where the control group files do not
// exist it logs once that sampling is off and starts nothing.
func StartSampler(log *slog.Logger) (stop func()) {
	return startSampler(newSampler(log, cgroupRoot, procStat), SampleEvery)
}

func startSampler(s *sampler, every time.Duration) (stop func()) {
	if !s.tick(time.Now()) {
		s.log.Info("cpu sampling is off: no control group files for the server, the judge or the sandbox slice on this machine", "root", s.root)
		return func() {}
	}
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case now := <-t.C:
				s.tick(now)
			case <-quit:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(quit) })
		<-done
	}
}
