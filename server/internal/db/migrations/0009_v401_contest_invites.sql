-- v4.01 stage I: invite-only contests. A contest's audience is everyone who
-- can sign in, or only the students invited to it. Invites are keyed by user
-- id: the sign-in list is the users table, so a listed student has a row
-- before their first sign-in, and leaving the list removes their invites.
ALTER TABLE contests ADD COLUMN audience TEXT NOT NULL DEFAULT 'everyone'
  CHECK (audience IN ('everyone', 'invited'));

CREATE TABLE contest_invites (
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  invited_by INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  invited_at INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

CREATE INDEX contest_invites_by_user ON contest_invites (user_id, contest_id);
