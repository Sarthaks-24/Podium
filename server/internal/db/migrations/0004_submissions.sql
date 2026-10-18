-- 0004_submissions: runs and submissions, drafts saved as students type,
-- and the integrity counts of decision D47 (cache/v1/m3-plan.md). Times are
-- Unix milliseconds, UTC.

-- One row per "Run" or "Submit". The judging itself is a job; verdict and
-- score are copied here when it finishes so standings never read job
-- results. contest_id is NULL outside contests (organisers trying a
-- problem in M3; practice later).
CREATE TABLE submissions (
  id            INTEGER PRIMARY KEY,
  job_id        INTEGER REFERENCES jobs (id),
  problem_id    INTEGER NOT NULL REFERENCES problems (id),
  contest_id    INTEGER,
  user_id       INTEGER NOT NULL REFERENCES users (id),
  kind          TEXT    NOT NULL CHECK (kind IN ('submit', 'run')),
  language      TEXT    NOT NULL,
  source        TEXT    NOT NULL,
  source_sha    TEXT    NOT NULL,
  -- custom input of a run, '' when the run used the samples
  input         TEXT    NOT NULL DEFAULT '',
  state         TEXT    NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'error')),
  verdict       TEXT    NOT NULL DEFAULT '',
  score         INTEGER NOT NULL DEFAULT 0,
  max_score     INTEGER NOT NULL DEFAULT 0,
  max_time_ms   INTEGER NOT NULL DEFAULT 0,
  max_memory_kb INTEGER NOT NULL DEFAULT 0,
  -- the server's full result (subtasks, tests) as JSON, set when done
  result        TEXT    NOT NULL DEFAULT '',
  error         TEXT    NOT NULL DEFAULT '',
  -- a random key the browser sends with each click, so a retried submit
  -- returns the first instead of counting twice
  client_key    TEXT    NOT NULL DEFAULT '',
  -- integrity counts (D47): characters typed and pasted in the editor
  -- since the last submission, focus losses, and a hash of IP and
  -- browser. Never keystroke timing.
  typed_chars   INTEGER NOT NULL DEFAULT 0,
  pasted_chars  INTEGER NOT NULL DEFAULT 0,
  focus_losses  INTEGER NOT NULL DEFAULT 0,
  client_hash   TEXT    NOT NULL DEFAULT '',
  -- made after a contest ended (practice): never in standings
  practice      INTEGER NOT NULL DEFAULT 0 CHECK (practice IN (0, 1)),
  created_at    INTEGER NOT NULL,
  judged_at     INTEGER
) STRICT;

CREATE INDEX submissions_by_user ON submissions (user_id, problem_id, id);
CREATE INDEX submissions_by_contest ON submissions (contest_id, problem_id, user_id);
CREATE INDEX submissions_by_job ON submissions (job_id);
CREATE UNIQUE INDEX submissions_by_key ON submissions (user_id, problem_id, client_key) WHERE client_key <> '';

-- The contest a job belongs to, NULL outside contests, so a paused
-- contest's jobs can be skipped at claim (M4).
ALTER TABLE jobs ADD COLUMN contest_id INTEGER;
CREATE INDEX jobs_by_contest ON jobs (contest_id, state);

-- "Generate outputs" runs: the reference solution run on each input, its
-- outputs becoming the expected outputs (D44, moved here from M2).
CREATE TABLE generations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  all_outputs  INTEGER NOT NULL DEFAULT 0 CHECK (all_outputs IN (0, 1)),
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX generations_by_problem ON generations (problem_id, id);

-- The code in the editor, saved every few seconds, so a refresh or another
-- machine restores it. contest_id 0 means outside a contest.
CREATE TABLE drafts (
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  problem_id INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  contest_id INTEGER NOT NULL DEFAULT 0,
  language   TEXT    NOT NULL,
  source     TEXT    NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, problem_id, contest_id)
) STRICT;

-- Paste, blur and focus events from the editor (D47): context for a
-- human reviewer, never a trigger on their own. Deleted by the retention
-- job unless part of an open case.
CREATE TABLE integrity_events (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  problem_id INTEGER NOT NULL,
  contest_id INTEGER NOT NULL DEFAULT 0,
  kind       TEXT    NOT NULL CHECK (kind IN ('paste', 'blur', 'focus', 'second_session')),
  size       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX integrity_events_by_contest ON integrity_events (contest_id, user_id, created_at);
