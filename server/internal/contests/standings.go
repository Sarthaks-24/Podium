// Package contests runs contests (milestone M4, cache/v1/m4-plan.md):
// scheduling and each student's window, access, per-contest limits, IOI
// scoring and the standings with freeze and reveal, rejudges, extensions,
// clarifications and announcements, the contest events sent to connected
// members, and the integrity rules of decision D47 (second sessions,
// retention). Since v2 it also keeps the contest audit log and the slug
// history, pins problems by revision, reports queue health, applies the
// board without the students an integrity decision excluded, and archives
// a term in the semester rollover. This file holds the scoring itself, a
// pure function of the contest, its problems and its submissions, so the
// rules can be tested against
// hand-computed boards.
package contests

import (
	"sort"
	"time"
)

// Scoring modes (PLAN.md section 9).
const (
	BestSubtask    = "best_subtask"
	BestSubmission = "best_submission"
)

// Board is the contest as scoring sees it.
type Board struct {
	StartsAt time.Time     // everyone's start
	Freeze   time.Duration // before the end; 0 means no freeze
	// FreezeAt is the contest-wide freeze (the close minus Freeze). A
	// personal extension never moves it: otherwise a student's first
	// minutes of the freeze would show to everyone.
	FreezeAt time.Time
	Problems []BoardProblem
}

// BoardProblem is one contest problem with its subtasks' points.
type BoardProblem struct {
	Label     string
	ProblemID int64
	Points    []int // by subtask, in order
}

// Participant is one student on the board.
type Participant struct {
	UserID  int64
	Name    string
	Started time.Time // window mode: when they pressed Start; fixed: StartsAt
	Ends    time.Time // their own end, with extensions
}

// Entry is one judged, non-practice submission.
type Entry struct {
	UserID    int64
	ProblemID int64
	At        time.Time
	Done      bool  // judged; pending ones only count as "+N" when frozen
	Earned    []int // per subtask, in order; nil for a compile error
	Score     int
}

// Cell is one student's result on one problem.
type Cell struct {
	Label   string `json:"label"`
	Score   int    `json:"score"`
	Max     int    `json:"max"`
	Pending int    `json:"pending"`
	Tried   bool   `json:"tried"`
}

// Row is one line of the board.
type Row struct {
	Rank   int    `json:"rank"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	// Email is set only on the organisers' board (GET
	// /api/admin/contests/{id}/standings for a site admin), so the console
	// can tell students apart; the public board never carries it.
	Email string `json:"email,omitempty"`
	Total int    `json:"total"`
	// LastImprovementS is when the student reached their total, in seconds
	// from their start; the tie-break.
	LastImprovementS int64  `json:"last_improvement_s"`
	Cells            []Cell `json:"cells"`
}

// Compute scores the board. With frozen set, a submission made at or after
// the student's freeze time (end minus Freeze) does not count; it appears
// as "+N" pending in its cell.
func Compute(b Board, people []Participant, entries []Entry, frozen bool) []Row {
	byUser := map[int64][]Entry{}
	for _, e := range entries {
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}
	rows := make([]Row, 0, len(people))
	for _, p := range people {
		freezeAt := p.Ends.Add(-b.Freeze)
		if !b.FreezeAt.IsZero() {
			freezeAt = b.FreezeAt
		}
		row := Row{UserID: p.UserID, Name: p.Name, Cells: make([]Cell, 0, len(b.Problems))}
		var last time.Duration
		mine := byUser[p.UserID]
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].At.Before(mine[j].At) })
		for _, prob := range b.Problems {
			cell := Cell{Label: prob.Label}
			for _, pt := range prob.Points {
				cell.Max += pt
			}
			best := make([]int, len(prob.Points))
			var reached time.Duration
			for _, e := range mine {
				if e.ProblemID != prob.ProblemID {
					continue
				}
				cell.Tried = true
				if frozen && b.Freeze > 0 && !e.At.Before(freezeAt) {
					cell.Pending++
					continue
				}
				if !e.Done {
					continue
				}
				// The best per subtask across submissions, against the pinned
				// points (best single submission was cut, V3-15).
				improved := false
				for i := range best {
					if i < len(e.Earned) {
						if v := min(max(e.Earned[i], 0), prob.Points[i]); v > best[i] {
							best[i], improved = v, true
						}
					}
				}
				if improved {
					reached = e.At.Sub(p.Started)
				}
			}
			cell.Score = 0
			for _, v := range best {
				cell.Score += v
			}
			row.Total += cell.Score
			if cell.Score > 0 && reached > last {
				last = reached
			}
			row.Cells = append(row.Cells, cell)
		}
		row.LastImprovementS = int64(last / time.Second)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		if rows[i].LastImprovementS != rows[j].LastImprovementS {
			return rows[i].LastImprovementS < rows[j].LastImprovementS
		}
		return rows[i].Name < rows[j].Name
	})
	for i := range rows {
		if i > 0 && rows[i].Total == rows[i-1].Total && rows[i].LastImprovementS == rows[i-1].LastImprovementS {
			rows[i].Rank = rows[i-1].Rank
		} else {
			rows[i].Rank = i + 1
		}
	}
	return rows
}
