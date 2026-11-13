package sizing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sizing table of docs/OPERATIONS.md, machine by machine. The 2 GB row
// is today's judge: nothing changes there.
func TestSizeTable(t *testing.T) {
	// memory -> budget; the cap is the budget plus 76.
	budget := map[int]int{2048: 1024, 4096: 1852, 8192: 3900, 16384: 4096}
	for _, vcpus := range []int{1, 2, 4, 8} {
		for mem, b := range budget {
			got := Size(vcpus, mem)
			want := Sizing{Lanes: vcpus, SandboxCapMB: b + 76, BudgetMB: b, UnitCapMB: 128 * vcpus}
			if got != want {
				t.Errorf("Size(%d vCPUs, %d MB) = %+v, want %+v", vcpus, mem, got, want)
			}
		}
	}
}

// On the 2 GB machine nothing changes: two lanes, the 1,100 MB slice cap,
// the 1,024 MB budget and the 256 MB unit cap that the deploy files carry.
func TestTwoGigabytesIsTodaysJudge(t *testing.T) {
	got := Size(2, 2048)
	if got != (Sizing{Lanes: 2, SandboxCapMB: 1100, BudgetMB: 1024, UnitCapMB: 256}) {
		t.Fatalf("2 vCPUs, 2 GB: %+v", got)
	}
	slice := readDeploy(t, "systemd/isolate.slice.d/podium.conf")
	if !strings.Contains(slice, "MemoryMax=1100M") {
		t.Error("the slice drop-in no longer caps at 1100M, the base the formula keeps for 2 GB")
	}
	unit := readDeploy(t, "systemd/podium-judge.service")
	if !strings.Contains(unit, "MemoryMax=256M") {
		t.Error("the judge unit no longer caps at 256M, the base the formula keeps for two lanes")
	}
}

