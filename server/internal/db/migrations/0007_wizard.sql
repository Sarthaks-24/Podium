-- 0007_wizard: tests from a generator and the AI wizard's drafts
-- (cache/v1/m5-plan.md sections 3 and 4). Times are Unix milliseconds, UTC.
-- The AI provider key is never stored: nothing here holds it.

-- A problem may carry a generator program (setter code, run only inside
-- isolate) and a plan of the tests it makes: a JSON array of
-- {"test", "subtask", "seed", "args"}. The generator is not part of the
-- content hash; the tests it produced are.
ALTER TABLE problems ADD COLUMN generator_language TEXT NOT NULL DEFAULT '';
ALTER TABLE problems ADD COLUMN generator_source   TEXT NOT NULL DEFAULT '';
ALTER TABLE problems ADD COLUMN generator_plan     TEXT NOT NULL DEFAULT '[]';

-- The arguments the generator gets for this subtask's tests (a JSON array
-- of strings), used when planning tests.
ALTER TABLE subtasks ADD COLUMN generator_args TEXT NOT NULL DEFAULT '[]';

-- jobs.kind gains 'generate_inputs'. SQLite cannot alter a CHECK, and
-- rebuilding jobs with foreign keys on would cascade into job_results, so
-- the constraint is widened in place: every existing row still satisfies
-- it, and nothing on disk changes (the procedure in SQLite's ALTER TABLE
-- documentation for changing constraints). RESET reloads this
-- connection's schema; the CREATE TABLE below changes the schema cookie,
-- so every other connection reloads it too.
PRAGMA writable_schema = ON;
UPDATE sqlite_master
  SET sql = replace(sql, '''validate'', ''submit'', ''run'', ''generate'')', '''validate'', ''submit'', ''run'', ''generate'', ''generate_inputs'')')
  WHERE type = 'table' AND name = 'jobs';
PRAGMA writable_schema = RESET;

-- One "Generate inputs" run: the generator once per planned test. On
-- success it queues output generation, which queues validation.
CREATE TABLE input_generations (
  id           INTEGER PRIMARY KEY,
  problem_id   INTEGER NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
  job_id       INTEGER NOT NULL REFERENCES jobs (id),
  requested_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX input_generations_by_problem ON input_generations (problem_id, id);
