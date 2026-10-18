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
| `problems` | problem bank, pack format, content hash, validation rules and summary, output generation |
| `submissions` | runs, submissions, drafts, rate limits, scoring, what each viewer may see, integrity counts |
| `events` | the server-sent event hub: streams per user, fan-out, slow clients dropped |
| `jobs` | the one queue of judge work: leases, results, uploads, sweeps, change notifications |
| `judgeapi` | request and response types shared by server and judge |
| `judge` | sandbox driver, verdicts, built-in and testlib checkers |
| `judge/calib` | host calibration and the hostile-program suite |
| `worker` | the judge's claim loop, blob cache, compile cache, and the validate, run, submit and generate jobs |

Later milestones add `contest` and `ai`.

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
  blobs dir   <-- PUT /api/judge/blob/{sha} -------  generate jobs only: one output, hash checked
  job_results <-- POST /api/judge/report ---------  after each compile and test; empty = heartbeat
                  final: server computes summary, score, verdict
```

| Piece | Design |
|---|---|
| One queue | A single `jobs` table (`kind`, `priority`, `state` queued, leased, done or failed, `payload`, lease hash, lease expiry, deadline, attempts). Four kinds: `run` (priority 10), `submit` (20 to 30), `generate` (85) and `validate` (90). A submission or run is a row in `submissions` that references its job, not a second queue. Lower priority numbers run first, so setter work waits behind contest work. |
| Fair queue | A submission's priority is 20 plus the number of the user's judged submissions on that problem, at most 30. Someone's first attempt goes ahead of someone else's tenth, so one person resubmitting cannot starve the room. |
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

**Output generation (M3).** A pack or test zip may carry inputs without outputs; such a test has `output_sha` NULL and the problem lists it under `problems`, so neither validation nor judging can start. "Generate outputs" queues a `generate` job: the judge compiles the reference, runs it on each input in isolate with the problem's limits, and uploads each output with `PUT /api/judge/blob/{sha}`. This is the one route where data flows from the judge into test data, so it is fenced in:

| Rule | Why |
|---|---|
| Uploads only for a `generate` job, under its current lease | A leaked token alone cannot write into the store. |
| Budget per job: one upload per input, plus 2 | A worker cannot fill the disk through one job. |
| At most 16 MB per output, stored only if it hashes to its name | The same cap as a test file in a pack; the name is the content. |
| A report may name only outputs this job uploaded | Otherwise a worker could point a test at any file already in the store, such as another problem's hidden output. |
| Applied only if the problem's content hash is unchanged since queuing | Outputs computed for an older reference or older inputs are never mixed in. |
| One generation per problem at a time; "Regenerate all outputs" asks to confirm | Replacing outputs is the one action here that destroys setter data. |
| Applied outputs change the content hash | The problem must pass validation again before a contest can use it. |

Generators (programs that write the inputs) are still deferred.

## Submissions and scoring

`0004_submissions.sql` adds `submissions` (one row per Run or Submit, with its job, state, verdict, score, the full result as JSON once done, the client key and the integrity counts), `drafts`, `generations` and `integrity_events`. In M3 the workspace is reached only by organisers ("Try as a student"), so the problem bank is never exposed to students; M4 mounts the same workspace under contests.

```
browser                          podium-server                              podium-judge
 Submit {code, client_key} --->  checks: problem judgeable, rate limits,
                                 key already seen? -> 200 with the first
                                 insert submission + submit job  -------->  claim
 <--- 202 Submission                                                        compile (compile cache)
                                 <-- report per test (no output text) ----  samples, then subtasks
 <--- event: submission          score recomputed from the rows
                                 final: score stored in submissions  <----  final
