-- 0010_revisions: problem revisions, pins by revision, the pin history and
-- the served log (v2 plan B2.1, decisions D61 and D69; open tests D115).
-- Expand only: contest_problems.pinned_hash stays one more release. Times
-- are Unix milliseconds, UTC. podium_sha256(text) is registered by the db
-- package (the lowercase hex SHA-256, as problems.SHA computes it).

-- The retention cutoffs the append-only triggers read (D69): a row older
-- than its table's cutoff may be deleted, nothing newer may. No row means
-- nothing may be deleted. Only the retention job (P3) writes here.
CREATE TABLE retention_cutoffs (
  tbl       TEXT    PRIMARY KEY,
  before_ms INTEGER NOT NULL
) STRICT;

-- A cutoff can only reach rows older than the shortest retention period
-- (90 days for unused draft revisions, a year for everything else; the
-- real periods, in P3, are longer), so a bug or a stray statement cannot
-- open the append-only tables by writing a cutoff of now.
CREATE TRIGGER retention_cutoffs_floor_insert BEFORE INSERT ON retention_cutoffs
WHEN NEW.before_ms > CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)
  - (CASE NEW.tbl WHEN 'problem_revisions' THEN 90 ELSE 365 END) * 86400000
BEGIN
  SELECT RAISE(ABORT, 'a retention cutoff may not be that recent');
END;

CREATE TRIGGER retention_cutoffs_floor_update BEFORE UPDATE ON retention_cutoffs
WHEN NEW.before_ms > CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)
  - (CASE NEW.tbl WHEN 'problem_revisions' THEN 90 ELSE 365 END) * 86400000
BEGIN
  SELECT RAISE(ABORT, 'a retention cutoff may not be that recent');
END;

-- One row per version of a problem. The problems row stays the editable
-- working copy; every save that changes anything adds a revision, a save
-- that changes nothing adds none. Programs are kept by hash (their text is
-- in the working copy and in the blob store whenever a job used them);
-- tests are listed in revision_tests by blob hash.
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
  samples            TEXT    NOT NULL DEFAULT '[]',
  open_tests         TEXT    NOT NULL DEFAULT '[]',
  subtasks           TEXT    NOT NULL DEFAULT '[]',
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

CREATE INDEX problem_revisions_by_hash ON problem_revisions (problem_id, content_hash);

-- The tests of a revision. kind is sample, open (D115: run by Run, not
-- scored) or hidden (in a subtask, judged by Submit; subtask membership is
-- in problem_revisions.subtasks). No foreign key to blobs: the files of a
-- revision no contest pins and no submission names may be collected.
CREATE TABLE revision_tests (
  revision_id INTEGER NOT NULL REFERENCES problem_revisions (id),
  idx         INTEGER NOT NULL CHECK (idx >= 1),
  kind        TEXT    NOT NULL CHECK (kind IN ('sample', 'open', 'hidden')),
  input_sha   TEXT    NOT NULL,
  output_sha  TEXT,
  input_size  INTEGER NOT NULL DEFAULT 0,
  output_size INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (revision_id, idx)
) STRICT;

-- Every change of the revision a contest label pins, from_at onward.
CREATE TABLE contest_pin_history (
  id          INTEGER PRIMARY KEY,
  contest_id  INTEGER NOT NULL REFERENCES contests (id),
  label       TEXT    NOT NULL,
  problem_id  INTEGER NOT NULL REFERENCES problems (id),
  revision_id INTEGER NOT NULL REFERENCES problem_revisions (id),
  from_at     INTEGER NOT NULL,
  -- NULL for the pins this migration mapped
  actor_id    INTEGER REFERENCES users (id),
  actor_email TEXT    NOT NULL DEFAULT '',
  actor_role  TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX contest_pin_history_by_contest ON contest_pin_history (contest_id, label, from_at);

-- The served log: the first time the contest workspace served a revision
-- to a student. With the pin history it answers "what X saw at T".
CREATE TABLE revision_views (
  user_id       INTEGER NOT NULL REFERENCES users (id),
  contest_id    INTEGER NOT NULL REFERENCES contests (id),
  revision_id   INTEGER NOT NULL REFERENCES problem_revisions (id),
  first_seen_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, contest_id, revision_id)
) STRICT;

-- Open tests of the working copy (D115): test numbers, a JSON array like
-- samples. Run executes them after the samples; they are in no subtask.
ALTER TABLE problems ADD COLUMN open_tests TEXT NOT NULL DEFAULT '[]';

-- Contests pin a revision; submissions and jobs record the revision they
-- were judged against. NULL for rows from before this release.
ALTER TABLE contest_problems ADD COLUMN revision_id INTEGER REFERENCES problem_revisions (id);
ALTER TABLE submissions ADD COLUMN revision_id INTEGER REFERENCES problem_revisions (id);
ALTER TABLE jobs ADD COLUMN revision_id INTEGER REFERENCES problem_revisions (id);

