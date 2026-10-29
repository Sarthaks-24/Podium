-- Podium v3 schema, squashed from v2's migrations 0001 to 0015 (V3-3).
-- Nothing was installed, so there is no data to carry over. Tables, then
-- indexes, then triggers; comments inside each CREATE TABLE are the ones
-- the original migrations wrote. Schema changes after this are new
-- numbered files (CLAUDE.md section 9).

CREATE TABLE allowed_domains (
  domain     TEXT    PRIMARY KEY COLLATE NOCASE,
  -- 1 once a Google token carried hd = domain: from then on every
  -- sign-in must carry it, so a consumer Google account on a college
  -- address (an alumnus) no longer gets in.
  workspace  INTEGER NOT NULL DEFAULT 0 CHECK (workspace IN (0, 1)),
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE announcements (
  id         INTEGER PRIMARY KEY,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  body_md    TEXT    NOT NULL,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE blobs (
  sha        TEXT    PRIMARY KEY CHECK (length(sha) = 64),
  size       INTEGER NOT NULL CHECK (size >= 0),
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE clarifications (
  id            INTEGER PRIMARY KEY,
  contest_id    INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  problem_label TEXT,
  asked_by      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  question      TEXT    NOT NULL,
  answer        TEXT    NOT NULL DEFAULT '',
  answered_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
  public        INTEGER NOT NULL DEFAULT 0 CHECK (public IN (0, 1)),
  created_at    INTEGER NOT NULL,
  answered_at   INTEGER
) STRICT;

CREATE TABLE contest_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE RESTRICT,
  actor_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  -- the actor's email and site role when they acted, so the record reads
  -- the same after a later role change
  actor_email TEXT   NOT NULL,
  actor_role  TEXT   NOT NULL,
  action     TEXT    NOT NULL,
  details    TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(details)),
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE contest_extensions (
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  minutes    INTEGER NOT NULL CHECK (minutes BETWEEN 0 AND 1440),
  reason     TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (contest_id, user_id)
) STRICT;

CREATE TABLE contest_problems (
  contest_id  INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  problem_id  INTEGER NOT NULL REFERENCES problems (id),
  label       TEXT    NOT NULL CHECK (length(label) BETWEEN 1 AND 3),
  ord         INTEGER NOT NULL,
  points      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(points)), revision_id INTEGER REFERENCES problem_revisions (id),
  PRIMARY KEY (contest_id, label),
  UNIQUE (contest_id, problem_id)
) STRICT;

CREATE TABLE contest_roster (
  contest_id   INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  email        TEXT    NOT NULL COLLATE NOCASE,
  display_name TEXT    NOT NULL DEFAULT '',
  roll_number  TEXT    NOT NULL DEFAULT '',
  section      TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (contest_id, email)
) STRICT;

CREATE TABLE contest_starts (
  contest_id         INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id            INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  started_at         INTEGER NOT NULL,
  honour_accepted_at INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

CREATE TABLE contests (
  id                  INTEGER PRIMARY KEY,
  slug                TEXT    NOT NULL UNIQUE,
  title               TEXT    NOT NULL,
  description_md      TEXT    NOT NULL DEFAULT '',
  starts_at           INTEGER NOT NULL,
  ends_at             INTEGER NOT NULL CHECK (ends_at > starts_at),
  start_mode          TEXT    NOT NULL DEFAULT 'fixed' CHECK (start_mode IN ('fixed', 'window')),
  duration_minutes    INTEGER NOT NULL DEFAULT 0,
  freeze_minutes      INTEGER NOT NULL DEFAULT 0 CHECK (freeze_minutes >= 0),
  scoring_mode        TEXT    NOT NULL DEFAULT 'best_subtask' CHECK (scoring_mode IN ('best_subtask', 'best_submission')),
  access              TEXT    NOT NULL DEFAULT 'domain' CHECK (access IN ('domain', 'roster')),
  -- 'full' (showing hidden tests) is deliberately absent: hidden tests
  -- never show input or output (decision D47).
  feedback_mode       TEXT    NOT NULL DEFAULT 'subtask' CHECK (feedback_mode IN ('subtask', 'exam')),
  published           INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
  practice_after_end  INTEGER NOT NULL DEFAULT 1 CHECK (practice_after_end IN (0, 1)),
  stop_on_sample_fail INTEGER NOT NULL DEFAULT 1 CHECK (stop_on_sample_fail IN (0, 1)),
  min_gap_s           INTEGER NOT NULL DEFAULT 30 CHECK (min_gap_s BETWEEN 0 AND 3600),
  max_per_problem     INTEGER NOT NULL DEFAULT 15 CHECK (max_per_problem BETWEEN 1 AND 1000),
  languages           TEXT    NOT NULL DEFAULT '["cpp17","java21","python3","node22"]' CHECK (json_valid(languages)),
  honour_text         TEXT    NOT NULL DEFAULT '',
  editorial_md        TEXT    NOT NULL DEFAULT '',
  editorial_published INTEGER NOT NULL DEFAULT 0 CHECK (editorial_published IN (0, 1)),
  -- judging paused: queued submissions wait, nothing is lost
  paused              INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0, 1)),
  extended_minutes    INTEGER NOT NULL DEFAULT 0 CHECK (extended_minutes >= 0),
  -- hold_reveal keeps students on the frozen board after the end until an
  -- organiser reveals it (revealed_at), so a projector walk is a surprise.
  hold_reveal         INTEGER NOT NULL DEFAULT 0 CHECK (hold_reveal IN (0, 1)),
  revealed_at         INTEGER,
  archived            INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
  created_by          INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at          INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE drafts (
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  problem_id INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  contest_id INTEGER NOT NULL DEFAULT 0,
  language   TEXT    NOT NULL,
  source     TEXT    NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, problem_id, contest_id)
) STRICT;

