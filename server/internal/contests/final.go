package contests

import (
	"context"
	"encoding/json"
)

// finalRow is one line of the standings kept after retention: no name or
// email (users holds those, and an erased student shows as the
// placeholder), only what the board scored.
type finalRow struct {
	Rank   int         `json:"rank"`
	UserID int64       `json:"user_id"`
	Total  int         `json:"total"`
	Cells  []finalCell `json:"cells"`
}

type finalCell struct {
	Label string `json:"label"`
	Score int    `json:"score"`
	Max   int    `json:"max"`
}

// FinalStandings returns the contest's full board as the JSON the
// retention job keeps in contest_results before the contest's
// submissions are deleted. It reads the database directly, bypassing the
// board cache.
func (s *Service) FinalStandings(ctx context.Context, id int64) ([]byte, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	st, err := s.computeBoard(ctx, c, VariantFull, 0)
	if err != nil {
		return nil, err
	}
	out := make([]finalRow, 0, len(st.Rows))
	for _, r := range st.Rows {
		row := finalRow{Rank: r.Rank, UserID: r.UserID, Total: r.Total, Cells: make([]finalCell, 0, len(r.Cells))}
		for _, cell := range r.Cells {
			row.Cells = append(row.Cells, finalCell{Label: cell.Label, Score: cell.Score, Max: cell.Max})
		}
		out = append(out, row)
	}
	return json.Marshal(out)
}
