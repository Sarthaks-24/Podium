package judge

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/sizing"
)

// What a verdict is judged under never follows the machine: the compile
// lock, each language's time factor and overheads, the compile limits, the
// wall-time formula and a problem's own limits. A resize changes how many
// lanes run and how much memory they share, and nothing here. This pins
// the values once and checks them for every machine of the sizing table,
// so a change that lets Size reach any of them fails here.
func TestWhatAVerdictIsJudgedUnderIsTheSameOnEveryMachine(t *testing.T) {
	// Size may only decide these four numbers.
	if got := reflect.TypeOf(sizing.Sizing{}).NumField(); got != 4 {
		t.Fatalf("sizing.Sizing has %d fields; only lanes, caps and budget may follow the machine", got)
	}
	type fixed struct {
		timeFactor       float64
		overheadMB       int
		processes        int
		compileMB        int
		compileProcesses int
		compileLock      bool
		runLock          bool
	}
	pinned := map[string]fixed{
		"cpp17":   {1.0, 0, 1, 512, 16, true, false},
		"java21":  {2.0, 256, 64, 512, 64, true, true},
		"python3": {3.0, 64, 1, 128, 1, false, false},
		"node22":  {2.0, 128, 16, 0, 0, false, false},
	}
	for _, m := range []struct{ vcpus, memMB int }{
		{1, 2048}, {2, 2048}, {4, 2048}, {8, 2048},
		{1, 4096}, {2, 4096}, {4, 4096}, {8, 4096},
		{1, 8192}, {2, 8192}, {4, 8192}, {8, 8192},
		{1, 16384}, {2, 16384}, {4, 16384}, {8, 16384},
	} {
		s := sizing.Size(m.vcpus, m.memMB)
		r := &Runner{MemoryBudgetKB: s.BudgetMB * 1024}
		if r.MemoryBudgetKB != s.BudgetMB*1024 {
			t.Fatal("the budget is not what the runner holds")
		}
		for id, want := range pinned {
			l := Languages[id]
			got := fixed{l.TimeFactor, l.MemoryOverheadMB, l.Processes, l.CompileMemoryMB, l.CompileProcesses, l.CompileExclusive, l.RunExclusive}
			if got != want {
				t.Errorf("%d vCPUs, %d MB, %s: %+v, want %+v", m.vcpus, m.memMB, id, got, want)
			}
			// A problem's own limits, here a 1 second, 1,024 MB problem.
			lim := l.LimitsFor(time.Second, 1024)
			wantCPU := time.Duration(want.timeFactor * float64(time.Second))
			if lim.CPU != wantCPU || lim.MemoryKB != (1024+want.overheadMB)*1024 {
				t.Errorf("%d vCPUs, %d MB, %s: limits %+v", m.vcpus, m.memMB, id, lim)
			}
		}
		if compileCPU != 10*time.Second || compileWall != 20*time.Second || compileFileSizeKB != 64*1024 || DefaultFileSizeKB != 16*1024 {
			t.Errorf("%d vCPUs, %d MB: compile limits changed", m.vcpus, m.memMB)
		}
		// The wall-time formula: two times the CPU limit plus one second.
		args := strings.Join(RunArgs(RunRequest{Box: 1, Limits: Limits{CPU: 3 * time.Second, MemoryKB: 1024, Processes: 1}, Args: []string{"./main"}}, "/m"), " ")
		if !strings.Contains(args, "--time=3.000 --wall-time=7.000") {
			t.Errorf("%d vCPUs, %d MB: wall-time formula changed: %s", m.vcpus, m.memMB, args)
		}
	}
	// The largest single run is what the sizing formula assumes.
	if got := Languages["java21"].LimitsFor(time.Second, 1024).MemoryKB / 1024; got != sizing.LargestRunMB {
		t.Errorf("largest run is %d MB, sizing assumes %d MB", got, sizing.LargestRunMB)
	}
	if DefaultMemoryBudgetMB != sizing.MinBudgetMB {
		t.Errorf("DefaultMemoryBudgetMB %d differs from sizing.MinBudgetMB %d", DefaultMemoryBudgetMB, sizing.MinBudgetMB)
	}
}