CREATE TABLE generations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  all_outputs  INTEGER NOT NULL DEFAULT 0 CHECK (all_outputs IN (0, 1)),
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE input_generations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE integrity_cases (
  id         INTEGER PRIMARY KEY,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- excluded hides the student from the contest's board
  decision   TEXT    NOT NULL CHECK (decision IN ('no_action', 'warning', 'excluded')),
  reason     TEXT    NOT NULL,
  decided_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  decided_at INTEGER NOT NULL,
  UNIQUE (contest_id, user_id)
) STRICT;

CREATE TABLE integrity_events (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  problem_id INTEGER NOT NULL,
  contest_id INTEGER NOT NULL DEFAULT 0,
  kind       TEXT    NOT NULL CHECK (kind IN ('paste', 'blur', 'focus', 'second_session')),
  size       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
) STRICT;

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

CREATE TABLE job_results (
  job_id    INTEGER NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
  seq       INTEGER NOT NULL,
  kind      TEXT    NOT NULL CHECK (kind IN ('compile', 'test')),
  idx       INTEGER NOT NULL DEFAULT 0,
  role      TEXT    NOT NULL,
  verdict   TEXT    NOT NULL,
  time_ms   INTEGER NOT NULL DEFAULT 0,
  memory_kb INTEGER NOT NULL DEFAULT 0,
  message   TEXT    NOT NULL DEFAULT '',
  -- Run jobs: the start of the program's output. Generate jobs: the hash
  -- of the uploaded output. Never set for submissions (hidden tests stay
  -- hidden, D47), except output_hash for integrity checks.
  output    TEXT    NOT NULL DEFAULT '',
  output_sha TEXT   NOT NULL DEFAULT '',
  -- Run jobs only: the end of the program's stderr, at most 4 KB. Never
  -- set for submissions (a program can print a hidden input there).
  stderr    TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (job_id, seq),
  UNIQUE (job_id, kind, idx, role)
) STRICT;

CREATE TABLE jobs (
  id               INTEGER PRIMARY KEY,
  kind             TEXT    NOT NULL CHECK (kind IN ('validate', 'submit', 'run', 'generate', 'generate_inputs')),
  priority         INTEGER NOT NULL,
  state            TEXT    NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'leased', 'done', 'failed')),
  payload          TEXT    NOT NULL,
  -- The server's own verdict once the job is closed (a JSON object).
  summary          TEXT    NOT NULL DEFAULT '',
  error            TEXT    NOT NULL DEFAULT '',
  -- SHA-256 of the lease token the judge holds; the token itself is never
  -- stored.
  lease_hash       TEXT,
  lease_expires_at INTEGER,
  -- A job still running at this time fails even if its worker keeps
  -- renewing the lease, so one job cannot hold a lane forever.
  deadline_at      INTEGER,
  attempts         INTEGER NOT NULL DEFAULT 0,
  worker           TEXT    NOT NULL DEFAULT '',
  -- podium-judge and isolate versions of the worker that ran it.
  worker_version   TEXT    NOT NULL DEFAULT '',
  created_by       INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at       INTEGER NOT NULL,
  started_at       INTEGER,
  finished_at      INTEGER
