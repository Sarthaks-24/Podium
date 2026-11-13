package problems

// Proving the languages of a function problem on the judge (v2.1.0, stage
// F). A proof is a set of validate-kind jobs, one round per request: for
// each language a solution job (the language's locked top, a solution and
// its locked bottom, run on every test) and a starter job (the untouched
// starting code, run on every test: it must fail at least one, or the locked
// code does not depend on the student's function). A proof job carries a
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

// ReasonIgnoresFunction is the reason of a starter job that was accepted on
// every test: the locked code passes without the student's function.
const ReasonIgnoresFunction = "ignores_function"

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
	// Variant is the form of a Java starting code with two forms that this
	// job proves, such as "ArrayList<Integer>"; empty everywhere else.
	Variant string `json:"variant,omitempty"`
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

// ProofJavaArrayListKey is the key in ProofSpec.Solutions of the Java solution
// written with an ArrayList, used when the starting code holds two forms; the
// key "java21" then holds the solution written with an array.
const ProofJavaArrayListKey = "java21/arraylist"

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
	// Variant is the form the job proved, empty for a language with one form.
	Variant string `json:"variant,omitempty"`
	// Reason is one of judge_error, internal_error, no_report, compile_error,
	// wrong_answer, runtime_error, time_limit, memory_limit, output_limit,
	// skipped, ignores_function or deleted; empty for a pass.
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
	// Hash is the content hash the round's own save produced, so a result of that
	// round still reads as current after it removed or added a language.
	Hash string `json:"hash,omitempty"`
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
// so that job would pass by construction), and a starter job for every
// language. A check or a fix covers spec.Language alone; a check has no
// starter job and a fix takes its three parts from spec.Parts. It refuses a
// round that could not be applied: a whole-program problem, an unknown source
// or language, a fix without its parts.
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
	enqueue := func(lang, part, variant, source string) error {
		sha, err := q.blobs.Put(ctx, tx, []byte(source))
		if err != nil {
			return err
		}
		pl := proofValidate{ValidatePayload: base, Proof: &Proof{
			Round: round, Source: spec.Source, Language: lang, Part: part, Variant: variant,
			ContentHash: p.ContentHash, StarterSHA: SHA([]byte(p.Starters[lang])), By: by, Parts: spec.Parts,
		}}
		pl.Programs.Reference = judgeapi.Program{Language: lang, SourceSHA: sha}
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
		// A wizard round proves each form of a two-form starting code on its own.
		var shared string
		forms := []Form{{Code: starter}}
		if two, ok := twoForms(lang, starter); ok && spec.Source == ProofSourceWizard {
			shared, forms = two.Shared, two.Forms
		}
		if spec.Source != ProofSourceWizard || lang != p.Reference.Language {
			for _, f := range forms {
				key := lang
				if strings.HasPrefix(f.Name, "ArrayList<") {
					key = ProofJavaArrayListKey
				}
				sha, ok := spec.Solutions[key]
				if !ok && key == lang {
					return 0, fmt.Errorf("proof: no solution for %s", lang)
				} else if !ok {
					return 0, fmt.Errorf("proof: no ArrayList solution for %s", lang)
				}
				solution, err := read(sha, MaxSource)
				if err != nil {
					return 0, err
				}
				if err := enqueue(lang, ProofPartSolution, f.Name, Compose(top, solution, bottom).Source); err != nil {
					return 0, err
				}
			}
		}
		if spec.Source != ProofSourceCheck {
			for _, f := range forms {
				if err := enqueue(lang, ProofPartStarter, f.Name, Compose(top, shared+f.Code, bottom).Source); err != nil {
					return 0, err
				}
			}
		}
	}
	return n, nil
}

// twoForms splits a Java starting code that holds an array form and an
// ArrayList form. Any other language or starting code is not one.
func twoForms(lang, starter string) (Choices, bool) {
	if lang != "java21" {
		return Choices{}, false
	}
	c, err := SplitStarter(starter)
	return c, err == nil && len(c.Forms) == 2
}

