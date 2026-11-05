-- v2.0.1 stage C: each contest can show its own contact email on the contest
-- page. Typed by the contest's organisers; empty means none is shown. The
-- column is additive, but the previous binary refuses a database that
-- records this migration: rolling back means restoring the pre-upgrade backup.
ALTER TABLE contests ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
