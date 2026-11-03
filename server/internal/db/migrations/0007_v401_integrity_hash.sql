-- Contest-scoped shared-browser review, excluding empty hashes.
CREATE INDEX submissions_by_contest_client_hash
  ON submissions (contest_id, client_hash)
  WHERE client_hash <> '';
