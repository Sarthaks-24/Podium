package problems

// Proving the languages of a function problem on the judge (v2.1.0, stage
// F). A proof is a set of validate-kind jobs, one round per request: for
// each language a solution job (the language's locked top, a solution and
// its locked bottom, run on every test) and, for a compiled language, a
// starter job (the starting code compiled, no tests). A proof job carries a
// server-only Proof in its payload, which the judge never reads. It never
// marks the problem validated and writes no validations row. The job that
// closes last in its round applies the result with an ordinary save, inside
// its own finishing transaction.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// ProofPriority runs proof jobs just ahead of the validation that follows
// them (generation is 85, validation 90). It also selects proof jobs in the
// queries below, through the jobs_by_state index.
const ProofPriority = 88

// What asked for a proof round.
const (
	ProofSourceWizard = "wizard" // every offered language, queued by the generate chain
	ProofSourceFix    = "fix"    // one language written by the AI, added on a pass
	ProofSourceCheck  = "check"  // one pasted solution, changes nothing
)

// The two parts of a language that are proved.
const (
	ProofPartSolution = "solution"
	ProofPartStarter  = "starter"
)

// The outcome of one proof job.
const (
	ProofPassed    = "passed"
	ProofFailed    = "failed"
	ProofUnchecked = "unchecked" // the judge could not run it: not the language's fault
)

// Limits on what a ProofResult keeps of a report.
const (
	proofCompilerLines = 10
	proofCompilerBytes = 600
	proofTestsKept     = 60
)

// Proof is the server-only part of a proof job's payload.
type Proof struct {
	// Round is one random id per queued set, so results of an earlier set
	// are never mixed into a later one.
	Round    string `json:"round"`
	Source   string `json:"source"`
	Language string `json:"language"`
	Part     string `json:"part"`
	// ContentHash is the problem's hash when the round was queued, and
	// StarterSHA the SHA-256 of the language's starting code then: the
	// content hash leaves out starting code, so it is checked on its own.
	ContentHash string      `json:"content_hash"`
	StarterSHA  string      `json:"starter_sha"`
	By          int64       `json:"by,omitempty"`
	Parts       *ProofParts `json:"parts,omitempty"`
}

// ProofParts are blob hashes of the three parts a fix adds on a pass.
type ProofParts struct {
	Top     string `json:"top"`
	Starter string `json:"starter"`
	Bottom  string `json:"bottom"`
}

// ProofSpec says what a round proves. Solutions maps a language to the blob
// hash of its bare solution text. For a wizard round it covers every offered
// language except the reference language; for a check or a fix it holds the
// one Language, and a fix also carries the Parts it would add.
type ProofSpec struct {
	Source    string            `json:"source"`
	Solutions map[string]string `json:"solutions"`
	Language  string            `json:"language,omitempty"`
	Parts     *ProofParts       `json:"parts,omitempty"`
}

// proofValidate is the judge's validate payload plus the server-only Proof.
// The judge decodes payloads leniently and never reads it.
type proofValidate struct {
	judgeapi.ValidatePayload
	Proof *Proof `json:"proof,omitempty"`
}

func decodeProof(raw json.RawMessage) (proofValidate, error) {
	var pl proofValidate
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("validate payload: %w", err)
	}
	return pl, nil
}

// ProofTest is one test that did not pass: its index and verdict only.
type ProofTest struct {
	Idx     int    `json:"idx"`
	Verdict string `json:"verdict"`
}

// ProofResult is what one proof job showed. It holds verdict counts, the
// first compiler lines and test indexes, never a message, an input or an
// output: a checker message can quote the expected output.
type ProofResult struct {
	Language string `json:"language"`
	Part     string `json:"part"`
	Outcome  string `json:"outcome"`
	// Reason is one of judge_error, internal_error, no_report, compile_error,
	// wrong_answer, runtime_error, time_limit, memory_limit, output_limit,
	// skipped or deleted; empty for a pass.
	Reason   string         `json:"reason,omitempty"`
	Failed   int            `json:"failed"`
	Total    int            `json:"total"`
	Verdicts map[string]int `json:"verdicts"`
	Compiler string         `json:"compiler,omitempty"`
	Tests    []ProofTest    `json:"tests,omitempty"`
}

