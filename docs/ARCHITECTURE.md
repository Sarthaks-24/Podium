# Architecture

podium is two Go programs and a static web app on one Linux VM.

```
students and admins (browser)
          | HTTPS
        Caddy            TLS, compression, http to https
          | 127.0.0.1:8080
   podium-server         API, live updates, web app, judge queue, SQLite
          | 127.0.0.1:8081, HTTP + token (judge API, loopback only)
   podium-judge          runs as user "judge", no database access
          |
       isolate           one fresh box per compile and per test
```

The design target is 700+ students in one contest on a VM with 2 GB of RAM. That constraint drives most choices below.

## Services

| Service | Runs as | Responsibility |
|---|---|---|
| `podium-server` | `podium` | Serves the embedded Svelte app, the JSON API, server-sent events, sign-in, scoring and the leaderboard cache. Owns the SQLite database. |
| `podium-judge` | `judge` | `podium-judge work` claims jobs from the server's judge API (validations in M2, submissions from M3), compiles and runs them inside isolate, reports per-test results. Cannot read the database directory. |
| `isolate` | setuid root, invoked by `judge` | The sandbox: namespaces, control groups, seccomp. |
| Caddy | `caddy` | Automatic HTTPS, compression, reverse proxy. |

Server packages (`server/internal`):

| Package | Responsibility |
|---|---|
| `config` | environment variables, run modes and the start-up validator |
| `db` | SQLite and migrations |
| `httpapi` | public routes and middleware, and the judge API handler on its own listener |
| `webui` | the embedded web app |
| `auth` | Google sign-in, sessions, roles, testing-mode sign-in |
| `problems` | problem bank, pack format, content hash, validation rules and summary |
| `jobs` | the one queue of judge work: leases, results, sweeps |
| `judgeapi` | request and response types shared by server and judge |
| `judge` | sandbox driver, verdicts, built-in and testlib checkers |
| `judge/calib` | host calibration and the hostile-program suite |
| `worker` | the judge's claim loop, blob cache and validation run |

Later milestones add `contest`, `ai` and `events`.

## Run modes

`PODIUM_MODE` (decision D48) sets how strict both services are:

| Mode | What it is for | What changes |
|---|---|---|
| `testing` | trying podium, automated end-to-end tests | No Google: a form signs anyone in as any role. The base URL and listen address must be on this machine, so the server refuses to start anywhere else. No secret is needed: the session secret is random per run and the judge uses a public testing token. The web app shows a banner on every page. |
| `dev` | local work with real Google sign-in | Google client and session secret required; http base URL allowed; the judge may use the testing token. |
| `production` (default) | the VM | Everything required, the base URL must be https, the testing judge token is refused. |

Why: before modes, running podium at all needed a Google OAuth client, which blocked quick trials and made browser tests against a real server impossible without real accounts. Testing mode removes every key but is only safe on loopback, so the validator enforces that instead of trusting a warning. Production is the default so a missing setting fails closed. The judge listener is loopback-only in every mode, which is also why a public testing token is acceptable outside production.

## Judge protocol and jobs

Decision D44 brought the judge protocol forward from M3 to M2, built for M3's needs.

```
podium-server                                      podium-judge work (one loop per lane)
  jobs table  <-- POST /api/judge/claim  ---------  every 2 s when idle, with jitter
              --> {job_id, lease, payload}
  blobs dir   <-- GET /api/judge/blob/{sha} -------  X-Podium-Job, X-Podium-Lease; re-hashed, cached
  job_results <-- POST /api/judge/report ---------  after each compile and test; empty = heartbeat
                  final: server computes summary
```

