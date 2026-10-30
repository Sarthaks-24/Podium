// Package retention holds how long the server keeps each kind of data
// and the daily job that empties old run payloads. The periods live in
// this one file: the jobs, their tests, the integrity retention, the
// session lifetime and GET /api/auth/config all read them,
// and the privacy page renders its table from that response, so the page
// cannot drift from what the code deletes. docs/OPERATIONS.md carries the
// same table and a test fails when the two differ.
//
// The package imports nothing of the server's own, so any package may use
// the constants.
package retention

import (
	"fmt"
	"time"
)

// Day and Year are the units of the periods below. A year is 365 days.
const (
	Day  = 24 * time.Hour
	Year = 365 * Day
)

// Every period the server applies. Submissions, scores, code, audit logs
// and integrity decisions are kept: v3 has no multi-year deletion
// (shortcut: nothing is deleted after 4 years, add a rule when a college
// policy asks for one).
const (
	// Sessions end 12 hours after sign-in however much they are used
	// (auth.SessionTTL); an hourly sweep deletes the rows.
	Sessions = 12 * time.Hour
	// IntegrityEvents: paste and focus events and the per-submission
	// counts, and the similarity check's signals, 180 days after the
	// contest ends for everyone (contests.Retain, integrity.RetainSignals).
	IntegrityEvents = 180 * Day
	// RunPayloads: a run's code reference, custom input and output
	// excerpt, 30 days after the run; the verdict row stays.
	RunPayloads = 30 * Day
	// Backups: the nightly backup keeps 14 days, locally and in the bucket
	// (deploy/backup/podium-backup, PODIUM_BACKUP_KEEP_DAYS).
	Backups = 14 * Day
)

// Period is one row of the retention table, as GET /api/auth/config
// returns it.
type Period struct {
	// ID is stable; the web app may key on it.
	ID string `json:"id"`
	// Data names what is kept, as the privacy page shows it.
	Data string `json:"data"`
	// Amount and Unit are the period: hours, days or years.
	Amount int    `json:"amount"`
	Unit   string `json:"unit"`
	// After is what the period counts from: sign_in, created,
	// contest_end or decision.
	After string `json:"after"`
	// Kept is the whole period as one phrase, like "4 years after the
	// contest".
	Kept string `json:"kept"`
	// Duration is the period itself, for the code.
	Duration time.Duration `json:"-"`
}

// afterWords reads an After value as the end of a phrase.
var afterWords = map[string]string{
	"sign_in":     "after sign-in",
	"created":     "",
	"contest_end": "after the contest",
	"decision":    "after the decision",
}

func row(id, data string, d time.Duration, after string) Period {
	p := Period{ID: id, Data: data, After: after, Duration: d}
	switch {
	case d%Year == 0:
		p.Amount, p.Unit = int(d/Year), "years"
	case d%Day == 0:
		p.Amount, p.Unit = int(d/Day), "days"
	default:
		p.Amount, p.Unit = int(d/time.Hour), "hours"
	}
	p.Kept = fmt.Sprintf("%d %s", p.Amount, p.Unit)
	if w := afterWords[after]; w != "" {
		p.Kept += " " + w
	}
	return p
}

// Periods returns the retention table in the order the privacy page lists
// it.
func Periods() []Period {
	return []Period{
		row("sessions", "Sign-in sessions", Sessions, "sign_in"),
		row("integrity_events", "Paste and focus events during contests, their counts and similar-code checks", IntegrityEvents, "contest_end"),
		row("run_payloads", "Custom input and program output of a Run", RunPayloads, "created"),
		row("backups", "Backups", Backups, "created"),
	}
}