, contest_id INTEGER, revision_id INTEGER REFERENCES problem_revisions (id)) STRICT;

CREATE TABLE problem_revisions (
  id                 INTEGER PRIMARY KEY,
  problem_id         INTEGER NOT NULL REFERENCES problems (id),
  rev                INTEGER NOT NULL CHECK (rev >= 1),
  -- problems.content_hash of this version (D45): what validation covers
  content_hash       TEXT    NOT NULL,
  -- SHA-256 of statement_md; '' when the statement was not recorded
  statement_hash     TEXT    NOT NULL DEFAULT '',
  title              TEXT    NOT NULL,
  statement_md       TEXT    NOT NULL DEFAULT '',
  statement_recorded INTEGER NOT NULL DEFAULT 1 CHECK (statement_recorded IN (0, 1)),
  -- NULL only in a reconstructed revision whose limits no job recorded
  time_limit_ms      INTEGER,
  memory_limit_mb    INTEGER,
  checker            TEXT,
  epsilon            REAL,
  checker_sha        TEXT    NOT NULL DEFAULT '',
  reference_language TEXT    NOT NULL DEFAULT '',
  reference_sha      TEXT    NOT NULL DEFAULT '',
  brute_language     TEXT    NOT NULL DEFAULT '',
  brute_sha          TEXT    NOT NULL DEFAULT '',
  validator_language TEXT    NOT NULL DEFAULT '',
  validator_sha      TEXT    NOT NULL DEFAULT '',
  generator_language TEXT    NOT NULL DEFAULT '',
  generator_sha      TEXT    NOT NULL DEFAULT '',
  -- test numbers (JSON arrays) and [{idx, points, title, tests}]
  samples            TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(samples)),
  subtasks           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(subtasks)),
  -- made at this upgrade for a contest pin older than the working copy:
  -- tests and limits from the contest's newest judging job, no statement
  reconstructed      INTEGER NOT NULL DEFAULT 0 CHECK (reconstructed IN (0, 1)),
  -- NULL for revisions the server made (this backfill, generated outputs)
  created_by         INTEGER REFERENCES users (id),
  created_at         INTEGER NOT NULL,
  -- the setter's reason, required while a live contest pins the problem
  reason             TEXT    NOT NULL DEFAULT '',
  UNIQUE (problem_id, rev)
) STRICT;

CREATE TABLE problems (
  id                 INTEGER PRIMARY KEY,
  slug               TEXT    NOT NULL UNIQUE,
  title              TEXT    NOT NULL,
  statement_md       TEXT    NOT NULL DEFAULT '',
  time_limit_ms      INTEGER NOT NULL CHECK (time_limit_ms BETWEEN 100 AND 10000),
  memory_limit_mb    INTEGER NOT NULL CHECK (memory_limit_mb BETWEEN 16 AND 1024),
  checker            TEXT    NOT NULL CHECK (checker IN ('tokens', 'exact', 'float', 'custom')),
  epsilon            REAL    NOT NULL DEFAULT 1e-6,
  checker_source     TEXT    NOT NULL DEFAULT '',
  reference_language TEXT    NOT NULL,
  reference_source   TEXT    NOT NULL DEFAULT '',
  -- An empty source means the problem has no brute force or validator.
  brute_language     TEXT    NOT NULL DEFAULT '',
  brute_source       TEXT    NOT NULL DEFAULT '',
  validator_language TEXT    NOT NULL DEFAULT '',
  validator_source   TEXT    NOT NULL DEFAULT '',
  tags               TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
  difficulty         TEXT    NOT NULL DEFAULT '' CHECK (difficulty IN ('', 'easy', 'medium', 'hard')),
  -- Test numbers shown as samples (a JSON array). Samples carry no points.
  samples            TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(samples)),
  origin             TEXT    NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'import', 'ai')),
  author_id          INTEGER REFERENCES users (id) ON DELETE SET NULL,
  archived           INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  -- The hash of everything validation depends on (D45). The problem is
  -- validated when validated_hash equals it; any edit to tests, limits,
  -- checker or solutions changes it, so no flag needs clearing.
  content_hash       TEXT    NOT NULL,
  validated_hash     TEXT,
  validated_at       INTEGER