// Applied is what the last job of a round did with the result.
type Applied struct {
	// Kept names why nothing was removed or added: reference_failed,
	// reference_unchecked, or not_added (a fix that did not pass).
	Kept string `json:"kept,omitempty"`
	// Discarded is gone or changed: the problem was deleted, or edited while
	// the proof ran, so the result was dropped.
	Discarded string `json:"discarded,omitempty"`
	// Removed and Added are language ids; Stale are languages whose starting
	// code was edited during the round and were left alone.
	Removed    []string `json:"removed,omitempty"`
	Added      string   `json:"added,omitempty"`
	Stale      []string `json:"stale,omitempty"`
	Validating bool     `json:"validating"`
	// Error is an expected refusal of the save, kept as text so the job
	// still closes.
	Error string `json:"error,omitempty"`
}

// proofSummary is what a proof job stores as its summary.
type proofSummary struct {
	Result  ProofResult `json:"result"`
	Applied *Applied    `json:"applied,omitempty"`
}

var proofReasons = map[string]string{
	string(judge.WrongAnswer):  "wrong_answer",
	string(judge.RuntimeError): "runtime_error",
	string(judge.TimeLimit):    "time_limit",
	string(judge.MemoryLimit):  "memory_limit",
	string(judge.OutputLimit):  "output_limit",
	string(judge.Skipped):      "skipped",
}

// proofVerdictOrder breaks a tie between verdicts with the same count.
var proofVerdictOrder = []string{"WA", "RE", "TLE", "MLE", "OLE", "SK"}

const (
	// openProofSQL finds an open proof, including the chain jobs that will
	// queue one, so a draft is marked from the moment it is saved.
	openProofSQL = `SELECT EXISTS (SELECT 1 FROM jobs
		WHERE state IN ('queued','leased') AND kind IN ('validate','generate','generate_inputs')
		AND json_extract(payload, '$.problem_id') = ? AND json_extract(payload, '$.proof') IS NOT NULL)`
	// openSiblingsSQL counts the open proof jobs of one round, the one being
	// finished excluded.
	openSiblingsSQL = `SELECT count(*) FROM jobs WHERE state IN ('queued','leased') AND priority = ?
		AND kind = 'validate' AND id <> ? AND json_extract(payload, '$.problem_id') = ?
		AND json_extract(payload, '$.proof.round') = ?`
	// roundJobsSQL reads the closed jobs of one round.
	roundJobsSQL = `SELECT state, payload, summary FROM jobs WHERE state IN ('done','failed') AND priority = ?
		AND kind = 'validate' AND json_extract(payload, '$.problem_id') = ? AND json_extract(payload, '$.proof.round') = ?`
)

// OpenProof reports whether the problem is being checked: a proof job, or a
// generate job that will queue one, is waiting or running. Validate, a
// contest pin and a student attempt are refused meanwhile.
func OpenProof(ctx context.Context, q queryer, problemID int64) (bool, error) {
	var open bool
	err := q.QueryRowContext(ctx, openProofSQL, problemID).Scan(&open)
	return open, err
}