// classify turns a closed proof job into its result.
func classify(pl proofValidate, job *jobs.Job) ProofResult {
	res := ProofResult{Language: pl.Proof.Language, Part: pl.Proof.Part, Variant: pl.Proof.Variant, Verdicts: map[string]int{}}
	unchecked := func(reason string) ProofResult {
		res.Outcome, res.Reason = ProofUnchecked, reason
		return res
	}
	if job.State == jobs.Failed {
		return unchecked("judge_error")
	}
	for _, c := range job.Result.Compile {
		switch {
		case c.Verdict == string(judge.CompileError) && c.Role == judgeapi.RoleReference:
			res.Outcome, res.Reason, res.Compiler = ProofFailed, "compile_error", compilerLines(c.Output)
			return res
		case c.Verdict == string(judge.CompileError):
			return unchecked("internal_error") // the checker, not the language
		}
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
	starter := pl.Proof.Part == ProofPartStarter
	if seen < res.Total || (starter && res.Total == 0) {
		res.Failed, res.Verdicts, res.Tests = 0, map[string]int{}, nil
		return unchecked("no_report")
	}
	if starter {
		// The untouched starting code must get at least one test wrong, with any
		// verdict: a crash or a timeout is as good as a wrong answer. If it is
		// accepted on every test, the locked code does not use the student's function.
		failed := res.Failed
		res.Failed, res.Verdicts, res.Tests = 0, map[string]int{}, nil
		if failed == 0 {
			res.Outcome, res.Reason = ProofFailed, ReasonIgnoresFunction
		} else {
			res.Outcome = ProofPassed
		}
		return res
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
			job.Result.Summary, _ = json.Marshal(proofSummary{Result: ProofResult{Language: pl.Proof.Language, Part: pl.Proof.Part, Variant: pl.Proof.Variant,
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
		out = append(out, roundJob{sib.Proof, summaryOf(sib, summary).Result})
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
	err := (&Service{Now: k.Now, Blobs: k.Store}).save(ctx, tx, p, note)
	// ErrDeleted, ErrModeLocked and a slug clash cannot happen here: the row was
	// loaded in this transaction and closeIfDeleted ran, the mode is unchanged
	// (store.go checkModeChange) and the slug is the row's own. Any other error
	// is the database's.
	var locked *LanguageLockedError // a student began the language while the round ran
	if errors.As(err, &locked) {
		app.Error = err.Error()
		return false, nil
	}
	if err == nil {
		app.Hash = p.ContentHash
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

// ErrChecking refuses an action while a proof is open.
var ErrChecking = errors.New("Podium is still checking the languages of this problem.")

// proofSolutionLanguages are the languages of a wizard draft that need a
// solution to be proved: every offered language but the reference language,
// whose outputs come from the reference.
func proofSolutionLanguages(p *Problem) []string {
	return slices.DeleteFunc(p.Offered(), func(lang string) bool { return lang == p.Reference.Language })
}

// proofLanguages are the languages a function problem can offer, in the
// order the editor lists them.
var proofLanguages = []string{"cpp17", "java21", "python3", "node22"}

// BadLanguageMessage is the sentence for a language id the routes do not know.
const BadLanguageMessage = "Choose C++17, Java 21, Python 3 or JavaScript."

const (
	// proofStateSQL reads the closed proof jobs of a problem, newest first.
	proofStateSQL = `SELECT payload, summary FROM jobs WHERE state IN ('done','failed') AND priority = ?
		AND kind = 'validate' AND json_extract(payload, '$.problem_id') = ? ORDER BY id DESC LIMIT 200`
	// openProofJobsSQL reads the open proof jobs of a problem.
	openProofJobsSQL = `SELECT state, payload FROM jobs WHERE state IN ('queued','leased') AND priority = ?
		AND kind = 'validate' AND json_extract(payload, '$.problem_id') = ? ORDER BY id`
	// sampleReadBytes cuts a sample sent to a model.
	sampleReadBytes = 4 << 10
)

// ErrNotFunction refuses a language action on a whole-program problem.
var ErrNotFunction = errors.New("Only a function problem has languages to check.")

// ErrChanged says the problem changed while the AI was working.
var ErrChanged = errors.New("The problem changed while the AI was working. Press Fix again.")

// LanguageStateError refuses an action because of whether a language is
// offered: a check needs it offered, a fix needs it not offered.
type LanguageStateError struct {
	Language string
	// Offered is true for a fix on a language that is already offered.
	Offered bool
}

func (e *LanguageStateError) Error() string {
	name := judge.Languages[e.Language].Name
	if e.Offered {
		return name + " is already offered. Use Check " + name + " to test it."
	}
	return name + " is not offered. Write its locked code and starting code and save, or use Fix " + name + " with AI."
}

// ProofLanguage is one language's line of the proof state.
type ProofLanguage struct {
	Language string `json:"language"`
	// Offered is live: the language has a locked bottom now.
	Offered bool `json:"offered"`
	// Result is passed, failed, unchecked or none (never checked).
	Result string `json:"result"`
	// Variant names the form of the worst job when the language has two.
	Variant  string      `json:"variant"`
	Reason   string      `json:"reason"`
	Failed   int         `json:"failed"`
	Total    int         `json:"total"`
	Compiler string      `json:"compiler"`
	Tests    []ProofTest `json:"tests"`
	// Current is false when the result is from other content than the problem's now.
	Current bool `json:"current"`
	// Source is what asked for the round: wizard, fix or check.
	Source string `json:"source"`
}

// ProofProgress counts the jobs of the open round.
type ProofProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
	// Current is the language of a running job, or empty.
	Current string `json:"current"`
}

// ProofState is what the proof route returns: whether the problem is being
// checked, how far, each language's latest result, and what the newest
// finished round did.
type ProofState struct {
	Checking  bool            `json:"checking"`
	Progress  *ProofProgress  `json:"progress,omitempty"`
	Languages []ProofLanguage `json:"languages"`
	Applied   *Applied        `json:"applied,omitempty"`
	ServerNow int64           `json:"server_now"`
}

// latestProof is the newest round that touched a language.
type latestProof struct {
	round, source, hash string
	// savedHash is the content hash the round's own save produced.
	savedHash string
	results   []ProofResult // newest first
}

// worst is the result that stands for the language: failed beats unchecked
// beats passed, and the solution job beats the starter job on a tie.
func (l *latestProof) worst() ProofResult {
	rank := map[string]int{ProofPassed: 1, ProofUnchecked: 2, ProofFailed: 3}
	var out ProofResult
	for i := len(l.results) - 1; i >= 0; i-- {
		if r := l.results[i]; rank[r.Outcome] > rank[out.Outcome] {
			out = r
		}
	}
	return out
}

// latestProofs reads each language's newest round of results and the newest
// Applied record. Reading one round per language keeps an earlier round's
// results out of a later one's.
func latestProofs(ctx context.Context, q queryer, id int64) (map[string]*latestProof, *Applied, error) {
	rows, err := q.QueryContext(ctx, proofStateSQL, ProofPriority, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	latest := map[string]*latestProof{}
	saved := map[string]string{} // round to the hash its save produced
	var applied *Applied
	for rows.Next() {
		var payload, summary string
		if err := rows.Scan(&payload, &summary); err != nil {
			return nil, nil, err
		}
		pl, err := decodeProof(json.RawMessage(payload))
		if err != nil {
			return nil, nil, err
		}
		if pl.Proof == nil {
			continue
		}
		sm := summaryOf(pl, summary)
		if sm.Applied != nil {
			if applied == nil {
				applied = sm.Applied
			}
			saved[pl.Proof.Round] = sm.Applied.Hash
		}
		l := latest[pl.Proof.Language]
		if l == nil {
			l = &latestProof{round: pl.Proof.Round, source: pl.Proof.Source, hash: pl.Proof.ContentHash}
			latest[pl.Proof.Language] = l
		}
		if l.round == pl.Proof.Round {
			l.results = append(l.results, sm.Result)
		}
	}
	for _, l := range latest {
		l.savedHash = saved[l.round]
	}
	return latest, applied, rows.Err()
}

// summaryOf reads a proof job's stored summary. A job closed without Finish
// (cancelled by a problem delete) has none: nothing ran, so it counts as not
// checked.
func summaryOf(pl proofValidate, summary string) proofSummary {
	var sm proofSummary
	if json.Unmarshal([]byte(summary), &sm) != nil || sm.Result.Outcome == "" {
		sm.Result = ProofResult{Language: pl.Proof.Language, Part: pl.Proof.Part, Variant: pl.Proof.Variant,
			Outcome: ProofUnchecked, Reason: "judge_error", Verdicts: map[string]int{}}
	}
	return sm
}

// ProofState returns the derived state of a problem's language checks. A
// whole-program problem has no languages to check and lists none.
func (s *Service) ProofState(ctx context.Context, id int64) (*ProofState, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if p.Deleted {
		return nil, ErrDeleted
	}
	st := &ProofState{Languages: []ProofLanguage{}}
	if !p.IsFunction() {
		return st, nil
	}
	if st.Checking, err = OpenProof(ctx, tx, id); err != nil {
		return nil, err
	}
	if st.Checking {
		if st.Progress, err = proofProgress(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	latest, applied, err := latestProofs(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	st.Applied = applied
	for _, lang := range proofLanguages {
		l := ProofLanguage{Language: lang, Offered: p.Offers(lang), Result: "none", Tests: []ProofTest{}}
		if r := latest[lang]; r != nil {
			res := r.worst()
			l.Result, l.Variant, l.Reason, l.Failed, l.Total, l.Compiler = res.Outcome, res.Variant, res.Reason, res.Failed, res.Total, res.Compiler
			if res.Tests != nil {
				l.Tests = res.Tests
			}
			l.Current = r.hash == p.ContentHash || (r.savedHash != "" && r.savedHash == p.ContentHash)
			l.Source = r.source
		}
		st.Languages = append(st.Languages, l)
	}
	return st, nil
}

// proofProgress counts the open round's closed and open jobs. Before the
// chain has queued any proof job it is 0 of 0.
func proofProgress(ctx context.Context, tx *sql.Tx, id int64) (*ProofProgress, error) {
	rows, err := tx.QueryContext(ctx, openProofJobsSQL, ProofPriority, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pr, round := &ProofProgress{}, ""
	for rows.Next() {
		var state, payload string
		if err := rows.Scan(&state, &payload); err != nil {
			return nil, err
		}
		pl, err := decodeProof(json.RawMessage(payload))
		if err != nil {
			return nil, err
		}
		if pl.Proof == nil || (round != "" && pl.Proof.Round != round) {
			continue
		}
		round = pl.Proof.Round
		pr.Total++
		if state == "leased" && pr.Current == "" {
			pr.Current = pl.Proof.Language
		}
	}
	if err := rows.Err(); err != nil || round == "" {
		return pr, err
	}
	rows.Close()
	closed, err := roundJobs(ctx, tx, proofValidate{ValidatePayload: judgeapi.ValidatePayload{ProblemID: id}, Proof: &Proof{Round: round}})
	pr.Done = len(closed)
	pr.Total += len(closed)
	return pr, err
}

// LanguageParts are the three parts of a language, as text.
type LanguageParts struct{ Top, Starter, Bottom string }

// ProofRequest asks for one proof round of one language.
type ProofRequest struct {
	Language string
	// Solution is the student's middle part, composed with the locked parts.
	Solution string
	// Parts, when set, make this a fix: the language is not offered, the
	// parts are its code, and they are added only if the round passes.
	// Nil makes it a check of an offered language, which changes nothing.
	Parts *LanguageParts
	// WantHash, when set, is the content hash the caller read; a different
	// one refuses the request with ErrChanged.
	WantHash string
}

// RequestLanguageProof queues one proof round for one language, in one
// transaction with the check that no round is open, so two presses cannot
// both queue. The solution and a fix's parts are stored only as blobs named
// by the jobs: no table holds their text.
func (s *Service) RequestLanguageProof(ctx context.Context, id, userID int64, req ProofRequest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return err
	}
	fix := req.Parts != nil
	switch {
	case p.Deleted:
		return ErrDeleted
	case !p.IsFunction():
		return ErrNotFunction
	case p.Offers(req.Language) == fix:
		return &LanguageStateError{Language: req.Language, Offered: fix}
	case req.WantHash != "" && p.ContentHash != req.WantHash:
		return ErrChanged
	}
	if open, err := OpenProof(ctx, tx, id); err != nil {
		return err
	} else if open {
		return ErrChecking
	}
	if probs := p.Problems(); len(probs) > 0 {
		return &NotReadyError{probs}
	}
	q := s.queue()
	sha, err := q.blobs.Put(ctx, tx, []byte(req.Solution))
	if err != nil {
		return err
	}
	spec := ProofSpec{Source: ProofSourceCheck, Language: req.Language, Solutions: map[string]string{req.Language: sha}}
	if fix {
		spec.Source, spec.Parts = ProofSourceFix, &ProofParts{}
		for _, part := range []struct {
			text string
			sha  *string
		}{{req.Parts.Top, &spec.Parts.Top}, {req.Parts.Starter, &spec.Parts.Starter}, {req.Parts.Bottom, &spec.Parts.Bottom}} {
			if *part.sha, err = q.blobs.Put(ctx, tx, []byte(part.text)); err != nil {
				return err
			}
		}
	}
	if _, err := q.enqueueProofs(ctx, tx, p, userID, spec); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Jobs.Wake()
	return nil
}

// FixSnapshot is what the fix route gives the model: the statement, the
// reference, the sample tests and the language's last failure. Hidden tests
// are never opened.
type FixSnapshot struct {
	ContentHash string
	Statement   string
	Reference   Source
	Samples     []SampleCase
	// Failure is the language's latest result when it did not pass.
	Failure *ProofResult
}

// FixSnapshot loads what the AI needs to write one language again. It
// refuses a problem that is deleted, whole-program, already offering the
// language, being checked or not ready.
func (s *Service) FixSnapshot(ctx context.Context, id int64, language string) (*FixSnapshot, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case p.Deleted:
		return nil, ErrDeleted
	case !p.IsFunction():
		return nil, ErrNotFunction
	case p.Offers(language):
		return nil, &LanguageStateError{Language: language, Offered: true}
	}
	if open, err := OpenProof(ctx, tx, id); err != nil {
		return nil, err
	} else if open {
		return nil, ErrChecking
	}
	if probs := p.Problems(); len(probs) > 0 {
		return nil, &NotReadyError{probs}
	}
	snap := &FixSnapshot{ContentHash: p.ContentHash, Statement: p.Statement, Reference: p.Reference}
	for _, t := range p.Tests {
		if !p.IsSample(t.Idx) {
			continue
		}
		in, err := s.Blobs.Read(t.InputSHA, sampleReadBytes)
		if err != nil {
			return nil, err
		}
		out, err := s.Blobs.Read(t.OutputSHA, sampleReadBytes)
		if err != nil {
			return nil, err
		}
		snap.Samples = append(snap.Samples, SampleCase{Input: string(in), Output: string(out)})
	}
	latest, _, err := latestProofs(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if l := latest[language]; l != nil {
		if res := l.worst(); res.Outcome != ProofPassed {
			snap.Failure = &res
		}
	}
	return snap, nil
}
