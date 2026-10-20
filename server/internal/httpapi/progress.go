package httpapi

// Progress (v2 plan B4.8, contract C8): a person's own results over the
// contests they took part in, read from the per-student contest summaries
// that are written when a contest turns final. Only final results reach
// it (a frozen or held contest is absent), exam-mode contests show their
// totals only, practice never changes a score, and tags come without the
// internal "ai draft" tag. This file reads summaries and the problem
// bank's tags and nothing about integrity standing or the rating (a test
// checks that it names nothing from integrity/rating.go).

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// ProgressProblem is one problem of a contest in Progress.
type ProgressProblem struct {
	Label          string                   `json:"label"`
	Title          string                   `json:"title"`
	ProblemID      int64                    `json:"problem_id"`
	Score          *int                     `json:"score"`
	MaxScore       int                      `json:"max_score"`
	Attempts       *int                     `json:"attempts"`
	FullAtMinutes  *int                     `json:"full_at_minutes"`
	BestVerdict    *string                  `json:"best_verdict"`
	Subtasks       []contests.SubtaskEarned `json:"subtasks"`
	Tags           []string                 `json:"tags"`
	Difficulty     string                   `json:"difficulty"`
	PracticeOpen   bool                     `json:"practice_open"`
	Upsolved       bool                     `json:"upsolved"`
	ZeroByDecision bool                     `json:"zero_by_decision"`
}

// ProgressContest is one contest in Progress.
type ProgressContest struct {
	ContestID    int64             `json:"contest_id"`
	Slug         string            `json:"slug"`
	Title        string            `json:"title"`
	Graded       bool              `json:"graded"`
	FeedbackMode string            `json:"feedback_mode"`
	Detail       string            `json:"detail"`
	EndedAt      int64             `json:"ended_at"`
	FinalisedAt  int64             `json:"finalised_at"`
	Score        int               `json:"score"`
	MaxScore     int               `json:"max_score"`
	Percent      float64           `json:"percent"`
	Rank         *int              `json:"rank"`
	RankedCount  *int              `json:"ranked_count"`
	Problems     []ProgressProblem `json:"problems"`
}

// ProgressTotals are the headline figures.
type ProgressTotals struct {
	ContestsTaken       int      `json:"contests_taken"`
	GradedTaken         int      `json:"graded_taken"`
	Points              int      `json:"points"`
	MaxPoints           int      `json:"max_points"`
	ProblemsFull        int      `json:"problems_full"`
	ProblemsPartial     int      `json:"problems_partial"`
	ProblemsZero        int      `json:"problems_zero"`
	PersonalBestPercent *float64 `json:"personal_best_percent"`
	Upsolved            int      `json:"upsolved"`
}

type tagTotal struct {
	Tag       string `json:"tag"`
	Earned    int    `json:"earned"`
	Available int    `json:"available"`
	Problems  int    `json:"problems"`
}

type difficultyTotal struct {
	Difficulty string `json:"difficulty"`
	Earned     int    `json:"earned"`
	Available  int    `json:"available"`
	Problems   int    `json:"problems"`
}

type lossCount struct {
	Verdict  string `json:"verdict"`
	Problems int    `json:"problems"`
}

type retryItem struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	Title        string `json:"title"`
	Score        int    `json:"score"`
	MaxScore     int    `json:"max_score"`
	PracticeOpen bool   `json:"practice_open"`
}

type progressUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Progress is the C8 response.
type Progress struct {
	User         progressUser      `json:"user"`
	HideProgress *bool             `json:"hide_progress,omitempty"`
	GeneratedAt  int64             `json:"generated_at"`
	Totals       ProgressTotals    `json:"totals"`
	Contests     []ProgressContest `json:"contests"`
	ByTag        []tagTotal        `json:"by_tag"`
	ByDifficulty []difficultyTotal `json:"by_difficulty"`
	Losses       []lossCount       `json:"losses"`
	Retry        []retryItem       `json:"retry"`
	ServerNow    int64             `json:"server_now"`
}

func emptyProgress(id int64, name string, now int64) *Progress {
	return &Progress{User: progressUser{id, name}, GeneratedAt: now, Contests: []ProgressContest{}, ByTag: []tagTotal{},
		ByDifficulty: []difficultyTotal{}, Losses: []lossCount{}, Retry: []retryItem{}, ServerNow: now}
}

func (d Deps) myProgress(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	hide := u.HideProgress
	var p *Progress
	var err error
	if hide {
		p = emptyProgress(u.ID, u.Name, d.serverNow())
	} else if p, err = d.progressFor(r.Context(), u.ID); err != nil {
		d.internalError(w, r, err)
		return
	}
	p.HideProgress = &hide
	WriteJSON(w, http.StatusOK, p)
}