func newRound() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// enqueueProofs queues one round of proof jobs for p inside tx and returns
// how many. A wizard round covers every offered language: a solution job
// unless it is the reference language (its outputs come from the reference,
// so that job would pass by construction), and a starter job for a compiled
// language. A check or a fix covers spec.Language alone; a check has no
// starter job and a fix takes its three parts from spec.Parts. It refuses a
// round that could not be applied or would never finish: a whole-program
// problem, an unknown source or language, a fix without its parts, and a
// round of no jobs (a caller with an interpreted reference language as the
// only language queues the validation itself).
func (q queue) enqueueProofs(ctx context.Context, tx *sql.Tx, p *Problem, by int64, spec ProofSpec) (int, error) {
	switch {
	case !p.IsFunction():
		return 0, errors.New("proof: not a function problem")
	case spec.Source != ProofSourceWizard && spec.Source != ProofSourceFix && spec.Source != ProofSourceCheck:
		return 0, fmt.Errorf("proof: unknown source %q", spec.Source)
	case spec.Source != ProofSourceWizard && judge.Languages[spec.Language].ID == "":
		return 0, fmt.Errorf("proof: unknown language %q", spec.Language)
	case spec.Source == ProofSourceFix && (spec.Parts == nil || spec.Parts.Top == "" || spec.Parts.Starter == "" || spec.Parts.Bottom == ""):
		return 0, errors.New("proof: a fix needs its three parts")
	}
	base, err := q.payloadBase(ctx, tx, p)
	if err != nil {
		return 0, err
	}
	round := newRound()
	langs := p.Offered()
	if spec.Source != ProofSourceWizard {
		langs = []string{spec.Language}
	}
	n := 0
	enqueue := func(lang, part, source string) error {
		sha, err := q.blobs.Put(ctx, tx, []byte(source))
		if err != nil {
			return err
		}
		pl := proofValidate{ValidatePayload: base, Proof: &Proof{
			Round: round, Source: spec.Source, Language: lang, Part: part,
			ContentHash: p.ContentHash, StarterSHA: SHA([]byte(p.Starters[lang])), By: by, Parts: spec.Parts,
		}}
		pl.Programs.Reference = judgeapi.Program{Language: lang, SourceSHA: sha}
		if part == ProofPartStarter {
			pl.Tests = nil
		}
		if _, err := q.jobs.Enqueue(ctx, tx, judgeapi.KindValidate, ProofPriority, pl, by); err != nil {
			return err
		}
		n++
		return nil
	}
	read := func(sha string, max int) (string, error) {
		b, err := q.blobs.Read(sha, int64(max)+1)
		return string(b), err
	}
	for _, lang := range langs {
		top, starter, bottom := p.LockedTop[lang], p.Starters[lang], p.LockedBottom[lang]
		if pt := spec.Parts; pt != nil {
			if top, err = read(pt.Top, MaxStarter); err != nil {
				return 0, err
			}
			if starter, err = read(pt.Starter, MaxStarter); err != nil {
				return 0, err
			}
			if bottom, err = read(pt.Bottom, MaxStarter); err != nil {
				return 0, err
			}
		}
		if spec.Source != ProofSourceWizard || lang != p.Reference.Language {
			sha, ok := spec.Solutions[lang]
			if !ok {
				return 0, fmt.Errorf("proof: no solution for %s", lang)
			}
			solution, err := read(sha, MaxSource)
			if err != nil {
				return 0, err
			}
			if err := enqueue(lang, ProofPartSolution, Compose(top, solution, bottom).Source); err != nil {
				return 0, err
			}
		}
		if spec.Source != ProofSourceCheck && judge.Languages[lang].Compile != nil {
			if err := enqueue(lang, ProofPartStarter, Compose(top, starter, bottom).Source); err != nil {
				return 0, err
			}
		}
	}
	if n == 0 {
		return 0, errors.New("proof: nothing to prove")
	}
	return n, nil
}

// classify turns a closed proof job into its result.
func classify(pl proofValidate, job *jobs.Job) ProofResult {
	res := ProofResult{Language: pl.Proof.Language, Part: pl.Proof.Part, Verdicts: map[string]int{}}
	unchecked := func(reason string) ProofResult {
		res.Outcome, res.Reason = ProofUnchecked, reason
		return res
	}
	if job.State == jobs.Failed {
		return unchecked("judge_error")
	}
	compiled := false
	for _, c := range job.Result.Compile {
		switch {
		case c.Verdict == string(judge.CompileError) && c.Role == judgeapi.RoleReference:
			res.Outcome, res.Reason, res.Compiler = ProofFailed, "compile_error", compilerLines(c.Output)
			return res
		case c.Verdict == string(judge.CompileError):
			return unchecked("internal_error") // the checker, not the language
		case c.Role == judgeapi.RoleReference:
			compiled = true
		}
	}
	if pl.Proof.Part == ProofPartStarter {
		if !compiled {
			return unchecked("no_report")
		}
		res.Outcome = ProofPassed
		return res
	}
	res.Total = len(pl.Tests)
	seen := 0
	for _, t := range job.Result.Tests {
		if t.Role != judgeapi.RoleReference {
			continue
		}
		seen++
		switch t.Verdict {
		case string(judge.InternalError):
			return unchecked("internal_error")
		case string(judge.Accepted):
		default:
			res.Failed++
			res.Verdicts[t.Verdict]++
			if len(res.Tests) < proofTestsKept {
				res.Tests = append(res.Tests, ProofTest{t.Idx, t.Verdict})
			}
		}
	}
	if seen < res.Total {
		res.Failed, res.Verdicts, res.Tests = 0, map[string]int{}, nil
		return unchecked("no_report")
	}
	if res.Failed == 0 {
		res.Outcome = ProofPassed
		return res
	}
	worst := ""
	for _, v := range proofVerdictOrder {
		if res.Verdicts[v] > res.Verdicts[worst] {
			worst = v
		}
	}
	if worst == "" { // a verdict the table does not know
		worst = slices.Sorted(maps.Keys(res.Verdicts))[0]
	}
	res.Outcome, res.Reason = ProofFailed, proofReasons[worst]
	if res.Reason == "" {
		res.Reason = strings.ToLower(worst)
	}
	return res
}

