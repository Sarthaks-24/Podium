-- v4 U13: idempotent creates. Submit's client_key pattern extended to
-- creating a contest, a problem, an announcement and a question: the web
-- app sends one key per form, so a double click or a retried request makes
-- one row, and the repeat gets the first one back. '' means no key (older
-- clients, the AI wizard, duplicates, imports) and is never unique. The
-- key belongs to the request, not to the content: it is in no view, no
-- revision, no content hash and no pack.
ALTER TABLE contests ADD COLUMN client_key TEXT NOT NULL DEFAULT '';
ALTER TABLE problems ADD COLUMN client_key TEXT NOT NULL DEFAULT '';
ALTER TABLE announcements ADD COLUMN client_key TEXT NOT NULL DEFAULT '';
ALTER TABLE clarifications ADD COLUMN client_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX contests_by_key ON contests (coalesce(created_by, 0), client_key) WHERE client_key <> '';
CREATE UNIQUE INDEX problems_by_key ON problems (coalesce(author_id, 0), client_key) WHERE client_key <> '';
CREATE UNIQUE INDEX announcements_by_key ON announcements (contest_id, coalesce(created_by, 0), client_key) WHERE client_key <> '';
CREATE UNIQUE INDEX clarifications_by_key ON clarifications (contest_id, asked_by, client_key) WHERE client_key <> '';
