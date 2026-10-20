-- The judge host's toolchain (B1.4): compilers, runtimes, isolate and the
-- kernel, as the judge reports them at start. Each distinct report is
-- stored once, keyed by the hash of its details; last_seen is the latest
-- report, so the row with the highest last_seen is what judges now.
CREATE TABLE toolchains (
  id         INTEGER PRIMARY KEY,
  hash       TEXT    NOT NULL UNIQUE,
  -- judgeapi.Toolchain as JSON: gcc, java, python, node, isolate, kernel.
  details    TEXT    NOT NULL,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL
) STRICT;

-- The toolchain of the worker that ran the job, set on each claim. A
-- validation's toolchain is its job's, which is what the contest console
-- compares with the judge's current one (the drift flag). NULL for jobs
-- before this migration and for judges that sent no report.
ALTER TABLE jobs ADD COLUMN toolchain_id INTEGER REFERENCES toolchains (id);
