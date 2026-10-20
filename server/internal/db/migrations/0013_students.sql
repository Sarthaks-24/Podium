-- 0013_students: manual review, outcomes and appeals, notices, Standing
-- and the rating, the record view log and the per-student contest
-- summaries (v2 plan P4, B4.1 to B4.9; decisions D74 as amended by D110,
-- D77 as amended, D97). Expand only: nothing is dropped or rewritten, so
-- the contract step for P3 has nothing to remove (P3 kept no column for a
-- release). Times are Unix milliseconds, UTC.

-- Cases opened from one submission ("manual") beside those opened from a
-- check's signals.
ALTER TABLE integrity_cases ADD COLUMN source TEXT NOT NULL DEFAULT 'signals' CHECK (source IN ('signals', 'manual'));
ALTER TABLE integrity_cases ADD COLUMN submission_id INTEGER REFERENCES submissions (id) ON DELETE SET NULL;
CREATE INDEX integrity_cases_by_submission ON integrity_cases (submission_id) WHERE submission_id IS NOT NULL;

-- The student's reply clock: from the first open of the latest share's
-- notice, capped from when the notice became available (held while the
-- student was in a live contest). reminded_at marks the day-5 reminder of
-- the latest share. A share resets all three.
ALTER TABLE integrity_case_users ADD COLUMN notice_available_at INTEGER;
ALTER TABLE integrity_case_users ADD COLUMN first_opened_at INTEGER;
ALTER TABLE integrity_case_users ADD COLUMN reminded_at INTEGER;

-- Outcomes on actioned decisions (the existing append-only triggers cover
-- the new columns): the kind, the problem a zero is for, the students it
-- applies to (a JSON array of user ids), the proposal it confirms, the
-- sole-organiser reason when the owner confirmed alone, and each
-- student's rank, total and problem score just before it, as a JSON
-- object by user id (boards are computed, not stored).
ALTER TABLE integrity_decisions ADD COLUMN outcome TEXT NOT NULL DEFAULT ''
  CHECK (outcome IN ('', 'warning', 'zero_problem', 'referred'));
ALTER TABLE integrity_decisions ADD COLUMN outcome_problem_id INTEGER;
ALTER TABLE integrity_decisions ADD COLUMN outcome_users TEXT NOT NULL DEFAULT '[]';
ALTER TABLE integrity_decisions ADD COLUMN proposal_id INTEGER;
ALTER TABLE integrity_decisions ADD COLUMN sole_organiser TEXT NOT NULL DEFAULT '';
ALTER TABLE integrity_decisions ADD COLUMN board_before TEXT NOT NULL DEFAULT '{}';

-- An outcome proposed by one reviewer, waiting for a second to confirm
-- it (the two-person rule). note is for reviewers, reason is the sentence
-- the student reads. Confirmation writes the decision row naming it.
CREATE TABLE integrity_proposals (
  id                 INTEGER PRIMARY KEY,
  case_id            INTEGER NOT NULL REFERENCES integrity_cases (id) ON DELETE RESTRICT,
  by_user            INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  outcome            TEXT    NOT NULL CHECK (outcome IN ('warning', 'zero_problem', 'referred')),
  outcome_problem_id INTEGER,
  user_ids           TEXT    NOT NULL CHECK (json_valid(user_ids)),
  note               TEXT    NOT NULL,
  reason             TEXT    NOT NULL,
  explanations_seen  TEXT    NOT NULL DEFAULT '{}',
  created_at         INTEGER NOT NULL
) STRICT;

CREATE INDEX integrity_proposals_by_case ON integrity_proposals (case_id, id);

CREATE TRIGGER integrity_proposals_no_update BEFORE UPDATE ON integrity_proposals
BEGIN
  SELECT RAISE(ABORT, 'integrity_proposals is append only');
END;

CREATE TRIGGER integrity_proposals_no_delete BEFORE DELETE ON integrity_proposals
BEGIN
  SELECT RAISE(ABORT, 'integrity_proposals is append only');
END;

