-- v3: one integrity decision per student per contest, with a reason
-- (owner answer B, 2026-10-26). The case workflow, proposals, appeals,
-- notes, notices, rating, record views and summaries go.
DROP TABLE integrity_appeal_rulings;
DROP TABLE integrity_appeals;
DROP TABLE integrity_proposals;
DROP TABLE outcome_adjustments;
DROP TABLE integrity_decisions;
DROP TABLE integrity_explanations;
DROP TABLE integrity_case_users;
DROP TABLE integrity_cases;
DROP TABLE submission_notes;
DROP TABLE notifications;
DROP TABLE record_views;
DROP TABLE student_contest_summary;
DROP TABLE rating_proposal_closures;
DROP TABLE rating_proposals;
DROP TABLE rating_rollovers;
DROP TABLE rating_history;
DROP TABLE student_rating;

CREATE TABLE integrity_cases (
  id         INTEGER PRIMARY KEY,
  contest_id INTEGER NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- excluded hides the student from the contest's board
  decision   TEXT    NOT NULL CHECK (decision IN ('no_action', 'warning', 'excluded')),
  reason     TEXT    NOT NULL,
  decided_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  decided_at INTEGER NOT NULL,
  UNIQUE (contest_id, user_id)
) STRICT;
