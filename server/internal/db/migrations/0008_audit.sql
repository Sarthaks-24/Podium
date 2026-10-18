-- 0008_audit: the contest audit log, the server's own settings, and a mark
-- for AI problems a person has written a solution for (security council
-- items S2, S9 and S10, 2026-10-18). Times are Unix milliseconds, UTC.

-- Every organiser action that can change a contest's results: settings,
-- problem pins, the roster, extensions, extend, pause and resume, rejudges,
-- the reveal, the editorial, co-organisers, and an admin's confirmation
-- that an AI problem may be graded. details is a JSON object (for a
-- settings change, each changed field as {"from": ..., "to": ...}).
-- Append only, enforced by the triggers below, as integrity_decisions is:
-- nothing can update or delete a row, including a cascade, so a contest
-- with audit rows cannot be deleted (nothing in the code deletes one; the
-- clone rollback removes only a contest created a moment earlier, before
-- anything is recorded for it). Users are never deleted either.
CREATE TABLE contest_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE RESTRICT,
  actor_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  -- the actor's email and site role when they acted, so the record reads
  -- the same after a later role change
  actor_email TEXT   NOT NULL,
  actor_role  TEXT   NOT NULL,
  action     TEXT    NOT NULL,
  details    TEXT    NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX contest_audit_by_contest ON contest_audit (contest_id, id);

CREATE TRIGGER contest_audit_no_update BEFORE UPDATE ON contest_audit
BEGIN
  SELECT RAISE(ABORT, 'contest_audit is append only');
END;

CREATE TRIGGER contest_audit_no_delete BEFORE DELETE ON contest_audit
BEGIN
  SELECT RAISE(ABORT, 'contest_audit is append only');
END;

-- Small facts the server keeps about itself. 'mode' is the PODIUM_MODE it
-- last ran in: a change ends every session, and a change to dev or
-- production also demotes organisers no Google account vouches for.
CREATE TABLE settings (
  key        TEXT    PRIMARY KEY,
  value      TEXT    NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

-- Set when a person's editor save changes the reference solution or the
-- brute force of a problem. A problem whose origin is ai needs it (or an
-- admin's recorded reason) before it can be added to a graded contest.
ALTER TABLE problems ADD COLUMN solution_by_person INTEGER NOT NULL DEFAULT 0 CHECK (solution_by_person IN (0, 1));
