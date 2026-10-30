package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestForgetUser(t *testing.T) {
	t.Setenv("PODIUM_OWNER_EMAIL", "owner@college.edu")
	path, d := historyDB(t)
	for _, q := range []string{
		`INSERT INTO users (id, email, name, avatar_url, role, google_sub, created_at) VALUES
			(1, 'owner@college.edu', 'Owner', '', 'owner', 'sub-1', 0),
			(7, 'asha@college.edu', 'Asha Rao', 'https://lh3.googleusercontent.com/a', 'organiser', 'sub-7', 0),
			(8, 'ravi@college.edu', 'Ravi', '', 'student', 'sub-8', 0)`,
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('s7a', 7, 0, 9999999999999), ('s7b', 7, 0, 9999999999999), ('s8', 8, 0, 9999999999999)`,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash)
			VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h')`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'week-1', 'Week 1', 1, 2, 0, 0)`,
		`INSERT INTO contest_extensions (contest_id, user_id, minutes, reason) VALUES (1, 7, 20, 'Medical accommodation')`,
		`INSERT INTO clarifications (contest_id, asked_by, question, answer, created_at) VALUES (1, 7, 'Is n positive? Asha', 'Yes.', 0)`,
		`INSERT INTO users (id, email, role, created_at) VALUES (9, 'co@college.edu', 'owner', 0)`,
		`INSERT INTO drafts (user_id, problem_id, contest_id, language, source, updated_at) VALUES (7, 1, 1, 'cpp17', 'x', 0), (8, 1, 1, 'cpp17', 'y', 0)`,
		`INSERT INTO integrity_events (user_id, problem_id, contest_id, kind, created_at) VALUES (7, 1, 1, 'paste', 0), (7, 1, 1, 'blur', 0)`,
		`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, score, client_hash, created_at)
			VALUES (1, 1, 7, 'submit', 'cpp17', 'x', 's', 'done', 100, 'hash-of-ip', 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string) int {
		var n int
		if err := d.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// The dry run reports and changes nothing.
	var out bytes.Buffer
	if code := forgetCmd([]string{"asha@college.edu", "-db", path}, &out); code != 0 {
		t.Fatalf("dry run exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "dry run, nothing changed") || !strings.Contains(out.String(), "sessions                       2") ||
		!strings.Contains(out.String(), `"Removed user 7"`) {
		t.Errorf("dry run output:\n%s", out.String())
	}
	if count("SELECT count(*) FROM sessions WHERE user_id = 7") != 2 || count("SELECT count(*) FROM users WHERE email = 'asha@college.edu'") != 1 ||
		count("SELECT count(*) FROM site_audit") != 0 {
		t.Fatal("the dry run changed something")
	}

	out.Reset()
	if code := forgetCmd([]string{"-db", path, "-apply", "Asha@College.edu"}, &out); code != 0 {
		t.Fatalf("apply exit %d\n%s", code, out.String())
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM sessions WHERE user_id = 7":                                         0,
		"SELECT count(*) FROM sessions WHERE user_id = 8":                                         1,
		"SELECT count(*) FROM drafts WHERE user_id = 7":                                           0,
		"SELECT count(*) FROM drafts WHERE user_id = 8":                                           1,
		"SELECT count(*) FROM integrity_events WHERE user_id = 7":                                 0,
		"SELECT count(*) FROM submissions WHERE user_id = 7 AND score = 100 AND client_hash = ''": 1,
		`SELECT count(*) FROM users WHERE id = 7 AND name = 'Removed user 7' AND email = 'removed-user-7@removed.invalid'
			AND avatar_url = '' AND google_sub IS NULL AND disabled = 1 AND role = 'student'`: 1,
		"SELECT count(*) FROM contest_extensions WHERE user_id = 7 AND minutes = 20 AND reason = ''":                1,
		"SELECT count(*) FROM clarifications WHERE asked_by = 7 AND answer = 'Yes.' AND question NOT LIKE '%Asha%'": 1,
	} {
		if got := count(q); got != want {
			t.Errorf("%s: %d, want %d", strings.Join(strings.Fields(q), " "), got, want)
		}
	}
	// Nothing in the database names her any more except audit rows from
	// before, and the erasure's own row names only the id.
	var actor, subject, details string
	d.QueryRow("SELECT actor_email, subject_email, details FROM site_audit WHERE action = 'user_forgotten'").Scan(&actor, &subject, &details)
	if actor != "owner@college.edu" || subject != "" || strings.Contains(details, "asha") || !strings.Contains(details, `"user_id":7`) {
		t.Errorf("audit row %q %q %s", actor, subject, details)
	}
	for _, tbl := range []string{"users", "drafts", "sessions"} {
		var n int
		d.QueryRow("SELECT count(*) FROM " + tbl + " WHERE CAST(" + map[string]string{"users": "email || name", "drafts": "source", "sessions": "id"}[tbl] + " AS TEXT) LIKE '%asha%'").Scan(&n)
		if n != 0 {
			t.Errorf("%s still names her", tbl)
		}
	}

	// Another owner needs -allow-owner.
	out.Reset()
	if code := forgetCmd([]string{"co@college.edu", "-db", path, "-apply"}, &out); code != 1 || !strings.Contains(out.String(), "-allow-owner") {
		t.Errorf("owner without -allow-owner: exit %d %s", code, out.String())
	}
	out.Reset()
	if code := forgetCmd([]string{"co@college.edu", "-db", path, "-apply", "-allow-owner"}, &out); code != 0 {
		t.Errorf("owner with -allow-owner: exit %d %s", code, out.String())
	}
	// Without the configured owner's address the guard cannot work.
	t.Setenv("PODIUM_OWNER_EMAIL", "")
	out.Reset()
	if code := forgetCmd([]string{"ravi@college.edu", "-db", path}, &out); code != 1 || !strings.Contains(out.String(), "PODIUM_OWNER_EMAIL is not set") {
		t.Errorf("no owner address: exit %d %s", code, out.String())
	}
	t.Setenv("PODIUM_OWNER_EMAIL", "owner@college.edu")

	// Refusals: the configured owner, a placeholder, an unknown address.
	for _, email := range []string{"owner@college.edu", "removed-user-7@removed.invalid", "nobody@college.edu"} {
		out.Reset()
		if code := forgetCmd([]string{email, "-db", path, "-apply"}, &out); code != 1 || !strings.HasPrefix(out.String(), "refused") {
			t.Errorf("%s: exit %d %s", email, code, out.String())
		}
	}
	if code := forgetCmd([]string{"-db", path}, &out); code != 2 {
		t.Errorf("no email: exit %d", code)
	}
}
