# Load test

A k6 script that plays a whole contest against a podium server in testing mode: one virtual user per student, plus one that watches the server's metrics until the judge queue has drained. Each run against the VM is written up in `cache/v2-plan/drills/<date>-load.md` (not shipped); its numbers go in a dated section of the calibration notes.

| File | What it does |
|---|---|
| `contest.js` | The k6 script. Fixed and window start modes, a submission spike at the end of each student's time, per-route latency, 429 counts, event stream reconnects, queue depth every 5 s. |
| `seed.py` | Imports and validates the example packs in `problems/` and creates the student accounts. Python 3 standard library only. |
| `summarize.py` | Turns the run's console log into a CSV time series of the queue and prints the peak queue, the judge's throughput and the drain time. |

## What one student does

| Action | How often (defaults) |
|---|---|
| Sign in from the testing form, open the contest list and page, accept the rules | once, at a random time in the first `RAMP_MINUTES` (fixed) or of the window (window mode) |
| Hold `GET /api/events` open | the whole session; a stream that ends early is counted as a reconnect and reopened with the web app's back-off |
| Open a problem's workspace | every 4 minutes on average, moving from A to C as time passes |
| Save the draft | every 7.5 to 22.5 s (`DRAFT_EVERY_S`, mean 15) |
| Fetch the standings | every 10 s on average (`STANDINGS_EVERY_S`): the standings page refetches on every board change, at most every 2 s, so this stands for a third of the class watching it |
| Send an integrity event batch | every 30 s |
| Reload the contest page | every 2 minutes on average |
| Run on the samples | `RUN_PER_MIN` across the class (default 1.5 times the submit rate) |
| Submit | `SUBMIT_PER_MIN` across the class, rising to `SPIKE_PER_MIN` in the last `SPIKE_MINUTES` of the student's own time; after a 429 the student waits `Retry-After` and presses Submit again; half the submissions send exactly the code last run (`SAME_AS_RUN`), which the judge's compile cache serves |

Submitted programs are the packs' own solutions and plausible wrong ones: correct C++, Java and Python, an `int` overflow that passes the small subtask, wrong answers on the first sample, compile errors, and a brute force too slow for the large subtask. Each carries a unique comment, so nothing is shared between students in the compile cache. The defaults scale PLAN.md section 4 (700 students: about 30 submissions a minute on average, 100 to 150 a minute at the end) to the number of students.

Every request is tagged: `kind:api` for the student API (the thresholds and the summary), `kind:sse` for event streams, `kind:monitor` for the metrics poll, `kind:setup` for creating the contest. A run fails its thresholds when the API's p95 is 300 ms or more, or more than 1 % of API requests fail; 403 (the window-mode board answering "closed") and 429 (a limit) are not failures, and 429s are counted on their own.

## Settings

Pass with `-e NAME=value`.

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8080` | the server |
| `ORIGIN` | `http://localhost:5173` | the server's `PODIUM_BASE_URL`, sent as `Origin`: testing mode accepts the sign-in form only from it (`seed.py --origin` is the same) |
| `VUS` | 100 | students |
| `MODE` | `fixed` | `fixed` or `window` |
| `MINUTES` | 15 | contest length (fixed), or the whole window (window mode) |
| `WINDOW_MINUTES` | 10 | window mode: each student's time, at least 10 |
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
| `PROBLEMS` | `a-plus-b,average-speed,pair-with-sum` | pack slugs, in label order |
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

Window mode: `-e MODE=window -e MINUTES=30 -e WINDOW_MINUTES=15`. Run the seed once per stack; every k6 run creates its own contest. Stop the stack with `sudo pkill -f /var/tmp/podium-load/podium-`.

## Run it against the VM

The acceptance target is the 2 GB VM itself (PLAN.md M7: API p95 under 300 ms at 700 students). Testing mode only listens on loopback and lets anyone sign in as anyone, so never run it on a VM that holds real data or is open to students:

1. Use a fresh VM, or one before its first real contest, and stop everything public first: `sudo systemctl stop caddy podium-server podium-judge`. Caddy must stay stopped for the whole run: it proxies to `127.0.0.1:8080`, and a testing-mode server behind it would let anyone on the internet sign in as an owner.
2. Start the testing stack from the release's binaries on the spare ports, which Caddy never proxies: `sudo PODIUM_DEV_STATE=/var/tmp/podium-load PODIUM_LISTEN=127.0.0.1:18080 PODIUM_JUDGE_LISTEN=127.0.0.1:18081 bash tools/dev-stack.sh /usr/local/bin` (all its state stays in `/var/tmp`; the real database is not touched).
3. From another machine with k6 (k6 at 700 students needs 1 to 2 GB of memory itself), forward the port: `ssh -N -L 18080:127.0.0.1:18080 you@contest.example.edu`, then run the seed and k6 against `http://127.0.0.1:18080` as above.
4. Afterwards stop the stack (`sudo pkill -f /var/tmp/podium-load/podium-`), delete `/var/tmp/podium-load`, and start `podium-server`, `podium-judge` and `caddy` again. Then run the "not in testing mode" check in `docs/RUNBOOK.md`, "A week before".

## Reading the results

- The k6 summary prints the API's p50, p95 and p99, failures, 429s, event stream reconnects and the per-route p95.
- `summarize.py` writes `results/console-*.csv` (one row per 5 s: queued and leased jobs, judged per minute, p95 wait for a submission, event streams, database pool waits, heap) and prints the peak queue, the judge's throughput while the queue was long, and how many seconds after the end the queue drained.
- Record the peak, the drain time and the throughput the run measured in a new dated section of the calibration notes; the docs quote only those numbers. A fixed-start spike of S submissions a minute for T minutes against a judge doing J a minute builds a queue of about (S - J) × T and drains in about that divided by J.
- The metrics route the monitor reads, `GET /api/admin/metrics`, is described in `docs/OPERATIONS.md`, "Metrics".
