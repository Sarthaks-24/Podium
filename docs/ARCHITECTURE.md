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
| `problems` | problem bank, pack format, content hash, validation rules and summary, output generation, generators and the generate inputs chain, AI drafts |
| `submissions` | runs, submissions, drafts, rate limits, scoring, what each viewer may see, integrity counts |
| `events` | the server-sent event hub: streams per user, fan-out, slow clients dropped |
| `jobs` | the one queue of judge work: leases, results, uploads, sweeps, change notifications |
| `judgeapi` | request and response types shared by server and judge |
| `judge` | sandbox driver, verdicts, built-in and testlib checkers |
| `judge/calib` | host calibration and the hostile-program suite |
| `worker` | the judge's claim loop, blob cache, compile cache, and the validate, run, submit, generate and generate_inputs jobs |
| `contests` | schedules, windows, access, standings, clarifications, export, integrity retention |
| `integrity` | integrity checks after a contest (lexers, winnowing, signals), the review cases and their retention |
| `ai` | the AI wizard's model side: the in-memory key store, the Anthropic client and the testing-mode fake, the problem schema and prompts, answer checks |

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
  blobs dir   <-- PUT /api/judge/blob/{sha} -------  generate and generate_inputs jobs only: hash checked
  job_results <-- POST /api/judge/report ---------  after each compile and test; empty = heartbeat
                  final: server computes summary, score, verdict
```

| Piece | Design |
|---|---|
| One queue | A single `jobs` table (`kind`, `priority`, `state` queued, leased, done or failed, `payload`, lease hash, lease expiry, deadline, attempts). Five kinds: `run` (priority 10), `submit` (20 to 30), `generate` and `generate_inputs` (85) and `validate` (90). A submission or run is a row in `submissions` that references its job, not a second queue. Lower priority numbers run first, so setter work waits behind contest work. |
| Fair queue | A submission's priority is 20 plus the number of the user's judged submissions on that problem, at most 30. Someone's first attempt goes ahead of someone else's tenth, so one person resubmitting cannot starve the room. Rejudges run at 50 and practice submissions at 60. |
| Claim rules | Strict priority with three exceptions. After a run is claimed, the next claim takes other work first when any is waiting, so students pressing Run (priority 10) cannot starve submissions: at least every other claim is a submission. Jobs of a paused contest are skipped. While a graded contest is live, at most one validation or generation job (outputs or inputs) is leased at a time, so setters' work never takes more than one lane from contest judging. |
| Upload budget | A generate job's upload slot is reserved, under the queue's lock, before the body is read and given back if the upload fails, so concurrent uploads under one lease cannot exceed the budget. The body streams to a temporary file in the blob directory while it is hashed and is renamed into place only if the hash matches, so uploads cost disk, not memory. When the job closes or its lease is swept, uploads that no problem recorded (and no other open job holds) are deleted. |
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

**Tests from a generator (M5).** A problem may carry a generator (setter code in any of the four languages, usually Python) and a plan: a list of tests, each with its subtask, a seed and arguments (`problems.generator_language`, `generator_source`, `generator_plan`, and `subtasks.generator_args` for the arguments the editor proposes). "Generate inputs" queues a `generate_inputs` job (priority 85): the judge compiles the generator, runs it in isolate once per planned test with argv seed then arguments and an empty stdin, and uploads each output through the same `PUT /api/judge/blob/{sha}` route and budget rules as output generation (one per planned test, plus 2). When every planned test got an input, the job's `Finish` stores them as tests and, in the same transaction, queues output generation from the reference with `then_validate` set; that job's `Finish` queues validation when every output arrived. One press runs the whole chain, and each step reports its own failures by test, subtask and seed.

Arguments are limited to letters, digits and `_ . , : = + -` (64 characters, 16 per test) and seeds to a signed 32-bit integer. Braces are excluded because the judge substitutes `{mem}` in run commands; the arguments reach the program as argv after isolate's `--`, never through a shell. The worker checks them again before building a command line.

**The generator is not part of the content hash (D53).** The content hash covers the tests the generator made, which is what students are judged on. Hashing the generator too would unvalidate a problem when someone tidies the generator without regenerating, although nothing a student sees changed. Instead a separate plan hash (generator language and source hash, plus the plan) travels in the `generate_inputs` payload, next to the content hash; `Finish` applies nothing if either changed while the job ran, so inputs from an older generator are never mixed in. Editing a generator therefore leaves validated tests alone until someone presses Generate inputs again.

## AI wizard

The `ai` package (M5) is the model side: the key store, the provider clients, the problem schema and prompts, and the checks that turn a model's answer into a draft. `httpapi` holds the routes and the in-memory wizard runs; `problems` turns a checked answer into a draft and runs the generator chain.

```
admin pastes key --> PUT /api/admin/ai/key --> ai.NewSecret at once --> Check (model lookup, no tokens) --> ai.Keys (memory, 2 h)
admin: brief or text --> POST /api/admin/ai/wizard --> slot (one per admin) --> goroutine, 3 min:
    ai.Generate: one structured call, checks, at most one retry
    --> problems.CreateDraft (origin ai, tag "ai draft") --> RequestInputGeneration
    --> generate_inputs --> generate (then_validate) --> validate          (all on the judge, in isolate)
