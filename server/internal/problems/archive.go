package problems

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// SlugTakenError says a restore found the slug used by another problem.
// Suggested is a free slug built from it.
type SlugTakenError struct {
	Slug, Suggested string
}

func (e *SlugTakenError) Error() string {
	return fmt.Sprintf("Another problem now uses %s. Choose a new slug.", e.Slug)
}

// archivedForm is the slug an archived problem takes so its own can be used
// again: base, "-archived-" and the id, with base cut so the whole fits 64
// characters and does not end in a hyphen.
func archivedForm(base string, id int64) string { return slugForm(base, "-archived-", id) }

// slugForm is base, tag and the id, with base cut so the whole fits 64
// characters and does not end in a hyphen.
func slugForm(base, tag string, id int64) string {
	suffix := tag + strconv.FormatInt(id, 10)
	if len(base) > 64-len(suffix) {
		base = base[:64-len(suffix)]
	}
	for len(base) > 0 && base[len(base)-1] == '-' {
		base = base[:len(base)-1]
	}
	return base + suffix
}

// PackSlug is the slug a pack file names the problem by: the original slug
// of an archived problem, so a re-import gets it back.
func (p *Problem) PackSlug() string {
	if p.Archived && p.ArchivedSlug != "" {
		return p.ArchivedSlug
	}
	return p.Slug
}

// Archive hides a problem from the bank and frees its slug for a new
// problem: the slug moves to ArchivedSlug and the problem takes
// <slug>-archived-<id>. Its tests and history stay. Archiving an archived
// problem changes nothing, so the original slug is never overwritten.
func (s *Service) Archive(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := archiveIn(ctx, tx, id, s.now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// archiveIn is Archive inside a transaction the caller commits.
func archiveIn(ctx context.Context, tx *sql.Tx, id int64, nowMS int64) error {
	var slug string
	var archived bool
	err := tx.QueryRowContext(ctx, "SELECT slug, archived FROM problems WHERE id = ?", id).Scan(&slug, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil || archived {
		return err
	}
	form, err := freeSlugIn(ctx, tx, archivedForm(slug, id))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE problems SET archived = 1, archived_slug = slug, slug = ?, updated_at = ? WHERE id = ? AND archived = 0",
		form, nowMS, id)
	return err
}

// Restore brings an archived problem back to the bank. An empty slug gives
// it its original slug back; a slug another problem now uses is a
// *SlugTakenError naming a free one. A given slug must follow the slug rule.
// Restoring a problem that is not archived changes nothing.
func (s *Service) Restore(ctx context.Context, id int64, slug string) (*Problem, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current, original string
	var archived, deleted bool
	err = tx.QueryRowContext(ctx, "SELECT slug, archived_slug, archived, deleted FROM problems WHERE id = ?", id).Scan(&current, &original, &archived, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if deleted {
		return nil, ErrDeleted
	}
	if archived {
		want := slug
		if want == "" {
			// An empty archived_slug means the slug never moved.
			want = original
			if want == "" {
				want = current
			}
		} else if len(want) > 64 || !slugPattern.MatchString(want) {
			return nil, FieldErrors{"slug": "Use lowercase letters, digits and single hyphens, like two-sum."}
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM problems WHERE slug = ? AND id <> ?", want, id).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, slugTaken(ctx, tx, want)
		}
		_, err = tx.ExecContext(ctx, "UPDATE problems SET archived = 0, archived_slug = '', slug = ?, updated_at = ? WHERE id = ? AND archived = 1",
			want, s.now().UnixMilli(), id)
		if isUnique(err) {
			return nil, slugTaken(ctx, tx, want)
		}
		if err != nil {
			return nil, err
		}
	}
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// slugTaken builds the restore conflict for want, with a free slug to offer.
// A failed lookup is returned as its own error.
func slugTaken(ctx context.Context, q queryer, want string) error {
	suggested, err := freeSlugIn(ctx, q, want)
	if err != nil {
		return err
	}
	return &SlugTakenError{Slug: want, Suggested: suggested}
}
