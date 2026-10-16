# Architecture

podium is two Go programs and a static web app on one Linux VM.

```
students and admins (browser)
          | HTTPS
        Caddy            TLS, compression, http to https
          | 127.0.0.1:8080
   podium-server         API, live updates, web app, judge queue, SQLite
          | HTTP + token (worker API, arrives in M3)
   podium-judge          runs as user "judge", no database access
          |
       isolate           one fresh box per compile and per test
```

The design target is 700+ students in one contest on a VM with 2 GB of RAM. That constraint drives most choices below.

## Services

| Service | Runs as | Responsibility |
|---|---|---|
| `podium-server` | `podium` | Serves the embedded Svelte app, the JSON API, server-sent events, sign-in, scoring and the leaderboard cache. Owns the SQLite database. |
| `podium-judge` | `judge` | Claims submissions from the server, compiles and runs them inside isolate, reports per-test results. Cannot read the database directory. |
| `isolate` | setuid root, invoked by `judge` | The sandbox: namespaces, control groups, seccomp. |
| Caddy | `caddy` | Automatic HTTPS, compression, reverse proxy. |

Server packages (`server/internal`): `config` (environment and start-up validator), `db` (SQLite and migrations), `httpapi` (routes and middleware), `webui` (embedded app), `judge` (sandbox driver, verdicts, checkers), `judge/calib` (host calibration and the hostile-program suite). Later milestones add `auth`, `problems`, `contest`, `ai` and `events`.

## The judge

### Sandbox

Every compile and every run happens inside an isolate box in control-group mode. The runner builds the isolate command line itself; nothing is passed through a shell.

| Limit | Value |
|---|---|
| CPU time | problem limit times the language factor |
| Wall time | 2 x CPU + 1 s (catches sleeping programs) |
| Extra time | 0.5 s, so the reported time shows how far past the limit a program ran |
| Memory | problem limit plus the language overhead, for the whole control group; swap disabled per box |
| Output | 16 MB per file (`--fsize`); exceeding it is "Output limit exceeded" |
| Processes and threads | per language (1 for C++ and Python, 16 for Node, 64 for Java) |
| Network | none: each box has its own empty network namespace |
| Environment | only `PATH`, `HOME`, `TMPDIR`, `LANG` |
| Filesystem | isolate defaults (`/usr`, `/bin`, `/lib`, a private `/tmp`), plus `/etc/java-21-openjdk` for Java only. `/etc` is otherwise absent. |

Compiles get 10 s of CPU, 20 s wall, 512 MB, and their output is truncated to 16 KB.

**A fresh box per test.** The runner initialises a new box for each compile and for each test: it copies the compiled program and the input in, runs, copies the output out, and destroys the box. A control group keeps the memory peak of earlier runs, so reusing a box would report wrong memory figures; and a program could leave files behind for later tests. The cost was measured at about 30 ms per test.

