# Changelog

Dated entries per release. Unreleased work is listed at the top.

## Unreleased

Becomes `v1.0.0` when the v2 P0 gate is signed.

### v2 P0, backend (2026-10-20)

- `install.sh upgrade [--force "<reason>"]`: asks the deploy guard, checks that every variable the new release's examples name exists in `/etc/podium/*.env` (names only), drains the judge, takes a backup, keeps the running release in `/usr/local/lib/podium/previous/`, installs, starts the server and waits for `/healthz`, starts the judge, verifies, and prints the backup folder and the rollback command.
- `install.sh rollback`: puts the previous binaries back; when the upgrade applied a migration, restores the pre-upgrade backup after a typed confirmation that names what is lost.
- `install.sh verify`: units, `/healthz`, judge API 401, ufw, loopback listeners, production mode, env files root 600 with no placeholder (names only), alert webhook, backup folder mode, time sync. Exits 1 on any failure.
- `podium-server deploy-check`: the deploy guard (D112). Reads the published contests from the database, so it works with the server down; refuses in the 48 hours before a contest, while it runs, and while its queue drains. `-force` records the reason in each refusing contest's audit log (`deploy_forced`).
- `tools/deploy.sh HOST` and `make deploy HOST=...`: guard, `make release`, confirmation before anything leaves the machine, `scp`, `install.sh upgrade`.
- The judge stops gracefully: the first SIGTERM stops claiming and lets running jobs finish and report; the unit waits up to 20 minutes and signals only the worker.
- Encrypted copy off the VM: each nightly backup is packed, encrypted with `age` to the owner's public key and uploaded to B2 with `rclone`; a heartbeat URL is pinged after a good run. `podium-backup` prints every variable it reads as set, default or missing.
- Alerts with nothing resident: `podium-alert@.service` on the failure of the server, judge, Litestream, backup and checker units; `podium-check.timer` every 15 minutes for free space, WAL size, Litestream running and uploading, the encrypted copy's age, and a weekly restore of the bucket's newest database checked with `podium-server check -hash`. Posts to an ntfy topic.
- Fresh installs reach the end: `caddy` runs last and skips while `PODIUM_BASE_URL` is a placeholder or DNS does not point at the VM; isolate's tuning (transparent huge pages off, core pattern, hard-link protection) survives reboots.
- `make release` on a fresh clone installs `web/node_modules`; the placeholder check reads the app's source instead of vendor chunks.
- New variables (all optional, in `/etc/podium/backup.env` and `/etc/podium/alert.env`): `PODIUM_BACKUP_AGE_RECIPIENT`, `PODIUM_BACKUP_B2_PATH`, `RCLONE_CONFIG_B2_TYPE`, `RCLONE_CONFIG_B2_ACCOUNT`, `RCLONE_CONFIG_B2_KEY`, `PODIUM_HEARTBEAT_URL`, `PODIUM_ALERT_WEBHOOK`.
- amd64 only, stated in the README and OPERATIONS, and enforced by `install.sh`.

### v2 P0, frontend (2026-10-20)

- The console's "Waiting or judging" filter works, and a new "Judge error" filter lists submissions the judge could not finish: the feed's `verdict` parameter takes `pending` (queued or being judged) and `error` as states (B2).
- On phones the standings header stays on screen while the rows scroll (B3).
- The reveal enters full screen when it opens and leaves it on exit (B4); the row climbing to its new place is drawn above the rows it passes (B5).
- The AI wizard shows its second step as done when the wizard is done (B6).
- The console's audit log names a forced deploy (`deploy_forced`, written by `podium-server deploy-check -force`) with its release and reason.
- Unpublishing (or publishing) a live contest asks first, like any other change students see at once (B7).
- Test suite: an axe-core accessibility pass over every route at 1440 and 320 px with forced-colours screenshots (`web/tests/a11y.spec.ts`, new dev dependency `@axe-core/playwright` 4.13.0), the student workspace and contest screens at more sizes and with touch emulation, pixel comparisons of the student screens (`web/tests/baseline.spec.ts`, references kept outside git), more contrast pairs in the theme test, and the specs' mocks moved to `web/tests/mocks/`.
- Docs: the console feed's `verdict` values in `docs/API.md`; unmeasured judge capacity figures removed from `docs/RUNBOOK.md` until the VM is measured.

