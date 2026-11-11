package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// The real driver, through Open, so the codes are the ones the server sees.
func TestConstraintCodes(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`CREATE TABLE parent (id INTEGER PRIMARY KEY, slug TEXT, key TEXT)`,
		`CREATE UNIQUE INDEX parent_slug ON parent (slug)`,
		`CREATE UNIQUE INDEX parent_by_key ON parent (coalesce(key, ''))`,
		`CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent (id))`,
		`INSERT INTO parent (id, slug, key) VALUES (1, 'a', 'k')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_, dupSlug := d.ExecContext(ctx, `INSERT INTO parent (slug, key) VALUES ('a', 'other')`)
	_, dupKey := d.ExecContext(ctx, `INSERT INTO parent (slug, key) VALUES ('b', 'k')`)
	_, dupID := d.ExecContext(ctx, `INSERT INTO parent (id, slug, key) VALUES (1, 'c', 'z')`)
	_, badRef := d.ExecContext(ctx, `INSERT INTO child (parent_id) VALUES (99)`)
	_, noTable := d.ExecContext(ctx, `INSERT INTO missing (x) VALUES (1)`)
	_, notNull := d.ExecContext(ctx, `INSERT INTO child (parent_id) VALUES (NULL)`)
	// Text that only looks like a constraint failure is not one.
	lookalike := errors.New("UNIQUE constraint failed: parent.slug")
	wrapped := fmt.Errorf("save: %w", dupSlug)

	for name, c := range map[string]struct {
		err             error
		unique, foreign bool
	}{
		"duplicate slug":            {dupSlug, true, false},
		"duplicate expression key":  {dupKey, true, false},
		"duplicate primary key":     {dupID, true, false},
		"wrapped duplicate":         {wrapped, true, false},
		"missing parent row":        {badRef, false, true},
		"missing table":             {noTable, false, false},
		"null in a NOT NULL column": {notNull, false, false},
		"nil":                       {nil, false, false},
		"text that says UNIQUE":     {lookalike, false, false},
	} {
		if c.err == nil && name != "nil" {
			t.Errorf("%s: the statement did not fail", name)
			continue
		}
		if got := IsUnique(c.err); got != c.unique {
			t.Errorf("%s: IsUnique = %v, want %v (%v)", name, got, c.unique, c.err)
		}
		if got := IsForeignKey(c.err); got != c.foreign {
			t.Errorf("%s: IsForeignKey = %v, want %v (%v)", name, got, c.foreign, c.err)
		}
	}
}
