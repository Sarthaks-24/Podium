-- v2 council run 1, C1-049: 0013 marked results_notified_at for every
-- contest whose ends_at was more than a day before the upgrade. With a
-- contest-wide or personal extension a contest's final time
-- (contests.Final: ends_at plus extended_minutes plus the longest personal
-- extension) can be later, so a contest still live at the upgrade was
-- marked as told and its students never got "results are final". Clear the
-- mark 0013 set (it equals the time 0013 ran, within a minute) wherever the
-- final time was after it; the summary sweep then sends the notice once.
-- Expand only: no column changes.
UPDATE contests SET results_notified_at = NULL
WHERE results_notified_at IS NOT NULL
  AND abs(results_notified_at - (SELECT applied_at FROM schema_migrations WHERE version = '0013_students')) <= 60000
  AND ends_at + 60000 * (extended_minutes
    + coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = contests.id), 0)) > results_notified_at;