-- Backfill, step 1: pins whose hash is not the working copy's. Each such
-- (problem, hash) becomes a reconstructed revision, its tests taken from
-- the newest submit job of a contest pinning that hash (a contest only
-- judges a problem whose content equals its pin).
CREATE TEMP TABLE old_pins AS
  SELECT cp.problem_id AS problem_id, cp.pinned_hash AS hash, min(cp.contest_id) AS first_contest,
    (SELECT max(s.job_id) FROM submissions s JOIN contest_problems cp2
       ON cp2.contest_id = s.contest_id AND cp2.problem_id = s.problem_id
     WHERE s.problem_id = cp.problem_id AND cp2.pinned_hash = cp.pinned_hash
       AND s.kind = 'submit' AND s.job_id IS NOT NULL) AS job_id
  FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
  WHERE cp.pinned_hash <> p.content_hash
  GROUP BY cp.problem_id, cp.pinned_hash;

INSERT INTO problem_revisions (problem_id, rev, content_hash, statement_hash, title, statement_md, statement_recorded,
    time_limit_ms, memory_limit_mb, checker, epsilon, checker_sha, samples, subtasks, reconstructed, created_by, created_at, reason)
  SELECT o.problem_id,
    row_number() OVER (PARTITION BY o.problem_id ORDER BY o.first_contest, o.hash),
    o.hash, '', p.title, '', 0,
    json_extract(j.payload, '$.time_limit_ms'), json_extract(j.payload, '$.memory_limit_mb'),
    json_extract(j.payload, '$.checker.type'), json_extract(j.payload, '$.checker.epsilon'),
    coalesce(json_extract(j.payload, '$.checker.source_sha'), ''),
    coalesce((SELECT json_group_array(json_extract(t.value, '$.idx')) FROM json_each(j.payload, '$.tests') t
      WHERE json_extract(t.value, '$.sample')), '[]'),
    coalesce((SELECT json_group_array(json_object('idx', json_extract(st.value, '$.idx'), 'points', json_extract(st.value, '$.points'),
        'title', '', 'tests', coalesce((SELECT group_concat(json_extract(t.value, '$.idx'), ',') FROM json_each(j.payload, '$.tests') t
          WHERE json_extract(t.value, '$.subtask') = json_extract(st.value, '$.idx') AND NOT json_extract(t.value, '$.sample')), '')))
      FROM json_each(j.payload, '$.subtasks') st), '[]'),
    1, NULL, coalesce((SELECT created_at FROM contests WHERE id = o.first_contest), p.created_at),
    'Reconstructed at the upgrade to v1.2.0 from a contest pin: tests only, statement not recorded.'
  FROM temp.old_pins o JOIN problems p ON p.id = o.problem_id LEFT JOIN jobs j ON j.id = o.job_id;

INSERT INTO revision_tests (revision_id, idx, kind, input_sha, output_sha, input_size, output_size)
  SELECT r.id, json_extract(t.value, '$.idx'),
    CASE WHEN json_extract(t.value, '$.sample') THEN 'sample' ELSE 'hidden' END,
    json_extract(t.value, '$.input_sha'), json_extract(t.value, '$.output_sha'),
    coalesce((SELECT size FROM blobs WHERE sha = json_extract(t.value, '$.input_sha')), 0),
    coalesce((SELECT size FROM blobs WHERE sha = json_extract(t.value, '$.output_sha')), 0)
  FROM problem_revisions r
  JOIN temp.old_pins o ON o.problem_id = r.problem_id AND o.hash = r.content_hash AND r.reconstructed = 1
  JOIN jobs j ON j.id = o.job_id, json_each(j.payload, '$.tests') t
  WHERE true
  ON CONFLICT (revision_id, idx) DO NOTHING;

