// Package jobs is podium's one queue of judge work (decision D44): jobs are
// rows in SQLite, a worker leases one at a time over the judge API, reports
// results as it goes, and closes it. The queue knows nothing about what a
// job means; each kind registers a Kind that checks reports against the
// job's payload, names the files the worker may fetch, and turns the final
// results into an outcome. Leases are secrets handed to the worker and
// stored only as hashes. A lease that is not renewed within LeaseTTL
// returns the job to the queue and the third expiry fails it; a job still
// running at its deadline fails even if its worker keeps renewing. Two
// contest rules apply at claim (M4): jobs of a paused contest wait, and
// while a graded contest is live at most one validation or generation job
// is leased at a time.
package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// LeaseTTL is how long a claim or report keeps a job leased.
const LeaseTTL = judgeapi.LeaseSeconds * time.Second

// MaxAttempts is how many leases a job gets before it fails.
const MaxAttempts = 3

// States of a job.
const (
	Queued = "queued"
	Leased = "leased"
	Done   = "done"
	Failed = "failed"
)

// Errors the judge API maps to status codes.
var (
	// ErrLease: the lease is unknown, expired and reassigned, or the job
	// is closed. The worker must drop the job (409).
	ErrLease = errors.New("lease is not current")
	// ErrNotFound: no such job.
	ErrNotFound = errors.New("job not found")
)

// ReportError is a report the server refuses (400). The worker has a bug
// or is not the worker it claims to be.
type ReportError struct{ Reason string }

func (e *ReportError) Error() string { return "bad report: " + e.Reason }

// Badf builds a ReportError. Values from the report should be clipped
// with Clip before they are formatted in.
func Badf(format string, a ...any) error { return &ReportError{fmt.Sprintf(format, a...)} }

// Clip shortens a string from a report before it is echoed or logged.
func Clip(s string) string { return limit(s, 64) }

// Result is everything reported for a job, plus the server's summary once
// the job is closed.
type Result struct {
	Compile []judgeapi.CompileResult `json:"compile"`
	Tests   []judgeapi.TestResult    `json:"tests"`
	Summary json.RawMessage          `json:"summary,omitempty"`
}

// Job is one row of the queue with its results.
type Job struct {
	ID            int64
	Kind          string
	Priority      int
	State         string
	Payload       json.RawMessage
	Result        Result
	Error         string
	Attempts      int
	Worker        string
	WorkerVersion string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
}

// Kind gives a job kind its meaning.
type Kind interface {
	// CheckReport refuses results that do not fit the payload: unknown
	// tests or roles, impossible numbers, duplicates within the report.
	// Results already stored are refused by the database.
	CheckReport(payload json.RawMessage, r judgeapi.Report) error
	// Blobs lists the hashes a worker holding this job may download.
	Blobs(payload json.RawMessage) ([]string, error)
	// MaxDuration bounds a job from its first claim to its close.
	MaxDuration(payload json.RawMessage) time.Duration
	// Finish runs inside the transaction that closes the job. It sets
	// job.Result.Summary and updates whatever the kind is about. It also
	// runs, with job.State Failed, when a job fails, so the kind can undo
	// anything it promised.
	Finish(ctx context.Context, tx *sql.Tx, job *Job) error
}

