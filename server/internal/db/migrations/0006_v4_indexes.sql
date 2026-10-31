-- v4 audit 2026-10-31: indexes for four queries that scanned or sorted a
-- whole table, found with EXPLAIN QUERY PLAN against this schema. Indexes
-- only: no table or row changes.

-- The organisers' submission feed: ORDER BY id DESC within a contest's
-- submits, read a page at a time, without a temporary sort of the contest.
CREATE INDEX submissions_by_feed ON submissions (contest_id, kind, id);

-- A student's started contests (second-session check, Your contests).
CREATE INDEX contest_starts_by_user ON contest_starts (user_id);

-- A contest's announcements, newest first, on every contest page.
CREATE INDEX announcements_by_contest ON announcements (contest_id, id);

-- Contests that pin a problem (practice reveal, the problem bank list).
CREATE INDEX contest_problems_by_problem ON contest_problems (problem_id);
