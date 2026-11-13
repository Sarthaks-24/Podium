# Load test

A k6 script that plays a whole contest against a podium server in testing mode: one virtual user per student, plus one that watches the server's metrics until the judge queue has drained. Each run against the VM is written up in `cache/v2-plan/drills/<date>-load.md` (not shipped); its numbers go in a dated section of the calibration notes.

| File | What it does |
|---|---|
| `contest.js` | The k6 script. A submission spike at the end of each student's time, per-route latency, 429 counts, event stream reconnects, queue depth every 5 s. |
| `seed.py` | Imports and validates the example packs in `problems/` and creates the student accounts. Python 3 standard library only. |
| `summarize.py` | Turns the run's console log into a CSV time series of the queue and prints the peak queue, the judge's throughput and the drain time. |

## What one student does

| Action | How often (defaults) |
|---|---|
| Sign in from the testing form, open the contest list and page, accept the rules | once, at a random time in the first `RAMP_MINUTES` |
| Hold `GET /api/events` open | the whole session; a stream that ends early is counted as a reconnect and reopened with the web app's back-off |
| Open a problem's workspace | every 4 minutes on average, moving from A to C as time passes |
| Save the draft | every 7.5 to 22.5 s (`DRAFT_EVERY_S`, mean 15) |
| Fetch the standings | every 10 s on average (`STANDINGS_EVERY_S`): the standings page refetches on every board change, at most every 2 s, so this stands for a third of the class watching it |
| Send an integrity event batch | every 30 s |
| Reload the contest page | every 2 minutes on average |
| Run on the samples | `RUN_PER_MIN` across the class (default 1.5 times the submit rate) |
| Submit | `SUBMIT_PER_MIN` across the class, rising to `SPIKE_PER_MIN` in the last `SPIKE_MINUTES` of the student's own time; after a 429 the student waits `Retry-After` and presses Submit again; half the submissions send exactly the code last run (`SAME_AS_RUN`), which the judge's compile cache serves |

Submitted programs are the packs' own solutions and plausible wrong ones: correct C++, Java and Python, an `int` overflow that passes the small subtask, wrong answers on the first sample, compile errors, and a brute force too slow for the large subtask. Each carries a unique comment, so nothing is shared between students in the compile cache. The defaults scale PLAN.md section 4 (700 students: about 30 submissions a minute on average, 100 to 150 a minute at the end) to the number of students.

Every request is tagged: `kind:api` for the student API (the thresholds and the summary), `kind:sse` for event streams, `kind:monitor` for the metrics poll, `kind:setup` for creating the contest. A run fails its thresholds when the API's p95 is 300 ms or more, or more than 1 % of API requests fail; 429 (a limit) is not a failure, and 429s are counted on their own.

## Settings

Pass with `-e NAME=value`.

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8080` | the server |
| `ORIGIN` | `http://localhost:5173` | the server's `PODIUM_BASE_URL`, sent as `Origin`: testing mode accepts the sign-in form only from it (`seed.py --origin` is the same) |
| `VUS` | 100 | students |
| `MINUTES` | 15 | contest length |
| `RAMP_MINUTES` | 2 | fixed: students arrive over this time |
| `SUBMIT_PER_MIN` | 30 × VUS / 700 | submissions a minute across the class |
| `SPIKE_PER_MIN` | 120 × VUS / 700 | the same in each student's last `SPIKE_MINUTES` |
| `SPIKE_MINUTES` | 3 | length of the spike |
| `RUN_PER_MIN` | 1.5 × `SUBMIT_PER_MIN` | runs a minute across the class |
| `SAME_AS_RUN` | 0.5 | share of submissions that repeat the last run's code |
| `STANDINGS_EVERY_S` | 10 | mean seconds between standings fetches per student |
| `DRAFT_EVERY_S` | 15 | mean seconds between draft saves per student |
| `MIN_GAP_S` | 30 | the contest's gap between submits |
| `DRAIN_MAX_MINUTES` | 30 | how long the monitor waits for the queue to drain after the end |
| `PROBLEMS` | `a-plus-b,average-speed,pair-with-sum,array-sum,largest-gap` | pack slugs, in label order |
| `INVITE_ONLY` | 1 | 1 makes the contest invite-only (v4.01): setup fetches the picker's candidates once (`name:candidates`, with its size in KB in the log) and invites the `VUS` seeded students in batches of 500 (`name:invite_add`), and every student request pays the invite check. 0 runs an open contest. `seed.py --users` must be at least `VUS` |
| `CODE_VIEWS_PER_MIN` | 3 | one staff user opens a recent submission's code this often (`name:feed`, then `name:code_view`), so `code_viewed` audit rows are written through the write gate during the submit spike; 0 turns it off |
| `RESULTS` | `results` | folder for the JSON summary |