// Service runs the queue.
type Service struct {
	DB *sql.DB
	// Kinds maps a kind name to its handler.
	Kinds map[string]Kind
	// Now is the clock; tests replace it.
	Now func() time.Time
	// OnChange, when set, is called after every committed report or close,
	// outside the transaction, so watchers (the live event stream) can
	// read the new state. It must not block.
	OnChange func(jobID int64, kind string)
	// Discard, when set, receives the outputs a job's worker uploaded once
	// the job closed or its lease was swept, if no other open job holds
	// them; it deletes those nothing ended up using.
	Discard func(shas []string)

	mu      sync.Mutex
	blobs   map[int64]map[string]bool // job id -> downloadable hashes, while leased
	uploads map[int64]map[string]bool // job id -> hashes its worker uploaded
	// reserved counts uploads in flight per job (MayUpload to RecordUpload).
	reserved map[int64]int
	// lastRun is set when the latest claim was a run, so the next claim
	// prefers a waiting submission (runs never starve submissions).
	lastRun bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Enqueue adds a job inside tx and returns its id.
func (s *Service) Enqueue(ctx context.Context, tx *sql.Tx, kind string, priority int, payload any, createdBy int64) (int64, error) {
	if _, ok := s.Kinds[kind]; !ok {
		return 0, fmt.Errorf("enqueue: unknown job kind %q", kind)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var by any
	if createdBy > 0 {
		by = createdBy
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO jobs (kind, priority, payload, created_by, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		kind, priority, string(body), by, s.now().UnixMilli()).Scan(&id)
	return id, err
}

// Supersede fails a queued job that a newer one replaces. Leased jobs are
// left to finish.
func (s *Service) Supersede(ctx context.Context, tx *sql.Tx, id int64, reason string) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'failed', error = ?, finished_at = ? WHERE id = ? AND state = 'queued'`,
		reason, s.now().UnixMilli(), id)
	return err
}

// Claim leases the next queued job of one of the given kinds to worker,
// or returns nil when there is none.
func (s *Service) Claim(ctx context.Context, req judgeapi.ClaimRequest) (*judgeapi.Claim, error) {
	var known []any
	for _, k := range req.Kinds {
		if _, ok := s.Kinds[k]; ok {
			known = append(known, k)
		}
	}
	if len(known) == 0 {
		return nil, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := s.now()
	known, err = s.contestGate(ctx, tx, known, now)
	if err != nil {
		return nil, err
	}
	if len(known) == 0 {
		return nil, nil
	}
	var c judgeapi.Claim
	var payload string
	var deadline sql.NullInt64
	// Jobs of a paused contest wait in the queue (judging paused, nothing
	// lost); everything behind them still runs.
	pick := func(kinds []any) error {
		q := `SELECT id, kind, payload, deadline_at FROM jobs WHERE state = 'queued' AND kind IN (?` + strings.Repeat(", ?", len(kinds)-1) + `)
			AND (contest_id IS NULL OR NOT EXISTS (SELECT 1 FROM contests c WHERE c.id = jobs.contest_id AND c.paused = 1))
			ORDER BY priority, id LIMIT 1`
		return tx.QueryRowContext(ctx, q, kinds...).Scan(&c.JobID, &c.Kind, &payload, &deadline)
	}
	// Runs outrank submissions, so students pressing Run could starve
	// judging. After a run, the next claim takes other work first when any
	// is waiting: at least every other claim goes to a submission.
	err = sql.ErrNoRows
	s.mu.Lock()
	afterRun := s.lastRun
	s.mu.Unlock()
	if others := withoutKind(known, judgeapi.KindRun); afterRun && len(others) > 0 && len(others) < len(known) {
		err = pick(others)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = pick(known)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kind := s.Kinds[c.Kind]
	lease := newLease()
	if !deadline.Valid {
		deadline.Int64 = now.Add(kind.MaxDuration(json.RawMessage(payload))).UnixMilli()
	}
	version := limit(req.Version, 40) + " / isolate " + limit(req.Isolate, 40)
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'leased', lease_hash = ?, lease_expires_at = ?, deadline_at = ?,
		attempts = attempts + 1, worker = ?, worker_version = ?, started_at = coalesce(started_at, ?) WHERE id = ?`,
		hashLease(lease), now.Add(LeaseTTL).UnixMilli(), deadline.Int64, limit(req.Worker, 64), version, now.UnixMilli(), c.JobID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.lastRun = c.Kind == judgeapi.KindRun
	s.mu.Unlock()
	s.changed(c.JobID, c.Kind)
	c.Lease = lease
	c.LeaseSeconds = judgeapi.LeaseSeconds
	c.Payload = json.RawMessage(payload)
	return &c, nil
}

func withoutKind(kinds []any, drop string) []any {
	out := make([]any, 0, len(kinds))
	for _, k := range kinds {
		if k.(string) != drop {
			out = append(out, k)
		}
	}
	return out
}

// heavyKinds are the setters' jobs that run a problem's whole test set or
// its generator.
var heavyKinds = map[string]bool{judgeapi.KindValidate: true, judgeapi.KindGenerate: true, judgeapi.KindGenerateInputs: true}

// contestGate drops validation and generation from the kinds a worker may
// claim while a graded contest is live and one such job is already leased,
// so setters' work never takes more than one lane from students' judging
// during a contest (m4-plan section 2). A contest is live from its start
// to its close plus the longest personal extension.
func (s *Service) contestGate(ctx context.Context, tx *sql.Tx, kinds []any, now time.Time) ([]any, error) {
	asksHeavy := false
	for _, k := range kinds {
		if heavyKinds[k.(string)] {
			asksHeavy = true
		}
	}
	if !asksHeavy {
		return kinds, nil
	}
	var live, leased int
	ms := now.UnixMilli()
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM contests c WHERE c.graded = 1 AND c.published = 1 AND c.archived = 0 AND c.starts_at <= ?
			AND c.ends_at + 60000 * (c.extended_minutes + coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) > ?),
		(SELECT count(*) FROM jobs WHERE state = 'leased' AND kind IN (?, ?, ?))`,
		ms, ms, judgeapi.KindValidate, judgeapi.KindGenerate, judgeapi.KindGenerateInputs).Scan(&live, &leased); err != nil {
		return nil, err
	}
	if live == 0 || leased == 0 {
		return kinds, nil
	}
	out := kinds[:0:0]
	for _, k := range kinds {
		if !heavyKinds[k.(string)] {
			out = append(out, k)
		}
	}
	return out, nil
}

// leased checks a lease inside tx and returns the job's kind and payload.
func (s *Service) leased(ctx context.Context, tx *sql.Tx, jobID int64, lease string) (string, json.RawMessage, error) {
	var kind, state, payload string
	var leaseHash sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT kind, state, payload, lease_hash FROM jobs WHERE id = ?", jobID).
		Scan(&kind, &state, &payload, &leaseHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrLease
	}
	if err != nil {
		return "", nil, err
	}
	if state != Leased || subtle.ConstantTimeCompare([]byte(leaseHash.String), []byte(hashLease(lease))) != 1 {
		return "", nil, ErrLease
	}
	return kind, json.RawMessage(payload), nil
}

// MayFetch reports whether the worker holding jobID's lease may download
// the blob: only files the job itself needs are served, so a leaked token
// alone does not expose the whole bank.
func (s *Service) MayFetch(ctx context.Context, jobID int64, lease, sha string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	kind, payload, err := s.leased(ctx, tx, jobID, lease)
	if errors.Is(err, ErrLease) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.blobs[jobID]
	if !ok {
		list, err := s.Kinds[kind].Blobs(payload)
		if err != nil {
			return false, err
		}
		set = make(map[string]bool, len(list))
		for _, b := range list {
			set[b] = true
		}
		if s.blobs == nil || len(s.blobs) > 64 {
			s.blobs = map[int64]map[string]bool{}
		}
		s.blobs[jobID] = set
	}
	return set[sha], nil
}

// Uploader is implemented by kinds whose worker sends files back (output
// and input generation). MaxUploads bounds how many files one job may send; every
// output hash a report names must be one of them.
type Uploader interface {
	MaxUploads(payload json.RawMessage) int
}

// MayUpload reports whether the worker holding jobID's lease may upload a
// file: only for a leased job whose kind accepts uploads, with a slot left
// in its budget. A true answer reserves the slot, so concurrent uploads
// under one lease cannot all pass the check before any is recorded; the
// caller must follow with RecordUpload on success or ReleaseUpload on
// failure.
func (s *Service) MayUpload(ctx context.Context, jobID int64, lease string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	kind, payload, err := s.leased(ctx, tx, jobID, lease)
	if errors.Is(err, ErrLease) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	u, ok := s.Kinds[kind].(Uploader)
	if !ok {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.uploads[jobID])+s.reserved[jobID] >= u.MaxUploads(payload) {
		return false, nil
	}
	if s.reserved == nil {
		s.reserved = map[int64]int{}
	}
	s.reserved[jobID]++
	return true, nil
}

// RecordUpload turns a reserved slot into a file a job's worker sent, so
// reports may name it.
func (s *Service) RecordUpload(jobID int64, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.release(jobID)
	if s.uploads == nil {
		s.uploads = map[int64]map[string]bool{}
	}
	if s.uploads[jobID] == nil {
		s.uploads[jobID] = map[string]bool{}
	}
	s.uploads[jobID][sha] = true
}

// ReleaseUpload gives back a slot reserved by MayUpload when the upload
// failed.
func (s *Service) ReleaseUpload(jobID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.release(jobID)
}

// release drops one reservation; s.mu must be held.
func (s *Service) release(jobID int64) {
	if s.reserved[jobID] > 1 {
		s.reserved[jobID]--
	} else {
		delete(s.reserved, jobID)
	}
}

// checkUploads refuses output hashes a job's worker did not upload.
func (s *Service) checkUploads(kind string, jobID int64, r judgeapi.Report) error {
	if _, ok := s.Kinds[kind].(Uploader); !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range r.Results {
		if t.OutputSHA != "" && !s.uploads[jobID][t.OutputSHA] {
			return Badf("output %s for input %d was not uploaded under this lease", Clip(t.OutputSHA), t.Idx)
		}
	}
	return nil
}

// Report records results for a leased job and renews the lease; a final
// or fatal report closes the job. An empty report is a heartbeat.
func (s *Service) Report(ctx context.Context, r judgeapi.Report) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	kindName, payload, err := s.leased(ctx, tx, r.JobID, r.Lease)
	if err != nil {
		return err
	}
	kind := s.Kinds[kindName]
	if kind == nil {
		return fmt.Errorf("job %d has unknown kind %q", r.JobID, kindName)
	}
	now := s.now()
	if len(r.Compile)+len(r.Results) > 0 {
		if err := kind.CheckReport(payload, r); err != nil {
			return err
		}
		if err := s.checkUploads(kindName, r.JobID, r); err != nil {
			return err
		}
		if err := s.insertResults(ctx, tx, r); err != nil {
			return err
		}
	}
	if r.Fatal == "" && !r.Final {
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET lease_expires_at = ? WHERE id = ?", now.Add(LeaseTTL).UnixMilli(), r.JobID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(r.Compile)+len(r.Results) > 0 {
			s.changed(r.JobID, kindName)
		}
		return nil
	}
	job, err := s.load(ctx, tx, r.JobID)
	if err != nil {
		return err
	}
	job.State = Done
	if r.Fatal != "" {
		job.State, job.Error = Failed, limit(r.Fatal, 500)
	}
	if err := s.close(ctx, tx, &job, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.dropUploads(r.JobID)
	s.changed(r.JobID, kindName)
	return nil
}

// dropUploads forgets the files the jobs' workers uploaded and hands the
// ones no other open job holds to Discard, which deletes those that ended
// up unused. Called after the transaction that closed or requeued the jobs
// has committed, so whatever Finish recorded is visible.
func (s *Service) dropUploads(jobIDs ...int64) {
	s.mu.Lock()
	var gone []string
	for _, id := range jobIDs {
		for sha := range s.uploads[id] {
			gone = append(gone, sha)
		}
		delete(s.uploads, id)
	}
	held := map[string]bool{}
	for _, set := range s.uploads {
		for sha := range set {
			held[sha] = true
		}
	}
	s.mu.Unlock()
	var orphans []string
	for _, sha := range gone {
		if !held[sha] {
			orphans = append(orphans, sha)
		}
	}
	if len(orphans) > 0 && s.Discard != nil {
		s.Discard(orphans)
	}
}

// Cancel ends an unfinished job inside tx without running its kind's
// Finish: a rejudge replaced it. A worker still holding it gets 409 on its
// next report and drops it.
func (s *Service) Cancel(ctx context.Context, tx *sql.Tx, id int64, reason string) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'failed', error = ?, lease_hash = NULL, lease_expires_at = NULL,
		finished_at = ? WHERE id = ? AND state IN ('queued', 'leased')`, reason, s.now().UnixMilli(), id)
	return err
}