-- Step 2: the working copy of every problem is its newest revision
-- (revision 1 unless step 1 made older ones).
INSERT INTO problem_revisions (problem_id, rev, content_hash, statement_hash, title, statement_md, statement_recorded,
    time_limit_ms, memory_limit_mb, checker, epsilon, checker_sha, reference_language, reference_sha,
    brute_language, brute_sha, validator_language, validator_sha, generator_language, generator_sha,
    samples, open_tests, subtasks, reconstructed, created_by, created_at, reason)
  SELECT p.id, coalesce((SELECT max(r.rev) FROM problem_revisions r WHERE r.problem_id = p.id), 0) + 1,
    p.content_hash, podium_sha256(p.statement_md), p.title, p.statement_md, 1,
    p.time_limit_ms, p.memory_limit_mb, p.checker, p.epsilon,
    CASE WHEN p.checker = 'custom' THEN podium_sha256(p.checker_source) ELSE '' END,
    p.reference_language, podium_sha256(p.reference_source),
    CASE WHEN p.brute_source <> '' THEN p.brute_language ELSE '' END,
    CASE WHEN p.brute_source <> '' THEN podium_sha256(p.brute_source) ELSE '' END,
    CASE WHEN p.validator_source <> '' THEN p.validator_language ELSE '' END,
    CASE WHEN p.validator_source <> '' THEN podium_sha256(p.validator_source) ELSE '' END,
    CASE WHEN p.generator_source <> '' THEN p.generator_language ELSE '' END,
    CASE WHEN p.generator_source <> '' THEN podium_sha256(p.generator_source) ELSE '' END,
    p.samples, p.open_tests,
    coalesce((SELECT json_group_array(json_object('idx', st.idx, 'points', st.points, 'title', st.title, 'tests', st.tests))
      FROM (SELECT idx, points, title, tests FROM subtasks WHERE problem_id = p.id ORDER BY idx) st), '[]'),
    0, NULL, p.updated_at, ''
  FROM problems p;

INSERT INTO revision_tests (revision_id, idx, kind, input_sha, output_sha, input_size, output_size)
  SELECT r.id, t.idx,
    CASE WHEN EXISTS (SELECT 1 FROM json_each(p.samples) WHERE value = t.idx) THEN 'sample'
      WHEN EXISTS (SELECT 1 FROM json_each(p.open_tests) WHERE value = t.idx) THEN 'open' ELSE 'hidden' END,
    t.input_sha, t.output_sha, t.input_size, t.output_size
  FROM problems p
  JOIN problem_revisions r ON r.problem_id = p.id AND r.reconstructed = 0 AND r.content_hash = p.content_hash
  JOIN testcases t ON t.problem_id = p.id;

-- Step 3: map every pin to its revision, and start the pin history with
-- the time of the contest's last problem change (or its creation).
UPDATE contest_problems SET revision_id = (
  SELECT r.id FROM problem_revisions r
  WHERE r.problem_id = contest_problems.problem_id AND r.content_hash = contest_problems.pinned_hash
  ORDER BY r.reconstructed, r.rev DESC LIMIT 1);

INSERT INTO contest_pin_history (contest_id, label, problem_id, revision_id, from_at)
  SELECT cp.contest_id, cp.label, cp.problem_id, cp.revision_id,
    coalesce((SELECT max(a.created_at) FROM contest_audit a WHERE a.contest_id = cp.contest_id AND a.action = 'problems'), c.created_at)
  FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
  WHERE cp.revision_id IS NOT NULL
  ORDER BY cp.contest_id, cp.ord;

DROP TABLE temp.old_pins;

-- Append only (D69): no update, and a delete only of rows older than the
-- table's retention cutoff. A revision's tests go with their revision.
CREATE TRIGGER problem_revisions_no_update BEFORE UPDATE ON problem_revisions
BEGIN
  SELECT RAISE(ABORT, 'problem_revisions is append only');
END;

CREATE TRIGGER problem_revisions_no_delete BEFORE DELETE ON problem_revisions
WHEN OLD.created_at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'problem_revisions'), 0)
BEGIN
  SELECT RAISE(ABORT, 'problem_revisions is append only');
END;

CREATE TRIGGER revision_tests_no_update BEFORE UPDATE ON revision_tests
BEGIN
  SELECT RAISE(ABORT, 'revision_tests is append only');
END;

CREATE TRIGGER revision_tests_no_delete BEFORE DELETE ON revision_tests
WHEN (SELECT created_at FROM problem_revisions WHERE id = OLD.revision_id)
  >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'problem_revisions'), 0)
BEGIN
  SELECT RAISE(ABORT, 'revision_tests is append only');
END;

CREATE TRIGGER contest_pin_history_no_update BEFORE UPDATE ON contest_pin_history
BEGIN
  SELECT RAISE(ABORT, 'contest_pin_history is append only');
END;

CREATE TRIGGER contest_pin_history_no_delete BEFORE DELETE ON contest_pin_history
WHEN OLD.from_at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'contest_pin_history'), 0)
BEGIN
  SELECT RAISE(ABORT, 'contest_pin_history is append only');
END;

CREATE TRIGGER revision_views_no_update BEFORE UPDATE ON revision_views
BEGIN
  SELECT RAISE(ABORT, 'revision_views is append only');
END;

CREATE TRIGGER revision_views_no_delete BEFORE DELETE ON revision_views
WHEN OLD.first_seen_at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'revision_views'), 0)
BEGIN
  SELECT RAISE(ABORT, 'revision_views is append only');
END;
