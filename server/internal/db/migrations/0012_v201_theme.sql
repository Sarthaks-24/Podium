-- v2.0.1 stage E: two more display preferences on the account. editor_colours
-- is one of five scheme ids (the server checks the list). theme_custom holds
-- the three inputs of a custom interface theme as JSON, or is empty; the
-- server checks the shape and the browser derives every colour from it. Both
-- columns are additive, but the previous binary refuses a database that
-- records this migration: rolling back means restoring the pre-upgrade backup.
ALTER TABLE users ADD COLUMN editor_colours TEXT NOT NULL DEFAULT 'night';
ALTER TABLE users ADD COLUMN theme_custom TEXT NOT NULL DEFAULT '';
