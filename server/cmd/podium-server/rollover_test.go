package main

import (
	"bytes"
	"strings"
	"testing"
)

// The rollover command: a dry run writes nothing, a run adds 0.5 to every
// student without an outcome, and the same label again changes nothing.
func TestSemesterRolloverCommand(t *testing.T) {
	path, d := historyDB(t)
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'o@college.edu', 'owner', 0),
		(2, 'a@college.edu', 'student', 0), (3, 'b@college.edu', 'student', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO student_rating (user_id, value_tenths, updated_at) VALUES (2, 80, 0)`); err != nil {
		t.Fatal(err)
	}
	rating := func() int {
		var v int
		d.QueryRow("SELECT value_tenths FROM student_rating WHERE user_id = 2").Scan(&v)
		return v
	}
	var out bytes.Buffer
	if code := rolloverCmd([]string{"-db", path, "-dry-run", "2026-odd"}, &out); code != 0 || rating() != 80 ||
		!strings.Contains(out.String(), "dry run") {
		t.Fatalf("dry run: %d %q, rating %d", code, out.String(), rating())
	}
	out.Reset()
	if code := rolloverCmd([]string{"2026-odd", "-db", path}, &out); code != 0 || rating() != 85 || !strings.Contains(out.String(), "given 0.5 (up to 10): 2") {
		t.Fatalf("run: %d %q, rating %d", code, out.String(), rating())
	}
	out.Reset()
	if code := rolloverCmd([]string{"-db", path, "2026-odd"}, &out); code != 0 || rating() != 85 || !strings.Contains(out.String(), "already") {
		t.Fatalf("second run: %d %q, rating %d", code, out.String(), rating())
	}
	var audits int
	d.QueryRow("SELECT count(*) FROM site_audit WHERE action = 'rating_rollover'").Scan(&audits)
	if audits != 1 {
		t.Errorf("site audit rows: %d", audits)
	}
}