```

**Judging order and stop rules.** The payload lists the samples first, then each subtask's tests in subtask order. A failing sample stops the whole submission (the payload flag `stop_on_sample_fail`, on for every M3 submission); a failing test stops the rest of its subtask, since the subtask is lost anyway. Every test not run is reported as `SK`, so the result always has a row per test.

**The server scores; the judge only reports.** Score and verdict are recomputed from the per-test rows and the payload on every read while judging, and once more when the job closes. Rules:

| Rule | Effect |
|---|---|
| Subtasks are all or nothing | A subtask earns its points only when every one of its tests is `AC`. Score is the sum of those points. |
| A subtask with no tests | Verdict `SK`, earns nothing. It cannot happen once the structure checks pass, but a silent full score would be the worst failure. |
| Verdict | `CE` when the program did not compile; otherwise the first failing test's verdict in judging order (skipped tests are not failures); `AC` when every test passed. |
| A test the judge never reported | `IE` at close, so a judge that stops early can never earn points. |
| The judge fails the job | The submission ends in `error`, shows no score and is not counted. |
| The checker does not compile | `error`: the student's code is not at fault, so nothing is counted. Only the program's own compile can produce `CE`. |

**What must hold before anything is judged.** Run and Submit require the problem's structure to be complete (`Problems()` empty: tests, samples and subtasks fit together, every test has an expected output); otherwise `409 not_ready` lists what an organiser must fix. Before this check an organiser could submit to a broken problem and an empty subtask scored full points. Validation is not required to try a problem; contests (M4) will require it.

**Rate limits and retries.**

| Limit | Value | Why |
|---|---|---|
| Pending submissions | one per user per problem | The verdict of the last one should be read before the next. |
| Gap between submits | 5 s per user across problems for organisers; contests set their own (M4) | Per user rather than per problem, otherwise spreading submits over problems defeats it. |
| Pending runs, gap between runs | one per user; 2 s | Runs have the highest priority, so they need their own brake. |
| Code and custom input | 64 KB each | Keeps payloads, blobs and compile times small. |
| Integrity events | 100 per request, 5,000 per user per problem | Bounded storage; beyond that more events tell a reviewer nothing new. |

Every refusal says how long to wait, in the message and as `retry_after_s`. The browser makes a `client_key` per press of Submit and reuses it on a retry; the server looks the key up inside the same transaction as the rate checks, scoped to the user and problem, and answers a repeat with the first submission (200). A retry after a lost answer therefore never counts twice and never trips the limit it would otherwise hit.

**Integrity counts (D47).** Each submission stores the typed and pasted character counts and focus losses since the previous one; paste, blur and focus events go to `integrity_events` in batches, outside the submit path, so they never slow or block a submit. Counts only: no keystroke timing and no replay. The client hash is an HMAC of IP address and user agent keyed by the session secret, enough to notice two people on one browser, not reversible by trying campus address ranges. Nothing here scores or flags anyone; it is evidence for a human reviewer in the integrity milestone after M4.

## What students can see

Decision D47 sets the rule: hidden tests never show their input, output or checker comment, and the figures a student sees are coarse enough that they cannot be used to read hidden tests.

| Boundary | Rule |
|---|---|
| Judge report | A `submit` report carrying any output text, or a checker message on a hidden test, is refused. The text never reaches the server, so no later bug can display it. Test verdicts are limited to `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` and `IE`. |
| Submission view | Verdicts, times and scores per test, never input, output or messages. Sample results come with the statement anyway. |
| Coarse view for students | No memory figure on hidden tests, maximum memory rounded up to 16 MB steps, times rounded (10 ms; 100 ms in graded contests from M4). |
| Live events for students | Sent when the sample block or a whole subtask finishes, never per hidden test. |
| Runs | Show output and the expected output, but only on samples or the student's own input. |

Why coarse: a program can encode a few bits of a hidden test's values in how much memory it allocates or how long it runs, then read them back from exact figures, and repeat over many submissions until the test is known. The per-test event timing leaks the same way, since when an event arrives says how long that test took. Rounding to steps far wider than the signal, dropping per-test memory on hidden tests, and batching events per subtask close those channels while keeping what a student needs: which subtask failed, with which verdict, and roughly how close to the limits.

In M3 only organisers submit, so the read routes return full figures; the coarse view is applied to live events for student accounts now and to every student-facing route when M4 opens the workspace to students.

## Live updates

One `GET /api/events` stream per open tab, served by the `events` hub. The jobs service calls a change hook after every committed claim, report, close and sweep; the submissions service then reads the run or submission as its author may see it and publishes it to that user's streams.

| Piece | Design |
|---|---|
| Ordering | Updates for one job are serialised, so they arrive in order. |
| Only visible changes | An update is sent only when its signature (state, verdict, number of finished tests, or for students finished subtasks) differs from the last one sent, which also keeps a 50-test submission from producing 50 identical reads. Users with no open stream are skipped before any read. |
| Never blocks judging | Each stream has a buffer of 16 events. A stream that is full is dropped, not waited for; the browser reconnects and refetches. Every write has a 10 s deadline, so a reader that stops reading frees its goroutine. |
| Limits | 4 streams per user (a fifth replaces the oldest) and 5,000 in total (then `503 busy`, and the page polls). |
| Clock | `event: clock` with `server_now` on connect and every 25 s, instead of a comment ping: it keeps proxies from timing out an idle stream and lets the page keep an offset to the server's clock for contest timers. |
| Fallback | After three failed connections the page polls every 15 s. |

The public server has no write timeout so streams can stay open for hours; the read deadline is cleared on the stream itself. Caddy passes `text/event-stream` responses through without buffering, and the shipped Caddyfile also sets `flush_interval -1` and leaves `/api/events` out of compression.

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

### Compile cache

Run, submit and generate jobs compile through a cache in the judge's cache directory, `compiled/<key>/`, holding the compiled files and the compile result. Validation compiles fresh each time.

| Rule | Value and reason |
|---|---|
| Key | SHA-256 of language, compile command, compile CPU budget and source. A change to the compiler flags or budget is a new key, never a stale hit. |
| What it saves | "Run on samples" followed by "Submit" of the same code compiles once; identical resubmissions and a problem's checker compile once. A hit also skips the compile mutex, which is the scarcest resource on two lanes. |
| Compile errors | Cached, so the same broken code is not rebuilt. |
| Compile timeouts and compiler out-of-memory | Never cached: they depend on load, and the same code may compile when the judge is quieter. Such a build lives in a temporary directory removed after the job. |
| Size | 500 MB, least recently used first (a hit refreshes the entry's time). Evicted after every job. |
| Entries in use | Reference-counted per running job and never evicted while held; an entry is renamed aside before deletion, so a concurrent lookup sees it gone rather than half deleted. Two lanes building the same key keep whichever finishes first. |

### Checkers

`tokens` (default) compares whitespace-separated tokens; a single token longer than 4 MB in the student's output is a wrong answer. `exact` compares bytes after turning CRLF into LF and dropping trailing newlines, streaming both sides so the judge never holds a whole output in memory. `float` compares numeric tokens with absolute or relative error 1e-6 by default. `custom` runs a testlib checker, compiled as C++17 with a 30 s compile budget (testlib is a large header), in a fresh box like any submission: 10 s CPU, 256 MB, one process. Exit 0 is accepted; wrong answer, presentation error and partial scores are wrong answer until per-test scoring arrives in v2; `_fail`, a crash or a limit is an internal error. `testlib.h` is installed by `install.sh` at a pinned commit, checked against its SHA-256.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction, which Litestream replication (M7) bounds.

Schema changes are numbered files in `server/internal/db/migrations` (`0001_identity.sql`, `0002_preferences.sql`, `0003_problems.sql`, `0004_submissions.sql`, then one per milestone), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

Before the first release, M3 edited `0003_problems.sql` in place instead of adding a table rebuild: `testcases.output_sha` became nullable (outputs may be missing), `jobs.kind` accepts `submit`, `run` and `generate`, and `job_results` gained `output` (run excerpts) and `output_sha` (generated outputs, and the hash of a submission's output). A development database created before that change must be deleted (`docs/OPERATIONS.md`, "Upgrade").

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
| Scores computed by the server from per-test rows | taking the judge's total | The judge is a separate process holding a token; the server already has the payload, so recomputing costs nothing and a buggy or compromised judge can only misreport single tests, which the report checks bound. |
| All-or-nothing subtasks, samples first, stop at the first failure | per-test points, running every test | The owner's IOI-style scoring. Stopping early saves judge time on a 2-lane VM; reporting skipped tests as `SK` keeps the result complete. |
| Submit reports carry no output text | filtering output when displaying | Text that never reaches the server cannot be leaked by a later display bug (D47). |
| Coarse figures and per-subtask events for students | exact figures and per-test events | Exact memory and timing on hidden tests are a side channel that reads test data a few bits per submit (D47). |
| Evidence for a reviewer, never an AI-likelihood score | automatic plagiarism or AI detection | No tool reliably tells AI-written code from a student's own; a number gets quoted as proof and flags strong students. Counts are collected for a human (D47). |
| A client key per press of Submit | relying on the rate limit alone | A retry after a timeout returns the first submission instead of being refused or counted twice. |
| Rate gap per user, across problems | a gap per problem | A per-problem gap is defeated by spreading submits over problems. |
| Fair queue priority | first come, first served | One student resubmitting in a loop cannot push everyone else's first attempt back. |
| Compile cache keyed by language, command and source | compiling every job | Run then Submit is the common pattern, and compiles hold the scarce compile mutex. Load-dependent failures are not cached, so a busy moment never becomes a permanent CE. |
| Output generation on the judge, uploaded under lease | running the reference on the server, or accepting any hash | The server never runs untrusted code; tying uploads to the job and its budget keeps the one judge-to-test-data path closed to everything else. |
| Event hub with a small buffer that drops slow clients | waiting for slow clients | A slow phone must never delay judging or other students' updates; reconnecting is cheap. |
| A clock event every 25 s | a comment ping | The same keep-alive also gives the page the server's time for contest timers. |
| `PODIUM_MODE` with a key-free testing mode | one strict configuration | Anyone can try podium and run browser tests against the real server with no Google client; production stays the strict default. |
| Go | Node, Python, Rust | Lowest memory of the practical options, one static binary per service, goroutines make 700 live connections cheap. |
| Svelte SPA served by Go | SvelteKit on Node, Next.js | No Node runtime on the VM, smallest client bundle, scoped CSS for a hand-written design system. |
| CodeMirror 6 | Monaco | About 300 KB instead of several MB, on mixed laptops and campus Wi-Fi. |
| Server-sent events | WebSockets, 2 s polling | 700 clients polling every 2 s is about 350 requests per second on one core. Events only flow one way, which SSE handles natively. |
| IOI subtasks, best per subtask | ICPC penalty scoring | Weaker students keep earning points; the owner's choice. |
| Hand-written CSS tokens | Tailwind or a component kit | The generic generated look comes mostly from kits and their defaults. |
| A curated theme list checked by tests | A custom theme builder | A builder's contrast search checks text against backgrounds but not whether "accepted" and "wrong" stay apart, which is what matters in a contest. |
| Caddy + systemd | nginx + certbot, Docker Compose | Fewest moving parts that still give HTTPS, restarts and memory caps. |
