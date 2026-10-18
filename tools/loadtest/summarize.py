#!/usr/bin/env python3
"""Turn a load test's console log into a queue time series and a summary.

contest.js prints one JSON line per metrics poll (every 5 s). Run k6 with
--console-output FILE, then:

    python3 -I tools/loadtest/summarize.py results/console-fixed-700.log

Writes FILE.csv next to the log (one row per poll: seconds from the
contest start, queued and leased jobs, judged per minute, event streams,
database waits, heap) and prints the peak queue, the judge's throughput
while the queue was long, and how long the queue took to drain after
the end.
"""

import csv
import json
import pathlib
import sys

FIELDS = ["t_s", "phase", "queued", "leased", "submit_queued", "run_queued", "oldest_queued_s",
          "finished_last_minute", "submit_wait_p95_ms", "submit_total_p95_ms", "failed_last_5_min",
          "sse", "db_in_use", "db_waits", "db_wait_ms", "heap_mb", "sys_mb", "goroutines"]


def events(path):
    for line in pathlib.Path(path).read_text(encoding="utf-8", errors="replace").splitlines():
        a, b = line.find("{"), line.rfind("}")
        if a < 0 or b < a:
            continue
        raw = line[a:b + 1]
        if '\\"' in raw:
            raw = raw.replace('\\\\', '\x00').replace('\\"', '"').replace('\x00', '\\')
        try:
            yield json.loads(raw)
        except ValueError:
            continue


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    log = sys.argv[1]
    contest, rows, end_event = None, [], None
    for e in events(log):
        kind = e.get("event")
        if kind == "contest":
            contest = e
        elif kind == "metrics":
            rows.append(e)
        elif kind in ("drained", "not_drained"):
            end_event = e
    if not rows:
        sys.exit(f"summarize: no metrics lines in {log}")
    t0 = contest["starts_at"] if contest else rows[0]["t"]
    out = pathlib.Path(log).with_suffix(".csv")
    with out.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=FIELDS)
        w.writeheader()
        for r in rows:
            r = dict(r, t_s=round((r["t"] - t0) / 1000))
            w.writerow({k: r.get(k, "") for k in FIELDS})

    peak = max(rows, key=lambda r: r["queued"])
    busy = [r["finished_last_minute"] for r in rows if r["queued"] > 10]
    print(f"polls: {len(rows)}, csv: {out}")
    if contest:
        print(f"contest: {contest['slug']} ({contest['mode']}, {contest['vus']} students), "
              f"{(contest['ends_at'] - contest['starts_at']) / 60000:.1f} min")
    print(f"peak queue: {peak['queued']} queued at t={round((peak['t'] - t0) / 1000)} s")
    if busy:
        print(f"judge throughput while more than 10 were queued: {min(busy)} to {max(busy)} jobs a minute, "
              f"median {sorted(busy)[len(busy) // 2]}")
    print(f"max submit wait p95: {max(r['submit_wait_p95_ms'] for r in rows) / 1000:.1f} s, "
          f"max submit total p95: {max(r['submit_total_p95_ms'] for r in rows) / 1000:.1f} s")
    print(f"peak event streams on the server: {max(r['sse'] for r in rows)}, "
          f"database pool waits at the end: {rows[-1]['db_waits']} ({rows[-1]['db_wait_ms']} ms), "
          f"peak heap {max(r['heap_mb'] for r in rows)} MB, peak sys {max(r['sys_mb'] for r in rows)} MB")
    if end_event and end_event["event"] == "drained":
        print(f"drained: {end_event['drain_s']:.0f} s after the end")
    elif end_event:
        print("not drained within DRAIN_MAX_MINUTES")


if __name__ == "__main__":
    main()
