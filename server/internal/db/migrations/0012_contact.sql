-- 0012_contact: the contact form, the standings kept after retention, the
-- indexes the retention job and the console's queue health read, and the
-- contract step for P2 (v2 plan B3.0, B3.1, B3.8, B3.9; decisions D71,
-- D103, D68). Times are Unix milliseconds, UTC.

-- Messages a signed-in person sent to the organisers (C6). Plain text,
-- never rendered as HTML. Deleted 180 days after they were sent, and when
-- the sender is erased (podium-server forget-user).
CREATE TABLE contact_messages (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id),
  topic      TEXT    NOT NULL CHECK (topic IN ('contest', 'account', 'privacy', 'other')),
  message    TEXT    NOT NULL CHECK (length(message) BETWEEN 1 AND 2000),
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX contact_messages_by_user ON contact_messages (user_id, created_at);
CREATE INDEX contact_messages_by_at ON contact_messages (created_at);

-- The final standings of a contest whose submissions passed retention
-- (4 years after it ended), written just before they are deleted: rank,
-- user id, total and the per-problem scores, as JSON. Names are read from
-- users, so an erased student shows under the placeholder.
CREATE TABLE contest_results (
  contest_id  INTEGER PRIMARY KEY REFERENCES contests (id),
  exported_at INTEGER NOT NULL,
  standings   TEXT    NOT NULL CHECK (json_valid(standings))
) STRICT;

-- Queue health (C10): waiting and judging counts and the oldest wait read
-- this index alone; it replaces 0004's (contest_id, state).
DROP INDEX jobs_by_contest;
CREATE INDEX jobs_by_contest ON jobs (contest_id, state, created_at);
-- Submissions that ended in a judge error, per contest: a handful at most.
CREATE INDEX submissions_errors ON submissions (contest_id) WHERE state = 'error';
-- Unanswered clarifications, per contest.
CREATE INDEX clarifications_unanswered ON clarifications (contest_id) WHERE answered_at IS NULL;

-- The run retention (30 days) finds the runs it has not emptied yet
-- without reading the table.
CREATE INDEX jobs_runs_kept ON jobs (created_at) WHERE kind = 'run' AND payload <> '{}';

-- Contract step for P2 (D68): every pin names its revision since 0010,
-- and nothing reads pinned_hash any more; the pinned content hash is the
-- revision's content_hash. A pin without a revision cannot exist after
-- 0010's backfill; refuse to drop the column if one does.
CREATE TEMP TABLE unmapped_pins AS SELECT count(*) AS n FROM contest_problems WHERE revision_id IS NULL;
CREATE TEMP TRIGGER refuse_unmapped_pins BEFORE DELETE ON unmapped_pins
WHEN OLD.n > 0
BEGIN
  SELECT RAISE(ABORT, 'contest_problems has pins without a revision; run podium-server check on v1.2.0 first');
END;
DELETE FROM unmapped_pins;
DROP TABLE temp.unmapped_pins;

ALTER TABLE contest_problems DROP COLUMN pinned_hash;

-- The draft revision retention (90 days) asks, per old revision, whether
-- anything names it.
CREATE INDEX submissions_by_revision ON submissions (revision_id) WHERE revision_id IS NOT NULL;
CREATE INDEX jobs_by_revision ON jobs (revision_id) WHERE revision_id IS NOT NULL;
CREATE INDEX revision_views_by_revision ON revision_views (revision_id);
CREATE INDEX contest_pin_history_by_revision ON contest_pin_history (revision_id);
