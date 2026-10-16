-- 0001_identity: people, allowed email domains and sessions (PLAN.md
-- sections 8 and 10). Later milestones add their tables in new files.
-- All times are Unix milliseconds, UTC.

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  name          TEXT    NOT NULL DEFAULT '',
  avatar_url    TEXT    NOT NULL DEFAULT '',
  role          TEXT    NOT NULL DEFAULT 'student' CHECK (role IN ('owner', 'admin', 'student')),
  disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
  created_at    INTEGER NOT NULL,
  last_login_at INTEGER
) STRICT;

CREATE TABLE allowed_domains (
  domain     TEXT    PRIMARY KEY COLLATE NOCASE,
  added_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
) STRICT;

-- id is the SHA-256 (hex) of the session cookie value. The cookie value
-- itself is never stored, so a copy of the database cannot be replayed as
-- a login.
CREATE TABLE sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  ip         TEXT    NOT NULL DEFAULT '',
  user_agent TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX sessions_by_user ON sessions (user_id);
CREATE INDEX sessions_by_expiry ON sessions (expires_at);