// compilerLines is the first lines of a compiler's output, kept short.
func compilerLines(out string) string {
	lines := strings.SplitN(out, "\n", proofCompilerLines+1)
	if len(lines) > proofCompilerLines {
		lines = lines[:proofCompilerLines]
	}
	s := strings.Join(lines, "\n")
	if len(s) > proofCompilerBytes {
		s = s[:proofCompilerBytes]
	}
	return strings.ToValidUTF8(s, "")
}

// roundJob is one closed job of a round, with what apply needs of it.
type roundJob struct {
	proof  *Proof
	result ProofResult
}

// finishProof records a proof job's result and, when it is the last of its
// round, applies the round. It must not return an error for anything but a
// database fault: an error from Finish leaves the job leased.
func (k ValidateKind) finishProof(ctx context.Context, tx *sql.Tx, job *jobs.Job, pl proofValidate) error {
	if gone, err := closeIfDeleted(ctx, tx, pl.ProblemID, job); gone || err != nil {
		if gone {
			job.Result.Summary, _ = json.Marshal(proofSummary{Result: ProofResult{Language: pl.Proof.Language, Part: pl.Proof.Part,
				Outcome: ProofUnchecked, Reason: "deleted", Verdicts: map[string]int{}}})
		}
		return err
	}
	sum := proofSummary{Result: classify(pl, job)}
	var open int
	if err := tx.QueryRowContext(ctx, openSiblingsSQL, ProofPriority, job.ID, pl.ProblemID, pl.Proof.Round).Scan(&open); err != nil {
		return err
	}
	if open == 0 {
		round, err := roundJobs(ctx, tx, pl)
		if err != nil {
			return err
		}
		round = append(round, roundJob{pl.Proof, sum.Result})
		if sum.Applied, err = k.applyProof(ctx, tx, pl, round); err != nil {
			return err
		}
	}
	b, err := json.Marshal(sum)
	job.Result.Summary = b
	return err
}

// roundJobs reads the closed jobs of the round the payload belongs to.
func roundJobs(ctx context.Context, tx *sql.Tx, pl proofValidate) ([]roundJob, error) {
	rows, err := tx.QueryContext(ctx, roundJobsSQL, ProofPriority, pl.ProblemID, pl.Proof.Round)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []roundJob
	for rows.Next() {
		var state, payload, summary string
		if err := rows.Scan(&state, &payload, &summary); err != nil {
			return nil, err
		}
		sib, err := decodeProof(json.RawMessage(payload))
		if err != nil {
			return nil, err
		}
		var sm proofSummary
		if json.Unmarshal([]byte(summary), &sm) != nil || sm.Result.Outcome == "" {
			// Closed without Finish (cancelled by the problem delete): nothing ran.
			sm.Result = ProofResult{Language: sib.Proof.Language, Part: sib.Proof.Part,
				Outcome: ProofUnchecked, Reason: "judge_error", Verdicts: map[string]int{}}
		}
		out = append(out, roundJob{sib.Proof, sm.Result})
	}
	return out, rows.Err()
}

// applyProof applies a finished round, in the transaction of the job that
// closed it. Expected refusals are recorded in Applied.Error; only a
// database fault is returned.
func (k ValidateKind) applyProof(ctx context.Context, tx *sql.Tx, pl proofValidate, round []roundJob) (*Applied, error) {
	p, err := load(ctx, tx, pl.ProblemID)
	if errors.Is(err, ErrNotFound) {
		return &Applied{Discarded: "gone"}, nil
	}
	if err != nil {
		return nil, err
	}
	if p.ContentHash != pl.Proof.ContentHash {
		return &Applied{Discarded: "changed"}, nil
	}
	app := &Applied{}
	// One outcome per language: failed beats unchecked beats passed. A
	// language whose starting code changed since the round began is left out.
	rank := map[string]int{ProofPassed: 1, ProofUnchecked: 2, ProofFailed: 3}
	outcome := map[string]string{}
	for _, j := range round {
		lang := j.proof.Language
		if SHA([]byte(p.Starters[lang])) != j.proof.StarterSHA {
			if !slices.Contains(app.Stale, lang) {
				app.Stale = append(app.Stale, lang)
			}
			continue
		}
		if rank[j.result.Outcome] > rank[outcome[lang]] {
			outcome[lang] = j.result.Outcome
		}
	}
	sort.Strings(app.Stale)
	switch pl.Proof.Source {
	case ProofSourceWizard:
		err = k.applyWizard(ctx, tx, p, pl.Proof.By, outcome, app)
	case ProofSourceFix:
		err = k.applyFix(ctx, tx, p, pl.Proof, outcome, app)
	}
	return app, err
}