| Piece | Design |
|---|---|
| One queue | A single `jobs` table (`kind`, `priority`, `state` queued, leased, done or failed, `payload`, lease hash, lease expiry, deadline, attempts). M3's submissions will reference a job instead of being a second queue. Lower priority numbers run first; validation is 90, behind all contest work. |
| Results | One `job_results` row per compile or test result, so a report is an insert, and a unique key on (job, kind, test, role) refuses a result reported twice. |
| Leases | A claim returns a random lease, stored only as its SHA-256. It lasts 60 s and every report renews it; the worker sends an empty report every 20 s during long runs. A sweep every 10 s requeues jobs whose lease expired and drops their partial results; the third expiry fails the job. A report with a stale lease gets 409 and the worker drops the job. |
| Deadlines | Set at the first claim from the job's size (for validation: 4 min plus 3 min and six times the time limit per test, at most 2 h) and kept across attempts, so a worker that keeps renewing cannot hold a lane forever. |
| Blob scoping | A blob is served only with a current lease on a job that needs that file. A leaked token alone does not expose the problem bank. The judge re-hashes every download. |
| The server decides | Every report is checked against the job's payload (known tests and roles, possible numbers, sizes). The final verdict on a problem is computed by the server from the stored results, never taken from the judge. |
| Loopback listener | The judge API is a second listener, `PODIUM_JUDGE_LISTEN`, which must be a loopback address and is never proxied by Caddy. A judge on another machine reaches it through an SSH or WireGuard tunnel. Bearer token of at least 32 characters, compared in constant time on SHA-256 digests so the comparison does not leak its length. |

Alternatives rejected: the server running isolate itself (breaks the rule that the server never runs untrusted code; setter and AI-written code is untrusted); an offline `podium-judge validate` over SSH (co-teachers have no shell, and the server could not verify an uploaded result); deferring validation to M3 (M2's acceptance test could not be verified). Judge workers on lab PCs were also rejected: the token reports verdicts and never goes on machines students can reach.

## Problems and validation

Problems live in SQLite (`0003_problems.sql`: `problems`, `subtasks`, `testcases`, `blobs`, `jobs`, `job_results`, `validations`). Sources and statements are columns; test files and, when a validation is queued, program sources are content-addressed files in `PODIUM_TESTS_DIR/<first two hex digits>/<sha256>`, written once and never changed, so the judge caches them by name forever.

**"Validated" is a content hash (D45).** The server hashes a canonical manifest of everything validation depends on: limits, checker, `epsilon`, checker source, reference, brute force and validator (language and source hash), samples, subtask points and ranges, and every test's input and output hashes. A problem is validated when this hash equals the one recorded by its last passing validation. Any edit to those fields changes the hash, so the problem shows as not validated without anyone clearing a flag; title, slug, statement, tags, difficulty and subtask titles are left out, so fixing a typo keeps a problem validated. The judge build is recorded with each result but not hashed, otherwise every judge upgrade would unvalidate the whole bank; instead, a failing re-run of exactly the validated content withdraws the validation. From M4 the contest editor checks the hash.

**The gate.** A validation compiles and runs the reference, an optional brute force, an optional input validator and a custom checker on every test, inside isolate. It passes when everything compiles, the reference is accepted on every test, the validator accepts every input, the brute force never disagrees, and the reference's slowest test takes at most half of its time limit (with its language factor). That last rule turns the authoring guideline "limit at least 3 times the reference" into a check: fail above a half, warn above a third. One open validation per problem: asking again for the same content returns the waiting run, and a queued run for older content is replaced.

**Packs are untrusted input (D46, as amended).** They come from setters and, from M5, from a model, and the server parses them outside any sandbox. Caps: 32 MB zipped, 128 MB unzipped, 2,000 entries, 16 MB per file, `problem.yaml` 64 KB and parsed strictly. Sizes are counted on the bytes actually decompressed rather than limited by a compression ratio, which would refuse legitimate repetitive tests. The entry count is read from the zip's end record before the archive is decoded, because Go's zip reader allocates every entry up front and a small zip can declare hundreds of thousands. Absolute paths, `..`, backslashes and non-regular files are refused. Only one pack operation runs at a time, since each can hold over 100 MB on a 2 GB VM.

**Deferred to M3:** generating outputs from the reference, and generators. Both send data from the judge back into test data, which needs an upload route with its own caps and an organiser confirmation step. Until then packs carry their expected outputs.

## Identity

Sign-in is Google OpenID Connect, run entirely by the server (`server/internal/auth`):

1. `/api/auth/google/start` creates a random state, a nonce and a PKCE verifier, keeps them in a 10-minute HttpOnly cookie signed with `PODIUM_SESSION_SECRET` (named `__Host-podium_signin` on https, so a sibling subdomain or a plain http response cannot plant one and finish an attacker's sign-in in a victim's browser), and redirects to Google. When exactly one domain is allowed it is passed as Google's `hd` hint, which only filters the account picker.
2. Google redirects to `/api/auth/google/callback`. The server checks the state against the cookie (and deletes the cookie), exchanges the code with the PKCE verifier and client secret, and verifies the ID token itself: RS256 signature against Google's published keys (cached, refetched for an unknown key id at most once a minute), issuer, audience, expiry, and the nonce.
3. The verified email must have `email_verified`. It may sign in when its domain is on the allowed list, when the account already holds the admin or owner role, or when it is `PODIUM_OWNER_EMAIL`. The domain check never trusts Google's `hd` claim. Each account is bound to the Google subject id (`sub`) that first signed in with it, and a different subject with the same address is refused: addresses get reassigned (a graduate's address given to someone new, or recreated as a consumer Google account), subject ids do not. An owner can reset the link from the People page.
4. The server creates or updates the user and starts a session: 32 random bytes in the cookie, only the SHA-256 in the `sessions` table. The cookie is HttpOnly and `SameSite=Lax`; with an `https` base URL it is also `Secure` and `__Host-` prefixed (host-only, whole site). Sessions last 30 days and renew on use, at most one database write per day; an hourly sweep deletes expired ones.

