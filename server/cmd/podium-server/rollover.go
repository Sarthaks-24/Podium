package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/integrity"
)

// rolloverCmd runs "podium-server semester-rollover LABEL" (D110, B4.5):
// when a term ends, every student with no confirmed outcome since the
// previous rollover gets 0.5 on their rating, up to 10. A label runs once;
// a second run changes nothing. With -dry-run it reports and writes
// nothing. The run is recorded in the site audit as rating_rollover.
// Exit 0 when done, 1 when refused, 2 when it could not run.
func rolloverCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("semester-rollover", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database of the server")
	dry := fs.Bool("dry-run", false, "report what would change and write nothing")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: podium-server semester-rollover [-db F] [-dry-run] LABEL")
		fs.PrintDefaults()
	}
	var label string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		label, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if label == "" && fs.NArg() == 1 {
		label = fs.Arg(0)
	} else if fs.NArg() > 0 || label == "" {
		fs.Usage()
		return 2
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(out, "semester-rollover: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	d, err := db.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(out, "semester-rollover: open %s: %v\n", *dbPath, err)
		return 2
	}
	defer d.Close()
	rep, err := db.Check(ctx, d)
	if err != nil || len(rep.Pending) > 0 {
		fmt.Fprintln(out, "semester-rollover: the database is not on this release's schema; start the server once to migrate it")
		return 2
	}
	svc := &integrity.Service{DB: d}
	r, err := svc.Rollover(ctx, label, !*dry)
	var in *integrity.InputError
	switch {
	case err != nil && asInput(err, &in):
		fmt.Fprintf(out, "refused: %s\n", in.Message)
		return 1
	case err != nil:
		fmt.Fprintf(out, "semester-rollover: %v\n", err)
		return 2
	case r.AlreadyDone:
		fmt.Fprintf(out, "semester %q was rolled over already; nothing changed\n", r.Label)
		return 0
	}
	from := "the beginning"
	if r.From.UnixMilli() > 0 {
		from = r.From.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(out, "semester %q: outcomes from %s to %s\n", r.Label, from, r.To.UTC().Format(time.RFC3339))
	fmt.Fprintf(out, "  students given 0.5 (up to 10): %d\n  students with an outcome this term: %d\n", r.Students, r.Blocked)
	if !r.Applied {
		fmt.Fprintln(out, "dry run: nothing changed. Run again without -dry-run to apply.")
		return 0
	}
	details, _ := json.Marshal(map[string]any{"label": r.Label, "students": r.Students, "blocked": r.Blocked})
	if _, err := d.ExecContext(ctx, `INSERT INTO site_audit (at, actor_id, actor_email, actor_role, action, details)
		VALUES (?, NULL, '', '', 'rating_rollover', ?)`, time.Now().UnixMilli(), string(details)); err != nil {
		fmt.Fprintf(out, "applied, but the site audit row failed: %v\n", err)
		return 2
	}
	fmt.Fprintln(out, "applied.")
	return 0
}

func asInput(err error, target **integrity.InputError) bool {
	e, ok := err.(*integrity.InputError)
	if ok {
		*target = e
	}
	return ok
}
