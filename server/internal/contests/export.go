package contests

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExportRow is one line of the results: the full board's row with the
// student's contact details from the roster and when they started.
type ExportRow struct {
	Row
	Email      string `json:"email"`
	RollNumber string `json:"roll_number"`
	Section    string `json:"section"`
	StartedAt  *int64 `json:"started_at"`
	// ZeroByDecision lists the labels a review decision set to zero
	// (D77 amended). It is set only for owners and admins: integrity data
	// never reaches a co-organiser whose site role is student.
	ZeroByDecision *[]string `json:"zero_by_decision,omitempty"`
}

// Export returns the full board with each row's email, roll number,
// section and start time. For organisers only; withZero adds each row's
// zero-by-decision labels (owners and admins).
func (s *Service) Export(ctx context.Context, c *Contest, withZero bool) ([]ExportRow, *Standings, error) {
	st, err := s.Board(ctx, c, VariantFull)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.email, coalesce(r.roll_number, ''), coalesce(r.section, ''), coalesce(cs.started_at, 0)
		FROM users u
		LEFT JOIN contest_roster r ON r.contest_id = ? AND r.email = u.email
		LEFT JOIN contest_starts cs ON cs.contest_id = ? AND cs.user_id = u.id
		WHERE u.id IN (SELECT user_id FROM contest_starts WHERE contest_id = ?
			UNION SELECT user_id FROM submissions WHERE contest_id = ? AND kind = 'submit')`, c.ID, c.ID, c.ID, c.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type contact struct {
		email, roll, section string
		started              int64
	}
	byUser := map[int64]contact{}
	for rows.Next() {
		var id int64
		var ct contact
		if err := rows.Scan(&id, &ct.email, &ct.roll, &ct.section, &ct.started); err != nil {
			return nil, nil, err
		}
		byUser[id] = ct
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	out := make([]ExportRow, 0, len(st.Rows))
	for _, r := range st.Rows {
		ct := byUser[r.UserID]
		e := ExportRow{Row: r, Email: ct.email, RollNumber: ct.roll, Section: ct.section}
		if withZero {
			z := append([]string{}, r.Zeroed...)
			e.ZeroByDecision = &z
		}
		if ct.started > 0 {
			e.StartedAt = &ct.started
		}
		out = append(out, e)
	}
	return out, st, nil
}

// csvCell neutralises a value a spreadsheet would run as a formula: a cell
// starting with =, +, -, @, a tab or a carriage return gets a leading
// single quote (CSV injection, OWASP).
func csvCell(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

// WriteCSV writes the results as CSV: rank, name, email, roll number,
// section, started (UTC, RFC 3339), total, then one column per problem,
// and with withZero last zero_by_decision (labels separated by ";").
// Scores stay numbers, so a zero by decision is a 0 in its column.
func WriteCSV(w io.Writer, c *Contest, rows []ExportRow, withZero bool) error {
	cw := csv.NewWriter(w)
	head := []string{"rank", "name", "email", "roll_number", "section", "started", "total"}
	for _, p := range c.Problems {
		head = append(head, p.Label)
	}
	if withZero {
		head = append(head, "zero_by_decision")
	}
	if err := cw.Write(head); err != nil {
		return err
	}
	for _, r := range rows {
		started := ""
		if r.StartedAt != nil {
			started = time.UnixMilli(*r.StartedAt).UTC().Format(time.RFC3339)
		}
		rec := []string{strconv.Itoa(r.Rank), csvCell(r.Name), csvCell(r.Email), csvCell(r.RollNumber), csvCell(r.Section),
			started, strconv.Itoa(r.Total)}
		for _, cell := range r.Cells {
			rec = append(rec, strconv.Itoa(cell.Score))
		}
		if withZero {
			z := ""
			if r.ZeroByDecision != nil {
				z = strings.Join(*r.ZeroByDecision, ";")
			}
			rec = append(rec, z)
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportSubmission is one submission in the JSON export.
type ExportSubmission struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	ProblemLabel string `json:"problem_label"`
	Language     string `json:"language"`
	State        string `json:"state"`
	Verdict      string `json:"verdict"`
	Score        int    `json:"score"`
	CreatedAt    int64  `json:"created_at"`
	Practice     bool   `json:"practice"`
}

// ExportSubmissions lists every submission of the contest, oldest first,
// without code or integrity data.
func (s *Service) ExportSubmissions(ctx context.Context, c *Contest) ([]ExportSubmission, error) {
	labels := map[int64]string{}
	for _, p := range c.Problems {
		labels[p.ProblemID] = p.Label
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, problem_id, language, state, verdict, score, created_at, practice
		FROM submissions WHERE contest_id = ? AND kind = 'submit' ORDER BY id`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportSubmission{}
	for rows.Next() {
		var e ExportSubmission
		var pid int64
		if err := rows.Scan(&e.ID, &e.UserID, &pid, &e.Language, &e.State, &e.Verdict, &e.Score, &e.CreatedAt, &e.Practice); err != nil {
			return nil, err
		}
		e.ProblemLabel = labels[pid]
		out = append(out, e)
	}
	return out, rows.Err()
}