Every authenticated request reads the user and role from the database, so disabling an account takes effect on the next request. Removing an organiser's role ends their sessions, and removing a domain ends the sessions of students on it, so nobody keeps access that the current rules would not grant. State-changing API requests must come from the site's own origin (`Origin` equal to `PODIUM_BASE_URL`), on top of the `SameSite` cookie.

| Rule | Why |
|---|---|
| Organisers may sign in from any domain | Admins are invited by exact email, often co-teachers on another domain; the role is a stronger statement than the domain. |
| `PODIUM_OWNER_EMAIL` is always an owner | A permanent recovery path: if every owner is removed or the domain list is emptied, setting it and restarting restores access. The People page shows this owner as locked. |
| Allowed domains seed from configuration once | Only on a fresh install; afterwards the owner's edits on the People page win, so a restart never undoes a removal. |
| ID token signature is verified, not only taken from TLS | The token arrives over TLS from Google's token endpoint, which OpenID Connect accepts on its own; verifying the signature too costs one cached key fetch and removes any dependence on that reasoning. Uses only the Go standard library plus `golang.org/x/oauth2` for the code exchange. |

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

`tokens` (default) compares whitespace-separated tokens; a single token longer than 4 MB in the student's output is a wrong answer. `exact` compares bytes after turning CRLF into LF and dropping trailing newlines, streaming both sides so the judge never holds a whole output in memory. `float` compares numeric tokens with absolute or relative error 1e-6 by default. `custom` runs a testlib checker, compiled as C++17 with a 30 s compile budget (testlib is a large header), in a fresh box like any submission: 10 s CPU, 256 MB, one process. Exit 0 is accepted; wrong answer, presentation error and partial scores are wrong answer until per-test scoring arrives in v2; `_fail`, a crash or a limit is an internal error. `testlib.h` is installed by `install.sh` at a pinned commit, checked against its SHA-256.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction, which Litestream replication (M7) bounds.

Schema changes are numbered files in `server/internal/db/migrations` (`0001_identity.sql`, `0002_preferences.sql`, `0003_problems.sql`, then one per milestone), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

Times are Unix milliseconds. Sessions are stored by the SHA-256 of the cookie value, so a copy of the database cannot be replayed as logins.

## Themes

The web app ships ten colour themes plus "System", which follows the device's light or dark setting:

