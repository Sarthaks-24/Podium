-- v2.0.2 stage D: the AI prompt versions. One row per save of the rules text
-- of the wizard prompt (wizard_rules), of the outside-AI pack prompt
-- (pack_rules) or of the admin's Preferences box (preferences). The newest
-- row of a kind is its active text; a reset row (is_reset = 1) means "back
-- to the built-in default" and never brings an older edit back. Restoring
-- an old version inserts a new row with that body. default_sha is the
-- sha256 of the built-in text when the row was saved, so the editor can say
-- the default changed since. Rows are insert only, like problem_revisions.
-- preferences is a kind from the start because a CHECK cannot be changed
-- later without rebuilding the table. The previous binary refuses a
-- database that records this migration: rolling back means restoring the
-- pre-upgrade backup.
CREATE TABLE ai_prompt_versions (
  id          INTEGER PRIMARY KEY,
  kind        TEXT    NOT NULL CHECK (kind IN ('wizard_rules', 'pack_rules', 'preferences')),
  body        TEXT    NOT NULL,
  is_reset    INTEGER NOT NULL DEFAULT 0 CHECK (is_reset IN (0, 1)),
  default_sha TEXT    NOT NULL DEFAULT '',
  created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX ai_prompt_versions_kind ON ai_prompt_versions (kind, id);

CREATE TRIGGER ai_prompt_versions_no_delete BEFORE DELETE ON ai_prompt_versions
BEGIN
  SELECT RAISE(ABORT, 'ai_prompt_versions is append only');
END;

CREATE TRIGGER ai_prompt_versions_no_update BEFORE UPDATE ON ai_prompt_versions
BEGIN
  SELECT RAISE(ABORT, 'ai_prompt_versions is append only');
END;
