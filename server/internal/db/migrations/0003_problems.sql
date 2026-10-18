-- 0003_problems: the problem bank, its tests, and the judge's job queue
-- (PLAN.md section 8 as amended by decisions D44 and D45). All times are
-- Unix milliseconds, UTC.

-- Test files and program sources the judge fetches, named by the SHA-256
-- of their content. The bytes live on disk under PODIUM_TESTS_DIR.
CREATE TABLE blobs (
  sha        TEXT    PRIMARY KEY CHECK (length(sha) = 64),
  size       INTEGER NOT NULL CHECK (size >= 0),
  created_at INTEGER NOT NULL
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
  tags               TEXT    NOT NULL DEFAULT '[]',
  difficulty         TEXT    NOT NULL DEFAULT '' CHECK (difficulty IN ('', 'easy', 'medium', 'hard')),
  -- Test numbers shown as samples (a JSON array). Samples carry no points.
  samples            TEXT    NOT NULL DEFAULT '[]',
  origin             TEXT    NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'import', 'ai')),
  source_note        TEXT    NOT NULL DEFAULT '',
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
) STRICT;

CREATE TABLE subtasks (
  id         INTEGER PRIMARY KEY,
  problem_id INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  idx        INTEGER NOT NULL CHECK (idx >= 1),
  points     INTEGER NOT NULL CHECK (points BETWEEN 1 AND 1000),
  title      TEXT    NOT NULL DEFAULT '',
  -- The test numbers in this subtask as the setter wrote them ("3-5,8").
  -- Kept as written so replacing the tests does not lose the grouping; a
  -- range that no longer matches the tests blocks validation instead.
  tests      TEXT    NOT NULL DEFAULT '',
  UNIQUE (problem_id, idx)
) STRICT;

CREATE TABLE testcases (
  id          INTEGER PRIMARY KEY,
  problem_id  INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  idx         INTEGER NOT NULL CHECK (idx >= 1),
  input_sha   TEXT    NOT NULL REFERENCES blobs (sha),
  output_sha  TEXT    NOT NULL REFERENCES blobs (sha),
  input_size  INTEGER NOT NULL,
  output_size INTEGER NOT NULL,
  UNIQUE (problem_id, idx)
) STRICT;

-- The one queue for all judge work (D44). M3's submissions reference a job
-- here rather than being a second queue. Lower priority runs first.
CREATE TABLE jobs (
  id               INTEGER PRIMARY KEY,
  kind             TEXT    NOT NULL CHECK (kind IN ('validate')),
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
) STRICT;

-- What the judge reported, one row per compile or test result, so a report
-- is an insert rather than a rewrite of everything so far. The unique key
-- refuses a result reported twice.
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
  PRIMARY KEY (job_id, seq),
  UNIQUE (job_id, kind, idx, role)
) STRICT;

CREATE INDEX jobs_by_state ON jobs (state, priority, id);

CREATE TABLE validations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  content_hash TEXT    NOT NULL,
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX validations_by_problem ON validations (problem_id, id);