func readDeploy(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSizeOddInputs(t *testing.T) {
	for _, c := range []struct {
		name       string
		vcpus, mem int
		lanes      int
		budget     int
		unit       int
	}{
		{"zero vCPUs", 0, 8192, 1, 3900, 128},
		{"negative vCPUs", -4, 8192, 1, 3900, 128},
		{"zero memory", 2, 0, 2, 1024, 256},
		{"negative memory", 2, -8192, 2, 1024, 256},
		{"less than one run needs", 2, 1000, 2, 1024, 256},
		{"huge machine", 1000, 1 << 30, 16, 4096, 16 * 128},
	} {
		got := Size(c.vcpus, c.mem)
		if got.Lanes != c.lanes || got.BudgetMB != c.budget || got.UnitCapMB != c.unit || got.SandboxCapMB != c.budget+76 {
			t.Errorf("%s: Size(%d, %d) = %+v", c.name, c.vcpus, c.mem, got)
		}
	}
}

// Whatever the memory: the budget stays between 1,024 and 4,096 MB, holds
// the largest single run once the machine can, and the cap fits under half
// the memory with the unit and the rest of the VM outside it.
func TestSizeProperties(t *testing.T) {
	for mem := 512; mem <= 1<<20; mem += 64 {
		s := Size(2, mem)
		if s.BudgetMB < 1024 || s.BudgetMB > 4096 {
			t.Fatalf("%d MB: budget %d outside 1024..4096", mem, s.BudgetMB)
		}
		if s.SandboxCapMB != s.BudgetMB+76 {
			t.Fatalf("%d MB: cap %d is not budget %d plus 76", mem, s.SandboxCapMB, s.BudgetMB)
		}
		if mem >= 2048+1024 && s.BudgetMB < LargestRunMB {
			t.Fatalf("%d MB: budget %d cannot hold the largest run (%d)", mem, s.BudgetMB, LargestRunMB)
		}
		if mem >= 2048 && s.SandboxCapMB > mem/2+SlackMB {
			t.Fatalf("%d MB: cap %d is more than half the memory", mem, s.SandboxCapMB)
		}
	}
}

type fakeHost struct {
	vcpus, mem int
	err        error
}

func (f fakeHost) VCPUs() (int, error)    { return f.vcpus, f.err }
func (f fakeHost) MemoryMB() (int, error) { return f.mem, f.err }
func (f fakeHost) CPUModel() string       { return "Fake CPU" }

func TestResolveAutomatic(t *testing.T) {
	p, err := Resolve(fakeHost{vcpus: 4, mem: 16384}, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Sizing != Size(4, 16384) || p.VCPUs != 4 || p.MemoryMB != 16384 || p.CPUModel != "Fake CPU" {
		t.Errorf("plan = %+v", p)
	}
}

func TestResolveHostErrorIsRefused(t *testing.T) {
	if _, err := Resolve(fakeHost{err: errors.New("no /proc")}, Overrides{}); err == nil {
		t.Fatal("a machine that cannot be read was sized")
	}
}

func TestAHandSetLaneCountAboveTheVCPUsIsRefused(t *testing.T) {
	_, err := Resolve(fakeHost{vcpus: 2, mem: 8192}, Overrides{Lanes: 4})
	want := "PODIUM_JUDGE_LANES=4 is more than this machine's 2 vCPUs; set it to 2 or less, or leave it out so the judge runs one lane per vCPU"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAHandSetLaneCountWinsWithinTheVCPUs(t *testing.T) {
	p, err := Resolve(fakeHost{vcpus: 4, mem: 8192}, Overrides{Lanes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.Lanes != 1 || p.UnitCapMB != 128 || p.BudgetMB != 3900 {
		t.Errorf("plan = %+v", p.Sizing)
	}
}

func TestAHandSetBudgetAboveTheMachineIsRefused(t *testing.T) {
	_, err := Resolve(fakeHost{vcpus: 2, mem: 8192}, Overrides{BudgetMB: 4000})
	want := "PODIUM_JUDGE_MEMORY_BUDGET_MB=4000 is more than this machine can hold: 8192 MB of memory allows at most 3900 MB; lower it or leave it out"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if _, err := Resolve(fakeHost{vcpus: 2, mem: 2048}, Overrides{BudgetMB: 1500}); err == nil {
		t.Error("a 1,500 MB budget was accepted on a 2 GB machine")
	}
}

// The automatic choice stops at 4,096 MB; a hand-set value may go up to
// what the machine holds.
func TestAHandSetBudgetWithinTheMachineWins(t *testing.T) {
	p, err := Resolve(fakeHost{vcpus: 4, mem: 16384}, Overrides{BudgetMB: 6000})
	if err != nil {
		t.Fatal(err)
	}
	if p.BudgetMB != 6000 || p.SandboxCapMB != 6076 || p.Lanes != 4 {
		t.Errorf("plan = %+v", p.Sizing)
	}
	p, err = Resolve(fakeHost{vcpus: 2, mem: 8192}, Overrides{BudgetMB: 2000})
	if err != nil || p.BudgetMB != 2000 || p.SandboxCapMB != 2076 {
		t.Errorf("plan = %+v, err %v", p.Sizing, err)
	}
}

func TestTheWorkerRefusesACapThatIsTooSmall(t *testing.T) {
	s := Size(2, 8192)
	err := CheckCap(1100, s)
	want := "the sandbox slice allows 1100 MB but this judge needs 3976 MB (a memory budget of 3900 MB plus 76 MB): the pre-start step did not set the cap. Restart the judge service so it runs again, or lower PODIUM_JUDGE_MEMORY_BUDGET_MB"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	for _, ok := range []int{3976, 4000, 0} {
		if err := CheckCap(ok, s); err != nil {
			t.Errorf("cap %d refused: %v", ok, err)
		}
	}
}

func TestReadSliceCap(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "memory.max")
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	for text, want := range map[string]int{"1153433600\n": 1100, "4169138176\n": 3976, "max\n": 0} {
		got, err := ReadSliceCapMB(write(text))
		if err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", text, got, err, want)
		}
	}
	if _, err := ReadSliceCapMB(write("lots\n")); err == nil {
		t.Error("garbage was read as a cap")
	}
	if _, err := ReadSliceCapMB(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing file was read as a cap")
	}
}

func TestParseMemTotalRoundsUpToTheNominalSize(t *testing.T) {
	for kb, want := range map[string]int{
		"1981236":  2048, // a 2 GB VM
		"8123456":  8192, // 7.7 GiB shown for 8 GB
		"16382000": 16384,
		"6738120":  6656, // this laptop's WSL
		"2097152":  2048, // exactly 2 GiB
	} {
		got, err := ParseMemTotalMB("MemFree: 5 kB\nMemTotal:       " + kb + " kB\nBuffers: 1 kB\n")
		if err != nil || got != want {
			t.Errorf("%s kB: got %d, %v; want %d", kb, got, err, want)
		}
	}
	if _, err := ParseMemTotalMB("MemFree: 5 kB\n"); err == nil {
		t.Error("meminfo without MemTotal was accepted")
	}
}

func TestParseCPUModel(t *testing.T) {
	text := "processor : 0\nvendor_id : GenuineIntel\nmodel name\t: Intel(R) Xeon(R) CPU @ 2.20GHz\nmodel name\t: other\n"
	if got := ParseCPUModel(text); got != "Intel(R) Xeon(R) CPU @ 2.20GHz" {
		t.Errorf("model = %q", got)
	}
	if got := ParseCPUModel("processor : 0\n"); got != "unknown" {
		t.Errorf("model = %q, want unknown", got)
	}
}
