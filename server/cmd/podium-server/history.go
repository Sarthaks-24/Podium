package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
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
	// sudo -u podium carries no server environment, so the VM's own path
	// is refused too, and so is any database with a -wal file beside it:
	// a copy made for a rehearsal has none, a database in use does.
	for _, live := range []string{os.Getenv("PODIUM_DB_PATH"), liveDBPath} {
		if live != "" && sameFile(live, *dbPath) {
			fmt.Fprintln(out, "migrate: that is the live database; copy it to a scratch folder first")
			return 2
		}
	}
	if _, err := os.Stat(*dbPath + "-wal"); err == nil {
		fmt.Fprintln(out, "migrate: "+*dbPath+"-wal exists, so the database may be in use; copy it with sqlite3 .backup to a scratch folder first")
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

// liveDBPath is where install.sh puts the database on the VM.
const liveDBPath = "/var/lib/podium/db/podium.db"

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}

// stamp formats a time for the operator tools.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
