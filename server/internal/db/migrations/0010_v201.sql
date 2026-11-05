-- v2.0.1 stage A: archiving frees the slug. An archived problem or contest
-- keeps its original slug in archived_slug and takes
-- <base>-archived-<id> as its live slug, so the original can be used again.
-- archived_slug is empty while the slug has never moved. The columns are
-- additive, but the previous binary refuses a database that records this
-- migration: rolling back means restoring the pre-upgrade backup.
ALTER TABLE problems ADD COLUMN archived_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE contests ADD COLUMN archived_slug TEXT NOT NULL DEFAULT '';

-- Rows archived before this release. The form is cut so the whole slug is at
-- most 64 characters and ends without a hyphen. A row whose new slug is
-- already taken is left as it is (archived_slug stays empty) rather than
-- failing start-up.
UPDATE problems SET
  archived_slug = slug,
  slug = rtrim(substr(slug, 1, 64 - length('-archived-' || id)), '-') || '-archived-' || id
WHERE archived = 1 AND archived_slug = ''
  AND NOT EXISTS (
    SELECT 1 FROM problems q
    WHERE q.slug = rtrim(substr(problems.slug, 1, 64 - length('-archived-' || problems.id)), '-') || '-archived-' || problems.id);

UPDATE contests SET
  archived_slug = slug,
  slug = rtrim(substr(slug, 1, 64 - length('-archived-' || id)), '-') || '-archived-' || id
WHERE archived = 1 AND archived_slug = ''
  AND NOT EXISTS (
    SELECT 1 FROM contests q
    WHERE q.slug = rtrim(substr(contests.slug, 1, 64 - length('-archived-' || contests.id)), '-') || '-archived-' || contests.id);
