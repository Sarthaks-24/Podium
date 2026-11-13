package problems

// Bring back an earlier version of a problem (v2.1.0 stage H). A revision
// keeps the text of its programs and its generator arguments from the day it
// is recorded, and the collector keeps the files of each problem's newest
// GCKeepRevisions revisions, so those versions can be written back as a new
// revision. Nothing here talks to the judge.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// GCKeepRevisions is how many of a problem's newest revisions the collector
// keeps whole, program and test files included.
const GCKeepRevisions = 3

const (
	copyBackInUse    = "Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead."
	copyBackRunning  = "A contest that is running uses this problem. Try again after it ends."
	copyBackNotKept  = "This version's files were not kept, so it cannot be brought back."
	copyBackArchived = "%s is archived, so a version cannot be brought back. Restore it first."
	copyBackCurrent  = "This is already the current version."
)

// HistoryEntry is one revision on a problem's history.
type HistoryEntry struct {
	Revision *Revision
	// Current marks the newest revision, the one the working copy is.
	Current bool
	// CanBringBack is true when every file of the revision still exists.
	CanBringBack bool
}

// History lists the problem's newest revisions, at most limit, newest first,
// and how many it has.
func (s *Service) History(ctx context.Context, problemID int64, limit int) ([]HistoryEntry, int, error) {
	list, _, total, err := s.Revisions(ctx, problemID, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]HistoryEntry, 0, len(list))
	for i, r := range list {
		tests, err := s.RevisionTests(ctx, r.ID)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, HistoryEntry{Revision: r, Current: i == 0, CanBringBack: s.complete(r, tests)})
	}
	return out, total, nil
}

// complete reports whether revision r records everything a bring back needs
// and every file it names is on disk: its limits and checker, the generator
// arguments of every subtask (a revision from before v2.1.0 has none), the
// text of each program and the checker, and every test file.
func (s *Service) complete(r *Revision, tests []RevisionTest) bool {
	if r.Reconstructed || !r.StatementRecorded || r.TimeLimitMS == nil || r.MemoryLimitMB == nil || r.Checker == nil || r.Epsilon == nil ||
		r.Reference.SHA == "" || slices.ContainsFunc(r.Subtasks, func(st RevisionSubtask) bool { return st.GeneratorArgs == nil }) {
		return false
	}
	files := []string{r.Reference.SHA, r.Brute.SHA, r.Validator.SHA, r.Generator.SHA, r.CheckerSHA}
	for _, t := range tests {
		files = append(files, t.InputSHA, t.OutputSHA)
	}
	for _, sha := range files {
		if sha == "" {
			continue
		}
		if !judgeapi.ValidSHA(sha) {
			return false
		}
		if _, err := os.Stat(s.Blobs.path(sha)); err != nil {
			return false
		}
	}
	return true
}

