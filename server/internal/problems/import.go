package problems

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a pack import does when its slug is already a problem's.
const (
	// ImportRename imports the pack as a new problem under another slug.
	ImportRename = "rename"
	// ImportArchiveExisting archives the problem that has the slug and
	// imports the pack under it, in one transaction.
	ImportArchiveExisting = "archive_existing"
	// ImportReplace saves the pack as a new revision of the problem that
	// has the slug.
	ImportReplace = "replace"
)

// ImportOptions says how ImportAs resolves a slug that is taken.
type ImportOptions struct {
	// OnConflict is "", ImportRename, ImportArchiveExisting or
	// ImportReplace. Empty imports under the pack's own slug and reports a
	// taken one as a *SlugConflictError.
	OnConflict string
	// Slug is the new slug for ImportRename.
	Slug string
	// FileName is the pack's file name, for the note of a replace.
	FileName string
}

// SlugConflictError says the slug is a problem's already: ExistingID is that
// problem, Suggested a free slug built from Slug.
type SlugConflictError struct {
	Slug, Suggested string
	ExistingID      int64
}

func (e *SlugConflictError) Error() string {
	return fmt.Sprintf("A problem with the slug %s already exists. Choose what to do with this pack.", e.Slug)
}

// ReplaceLiveError says a replace was refused because a running contest
// pins the problem.
type ReplaceLiveError struct {
	ExistingID int64
	Live       []LiveContest
}

func (e *ReplaceLiveError) Error() string {
	return "The problem is in a running contest and cannot be replaced from a pack."
}

// ImportAs adds a parsed pack to the bank, resolving a taken slug as o says.
// A replace keeps the problem's id, slug, author and history and saves the
// pack as one new revision, taking origin and tags from the pack as Import
// does; it is refused with *ReplaceLiveError while a running contest pins
// the problem. Other conflicts come back as *SlugConflictError.
func (s *Service) ImportAs(ctx context.Context, pk *Pack, authorID int64, o ImportOptions) (*Problem, error) {
	p := &pk.Problem
	switch o.OnConflict {
	case "":
	case ImportRename:
		if len(o.Slug) > 64 || !slugPattern.MatchString(o.Slug) {
			return nil, FieldErrors{"slug": "Use lowercase letters, digits and single hyphens, like two-sum."}
		}
		p.Slug = o.Slug
	case ImportArchiveExisting, ImportReplace:
		return s.importOver(ctx, pk, authorID, o)
	default:
		return nil, FieldErrors{"on_conflict": "Choose rename, archive_existing or replace."}
	}
	out, err := s.Import(ctx, pk, authorID)
	return out, s.asConflict(ctx, s.DB, p.Slug, err)
}

// asConflict turns a taken-slug field error into a *SlugConflictError that
// names the problem holding the slug; any other error passes through.
func (s *Service) asConflict(ctx context.Context, q queryer, slug string, err error) error {
	var fields FieldErrors
	if !errors.As(err, &fields) || fields["slug"] == "" {
		return err
	}
	var id int64
	if e := q.QueryRowContext(ctx, "SELECT id FROM problems WHERE slug = ?", slug).Scan(&id); e != nil {
		return err
	}
	suggested, e := freeSlugIn(ctx, q, slug)
	if e != nil {
		return e
	}
	return &SlugConflictError{Slug: slug, Suggested: suggested, ExistingID: id}
}

// importOver is ImportAs for archive_existing and replace: one transaction
// that finds the problem holding the slug and acts on it.
func (s *Service) importOver(ctx context.Context, pk *Pack, authorID int64, o ImportOptions) (*Problem, error) {
	p := &pk.Problem
	p.AuthorID = authorID
	if err := s.writeTests(p.Tests, pk.Inputs, pk.Outputs); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.recordTests(ctx, tx, p.Tests); err != nil {
		return nil, err
	}
	var existing int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM problems WHERE slug = ?", p.Slug).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var out *Problem
	switch {
	case existing == 0:
		// The slug was freed meanwhile: a plain import.
		if err := s.insert(ctx, tx, p, RevisionNote{By: authorID}, ""); err != nil {
			return nil, s.asConflict(ctx, tx, p.Slug, err)
		}
		out = p
	case o.OnConflict == ImportArchiveExisting:
		if err := archiveIn(ctx, tx, existing, s.now().UnixMilli()); err != nil {
			return nil, err
		}
		if err := s.insert(ctx, tx, p, RevisionNote{By: authorID}, ""); err != nil {
			return nil, s.asConflict(ctx, tx, p.Slug, err)
		}
		out = p
	default:
		out, err = s.replaceIn(ctx, tx, existing, pk, authorID, o.FileName)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// replaceIn saves the pack over problem id as one new revision, in tx.
// It goes through save, not Update, so the AI draft marking follows the
// pack's tags and the samples and subtasks are the pack's, as in Import.
func (s *Service) replaceIn(ctx context.Context, tx *sql.Tx, id int64, pk *Pack, authorID int64, fileName string) (*Problem, error) {
	old, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.Deleted {
		return nil, ErrDeleted
	}
	if old.Archived {
		return nil, FieldErrors{"on_conflict": "Restore the problem before replacing it from a pack."}
	}
	live, err := liveContestsOf(ctx, tx, id, s.now())
	if err != nil {
		return nil, err
	}
	if len(live) > 0 {
		return nil, &ReplaceLiveError{ExistingID: id, Live: live}
	}
	np := pk.Problem
	np.ID, np.Slug, np.AuthorID, np.CreatedAt, np.Archived, np.ArchivedSlug = old.ID, old.Slug, old.AuthorID, old.CreatedAt, false, ""
	note := RevisionNote{By: authorID, Reason: replaceReason(fileName)}
	if err := s.save(ctx, tx, &np, note); err != nil {
		return nil, err
	}
	// A pack is not a person's edit of a draft: origin and the solution flag
	// are what a fresh Import would give.
	if _, err := tx.ExecContext(ctx, "UPDATE problems SET origin = ?, solution_by_person = 0 WHERE id = ?", np.Origin, id); err != nil {
		return nil, err
	}
	return load(ctx, tx, id)
}

// replaceReason is the revision note of a replace: "Replaced from pack
// <file name>", the name cleaned of control characters and cut short.
func replaceReason(fileName string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, fileName)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 80 {
		name = strings.ToValidUTF8(string([]rune(name)[:80]), "")
	}
	if name == "" {
		return "Replaced from a pack"
	}
	return "Replaced from pack " + name
}
