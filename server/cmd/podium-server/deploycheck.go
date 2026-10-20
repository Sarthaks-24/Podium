package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
)

// DeployQuietPeriod is how long before a published contest no deploy may
// start (PLAN.md section 10, D112).
const DeployQuietPeriod = 48 * time.Hour

// auditDeployForced is the contest audit action an emergency override
// writes, one row per contest that refused the deploy.
const auditDeployForced = "deploy_forced"

// guardContest is what the deploy guard needs to know about one published
// contest.
type guardContest struct {
	ID       int64
	Slug     string
	Title    string
	StartsAt time.Time
	// Close is the latest moment any student's time can end: the end, the
	// contest-wide extension and the largest personal extension.
	Close time.Time
	// Pending counts the contest's submissions and runs still waiting for
	// or being judged (practice ones aside).
	Pending int
}

// guardVerdict is the deploy guard's answer at one moment.
type guardVerdict struct {
	Allowed bool
	// Refusals names each contest that blocks the deploy, with why.
	Refusals []guardRefusal
	// AllowedUntil is when the next contest's quiet period starts; zero
	// when no published contest lies ahead.
	AllowedUntil time.Time
	NextContest  *guardContest
}

type guardRefusal struct {
	Contest guardContest
	Reason  string
	// Until is when this refusal ends on the clock; zero when it ends
	// only once the queue is empty.
	Until time.Time
}

// decideDeploy applies the rules of D112 to the published contests at now:
// refused within DeployQuietPeriod before a contest starts, while any
// student's time is running, and while the contest's submissions are
// still being judged after the end.
func decideDeploy(list []guardContest, now time.Time) guardVerdict {
	v := guardVerdict{Allowed: true}
	sort.Slice(list, func(i, j int) bool { return list[i].StartsAt.Before(list[j].StartsAt) })
	for _, c := range list {
		quiet := c.StartsAt.Add(-DeployQuietPeriod)
		switch {
		case now.Before(quiet):
			if v.NextContest == nil {
				cc := c
				v.NextContest, v.AllowedUntil = &cc, quiet
			}
		case now.Before(c.StartsAt):
			v.Refusals = append(v.Refusals, guardRefusal{c, fmt.Sprintf("starts %s, in %s: no deploy in the %d hours before a contest",
				stamp(c.StartsAt), span(c.StartsAt.Sub(now)), int(DeployQuietPeriod.Hours())), c.Close})
		case now.Before(c.Close):
			v.Refusals = append(v.Refusals, guardRefusal{c, fmt.Sprintf("is running until %s (%s from now, personal extensions included)",
				stamp(c.Close), span(c.Close.Sub(now))), c.Close})
		case c.Pending > 0:
			v.Refusals = append(v.Refusals, guardRefusal{c, fmt.Sprintf("ended %s and %d of its submissions are still waiting for or being judged",
				stamp(c.Close), c.Pending), time.Time{}})
		}
	}
	v.Allowed = len(v.Refusals) == 0
	return v
}