```

**The key never touches disk (D50).** The key is pasted by an admin, never configured: there is no environment variable, so the owner never has to put a provider key on the VM, and an organisation with no key loses nothing but the wizard. On the key route the request body is decoded into a local string, wrapped in `ai.Secret` and the string cleared before anything else runs. `Secret` prints, formats, marshals to JSON or text and logs (`slog.LogValuer`) as `[redacted]`; only the `ai` package can read the value, to hand it to the SDK per request with `option.WithAPIKey`, with the SDK's environment lookup switched off (`option.WithoutEnvironmentDefaults`). Keys sit in a map keyed by the SHA-256 of the session cookie, for 2 hours; a sweep every 10 minutes drops expired ones, signing out (or signing in again in the same browser) drops the session's key, and a restart drops them all. Provider errors are mapped to fixed sentences: the SDK's error holds the request, whose headers carry the key, and the raw response body, so it is never wrapped, formatted or logged. Job payloads hold only the generator, the plan and hashes. Request bodies are never logged. An end-to-end test runs the wizard with a real-looking key against a stand-in for the Claude API and then searches every log line (debug included), the SQLite database and WAL files and the blob store for it.

**One structured call.** The client is the official Go SDK (`anthropic-sdk-go`, pinned). A generation streams (`Messages.NewStreaming`, accumulated), with `max_tokens` 64,000, thinking left at the model's default and effort `high`. The answer's shape is enforced with structured outputs: `output_config.format` carries a JSON schema for the whole pack (statement, samples, subtasks with generator arguments and test counts, C++ reference, Python brute force, generator and validator, checker, limits, tags, difficulty) with `additionalProperties: false` everywhere. Lengths and ranges are not expressible there, so the server decodes strictly and then checks the wizard's rules (one to five samples, points adding up to 100, at most 30 planned tests) and the editor's own field rules on the draft the answer would become. A failing answer is sent back once with the list of reasons; a cut-off answer (`stop_reason` `max_tokens`) is retried once with a request for a shorter answer; a refusal (`stop_reason` `refusal`) is reported with its category and not retried.

**Refusal fallbacks (D54).** For Opus 5.5 and Sonnet 5.5 requests carry the beta `server-side-fallback-2026-07-01` with `fallbacks: "default"`, so a request refused by the safety classifier for a reason that does not apply to a fallback model is answered by that model instead of failing (contest problems about "attacks" on a grid or "exploiting" a recurrence are ordinary). A mid-stream fallback restarts the text, and the client keeps only the fallback's answer. Haiku 5.5 has no fallback. The default model is Opus 5.5; the admin may choose Sonnet 5.5 or Haiku 5.5.

**Prompt injection.** The system prompt is fixed text. The brief or pasted text goes into the user message inside one `<brief>` or `<problem_text>` block, the system prompt says that everything inside it is data, and any tag in the data that could close the block early is defused. The worst a hostile text can do is make the model write a bad problem, which the checks, the judge and the admin's review catch: the model's code is never run on the server, only on the judge in isolate, like any setter's code.

**Nothing AI-made reaches students unreviewed.** A draft carries the tag `ai draft` until a person saves it. The chain may validate the draft on its own, but a contest refuses a problem still tagged `ai draft`, and only validated problems can join a contest at all.

**Can a model solve this?** sends the statement alone and asks for a C++17 solution, then judges the answer as the admin's own submission (client key `ai-solve-{wizard_id}`), so the editor reads the score from the ordinary submission view.

**Limits.** One AI request per admin at a time (a second is refused, not queued, so a double click cannot spend twice), 3 minutes per request with its retry, 20,000 characters of pasted text. Wizard runs live in memory for an hour after they end. In testing mode a `fake` provider answers with two canned packs whose programs really work on the judge, so the whole pipeline is tested without a network or a key.

**Cost on the VM.** The SDK adds about 4.9 MB to the stripped release binary (13.7 MB to 18.6 MB, linux/amd64, measured 2026-10-18; 7.1 MB unstripped) and nothing at rest: no goroutine runs until a request does.

## Submissions and scoring

`0004_submissions.sql` adds `submissions` (one row per Run or Submit, with its job, state, verdict, score, the full result as JSON once done, the client key and the integrity counts), `drafts`, `generations` and `integrity_events`. Outside contests the workspace is reached only by organisers ("Try as a student"), so the problem bank is never exposed to students; contests mount the same workspace for students with contest rules (see "Contests"). A run row keeps only the hash of its code and no copy of its custom input: both are in the blob store, and 128 KB inline per run would make every scan of a user's rows walk overflow pages while holding SQLite's single writer lock.

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

**Judging order and stop rules.** The payload lists the samples first, then each subtask's tests in subtask order. A failing sample stops the whole submission (the payload flag `stop_on_sample_fail`, on outside contests and a per-contest setting, on by default); a failing test stops the rest of its subtask, since the subtask is lost anyway. Every test not run is reported as `SK`, so the result always has a row per test.

**The server scores; the judge only reports.** Score and verdict are recomputed from the per-test rows and the payload on every read while judging, and once more when the job closes. Rules:

| Rule | Effect |
|---|---|
| Subtasks are all or nothing | A subtask earns its points only when every one of its tests is `AC`. Score is the sum of those points. |
| A subtask with no tests | Verdict `SK`, earns nothing. It cannot happen once the structure checks pass, but a silent full score would be the worst failure. |
| Verdict | `CE` when the program did not compile; otherwise the first failing test's verdict in judging order (skipped tests are not failures); `AC` when every test passed. |
| A test the judge never reported | `IE` at close, so a judge that stops early can never earn points. |
| The judge fails the job | The submission ends in `error`, shows no score and is not counted. |
| The checker does not compile | `error`: the student's code is not at fault, so nothing is counted. Only the program's own compile can produce `CE`. |

**What must hold before anything is judged.** Run and Submit require the problem's structure to be complete (`Problems()` empty: tests, samples and subtasks fit together, every test has an expected output); otherwise `409 not_ready` lists what an organiser must fix. Before this check an organiser could submit to a broken problem and an empty subtask scored full points. Validation is not required to try a problem; contests require it, and the problem must still be the validated version that was added.

**Rate limits and retries.**

| Limit | Value | Why |
|---|---|---|
| Pending submissions | one per user per problem | The verdict of the last one should be read before the next. |
| Gap between submits | 5 s per user across problems for organisers; contests set their own (M4) | Per user rather than per problem, otherwise spreading submits over problems defeats it. |
| Pending runs, gap between runs | one per user; 2 s | Runs have the highest priority, so they need their own brake. Both checks read only the `submissions_by_kind (user_id, kind, state, created_at)` index. |
| Code and custom input | 64 KB each | Keeps payloads, blobs and compile times small. |
| Draft saves | one per user every 2 s | Each save takes the writer lock; a script saving in a loop would hold up judge reports for everyone. |
| Integrity events | one batch per user every 5 s, 100 per request, 5,000 per user per problem | Bounded storage; beyond that more events tell a reviewer nothing new. Kinds, rate and cap are checked before the write lock is taken. |

Every refusal says how long to wait, in the message and as `retry_after_s`. The browser makes a `client_key` per press of Submit and reuses it on a retry; the server looks the key up inside the same transaction as the rate checks, scoped to the user, problem and contest, and answers a repeat with the first submission (200), shown as the viewer may see it. A retry after a lost answer therefore never counts twice and never trips the limit it would otherwise hit.

**Integrity counts (D47).** Each submission stores the typed and pasted character counts and focus losses since the previous one; paste, blur and focus events go to `integrity_events` in batches, outside the submit path, so they never slow or block a submit. Counts only: no keystroke timing and no replay. The client hash is an HMAC of IP address and user agent keyed by the session secret, enough to notice two people on one browser, not reversible by trying campus address ranges. Nothing here scores or flags anyone; it is evidence for a human reviewer in the integrity milestone after M4.

## Contests

`0005_contests.sql` adds `contests` (schedule, rules and switches), `contest_problems` (label, order and the pinned content hash), `contest_roster`, `contest_admins` (co-organisers), `contest_starts` (honour code acceptance and, in window mode, the start of the student's clock), `contest_extensions`, `clarifications` and `announcements`. The code is `server/internal/contests`; the contest workspace reuses `server/internal/submissions` with a scope built from the contest.

**Problems are pinned.** A contest takes only validated problems, and adding one records its content hash and its subtask points; the board scores against the pinned points and clamps any earned value to them, so an edit to a problem never changes a contest's board until the problem is added again. Only owners and admins choose a contest's problems and publish it: the problem bank is theirs, and co-organisers (often student TAs) never see it. A problem is ready only while both its content hash and its validated hash still equal the pin. Publishing, running, submitting and rejudging all require that: a test edited mid-contest stops judging of that problem (`409 not_ready`) until it is validated and added again, so no student is ever judged against tests nobody checked.

**Time.** Every rule uses the server's clock, and every answer carries `server_now` so the page can show a correct countdown from a wrong laptop clock.

| Start mode | A student's window |
|---|---|
| Fixed | From the start to the end, plus any contest-wide extension, plus their own extension. |
| Window | From when they press Start (after accepting the honour code) for `duration_minutes`, plus extensions, never past the end plus the contest-wide extension plus their own. |

Submissions count while the student's own window is open. After it they are practice (judged at priority 60, never on the board) when the contest allows practice and every student's window is over (the close plus the longest personal extension), so nobody can test solutions against hidden tests, with full feedback, while a classmate with a later start or extra time is still competing. The published editorial waits for the same moment. Otherwise run and submit are refused. Problems open once the student has accepted the honour code and their window has opened (graded contests record the acceptance time, D47), and stay readable after it closes.

**Access.** Domain access: anyone who can sign in (sign-in already requires an allowed domain). Roster access: the emails on the roster. Owners, admins and the contest's co-organisers organise it and are never on the board. Co-organisers are trusted with the running of their contest, not with hidden data: they may try its problems at any time, but with the contest's limits and the student view of results, and they cannot choose problems or publish. Every id in a contest route is resolved through the contest in the URL (a label to a problem id, a clarification or submission id checked against the contest), so ids from another contest give `404`, and organiser routes answer `404` rather than `403` to anyone who does not organise that contest.

**Scoring (PLAN.md section 9).**

| Rule | Detail |
|---|---|
| Best per subtask (default) | Per problem, the sum over subtasks of the best points earned on that subtask across the student's non-practice submissions. Two partial submissions can make a full score. |
| Best submission | The single highest-scoring submission per problem. |
| Rank | Total, highest first; ties broken by the earliest time at which the student reached their final total, measured from their own start in window mode; equal totals and times share a rank. |
| Freeze | The frozen board counts only submissions made before the freeze. Fixed start: one time for everyone, the close minus `freeze_minutes`; a personal extension never moves it, or a student's first minutes of the freeze would show to everyone. Window mode: each student's own end minus `freeze_minutes`. Later submissions show as "+N" pending in their cell. |

**Which board a student sees.**

| Contest | Board |
|---|---|
| Fixed start | Full until the freeze; frozen from the freeze until every window has closed (including personal extensions); then full. With `hold_reveal`, frozen until an organiser reveals, so the projector walk is a surprise. |
| Window mode | None until the student's own window closes (the window-mode ruling: a live board in a flexible window tells later starters what is achievable), then full. |
| Exam feedback | None until the student's own window closes, since the board would show their score. |

Organisers always see the full board, and either variant on request. The reveal hands the projector both boards once every window has closed; the web app walks the frozen board from the bottom, moving each row to its final place.

**The board cache.** Standings are a pure function of the contest, its problems and its submissions (`contests.Compute`, tested against hand-computed boards), cached in memory per contest and variant. Anything that changes a board (a verdict, a rejudge, a pin change, an extension, a new participant) bumps the contest's version; a new submission does not, since it changes nothing until its verdict. The work is bounded however many people look at once:

| Rule | Effect |
|---|---|
| One computation at a time per contest and variant | Readers arriving meanwhile get the last board, or wait for the running computation when there is no board yet or it is already for the current version. Measured: 100 concurrent reads after a change compute once. |
| At most one recompute every 2 s per contest | Inside that time the last board is served and one refresh is scheduled; a burst of verdicts costs one recompute. |
| Refresh, then tell | After a change the server recomputes the boards that are being watched, then sends the `standings` event, so 700 browsers refetch a board already in the cache. Exam-mode contests send it to organisers only. |
| Index-only read | Each judged submission stores its earned points per subtask in a small `earned` column, written when judging finishes; the board reads only the `submissions_by_board` index, never the rows with their inline code and full results. Measured: about 34 ms for 300 students with 40 submissions each (it was 0.87 s reading the rows). |

Why in memory and not a `standings_cache` table: one server, a few thousand submissions per contest, and a recompute measured in milliseconds. A table would add writes on the judging path for no gain, and a stale row would be a correctness risk after a restart.

**Feedback.** "Subtask" shows subtask results and per-test verdicts with coarse figures. "Exam" shows only "Submitted" until the student's window closes, then the score. Displayed times round to 100 ms in graded contests and 10 ms otherwise (D47). Hidden tests never show input or output in any mode.

**Running a contest.** Pause stops the judge taking the contest's jobs (they queue; nothing is lost). Extend adds minutes to everyone's end; a personal extension adds minutes for one student. Rejudge queues a problem's submissions (or one) again at priority 50 with the current tests and clears their results, so the board counts each again once judged; a waiting rejudge never blocks the student's own next submission. Submissions still waiting or being judged on the old tests are included: their job is cancelled and replaced, and a job's Finish updates its submission only while it is still that submission's job, so a replaced job can never overwrite the new result. Targets are chosen inside the same write transaction, and one already waiting on exactly the current tests is skipped, so two rejudges at once do the work once. Clarifications are private to the asker until an organiser makes the answer public; students never see who asked. Announcements and answers arrive as live events.

**Integrity (D47).** A second event stream opened from another browser or machine while one is open is recorded as a `second_session` event for every contest whose window is open for that student, at most once per 10 minutes per contest. A retention job, run at start-up and every 12 hours, deletes integrity events and clears submissions' client hash, typed and pasted counts and focus losses 180 days after a contest ended for everyone, except for students in an `open` or `awaiting_student` review case for that contest (see "Integrity" below).

## Integrity

`0006_integrity.sql` adds `integrity_runs` (one per "Check integrity" press, with its counts), `integrity_signals`, `integrity_cases` (with a copy of their evidence), `integrity_case_users` (sharing and the student's explanation) and `integrity_decisions` (append only). The code is `server/internal/integrity`; the routes are in `server/internal/httpapi/integrity.go`. The reviewer's guide and the policy template are `docs/INTEGRITY.md`.

**What is checked.** An organiser with review permission starts a check once the contest is over for everyone (the close plus the longest personal extension). It reads each problem's judged, non-practice contest submissions by students; organisers' and co-organisers' submissions are never evidence.

| Signal | Method |
|---|---|
| Similar code | Each student's best submission per problem and their last one below full marks. A lexer per language turns code into tokens with identifiers and literals collapsed and comments and layout dropped; winnowing (k=5, w=4, FNV-64) picks fingerprints. Fingerprints of the reference and brute force, and those more than a tenth of the students (at least 5) share, are dropped. Score is containment: shared fingerprints over the smaller program's. Programs under 40 tokens or 8 remaining fingerprints are skipped. Pairs at 0.6 or more are reported (weak; moderate at 0.75, strong at 0.9), with matched byte spans for the side-by-side view, and joined into clusters. |
| Same wrong output | Students grouped by problem, hidden test and the SHA-256 of their wrong output (reported by the judge since M3). Outputs more than 3 students printed, or empty output, are ignored. Two shared tests are moderate, four strong. Works across languages. |
| Hard-coded answers | A hidden test's expected output (8 bytes to 4 KB, not a single small number) found in a source with whitespace normalised. |
| Probing | Six or more distinct verdict vectors on hidden tests from consecutive attempts whose code barely changed (ten is strong). |

The timeline (submissions with typed and pasted counts, pastes, focus changes, second sessions, other students on the same browser and network) is context for a reviewer and never a signal: lab machines are shared, and pasting your own template is allowed.

**Calibration.** The calibration test in `server/internal/integrity/similar_test.go` has six independent solutions to one problem in different styles and three disguised copies (renamed identifiers, reordered functions, changed comments and layout). The copies score 0.94 to 0.98; the most similar independent pair scores 0.27 against the 0.6 threshold, and the test fails if any independent pair comes within 0.2 of it. Short programs (A plus B) and programs everyone shares produce nothing.

**Review.** A case is opened from signals with a title and a reason of at least 20 characters; the signals are copied into it, so a new check (which replaces the contest's signals) never changes a case. Sharing the case (`awaiting_student`) lets each student see their own evidence and write an explanation; they never see another student's name, email or code, or the reviewers' notes. Clearing or actioning closes the case; action is refused until every student in it was shared it. Every state change, the first one included, is a row in `integrity_decisions`, which the code only ever inserts into, and the server log records opens and decisions by id.

**Who reviews.** Owners and admins, and co-organisers of the contest whose role is not student. Co-organisers who are students (TAs) run contests but never review their classmates.

**Retention.** Contest records follow the 180-day rule above, skipping students in an open case until it is decided. A closed case keeps its evidence and explanations for 2 years after the decision; then both are emptied and the case row with its decision log stays, so the outcome and its reasons remain on record. Submissions themselves are not deleted by either rule.

**Why there is no AI score.** No tool reliably tells AI-written code from a student's own: detectors are wrong often enough to accuse honest students, and a number on a screen gets quoted as proof. Podium relies on prevention (fixed-start contests in a lab, the honour code, and in M5 a "Can a model solve this?" check for setters), indirect evidence (large pastes, a full score far faster than the class, clusters of near-identical code) and verification (the student explains or changes the code in front of an organiser). Signals are worded as strength, with the raw number beside them, and nothing happens without a person.

**Why on the server (D49).** The check runs in one bounded background goroutine in `podium-server`, not as a judge job: its inputs are trusted text (sources, verdicts, output hashes) and nothing is executed, so the sandbox adds nothing, while sending every source to the judge would widen what the judge holds. One check runs at a time per server (`409 busy` otherwise), with a 10-minute limit; the request returns at once with the run's id and the page polls. A check left running by a restart is marked failed at start-up. Revisit if a large contest's check takes long enough to hurt request latency.

### Fixes after the integrity review

- **Signal and run ids are never reused** (`AUTOINCREMENT`), and a case's compare view reads the case's own copy of the evidence through its own route. A re-run used to free ids that SQLite handed out again, so a case could open another pair of students.
- **The case state machine is enforced on the server.** An allowed-transitions table; action needs every student's turn to be over (an explanation after the latest share, or the 7-day response window, `integrity.ResponseWindow`); a reopen ends the turn and needs a new share. Clearing is always allowed because it never hurts a student. Reason: D47 says students explain before any decision; the review showed a case could be actioned the instant it was shared.
- **Explanations are versioned** in `integrity_explanations`; a decision carries the versions the reviewer read and is refused (409) if a newer one exists. The decision row stores them (`explanations_seen`).
- **The decision log is append only in the database**: triggers refuse UPDATE and DELETE on `integrity_decisions`, and `case_id` and `by_user` are `ON DELETE RESTRICT`. The triggers also block cascades, on purpose: nothing deletes users, cases, or contests that have cases (the clone rollback deletes only a contest created a moment earlier, which has none), so a cascade into the log would only ever be a mistake.
- **False-positive guards found by the review:**
  - Hard-coded answers skip expected outputs that appear in the reference, the brute force or the statement, or in the programs of more than max(3, 10%) of the students; each distinct source is squashed once (was once per submission per output, about 8.5 s per problem in the worst case).
  - Probing ignores all-SK attempts, compares near-identity with the first attempt of a run, and counts only flips (AC back to a failure, or one failure to another), not forward progress.
  - Similar code: strength needs an amount of shared fingerprints as well as the score; the common-fingerprint limit is counted per language; `Settings.ExtraBase` accepts more base programs (templates), not yet filled from any setting.
  - Same wrong output: the common-mistake limit is max(3, 2% of the students).
- **The lexer reads hidden code**: the body of a C++ `#define` (only `#include` lines are dropped; line continuations join lines) and a Python string literal passed straight to `exec` or `eval`.
- `integrity` now imports `contests` for `RetentionPeriod`.

