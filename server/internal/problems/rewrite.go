package problems

// Rewrite with AI, the Replace half (v2.1.0 stage W). The wizard makes the
// rewrite as an ordinary draft problem; Replace puts it on the old problem.
// Nothing here talks to a model.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// RewriteRefusal is a refusal of Replace with its own code and a sentence
// for the organiser. The rewrite stays a problem of its own whenever Replace
// is refused.
type RewriteRefusal struct{ Code, Message string }

func (e *RewriteRefusal) Error() string { return e.Message }

// refuse builds a refusal whose message is format filled with the old
// problem's title.
func refuse(code, format, title string) error {
	return &RewriteRefusal{Code: code, Message: fmt.Sprintf(format, title)}
}

const (
	copyRewriteNotValidated = "Validate this rewrite first."
	copyRewriteChanged      = "%s changed after this rewrite began. Rewrite it again, or keep this rewrite as its own problem."
	copyRewriteRunning      = "A contest that is running uses %s, so it cannot be replaced now. Try again after the contest ends, or keep this rewrite as its own problem."
	copyRewriteInUse        = "Students have submitted or saved code for %s, so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests."
	copyRewriteArchived     = "%s is archived, so it cannot be replaced. Restore it first, or keep this rewrite as its own problem."
	copyRewriteDeleted      = "%s was deleted, so it cannot be replaced. Keep this rewrite as its own problem."
	copyRewriteGone         = "This rewrite is archived or deleted, so it cannot replace %s. Rewrite it again."
)

// copyTitle names the archived copy of a problem: "<title> (before rewrite,
// 11 Nov 2026)", with the title cut so the whole fits MaxTitle.
func copyTitle(title string, at time.Time) string {
	suffix := " (before rewrite, " + at.Format("2 Jan 2006") + ")"
	if room := MaxTitle - utf8.RuneCountInString(suffix); utf8.RuneCountInString(title) > room {
		title = string([]rune(title)[:room])
	}
	return title + suffix
}

