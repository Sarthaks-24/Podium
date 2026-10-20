package problems

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// GeneratePriority runs output generation just ahead of validation, behind
// every contest job.
const GeneratePriority = 85

// Generation is an output generation run as the editor shows it.
type Generation struct {
	ID         int64
	State      string // queued, running, done, error
	All        bool
	QueuedAt   time.Time
	FinishedAt time.Time
	Compile    []judgeapi.CompileResult
	Tests      []judgeapi.TestResult
	Summary    *GenerationSummary
	Error      string
}

// GenerationSummary is what a finished generation changed.
type GenerationSummary struct {
	Applied  int      `json:"applied"`
	Problems []string `json:"problems"`
	// Validating is set when this generation queued a validation (the
	// generate inputs, outputs, validate chain).
	Validating bool `json:"validating,omitempty"`
}

// RequestGeneration queues a run of the reference solution on the
// problem's inputs: those missing an output, or every input when all is
// set (the editor confirms that first, since it replaces outputs).
func (s *Service) RequestGeneration(ctx context.Context, id, userID int64, all bool) (*Generation, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if len(p.Tests) == 0 {
		return nil, &NotReadyError{[]string{"Add tests before generating outputs."}}
	}
	if strings.TrimSpace(p.Reference.Source) == "" {
		return nil, &NotReadyError{[]string{"Write the reference solution before generating outputs."}}
	}
	if err := pendingGeneration(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := s.queue().enqueueGeneration(ctx, tx, p, userID, all, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.LatestGeneration(ctx, id)
}

// pendingGeneration refuses a new input or output generation while one of
// either is waiting or running for the problem: each replaces files the
// other reads.
func pendingGeneration(ctx context.Context, tx *sql.Tx, id int64) error {
	var outputs, inputs int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM generations g JOIN jobs j ON j.id = g.job_id WHERE g.problem_id = ? AND j.state IN ('queued', 'leased')),
		(SELECT count(*) FROM input_generations g JOIN jobs j ON j.id = g.job_id WHERE g.problem_id = ? AND j.state IN ('queued', 'leased'))`,
		id, id).Scan(&outputs, &inputs); err != nil {
		return err
	}
	switch {
	case outputs > 0:
		return &NotReadyError{[]string{"Outputs are already being generated. Wait for that run to finish."}}
	case inputs > 0:
		return &NotReadyError{[]string{"Inputs are being generated. Wait for that run to finish."}}
	}
	return nil
}

// enqueueGeneration queues output generation for p inside tx: tests
// missing an output, or every test when all is set. thenValidate asks the
// job's Finish to queue a validation once every output arrived.
func (q queue) enqueueGeneration(ctx context.Context, tx *sql.Tx, p *Problem, userID int64, all, thenValidate bool) error {
	pl := judgeapi.GeneratePayload{ProblemID: p.ID, ContentHash: p.ContentHash, TimeLimitMS: p.TimeLimitMS,
		MemoryLimitMB: p.MemoryLimitMB, All: all, ThenValidate: thenValidate}
	for _, t := range p.Tests {
		if all || t.OutputSHA == "" {
			pl.Inputs = append(pl.Inputs, judgeapi.RunInput{Idx: t.Idx, InputSHA: t.InputSHA})
		}
	}
	if len(pl.Inputs) == 0 {
		return &NotReadyError{[]string{"Every test already has an output. Use Regenerate all outputs to replace them."}}
	}
	sha, err := q.blobs.Put(ctx, tx, []byte(p.Reference.Source))
	if err != nil {
		return err
	}
	pl.Program = judgeapi.Program{Language: p.Reference.Language, SourceSHA: sha}
	jobID, err := q.jobs.Enqueue(ctx, tx, judgeapi.KindGenerate, GeneratePriority, pl, userID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO generations (problem_id, job_id, all_outputs, requested_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, jobID, all, nullUser(userID), q.at())
	return err
}

// LatestGeneration returns the problem's most recent generation run.
func (s *Service) LatestGeneration(ctx context.Context, id int64) (*Generation, error) {
	var g Generation
	var jobID, created int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, job_id, all_outputs, created_at FROM generations WHERE problem_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&g.ID, &jobID, &g.All, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.QueuedAt = time.UnixMilli(created)
	job, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	g.Compile, g.Tests, g.FinishedAt = job.Result.Compile, job.Result.Tests, job.FinishedAt
	switch job.State {
	case jobs.Queued:
		g.State = ValidationQueued
	case jobs.Leased:
		g.State = ValidationRunning
	case jobs.Failed:
		g.State, g.Error = ValidationError, job.Error
	case jobs.Done:
		g.State = "done"
		var sm GenerationSummary
		if json.Unmarshal(job.Result.Summary, &sm) == nil {
			g.Summary = &sm
		}
	}
	return &g, nil
}

// GenerateKind is the jobs.Kind for output generation. Uploaded outputs
// land in the blob store through the judge API's PUT route; Finish checks
// each one exists before using it.
type GenerateKind struct {
	Store Blobs
	// Jobs queues the validation that follows a generation started by
	// generate_inputs. When nil, nothing is chained.
	Jobs *jobs.Service
	Now  func() time.Time
}

// MaxUploads lets the worker upload one output per input, with a little
// room for a retried upload.
func (GenerateKind) MaxUploads(raw json.RawMessage) int {
	pl, err := decodeGenerate(raw)
	if err != nil {
		return 0
	}
	return len(pl.Inputs) + 2
}

func decodeGenerate(raw json.RawMessage) (judgeapi.GeneratePayload, error) {
	var pl judgeapi.GeneratePayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("generate payload: %w", err)
	}
	return pl, nil
}

// CheckReport refuses results for inputs the job does not have, roles
// other than the reference program, and output hashes that are not hashes.
func (GenerateKind) CheckReport(raw json.RawMessage, r judgeapi.Report) error {
	pl, err := decodeGenerate(raw)
	if err != nil {
		return err
	}
	inputs := map[int]bool{}
	for _, in := range pl.Inputs {
		inputs[in.Idx] = true
	}
	for _, c := range r.Compile {
		if c.Role != judgeapi.RoleProgram || (c.Verdict != string(judge.OK) && c.Verdict != string(judge.CompileError)) || len(c.Output) > judgeapi.MaxCompileOutput {
			return jobs.Badf("compile result %q %q", jobs.Clip(c.Role), jobs.Clip(c.Verdict))
		}
	}
	seen := map[int]bool{}
	for _, t := range r.Results {
		switch {
		case !inputs[t.Idx] || seen[t.Idx]:
			return jobs.Badf("result for input %d", t.Idx)
		case t.Role != judgeapi.RoleProgram || !judgeapi.Verdicts[t.Verdict]:
			return jobs.Badf("result role %q verdict %q", jobs.Clip(t.Role), jobs.Clip(t.Verdict))
		case t.OutputSHA != "" && !judgeapi.ValidSHA(t.OutputSHA):
			return jobs.Badf("output hash on input %d", t.Idx)
		case t.Output != "" || len(t.Message) > judgeapi.MaxMessage:
			return jobs.Badf("generate results carry no output text")
		case t.TimeMS < 0 || t.TimeMS > 600_000 || t.MemoryKB < 0 || t.MemoryKB > 8<<20:
			return jobs.Badf("impossible time or memory on input %d", t.Idx)
		}
		seen[t.Idx] = true
	}
	return nil
}

// Blobs lists the reference source and the inputs.
func (GenerateKind) Blobs(raw json.RawMessage) ([]string, error) {
	pl, err := decodeGenerate(raw)
	if err != nil {
		return nil, err
	}
	out := []string{pl.Program.SourceSHA}
	for _, in := range pl.Inputs {
		out = append(out, in.InputSHA)
	}
	return out, nil
}

// MaxDuration is a compile plus each input at twice its limit, with slack.
func (GenerateKind) MaxDuration(raw json.RawMessage) time.Duration {
	pl, err := decodeGenerate(raw)
	if err != nil {
		return 10 * time.Minute
	}
	d := 2*time.Minute + time.Duration(len(pl.Inputs))*(6*time.Duration(pl.TimeLimitMS)*time.Millisecond+30*time.Second)
	return min(d, time.Hour)
}

// Finish writes the generated outputs into the problem's tests, if the
// problem has not changed since the job was queued.
func (k GenerateKind) Finish(ctx context.Context, tx *sql.Tx, job *jobs.Job) error {
	if job.State == jobs.Failed {
		return nil
	}
	pl, err := decodeGenerate(job.Payload)
	if err != nil {
		return err
	}
	sm := GenerationSummary{Problems: []string{}}
	defer func() {
		job.Result.Summary, _ = json.Marshal(sm)
	}()
	for _, c := range job.Result.Compile {
		if c.Verdict == string(judge.CompileError) {
			sm.Problems = append(sm.Problems, "The reference solution does not compile, so no outputs were generated.")
			return nil
		}
	}
	p, err := load(ctx, tx, pl.ProblemID)
	if errors.Is(err, ErrNotFound) {
		sm.Problems = append(sm.Problems, "The problem no longer exists.")
		return nil
	}
	if err != nil {
		return err
	}
	if p.ContentHash != pl.ContentHash {
		sm.Problems = append(sm.Problems, "The problem changed while outputs were being generated. Generate them again.")
		return nil
	}
	got := map[int]judgeapi.TestResult{}
	for _, t := range job.Result.Tests {
		got[t.Idx] = t
	}
	for _, in := range pl.Inputs {
		r, ok := got[in.Idx]
		switch {
		case !ok:
			sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the judge did not report an output.", in.Idx))
			continue
		case r.Verdict != string(judge.OK) && r.Verdict != string(judge.Accepted):
			sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the reference solution got %s, so its output was not generated.", in.Idx, verdictWords[r.Verdict]))
			continue
		}
		f, err := k.Store.Open(r.OutputSHA)
		if err != nil {
			sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the output did not arrive.", in.Idx))
			continue
		}
		st, err := f.Stat()
		f.Close()
		if err != nil || !st.Mode().IsRegular() {
			sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the output did not arrive.", in.Idx))
			continue
		}
		if err := k.Store.Record(ctx, tx, r.OutputSHA, st.Size()); err != nil {
			return err
		}
		for i := range p.Tests {
			if p.Tests[i].Idx == in.Idx {
				p.Tests[i].OutputSHA, p.Tests[i].OutputSize = r.OutputSHA, st.Size()
				sm.Applied++
			}
		}
	}
	if sm.Applied == 0 {
		return nil
	}
	svc := Service{DB: nil, Now: k.Now}
	if err := svc.save(ctx, tx, p, RevisionNote{Reason: "Outputs generated from the reference solution."}); err != nil {
		return err
	}
	if !pl.ThenValidate || k.Jobs == nil || len(sm.Problems) > 0 || len(p.Problems()) > 0 {
		return nil
	}
	by, err := jobCreator(ctx, tx, job.ID)
	if err != nil {
		return err
	}
	sm.Validating, err = queue{k.Jobs, k.Store, k.Now}.enqueueValidation(ctx, tx, p, by)
	return err
}

// jobCreator returns the user who queued a job, or 0.
func jobCreator(ctx context.Context, tx *sql.Tx, jobID int64) (int64, error) {
	var by sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT created_by FROM jobs WHERE id = ?", jobID).Scan(&by)
	return by.Int64, err
}

// ErrUploadTooLarge means an upload passed its size cap.
var ErrUploadTooLarge = errors.New("upload too large")

// ErrUploadHash means an upload does not hash to its name.
var ErrUploadHash = errors.New("upload does not match its hash")

// UploadOutput stores an output a generate job's worker sends, streamed
// to a temporary file in the blob directory while it is hashed, and
// renamed into place only if it hashes to want and is at most maxBytes.
// Nothing is held in memory, so concurrent uploads cost disk, not RAM.
func (b Blobs) UploadOutput(r io.Reader, want string, maxBytes int64) error {
	if !judgeapi.ValidSHA(want) {
		return ErrUploadHash
	}
	p := b.path(want)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, maxBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return ErrUploadTooLarge
		}
		return err
	}
	if n > maxBytes {
		return ErrUploadTooLarge
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return ErrUploadHash
	}
	if _, err := os.Stat(p); err == nil {
		return nil // already stored: same content
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	keep = true
	return nil
}

// DiscardUnrecorded deletes uploaded files that no problem ended up using:
// those not in the blobs table. Generate jobs record the outputs they
// apply; anything else a worker uploaded is an orphan.
func (b Blobs) DiscardUnrecorded(ctx context.Context, db *sql.DB, shas []string) {
	for _, sha := range shas {
		if !judgeapi.ValidSHA(sha) {
			continue
		}
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM blobs WHERE sha = ?", sha).Scan(&n); err != nil || n > 0 {
			continue
		}
		os.Remove(b.path(sha))
	}
}
