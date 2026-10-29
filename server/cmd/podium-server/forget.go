package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/db"
)

// forgetCmd runs "podium-server forget-user EMAIL": erase one person
// (B3.2, PLAN.md section 9). It is a dry run unless -apply is given: it
// prints what would be deleted and changes nothing. With -apply the
// change and its site audit row are one transaction, recorded as the
// owner account. Exit 0 when done (or the dry run worked), 1 when
// refused, 2 when it could not run.
func forgetCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("forget-user", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database of the server")
	apply := fs.Bool("apply", false, "erase for real; without it nothing changes")
	allowOwner := fs.Bool("allow-owner", false, "allow erasing an account that has the owner role")
	owner := fs.String("owner", envOr("PODIUM_OWNER_EMAIL", ""), "the owner running this, recorded in the site audit (default PODIUM_OWNER_EMAIL)")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: podium-server forget-user [-db F] [-apply] EMAIL")
		fs.PrintDefaults()
	}
	// The email may come before or after the flags.
	var email string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		email, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if email == "" && fs.NArg() == 1 {
		email = fs.Arg(0)
	} else if fs.NArg() > 0 || email == "" {
		fs.Usage()
		return 2
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(out, "forget-user: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	d, err := db.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(out, "forget-user: open %s: %v\n", *dbPath, err)
		return 2
	}
	defer d.Close()
	var actor auth.Actor
	err = d.QueryRowContext(ctx, `SELECT id, email, role FROM users WHERE role = 'owner' AND disabled = 0
		ORDER BY (email = ? COLLATE NOCASE) DESC, id LIMIT 1`, *owner).Scan(&actor.ID, &actor.Email, &actor.Role)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintln(out, "forget-user: no owner account in the database; start the server once so PODIUM_OWNER_EMAIL is created")
		return 2
	}
	if err != nil {
		fmt.Fprintf(out, "forget-user: %v\n", err)
		return 2
	}
	svc := &auth.Service{DB: d, OwnerEmail: strings.ToLower(strings.TrimSpace(*owner))}
	r, err := svc.Forget(auth.WithActor(ctx, actor), email, *apply, *allowOwner)
	var ce *auth.ChangeError
	switch {
	case errors.As(err, &ce), errors.Is(err, auth.ErrForgetOwner), errors.Is(err, auth.ErrAlreadyForgotten),
		errors.Is(err, auth.ErrForgetOwnerRole), errors.Is(err, auth.ErrNoOwnerEmail):
		fmt.Fprintf(out, "refused: %v\n", err)
		return 1
	case err != nil:
		fmt.Fprintf(out, "forget-user: %v\n", err)
		return 2
	}
	who := fmt.Sprintf("account %d (%s, %s)", r.UserID, r.Email, r.Role)
	if r.Applied {
		fmt.Fprintf(out, "forgotten: %s\n", who)
	} else {
		fmt.Fprintf(out, "dry run, nothing changed: %s\n", who)
	}
	for _, c := range r.Counts {
		fmt.Fprintf(out, "  %-30s %d\n", c.What, c.N)
	}
	fmt.Fprintf(out, "  name and email become %q, %s; Google link cleared; account disabled\n", r.Placeholder, r.PlaceholderEmail)
	for _, k := range auth.ForgetKept {
		fmt.Fprintf(out, "  kept: %s\n", k)
	}
	if !r.Applied {
		fmt.Fprintln(out, "  run again with -apply to erase")
	}
	return 0
}
