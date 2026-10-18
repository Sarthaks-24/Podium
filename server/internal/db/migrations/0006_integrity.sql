-- 0006_integrity: integrity checks after a contest and the review of what
-- they find (cache/v1/integrity-plan.md, decisions D47 and D49). Signals
-- are reasons to look, not verdicts: a person opens a case, the student
-- explains, and a person decides with a written reason. Times are Unix
-- milliseconds, UTC.

-- One "Check integrity" press. A new run replaces the previous run's
-- signals; cases keep their own copy of the evidence.
CREATE TABLE integrity_runs (
  id          INTEGER PRIMARY KEY,
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
  id            INTEGER PRIMARY KEY,
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
  -- set once the case is shared with the student; they explain before
  -- any decision
  shared_at    INTEGER,
  explanation  TEXT    NOT NULL DEFAULT '',
  explained_at INTEGER,
  PRIMARY KEY (case_id, user_id)
) STRICT;

CREATE INDEX integrity_case_users_by_user ON integrity_case_users (user_id);

-- Every state change with its reason. Append only: the code never updates
-- or deletes a row here.
CREATE TABLE integrity_decisions (
  id         INTEGER PRIMARY KEY,
  case_id    INTEGER NOT NULL REFERENCES integrity_cases (id) ON DELETE CASCADE,
  by_user    INTEGER REFERENCES users (id) ON DELETE SET NULL,
  state      TEXT    NOT NULL CHECK (state IN ('open', 'awaiting_student', 'cleared', 'actioned')),
  note       TEXT    NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX integrity_decisions_by_case ON integrity_decisions (case_id, id);