func (s *Service) changed(jobID int64, kind string) {
	if s.OnChange != nil {
		s.OnChange(jobID, kind)
	}
}

func (s *Service) insertResults(ctx context.Context, tx *sql.Tx, r judgeapi.Report) error {
	var seq int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(seq), 0) FROM job_results WHERE job_id = ?", r.JobID).Scan(&seq); err != nil {
		return err
	}
	insert := func(kind string, idx int, role, verdict string, timeMS, memKB int, msg, output, outputSHA string) error {
		seq++
		_, err := tx.ExecContext(ctx, `INSERT INTO job_results (job_id, seq, kind, idx, role, verdict, time_ms, memory_kb, message, output, output_sha)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.JobID, seq, kind, idx, role, verdict, timeMS, memKB, msg, output, outputSHA)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Badf("second result for %s %s %d", kind, Clip(role), idx)
		}
		return err
	}
	for _, c := range r.Compile {
		if err := insert("compile", 0, c.Role, c.Verdict, 0, 0, c.Output, "", ""); err != nil {
			return err
		}
	}
	for _, t := range r.Results {
		if err := insert("test", t.Idx, t.Role, t.Verdict, t.TimeMS, t.MemoryKB, t.Message, t.Output, t.OutputSHA); err != nil {
			return err
		}
	}
	return nil
}

// close finishes a job in state Done or Failed and lets its kind react.
func (s *Service) close(ctx context.Context, tx *sql.Tx, job *Job, now time.Time) error {
	if err := s.Kinds[job.Kind].Finish(ctx, tx, job); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET state = ?, error = ?, summary = ?, lease_hash = NULL,
		lease_expires_at = NULL, finished_at = ? WHERE id = ?`,
		job.State, job.Error, string(job.Result.Summary), now.UnixMilli(), job.ID)
	s.mu.Lock()
	delete(s.blobs, job.ID)
	s.mu.Unlock()
	return err
}