| Group | Themes |
|---|---|
| Light | Light, Nordic frost, Soft lavender |
| Dark | Dark, Overcast, OLED black, Nordic slate, Cosmic violet |
| Accessible | High contrast light, High contrast dark (all text at 7:1 or more) |

All colours live in `web/src/styles/tokens.css`. A theme changes surfaces, ink, rules and the one action colour; it never changes what a verdict colour means. Light themes share one set of verdict colours and dark themes another. A separate setting, "Colour-blind safe verdict colours", replaces green and red with teal, purple and orange in any theme; the three stay apart under simulated protanopia and deuteranopia. The editor well stays dark in every theme.

The choice belongs to the account (`users.theme`, `users.verdicts`; `PUT /api/me/preferences`), because students share lab machines: a high-contrast theme chosen for low vision follows the person, and signing out returns the machine to the system theme. The browser keeps a copy in localStorage only so the first frame is painted right.

Every theme is checked by tests rather than by eye: text at 4.5:1 (7:1 in the high-contrast themes) on every surface, control borders at 3:1, verdicts at 4.5:1 on their tints, and a minimum OKLab distance between the action colour and each verdict and between the verdicts themselves. A browser test confirms the CSS cascade produces exactly the values checked.

The themes are curated versions of palettes from the owner's AdminPanelManager project. Palettes were left out where they break the design brief's bans (dark slate with neon, cream with terracotta, glows), where their accent is a verdict colour (green or amber accents read as "accepted" or "partial"), or where they depend on two competing accents. Projector reveal mode (M4) ignores personal themes and uses High contrast light.

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
| One `jobs` table for every kind of judge work, from M2 | a submissions table as the queue (the original plan), or one queue per kind | Validation needed the judge in M2; building the M3 protocol then, with one queue, avoids a second queue and a migration later. |
| Judge pulls work over HTTP with leases | the server pushing to workers, or the server running isolate | Workers stay stateless and can restart at any time; an expired lease simply returns the job. The server never runs untrusted code. |
| Judge API on a loopback-only listener | judge routes on the public listener behind a token | A wrong proxy rule cannot expose them; remote judges use a tunnel. |
| Validated = hash of the content | a "validated" flag cleared on edit | Nothing can forget to clear it; statement typos do not unvalidate; the contest editor can compare one value. |
| Reference must use at most half the limit | trusting the setter's limit | A limit too close to the reference turns judge noise into wrong verdicts for correct students. |
| Pack sizes counted on decompressed bytes | a compression-ratio cap | Real tests are often repetitive and compress far beyond 100:1; counting what is actually decompressed stops zip bombs without refusing them. |
| Testlib checkers | a home-made checker protocol | The format setters already know from Codeforces and Polygon. |
| `PODIUM_MODE` with a key-free testing mode | one strict configuration | Anyone can try podium and run browser tests against the real server with no Google client; production stays the strict default. |
| Go | Node, Python, Rust | Lowest memory of the practical options, one static binary per service, goroutines make 700 live connections cheap. |
| Svelte SPA served by Go | SvelteKit on Node, Next.js | No Node runtime on the VM, smallest client bundle, scoped CSS for a hand-written design system. |
| CodeMirror 6 | Monaco | About 300 KB instead of several MB, on mixed laptops and campus Wi-Fi. |
| Server-sent events | WebSockets, 2 s polling | 700 clients polling every 2 s is about 350 requests per second on one core. Events only flow one way, which SSE handles natively. |
| IOI subtasks, best per subtask | ICPC penalty scoring | Weaker students keep earning points; the owner's choice. |
| Hand-written CSS tokens | Tailwind or a component kit | The generic generated look comes mostly from kits and their defaults. |
| A curated theme list checked by tests | A custom theme builder | A builder's contrast search checks text against backgrounds but not whether "accepted" and "wrong" stay apart, which is what matters in a contest. |
| Caddy + systemd | nginx + certbot, Docker Compose | Fewest moving parts that still give HTTPS, restarts and memory caps. |