-- A student's written appeal, of an outcome (case_id and decision_id) or
-- of a manual rating adjustment (rating_history_id). Append only, except
-- that the case retention empties the text 2 years after the decision,
-- as it does an explanation.
CREATE TABLE integrity_appeals (
  id                INTEGER PRIMARY KEY,
  user_id           INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  case_id           INTEGER REFERENCES integrity_cases (id) ON DELETE RESTRICT,
  decision_id       INTEGER REFERENCES integrity_decisions (id) ON DELETE RESTRICT,
  rating_history_id INTEGER,
  text              TEXT    NOT NULL,
  created_at        INTEGER NOT NULL,
  CHECK ((case_id IS NOT NULL AND decision_id IS NOT NULL AND rating_history_id IS NULL)
      OR (case_id IS NULL AND decision_id IS NULL AND rating_history_id IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX integrity_appeals_one_per_outcome ON integrity_appeals (decision_id, user_id) WHERE decision_id IS NOT NULL;
CREATE UNIQUE INDEX integrity_appeals_one_per_adjustment ON integrity_appeals (rating_history_id) WHERE rating_history_id IS NOT NULL;
CREATE INDEX integrity_appeals_by_user ON integrity_appeals (user_id, id);
CREATE INDEX integrity_appeals_by_case ON integrity_appeals (case_id) WHERE case_id IS NOT NULL;

CREATE TRIGGER integrity_appeals_no_update BEFORE UPDATE ON integrity_appeals
WHEN NEW.text <> '' OR NEW.id IS NOT OLD.id OR NEW.user_id IS NOT OLD.user_id OR NEW.case_id IS NOT OLD.case_id
  OR NEW.decision_id IS NOT OLD.decision_id OR NEW.rating_history_id IS NOT OLD.rating_history_id
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
  SELECT RAISE(ABORT, 'integrity_appeals is append only');
END;

CREATE TRIGGER integrity_appeals_no_delete BEFORE DELETE ON integrity_appeals
BEGIN
  SELECT RAISE(ABORT, 'integrity_appeals is append only');
END;

-- The ruling on an appeal, by a reviewer who took no part. An upheld
-- ruling on an outcome also writes a cleared decision when no student of
-- the case keeps one (decision_id).
CREATE TABLE integrity_appeal_rulings (
  id             INTEGER PRIMARY KEY,
  appeal_id      INTEGER NOT NULL UNIQUE REFERENCES integrity_appeals (id) ON DELETE RESTRICT,
  by_user        INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  upheld         INTEGER NOT NULL CHECK (upheld IN (0, 1)),
  reason         TEXT    NOT NULL,
  sole_organiser TEXT    NOT NULL DEFAULT '',
  decision_id    INTEGER,
  created_at     INTEGER NOT NULL
) STRICT;

CREATE TRIGGER integrity_appeal_rulings_no_update BEFORE UPDATE ON integrity_appeal_rulings
BEGIN
  SELECT RAISE(ABORT, 'integrity_appeal_rulings is append only');
END;

CREATE TRIGGER integrity_appeal_rulings_no_delete BEFORE DELETE ON integrity_appeal_rulings
BEGIN
  SELECT RAISE(ABORT, 'integrity_appeal_rulings is append only');
END;

-- Private notes on a contest submission, for reviewers only. Append only;
-- the retention job deletes a note 30 days after it was written unless a
-- case was opened from its submission, then with the case's evidence.
-- The submission is named by id only, so its own retention is never
-- blocked by a note.
CREATE TABLE submission_notes (
  id            INTEGER PRIMARY KEY,
  submission_id INTEGER NOT NULL,
  contest_id    INTEGER NOT NULL REFERENCES contests (id) ON DELETE RESTRICT,
  user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  by_user       INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  text          TEXT    NOT NULL CHECK (length(text) BETWEEN 1 AND 2000),
  lines         TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(lines)),
  created_at    INTEGER NOT NULL
) STRICT;

CREATE INDEX submission_notes_by_submission ON submission_notes (submission_id, id);
CREATE INDEX submission_notes_by_user ON submission_notes (user_id, id);
CREATE INDEX submission_notes_by_at ON submission_notes (created_at);

CREATE TRIGGER submission_notes_no_update BEFORE UPDATE ON submission_notes
BEGIN
  SELECT RAISE(ABORT, 'submission_notes is append only');
END;

CREATE TRIGGER submission_notes_no_delete BEFORE DELETE ON submission_notes
WHEN OLD.created_at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'submission_notes'), 0)
BEGIN
  SELECT RAISE(ABORT, 'submission_notes is append only');
END;

-- Notes reach the trigger floor at 30 days, not a year: rebuild the floor
-- triggers of 0010 with that one exception.
DROP TRIGGER retention_cutoffs_floor_insert;
DROP TRIGGER retention_cutoffs_floor_update;

CREATE TRIGGER retention_cutoffs_floor_insert BEFORE INSERT ON retention_cutoffs
WHEN NEW.before_ms > CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)
  - (CASE NEW.tbl WHEN 'problem_revisions' THEN 90 WHEN 'submission_notes' THEN 30 ELSE 365 END) * 86400000
BEGIN
  SELECT RAISE(ABORT, 'a retention cutoff may not be that recent');
