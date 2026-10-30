-- v4 O3 (V4-15, V4-18): sign-in is decided by the list of people alone.
-- Every row of users is a person on the list, so the allowed email domains
-- go, and with them the hosted-domain (workspace) check; the binding of an
-- account to its first Google account (users.google_sub) stays.
DROP TABLE allowed_domains;

-- A verified Google email that is not on the list, kept so an owner or an
-- admin can let the person in with one click. At most 500 rows, the most
-- recent by last_at (the server deletes the rest), so strangers on the
-- internet cannot fill the disk.
CREATE TABLE refused_signins (
  email    TEXT    PRIMARY KEY COLLATE NOCASE,
  count    INTEGER NOT NULL DEFAULT 1,
  first_at INTEGER NOT NULL,
  last_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX refused_signins_by_last ON refused_signins (last_at);