// loadGuardContests reads every published, unarchived contest. Only
// columns that exist since migration 0005 are read, so the check works on
// a database of any later schema.
func loadGuardContests(ctx context.Context, d *sql.DB) ([]guardContest, error) {
	rows, err := d.QueryContext(ctx, `SELECT c.id, c.slug, c.title, c.starts_at, c.ends_at, c.extended_minutes,
		(SELECT coalesce(max(e.minutes), 0) FROM contest_extensions e WHERE e.contest_id = c.id),
		(SELECT count(*) FROM submissions s WHERE s.contest_id = c.id AND s.practice = 0 AND s.state IN ('queued', 'running'))
		FROM contests c WHERE c.published = 1 AND c.archived = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []guardContest
	for rows.Next() {
		var c guardContest
		var starts, ends int64
		var extended, personal int
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &starts, &ends, &extended, &personal, &c.Pending); err != nil {
			return nil, err
		}
		c.StartsAt = time.UnixMilli(starts).UTC()
		c.Close = time.UnixMilli(ends).Add(time.Duration(extended+personal) * time.Minute).UTC()
		list = append(list, c)
	}
	return list, rows.Err()
}

// deployCheckCmd runs "podium-server deploy-check": may a new release be
// installed now? It reads the published contests straight from the
// database, so it answers whether or not the server is running (D112).
// The first line of the output is "allowed", "refused" or "allowed
// (forced)"; the exit code is 0 when allowed, 1 when refused, 2 when the
// check could not run.
//
// With -force REASON a refusal is overridden: one contest_audit row per
// refusing contest records the reason and -release, written as the owner
// account. install.sh upgrade --force calls it this way. The student
// banner of D112 reads these rows from P2 on.
func deployCheckCmd(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("deploy-check", flag.ContinueOnError)
	fs.SetOutput(out)
	dbPath := fs.String("db", envOr("PODIUM_DB_PATH", "data/podium.db"), "SQLite database of the running server")
	at := fs.String("at", "", "check at this moment instead of now (RFC 3339), for rehearsals")
	force := fs.String("force", "", "override a refusal for this reason (at least 10 characters); recorded in each contest's audit log")
	release := fs.String("release", "", "the release being installed, recorded with -force")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	now := time.Now().UTC()
	if *at != "" {
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fmt.Fprintf(out, "deploy-check: -at: %v\n", err)
			return 2
		}
		now = t.UTC()
	}
	reason := strings.TrimSpace(*force)
	if *force != "" && len(reason) < 10 {
		fmt.Fprintln(out, "deploy-check: give a reason of at least 10 characters with -force; it goes in the contest's audit log")
		return 2
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(out, "deploy-check: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	d, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		fmt.Fprintf(out, "deploy-check: open %s: %v\n", *dbPath, err)
		return 2
	}
	defer d.Close()
	list, err := loadGuardContests(ctx, d)
	if err != nil {
		fmt.Fprintf(out, "deploy-check: read contests from %s: %v\n", *dbPath, err)
		return 2
	}
	v := decideDeploy(list, now)
	if v.Allowed {
		fmt.Fprintln(out, "allowed")
		if v.NextContest != nil {
			fmt.Fprintf(out, "  next contest %q (%s) starts %s; deploys are allowed until %s\n",
				v.NextContest.Slug, v.NextContest.Title, stamp(v.NextContest.StartsAt), stamp(v.AllowedUntil))
		} else {
			fmt.Fprintln(out, "  no published contest is coming up, running or still being judged")
		}
		return 0
	}
	if reason == "" {
		fmt.Fprintln(out, "refused")
	} else {
		fmt.Fprintln(out, "allowed (forced)")
	}
	for _, r := range v.Refusals {
		fmt.Fprintf(out, "  contest %q (%s) %s\n", r.Contest.Slug, r.Contest.Title, r.Reason)
	}
	if reason == "" {
		fmt.Fprintln(out, "  to deploy anyway for a fix that cannot wait: sudo ./install.sh upgrade --force \"<reason>\"")
		return 1
	}
	d.Close()
	if err := recordForcedDeploy(ctx, *dbPath, v.Refusals, reason, *release, now); err != nil {
		fmt.Fprintf(out, "deploy-check: record the override: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "  override recorded in the audit log of %d contest(s)\n", len(v.Refusals))
	return 0
}

// recordForcedDeploy writes one contest_audit row per refusing contest,
// as the owner account (the only person who runs install.sh). P0 adds no
// table, so the per-contest audit log carries the record until the site
// audit of P2 exists.
func recordForcedDeploy(ctx context.Context, dbPath string, refusals []guardRefusal, reason, release string, now time.Time) error {
	d, err := db.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	var ownerID int64
	var email string
	err = d.QueryRowContext(ctx, "SELECT id, email FROM users WHERE role = 'owner' AND disabled = 0 ORDER BY id LIMIT 1").Scan(&ownerID, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no owner account in the database; start the server once so PODIUM_OWNER_EMAIL is created")
	}
	if err != nil {
		return err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range refusals {
		details, _ := json.Marshal(map[string]string{"reason": reason, "release": release, "refused": r.Reason})
		if _, err := tx.ExecContext(ctx, `INSERT INTO contest_audit (contest_id, actor_id, actor_email, actor_role, action, details, created_at)
			VALUES (?, ?, ?, 'owner', ?, ?, ?)`, r.Contest.ID, ownerID, email, auditDeployForced, string(details), now.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// span is a duration in whole hours, or minutes under two hours.
func span(d time.Duration) string {
	if d < 2*time.Hour {
		return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
	}
	return fmt.Sprintf("%d h", int(d.Round(time.Hour).Hours()))
}
