package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/sizing"
)

// sizeJudge sizes the judge for host h from the hand-set values in cfg,
// checks the box range for the lane count it chose, and reads the cap of
// the sandbox slice at capPath. foundMB is -1 when the cap cannot be read
// (no cgroup v2 here) and 0 when the slice has none; neither stops the
// worker. A cap smaller than the budget needs does.
func sizeJudge(cfg config.Judge, h sizing.Host, capPath string) (plan sizing.Plan, foundMB int, err error) {
	plan, err = sizing.Resolve(h, sizing.Overrides{Lanes: cfg.Lanes, BudgetMB: cfg.BudgetMB})
	if err != nil {
		return plan, 0, err
	}
	if err := config.CheckBoxRange(cfg.BoxBase, plan.Lanes); err != nil {
		return plan, 0, err
	}
	foundMB, err = sizing.ReadSliceCapMB(capPath)
	if err != nil {
		return plan, -1, nil
	}
	return plan, foundMB, sizing.CheckCap(foundMB, plan.Sizing)
}

// size is the root pre-start step's question: how big is this judge? It
// prints the numbers as KEY=value lines on stdout for deploy/reset-boxes
// and one readable line on stderr for the journal. It does not touch the
// machine; reset-boxes sets the caps from these numbers.
func size(w io.Writer, log io.Writer) error {
	cfg, err := config.LoadJudge(os.Getenv)
	if err != nil {
		return err
	}
	plan, err := sizing.Resolve(sizing.LinuxHost{}, sizing.Overrides{Lanes: cfg.Lanes, BudgetMB: cfg.BudgetMB})
	if err != nil {
		return err
	}
	if err := config.CheckBoxRange(cfg.BoxBase, plan.Lanes); err != nil {
		return err
	}
	fmt.Fprintf(log, "podium-judge size: %d vCPUs, %d MB memory, %s: %d lanes, budget %d MB, sandbox cap %d MB, judge unit cap %d MB\n",
		plan.VCPUs, plan.MemoryMB, plan.CPUModel, plan.Lanes, plan.BudgetMB, plan.SandboxCapMB, plan.UnitCapMB)
	printSize(w, plan.Sizing)
	return nil
}

// printSize writes the three numbers the root pre-start step reads, one
// KEY=value per line.
func printSize(w io.Writer, s sizing.Sizing) {
	fmt.Fprintf(w, "lanes=%d\nsandbox_cap_mb=%d\nunit_cap_mb=%d\n", s.Lanes, s.SandboxCapMB, s.UnitCapMB)
}

// sizeAttrs are the log fields of the start line that say how the judge was
// sized: what a reader checks after a resize.
func sizeAttrs(plan sizing.Plan, foundMB int) []any {
	found := strconv.Itoa(foundMB)
	switch {
	case foundMB == 0:
		found = "none"
	case foundMB < 0:
		found = "unknown"
	}
	return []any{"lanes", plan.Lanes, "budget_mb", plan.BudgetMB, "sandbox_cap_mb", found,
		"vcpus", plan.VCPUs, "memory_mb", plan.MemoryMB, "cpu_model", plan.CPUModel}
}
