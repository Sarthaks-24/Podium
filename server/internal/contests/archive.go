package contests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// SlugTakenError says a restore found the slug used by another contest.
// Suggested is a free slug built from it.
type SlugTakenError struct {
	Slug, Suggested string
}

func (e *SlugTakenError) Error() string {
	return fmt.Sprintf("Another contest now uses %s. Choose a new slug.", e.Slug)
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// archivedForm is the slug an archived contest takes so its own can be used
// again: base, "-archived-" and the id, cut so the whole fits 64 characters
// and does not end in a hyphen.
func archivedForm(base string, id int64) string {
	suffix := "-archived-" + strconv.FormatInt(id, 10)
	if len(base) > 64-len(suffix) {
		base = base[:64-len(suffix)]
	}
	for len(base) > 0 && base[len(base)-1] == '-' {
		base = base[:len(base)-1]
	}
	return base + suffix
}

// freeSlugIn returns base, or base with "-2", "-3" and so on when another
// contest has it, reading through tx.
func freeSlugIn(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	slug := base
	for i := 2; i <= 100; i++ {
		var taken int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM contests WHERE slug = ?", slug).Scan(&taken); err != nil {
			return "", err
		}
		if taken == 0 {
			return slug, nil
		}
		suffix := "-" + strconv.Itoa(i)
		cut := base
		if len(cut) > 64-len(suffix) {
			cut = cut[:64-len(suffix)]
		}
		for len(cut) > 0 && cut[len(cut)-1] == '-' {
			cut = cut[:len(cut)-1]
		}
		slug = cut + suffix
	}
	return "", FieldErrors{"slug": fmt.Sprintf("Another contest already uses the slug %s.", base)}
}

// Archive hides a contest from students and the default list and frees its
// slug: the slug moves to ArchivedSlug and the contest takes
// <slug>-archived-<id>. Nothing is deleted: submissions and results stay for
// the record. Archiving an archived contest changes nothing, so the original
// slug is never overwritten. by is audited.
func (s *Service) Archive(ctx context.Context, id int64, by Actor) error {
	err := s.audited(ctx, id, by, AuditArchive, func(tx *sql.Tx) (any, bool, error) {
		var slug string
		var archived bool
		err := tx.QueryRowContext(ctx, "SELECT slug, archived FROM contests WHERE id = ?", id).Scan(&slug, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		if err != nil || archived {
			return nil, false, err
		}
		form, err := freeSlugIn(ctx, tx, archivedForm(slug, id))
		if err != nil {
			return nil, false, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE contests SET archived = 1, archived_slug = slug, slug = ?, updated_at = ? WHERE id = ? AND archived = 0",
			form, s.now().UnixMilli(), id)
		return nil, err == nil, err
	})
	if err == nil {
		s.board.drop(id)
		s.changed(id)
	}
	return err
}

// Restore brings an archived contest back and returns it. An empty slug
// gives it its original slug back; a slug another contest now uses is a
// *SlugTakenError naming a free one. A given slug must be lowercase letters,
// digits and single hyphens. Restoring a contest that is not archived changes
// nothing. by is audited.
func (s *Service) Restore(ctx context.Context, id int64, slug string, by Actor) (*Contest, error) {
	err := s.audited(ctx, id, by, AuditRestore, func(tx *sql.Tx) (any, bool, error) {
		var current, original string
		var archived bool
		err := tx.QueryRowContext(ctx, "SELECT slug, archived_slug, archived FROM contests WHERE id = ?", id).Scan(&current, &original, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		if err != nil || !archived {
			return nil, false, err
		}
		want := slug
		if want == "" {
			// An empty archived_slug means the slug never moved.
			want = original
			if want == "" {
				want = current
			}
		} else if len(want) > 64 || !slugPattern.MatchString(want) {
			return nil, false, FieldErrors{"slug": "Use lowercase letters, digits and single hyphens, like week-3."}
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM contests WHERE slug = ? AND id <> ?", want, id).Scan(&n); err != nil {
			return nil, false, err
		}
		if n == 0 {
			_, err = tx.ExecContext(ctx, "UPDATE contests SET archived = 0, archived_slug = '', slug = ?, updated_at = ? WHERE id = ? AND archived = 1",
				want, s.now().UnixMilli(), id)
			if err == nil {
				return map[string]any{"slug": want}, true, nil
			}
			if !isUniqueSlug(err) {
				return nil, false, err
			}
		}
		suggested, err := freeSlugIn(ctx, tx, want)
		if err != nil {
			return nil, false, err
		}
		return nil, false, &SlugTakenError{Slug: want, Suggested: suggested}
	})
	if err != nil {
		return nil, err
	}
	s.changed(id)
	return s.Get(ctx, id)
}
