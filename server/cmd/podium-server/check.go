package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// checkCmd runs "podium-server check": SQLite's integrity and foreign key
// checks, the migrations against this binary, and every test file and
// program source against the database. It never writes to the database,
// so it is safe on a restored copy and on the live file. It returns the
// exit code: 0 when everything is sound, 1 when something is damaged or
// missing, 2 when the check could not run.
func checkCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database to check")
	testsDir := fs.String("tests", envOr("PODIUM_TESTS_DIR", "data/tests"), "test files directory that goes with it")
	hash := fs.Bool("hash", false, "also re-hash every test file (slower: reads every byte)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(out, "check: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	// Read-only: no WAL switch, no migration, nothing created.
	d, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		fmt.Fprintf(out, "check: open %s: %v\n", *dbPath, err)
		return 2
	}
	defer d.Close()
	d.SetMaxOpenConns(1)

	rep, err := db.Check(ctx, d)
	if err != nil {
		fmt.Fprintf(out, "check: %s: %v\n", *dbPath, err)
		return 2
	}
	ok := rep.OK()
	fmt.Fprintf(out, "database %s\n", *dbPath)
	fmt.Fprintf(out, "  integrity: %s\n", strings.Join(limitList(rep.Integrity, 10), "; "))
	if len(rep.ForeignKeys) == 0 {
		fmt.Fprintln(out, "  foreign keys: ok")
	} else {
		fmt.Fprintf(out, "  foreign keys: %d broken: %s\n", len(rep.ForeignKeys), strings.Join(limitList(rep.ForeignKeys, 10), "; "))
	}
	fmt.Fprintf(out, "  migrations: %d applied, %d this server would apply on start", len(rep.Applied), len(rep.Pending))
	if len(rep.Unknown) > 0 {
		fmt.Fprintf(out, ", %d unknown to this server (%s): use the newer release", len(rep.Unknown), strings.Join(rep.Unknown, ", "))
	}
	fmt.Fprintln(out)
	var counts []string
	for _, t := range db.CountedTables() {
		if n, found := rep.Counts[t]; found {
			counts = append(counts, fmt.Sprintf("%s %d", t, n))
		}
	}
	fmt.Fprintf(out, "  rows: %s\n", strings.Join(counts, ", "))

	if _, found := rep.Counts["blobs"]; found {
		v, err := problems.Blobs{Dir: *testsDir}.Verify(ctx, d, *hash)
		if err != nil {
			fmt.Fprintf(out, "check: test files: %v\n", err)
			return 2
		}
		how := "sizes checked"
		if *hash {
			how = "contents re-hashed"
		}
		fmt.Fprintf(out, "test files %s\n  %d recorded, %.1f MB, %s\n", *testsDir, v.Blobs, float64(v.Bytes)/(1<<20), how)
		for _, p := range []struct {
			what string
			list []string
		}{{"missing", v.Missing}, {"wrong size", v.WrongSize}, {"wrong content", v.BadHash}, {"named by a test or submission but not recorded", v.Dangling}} {
			if len(p.list) > 0 {
				fmt.Fprintf(out, "  %d %s: %s\n", len(p.list), p.what, strings.Join(limitList(p.list, 5), ", "))
			}
		}
		if v.Orphans > 0 {
			fmt.Fprintf(out, "  %d files with no record (harmless; removed by the server after a day)\n", v.Orphans)
		}
		ok = ok && v.OK()
	}
	if ok {
		fmt.Fprintln(out, "result: ok")
		return 0
	}
	fmt.Fprintln(out, "result: problems found")
	return 1
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func limitList(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	return append(l[:n:n], fmt.Sprintf("and %d more", len(l)-n))
}