// ProblemStats is one problem's numbers after (or during) a contest.
type ProblemStats struct {
	Label     string `json:"label"`
	Title     string `json:"title"`
	MaxScore  int    `json:"max_score"`
	Attempted int    `json:"attempted"`
	// Solved counts students with the full score.
	Solved   int           `json:"solved"`
	Subtasks []SubtaskStat `json:"subtasks"`
	// Histogram counts students by their score on the problem, among those
	// who attempted it, highest score first.
	Histogram []ScoreBucket `json:"histogram"`
	// MedianAttempts is the median number of submissions among students
	// who attempted the problem.
	MedianAttempts float64 `json:"median_attempts"`
}

// SubtaskStat counts the students who earned a subtask.
type SubtaskStat struct {
	Idx    int `json:"idx"`
	Points int `json:"points"`
	Earned int `json:"earned"`
}

// ScoreBucket is one bar of a histogram.
type ScoreBucket struct {
	Score    int `json:"score"`
	Students int `json:"students"`
}

// Stats computes per-problem numbers from the participants' non-practice
// submissions.
func (s *Service) Stats(ctx context.Context, c *Contest) ([]ProblemStats, error) {
	people, err := s.participants(ctx, c)
	if err != nil {
		return nil, err
	}
	counted := map[int64]bool{}
	for _, p := range people {
		counted[p.UserID] = true
	}
	entries, _, err := s.entries(ctx, c, 0)
	if err != nil {
		return nil, err
	}
	out := make([]ProblemStats, 0, len(c.Problems))
	for _, p := range c.Problems {
		ps := ProblemStats{Label: p.Label, Title: p.Title, MaxScore: p.MaxScore(), Subtasks: []SubtaskStat{}, Histogram: []ScoreBucket{}}
		attempts := map[int64]int{}
		best := map[int64][]int{}
		bestSingle := map[int64]int{}
		for _, e := range entries {
			if e.ProblemID != p.ProblemID || !counted[e.UserID] {
				continue
			}
			attempts[e.UserID]++
			if !e.Done {
				continue
			}
			b := best[e.UserID]
			if b == nil {
				b = make([]int, len(p.Points))
				best[e.UserID] = b
			}
			for i := range b {
				if i < len(e.Earned) && e.Earned[i] > b[i] {
					b[i] = e.Earned[i]
				}
			}
			bestSingle[e.UserID] = max(bestSingle[e.UserID], e.Score)
		}
		ps.Attempted = len(attempts)
		for i, pts := range p.Points {
			st := SubtaskStat{Idx: i + 1, Points: pts}
			for _, b := range best {
				if b[i] >= pts && pts > 0 {
					st.Earned++
				}
			}
			ps.Subtasks = append(ps.Subtasks, st)
		}
		hist := map[int]int{}
		for u := range attempts {
			score := bestSingle[u]
			if c.Scoring != BestSubmission {
				score = 0
				for _, v := range best[u] {
					score += v
				}
			}
			hist[score]++
			if score == ps.MaxScore && ps.MaxScore > 0 {
				ps.Solved++
			}
		}
		for score, n := range hist {
			ps.Histogram = append(ps.Histogram, ScoreBucket{Score: score, Students: n})
		}
		sort.Slice(ps.Histogram, func(i, j int) bool { return ps.Histogram[i].Score > ps.Histogram[j].Score })
		ps.MedianAttempts = median(attempts)
		out = append(out, ps)
	}
	return out, nil
}

func median(m map[int64]int) float64 {
	if len(m) == 0 {
		return 0
	}
	v := make([]int, 0, len(m))
	for _, n := range m {
		v = append(v, n)
	}
	sort.Ints(v)
	if len(v)%2 == 1 {
		return float64(v[len(v)/2])
	}
	return float64(v[len(v)/2-1]+v[len(v)/2]) / 2
}

// FeedFilter narrows the organisers' live feed.
type FeedFilter struct {
	ProblemID int64
	UserID    int64
	// Verdict is a verdict code (AC, WA, ...), "pending" for queued or
	// running, or "error" for a judge error; case does not matter.
	Verdict string
	Page    int // from 1
	// IntegrityCounts includes each submission's typed, pasted and focus
	// counts. Only owners and admins get them: a co-organiser whose site
	// role is student (a TA) never sees a classmate's integrity data.
	IntegrityCounts bool
	// Hide blanks the results a co-organiser without the admin role may not
	// see yet (CoOrganiserHide). A verdict filter must not be combined
	// with it: the handler refuses one while anything is hidden.
	Hide Hide
}

