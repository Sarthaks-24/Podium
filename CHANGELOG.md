# Changelog

Dated entries per release. Unreleased work is listed at the top.

## Unreleased

### Integrity checks (2026-10-18)

- Integrity checks after a contest (decisions D47 and D49), run by owners, admins and co-organisers who are not students, once the contest is over for everyone. Four signals: similar code (per-language lexers, winnowing, containment with the reference, brute force and class-wide idioms subtracted, programs under 40 tokens skipped, pairs at 0.6 or more and clusters), the same wrong output on hidden tests (across languages), hard-coded expected outputs, and probing. Strength in words with the raw number beside it; no AI-likelihood score and nothing automatic. Migration `0006_integrity.sql`.
- The check runs in one background goroutine on the server, not on the judge: nothing is executed, and the judge never holds every source. One check at a time per server; a check left running by a restart is marked failed at start-up.
- Organiser routes under `/api/admin/contests/{id}/integrity` (run, latest run with `users` and `problems` maps, side-by-side pair with matched spans, a student's timeline, cases) and `/api/admin/integrity/cases/{case}` (read, change state with a note). Refusals: `409 contest_live` before every window has closed, `409 busy` while a check runs. Ids from another contest are `404`.
- Review cases: opened from signals with a reason of at least 20 characters, evidence copied at opening so a re-run cannot change it, shared with the students before any action, cleared or actioned with a reason. Every change is an append-only decision, and opens and decisions are logged by id.
- Students: `GET /api/integrity/cases` lists cases shared with them, with only their own evidence and code (never another student's name or program, nor the reviewers' notes), and `PUT /api/integrity/cases/{case}/explanation` until the decision.
- Retention: the 180-day job skips students in an open or awaiting_student case for that contest; a closed case's evidence and explanations are emptied 2 years after its decision, and its decision log is kept.

### Milestone M4: contests (2026-10-18)

- Contests (server): fixed start or a flexible window per student, freeze, best-per-subtask or best-submission scoring, domain or roster access, subtask or exam feedback, graded flag, practice after the end, per-contest gap between submits, submissions per problem and languages, honour code with recorded acceptance, editorial. Migration `0005_contests.sql`.
- Problems are pinned to the validated content hash when added; a problem edited since is refused for publishing, running, submitting and rejudging until it is validated and added again.
- Student routes under `/api/contests/`: the list (live, upcoming, past), the contest page, Start (accepts the honour code; starts the clock in window mode), the contest workspace (the M3 shapes plus label, window and why Run or Submit is off), standings and clarifications. Every answer carries `server_now`; every time rule uses the server's clock.
- Standings computed from submissions and cached in memory per contest and variant, recomputed after any finished submission, rejudge, pin change or extension. Frozen board with "+N" pending cells; students see it from the freeze until every window has closed (window mode and exam feedback: no board until their own window closes), with an optional hold until an organiser reveals. Names on the board come from the roster or the account, never an email.
- Organiser routes under `/api/admin/contests/`: create, edit, archive, clone, problems, roster (CSV or JSON), co-organisers, personal extensions, extend for everyone, pause and resume judging, live feed, rejudge (a problem or one submission, priority 50), announcements, clarifications (answer, make public), standings, reveal (frozen and full boards), CSV and JSON export (spreadsheet formulas neutralised), per-problem stats, editorial. Co-organisers manage only their contests; everyone else gets 404.
- Live contest events on `GET /api/events`: `standings` (version only, sent once the new board is ready, at most about every 2 s), `announcement`, `clarification` (to the asker unless public) and `contest` (new end time), sent to the contest's connected members.
- Judge queue: jobs of a paused contest wait at claim; while a graded contest is live, at most one validation or generation job is leased at a time.
- Integrity (D47): a second event stream from another browser during a live contest is recorded; a retention job deletes integrity events and clears per-submission integrity counts 180 days after a contest ended.
- Security review fixes (`m3-security-2`): students get the coarse, contest-aware view on every path (submission reads, the workspace list, a repeated client key, live events), times rounded to 100 ms in graded contests, exam mode hiding results everywhere, and per-test progress hidden inside unfinished subtasks; client keys are scoped per contest; run rows no longer store code or input inline and the run checks read only a covering index; drafts (one per 2 s) and integrity batches (one per 5 s) are rate limited and events are validated before the write lock; a replaced event stream gets a final `replaced` event; at least every other claim goes to a submission when runs are queued; a program that makes its own output unreadable gets empty output instead of failing the job; upload slots are reserved atomically; event streams close when their session ends.

- M4 security review fixes (`m4-security`): standings recompute once per contest and variant at a time, at most every 2 s, from an index-only read of a new per-submission `earned` column (300 students: 34 ms, was 0.87 s), with the last board served meanwhile and the event sent once the new board is ready; only owners and admins choose a contest's problems or publish it, and co-organisers get the student view and the contest's limits; exam mode never refuses or notifies in a way that depends on judging; rejudges replace submissions still waiting on old tests, never double up, and a replaced job cannot overwrite a result; the fixed-start freeze ignores personal extensions; judge failures show in exam mode and do not count toward the per-problem limit; editorial and practice wait for every window; boards score against the subtask points pinned with each problem; the clarification cap is atomic; heavy reads are rate limited per user and the contest list loads in two queries; judge uploads stream to disk and unused uploads are deleted.
- Contest page problems carry `my_submission_count`; replacing a roster answers with how many were added and removed.

### Milestone M3: submissions (2026-10-18)

- Student workspace: statement with copyable samples, CodeMirror 6 editor in C++, Java, Python and JavaScript, Run on samples or on a custom input, Submit, and a verdict panel with a subtask strip that fills as tests are judged, per-test rows, queue position and compile errors verbatim. Three columns from 1024 px, tabs below. Ctrl+Enter submits, Ctrl+' runs.
- Until contests arrive in M4, organisers reach it with "Try as a student" in the problem editor and "Try" in the bank (`/admin/problems/{id}/try`); the bank is never shown to students.
- Drafts saved to the server every 10 s and on blur, mirrored in the browser; a refresh restores the newer copy.
- New job kinds `run`, `submit` and `generate` on the M2 judge protocol, with priorities 10, 20 to 30 (a fair queue that puts a first attempt ahead of a tenth) and 85.
- Scoring on the server from per-test results: samples first, all-or-nothing subtasks, judging stops at a failing sample and at the first failure in a subtask (the rest `SK`), unreported tests `IE`, an empty subtask earns nothing. A job the judge cannot finish leaves the submission in `error`, not counted.
- Live updates over server-sent events (`GET /api/events`): `submission`, `run` and a `clock` event every 25 s; 4 streams per user, 5,000 in total, slow clients dropped; the page falls back to polling every 15 s. No Caddy change needed.
- Hidden tests stay hidden (decision D47): submit reports carrying output text, or checker messages on hidden tests, are refused; live events for students are coarse (no per-test memory on hidden tests, memory in 16 MB steps, rounded times) and sent per subtask.
- Rate limits: one pending submission per user per problem, 5 s between an organiser's submits across problems, one pending run per user and 2 s between runs, 64 KB of code and of input. Refusals say when to try again. A `client_key` per press of Submit makes retries safe: a repeat returns the first submission with 200.
- Run and Submit refuse a problem whose structure is incomplete or that lacks expected outputs (`409 not_ready`, listing what to fix). Validate and Generate outputs now refuse with the same `409 not_ready`; M2 answered `400`.
- Integrity counts (D47): typed and pasted characters and focus losses per submission, paste, blur and focus events in batches, and an HMAC of IP and user agent keyed by the session secret. Counts only, never keystroke timing; nothing is scored or flagged.
- Output generation: packs and test zips may leave out `.out` files; "Generate outputs" fills missing ones from the reference solution on the judge and "Regenerate all outputs" replaces them after a confirmation. Outputs come back through `PUT /api/judge/blob/{sha}`, accepted only for a generate job under its lease, within a budget of one per input plus 2, hash-checked, at most 16 MB; a report may name only outputs that job uploaded, and nothing is applied if the problem changed meanwhile.
- Compile cache on the judge (`compiled/` in the cache directory): keyed by language, compile command and source, 500 MB, least recently used evicted after each job, entries in use never evicted, compile timeouts and compiler out-of-memory never cached.
- Migration `0004_submissions.sql` (submissions, drafts, generations, integrity events). `0003_problems.sql` was changed before the first release (nullable `output_sha`, new job kinds, output columns in `job_results`): delete any development database created earlier (`data/podium.db`, or `/var/tmp/podium-dev` for `make dev-test`).
- New routes under `/api/workspace/`, `/api/runs/`, `/api/submissions/`, `/api/events`, `/api/admin/problems/{id}/generate` and `/generation`, and `PUT /api/judge/blob/{sha}`.
- `make sandbox-test` also runs the M3 acceptance test: AC, WA, TLE, MLE and CE submissions in all four languages, a run on samples, and outputs generated for a pack shipped without them, then validated.

### Milestone M2: problems (2026-10-18)

- Problem bank for admins and owners: list, create, edit, archive; statement editor with a live preview (Markdown, KaTeX maths, sanitised with DOMPurify).
- Problem packs: import and export of `problem.yaml`, `statement.md`, sources, an optional testlib `checker.cpp` and `tests/NN.in`/`NN.out`; strict YAML; a refused pack lists everything wrong with it at once. Packs must contain their outputs (output generation arrives in M3).
- Pack import treated as untrusted parsing: 32 MB zipped, 128 MB counted on decompressed bytes, 2,000 entries read from the zip's end record before decoding, 16 MB per file, unsafe paths and links refused, one pack operation at a time.
- Validation on the real judge: compiles and runs the reference, an optional brute force, an optional input validator and the checker on every test in isolate; fails when the reference is not accepted everywhere, the brute force disagrees, the validator rejects an input, or the reference uses more than half its time limit. The report names each failing test with program and verdict.
- "Validated" is bound to a content hash of everything validation depends on; statement and title edits keep it, any other change clears it, and a failing re-run of the same content withdraws it.
- Custom checkers with testlib: `install.sh` step `testlib` installs `testlib.h` at a pinned commit, checked by SHA-256; checkers compile with a 30 s budget and run in a fresh box.
- Judge protocol (brought forward from M3): one `jobs` queue in SQLite, `job_results`, 60 s leases renewed by reports, three attempts, per-job deadlines, blobs served only to the lease holder and re-hashed by the judge, summary computed by the server. Served on a separate loopback-only listener, `PODIUM_JUDGE_LISTEN`, with a bearer token.
- `podium-judge work` runs the worker loop; the systemd unit now starts it.
- Migration `0003_problems.sql`; new routes under `/api/admin/problems` and `/api/judge/`.
- `make sandbox-test` also runs the M2 acceptance test: the three example packs in `problems/` validate on real isolate, and a broken output fails naming the test.
- New variables: server `PODIUM_JUDGE_LISTEN`, `PODIUM_JUDGE_TOKEN`; judge `PODIUM_SERVER_URL`, `PODIUM_JUDGE_TOKEN`.

### Run modes (2026-10-18)

- `PODIUM_MODE` for both services: `testing`, `dev` or `production` (the default).
- Testing mode needs no keys: no Google client, a sign-in form for any email and role (`POST /api/auth/test-signin`), a random session secret, the public testing judge token, loopback only, and a banner on every page.
- Dev mode: Google sign-in over http, judge testing token allowed. Production: every value required, https base URL, testing token refused.
- `GET /api/auth/config` now returns `mode`.
- `make dev-test` runs server, judge and web app in testing mode (`tools/dev-stack.sh`, state in `/var/tmp/podium-dev`); `npm run test:live` runs browser tests against it with nothing mocked.

### Themes (2026-10-18)

- Ten colour themes plus System, in Light, Dark and Accessible groups, chosen from a menu in the header with a swatch for each.
- Colour-blind safe verdict colours (teal, purple, orange) as a separate setting that works with every theme.
- Preferences belong to the account (`PUT /api/me/preferences`, migration `0002_preferences.sql`) and are cleared from the browser on sign-out.
- Every theme is tested for text contrast, control borders at 3:1, verdict contrast, and the distance between the action colour and the verdicts; a browser test checks the CSS cascade gives the tested values.
- Fixed in the existing themes: light "accepted" and "partial" colours and control borders in both themes were below the contrast floor.

### Milestone M1: identity (2026-10-17)

- Google sign-in run by the server: PKCE, nonce, signed single-use state cookie, ID token signature and claims verified with the standard library.
- Email-domain check on the server; organisers and the configured owner may sign in from any domain; refusals explain what to do.
- Sessions stored by token hash, 30 days with renewal on use, `__Host-` and `Secure` cookies on https, hourly sweep of expired sessions.
- Roles owner, admin and student, read from the database on every request; `PODIUM_OWNER_EMAIL` is a permanent owner and the recovery path.
- Owner People page: add and remove organisers, manage allowed domains; the last owner cannot be removed.
- Cross-site check on state-changing API requests.
- New server variables: `PODIUM_OWNER_EMAIL`, `PODIUM_ALLOWED_DOMAINS`, `PODIUM_SESSION_SECRET`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`.

### Milestone M0: foundations (2026-10-17)

- Go module with `podium-server` and `podium-judge`, cross-compiled for Linux with `CGO_ENABLED=0`.
- Start-up validator for environment variables; `server/.env.example` and `judge/.env.example`.
- SQLite in WAL mode with numbered migrations; first migration holds users, allowed domains and sessions.
- HTTP server with `/healthz`, JSON errors, security headers, and the embedded web app.
- Judge sandbox driver on isolate 2.7: fresh box per compile and per test on a tmpfs box root, link-safe reads of box files, a memory budget across lanes, verdict classification with size-based output limits, streaming `tokens`, `exact` and `float` checkers, language table for C++17, Java 21, Python 3 and Node 22.
- Hostile-program suite (16 programs) and `podium-judge calibrate`; all contained in the local WSL2 run.
- Svelte 5 web scaffold with design tokens, light and dark themes, self-hosted Schibsted Grotesk and Fragment Mono.
- `deploy/install.sh`, systemd units, Caddyfile, Makefile and CI.
