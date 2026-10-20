// Package retention holds how long the server keeps each kind of data
// (v2 plan section 9) and the daily job that deletes what is older. The
// periods live in this one file: the jobs, their tests, the integrity
// retention, the session lifetime and GET /api/auth/config all read them,
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

// Every period the server applies.
const (
	// Sessions end 12 hours after sign-in however much they are used
	// (auth.SessionTTL); an hourly sweep deletes the rows.
	Sessions = 12 * time.Hour
	// IntegrityEvents: paste and focus events and the per-submission
	// counts, 180 days after the contest ends for everyone, except for
	// students in an open case (contests.Retain).
	IntegrityEvents = 180 * Day
	// CaseEvidence: a closed case's evidence and explanations, 2 years
	// after the decision (integrity.Service.Retain).
	CaseEvidence = 2 * Year
	// Backups: the nightly backup keeps 14 days, locally and in the bucket
	// (deploy/backup/podium-backup, PODIUM_BACKUP_KEEP_DAYS).
	Backups = 14 * Day
	// RunPayloads: a run's code reference, custom input and output
	// excerpt, 30 days after the run; the verdict row stays.
	RunPayloads = 30 * Day
	// ContactMessages: messages sent through the contact form, 180 days.
	ContactMessages = 180 * Day
	// DraftRevisions: a problem revision no contest pins or pinned, no
	// submission or job names and no student was served, 90 days after it
	// was made. The newest revision of a problem is never deleted.
	DraftRevisions = 90 * Day
	// Submissions: submissions with their verdicts, the contest's pins and
	// what each student was served, 4 years after the contest ends (owner,
	// 2026-10-19). The standings totals are kept as an export first.
	Submissions = 4 * Year
	// Audit: the site audit and every contest audit log, 4 years (owner,
	// 2026-10-19).
	Audit = 4 * Year
	// PrivateNotes: a reviewer's note on a submission, 30 days after it
	// was written, unless a case was opened from the submission; then it
	// goes with the case's evidence (v1.4.0).
	PrivateNotes = 30 * Day
	// Notifications: a person's notices, 180 days after they were made.
	Notifications = 180 * Day
	// RecordViews: who read a student's record page, a case or notes
	// (D97), 2 years.
	RecordViews = 2 * Year
	// RatingHistory: each change of a student's rating, 4 years like the
	// audits (D110); the current value is kept, so pruning never changes
	// it.
	RatingHistory = 4 * Year
	// ContestSummaries: a student's result summary per contest, as long
	// as the submissions it comes from.
	ContestSummaries = Submissions
	// StandingEntries: an outcome or adjustment stays on the student's
	// standing 2 years after it; the decision record stays with the case.
	StandingEntries = CaseEvidence
)

// Trigger floors: the append-only tables' triggers (migration 0010)
// refuse a cutoff newer than these, so every period that reaches those
// tables must be at least as long. A test checks it.
const (
	FloorDraftRevisions = 90 * Day
	FloorNotes          = 30 * Day
	FloorOther          = 365 * Day
)

// Period is one row of the retention table, as GET /api/auth/config
// returns it (contract C5).
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
// it. Rows for data added later join it in the release that starts
// collecting that data (v1.4.0: notes, notices, record views, the rating,
// summaries, standing entries).
func Periods() []Period {
	return []Period{
		row("sessions", "Sign-in sessions", Sessions, "sign_in"),
		row("integrity_events", "Paste and focus events during contests, and their counts", IntegrityEvents, "contest_end"),
		row("case_evidence", "Evidence, explanations and appeals in a closed integrity case", CaseEvidence, "decision"),
		row("standing_entries", "Outcomes and adjustments shown on your standing", StandingEntries, "decision"),
		row("private_notes", "Organisers' private notes on a submission, unless a case is opened from it", PrivateNotes, "created"),
		row("notifications", "Notices in your inbox", Notifications, "created"),
		row("record_views", "Records of who read a student's record, cases or notes", RecordViews, "created"),
		row("rating_history", "The organisers' rating of a student and its changes", RatingHistory, "created"),
		row("contest_summaries", "Your result summary for each contest", ContestSummaries, "contest_end"),
		row("run_payloads", "Custom input and program output of a Run", RunPayloads, "created"),
		row("contact_messages", "Messages sent to the organisers", ContactMessages, "created"),
		row("draft_revisions", "Problem versions no contest or submission used", DraftRevisions, "created"),
		row("submissions", "Submissions, their verdicts and what each contest showed", Submissions, "contest_end"),
		row("audit", "Records of organiser actions", Audit, "created"),
		row("backups", "Backups", Backups, "created"),
	}
}
