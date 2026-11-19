-- v2.1.1 stage P: the contest setting "Locked code" for function problems.
-- shown is today's behaviour and the default, so no existing contest changes;
-- folded opens the editor with the locked blocks folded; hidden sends
-- students no locked code. The previous binary refuses a database that
-- records this migration: rolling back means restoring the pre-upgrade backup.
ALTER TABLE contests ADD COLUMN locked_code TEXT NOT NULL DEFAULT 'shown' CHECK (locked_code IN ('shown', 'folded', 'hidden'));