### Security council fixes (2026-10-18)

- Co-organisers whose site role is student can no longer change a contest's schedule or rules once it starts, replace the roster, give extra time, extend, pause, resume or rejudge. They see results only when students do (no exam-mode verdicts, the frozen board during the freeze, no stats or exports until then), including on their own submissions. Extensions go only to students on the roster or taking part.
- Testing mode refuses requests from anywhere but this machine's browser (any proxy header or a public `Host`), test sign-in needs the page's own `Origin`, and starting in another mode ends every session and demotes organisers no Google account vouches for.
- Sessions end when the browser closes and at most 12 hours after sign-in; Google sign-in accepts only a sign-in from the last 5 minutes, so a lab PC's previous Google session is not one click away. Owners can end someone's sessions and disable or re-enable accounts from the People page.
- New append-only contest audit log (migration 0008) for settings, problems, roster, extensions, extend, pause, resume, rejudges, the reveal, the editorial and co-organisers; shown in the console, included in the JSON export, `GET /api/admin/contests/{id}/audit`.
- Validation reports tests the brute force did not finish and fails with fewer than half cross-checked. The `ai draft` tag stays until a person changes the statement or a solution (which withdraws the validation). Graded contests take an AI problem only with a person-written solution or an admin's reason, recorded in the audit log. New setter checklist in `docs/AUTHORING.md`.
- A student whose program takes the whole compile time limit can no longer hold the judge's compile lock by pressing Run in a loop: the judge caches that compile error for 10 minutes, and the server refuses that student's Run and Submit for 60 seconds. Judge compile reports carry `timed_out`, accepted only on a CE.

### Milestone M7: hardening (2026-10-18)

- Load test in `tools/loadtest/`: a k6 script that plays a whole contest per student (testing-mode sign-in, an event stream held for the session, workspaces, drafts every 10 to 20 s, standings, integrity batches, runs and submissions with an end-of-contest spike, retries after 429), fixed and window start modes, per-route latency, 429 counts, event stream reconnects and the judge queue every 5 s; a seed script (imports and validates the example packs, creates the students) and a summary script (queue CSV, peak, throughput, drain time). Results: the M7 desktop numbers are in the next item; that note was not kept, and the VM rerun goes in `cache/v2-plan/drills/` (not shipped).
- SQLite writes now take turns in Go before they reach SQLite (`server/internal/db/writegate.go`), and the pool has 8 connections instead of 4. At 700 students the busy handler's sleep-and-poll hand-off between writers had filled every connection: API p95 586 ms, judging stalled for 90 s and every event stream was dropped once. With the gate, the same load gives p95 10.3 ms and no dropped streams. A write that waits 10 s fails with "database is busy", as before.
- `GET /api/admin/metrics` (owners and admins): queue depth and leased jobs by kind, the oldest waiting job, jobs closed in the last minute and five minutes, submission wait and verdict percentiles over ten minutes, failures, event streams, SQLite pool use and waits, server memory. Documented in `docs/OPERATIONS.md`, "Metrics", and `docs/API.md`.
- Backups: Litestream 0.5.16 (pinned, checksum-verified by `install.sh litestream`) streams the database to an S3-compatible bucket, Backblaze B2 by default, uploading every 10 s and keeping 14 days; credentials and bucket in `/etc/podium/litestream.env` (template `deploy/litestream.env.example`), checked at start-up by the unit. A nightly `podium-backup.timer` copies the database with SQLite's online backup and hard-links the test files into `/var/backups/podium/<date>`, checks the copy, and keeps 14 days.
- `podium-server check [-db F] [-tests D] [-hash]`: a read-only check of a database and its test files (integrity and foreign key checks, migrations against the binary, row counts, every recorded file present with its size or content). Used by the nightly backup and by `podium-restore` (`tools/restore.sh` in a checkout), which restores a nightly backup or the bucket (newest, or any moment in 14 days) into a scratch directory and checks it. Restore rehearsed against a local replica in M7 (that note was not kept); the drill onto a blank VM goes in `cache/v2-plan/drills/` (not shipped).
- The judge's blob cache is bounded: `PODIUM_JUDGE_BLOB_CACHE_MB` (new, optional, default 2048) caps `blobs/`; after each job the least recently used files no running job holds are removed down to 90 % of the cap. The compile cache keeps its 500 MB cap.
- The server removes test files and sources nothing refers to any more in its 12-hourly retention pass: not a test, not a submission's or run's source, not named by a job that is waiting, running or under a week old, and not saved in the last day. Saving a blob now refreshes its row and checks the file inside the same transaction, so a removal and a save cannot leave a row without its file.
- `docs/RUNBOOK.md`: contest-day checklists, what to watch in the metrics and when to act, pausing, extending, rejudging after a bad test, a dead judge, a server restart, locked-out students, restoring from a backup, and the dry run with real students with a template for the list of fixes.
- Security council items S3, S7 and S8 (the parts outside the identity and contest code): backups are created with umask 077 in a 700 folder, and `/etc/podium/backup.env` (optional, template `deploy/backup.env.example`) can copy each night's backup off the VM with rsync over ssh (`PODIUM_BACKUP_RSYNC`, `PODIUM_BACKUP_KEEP_DAYS`). The server unit sets `GOMEMLIMIT=220MiB` under its 300 MB cap. A request must arrive whole within 15 s (was 120 s); pack import, test replacement and roster upload extend their own deadline to 120 s. Each browser session may hold 8 open API requests at once (event streams aside), then `429`. Caddy refuses bodies over 33 MB and its admin API is off (`install.sh caddy` restarts Caddy instead of reloading). New `install.sh firewall` step: ufw with only ssh, 80 and 443 inbound, and a warning while ssh accepts passwords. Production refuses a `PODIUM_LISTEN` that is not loopback. `docs/OPERATIONS.md` documents how `X-Forwarded-For` is trusted and how to restrict a remote judge's tunnel key to port 8081.
- `tools/dev-stack.sh` takes `PODIUM_LISTEN`, `PODIUM_JUDGE_LISTEN`, `PODIUM_LOG_LEVEL` and `PODIUM_DEV_CPUS` (pins both services to some CPUs, to approximate the VM). `install.sh packages` adds `jq`.

