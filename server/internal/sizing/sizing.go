// Package sizing decides how big a judge is for the machine it runs on:
// how many lanes, how much memory the sandboxes may hold, and the memory
// cap of the judge's own unit. Size is the single source of truth. The
// judge's root pre-start step ("podium-judge size") and the worker both call
// it, so the cap that is set and the budget that is used cannot drift apart.
//
// Only these four numbers follow the machine. The compile lock, the time
// factor of each language, the compile limits, the wall-time formula and a
// problem's own limits never do: a resize must not change the conditions a
// verdict is judged under.
package sizing

// Fixed numbers behind the formula.
const (
	// MinBudgetMB is the budget of the 2 GB machine, today's value.
	MinBudgetMB = 1024
	// MaxBudgetMB is the largest budget chosen automatically.
	MaxBudgetMB = 4096
	// SlackMB is what the sandbox cap holds above the budget, for the
	// box files and runtime overhead the budget does not count.
	SlackMB = 76
	// LargestRunMB is the most one run can be given: a Java run on a
	// problem with the largest memory limit (1,024 MB plus 256 MB).
	LargestRunMB = 1280
	// UnitPerLaneMB is the judge unit's own memory cap for each lane.
	UnitPerLaneMB = 128
	// MaxLanes is the most lanes the judge allows.
	MaxLanes = 16
	// HeadroomMB is taken off half the memory before it goes to sandboxes.
	HeadroomMB = 120
)

// Sizing is what Size decides for one machine.
type Sizing struct {
	// Lanes is the number of sandboxes judged in parallel.
	Lanes int
	// SandboxCapMB is the memory cap of isolate.slice.
	SandboxCapMB int
	// BudgetMB is the total memory the runner lets boxes hold at once.
	BudgetMB int
	// UnitCapMB is the memory cap of the podium-judge unit.
	UnitCapMB int
}

// Size is the single source of truth for a judge's size.
//
//	lanes      = vCPUs, from 1 to 16 (one lane per vCPU, never more)
//	ceiling    = memory/2 - 120 - 76 MB, or 1,024 MB when that is under
//	             1,280 MB (the machine is too small to hold the largest run)
//	budget     = ceiling, at most 4,096 MB
//	sandbox cap = budget + 76 MB
//	unit cap   = 128 MB per lane
//
// Sandboxes get just under half the memory. The other half and more is
// left for the server, Litestream, Caddy, the operating system, the tmpfs
// the boxes live on and the judge unit. A 2 GB machine keeps today's
// numbers (1,024 MB budget, 1,100 MB cap, 256 MB unit for two lanes). Such a
// machine cannot hold a Java run at the largest memory limit (1,280 MB) and
// the 76 MB; that is the reason to resize, not something to hide.
func Size(vCPUs, memoryMB int) Sizing {
	budget := min(CeilingMB(memoryMB), MaxBudgetMB)
	lanes := min(max(vCPUs, 1), MaxLanes)
	return Sizing{Lanes: lanes, SandboxCapMB: budget + SlackMB, BudgetMB: budget, UnitCapMB: lanes * UnitPerLaneMB}
}

// CeilingMB is the most memory budget this machine can hold, before the
// automatic choice is limited to MaxBudgetMB. A hand-set budget may go up
// to it.
func CeilingMB(memoryMB int) int {
	c := memoryMB/2 - HeadroomMB - SlackMB
	if c < LargestRunMB {
		return MinBudgetMB
	}
	return c
}