// ReplaceWithRewrite puts the AI rewrite draftID on problem oldID, in one
// transaction. base is oldID's EditSnapshot hash from when the rewrite began.
// It checks the rewrite has passed Validate and that the old problem is
// unchanged, not archived or deleted, not held by a running contest and
// untouched by students, then inserts an archived copy of the old problem,
// saves the rewrite's content over the old problem as one new revision (id
// and slug stay), marks it an AI draft that must pass Validate again, and
// archives the rewrite. A refusal is a *RewriteRefusal; nothing is written.
// It returns the replaced problem and the archived copy. Validate is queued
// by the caller. A copy is dated in loc (the caller's zone), or in the server's own
// zone when loc is nil.
func (s *Service) ReplaceWithRewrite(ctx context.Context, oldID, draftID int64, base string, by int64, loc *time.Location) (replaced, kept *Problem, err error) {
	if oldID == draftID {
		return nil, nil, FieldErrors{"draft_id": "A problem cannot replace itself."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	old, err := load(ctx, tx, oldID)
	if err != nil {
		return nil, nil, err
	}
	draft, err := load(ctx, tx, draftID)
	if err != nil {
		return nil, nil, err
	}
	if draft.Origin != OriginAI {
		return nil, nil, FieldErrors{"draft_id": "Only an AI draft can replace a problem."}
	}
	switch {
	case draft.Deleted || draft.Archived:
		return nil, nil, refuse("rewrite_gone", copyRewriteGone, old.Title)
	case !draft.Validated():
		return nil, nil, &RewriteRefusal{Code: "not_validated", Message: copyRewriteNotValidated}
	case old.Deleted:
		return nil, nil, refuse("deleted", copyRewriteDeleted, old.Title)
	case old.Archived:
		return nil, nil, refuse("archived", copyRewriteArchived, old.Title)
	}
	if cur, err := RewriteBase(old); err != nil {
		return nil, nil, err
	} else if cur != base {
		return nil, nil, refuse("changed", copyRewriteChanged, old.Title)
	}
	live, err := liveContestsOf(ctx, tx, oldID, s.now())
	if err != nil {
		return nil, nil, err
	}
	if len(live) > 0 {
		return nil, nil, refuse("contest_running", copyRewriteRunning, old.Title)
	}
	// Only students count: an organiser's own Try as a student is not use.
	var used bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM submissions s JOIN users u ON u.id = s.user_id
			WHERE s.problem_id = ? AND u.role = 'student')
		OR EXISTS (SELECT 1 FROM drafts d JOIN users u ON u.id = d.user_id WHERE d.problem_id = ? AND u.role = 'student')`, oldID, oldID).Scan(&used); err != nil {
		return nil, nil, err
	}
	if used {
		return nil, nil, refuse("in_use", copyRewriteInUse, old.Title)
	}
	// A proof that finds a changed problem drops its result, so the replace
	// waits for the round.
	if open, err := OpenProof(ctx, tx, oldID); err != nil {
		return nil, nil, err
	} else if open {
		return nil, nil, ErrChecking
	}

	kept, err = s.copyArchivedIn(ctx, tx, old, by, loc)
	if err != nil {
		return nil, nil, err
	}
	pk := &Pack{Problem: *draft}
	pk.Problem.Origin = OriginAI
	tags := withoutAIDraftTag(append([]string{}, draft.Tags...))
	if len(tags) >= MaxTags {
		tags = tags[:MaxTags-1]
	}
	pk.Problem.Tags = append(tags, AIDraftTag)
	replaced, err = s.replaceIn(ctx, tx, oldID, pk, by, "Replaced with an AI rewrite")
	if err != nil {
		return nil, nil, err
	}
	// The new content must pass Validate again on this problem.
	if _, err := tx.ExecContext(ctx, "UPDATE problems SET validated_hash = NULL, validated_at = NULL WHERE id = ?", oldID); err != nil {
		return nil, nil, err
	}
	replaced.ValidatedHash, replaced.ValidatedAt = "", time.Time{}
	if err := archiveIn(ctx, tx, draftID, s.now().UnixMilli()); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return replaced, kept, nil
}

// copyArchivedIn inserts a copy of p as an archived problem named by
// copyTitle, with p's content, tests (the same blobs, so they stay out of
// garbage collection), validation state and original slug, which Restore
// offers back. p is not changed.
func (s *Service) copyArchivedIn(ctx context.Context, tx *sql.Tx, p *Problem, by int64, loc *time.Location) (*Problem, error) {
	cp := *p
	cp.ID = 0
	day := s.now()
	if loc != nil {
		day = day.In(loc)
	}
	cp.Title = copyTitle(p.Title, day)
	const suffix = "-before-rewrite"
	base := p.Slug
	if len(base) > 64-len(suffix) {
		base = strings.TrimRight(base[:64-len(suffix)], "-")
	}
	slug, err := freeSlugIn(ctx, tx, base+suffix)
	if err != nil {
		return nil, err
	}
	cp.Slug = slug
	if err := s.insert(ctx, tx, &cp, RevisionNote{By: by, Reason: "Copy made before an AI rewrite"}, ""); err != nil {
		return nil, err
	}
	if err := archiveIn(ctx, tx, cp.ID, s.now().UnixMilli()); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE problems SET archived_slug = ?,
		validated_hash = (SELECT validated_hash FROM problems WHERE id = ?),
		validated_at = (SELECT validated_at FROM problems WHERE id = ?),
		solution_by_person = (SELECT solution_by_person FROM problems WHERE id = ?) WHERE id = ?`,
		p.Slug, p.ID, p.ID, p.ID, cp.ID); err != nil {
		return nil, err
	}
	return load(ctx, tx, cp.ID)
}

// RewriteBase fingerprints problem p for a rewrite: the pack files without
// test data and the tests by hash, the same as an AI edit's base. A deleted
// or archived problem has none.
func RewriteBase(p *Problem) (string, error) {
	snap, err := snapshotOf(p)
	if err != nil {
		return "", err
	}
	return snap.Hash, nil
}

// maxRewriteBase bounds the fingerprint a Replace request carries (a SHA-256
// in hex is 64 characters).
const maxRewriteBase = 128
