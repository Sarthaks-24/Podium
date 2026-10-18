-- 0006_integrity: integrity checks after a contest and the review of what
-- they find (cache/v1/integrity-plan.md, decisions D47 and D49). Signals
-- are reasons to look, not verdicts: a person opens a case, the student
-- explains, and a person decides with a written reason. Times are Unix
-- milliseconds, UTC.

-- One "Check integrity" press. A new run replaces the previous run's
-- signals; cases keep their own copy of the evidence.
-- Run and signal ids are AUTOINCREMENT so an id freed by a re-run or by
-- retention is never handed out again: a link or a case that names an old
-- id can never land on a different finding.
CREATE TABLE integrity_runs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  contest_id  INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  state       TEXT    NOT NULL DEFAULT 'running' CHECK (state IN ('running', 'done', 'error')),
  started_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
  started_at  INTEGER NOT NULL,
  finished_at INTEGER,
  error       TEXT    NOT NULL DEFAULT '',
  -- counts for the page: students, submissions compared, signals by kind
  stats       TEXT    NOT NULL DEFAULT '{}'
) STRICT;

CREATE INDEX integrity_runs_by_contest ON integrity_runs (contest_id, id);

CREATE TABLE integrity_signals (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id        INTEGER NOT NULL REFERENCES integrity_runs (id) ON DELETE CASCADE,
  contest_id    INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  problem_id    INTEGER REFERENCES problems (id) ON DELETE SET NULL,
  kind          TEXT    NOT NULL CHECK (kind IN ('similar', 'wrong_output', 'hardcoded', 'probing')),
  strength      TEXT    NOT NULL CHECK (strength IN ('strong', 'moderate', 'weak')),
  -- the raw number beside the word: containment, shared tests, distinct results
  score         REAL    NOT NULL,
  user_a        INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  user_b        INTEGER REFERENCES users (id) ON DELETE CASCADE,
  submission_a  INTEGER REFERENCES submissions (id) ON DELETE SET NULL,
  submission_b  INTEGER REFERENCES submissions (id) ON DELETE SET NULL,
  -- matched spans, test numbers, counts: what the reviewer is shown
  evidence      TEXT    NOT NULL DEFAULT '{}'
) STRICT;

CREATE INDEX integrity_signals_by_run ON integrity_signals (run_id, problem_id);
CREATE INDEX integrity_signals_by_user ON integrity_signals (contest_id, user_a);

CREATE TABLE integrity_cases (
  id         INTEGER PRIMARY KEY,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  state      TEXT    NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'awaiting_student', 'cleared', 'actioned')),
  title      TEXT    NOT NULL,
  -- the signals as they were when the case opened, so a re-run cannot
  -- change what the case is about
  evidence   TEXT    NOT NULL DEFAULT '[]',
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  closed_at  INTEGER
) STRICT;

CREATE INDEX integrity_cases_by_contest ON integrity_cases (contest_id, state);

CREATE TABLE integrity_case_users (
  case_id      INTEGER NOT NULL REFERENCES integrity_cases (id) ON DELETE CASCADE,
  user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- set the first time the case is shared with the student; from then on
  -- they see it and may explain
  shared_at    INTEGER,
  -- the latest share, which starts the student's turn: action waits until
  -- they explained after it or the response window passed. A reopen sets
  -- it back to NULL, so the case must be shared again.
  asked_at     INTEGER,
  -- the latest version in integrity_explanations, kept here for reading
  explanation  TEXT    NOT NULL DEFAULT '',
  explained_at INTEGER,
  PRIMARY KEY (case_id, user_id)
) STRICT;

CREATE INDEX integrity_case_users_by_user ON integrity_case_users (user_id);

-- Every version of a student's explanation. A new save adds a row, so the
-- text a decision relied on is never lost. created_at is unique per case
-- and student (a save in the same millisecond as the previous one is
-- stamped one millisecond later), which makes it the version's name.
CREATE TABLE integrity_explanations (
  id         INTEGER PRIMARY KEY,
  case_id    INTEGER NOT NULL REFERENCES integrity_cases (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  text       TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (case_id, user_id, created_at)
) STRICT;

-- Every state change with its reason. Append only, enforced by the
-- triggers below: nothing can update or delete a row, and that includes
-- the cascades, so a case or contest with decisions cannot be deleted at
-- all (nothing in the code deletes either; the clone rollback removes only
-- a contest created a moment earlier, which has no cases). Users are never
-- deleted, so by_user never needs to change either.
CREATE TABLE integrity_decisions (
  id         INTEGER PRIMARY KEY,
  case_id    INTEGER NOT NULL REFERENCES integrity_cases (id) ON DELETE RESTRICT,
  by_user    INTEGER REFERENCES users (id) ON DELETE RESTRICT,
  state      TEXT    NOT NULL CHECK (state IN ('open', 'awaiting_student', 'cleared', 'actioned')),
  note       TEXT    NOT NULL,
  -- the explanation versions the reviewer had in front of them, as a JSON
  -- object of user id to the version's created_at
  explanations_seen TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX integrity_decisions_by_case ON integrity_decisions (case_id, id);

CREATE TRIGGER integrity_decisions_no_update BEFORE UPDATE ON integrity_decisions
BEGIN
  SELECT RAISE(ABORT, 'integrity_decisions is append only');
END;

CREATE TRIGGER integrity_decisions_no_delete BEFORE DELETE ON integrity_decisions
BEGIN
  SELECT RAISE(ABORT, 'integrity_decisions is append only');
END;
