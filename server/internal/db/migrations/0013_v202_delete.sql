-- v2.0.2 stage A: deleting a problem keeps its row as a tombstone. deleted
-- marks it, deleted_at and deleted_by say when and by whom (the person is set
-- to NULL if their account is ever erased). The working copy's tests and
-- solutions are cleared by the delete itself; revisions, their tests,
-- submissions and contest rows are never touched, so ended contests still
-- read. The original slug is in archived_slug, as for an archived problem.
-- The columns are additive, but the previous binary refuses a database that
-- records this migration: rolling back means restoring the pre-upgrade backup.
ALTER TABLE problems ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1));
ALTER TABLE problems ADD COLUMN deleted_at INTEGER;
ALTER TABLE problems ADD COLUMN deleted_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