END;

CREATE TRIGGER retention_cutoffs_floor_update BEFORE UPDATE ON retention_cutoffs
WHEN NEW.before_ms > CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)
  - (CASE NEW.tbl WHEN 'problem_revisions' THEN 90 WHEN 'submission_notes' THEN 30 ELSE 365 END) * 86400000
BEGIN
  SELECT RAISE(ABORT, 'a retention cutoff may not be that recent');
END;

-- A student's inbox. The text is written at read time from kind and the
-- ids, never stored, so a notice cannot carry code, evidence or a name.
-- available_at holds a notice made while the student was in a live
-- contest. Deleted 180 days after it was made, and on erasure.
CREATE TABLE notifications (
  id                INTEGER PRIMARY KEY,
  user_id           INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind              TEXT    NOT NULL CHECK (kind IN ('case_shared', 'reminder', 'case_decided', 'appeal_decided',
                                                    'standing_adjusted', 'results_final')),
  case_id           INTEGER,
  contest_id        INTEGER,
  decision_id       INTEGER,
  appeal_id         INTEGER,
  rating_history_id INTEGER,
  created_at        INTEGER NOT NULL,
  available_at      INTEGER NOT NULL,
  read_at           INTEGER
) STRICT;

CREATE INDEX notifications_by_user ON notifications (user_id, id);
CREATE INDEX notifications_unread ON notifications (user_id, available_at) WHERE read_at IS NULL;
CREATE INDEX notifications_by_at ON notifications (created_at);
CREATE UNIQUE INDEX notifications_results_once ON notifications (user_id, contest_id) WHERE kind = 'results_final';

-- Who read whose record page, case or notes (D97). Append only, kept 2
-- years, read by the owner.
CREATE TABLE record_views (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  at         INTEGER NOT NULL,
  viewer_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  subject_id INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  kind       TEXT    NOT NULL CHECK (kind IN ('record', 'case', 'notes')),
  ref_id     INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX record_views_by_subject ON record_views (subject_id, id);
CREATE INDEX record_views_by_viewer ON record_views (viewer_id, id);
CREATE INDEX record_views_by_at ON record_views (at);

CREATE TRIGGER record_views_no_update BEFORE UPDATE ON record_views
BEGIN
  SELECT RAISE(ABORT, 'record_views is append only');
END;

CREATE TRIGGER record_views_no_delete BEFORE DELETE ON record_views
WHEN OLD.at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'record_views'), 0)
BEGIN
  SELECT RAISE(ABORT, 'record_views is append only');
END;

-- One row per student and final contest: the board's result as the
-- student's own view may show it. Written when the contest turns final
-- (every window closed, judging done) and again after a rejudge or a
-- review decision; never during a live contest. problems is a JSON array
-- of per-problem results. Deleted with the contest's submissions, 4 years
-- after it ended, and on erasure.
CREATE TABLE student_contest_summary (
  contest_id         INTEGER NOT NULL REFERENCES contests (id),
  user_id            INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  graded             INTEGER NOT NULL CHECK (graded IN (0, 1)),
  feedback           TEXT    NOT NULL,
  score              INTEGER NOT NULL,
  max_score          INTEGER NOT NULL,
  percent            REAL    NOT NULL,
  problems_attempted INTEGER NOT NULL,
  problems_full      INTEGER NOT NULL,
  submissions        INTEGER NOT NULL,
  accepted           INTEGER NOT NULL,
  rank               INTEGER NOT NULL,
  ranked_count       INTEGER NOT NULL,
  problems           TEXT    NOT NULL CHECK (json_valid(problems)),
  finalised_at       INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

CREATE INDEX student_contest_summary_by_user ON student_contest_summary (user_id, contest_id);

-- When the contest's summaries were last built, when its participants
-- were told the results are final, and when a review decision last
-- changed its board (the results page says so, without names).
ALTER TABLE contests ADD COLUMN summarised_at INTEGER;
ALTER TABLE contests ADD COLUMN results_notified_at INTEGER;
ALTER TABLE contests ADD COLUMN board_adjusted_at INTEGER;

-- Zero for problem (D77 amended): standings.Compute drops these
-- students' submissions for these problems on every board. An upheld
-- appeal deletes the row; the record of both is the decision and the
-- ruling.
CREATE TABLE outcome_adjustments (
  contest_id  INTEGER NOT NULL REFERENCES contests (id),
  user_id     INTEGER NOT NULL REFERENCES users (id),
  problem_id  INTEGER NOT NULL,
  decision_id INTEGER NOT NULL REFERENCES integrity_decisions (id),
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id, problem_id, decision_id)
) STRICT;