// applyWizard removes the languages that did not pass, then queues the
// validation. Nothing is removed when the reference language itself did not
// pass: its code is what every test output came from.
func (k ValidateKind) applyWizard(ctx context.Context, tx *sql.Tx, p *Problem, by int64, outcome map[string]string, app *Applied) error {
	switch outcome[p.Reference.Language] {
	case ProofFailed:
		app.Kept = "reference_failed"
		return nil
	case ProofUnchecked:
		app.Kept = "reference_unchecked"
		return nil
	}
	var names []string
	for _, lang := range slices.Sorted(maps.Keys(outcome)) {
		if outcome[lang] == ProofPassed || !p.Offers(lang) {
			continue
		}
		delete(p.LockedTop, lang)
		delete(p.LockedBottom, lang)
		delete(p.Starters, lang)
		app.Removed = append(app.Removed, lang)
		names = append(names, judge.Languages[lang].Name)
	}
	if len(app.Removed) > 0 {
		note := RevisionNote{Reason: "Languages that did not pass their check were removed: " + strings.Join(names, ", ") + "."}
		if ok, err := k.saveApplied(ctx, tx, p, note, app); !ok {
			app.Removed = nil
			return err
		}
	}
	return k.queueValidation(ctx, tx, p, by, app)
}

// applyFix adds the language a fix wrote, only when every one of its jobs
// passed.
func (k ValidateKind) applyFix(ctx context.Context, tx *sql.Tx, p *Problem, pr *Proof, outcome map[string]string, app *Applied) error {
	lang := pr.Language
	if outcome[lang] != ProofPassed {
		app.Kept = "not_added"
		return nil
	}
	if pr.Parts == nil {
		app.Error = "The AI's code was not kept with this check, so nothing was changed."
		return nil
	}
	var parts [3]string
	for i, sha := range []string{pr.Parts.Top, pr.Parts.Starter, pr.Parts.Bottom} {
		b, err := k.Store.Read(sha, MaxStarter+1)
		if err != nil {
			app.Error = "The AI's code could not be read back, so nothing was changed."
			return nil
		}
		parts[i] = string(b)
	}
	p.LockedTop[lang], p.Starters[lang], p.LockedBottom[lang] = parts[0], parts[1], parts[2]
	note := RevisionNote{By: pr.By, Reason: judge.Languages[lang].Name + " added after the AI's version passed every test."}
	if ok, err := k.saveApplied(ctx, tx, p, note, app); !ok {
		return err
	}
	app.Added = lang
	return k.queueValidation(ctx, tx, p, pr.By, app)
}

// saveApplied runs the field rules and the ordinary save. A refusal the
// organiser can act on is recorded in app.Error and reported as not ok; only
// a database fault is returned.
func (k ValidateKind) saveApplied(ctx context.Context, tx *sql.Tx, p *Problem, note RevisionNote, app *Applied) (bool, error) {
	if errs := checkFields(p); len(errs) > 0 {
		app.Error = errs.Error()
		return false, nil
	}
	err := (&Service{Now: k.Now}).save(ctx, tx, p, note)
	// ErrDeleted, ErrModeLocked and a slug clash cannot happen here: the row was
	// loaded in this transaction and closeIfDeleted ran, the mode is unchanged
	// (store.go checkModeChange) and the slug is the row's own. Any other error
	// is the database's.
	var locked *LanguageLockedError // a student began the language while the round ran
	if errors.As(err, &locked) {
		app.Error = err.Error()
		return false, nil
	}
	return err == nil, err
}

// queueValidation queues the normal validation of p once nothing is left to
// prove, when the queue is wired and the problem is ready.
func (k ValidateKind) queueValidation(ctx context.Context, tx *sql.Tx, p *Problem, by int64, app *Applied) error {
	if k.Jobs == nil || len(p.Problems()) > 0 {
		return nil
	}
	var err error
	app.Validating, err = queue{k.Jobs, k.Store, k.Now}.enqueueValidation(ctx, tx, p, by)
	return err
}
