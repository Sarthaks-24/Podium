// Package jobs is podium's one queue of judge work (decision D44): jobs are
// rows in SQLite, a worker leases one at a time over the judge API, reports
// results as it goes, and closes it. The queue knows nothing about what a
// job means; each kind registers a Kind that checks reports against the
// job's payload, names the files the worker may fetch, and turns the final
// results into an outcome. Leases are secrets handed to the worker and
// stored only as hashes. A lease that is not renewed within LeaseTTL
// returns the job to the queue and the third expiry fails it; a job still
// running at its deadline fails even if its worker keeps renewing.
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

	mu    sync.Mutex
	blobs map[int64]map[string]bool // job id -> downloadable hashes, while leased
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

	var c judgeapi.Claim
	var payload string
	var deadline sql.NullInt64
	q := `SELECT id, kind, payload, deadline_at FROM jobs WHERE state = 'queued' AND kind IN (?` + strings.Repeat(", ?", len(known)-1) + `)
		ORDER BY priority, id LIMIT 1`
	err = tx.QueryRowContext(ctx, q, known...).Scan(&c.JobID, &c.Kind, &payload, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kind := s.Kinds[c.Kind]
	lease := newLease()
	now := s.now()
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
	c.Lease = lease
	c.LeaseSeconds = judgeapi.LeaseSeconds
	c.Payload = json.RawMessage(payload)
	return &c, nil
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
		if err := s.insertResults(ctx, tx, r); err != nil {
			return err
		}
	}
	if r.Fatal == "" && !r.Final {
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET lease_expires_at = ? WHERE id = ?", now.Add(LeaseTTL).UnixMilli(), r.JobID); err != nil {
			return err
		}
		return tx.Commit()
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
	return tx.Commit()
}

func (s *Service) insertResults(ctx context.Context, tx *sql.Tx, r judgeapi.Report) error {
	var seq int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(seq), 0) FROM job_results WHERE job_id = ?", r.JobID).Scan(&seq); err != nil {
		return err
	}
	insert := func(kind string, idx int, role, verdict string, timeMS, memKB int, msg string) error {
		seq++
		_, err := tx.ExecContext(ctx, `INSERT INTO job_results (job_id, seq, kind, idx, role, verdict, time_ms, memory_kb, message)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.JobID, seq, kind, idx, role, verdict, timeMS, memKB, msg)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Badf("second result for %s %s %d", kind, Clip(role), idx)
		}
		return err
	}
	for _, c := range r.Compile {
		if err := insert("compile", 0, c.Role, c.Verdict, 0, 0, c.Output); err != nil {
			return err
		}
	}
	for _, t := range r.Results {
		if err := insert("test", t.Idx, t.Role, t.Verdict, t.TimeMS, t.MemoryKB, t.Message); err != nil {
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
	}
	// Results from an abandoned attempt are dropped: the next worker
	// starts the job again from the beginning.
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
	return requeued, failed, tx.Commit()
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
	rows, err := q.QueryContext(ctx, `SELECT kind, idx, role, verdict, time_ms, memory_kb, message FROM job_results
		WHERE job_id = ? ORDER BY seq`, id)
	if err != nil {
		return Job{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, role, verdict, msg string
		var idx, timeMS, memKB int
		if err := rows.Scan(&kind, &idx, &role, &verdict, &timeMS, &memKB, &msg); err != nil {
			return Job{}, err
		}
		if kind == "compile" {
			j.Result.Compile = append(j.Result.Compile, judgeapi.CompileResult{Role: role, Verdict: verdict, Output: msg})
		} else {
			j.Result.Tests = append(j.Result.Tests, judgeapi.TestResult{Idx: idx, Role: role, Verdict: verdict,
				TimeMS: timeMS, MemoryKB: memKB, Message: msg})
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