## What students can see

Decision D47 sets the rule: hidden tests never show their input, output or checker comment, and the figures a student sees are coarse enough that they cannot be used to read hidden tests.

| Boundary | Rule |
|---|---|
| Judge report | A `submit` report carrying any output text, or a checker message on a hidden test, is refused. The text never reaches the server, so no later bug can display it. Test verdicts are limited to `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` and `IE`. |
| Submission view | Verdicts, times and scores per test, never input, output or messages. Sample results come with the statement anyway. |
| Coarse view for students | No memory figure on hidden tests, maximum memory rounded up to 16 MB steps, times rounded (10 ms; 100 ms in graded contests). Applied on every path a student can reach: reading a submission, the workspace list, the answer to a submit or a retried key, and live events. |
| Progress while judging | Students see per-test verdicts only inside finished blocks (the samples, a whole subtask), and no running maximum time or memory, so polling shows no more than the stream. |
| Live events for students | Sent when the sample block or a whole subtask finishes, never per hidden test. |
| Exam feedback mode | Until the student's window closes, only "Submitted": no verdict, score, tests, times or compile output, on every path. Nothing the student can observe changes when judging finishes: no live submission or standings events, and no "still being judged" refusal (a new submission queues behind the pending one), since the moment judging ends depends on how long the program chose to run on hidden tests. A judge failure is shown, because it is never the program's doing and the student must resubmit. |
| Runs | Show output and the expected output, but only on samples or the student's own input. |
| Box output | Whatever a program does to its own output file (removes it, replaces it with a link or a FIFO, makes it unreadable) gives empty or cut-short output, so a verdict, never a judge error. Otherwise a program could choose, from the hidden input, which tests fail the whole job: a one-bit channel even in exam mode. |

