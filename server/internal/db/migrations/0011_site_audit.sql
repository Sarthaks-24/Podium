-- 0011_site_audit: the site audit and the slug history (v2 plan B2.7 and
-- B2.9, decisions D62, D65 and D69). Times are Unix milliseconds, UTC.

-- Every change to people, domains and the site: roles, domains, disabled
-- accounts, ended sessions, Google link resets, run mode changes, forced
-- deploys (and in P3 site settings, contact messages, erasure). The actor's
-- email and role are kept as they were. actor_id is NULL for the server's
-- own changes. Never holds an AI key, a session token, a cookie or an
-- OAuth code (a test searches the table for fake ones).
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

CREATE INDEX site_audit_by_action ON site_audit (action, id);
CREATE INDEX site_audit_by_at ON site_audit (at);

CREATE TRIGGER site_audit_no_update BEFORE UPDATE ON site_audit
BEGIN
  SELECT RAISE(ABORT, 'site_audit is append only');
END;

CREATE TRIGGER site_audit_no_delete BEFORE DELETE ON site_audit
WHEN OLD.at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'site_audit'), 0)
BEGIN
  SELECT RAISE(ABORT, 'site_audit is append only');
END;

-- The contest audit log of 0008 gets the same retention exception (D69):
-- a delete of a row older than its cutoff is allowed, nothing else.
DROP TRIGGER contest_audit_no_delete;
CREATE TRIGGER contest_audit_no_delete BEFORE DELETE ON contest_audit
WHEN OLD.created_at >= coalesce((SELECT before_ms FROM retention_cutoffs WHERE tbl = 'contest_audit'), 0)
BEGIN
  SELECT RAISE(ABORT, 'contest_audit is append only');
END;

-- Old slugs of renamed contests: /contests/<old> answers 301 to the
-- contest's current slug, and no other contest may take an old slug.
-- Append only, with no retention: a slug is not personal data.
CREATE TABLE slug_history (
  entity     TEXT    NOT NULL CHECK (entity IN ('contest')),
  old_slug   TEXT    NOT NULL,
  entity_id  INTEGER NOT NULL,
  changed_at INTEGER NOT NULL,
  PRIMARY KEY (entity, old_slug)
) STRICT;

CREATE TRIGGER slug_history_no_update BEFORE UPDATE ON slug_history
BEGIN
  SELECT RAISE(ABORT, 'slug_history is append only');
END;

CREATE TRIGGER slug_history_no_delete BEFORE DELETE ON slug_history
BEGIN
  SELECT RAISE(ABORT, 'slug_history is append only');
END;