, generator_language TEXT NOT NULL DEFAULT '', generator_source   TEXT NOT NULL DEFAULT '', generator_plan     TEXT NOT NULL DEFAULT '[]', solution_by_person INTEGER NOT NULL DEFAULT 0 CHECK (solution_by_person IN (0, 1))) STRICT;

CREATE TABLE revision_tests (
  revision_id INTEGER NOT NULL REFERENCES problem_revisions (id),
  idx         INTEGER NOT NULL CHECK (idx >= 1),
  kind        TEXT    NOT NULL CHECK (kind IN ('sample', 'hidden')),
  input_sha   TEXT    NOT NULL,
  output_sha  TEXT,
  input_size  INTEGER NOT NULL DEFAULT 0,
  output_size INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (revision_id, idx)
) STRICT;

CREATE TABLE sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  ip         TEXT    NOT NULL DEFAULT '',
  user_agent TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE settings (
  key        TEXT    PRIMARY KEY,
  value      TEXT    NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE site_audit (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  at            INTEGER NOT NULL,
  actor_id      INTEGER REFERENCES users (id),
  actor_email   TEXT    NOT NULL DEFAULT '',
  actor_role    TEXT    NOT NULL DEFAULT '',
  action        TEXT    NOT NULL,
  subject_email TEXT    NOT NULL DEFAULT '',
  details       TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(details))
) STRICT;

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
, rejudged_at INTEGER, earned TEXT NOT NULL DEFAULT '' CHECK (earned = '' OR json_valid(earned)), revision_id INTEGER REFERENCES problem_revisions (id)) STRICT;

CREATE TABLE subtasks (
  id         INTEGER PRIMARY KEY,
  problem_id INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  idx        INTEGER NOT NULL CHECK (idx >= 1),
  points     INTEGER NOT NULL CHECK (points BETWEEN 1 AND 1000),
  title      TEXT    NOT NULL DEFAULT '',
  -- The test numbers in this subtask as the setter wrote them ("3-5,8").
  -- Kept as written so replacing the tests does not lose the grouping; a
  -- range that no longer matches the tests blocks validation instead.
  tests      TEXT    NOT NULL DEFAULT '', generator_args TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(generator_args)),
  UNIQUE (problem_id, idx)
) STRICT;

CREATE TABLE testcases (
  id          INTEGER PRIMARY KEY,
  problem_id  INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  idx         INTEGER NOT NULL CHECK (idx >= 1),
  input_sha   TEXT    NOT NULL REFERENCES blobs (sha),
  -- NULL until the expected output exists: packs may carry inputs only,
  -- and "Generate outputs" fills them from the reference solution.
  output_sha  TEXT    REFERENCES blobs (sha),
  input_size  INTEGER NOT NULL,
  output_size INTEGER NOT NULL,
  UNIQUE (problem_id, idx)
) STRICT;

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  name          TEXT    NOT NULL DEFAULT '',
  avatar_url    TEXT    NOT NULL DEFAULT '',
  role          TEXT    NOT NULL DEFAULT 'student' CHECK (role IN ('owner', 'admin', 'student')),
  disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
  -- Google's stable account id ("sub"), bound at first sign-in. A later
  -- sign-in with the same email but a different sub is refused: email
  -- addresses can be reassigned, sub values are not.
  google_sub    TEXT UNIQUE,
  created_at    INTEGER NOT NULL,
  last_login_at INTEGER
, theme TEXT NOT NULL DEFAULT ''
  CHECK (theme = '' OR (length(theme) <= 32 AND theme NOT GLOB '*[^a-z0-9-]*')), verdicts TEXT NOT NULL DEFAULT 'standard'
  CHECK (verdicts IN ('standard', 'signal'))) STRICT;

CREATE TABLE validations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  content_hash TEXT    NOT NULL,
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX clarifications_by_contest ON clarifications (contest_id, id);

CREATE INDEX clarifications_unanswered ON clarifications (contest_id) WHERE answered_at IS NULL;