func (d Deps) userProgress(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	p, err := d.progressFor(r.Context(), st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

// problemMeta is a problem's tags (without the AI draft tag) and
// difficulty, from the bank.
func (d Deps) problemMeta(ctx context.Context, id int64) ([]string, string) {
	var raw, diff string
	if d.DB.QueryRowContext(ctx, "SELECT tags, difficulty FROM problems WHERE id = ?", id).Scan(&raw, &diff) != nil {
		return []string{}, ""
	}
	var tags []string
	json.Unmarshal([]byte(raw), &tags)
	out := []string{}
	for _, t := range tags {
		if strings.ToLower(strings.TrimSpace(t)) != "ai draft" {
			out = append(out, t)
		}
	}
	return out, diff
}

// progressFor builds one person's progress from their summaries.
func (d Deps) progressFor(ctx context.Context, userID int64) (*Progress, error) {
	now := d.serverNow()
	var name, email string
	if err := d.DB.QueryRowContext(ctx, "SELECT name, email FROM users WHERE id = ?", userID).Scan(&name, &email); err != nil {
		return nil, err
	}
	p := emptyProgress(userID, name, now)
	if strings.HasSuffix(email, "@removed.invalid") {
		return p, nil
	}
	sums, err := d.Contests.Summaries(ctx, userID)
	if err != nil {
		return nil, err
	}
	tags := map[string]*tagTotal{}
	diffs := map[string]*difficultyTotal{}
	losses := map[string]int{}
	for _, sm := range sums {
		c, err := d.Contests.Get(ctx, sm.ContestID)
		if err != nil {
			return nil, err
		}
		if !c.StudentsSeeFinal() {
			continue
		}
		full := c.Feedback != "exam"
		up, err := d.Contests.Upsolved(ctx, c.ID, userID)
		if err != nil {
			return nil, err
		}
		pc := ProgressContest{ContestID: c.ID, Slug: c.Slug, Title: c.Title, Graded: sm.Graded, FeedbackMode: c.Feedback, Detail: "full",
			EndedAt: c.Final().UnixMilli(), FinalisedAt: sm.FinalisedAt, Score: sm.Score, MaxScore: sm.MaxScore, Percent: sm.Percent,
			Problems: []ProgressProblem{}}
		rank, count := sm.Rank, sm.RankedCount
		pc.Rank, pc.RankedCount = &rank, &count
		if !full {
			pc.Detail = "totals"
		}
		practice := c.PracticeAfterEnd && !c.Archived
		titles := map[int64]string{}
		for _, cp := range c.Problems {
			titles[cp.ProblemID] = cp.Title
		}
		for _, sp := range sm.Problems {
			t, diff := d.problemMeta(ctx, sp.ProblemID)
			pp := ProgressProblem{Label: sp.Label, Title: titles[sp.ProblemID], ProblemID: sp.ProblemID, MaxScore: sp.MaxScore, Tags: t,
				Difficulty: diff, PracticeOpen: practice, ZeroByDecision: sp.Zeroed,
				Upsolved: up[sp.ProblemID] && sp.Score < sp.MaxScore}
			if pp.Upsolved {
				p.Totals.Upsolved++
			}
			if full {
				score, attempts := sp.Score, sp.Attempts
				pp.Score, pp.Attempts, pp.FullAtMinutes, pp.Subtasks = &score, &attempts, sp.FullAtMinutes, sp.Subtasks
				if sp.BestVerdict != "" {
					v := sp.BestVerdict
					pp.BestVerdict = &v
				}
				switch {
				case sp.MaxScore > 0 && sp.Score >= sp.MaxScore:
					p.Totals.ProblemsFull++
				case sp.Score > 0:
					p.Totals.ProblemsPartial++
				default:
					p.Totals.ProblemsZero++
				}
				for _, tg := range t {
					if tags[tg] == nil {
						tags[tg] = &tagTotal{Tag: tg}
					}
					tags[tg].Earned += sp.Score
					tags[tg].Available += sp.MaxScore
					tags[tg].Problems++
				}
				if diff != "" {
					if diffs[diff] == nil {
						diffs[diff] = &difficultyTotal{Difficulty: diff}
					}
					diffs[diff].Earned += sp.Score
					diffs[diff].Available += sp.MaxScore
					diffs[diff].Problems++
				}
				if sp.Score < sp.MaxScore {
					if sp.BestVerdict != "" && sp.BestVerdict != "AC" {
						losses[sp.BestVerdict]++
					}
					p.Retry = append(p.Retry, retryItem{Slug: c.Slug, Label: sp.Label, Title: pp.Title, Score: sp.Score, MaxScore: sp.MaxScore,
						PracticeOpen: practice})
				}
			} else {
				pp.Subtasks = nil
			}
			pc.Problems = append(pc.Problems, pp)
		}
		p.Totals.ContestsTaken++
		p.Totals.Points += sm.Score
		p.Totals.MaxPoints += sm.MaxScore
		if sm.Graded {
			p.Totals.GradedTaken++
			if p.Totals.PersonalBestPercent == nil || sm.Percent > *p.Totals.PersonalBestPercent {
				best := sm.Percent
				p.Totals.PersonalBestPercent = &best
			}
		}
		p.Contests = append(p.Contests, pc)
	}
	for _, t := range tags {
		p.ByTag = append(p.ByTag, *t)
	}
	sort.Slice(p.ByTag, func(i, j int) bool { return p.ByTag[i].Tag < p.ByTag[j].Tag })
	for _, k := range []string{"easy", "medium", "hard"} {
		if diffs[k] != nil {
			p.ByDifficulty = append(p.ByDifficulty, *diffs[k])
		}
	}
	for v, n := range losses {
		p.Losses = append(p.Losses, lossCount{v, n})
	}
	sort.Slice(p.Losses, func(i, j int) bool {
		if p.Losses[i].Problems != p.Losses[j].Problems {
			return p.Losses[i].Problems > p.Losses[j].Problems
		}
		return p.Losses[i].Verdict < p.Losses[j].Verdict
	})
	return p, nil
}
