-- 0002_preferences: each person's display preferences, so a theme chosen
-- for low vision follows the account across shared lab machines (decision
-- D42). The browser keeps a copy only to paint the first frame.

-- theme is a theme id from the web app's list, or '' for "follow the
-- system". Only the shape is checked here; the web app ignores ids it does
-- not know, so the list lives in one place.
ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT ''
  CHECK (theme = '' OR (length(theme) <= 32 AND theme NOT GLOB '*[^a-z0-9-]*'));

-- verdicts selects the verdict colours: the standard set, or the
-- colour-blind safe set (decision D40).
ALTER TABLE users ADD COLUMN verdicts TEXT NOT NULL DEFAULT 'standard'
  CHECK (verdicts IN ('standard', 'signal'));
