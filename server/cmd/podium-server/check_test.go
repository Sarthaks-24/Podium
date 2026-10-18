package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/problems"
)

func TestCheckCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath, tests := filepath.Join(dir, "podium.db"), filepath.Join(dir, "tests")
	d, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	blobs := problems.Blobs{Dir: tests}
	tx, _ := d.BeginTx(ctx, nil)
	sha, err := blobs.Put(ctx, tx, []byte("2\n1 2\n3 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	d.Close()

	var out bytes.Buffer
	if code := checkCmd([]string{"-db", dbPath, "-tests", tests, "-hash"}, &out); code != 0 {
		t.Fatalf("sound database: exit %d\n%s", code, out.String())
	}
	for _, want := range []string{"integrity: ok", "foreign keys: ok", "applied, 0 this server would apply", "blobs 1", "1 recorded", "result: ok"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	os.WriteFile(filepath.Join(tests, sha[:2], sha), []byte("2\n1 2\n3 5\n"), 0o600)
	out.Reset()
	if code := checkCmd([]string{"-db", dbPath, "-tests", tests, "-hash"}, &out); code != 1 || !strings.Contains(out.String(), "1 wrong content") {
		t.Errorf("damaged file: exit %d\n%s", code, out.String())
	}
	out.Reset()
	if code := checkCmd([]string{"-db", filepath.Join(dir, "nope.db")}, &out); code != 2 {
		t.Errorf("missing database: exit %d\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "nope.db")); err == nil {
		t.Error("check created a database")
	}
}
