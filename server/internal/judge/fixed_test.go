package judge

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/sizing"
)

// What a verdict is judged under never follows the machine: the compile
// lock, each language's time factor and overheads, the compile limits, the
// wall-time formula and a problem's own limits. A resize changes how many
// lanes run and how much memory they share, and nothing here.
//
// For every machine of the sizing table this builds the runner the way
// podium-judge does at start (NewRunner with that machine's budget), asks
// it for the limits it would use for a fixed set of languages and problems,
// and requires the answers to be identical on every machine while the
// memory budget differs. A change that wires any of these to the machine
// (to the budget the runner holds, or to Size) shows up as a difference.
func TestWhatAVerdictIsJudgedUnderIsTheSameOnEveryMachine(t *testing.T) {
	if got := reflect.TypeOf(sizing.Sizing{}).NumField(); got != 4 {
		t.Fatalf("sizing.Sizing has %d fields; only lanes, caps and budget may follow the machine", got)
	}
	machines := []struct{ vcpus, memMB int }{}
	for _, v := range []int{1, 2, 4, 8} {
		for _, m := range []int{2048, 4096, 8192, 16384} {
			machines = append(machines, struct{ vcpus, memMB int }{v, m})
		}
	}
	problems := []struct {
		timeLimit time.Duration
		memoryMB  int
	}{{500 * time.Millisecond, 16}, {time.Second, 256}, {3 * time.Second, 1024}}

	// signature is everything a verdict depends on that Size could touch.
	signature := func(r *Runner) string {
		var b strings.Builder
		ids := make([]string, 0, len(Languages))
		for id := range Languages {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		langs := []Language{CheckerLanguage()}
		for _, id := range ids {
			langs = append(langs, Languages[id])
		}
		for _, l := range langs {
			b.WriteString(l.ID)
			b.WriteString(" lock:")
			b.WriteString(boolWord(l.CompileExclusive) + "/" + boolWord(l.RunExclusive))
			c := r.compileLimits(l)
			b.WriteString(" compile:" + c.CPU.String() + "," + c.Wall.String() + "," + itoa(c.MemoryKB) + "," + itoa(c.Processes) + "," + itoa(c.FileSizeKB))
			for _, p := range problems {
				lim := r.runLimits(l, p.timeLimit, p.memoryMB)
				args := strings.Join(RunArgs(RunRequest{Box: 1, Limits: lim, Args: []string{"./main"}}, "/m"), " ")
				b.WriteString(" run:" + lim.CPU.String() + "," + itoa(lim.MemoryKB) + "," + itoa(lim.Processes) + "," + itoa(lim.FileSizeKB) + " " + args)
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	var first string
	budgets := map[int]bool{}
	for i, m := range machines {
		s := sizing.Size(m.vcpus, m.memMB)
		r := NewRunner(Isolate{Path: "isolate"}, s.BudgetMB)
		if r.MemoryBudgetKB != s.BudgetMB*1024 {
			t.Fatalf("%d vCPUs, %d MB: the runner holds %d KB, Size gave %d MB", m.vcpus, m.memMB, r.MemoryBudgetKB, s.BudgetMB)
		}
		budgets[r.MemoryBudgetKB] = true
		sig := signature(r)
		if i == 0 {
			first = sig
			continue
		}
		if sig != first {
			t.Errorf("%d vCPUs, %d MB: the limits a verdict is judged under differ from the first machine's:\n%s\nwant:\n%s", m.vcpus, m.memMB, sig, first)
		}
	}
	if len(budgets) < 3 {
		t.Errorf("the machines of the table gave %d distinct budgets; the test would prove nothing", len(budgets))
	}
	// The wall-time formula: two times the CPU limit plus one second.
	if !strings.Contains(first, "--time=1.000 --wall-time=3.000") {
		t.Errorf("wall-time formula changed:\n%s", first)
	}
	// Pin the values themselves, so a change to both sides still shows.
	if !strings.Contains(first, "java21 lock:true/true compile:10s,20s,524288,64,65536") ||
		!strings.Contains(first, "cpp17 lock:true/false compile:10s,20s,524288,16,65536") {
		t.Errorf("compile lock or compile limits changed:\n%s", first)
	}
	// The largest single run is what the sizing formula assumes.
	if got := Languages["java21"].LimitsFor(time.Second, 1024).MemoryKB / 1024; got != sizing.LargestRunMB {
		t.Errorf("largest run is %d MB, sizing assumes %d MB", got, sizing.LargestRunMB)
	}
	if DefaultMemoryBudgetMB != sizing.MinBudgetMB {
		t.Errorf("DefaultMemoryBudgetMB %d differs from sizing.MinBudgetMB %d", DefaultMemoryBudgetMB, sizing.MinBudgetMB)
	}
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string { return strconv.Itoa(n) }