Why coarse: a program can encode a few bits of a hidden test's values in how much memory it allocates or how long it runs, then read them back from exact figures, and repeat over many submissions until the test is known. The per-test event timing leaks the same way, since when an event arrives says how long that test took. Rounding to steps far wider than the signal, dropping per-test memory on hidden tests, and batching events per subtask close those channels while keeping what a student needs: which subtask failed, with which verdict, and roughly how close to the limits.

Owners and admins see every figure; a contest's co-organisers see the student view.

## Live updates

One `GET /api/events` stream per open tab, served by the `events` hub. The jobs service calls a change hook after every committed claim, report, close and sweep; the submissions service then reads the run or submission as its author may see it and publishes it to that user's streams.

| Piece | Design |
|---|---|
| Ordering | Updates for one job are serialised, so they arrive in order. |
| Only visible changes | An update is sent only when its signature (state, verdict, number of finished tests, or for students finished subtasks) differs from the last one sent, which also keeps a 50-test submission from producing 50 identical reads. Users with no open stream are skipped before any read. |
| Never blocks judging | Each stream has a buffer of 16 events. A stream that is full is dropped, not waited for; the browser reconnects and refetches. Every write has a 10 s deadline, so a reader that stops reading frees its goroutine. |
| Limits | 4 streams per user and 5,000 in total (then `503 busy`, and the page polls). A fifth stream replaces the oldest, which first gets `event: replaced` so that tab stops reconnecting; without it, five tabs evicted each other in an endless reconnect loop. |
| Session | Checked again on every clock tick; a stream ends with its session (sign-out, expiry, disabled account). |
| Contest events | Standings versions, announcements, clarifications and new end times go to the connected users who belong to the contest, worked out with one query per event against the hub's list of connected users. There is no topic registry: membership (roster, co-organisers, publication) is read at send time, so a change of access applies to the next event. Standings events follow the board refresh, at most about one per contest every 2 s, and go to organisers only in exam-mode contests. |
| Setter events (M5) | `wizard` goes to the admin who started an AI request when it ends. `input_generation` goes to the admin who queued a generate inputs job on each change, sent by one goroutine fed by a buffered channel from the change hook, so events keep their order and a full buffer drops an event rather than blocking judging (the editor also polls). |
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

