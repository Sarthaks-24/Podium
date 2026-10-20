package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// openReadOnly opens the database at path without changing it.
func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// sawCmd runs "podium-server saw": what a student had been shown of a
// contest problem at a moment, from the pin history and the served log
// (B2.3, B2.4). It only reads, so it is safe on the live database.
func sawCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("saw", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database")
	slug := fs.String("contest", "", "the contest's slug")
	email := fs.String("email", "", "the student's email")
	label := fs.String("label", "", "the problem's label, like A")
	at := fs.String("at", "", "the moment, RFC 3339 (for example 2026-11-10T14:35:00+05:30); default now")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *slug == "" || *email == "" || *label == "" {
		fmt.Fprintln(out, "saw: give -contest, -email and -label")
		return 2
	}
	when := time.Now()
	if *at != "" {
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fmt.Fprintf(out, "saw: -at: %v\n", err)
			return 2
		}
		when = t
	}
	d, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(out, "saw: %v\n", err)
		return 2
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	svc := &contests.Service{DB: d, Problems: &problems.Service{DB: d}}
	c, err := svc.GetBySlug(ctx, *slug)
	if err != nil {
		fmt.Fprintf(out, "saw: contest %q: %v\n", *slug, err)
		return 2
	}
	var userID int64
	if err := d.QueryRowContext(ctx, "SELECT id FROM users WHERE email = ?", *email).Scan(&userID); err != nil {
		fmt.Fprintf(out, "saw: no account %q\n", *email)
		return 2
	}
	seen, err := svc.SeenAt(ctx, c.ID, userID, *label, when)
	if err != nil {
		fmt.Fprintf(out, "saw: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "contest %s, problem %s, %s, at %s\n", c.Slug, *label, *email, stamp(when))
	if seen.PinnedRev == nil {
		fmt.Fprintln(out, "  no revision was pinned to this label yet")
		return 0
	}
	fmt.Fprintf(out, "  pinned: revision %d since %s%s\n", seen.PinnedRev.Rev, stamp(seen.PinnedSince), reconstructedNote(seen.PinnedRev))
	if seen.FirstSeenAt.IsZero() {
		fmt.Fprintln(out, "  served: not yet (the student had not opened this revision)")
	} else {
		fmt.Fprintf(out, "  served: first at %s\n", stamp(seen.FirstSeenAt))
	}
	for _, s := range seen.Served {
		fmt.Fprintf(out, "  served log: revision %d first at %s\n", s.Rev, stamp(s.FirstSeenAt))
	}
	return 0
}

func reconstructedNote(r *problems.Revision) string {
	if r.Reconstructed {
		return " (reconstructed at the upgrade: tests only, statement not recorded)"
	}
	return ""
}

// runsReportCmd runs "podium-server runs-report": per contest, how many
// runs students made and how many of them used a custom input, for the
// calibration row "Runs with a custom input, per contest" (P2 gate, D114).
// It reads the run jobs' payloads, which P3's retention job deletes after
// 30 days, so take it before that job first runs.
func runsReportCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("runs-report", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	d, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(out, "runs-report: %v\n", err)
		return 2
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rows, err := d.QueryContext(ctx, runsReportSQL)
	if err != nil {
		fmt.Fprintf(out, "runs-report: %v\n", err)
		return 2
	}
	defer rows.Close()
	fmt.Fprintln(out, "| Contest | Starts (UTC) | Students who ran | Runs | Runs with a custom input | Share | Students with a custom input |")
	fmt.Fprintln(out, "|---|---|---|---|---|---|---|")
	n := 0
	for rows.Next() {
		var slug string
		var starts int64
		var students, runs, custom, customStudents int
		if err := rows.Scan(&slug, &starts, &students, &runs, &custom, &customStudents); err != nil {
			fmt.Fprintf(out, "runs-report: %v\n", err)
			return 2
		}
		share := 0.0
		if runs > 0 {
			share = 100 * float64(custom) / float64(runs)
		}
		fmt.Fprintf(out, "| %s | %s | %d | %d | %d | %.0f%% | %d |\n", slug, stamp(time.UnixMilli(starts)), students, runs, custom, share, customStudents)
		n++
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(out, "runs-report: %v\n", err)
		return 2
	}
	if n == 0 {
		fmt.Fprintln(out, "| (no runs in any contest) | | | | | | |")
	}
	return 0
}

// runsReportSQL counts students' runs per contest. A run used a custom
// input when its job's only input is number 0 (samples are numbered from
// 1); runs whose job is gone are counted as runs but not as custom.
const runsReportSQL = `SELECT c.slug, c.starts_at,
	count(DISTINCT s.user_id),
	count(*),
	coalesce(sum(json_extract(j.payload, '$.inputs[0].idx') = 0), 0),
	count(DISTINCT CASE WHEN json_extract(j.payload, '$.inputs[0].idx') = 0 THEN s.user_id END)
	FROM submissions s
	JOIN contests c ON c.id = s.contest_id
	JOIN users u ON u.id = s.user_id AND u.role = 'student'
	LEFT JOIN jobs j ON j.id = s.job_id
	WHERE s.kind = 'run'
	GROUP BY c.id ORDER BY c.starts_at, c.id`

// migrateCmd runs "podium-server migrate -db COPY": applies this release's
// migrations to a database file and checks the result, for rehearsing an
// upgrade on a copy of the live database before the deploy slot (D68).
// It refuses the configured live database, which the server migrates
// itself when it starts.
func migrateCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", "", "the copy to migrate (never the live database)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dbPath == "" {
		fmt.Fprintln(out, "migrate: give -db with the path of a copy")
		return 2
	}
	if live := os.Getenv("PODIUM_DB_PATH"); live != "" && sameFile(live, *dbPath) {
		fmt.Fprintln(out, "migrate: that is the live database (PODIUM_DB_PATH); copy it to a scratch folder first")
		return 2
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(out, "migrate: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	d, err := db.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(out, "migrate: %v\n", err)
		return 2
	}
	applied, err := db.Migrate(ctx, d)
	d.Close()
	if err != nil {
		fmt.Fprintf(out, "migrate: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "applied %d migrations: %v\n", len(applied), applied)
	return checkCmd([]string{"-db", *dbPath, "-tests", envOr("PODIUM_TESTS_DIR", "data/tests")}, out)
}

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}
