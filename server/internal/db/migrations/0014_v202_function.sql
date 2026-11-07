-- v2.0.2 stage B: function problems. mode is 'whole' (a student writes the
-- whole program, as before) or 'function' (the student writes only the
-- middle of the file). locked_top and locked_bottom are JSON objects of
-- language id to the code the judge puts before and after the student's
-- part ('{}' for a whole-program problem), so a new language needs no
-- migration. They decide verdicts, so they are in the content hash and
-- each revision records them. top_lines is the locked top's line count when
-- a submission was judged: the server maps compile output back to the
-- student's own line numbers with it, also for a practice submission made
-- after a later edit of the top. All columns are additive with defaults
-- that read every existing row as a whole-program problem, but the
-- previous binary refuses a database that records this migration: rolling
-- back means restoring the pre-upgrade backup.
ALTER TABLE problems ADD COLUMN mode TEXT NOT NULL DEFAULT 'whole' CHECK (mode IN ('whole', 'function'));
ALTER TABLE problems ADD COLUMN locked_top TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(locked_top));
ALTER TABLE problems ADD COLUMN locked_bottom TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(locked_bottom));
ALTER TABLE problem_revisions ADD COLUMN mode TEXT NOT NULL DEFAULT 'whole' CHECK (mode IN ('whole', 'function'));
ALTER TABLE problem_revisions ADD COLUMN locked_top TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(locked_top));
ALTER TABLE problem_revisions ADD COLUMN locked_bottom TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(locked_bottom));
ALTER TABLE submissions ADD COLUMN top_lines INTEGER NOT NULL DEFAULT 0 CHECK (top_lines >= 0);