**Security council fixes (2026-10-18).** Sessions are browser-session cookies with a fixed 12-hour server-side life and no renewal; Google sign-in accepts only a sign-in from the last 5 minutes (`max_age`; Google does not support `prompt=login`) and the server checks `auth_time`. Owners can end a person's sessions and disable accounts. Testing mode is refused through any proxy and is undone when the server next starts in another mode (the mode is stored in the `settings` table). A co-organiser whose site role is student is treated as a student whose window closes last: they see results only when students do, cannot change the schedule or rules after the start, and cannot use the levers that change results (roster, extensions, extend, pause, resume, rejudge). Every such lever, and every settings change, writes an append-only `contest_audit` row (migration 0008; triggers refuse update and delete, as for `integrity_decisions`), which the console shows and the JSON export carries. Settings and problem changes are recorded in the same transaction as the change; the other actions are recorded right after it, and a failed write is logged at error level. Validation now reports the tests the brute force did not finish and fails when fewer than half were cross-checked, the `ai draft` tag needs a real edit and a new validation to clear, and a graded contest takes an AI problem only with a person's solution or an admin's recorded reason.

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

**Only regular files cross the boundary, and links are never followed.** Every file the judge reads from a box is opened with `O_NOFOLLOW` and `O_NONBLOCK` and must be a regular file. A program that replaces its output with a symlink (say to `/proc/self/environ`, which would expose the judge's own environment) or with a FIFO (which would hang the lane) simply produces empty output. So does one that makes its output unreadable (`chmod 000` on its own stdout): any failure to open or read the box's output file is the program's output, empty or cut short where the read failed, never a judge error. Only a failure writing the judge's own copy fails the job. Artifacts are capped in total size.

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

### Blob cache

Tests, sources and custom inputs a job fetches land in `blobs/<sha[0:2]>/<sha>` in the judge's cache directory, stored only once the content hashes to the name. The cache is bounded by `PODIUM_JUDGE_BLOB_CACHE_MB` (2 GB by default):

| Rule | Value and reason |
|---|---|
| Holding | A job holds every blob it asks for, from the moment it asks (before the file is even looked at) until the job ends, so a blob can never be removed between the check and its use. |
| Eviction | After every job, if the tracked total may be over the cap, the directory is walked and the least recently used blobs no job holds are removed until the total is under 90 % of the cap, so the next few jobs do not each walk it again. A hit refreshes the file's time. |
| Cost when under the cap | None: the total is tracked from downloads and the walk only happens when it passes the cap. |
| A removed blob | Downloads again, under the lease of the job that needs it. |

### Checkers

`tokens` (default) compares whitespace-separated tokens; a single token longer than 4 MB in the student's output is a wrong answer. `exact` compares bytes after turning CRLF into LF and dropping trailing newlines, streaming both sides so the judge never holds a whole output in memory. `float` compares numeric tokens with absolute or relative error 1e-6 by default. `custom` runs a testlib checker, compiled as C++17 with a 30 s compile budget (testlib is a large header), in a fresh box like any submission: 10 s CPU, 256 MB, one process. Exit 0 is accepted; wrong answer, presentation error and partial scores are wrong answer until per-test scoring arrives in v2; `_fail`, a crash or a limit is an internal error. `testlib.h` is installed by `install.sh` at a pinned commit, checked against its SHA-256.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). Writes take turns in Go before they reach SQLite (`server/internal/db/writegate.go`): every transaction that is not read-only, and every statement outside a transaction that is not a read, waits on one in-process gate, so writers never meet SQLite's busy handler, which sleeps and polls (1 ms rising to 100 ms) while holding a pooled connection. The 700-student load test showed why: with that hand-off, four connections all sat in waiting writers, reads queued behind them, and judging stalled for 90 s (M7, measured on a desktop; the rerun on the VM is in the P0 drill notes). Reads run in parallel on the pool's other connections (8 in all). A write that waits 10 s for its turn fails with "database is busy", as `SQLITE_BUSY` did before; other processes (the sqlite3 shell, Litestream) still use the busy timeout. `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction. Litestream replicates the database to a bucket, uploading changes every 10 s, and a nightly job copies the database (SQLite's online backup) and hard-links the test files into `/var/backups/podium` (`docs/OPERATIONS.md`, "Backups"). `podium-server check` verifies a database and its files read-only: integrity and foreign key checks, migrations against the binary, and every recorded file present with its size (or, with `-hash`, its content).

