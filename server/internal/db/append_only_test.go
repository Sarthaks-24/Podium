package db

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
)

// released pins every migration file. The runner records only a version's
// name, so a database that already applied a file never sees a later edit
// to it: 0001_v3.sql was changed four times after dev databases applied it
// (2026-10-28 to 2026-10-30), and those databases then failed with "no such
// column: full_screen" and "no such column: stderr". Schema changes go in a
// new numbered file (V4-23); add its hash here in the same commit.
var released = map[string]string{
	"0001_v3.sql":             "a4e642ded2914a10b31a36cacd396092e73ca268d612cda49e62cca96e231c94",
	"0002_v4_roles.sql":       "fff24a5a4c01bf2a39dab88f106c01fc48d7ddde54d67585cb78ed9de0228a19",
	"0003_v4_signin.sql":      "2c637090a832f1be0b0f88b809a8cc1ef15dfe3cd754a36cccc6e6adfc941c04",
	"0004_v4_starters.sql":    "129e51a73d2be9440822e08627edf78792def156cd82c51d41a8591b929b5171",
	"0005_v4_client_keys.sql": "d6044502bee31557338c8a21b3fdf8faadbf059ddace1ca53c9a3fe3f7dc9814",
	"0006_v4_indexes.sql":     "85187096dced004b5582f9d1fc95728b472cf1755c2f6920dd741c95c836beeb",
}

func TestMigrationsAreAppendOnly(t *testing.T) {
	names, err := fs.Glob(migrationFS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
		b, err := fs.ReadFile(migrationFS, n)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(strings.ReplaceAll(string(b), "\r\n", "\n")))
		got := hex.EncodeToString(sum[:])
		want, ok := released[n]
		switch {
		case !ok:
			t.Errorf("%s is new: add %q to released in this file", n, got)
		case got != want:
			t.Errorf("%s changed after it was released: databases that applied it never see the change. Put the change in a new numbered migration and restore this file", n)
		}
	}
	for n := range released {
		if !seen[n] {
			t.Errorf("%s was removed: a database that applied it is then refused as unknown", n)
		}
	}
}