## Run it in WSL2 or on Linux

Prerequisites: the judge setup (`deploy/install.sh packages isolate testlib users`) and k6. Install k6 from a pinned release, checked against its published checksums:

```sh
v=v2.3.0; cd "$(mktemp -d)"
curl -fsSLO https://github.com/grafana/k6/releases/download/$v/k6-$v-linux-amd64.tar.gz
curl -fsSLO https://github.com/grafana/k6/releases/download/$v/k6-$v-checksums.txt
grep " k6-$v-linux-amd64.tar.gz\$" k6-$v-checksums.txt | sha256sum -c
tar xzf k6-$v-linux-amd64.tar.gz && sudo install -m 755 k6-$v-linux-amd64/k6 /usr/local/bin/k6
```

Then, from the repository root (`/mnt/e/...` in WSL2 is fine for the scripts; the stack keeps its state in `/var/tmp`):

```sh
# 1. Linux builds of both binaries (on Windows: make dev-test builds them too)
cd server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/linux/ ./cmd/... && cd ..

# 2. A fresh testing-mode stack on spare ports, pinned to two CPUs like the VM
sudo PODIUM_DEV_STATE=/var/tmp/podium-load PODIUM_LISTEN=127.0.0.1:18080 \
  PODIUM_JUDGE_LISTEN=127.0.0.1:18081 PODIUM_DEV_CPUS=0-1 \
  bash tools/dev-stack.sh server/bin/linux > /var/tmp/podium-load.log 2>&1 &

# 3. Problems and students
python3 -I tools/loadtest/seed.py --base http://127.0.0.1:18080 --users 700

# 4. The run (k6 on the other CPUs), then the summary
cd tools/loadtest && mkdir -p results
ulimit -n 65536
taskset -c 2-15 k6 run -e BASE_URL=http://127.0.0.1:18080 -e VUS=700 -e MINUTES=25 -e SPIKE_MINUTES=10 \
  --console-output results/console-fixed-700.log contest.js
python3 -I summarize.py results/console-fixed-700.log
```

Run the seed once per stack; every k6 run creates its own contest. Stop the stack with `sudo pkill -f /var/tmp/podium-load/podium-`.

## Run it against the VM

The acceptance target is the 2 GB VM itself (PLAN.md M7: API p95 under 300 ms at 700 students). Testing mode only listens on loopback and lets anyone sign in as anyone, so never run it on a VM that holds real data or is open to students:

1. Use a fresh VM, or one before its first real contest, and stop everything public first: `sudo systemctl stop caddy podium-server podium-judge`. Caddy must stay stopped for the whole run: it proxies to `127.0.0.1:8080`, and a testing-mode server behind it would let anyone on the internet sign in as an owner.
2. The release does not ship `tools/dev-stack.sh`; copy it over first (`scp tools/dev-stack.sh you@contest.example.edu:`). Start the testing stack from the release's binaries on the spare ports, which Caddy never proxies: `sudo PODIUM_DEV_STATE=/var/tmp/podium-load PODIUM_LISTEN=127.0.0.1:18080 PODIUM_JUDGE_LISTEN=127.0.0.1:18081 bash dev-stack.sh /usr/local/bin` (all its state stays in `/var/tmp`; the real database is not touched).
3. From another machine with k6 (k6 at 700 students needs 1 to 2 GB of memory itself), forward the port: `ssh -N -L 18080:127.0.0.1:18080 you@contest.example.edu`, then run the seed and k6 against `http://127.0.0.1:18080` as above.
4. Afterwards stop the stack (`sudo pkill -f /var/tmp/podium-load/podium-`), delete `/var/tmp/podium-load`, and start `podium-server`, `podium-judge` and `caddy` again. Then run the "not in testing mode" check in `docs/RUNBOOK.md`, "A week before".

