package integrity

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// WritePairsCSV writes the run's similar-code pairs, strongest first: the
// problem, the strength and score, each student's name, email and
// submission id, and when the check finished.
func WritePairsCSV(w io.Writer, run *Run, people map[int64]Person, labels map[int64]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"problem", "strength", "score", "name_a", "email_a", "submission_a",
		"name_b", "email_b", "submission_b", "checked_at"}); err != nil {
		return err
	}
	checked := ""
	if run.FinishedAt != nil {
		checked = time.UnixMilli(*run.FinishedAt).UTC().Format(time.RFC3339)
	}
	for _, sg := range run.Signals {
		if sg.Kind != "similar" {
			continue
		}
		a, b := people[sg.UserA], people[sg.UserB]
		rec := []string{contests.CSVCell(labels[sg.ProblemID]), sg.Strength, fmt.Sprintf("%.2f", sg.Score),
			contests.CSVCell(a.Name), contests.CSVCell(a.Email), strconv.FormatInt(sg.SubmissionA, 10),
			contests.CSVCell(b.Name), contests.CSVCell(b.Email), strconv.FormatInt(sg.SubmissionB, 10),
			checked}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
