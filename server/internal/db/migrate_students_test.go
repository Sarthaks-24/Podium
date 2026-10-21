package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// v130Schema is the newest migration of release v1.3.0.
const v130Schema = "0012_contact"

// openV130 builds a v1.3.0 database: the v1.1.0 fixtures migrated through
// v1.2.0 and v1.3.0, plus rows v1.3.0 writes (a case with its decision, an
// explanation, a contact message, an extension), the state a real upgrade
// to v1.4.0 starts from.
func openV130(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	d, ctx := openV120(t)
	if _, err := MigrateTo(ctx, d, v130Schema); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO integrity_cases (id, contest_id, state, title, evidence, created_by, created_at, updated_at, closed_at)
			VALUES (1, 2, 'actioned', 'Week 2, A', '[]', 1, 8000, 8100, 8100)`,
		`INSERT INTO integrity_case_users (case_id, user_id, shared_at, asked_at, explanation, explained_at) VALUES (1, 2, 8000, 8000, 'mine', 8050)`,
		`INSERT INTO integrity_explanations (case_id, user_id, text, created_at) VALUES (1, 2, 'mine', 8050)`,
		`INSERT INTO integrity_decisions (case_id, by_user, state, note, created_at) VALUES (1, 1, 'open', 'opened for a reason', 8000),
			(1, 1, 'actioned', 'decided for a reason', 8100)`,
		`INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (2, 'contest', 'hello', 8200)`,
		`INSERT INTO contest_extensions (contest_id, user_id, minutes, reason) VALUES (2, 2, 15, 'laptop')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", strings.Fields(q)[:3], err)
		}
	}
	return d, ctx
}