### Milestone M6: design pass (2026-10-18)

- The web app was audited against the web interface guidelines, the accessibility rules and design.md, and fixed where it fell short.
- Live regions announce from every layout; focus moves somewhere sensible when the focused button disables itself or disappears (the reveal's Next, Back and Start over among them); errors that appear without a field take focus; the editor's focus ring is drawn in the keyword colour so it meets 3:1 on every editor well.
- Copy made consistent: "Standings" and "Problem bank" everywhere, plain words on student screens.
- Keyboard shortcuts: `?` opens a list of every shortcut from anywhere except while typing; `[` and `]` open the previous and next problem in a contest, also not while typing (both are common in code). A "Keyboard shortcuts" link in the footer.
- Keyboard-only run-throughs in Playwright: a student starts a contest, solves, submits, reads the verdict, asks a question and checks the standings; an organiser edits a contest, runs the console and walks the reveal (`web/tests/keyboard.ts`).

### Milestone M5: AI wizard (2026-10-18)

- AI wizard (server): "New problem with AI" writes a whole stdin/stdout problem from a brief (topic, difficulty, constraints, subtask ideas) or from pasted text of an existing problem (at most 20,000 characters, rewritten in the model's own words): statement, samples, subtasks, C++ reference, Python brute force, generator and validator, checker and limits. The answer becomes a draft with origin `ai` and the tag "ai draft", and input generation is queued at once, which chains output generation and validation.
- Bring your own key (decision D50): an admin pastes an Anthropic key per session; it is checked with a model lookup that costs no tokens and held in server memory only, for 2 hours, forgotten on sign-out and restart. It never reaches the database, logs, errors, job payloads or the judge; an end-to-end test searches the log output, the SQLite files and the blob store for it after a full run. No new environment variable.
- Anthropic client on the official Go SDK (`anthropic-sdk-go` v1.78.0): streamed requests, structured outputs with a JSON schema for the whole problem, effort `high`, Claude Opus 5.5 by default (Sonnet 5.5 and Haiku 5.5 on offer), server-side refusal fallbacks for Opus and Sonnet (D54). Answers are checked against the wizard's rules and the editor's field rules and retried once with the reasons; refusals, cut-off answers, rate limits and outages are reported in words. The pasted text travels as delimited data and is never treated as instructions.
- "Can a model solve this?": sends the statement alone and judges the model's C++ answer as the admin's own submission.
- Tests from a generator: problems gain a generator and a plan of tests (subtask, seed, arguments); packs gain `generator.py` and `tests.yaml`, and subtasks `generator_args`. New job kind `generate_inputs` (priority 85): the judge runs the generator in isolate once per planned test and uploads each input under the M3 upload rules; on success, outputs are generated and then the problem is validated. Failures name the test, subtask and seed. The generator is not part of the content hash; a plan hash in the job stops stale inputs (D53).
- No AI draft joins a contest until a person has saved it, even when the chain validated it.
- New routes: `GET`, `PUT` and `DELETE /api/admin/ai/key`, `POST /api/admin/ai/wizard`, `GET /api/admin/ai/wizard/{id}`, `POST /api/admin/problems/{id}/solve`, `POST /api/admin/problems/{id}/generate-inputs`, `GET /api/admin/problems/{id}/input-generation`; the problem JSON gains `generator`, `plan` and `subtasks[].generator_args`; new events `wizard` and `input_generation`. One AI request per admin at a time, 3 minutes each.
- Testing mode offers a fake provider with two canned problems, so the whole pipeline runs without a key or a network.
- Migration `0007_wizard.sql`: generator columns, `input_generations`, and `generate_inputs` added to the `jobs.kind` CHECK in place with SQLite's `writable_schema` procedure, since a table rebuild would cascade into `job_results` (D52). Take a backup before upgrading.

### Integrity checks (2026-10-18)

- Integrity checks after a contest (decisions D47 and D49), run by owners, admins and co-organisers who are not students, once the contest is over for everyone. Four signals: similar code (per-language lexers, winnowing, containment with the reference, brute force and class-wide idioms subtracted, programs under 40 tokens skipped, pairs at 0.6 or more and clusters), the same wrong output on hidden tests (across languages), hard-coded expected outputs, and probing. Strength in words with the raw number beside it; no AI-likelihood score and nothing automatic. Migration `0006_integrity.sql`.
- The check runs in one background goroutine on the server, not on the judge: nothing is executed, and the judge never holds every source. One check at a time per server; a check left running by a restart is marked failed at start-up.
- Organiser routes under `/api/admin/contests/{id}/integrity` (run, latest run with `users` and `problems` maps, side-by-side pair with matched spans, a student's timeline, cases) and `/api/admin/integrity/cases/{case}` (read, change state with a note). Refusals: `409 contest_live` before every window has closed, `409 busy` while a check runs. Ids from another contest are `404`.
- Review cases: opened from signals with a reason of at least 20 characters, evidence copied at opening so a re-run cannot change it, shared with the students before any action, cleared or actioned with a reason. Every change is an append-only decision, and opens and decisions are logged by id.
- Students: `GET /api/integrity/cases` lists cases shared with them, with only their own evidence and code (never another student's name or program, nor the reviewers' notes), and `PUT /api/integrity/cases/{case}/explanation` until the decision.
- Retention: the 180-day job skips students in an open or awaiting_student case for that contest; a closed case's evidence and explanations are emptied 2 years after its decision, and its decision log is kept.
- Review fixes: Hard-coded answers no longer flag fixed words and constants the problem names (IMPOSSIBLE, 1000000000) and run in one pass per distinct program. Probing ignores compile errors and honest step-by-step fixes. Similar-code strength now needs enough shared code, the common-code limit is counted per language, and the pages show how much code is behind a score. A case's compare view shows its own evidence, and signal ids are never reused. Decisions follow the students' turn: action waits until each student explained after the latest share or had 7 days, a reopen needs a new share, and a decision is refused if an explanation changed since the page loaded; every explanation version is kept. The decision log names reviewers and the database refuses to edit it. Checks and signals are deleted 180 days after the contest. Student co-organisers no longer receive classmates' typed, pasted and focus counts in the console feed. `#define` bodies and strings passed to Python `exec` or `eval` are compared as code. Migration 0006 was edited in place (unreleased): recreate development databases.

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