-- The rating (D110): tenths of a point, 0 to 100, 100 when no row. Only
-- confirmed outcomes, manual edits, upheld appeals and the semester
-- rollover move it, each with a rating_history row.
CREATE TABLE student_rating (
  user_id      INTEGER PRIMARY KEY REFERENCES users (id),
  value_tenths INTEGER NOT NULL CHECK (value_tenths BETWEEN 0 AND 100),
  updated_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE rating_history (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES users (id),
  at               INTEGER NOT NULL,
  delta_tenths     INTEGER NOT NULL,
  new_value_tenths INTEGER NOT NULL CHECK (new_value_tenths BETWEEN 0 AND 100),
  cause            TEXT    NOT NULL CHECK (cause IN ('outcome', 'manual', 'appeal', 'rollover')),
  decision_id      INTEGER,
  appeal_id        INTEGER,
  proposal_id      INTEGER,
  actor_id         INTEGER,
  confirmer_id     INTEGER,
  reason           TEXT    NOT NULL DEFAULT '',
  semester_label   TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX rating_history_by_user ON rating_history (user_id, id);
CREATE INDEX rating_history_by_at ON rating_history (at);
CREATE UNIQUE INDEX rating_history_rollover_once ON rating_history (user_id, semester_label) WHERE cause = 'rollover';

CREATE TRIGGER rating_history_no_update BEFORE UPDATE ON rating_history
BEGIN
  SELECT RAISE(ABORT, 'rating_history is append only');
END;

CREATE TRIGGER rating_history_no_delete BEFORE DELETE ON rating_history
WHEN OLD.at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'rating_history'), 0)
BEGIN
  SELECT RAISE(ABORT, 'rating_history is append only');
END;

-- A manual edit that would lower a student's band, waiting for a second
-- owner or admin; and how each proposal ended.
CREATE TABLE rating_proposals (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users (id),
  from_tenths INTEGER NOT NULL CHECK (from_tenths BETWEEN 0 AND 100),
  to_tenths   INTEGER NOT NULL CHECK (to_tenths BETWEEN 0 AND 100),
  reason      TEXT    NOT NULL,
  by_user     INTEGER NOT NULL REFERENCES users (id),
  created_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX rating_proposals_by_user ON rating_proposals (user_id, id);

CREATE TRIGGER rating_proposals_no_update BEFORE UPDATE ON rating_proposals
BEGIN
  SELECT RAISE(ABORT, 'rating_proposals is append only');
END;

CREATE TRIGGER rating_proposals_no_delete BEFORE DELETE ON rating_proposals
BEGIN
  SELECT RAISE(ABORT, 'rating_proposals is append only');
END;

CREATE TABLE rating_proposal_closures (
  proposal_id INTEGER PRIMARY KEY REFERENCES rating_proposals (id),
  state       TEXT    NOT NULL CHECK (state IN ('confirmed', 'declined', 'superseded')),
  by_user     INTEGER,
  at          INTEGER NOT NULL
) STRICT;

CREATE TRIGGER rating_proposal_closures_no_update BEFORE UPDATE ON rating_proposal_closures
BEGIN
  SELECT RAISE(ABORT, 'rating_proposal_closures is append only');
END;

CREATE TRIGGER rating_proposal_closures_no_delete BEFORE DELETE ON rating_proposal_closures
BEGIN
  SELECT RAISE(ABORT, 'rating_proposal_closures is append only');
END;

-- Each semester rollover of the rating, once per label: the period it
-- covered, (from_at, to_at].
CREATE TABLE rating_rollovers (
  label    TEXT    PRIMARY KEY,
  from_at  INTEGER NOT NULL,
  to_at    INTEGER NOT NULL,
  ran_at   INTEGER NOT NULL,
  students INTEGER NOT NULL
) STRICT;

CREATE TRIGGER rating_rollovers_no_update BEFORE UPDATE ON rating_rollovers
BEGIN
  SELECT RAISE(ABORT, 'rating_rollovers is append only');
END;

CREATE TRIGGER rating_rollovers_no_delete BEFORE DELETE ON rating_rollovers
BEGIN
  SELECT RAISE(ABORT, 'rating_rollovers is append only');
END;

-- The student's choice to hide their own progress (like the theme, 0002).
ALTER TABLE users ADD COLUMN hide_progress INTEGER NOT NULL DEFAULT 0 CHECK (hide_progress IN (0, 1));

-- Upsolved problems and per-student reads.
CREATE INDEX submissions_by_user_kind ON submissions (user_id, kind, practice, problem_id);
