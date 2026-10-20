// Package migrations holds the numbered schema files that db.Migrate
// applies, and their names. It has no SQL driver, so podium-judge can
// report the schema version its release was built with (podium-judge
// -version) without carrying SQLite.
package migrations

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// FS holds every migration, named NNNN_what.sql.
//
//go:embed *.sql
var FS embed.FS

// Files returns the migration file names in the order they apply.
func Files() []string {
	names, _ := fs.Glob(FS, "*.sql")
	sort.Strings(names)
	return names
}

// Versions returns the migration names without ".sql", in order.
func Versions() []string {
	var out []string
	for _, n := range Files() {
		out = append(out, strings.TrimSuffix(n, ".sql"))
	}
	return out
}

// Latest is the schema version of this build: the highest migration name.
func Latest() string {
	v := Versions()
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}