**Only regular files cross the boundary, and links are never followed.** Every file the judge reads from a box is opened with `O_NOFOLLOW` and `O_NONBLOCK` and must be a regular file. A program that replaces its output with a symlink (say to `/proc/self/environ`, which would expose the judge's own environment) or with a FIFO (which would hang the lane) simply produces empty output. Artifacts are capped in total size.

**Boxes live on tmpfs.** isolate's box root `/var/lib/isolate` is a tmpfs mount. Pages written to tmpfs are charged to the writing process's memory control group, so a program's files count against its own memory limit: writing many files just under the 16 MB per-file cap ends in "Memory limit exceeded" instead of filling the disk the database lives on. It also makes the per-test box set-up cheaper (about 22 ms).

**A memory budget across lanes.** Before starting a box the runner reserves that box's memory limit from a 1024 MB budget, just under the 1100 MB cap on `isolate.slice`. Concurrent boxes can therefore never push the slice into its own OOM killer, which would kill an innocent box and blame it for memory it did not use.

**Verdicts.** The isolate meta file decides: control-group OOM kill is "Memory limit exceeded"; a timeout or CPU time over the limit is "Time limit exceeded"; another signal or non-zero exit is "Runtime error", except when a managed runtime reports its own heap exhaustion (Java `OutOfMemoryError`, Node "heap out of memory", Python `MemoryError`), which is also "Memory limit exceeded". Output that reaches the 16 MB cap is "Output limit exceeded": native programs die on SIGXFSZ, but Python exits with an error and Java keeps looping with its writes failing, so the output size is the signal that works for every language. A program can print the OOM text on purpose to turn a runtime error into a memory verdict; both score zero, so this is accepted.

**Cancellation.** When a judging step is cancelled, isolate gets SIGTERM so it can kill the sandboxed processes and tear down the control group itself, and SIGKILL only after a 5 s grace period.

**Hostile-program suite.** `server/internal/judge/calib` holds programs that must be contained: a fork bomb, a 2 GB allocation, an infinite loop, a sleeping program, endless output in C++, Java and Python, reading `/etc/shadow`, a TCP connection to the internet, `#include "/dev/urandom"` at compile time, a 2 GB Java array, unbounded Python recursion, 100 Node threads, output replaced by a symlink to `/proc/self/environ`, output replaced by a FIFO, and filling `/tmp` with 16 MB files. `make sandbox-test` runs them as the `judge` user; every change to the judge must pass it.

### Languages

| Language | Compile | Time factor | Memory overhead |
|---|---|---|---|
| C++17 (GCC 13) | `g++ -O2 -std=gnu++17 -pipe -static` | 1.0 | 0 |
| Java 21 | `javac`, run with `-Xmx<limit>m -XX:+UseSerialGC -XX:TieredStopAtLevel=1` | 2.0 | +256 MB |
| Python 3.12 | byte-compile check | 3.0 | +64 MB |
| JavaScript (Node 22) | none | 2.0 | +128 MB |

g++ compiles and every JVM invocation share one mutex across lanes, so two lanes plus one JVM stay inside the memory budget.

### Checkers

`tokens` (default) compares whitespace-separated tokens; a single token longer than 4 MB in the student's output is a wrong answer. `exact` compares bytes after turning CRLF into LF and dropping trailing newlines, streaming both sides so the judge never holds a whole output in memory. `float` compares numeric tokens with absolute or relative error 1e-6 by default. `custom` (testlib checkers in the sandbox) arrives with problems in M2.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction, which Litestream replication (M7) bounds.

Schema changes are numbered files in `server/internal/db/migrations` (`0001_identity.sql`, then one per milestone), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

Times are Unix milliseconds. Sessions are stored by the SHA-256 of the cookie value, so a copy of the database cannot be replayed as logins.

## Memory budget on the 2 GB VM

| Component | Approx. RSS |
|---|---|
| Kernel, systemd, sshd, journald | 200 to 250 MB |
| Caddy | 30 to 40 MB |
| `podium-server` (capped at 300 MB by systemd) | 50 to 80 MB |
| `podium-judge` worker (capped at 256 MB, Go heap limit 96 MB; inputs copied into tmpfs boxes count here) | 20 to 60 MB |
| Two sandbox lanes plus one JVM (all boxes together capped at 1100 MB on `isolate.slice`, 1024 MB budget in the runner) | up to about 1 GB |

isolate places every box under `isolate.service`, not under the `podium-judge` unit, which is why the sandbox cap sits on `isolate.slice`. A 1 GB swap file protects the server from a transient spike; boxes cannot swap (isolate sets `memory.swap.max=0` on every box with a memory limit).

## Why these choices

| Choice | Instead of | Reason |
|---|---|---|
| isolate | Docker per submission, Judge0, a home-made sandbox | Docker is a packaging tool and needs many flags set correctly to be a sandbox, and container start-up is slow on 1 to 2 vCPUs. Judge0 brings Rails, Postgres, Redis and Docker, heavier than this whole app. isolate is the IOI sandbox, with simple resource reporting. |
| isolate from the upstream apt repository | building from source | Upstream publishes signed packages for current Ubuntu LTS releases; security fixes (several landed in 2.5) arrive with `apt upgrade`. |
| SQLite (WAL) | Postgres | Postgres costs 150 to 200 MB of RAM and a second process to run. A few writes per second is far inside SQLite's range. The pure-Go driver lets the server cross-compile from Windows without cgo. |
| A table as the queue | Redis or a broker | No extra process; the queue survives restarts because it is the database; remote workers need only HTTP and a token. |
| Go | Node, Python, Rust | Lowest memory of the practical options, one static binary per service, goroutines make 700 live connections cheap. |
| Svelte SPA served by Go | SvelteKit on Node, Next.js | No Node runtime on the VM, smallest client bundle, scoped CSS for a hand-written design system. |
| CodeMirror 6 | Monaco | About 300 KB instead of several MB, on mixed laptops and campus Wi-Fi. |
| Server-sent events | WebSockets, 2 s polling | 700 clients polling every 2 s is about 350 requests per second on one core. Events only flow one way, which SSE handles natively. |
| IOI subtasks, best per subtask | ICPC penalty scoring | Weaker students keep earning points; the owner's choice. |
| Hand-written CSS tokens | Tailwind or a component kit | The generic generated look comes mostly from kits and their defaults. |
| Caddy + systemd | nginx + certbot, Docker Compose | Fewest moving parts that still give HTTPS, restarts and memory caps. |
