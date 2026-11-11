-- v2.1.0 stage B: keep me signed in. A remembered session (remember = 1) has
-- a cookie with a Max-Age and an expiry that moves forward on use, at most
-- once an hour (last_seen_at is the last move); an ordinary session
-- (remember = 0) still ends 12 hours after sign-in. Every row that exists at
-- the upgrade is ordinary, so nobody's session changes. The previous binary
-- refuses a database that records this migration: rolling back means
-- restoring the pre-upgrade backup.
ALTER TABLE sessions ADD COLUMN remember INTEGER NOT NULL DEFAULT 0 CHECK (remember IN (0, 1));
ALTER TABLE sessions ADD COLUMN last_seen_at INTEGER;