// FeedPage is how many submissions one page of the feed holds.
const FeedPage = 50

// FeedItem is one submission in the organisers' feed.
type FeedItem struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	ProblemLabel string `json:"problem_label"`
	Language     string `json:"language"`
	State        string `json:"state"`
	Verdict      string `json:"verdict"`
	Score        int    `json:"score"`
	MaxScore     int    `json:"max_score"`
	CreatedAt    int64  `json:"created_at"`
	JudgedAt     *int64 `json:"judged_at"`
	Practice     bool   `json:"practice"`
	Rejudged     bool   `json:"rejudged"`
	// Hidden marks a submission whose result the viewer may not see yet:
	// state "submitted", no verdict, score, judging time or rejudge mark.
	Hidden bool `json:"hidden,omitempty"`
	// The integrity counts are left out unless FeedFilter.IntegrityCounts.
	TypedChars  *int `json:"typed_chars,omitempty"`
	PastedChars *int `json:"pasted_chars,omitempty"`
	FocusLosses *int `json:"focus_losses,omitempty"`
	// NoteCount and CaseID (v1.4.0) are the private notes on the
	// submission and the manual case opened from it, for reviewers only.
	NoteCount *int   `json:"note_count,omitempty"`
	CaseID    *int64 `json:"case_id,omitempty"`
}

// Feed lists the contest's submissions, newest first, a page at a time,
// and reports whether there are more.
func (s *Service) Feed(ctx context.Context, c *Contest, f FeedFilter) ([]FeedItem, bool, error) {
	labels := map[int64]string{}
	for _, p := range c.Problems {
		labels[p.ProblemID] = p.Label
	}
	if f.Page < 1 {
		f.Page = 1
	}
	q := `SELECT s.id, s.user_id, coalesce(nullif(u.name, ''), u.email), u.email, s.problem_id, s.language, s.state, s.verdict, s.score,
		s.max_score, s.created_at, s.judged_at, s.practice, s.rejudged_at IS NOT NULL, s.typed_chars, s.pasted_chars, s.focus_losses
		FROM submissions s JOIN users u ON u.id = s.user_id WHERE s.contest_id = ? AND s.kind = 'submit'`
	args := []any{c.ID}
	if f.ProblemID > 0 {
		q += " AND s.problem_id = ?"
		args = append(args, f.ProblemID)
	}
	if f.UserID > 0 {
		q += " AND s.user_id = ?"
		args = append(args, f.UserID)
	}
	// "pending" and "error" are states, not verdicts: a submission waiting
	// or being judged has no verdict yet, and a judge error has none at all.
	switch v := strings.ToUpper(strings.TrimSpace(f.Verdict)); v {
	case "":
	case "PENDING":
		q += " AND s.state IN ('queued', 'running')"
	case "ERROR":
		q += " AND s.state = 'error'"
	default:
		q += " AND s.verdict = ?"
		args = append(args, v)
	}
	q += " ORDER BY s.id DESC LIMIT ? OFFSET ?"
	args = append(args, FeedPage+1, (f.Page-1)*FeedPage)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []FeedItem{}
	for rows.Next() {
		var it FeedItem
		var pid int64
		var judged *int64
		var typed, pasted, losses int
		if err := rows.Scan(&it.ID, &it.UserID, &it.Name, &it.Email, &pid, &it.Language, &it.State, &it.Verdict, &it.Score,
			&it.MaxScore, &it.CreatedAt, &judged, &it.Practice, &it.Rejudged, &typed, &pasted, &losses); err != nil {
			return nil, false, err
		}
		if f.IntegrityCounts {
			it.TypedChars, it.PastedChars, it.FocusLosses = &typed, &pasted, &losses
		}
		it.JudgedAt = judged
		it.ProblemLabel = labels[pid]
		if f.Hide.Covers(time.UnixMilli(it.CreatedAt)) && it.State != "error" {
			it.State, it.Verdict, it.Score, it.JudgedAt, it.Rejudged, it.Hidden = "submitted", "", 0, nil, false, true
		}
		out = append(out, it)
	}
	more := len(out) > FeedPage
	if more {
		out = out[:FeedPage]
	}
	return out, more, rows.Err()
}

// JSONExport is everything the JSON export holds.
type JSONExport struct {
	Contest     any                `json:"contest"`
	Standings   []ExportRow        `json:"standings"`
	Submissions []ExportSubmission `json:"submissions"`
	// Audit is the contest's audit log, oldest first.
	Audit      []AuditEntry `json:"audit"`
	ExportedAt int64        `json:"exported_at"`
}

// MarshalExport encodes an export; split out so tests can check it.
func MarshalExport(e JSONExport) ([]byte, error) { return json.MarshalIndent(e, "", "  ") }
