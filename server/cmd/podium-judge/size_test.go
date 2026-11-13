package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/sizing"
)

type fakeHost struct{ vcpus, mem int }

func (f fakeHost) VCPUs() (int, error)    { return f.vcpus, nil }
func (f fakeHost) MemoryMB() (int, error) { return f.mem, nil }
func (f fakeHost) CPUModel() string       { return "Fake CPU" }

type brokenHost struct{ fakeHost }

func (brokenHost) MemoryMB() (int, error) { return 0, errors.New("no /proc") }

func capFile(t *testing.T, bytes string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "memory.max")
	if err := os.WriteFile(p, []byte(bytes+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The worker sizes itself again at start and refuses a slice that is
// smaller than its budget needs, naming both numbers.
func TestTheWorkerRefusesASliceThatIsTooSmall(t *testing.T) {
	_, found, err := sizeJudge(config.Judge{}, fakeHost{2, 8192}, capFile(t, "1153433600"))
	want := "the sandbox slice allows 1100 MB but this judge needs 3976 MB (a memory budget of 3900 MB plus 76 MB): the pre-start step did not set the cap. Restart the judge service so it runs again, or lower PODIUM_JUDGE_MEMORY_BUDGET_MB"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if found != 1100 {
		t.Errorf("found = %d, want 1100", found)
	}
}

func TestTheWorkerStartsWhenTheSliceHoldsTheBudget(t *testing.T) {
	plan, found, err := sizeJudge(config.Judge{}, fakeHost{2, 8192}, capFile(t, "4169138176"))
	if err != nil || found != 3976 || plan.Lanes != 2 || plan.BudgetMB != 3900 || plan.CPUModel != "Fake CPU" {
		t.Fatalf("plan %+v found %d err %v", plan, found, err)
	}
	// A 2 GB machine keeps today's slice and starts as before.
	plan, _, err = sizeJudge(config.Judge{}, fakeHost{2, 2048}, capFile(t, "1153433600"))
	if err != nil || plan.BudgetMB != 1024 {
		t.Fatalf("2 GB: plan %+v err %v", plan, err)
	}
}

func TestAnUnreadableOrUnlimitedSliceDoesNotStopTheWorker(t *testing.T) {
	_, found, err := sizeJudge(config.Judge{}, fakeHost{2, 8192}, filepath.Join(t.TempDir(), "missing"))
	if err != nil || found != -1 {
		t.Errorf("missing file: found %d err %v", found, err)
	}
	_, found, err = sizeJudge(config.Judge{}, fakeHost{2, 8192}, capFile(t, "max"))
	if err != nil || found != 0 {
		t.Errorf("max: found %d err %v", found, err)
	}
}

func TestHandSetValuesAreCheckedAgainstTheMachine(t *testing.T) {
	big := capFile(t, "max")
	if _, _, err := sizeJudge(config.Judge{Lanes: 4}, fakeHost{2, 8192}, big); err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_LANES=4") {
		t.Errorf("lanes above vCPUs: %v", err)
	}
	if _, _, err := sizeJudge(config.Judge{BudgetMB: 5000}, fakeHost{2, 8192}, big); err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_MEMORY_BUDGET_MB=5000") {
		t.Errorf("budget above the machine: %v", err)
	}
	plan, _, err := sizeJudge(config.Judge{Lanes: 1, BudgetMB: 2000}, fakeHost{2, 8192}, big)
	if err != nil || plan.Lanes != 1 || plan.BudgetMB != 2000 {
		t.Errorf("hand-set: %+v %v", plan, err)
	}
	if _, _, err := sizeJudge(config.Judge{}, brokenHost{fakeHost{2, 8192}}, big); err == nil {
		t.Error("an unreadable machine was sized")
	}
}

// With the lane count chosen by the machine, the box range is checked
// after the choice, not only when the count is set by hand.
func TestTheChosenLanesKeepClearOfTheSelftestBox(t *testing.T) {
	_, _, err := sizeJudge(config.Judge{BoxBase: 79}, fakeHost{4, 8192}, capFile(t, "max"))
	if err == nil || !strings.Contains(err.Error(), "boxes 79 to 82") {
		t.Errorf("err = %v", err)
	}
}

func TestPrintSizeIsWhatThePreStartStepReads(t *testing.T) {
	var b strings.Builder
	printSize(&b, sizing.Size(4, 16384))
	want := "lanes=4\nsandbox_cap_mb=4172\nunit_cap_mb=512\n"
	if b.String() != want {
		t.Errorf("printSize = %q, want %q", b.String(), want)
	}
}

// The start line carries what a resize changes: lanes, budget, the cap the
// slice has, and the CPU model.
func TestTheStartLineSaysHowTheJudgeWasSized(t *testing.T) {
	plan := sizing.Plan{Sizing: sizing.Size(2, 8192), VCPUs: 2, MemoryMB: 8192, CPUModel: "Fake CPU"}
	want := map[string]any{"lanes": int64(2), "budget_mb": int64(3900), "sandbox_cap_mb": "3976", "cpu_model": "Fake CPU", "vcpus": int64(2), "memory_mb": int64(8192)}
	for found, label := range map[int]string{3976: "3976", 0: "none", -1: "unknown"} {
		got := map[string]any{}
		attrs := sizeAttrs(plan, found)
		for i := 0; i+1 < len(attrs); i += 2 {
			got[attrs[i].(string)] = attrs[i+1]
		}
		for k, v := range want {
			if k == "sandbox_cap_mb" {
				v = label
			}
			g := got[k]
			if n, ok := g.(int); ok {
				g = int64(n)
			}
			if g != v {
				t.Errorf("found %d: %s = %v, want %v", found, k, got[k], v)
			}
		}
	}
}
