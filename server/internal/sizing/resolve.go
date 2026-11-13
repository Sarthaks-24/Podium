package sizing

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Host is how the judge learns about its machine. Tests pass a fake.
type Host interface {
	// VCPUs is the number of vCPUs the judge can use.
	VCPUs() (int, error)
	// MemoryMB is the machine's memory, rounded up to the next 512 MB.
	MemoryMB() (int, error)
	// CPUModel names the processor, for the start line.
	CPUModel() string
}

// Overrides are the values set by hand; zero means choose automatically.
type Overrides struct {
	Lanes    int
	BudgetMB int
}

// Plan is a Sizing together with the machine it was made for.
type Plan struct {
	Sizing
	VCPUs    int
	MemoryMB int
	CPUModel string
}

// Resolve sizes the judge for h. A value set by hand wins over the formula,
// and is refused when the machine cannot hold it.
func Resolve(h Host, o Overrides) (Plan, error) {
	vcpus, err := h.VCPUs()
	if err != nil {
		return Plan{}, fmt.Errorf("read this machine's vCPUs: %w", err)
	}
	mem, err := h.MemoryMB()
	if err != nil {
		return Plan{}, fmt.Errorf("read this machine's memory: %w", err)
	}
	p := Plan{Sizing: Size(vcpus, mem), VCPUs: vcpus, MemoryMB: mem, CPUModel: h.CPUModel()}
	if o.Lanes > 0 {
		if o.Lanes > vcpus {
			return Plan{}, fmt.Errorf("PODIUM_JUDGE_LANES=%d is more than this machine's %d vCPUs; set it to %d or less, or leave it out so the judge runs one lane per vCPU", o.Lanes, vcpus, vcpus)
		}
		p.Lanes, p.UnitCapMB = o.Lanes, o.Lanes*UnitPerLaneMB
	}
	if o.BudgetMB > 0 {
		if limit := CeilingMB(mem); o.BudgetMB > limit {
			return Plan{}, fmt.Errorf("PODIUM_JUDGE_MEMORY_BUDGET_MB=%d is more than this machine can hold: %d MB of memory allows at most %d MB; lower it or leave it out", o.BudgetMB, mem, limit)
		}
		p.BudgetMB, p.SandboxCapMB = o.BudgetMB, o.BudgetMB+SlackMB
	}
	return p, nil
}

// CheckCap refuses to start when the sandbox slice allows less than the
// plan needs. foundMB is the slice's cap; zero or less means no cap.
func CheckCap(foundMB int, s Sizing) error {
	if foundMB <= 0 || foundMB >= s.SandboxCapMB {
		return nil
	}
	return fmt.Errorf("the sandbox slice allows %d MB but this judge needs %d MB (a memory budget of %d MB plus %d MB): the pre-start step did not set the cap. Restart the judge service so it runs again, or lower PODIUM_JUDGE_MEMORY_BUDGET_MB",
		foundMB, s.SandboxCapMB, s.BudgetMB, SlackMB)
}

// SliceCapPath is where cgroup v2 shows the cap of isolate.slice.
const SliceCapPath = "/sys/fs/cgroup/isolate.slice/memory.max"

// ReadSliceCapMB reads the memory cap of a cgroup (its memory.max file) in
// MB. It returns 0 for "max" (no cap).
func ReadSliceCapMB(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s holds %q, not a byte count", path, s)
	}
	return int(n >> 20), nil
}

// LinuxHost reads the machine from /proc.
type LinuxHost struct{}

// VCPUs is the number of CPUs this process may run on.
func (LinuxHost) VCPUs() (int, error) { return runtime.NumCPU(), nil }

// MemoryMB reads /proc/meminfo.
func (LinuxHost) MemoryMB() (int, error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	return ParseMemTotalMB(string(b))
}

// CPUModel reads /proc/cpuinfo.
func (LinuxHost) CPUModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	return ParseCPUModel(string(b))
}

// ParseMemTotalMB reads MemTotal from /proc/meminfo text and rounds it up
// to the next 512 MB, so a "8 GB" machine that shows 7.7 GiB counts as 8 GB.
func ParseMemTotalMB(meminfo string) (int, error) {
	for _, line := range strings.Split(meminfo, "\n") {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 1 {
			break
		}
		kb, err := strconv.Atoi(f[0])
		if err != nil || kb <= 0 {
			break
		}
		const step = 512
		return (kb/1024 + step - 1) / step * step, nil
	}
	return 0, errors.New("no MemTotal line in /proc/meminfo")
}

// ParseCPUModel reads the first "model name" from /proc/cpuinfo text.
func ParseCPUModel(cpuinfo string) string {
	for _, line := range strings.Split(cpuinfo, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return "unknown"
}