CREATE INDEX contest_audit_by_contest ON contest_audit (contest_id, id);

CREATE INDEX generations_by_problem ON generations (problem_id, id);

CREATE INDEX input_generations_by_problem ON input_generations (problem_id, id);

CREATE INDEX integrity_events_by_contest ON integrity_events (contest_id, user_id, created_at);

CREATE INDEX integrity_runs_by_contest ON integrity_runs (contest_id, id);

CREATE INDEX integrity_signals_by_run ON integrity_signals (run_id, problem_id);

CREATE INDEX integrity_signals_by_user ON integrity_signals (contest_id, user_a);

CREATE INDEX jobs_by_contest ON jobs (contest_id, state, created_at);

CREATE INDEX jobs_by_revision ON jobs (revision_id) WHERE revision_id IS NOT NULL;

CREATE INDEX jobs_by_state ON jobs (state, priority, id);

CREATE INDEX jobs_runs_kept ON jobs (created_at) WHERE kind = 'run' AND payload <> '{}';

CREATE INDEX problem_revisions_by_hash ON problem_revisions (problem_id, content_hash);

CREATE INDEX sessions_by_expiry ON sessions (expires_at);

CREATE INDEX sessions_by_user ON sessions (user_id);

CREATE INDEX site_audit_by_action ON site_audit (action, id);

CREATE INDEX site_audit_by_at ON site_audit (at);

CREATE INDEX submissions_by_board ON submissions (contest_id, kind, practice, user_id, problem_id, created_at, state, score, earned);

CREATE INDEX submissions_by_contest ON submissions (contest_id, problem_id, user_id);

CREATE INDEX submissions_by_job ON submissions (job_id);

CREATE UNIQUE INDEX submissions_by_key ON submissions (user_id, problem_id, coalesce(contest_id, 0), client_key)
  WHERE client_key <> '';

CREATE INDEX submissions_by_kind ON submissions (user_id, kind, state, created_at);

CREATE INDEX submissions_by_revision ON submissions (revision_id) WHERE revision_id IS NOT NULL;

CREATE INDEX submissions_by_user ON submissions (user_id, problem_id, id);

CREATE INDEX submissions_by_user_kind ON submissions (user_id, kind, practice, problem_id);

CREATE INDEX submissions_errors ON submissions (contest_id) WHERE state = 'error';

CREATE INDEX validations_by_problem ON validations (problem_id, id);

-- contest_final is when a contest is over for everyone: its end plus the
-- contest-wide extension plus the longest personal extension. It is the
-- SQL twin of contests.Contest.Final; queries use it instead of their own
-- copy of the rule (S1-08).
CREATE VIEW contest_final AS
SELECT c.id AS contest_id,
       c.ends_at + 60000 * (c.extended_minutes
         + coalesce((SELECT max(e.minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) AS final_at
FROM contests c;

CREATE TRIGGER contest_audit_no_delete BEFORE DELETE ON contest_audit
BEGIN
  SELECT RAISE(ABORT, 'contest_audit is append only');
END;

CREATE TRIGGER contest_audit_no_update BEFORE UPDATE ON contest_audit
BEGIN
  SELECT RAISE(ABORT, 'contest_audit is append only');
END;

CREATE TRIGGER problem_revisions_no_delete BEFORE DELETE ON problem_revisions
BEGIN
  SELECT RAISE(ABORT, 'problem_revisions is append only');
END;

CREATE TRIGGER problem_revisions_no_update BEFORE UPDATE ON problem_revisions
BEGIN
  SELECT RAISE(ABORT, 'problem_revisions is append only');
END;

CREATE TRIGGER revision_tests_no_delete BEFORE DELETE ON revision_tests
BEGIN
  SELECT RAISE(ABORT, 'revision_tests is append only');
END;

CREATE TRIGGER revision_tests_no_update BEFORE UPDATE ON revision_tests
BEGIN
  SELECT RAISE(ABORT, 'revision_tests is append only');
END;

CREATE TRIGGER site_audit_no_delete BEFORE DELETE ON site_audit
BEGIN
  SELECT RAISE(ABORT, 'site_audit is append only');
END;

CREATE TRIGGER site_audit_no_update BEFORE UPDATE ON site_audit
BEGIN
  SELECT RAISE(ABORT, 'site_audit is append only');
END;