func TestMigrateFromV130(t *testing.T) {
	d, ctx := openV130(t)
	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(applied, ",") != "0013_students,0014_results_notice" {
		t.Fatalf("applied %v", applied)
	}
	rep, err := Check(ctx, d)
	if err != nil || !rep.OK() || len(rep.Pending) != 0 {
		t.Fatalf("check after migration: %+v, %v", rep, err)
	}
	// Old rows take the defaults: a signals case, decisions without an
	// outcome, a student who does not hide progress.
	var source, outcome, users string
	var hide int
	if err := d.QueryRowContext(ctx, `SELECT c.source, d.outcome, d.outcome_users, u.hide_progress FROM integrity_cases c
		JOIN integrity_decisions d ON d.case_id = c.id AND d.state = 'actioned' JOIN users u ON u.id = 2 WHERE c.id = 1`).
		Scan(&source, &outcome, &users, &hide); err != nil {
		t.Fatal(err)
	}
	if source != "signals" || outcome != "" || users != "[]" || hide != 0 {
		t.Fatalf("defaults: %q %q %q %d", source, outcome, users, hide)
	}
	// The v1.3.0 rows are all still there.
	for table, want := range map[string]int{"integrity_explanations": 1, "integrity_decisions": 2, "contact_messages": 1, "contest_extensions": 1} {
		var n int
		d.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		if n != want {
			t.Errorf("%s: %d rows, want %d", table, n, want)
		}
	}
	// The new tables take rows.
	for _, q := range []string{
		`INSERT INTO integrity_proposals (id, case_id, after_decision, by_user, outcome, user_ids, note, reason, created_at) VALUES (1, 1, 2, 1, 'warning', '[2]', 'n', 'r', 9000)`,
		`INSERT INTO integrity_appeals (id, user_id, case_id, decision_id, text, created_at) VALUES (1, 2, 1, 2, 'please look again', 9100)`,
		`INSERT INTO integrity_appeal_rulings (appeal_id, by_user, upheld, reason, created_at) VALUES (1, 1, 0, 'kept as decided', 9200)`,
		`INSERT INTO submission_notes (id, submission_id, contest_id, user_id, by_user, text, created_at) VALUES (1, 1, 2, 2, 1, 'look at this', 1000)`,
		`INSERT INTO submission_notes (id, submission_id, contest_id, user_id, by_user, text, created_at) VALUES (2, 1, 2, 2, 1, 'and this', 90000000000000)`,
		`INSERT INTO notifications (user_id, kind, case_id, created_at, available_at) VALUES (2, 'case_shared', 1, 9000, 9000)`,
		`INSERT INTO record_views (at, viewer_id, subject_id, kind, ref_id) VALUES (1000, 1, 2, 'record', 0), (90000000000000, 1, 2, 'case', 1)`,
		`INSERT INTO student_contest_summary (contest_id, user_id, graded, feedback, score, max_score, percent, problems_attempted,
			problems_full, submissions, accepted, rank, ranked_count, problems, finalised_at) VALUES (2, 2, 1, 'subtask', 0, 100, 0, 1, 0, 1, 0, 1, 1, '[]', 9000)`,
		`INSERT INTO outcome_adjustments (contest_id, user_id, problem_id, decision_id, created_at) VALUES (2, 2, 2, 2, 9000)`,
		`INSERT INTO student_rating (user_id, value_tenths, updated_at) VALUES (2, 95, 9000)`,
		`INSERT INTO rating_history (user_id, at, delta_tenths, new_value_tenths, cause, decision_id) VALUES (2, 1000, -5, 95, 'outcome', 2)`,
		`INSERT INTO rating_history (user_id, at, delta_tenths, new_value_tenths, cause, semester_label) VALUES (2, 90000000000000, 0, 95, 'rollover', 'S1')`,
		`INSERT INTO rating_proposals (id, user_id, from_tenths, to_tenths, reason, by_user, created_at) VALUES (1, 2, 95, 50, 'r', 1, 9000)`,
		`INSERT INTO rating_proposal_closures (proposal_id, state, by_user, at) VALUES (1, 'declined', 1, 9100)`,
		`INSERT INTO rating_rollovers (label, from_at, to_at, ran_at, students) VALUES ('S1', 0, 9000, 9000, 1)`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q[:60], err)
		}
	}
	// Shape checks.
	for _, q := range []string{
		`INSERT INTO student_rating (user_id, value_tenths, updated_at) VALUES (1, 101, 1)`,
		`INSERT INTO rating_history (user_id, at, delta_tenths, new_value_tenths, cause) VALUES (2, 1, 0, -1, 'manual')`,
		`INSERT INTO rating_history (user_id, at, delta_tenths, new_value_tenths, cause, semester_label) VALUES (2, 2, 5, 100, 'rollover', 'S1')`,
		`INSERT INTO integrity_appeals (user_id, text, created_at) VALUES (2, 'no target', 1)`,
		`INSERT INTO integrity_appeals (user_id, case_id, decision_id, text, created_at) VALUES (2, 1, 2, 'a second appeal', 1)`,
		`INSERT INTO notifications (user_id, kind, created_at, available_at) VALUES (2, 'flagged', 1, 1)`,
		`INSERT INTO integrity_decisions (case_id, by_user, state, note, outcome, created_at) VALUES (1, 1, 'actioned', 'n', 'expelled', 1)`,
		`UPDATE users SET hide_progress = 2 WHERE id = 2`,
	} {
		if _, err := d.ExecContext(ctx, q); err == nil {
			t.Errorf("accepted: %s", q)
		}
	}
	// Every new history table is append only.
	for _, q := range []string{
		"UPDATE integrity_proposals SET note = 'x'",
		"DELETE FROM integrity_proposals",
		"UPDATE integrity_appeals SET text = 'changed'",
		"UPDATE integrity_appeals SET created_at = 1",
		"DELETE FROM integrity_appeals",
		"UPDATE integrity_appeal_rulings SET reason = 'x'",
		"DELETE FROM integrity_appeal_rulings",
		"UPDATE submission_notes SET text = 'x'",
		"DELETE FROM submission_notes",
		"UPDATE record_views SET kind = 'notes'",
		"DELETE FROM record_views",
		"UPDATE rating_history SET reason = 'x'",
		"DELETE FROM rating_history",
		"UPDATE rating_proposals SET to_tenths = 1",
		"DELETE FROM rating_proposals",
		"UPDATE rating_proposal_closures SET state = 'confirmed'",
		"DELETE FROM rating_proposal_closures",
		"UPDATE rating_rollovers SET students = 0",
		"DELETE FROM rating_rollovers",
		"UPDATE integrity_decisions SET outcome = 'warning'",
	} {
		if _, err := d.ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "append only") {
			t.Errorf("%s: want append only, got %v", q, err)
		}
	}
	// The case retention may empty an appeal's text and a proposal's
	// note and reason, and nothing else.
	if _, err := d.ExecContext(ctx, "UPDATE integrity_appeals SET text = '' WHERE id = 1"); err != nil {
		t.Errorf("emptying an appeal: %v", err)
	}
	if _, err := d.ExecContext(ctx, "UPDATE integrity_proposals SET note = '', reason = '' WHERE id = 1"); err != nil {
		t.Errorf("emptying a proposal: %v", err)
	}
	if _, err := d.ExecContext(ctx, "UPDATE integrity_proposals SET outcome = 'referred'"); err == nil {
		t.Error("a proposal's outcome changed")
	}
	// Notes reach the floor at 30 days; anything else still at a year.
	for q, ok := range map[string]bool{
		"INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('submission_notes', (CAST(unixepoch() AS INTEGER) - 31 * 86400) * 1000)": true,
		"INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('record_views', (CAST(unixepoch() AS INTEGER) - 31 * 86400) * 1000)":     false,
		"INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('rating_history', (CAST(unixepoch() AS INTEGER) - 300 * 86400) * 1000)":  false,
	} {
		_, err := d.ExecContext(ctx, q)
		if ok && err != nil {
			t.Errorf("%s: %v", q, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "may not be that recent")) {
			t.Errorf("%s: want refused, got %v", q, err)
		}
	}
	// The cutoff rows let old rows go and keep the new ones.
	if _, err := d.ExecContext(ctx, `INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('record_views', 2000), ('rating_history', 2000)`); err != nil {
		t.Fatal(err)
	}
	for table, col := range map[string]string{"submission_notes": "created_at", "record_views": "at", "rating_history": "at"} {
		if _, err := d.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+col+" < 2000"); err != nil {
			t.Errorf("%s past the cutoff: %v", table, err)
		}
		if _, err := d.ExecContext(ctx, "DELETE FROM "+table); err == nil {
			t.Errorf("%s: a row newer than the cutoff was deleted", table)
		}
		var n int
		d.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		if n != 1 {
			t.Errorf("%s: %d rows left, want 1", table, n)
		}
	}
	// The new index exists.
	var idx int
	d.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'submissions_by_user_kind'").Scan(&idx)
	if idx != 1 {
		t.Error("submissions_by_user_kind is missing")
	}
}

// C1-049: a contest whose ends_at passed more than a day before the 0013
// upgrade but which an extension kept live gets its "results are final"
// mark cleared by 0014, so its students are told; one that was really over
// keeps the mark.
func TestMigration0014ClearsTheMarkOfAContestStillLive(t *testing.T) {
	d, ctx := openV130(t)
	now := time.Now().UnixMilli()
	day := int64(86_400_000)
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO contests (id, slug, title, starts_at, ends_at, extended_minutes, created_at, updated_at)
			VALUES (40, 'long', 'Extended', %d, %d, %d, 1, 1)`, now-3*day, now-2*day, 3*1440),
		fmt.Sprintf(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at)
			VALUES (41, 'over', 'Over', %d, %d, 1, 1)`, now-11*day, now-10*day),
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	var live, over sql.NullInt64
	d.QueryRowContext(ctx, "SELECT results_notified_at FROM contests WHERE id = 40").Scan(&live)
	d.QueryRowContext(ctx, "SELECT results_notified_at FROM contests WHERE id = 41").Scan(&over)
	if live.Valid {
		t.Errorf("a contest live at the upgrade is still marked as told (%d)", live.Int64)
	}
	if !over.Valid {
		t.Error("a contest over long before the upgrade lost its mark")
	}
}