// studentCode reports whether a student has a submission, run or saved draft
// for the problem. An organiser's own Try as a student does not count.
func studentCode(ctx context.Context, q queryer, problemID int64) (bool, error) {
	var used bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM submissions s JOIN users u ON u.id = s.user_id
			WHERE s.problem_id = ? AND u.role = 'student')
		OR EXISTS (SELECT 1 FROM drafts d JOIN users u ON u.id = d.user_id WHERE d.problem_id = ? AND u.role = 'student')`,
		problemID, problemID).Scan(&used)
	return used, err
}

// BringBack writes revision revisionID of the problem back onto it as one new
// revision, in one transaction: the version's title, statement, limits,
// checker, samples, subtasks with their generator arguments, tests, programs,
// starting code and locked code. Tags, difficulty, the generator's plan,
// origin and slug stay as they are now. The problem must pass Validate again,
// so its validation is withdrawn; the caller queues Validate. Contests that
// pin the problem keep the revision they pin. It returns the problem and the
// new revision.
//
// It is refused, with a *RewriteRefusal, while a running contest holds the
// problem, when the problem is archived, when a student has code for it, when
// the version's files were not all kept, and when the version is the current
// one; with ErrChecking while a language check is open, ErrDeleted for a
// deleted problem and ErrNotFound for a revision of another problem. Nothing
// is written then.
//
// If the problem is an AI draft and the version's reference or brute force
// differs from the current one, the mark that a person wrote the solution is
// cleared: the text brought back was not written by whoever wrote the text it
// replaces.
func (s *Service) BringBack(ctx context.Context, problemID, revisionID, by int64) (*Problem, *Revision, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, problemID)
	if err != nil {
		return nil, nil, err
	}
	if p.Deleted {
		return nil, nil, ErrDeleted
	}
	r, err := scanRevision(tx.QueryRowContext(ctx, "SELECT "+revisionColumns+" FROM problem_revisions WHERE id = ? AND problem_id = ?", revisionID, problemID))
	if err != nil {
		return nil, nil, err
	}
	if p.Archived {
		return nil, nil, refuse("archived", copyBackArchived, p.Title)
	}
	live, err := liveContestsOf(ctx, tx, problemID, s.now())
	if err != nil {
		return nil, nil, err
	}
	if len(live) > 0 {
		return nil, nil, &RewriteRefusal{Code: "contest_running", Message: copyBackRunning}
	}
	if used, err := studentCode(ctx, tx, problemID); err != nil {
		return nil, nil, err
	} else if used {
		return nil, nil, &RewriteRefusal{Code: "in_use", Message: copyBackInUse}
	}
	// A proof that finds a changed problem drops its result, so a bring back
	// waits for the round.
	if open, err := OpenProof(ctx, tx, problemID); err != nil {
		return nil, nil, err
	} else if open {
		return nil, nil, ErrChecking
	}
	tests, err := revisionTestsIn(ctx, tx, r.ID)
	if err != nil {
		return nil, nil, err
	}
	notKept := &RewriteRefusal{Code: "not_kept", Message: copyBackNotKept}
	if !s.complete(r, tests) {
		return nil, nil, notKept
	}
	np, err := s.problemAt(r, tests, p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, notKept
	}
	if err != nil {
		return nil, nil, err
	}
	np.ContentHash = ContentHash(np)
	latest, err := latestRevision(ctx, tx, problemID)
	if err != nil {
		return nil, nil, err
	}
	if latest != nil && sameRevision(*latest, revisionOf(np)) {
		return nil, nil, &RewriteRefusal{Code: "current", Message: copyBackCurrent}
	}
	if err := s.recordTests(ctx, tx, np.Tests); err != nil {
		if errors.Is(err, ErrBlobMissing) {
			return nil, nil, notKept
		}
		return nil, nil, err
	}
	if err := s.save(ctx, tx, np, RevisionNote{By: by, Reason: fmt.Sprintf("Brought back version %d", r.Rev)}); err != nil {
		return nil, nil, err
	}
	// The version must pass Validate again on this problem.
	byPerson := p.Origin == OriginAI && (np.Reference != p.Reference || !sameSource(np.Brute, p.Brute))
	if _, err := tx.ExecContext(ctx, `UPDATE problems SET validated_hash = NULL, validated_at = NULL,
		solution_by_person = CASE WHEN ? THEN 0 ELSE solution_by_person END WHERE id = ?`, byPerson, problemID); err != nil {
		return nil, nil, err
	}
	out, err := load(ctx, tx, problemID)
	if err != nil {
		return nil, nil, err
	}
	rev, err := latestRevision(ctx, tx, problemID)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return out, rev, nil
}

// problemAt is the working copy p with everything revision r records put
// back: the programs are read from the file store. p itself is not changed.
func (s *Service) problemAt(r *Revision, tests []RevisionTest, p *Problem) (*Problem, error) {
	np := *p
	np.Title, np.Statement = r.Title, r.Statement
	np.TimeLimitMS, np.MemoryLimitMB, np.Checker, np.Epsilon = *r.TimeLimitMS, *r.MemoryLimitMB, *r.Checker, *r.Epsilon
	np.Samples = slices.Clone(r.Samples)
	np.Starters, np.Mode = maps.Clone(startersOrEmpty(r.Starters)), modeOrWhole(r.Mode)
	np.LockedTop, np.LockedBottom = maps.Clone(startersOrEmpty(r.LockedTop)), maps.Clone(startersOrEmpty(r.LockedBottom))
	np.Subtasks, np.Tests = nil, nil
	for _, st := range r.Subtasks {
		np.Subtasks = append(np.Subtasks, Subtask{st.Idx, st.Points, st.Title, st.Tests, slices.Clone(st.GeneratorArgs)})
	}
	for _, t := range tests {
		np.Tests = append(np.Tests, Test{Idx: t.Idx, InputSHA: t.InputSHA, OutputSHA: t.OutputSHA, InputSize: t.InputSize, OutputSize: t.OutputSize})
	}
	program := func(ref SourceRef) (*Source, error) {
		if ref.SHA == "" {
			return nil, nil
		}
		text, err := s.Blobs.Read(ref.SHA, MaxSource+1)
		return &Source{ref.Language, string(text)}, err
	}
	ref, err := program(r.Reference)
	if err != nil {
		return nil, err
	}
	np.Reference = *ref
	if np.Brute, err = program(r.Brute); err != nil {
		return nil, err
	}
	if np.Validator, err = program(r.Validator); err != nil {
		return nil, err
	}
	if np.Generator, err = program(r.Generator); err != nil {
		return nil, err
	}
	np.CheckerSource = ""
	if r.CheckerSHA != "" {
		text, err := s.Blobs.Read(r.CheckerSHA, MaxSource+1)
		if err != nil {
			return nil, err
		}
		np.CheckerSource = string(text)
	}
	return &np, nil
}