// Sweep returns jobs whose lease ran out to the queue, and fails those
// that have used every attempt or passed their deadline. It returns how
// many it requeued and failed.
func (s *Service) Sweep(ctx context.Context) (requeued, failed int64, err error) {
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, CASE WHEN deadline_at < ? THEN 'deadline' ELSE 'attempts' END FROM jobs
		WHERE state = 'leased' AND (deadline_at < ? OR (lease_expires_at < ? AND attempts >= ?))`,
		now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), MaxAttempts)
	if err != nil {
		return 0, 0, err
	}
	type dead struct {
		id     int64
		reason string
	}
	var touched [][2]any
	var doomed []dead
	for rows.Next() {
		var d dead
		if err := rows.Scan(&d.id, &d.reason); err != nil {
			rows.Close()
			return 0, 0, err
		}
		doomed = append(doomed, d)
	}
	rows.Close()
	for _, d := range doomed {
		job, err := s.load(ctx, tx, d.id)
		if err != nil {
			return 0, 0, err
		}
		job.State = Failed
		job.Error = "The judge stopped responding three times. Check podium-judge's log."
		if d.reason == "deadline" {
			job.Error = "The judge took too long on this job and it was stopped. Check the limits and test sizes, then try again."
		}
		if err := s.close(ctx, tx, &job, now); err != nil {
			return 0, 0, err
		}
		failed++
		touched = append(touched, [2]any{job.ID, job.Kind})
	}
	// Results from an abandoned attempt are dropped: the next worker
	// starts the job again from the beginning.
	expired, err := tx.QueryContext(ctx, "SELECT id, kind FROM jobs WHERE state = 'leased' AND lease_expires_at < ?", now.UnixMilli())
	if err != nil {
		return 0, 0, err
	}
	for expired.Next() {
		var id int64
		var kind string
		if err := expired.Scan(&id, &kind); err != nil {
			expired.Close()
			return 0, 0, err
		}
		touched = append(touched, [2]any{id, kind})
	}
	expired.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_results WHERE job_id IN
		(SELECT id FROM jobs WHERE state = 'leased' AND lease_expires_at < ?)`, now.UnixMilli()); err != nil {
		return 0, 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'queued', lease_hash = NULL, lease_expires_at = NULL
		WHERE state = 'leased' AND lease_expires_at < ?`, now.UnixMilli())
	if err != nil {
		return 0, 0, err
	}
	requeued, _ = res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	ids := make([]int64, 0, len(touched))
	for _, t := range touched {
		ids = append(ids, t[0].(int64))
	}
	s.dropUploads(ids...)
	for _, t := range touched {
		s.changed(t[0].(int64), t[1].(string))
	}
	return requeued, failed, nil
}

// Get returns a job by id with its results.
func (s *Service) Get(ctx context.Context, id int64) (Job, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	return s.load(ctx, tx, id)
}

// QueuePosition is the job's place in the queue counting from 1, or 0
// when it is not waiting.
func (s *Service) QueuePosition(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT CASE WHEN j.state = 'queued' THEN
		(SELECT count(*) FROM jobs o WHERE o.state = 'queued' AND (o.priority < j.priority OR (o.priority = j.priority AND o.id <= j.id)))
		ELSE 0 END FROM jobs j WHERE j.id = ?`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (s *Service) load(ctx context.Context, q queryer, id int64) (Job, error) {
	var j Job
	var payload, summary string
	var started, finished sql.NullInt64
	var created int64
	err := q.QueryRowContext(ctx, `SELECT id, kind, priority, state, payload, summary, error, attempts, worker, worker_version,
		created_at, started_at, finished_at FROM jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.Kind, &j.Priority, &j.State, &payload, &summary, &j.Error, &j.Attempts, &j.Worker, &j.WorkerVersion,
			&created, &started, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	j.Payload = json.RawMessage(payload)
	if summary != "" {
		j.Result.Summary = json.RawMessage(summary)
	}
	j.CreatedAt = time.UnixMilli(created)
	if started.Valid {
		j.StartedAt = time.UnixMilli(started.Int64)
	}
	if finished.Valid {
		j.FinishedAt = time.UnixMilli(finished.Int64)
	}
	rows, err := q.QueryContext(ctx, `SELECT kind, idx, role, verdict, time_ms, memory_kb, message, output, output_sha FROM job_results
		WHERE job_id = ? ORDER BY seq`, id)
	if err != nil {
		return Job{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, role, verdict, msg, output, outputSHA string
		var idx, timeMS, memKB int
		if err := rows.Scan(&kind, &idx, &role, &verdict, &timeMS, &memKB, &msg, &output, &outputSHA); err != nil {
			return Job{}, err
		}
		if kind == "compile" {
			j.Result.Compile = append(j.Result.Compile, judgeapi.CompileResult{Role: role, Verdict: verdict, Output: msg})
		} else {
			j.Result.Tests = append(j.Result.Tests, judgeapi.TestResult{Idx: idx, Role: role, Verdict: verdict,
				TimeMS: timeMS, MemoryKB: memKB, Message: msg, Output: output, OutputSHA: outputSHA})
		}
	}
	return j, rows.Err()
}

func newLease() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashLease(lease string) string {
	sum := sha256.Sum256([]byte(lease))
	return hex.EncodeToString(sum[:])
}

func limit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
