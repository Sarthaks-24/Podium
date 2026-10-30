-- podium: foreign_keys off
-- v4 O1 (V4-16, V4-23): the stored role "admin" becomes "organiser", and
-- the CHECK makes room for a new "admin" between organiser and owner. In
-- the other order every organiser would silently become an admin.
-- SQLite cannot change a CHECK in place, so users is rebuilt by its
-- documented 12-step procedure: the runner applies this file with
-- foreign_keys off (the first line asks for it) and refuses to commit
-- unless PRAGMA foreign_key_check finds nothing. The 23 tables that
-- reference users name it by name, so they point at the new table once
-- it is renamed.
CREATE TABLE users_new (
  id            INTEGER PRIMARY KEY,
  email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  name          TEXT    NOT NULL DEFAULT '',
  avatar_url    TEXT    NOT NULL DEFAULT '',
  role          TEXT    NOT NULL DEFAULT 'student' CHECK (role IN ('owner', 'admin', 'organiser', 'student')),
  disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
  -- Google's stable account id ("sub"), bound at first sign-in. A later
  -- sign-in with the same email but a different sub is refused: email
  -- addresses can be reassigned, sub values are not.
  google_sub    TEXT UNIQUE,
  created_at    INTEGER NOT NULL,
  last_login_at INTEGER,
  theme         TEXT    NOT NULL DEFAULT ''
    CHECK (theme = '' OR (length(theme) <= 32 AND theme NOT GLOB '*[^a-z0-9-]*')),
  verdicts      TEXT    NOT NULL DEFAULT 'standard' CHECK (verdicts IN ('standard', 'signal'))
) STRICT;

INSERT INTO users_new (id, email, name, avatar_url, role, disabled, google_sub, created_at, last_login_at, theme, verdicts)
  SELECT id, email, name, avatar_url, CASE role WHEN 'admin' THEN 'organiser' ELSE role END,
         disabled, google_sub, created_at, last_login_at, theme, verdicts
  FROM users;

DROP TABLE users;

ALTER TABLE users_new RENAME TO users;