Schema changes are numbered files in `server/internal/db/migrations` (`0001_identity.sql`, `0002_preferences.sql`, `0003_problems.sql`, `0004_submissions.sql`, then one per milestone), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

Before the first release, M3 edited `0003_problems.sql` in place instead of adding a table rebuild: `testcases.output_sha` became nullable (outputs may be missing), `jobs.kind` accepts `submit`, `run` and `generate`, and `job_results` gained `output` (run excerpts) and `output_sha` (generated outputs, and the hash of a submission's output). A development database created before that change must be deleted (`docs/OPERATIONS.md`, "Upgrade").

`0007_wizard.sql` (M5) adds the generator columns and `input_generations`, and widens the `jobs.kind` CHECK to accept `generate_inputs`. SQLite cannot alter a CHECK constraint, and the usual way round it, rebuilding the table, does not work here: with foreign keys on, dropping the old `jobs` table would cascade into `job_results` and delete every recorded result, and the migration runner cannot switch foreign keys off because `PRAGMA foreign_keys` is a no-op inside the transaction each migration runs in. So the migration widens the constraint in place with SQLite's documented `writable_schema` procedure: it rewrites only the text of the `CREATE TABLE jobs` statement in `sqlite_master` (every existing row already satisfies the wider check, and no page on disk changes), then `PRAGMA writable_schema = RESET` reloads the schema, and the `CREATE TABLE` that follows bumps the schema cookie so every other connection reloads it too (D52). A test upgrades a database holding jobs and results from 0006 to 0007 and checks the rows survive, every pooled connection accepts the new kind, `PRAGMA integrity_check` says `ok` and `foreign_key_check` finds nothing.

Times are Unix milliseconds. Sessions are stored by the SHA-256 of the cookie value, so a copy of the database cannot be replayed as logins.

## Unused files on the server

`PODIUM_TESTS_DIR` keeps every file a job may fetch. Tests and submitted programs are referenced from the database for as long as their rows live; reference solutions, checkers, generators and custom inputs are written again each time a job needs them and are only referenced by job payloads. Without a clean-up, every edited reference solution, replaced test and custom input would stay forever. The 12-hourly retention pass removes them (`problems.Blobs.GC`):

| Kept | Why |
|---|---|
| Any test of any problem, any submission's or run's source | still referenced |
| Anything a job that is waiting or running names, and anything a job less than 7 days old names | the judge may still fetch it; a recent run still shows its samples' expected output |
| A blob recorded in the last 24 hours, or a file with no row written in the last 24 hours | a save or an upload in progress; 24 hours is longer than any job may run |

Removal and saving cannot race: every save records its blob (or refreshes the row's time) inside the transaction that adds the reference, and checks the file is still there inside that transaction. The collection deletes each batch of rows and files inside one write transaction, and deletes a row only if its time is still old, so a blob saved again after the candidates were read is skipped, and a save that lands just after a removal writes the file back (or, for test uploads written before their transaction, fails with "try again" instead of leaving a row without a file). `testcases` also references `blobs` by foreign key, so a test can never lose its row. Job payloads are scanned as text for 64-digit hex runs, so every hash any job kind could fetch is seen whatever the payload's shape.

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
| `podium-server` (capped at 300 MB by systemd, Go heap limit 220 MB) | 50 to 80 MB |
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
| Integrity checks in a background goroutine on the server | a judge job kind | Nothing is executed, so the sandbox adds nothing, and the judge never needs every student's source (D49). |
| Containment over winnowed fingerprints, minus shared code | edit distance, or a plain token overlap | Survives renaming and reordering; dropping the reference and class-wide idioms keeps short and textbook problems from flagging everyone. |
| Case evidence copied at opening, decisions append only | cases pointing at live signals | A re-run or a later change cannot alter what a student was asked to explain or why a decision was made. |
| A client key per press of Submit | relying on the rate limit alone | A retry after a timeout returns the first submission instead of being refused or counted twice. |
| Rate gap per user, across problems | a gap per problem | A per-problem gap is defeated by spreading submits over problems. |
| Fair queue priority | first come, first served | One student resubmitting in a loop cannot push everyone else's first attempt back. |
| Compile cache keyed by language, command and source | compiling every job | Run then Submit is the common pattern, and compiles hold the scarce compile mutex. Load-dependent failures are not cached, so a busy moment never becomes a permanent CE. |
| Output generation on the judge, uploaded under lease | running the reference on the server, or accepting any hash | The server never runs untrusted code; tying uploads to the job and its budget keeps the one judge-to-test-data path closed to everything else. |
| Generators run on the judge, chained into outputs and validation | generating inputs on the server, or one button per step | Generator code is setter or model code and runs only in isolate; one press for the whole chain is what an AI draft needs to reach "Validated" with at most one edit. |
| The generator outside the content hash, a plan hash in the payload (D53) | hashing the generator with the tests | Students are judged on the tests, not the generator; a tidy-up should not unvalidate a problem, yet inputs from an older generator must never be applied. |
| AI key pasted per session, in memory only (D50) | an environment variable or a stored key | Nothing to leak from the disk, the database, backups or logs; the owner never handles a provider key, and a deployment without one loses only the wizard. |
| Structured outputs plus server-side checks | asking for JSON in the prompt | The provider enforces the shape; the server still checks every rule the schema cannot express, and the answer passes the editor's own field rules before it becomes a draft. |
| The official Anthropic Go SDK | a hand-written HTTP client | Streaming, retries, error types and beta fields are maintained upstream; it costs about 5 MB of binary and no memory at rest. |
| Refusal fallbacks on for Opus and Sonnet (D54) | failing on any refusal | Contest problems use words like "attack" and "exploit" innocently; a fallback model answers instead of the request failing. |
| Widening `jobs.kind` with `writable_schema` (D52) | rebuilding the table | A rebuild with foreign keys on would cascade into `job_results`, and migrations cannot turn foreign keys off. |
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
