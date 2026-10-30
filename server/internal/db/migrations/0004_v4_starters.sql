-- v4 E5: starter code per problem, a JSON object of language id to the
-- code a student's editor shows before they type ('{}' when the problem
-- has none, and the web app's own starter is used). It is problem
-- content students read, like the statement: the working copy holds it,
-- each revision snapshots it, and a contest shows the pinned revision's,
-- so an edit reaches students only through "Update students now". It is
-- not judging content, so it is outside the content hash.
ALTER TABLE problems ADD COLUMN starters TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(starters));
ALTER TABLE problem_revisions ADD COLUMN starters TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(starters));
