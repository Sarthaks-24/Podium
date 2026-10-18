-- 0005_contests: contests, their problems and people, clarifications and
-- announcements (cache/v1/m4-plan.md, PLAN.md sections 8 and 9). Standings
-- are computed from submissions and cached in memory: one server, a few
-- thousand rows per contest. Times are Unix milliseconds, UTC.

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
  graded              INTEGER NOT NULL DEFAULT 1 CHECK (graded IN (0, 1)),
  published           INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
  practice_after_end  INTEGER NOT NULL DEFAULT 1 CHECK (practice_after_end IN (0, 1)),
  stop_on_sample_fail INTEGER NOT NULL DEFAULT 1 CHECK (stop_on_sample_fail IN (0, 1)),
  min_gap_s           INTEGER NOT NULL DEFAULT 30 CHECK (min_gap_s BETWEEN 0 AND 3600),
  max_per_problem     INTEGER NOT NULL DEFAULT 15 CHECK (max_per_problem BETWEEN 1 AND 1000),
  languages           TEXT    NOT NULL DEFAULT '["cpp17","java21","python3","node22"]',
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

-- pinned_hash is the problem's content hash when it was added; the
-- contest only runs a problem whose validated hash still equals it.
CREATE TABLE contest_problems (
  contest_id  INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  problem_id  INTEGER NOT NULL REFERENCES problems (id),
  label       TEXT    NOT NULL CHECK (length(label) BETWEEN 1 AND 3),
  ord         INTEGER NOT NULL,
  pinned_hash TEXT    NOT NULL,
  -- the subtask points when it was pinned (a JSON array): the board
  -- scores against these, not against a later edit
  points      TEXT    NOT NULL DEFAULT '[]',
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

CREATE TABLE contest_admins (
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

-- One row per student who opened the contest: when they accepted the
-- honour code and, in window mode, when their clock started.
CREATE TABLE contest_starts (
  contest_id         INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id            INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  started_at         INTEGER NOT NULL,
  honour_accepted_at INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

-- Extra time for one student (an accommodation, a crashed laptop).
CREATE TABLE contest_extensions (
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  minutes    INTEGER NOT NULL CHECK (minutes BETWEEN 0 AND 1440),
  reason     TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (contest_id, user_id)
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

CREATE INDEX clarifications_by_contest ON clarifications (contest_id, id);

CREATE TABLE announcements (
  id         INTEGER PRIMARY KEY,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  body_md    TEXT    NOT NULL,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
) STRICT;

-- When an organiser last asked for a submission to be judged again. A
-- rejudge waiting in the queue does not count as the student's own pending
-- submission, so it never blocks them from submitting.
ALTER TABLE submissions ADD COLUMN rejudged_at INTEGER;

-- A retried submit returns the first submission only within the same
-- contest (0 outside contests): the same browser key must never reach a
-- submission of another contest or of practice. Replaces 0004's index.
DROP INDEX submissions_by_key;
CREATE UNIQUE INDEX submissions_by_key ON submissions (user_id, problem_id, coalesce(contest_id, 0), client_key)
  WHERE client_key <> '';

-- The run rate checks (one pending run, the gap between runs) read only
-- this index, never the rows with their inline code.
CREATE INDEX submissions_by_kind ON submissions (user_id, kind, state, created_at);

-- The points earned per subtask (a JSON array like [40,0]), written when
-- judging finishes. With the index below the standings read only the
-- index, never the rows with their inline code and full results.
ALTER TABLE submissions ADD COLUMN earned TEXT NOT NULL DEFAULT '';
UPDATE submissions SET earned = (SELECT json_group_array(coalesce(json_extract(value, '$.earned'), 0))
  FROM json_each(submissions.result, '$.subtasks'))
  WHERE kind = 'submit' AND state = 'done' AND verdict <> 'CE' AND json_valid(result);
CREATE INDEX submissions_by_board ON submissions (contest_id, kind, practice, user_id, problem_id, created_at, state, score, earned);
