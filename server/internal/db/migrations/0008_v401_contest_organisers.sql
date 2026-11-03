CREATE TABLE contest_organisers (
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  added_by   INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  added_at   INTEGER NOT NULL,
  PRIMARY KEY (contest_id, user_id)
) STRICT;

CREATE INDEX contest_organisers_by_user ON contest_organisers (user_id, contest_id);