## Reading the results

- The k6 summary prints the API's p50, p95 and p99, failures, 429s, event stream reconnects and the per-route p95.
- `summarize.py` writes `results/console-*.csv` (one row per 5 s: queued and leased jobs, judged per minute, p95 wait for a submission, event streams, database pool waits, heap) and prints the peak queue, the judge's throughput while the queue was long, and how many seconds after the end the queue drained.
- Record the peak, the drain time and the throughput the run measured in a new dated section of the calibration notes; the docs quote only those numbers. A fixed-start spike of S submissions a minute for T minutes against a judge doing J a minute builds a queue of about (S - J) × T and drains in about that divided by J.
- The metrics route the monitor reads, `GET /api/admin/metrics`, is described in `docs/OPERATIONS.md`, "Metrics".

## The replay corpus and the comparison (v2.1.0)

The k6 script draws programs at random, so two runs judge different work. For comparing one setting against another (the test VM session of v2.1.0) there is a fixed corpus instead.

| Part | What it is |
|---|---|
| `corpus/corpus.json` | 17 programs from the packs in `problems/` and the files in `corpus/`. One round is 30 Runs and 21 submissions (59 percent Runs; the measured mix was 302 of 520 queued jobs). Each program has the expected verdict of a Run (examples only) and of a submission beside it |
| `server/tools/loadreplay` | Sends rounds of the corpus in a fixed order and at fixed moments (steady, then 3 times as dense in the last `-burst` minutes), waits for every verdict, writes `verdicts.jsonl`, prints the verdicts per program and exits 1 if any differs from the expected one. No k6 |
| `server/tools/loadcompare` | Reads the runs of two arms and prints pass or fail for J0, J1, J5 or J6 against thresholds written in `compare.go` |

What the corpus holds: C++ that includes `<bits/stdc++.h>` (three programs, "heavy"), C++ without it, wrong answers (an `int` overflow that passes the examples, a constant), a compile error, a run error, brute-force time-outs (C++ on `largest-gap`, Python on `pair-with-sum`), Node and Java, and four programs written to use a set share of their limit (`apb-cpp-near55`, `apb-cpp-near80`, `as-java-near55`, `as-java-near80`: a spin of CPU time before the real answer). The near-limit shares depend on the machine; confirm them on the test VM before relying on the 50 to 90 percent target.

```sh
# Linux builds (the stack of the section above, plus the two tools)
cd server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/linux/ ./tools/loadreplay ./tools/loadcompare && cd ..

# Seed once per stack (the packs, validated on the judge), then replay.
python3 -I tools/loadtest/seed.py --base http://127.0.0.1:18080 --users 1
server/bin/linux/loadreplay -base http://127.0.0.1:18080 -rounds 2 -minutes 3 -burst 1 -tail 20s -out run-a1
```

`-rounds` sets how much work there is, `-minutes` and `-burst` how it is spread; the same flags give the same list at the same moments. The contest closes `-tail` before the end, so the last requests are practice: submissions that are not graded and Runs of students whose time is over (J0 puts those behind graded work). `-list` prints the list without a server; `-only id,id` sends a few programs for checking one.

For the comparison, a run is a directory with three files: `verdicts.jsonl` (the replay), `judge.log` (the judge's JSON log lines, from `journalctl -u podium-judge -o cat`, or the stack's log file; other lines are ignored) and optionally `sampler.log` (JSON lines with a cumulative `slice_oom_kills`). The timing line of a job joins to its request by `kind` and `subject_id`. Run the A/A pair first, then three runs of each arm:

```sh
server/bin/linux/loadcompare -change j5 -aa aa1,aa2 -base off1,off2,off3 -test on1,on2,on3
```

It prints PASS, FAIL or SKIP per check and `RESULT J5: ships` or `does not ship`; the exit status is 1 when it does not ship. A verdict that moved, or a program that is missing, fails every change. The thresholds are in `server/tools/loadcompare/compare.go`: change one only with a dated record in the decision log.
