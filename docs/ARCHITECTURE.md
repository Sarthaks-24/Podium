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

Version labels in this file (v1.1.0 to v1.5.0) name the v2 phases P1 to P5 they were planned as; nothing is tagged yet, and the phases are proposed to ship as one release (D236, the owner's decision). "Decisions of the v2 plan" near the end lists what each v2 decision means for the people who use podium.

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
| `webui` | the embedded web app, and the head, body blocks, 404 status, robots, sitemap and JSON-LD of public pages |
| `auth` | Google sign-in, sessions, roles, testing-mode sign-in; the site audit, site settings, account lookup, the account side of the rollover and erasure |
| `problems` | problem bank, pack format, content hash, validation rules and summary, output generation, generators and the generate inputs chain, AI drafts, revisions and their line diff |
| `submissions` | runs, submissions, drafts, rate limits, scoring, what each viewer may see, integrity counts |
| `events` | the server-sent event hub: streams per user, fan-out, slow clients dropped |
| `jobs` | the one queue of judge work: leases, results, uploads, sweeps, change notifications |
| `judgeapi` | request and response types shared by server and judge |
| `judge` | sandbox driver, verdicts, built-in and testlib checkers |
| `judge/calib` | host calibration and the hostile-program suite |
| `worker` | the judge's claim loop, blob cache, compile cache, and the validate, run, submit, generate and generate_inputs jobs |
| `contests` | schedules, windows, access, standings, clarifications, export, integrity retention; the contest audit, slug history, pins by revision, queue health, board adjustments, per-student summaries and the term archive |
| `integrity` | integrity checks after a contest (lexers, winnowing, signals), the review cases and their retention; manual review and notes, outcomes and the two-person rule, appeals, clocks, record views, the rating and Standing band |
| `notifications` | each person's inbox: notices stored as a kind and ids, written as sentences at read time, held during the reader's live contest |
| `retention` | how long each kind of data is kept (`periods.go`) and the daily job that deletes what is older |
| `contact` | the signed-in contact form's messages and the organisers' inbox |
| `ai` | the AI wizard's model side: the in-memory key store, the Anthropic, OpenAI and Gemini clients and the testing-mode fake, the problem schema and prompts, answer checks |
| `buildinfo` | what a build is: version, commit, date (set by `make release`), schema and judge protocol, for `-version` and the release `MANIFEST` |

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
| Deadlines | Set at the first claim from the job's size (for validation: 4 min plus 3 min and six times the time limit per test, at most 2 h), per attempt: a worker that keeps renewing cannot hold a lane forever, and a job requeued after a judge crash gets a fresh deadline at its next claim instead of failing at once (council C1-050). |
| Blob scoping | A blob is served only with a current lease on a job that needs that file. A leaked token alone does not expose the problem bank. The judge re-hashes every download. |
| The server decides | Every report is checked against the job's payload (known tests and roles, possible numbers, sizes). The final verdict on a problem is computed by the server from the stored results, never taken from the judge. |
| Loopback listener | The judge API is a second listener, `PODIUM_JUDGE_LISTEN`, which must be a loopback address and is never proxied by Caddy. A judge on another machine reaches it through an SSH or WireGuard tunnel. Bearer token of at least 32 characters, compared in constant time on SHA-256 digests so the comparison does not leak its length. |

Alternatives rejected: the server running isolate itself (breaks the rule that the server never runs untrusted code; setter and AI-written code is untrusted); an offline `podium-judge validate` over SSH (co-teachers have no shell, and the server could not verify an uploaded result); deferring validation to M3 (M2's acceptance test could not be verified). Judge workers on lab PCs were also rejected: the token reports verdicts and never goes on machines students can reach.

## Versions, toolchains and the judge protocol

**What a build is (B1.1, B1.2).** Both binaries print five lines with `-version`: the release, the commit, the build date, the schema (the highest migration it carries) and the judge protocol. `make release` writes the same facts into `MANIFEST` in the tarball, with the list of migrations and the oldest release this one upgrades from, and a `SHA256SUMS` of every file. `install.sh` checks the sums before installing, and `upgrade` refuses an installed release older than `upgrades_from`, and a downgrade (rollback is the way back). `/healthz` adds the version and schema only for a caller on the VM itself: through Caddy the release stays private, so a scanner cannot match it against known bugs.

**Judge protocol (B1.3).** `judgeapi.ProtocolVersion` (2 since v1.1.0, 3 since v1.2.0 for the revealed test, and for open tests until v3 removed them; a claim without the field is 1) travels on every claim and toolchain report. The server answers any other version with `426` and "judge protocol 3 needed, this judge speaks 2: install the same release on both". The judge stops on that with exit status 3, which its unit does not restart, because no retry can fix it; a judge newer than the server reads the old server's 400 (unknown fields) or 404 (no toolchain route) the same way. Without this, a half-done upgrade would leave a judge looping on claims it cannot read, and every submission queued. A refused token (401) is treated the same way after five in a row: the judge exits with status 4, also not restarted, so a token mismatch fails the unit and alerts instead of retrying silently (council C1-048).

**Toolchain report (B1.4).** At start the judge reports the first line of `g++ --version`, `javac -version`, `python3 --version`, `node --version` and `isolate --version`, by the same paths the language table runs, and the kernel release. The server checks the report, logs the versions and answers with their hash. v3 removed the registry that stored each report and flagged problems validated on another compiler (V3-4): one college VM changes compilers only when the owner upgrades, and `install.sh hold` stops apt from moving the compilers, runtimes and isolate during a contest season. The judge protocol is unchanged (V3-9).

**Pack format (B1.7).** `problem.yaml` carries `format: 1`, or `format: 2` (v1.2.0, D115, which added open tests; v3 imports them as samples and exports format 1); a missing line means 1 and a higher number is refused with both versions named, before its unknown fields are listed, so a pack from a newer Podium fails with the cause rather than a list of symptoms.

**Sandbox proof on the host (B1.9).** `podium-judge selftest` runs the hostile-program suite and a hello world per language in box 80 and prints PASS or FAIL per program; it is the sandbox tests shipped in the binary, for the VM where there is no Go toolchain. CI runs the same suite and `selftest` on GitHub's ubuntu-24.04 runner, with isolate installed by `install.sh`'s own steps.

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

The `ai` package (M5, three providers since v1.1.0, D111) is the model side: the key store, the provider clients, the problem schema and prompts, and the checks that turn a model's answer into a draft. `httpapi` holds the routes and the in-memory wizard runs; `problems` turns a checked answer into a draft and runs the generator chain.

```
admin picks a provider, pastes key --> PUT /api/admin/ai/key --> ai.NewSecret at once
    no model yet: ListModels (the check, and the picker's list)   with a model: Check (model lookup)   neither costs tokens
    --> ai.Keys (memory, per session and provider, 2 h)
admin: brief or text --> POST /api/admin/ai/wizard --> slot (one per admin) --> goroutine, 3 min:
    ai.Generate: one structured call, checks, at most one retry
    --> problems.CreateDraft (origin ai, tag "ai draft") --> RequestInputGeneration
    --> generate_inputs --> generate (then_validate) --> validate          (all on the judge, in isolate)
```

**Three providers, one interface (D111).** `ai.Provider` has `Generate`, `Solve`, `Check` and `ListModels`. Anthropic stays on the official Go SDK; OpenAI (the Responses API) and Google Gemini (`generateContent`) are about 250 lines each of `net/http` and the standard library, because their SDKs would add megabytes and dependencies to a 2 GB VM for three HTTP calls. Both new clients share one helper: a JSON request with a bounded answer, two retries on 5xx (and on OpenAI's plain rate limit, not on a used-up quota), no redirects followed (the key is a header and must not travel to another host), and errors that never carry the body or the URL.

| | Anthropic | OpenAI | Gemini |
|---|---|---|---|
| Key header | `x-api-key` (SDK) | `Authorization: Bearer` | `x-goog-api-key`, never in the URL |
| Structured output | `output_config.format`, JSON schema | `text.format` `json_schema`, `strict: true` (every property required, `additionalProperties: false` on every object) | `responseSchema`, an OpenAPI subset: types upper case, no `additionalProperties`, every property required and ordered with `propertyOrdering` |
| Model filter (no ids in code) | `capabilities`: structured outputs, adaptive thinking, effort `high` | id family: `gpt-*` and `o<digit>*`, without pre-structured-output and audio, image, embedding, search, moderation models | `supportedGenerationMethods` has `generateContent`, without embedding, image, speech, live, Gemma and AQA ids |
| Refusal | `stop_reason` `refusal` with its category; server-side fallback where offered | a `refusal` content item, or `incomplete` for `content_filter` | `promptFeedback.blockReason`, or `finishReason` `SAFETY`, `PROHIBITED_CONTENT` and the like |
| Cut off | `max_tokens` | `incomplete`, `max_output_tokens` | `MAX_TOKENS` |
| Not stored by the provider | | `store: false` | |

The wizard's schema is written once as JSON schema (`PackSchema`) and translated per provider. Each provider's failures map to one sentence each for a refused key, a used-up quota or credit, an unknown model and no access, in the provider's own words (`docs/API.md`, "The key form"). A model id is free text the admin types, checked against `^[A-Za-z0-9._:-]+$` so it cannot change the path of a request, and proved by the provider's own model lookup; the filtered list is only a hint, because providers name new models faster than any list in code. The list is cached in the session's key entry, so the picker costs one listing per key.

**The Anthropic client (R16, checked 2026-10-20 with the `claude-api` skill).** Requests stream with `max_tokens` 64,000, send `thinking: {type: "adaptive", display: "omitted"}` explicitly (leaving it out means no thinking on some models the list offers, such as Opus 4.8) and effort `high` (Opus 5.5 and Haiku 5.5 default to medium). No deprecated parameter is sent: no `budget_tokens`, sampling parameters or `output_format`, and no assistant prefill (the retry ends with a user turn). The fixed system prompt, which carries the pack format rules, has a cache breakpoint (`cache_control: ephemeral`); a prompt below the model's minimum cacheable length silently does not cache, which costs nothing. Billing errors (402, or a 400 about the credit balance) are the no-credit sentence. The owner's answer to input 27 is applied: no default model; the admin chooses from the live list or types an id.

**The key never touches disk (D50).** The key is pasted by an admin, never configured: there is no environment variable, so the owner never has to put a provider key on the VM, and an organisation with no key loses nothing but the wizard. On the key route the request body is decoded into a local string, wrapped in `ai.Secret` and the string cleared before anything else runs. `Secret` prints, formats, marshals to JSON or text and logs (`slog.LogValuer`) as `[redacted]`; only the `ai` package can read the value, to hand it to the Anthropic SDK per request with `option.WithAPIKey`, with the SDK's environment lookup switched off (`option.WithoutEnvironmentDefaults`), or to set the one header of an OpenAI or Gemini request. Keys sit in a map keyed by the SHA-256 of the session cookie and then by provider, for 2 hours each; a sweep every 10 minutes drops expired ones, signing out (or signing in again in the same browser) drops the session's key, and a restart drops them all. Provider errors are mapped to fixed sentences: the SDK's error holds the request, whose headers carry the key, and the raw response body, so it is never wrapped, formatted or logged. Job payloads hold only the generator, the plan and hashes. Request bodies are never logged. An end-to-end test runs the wizard with a real-looking key for each of the three providers against stand-ins for their APIs and then searches every log line (debug included), the SQLite database and WAL files and the blob store for it.

**One structured call.** Each provider makes one call per attempt; the Anthropic one streams (`Messages.NewStreaming`, accumulated). The answer's shape is enforced with structured outputs: `output_config.format` carries a JSON schema for the whole pack (statement, samples, subtasks with generator arguments and test counts, C++ reference, Python brute force, generator and validator, checker, limits, tags, difficulty) with `additionalProperties: false` everywhere. Lengths and ranges are not expressible there, so the server decodes strictly and then checks the wizard's rules (one to five samples, points adding up to 100, at most 30 planned tests) and the editor's own field rules on the draft the answer would become. A failing answer is sent back once with the list of reasons; a cut-off answer (`stop_reason` `max_tokens`) is retried once with a request for a shorter answer; a refusal (`stop_reason` `refusal`) is reported with its category and not retried.

**Refusal fallbacks (D54).** For the models that accept it (Fable 5.1, Opus 5.5, Opus 5, Sonnet 5.5; a table of a feature, not a choice of model) requests carry the beta `server-side-fallback-2026-07-01` with `fallbacks: "default"`, so a request refused by the safety classifier for a reason that does not apply to a fallback model is answered by that model instead of failing (contest problems about "attacks" on a grid or "exploiting" a recurrence are ordinary). A mid-stream fallback restarts the text, and the client keeps only the fallback's answer. Haiku 5.5 has no fallback. There is no default model: the admin picks one from the live list or types its id (D111).

**Prompt injection.** The system prompt is fixed text. The brief or pasted text goes into the user message inside one `<brief>` or `<problem_text>` block, the system prompt says that everything inside it is data, and any tag in the data that could close the block early is defused. The worst a hostile text can do is make the model write a bad problem, which the checks, the judge and the admin's review catch: the model's code is never run on the server, only on the judge in isolate, like any setter's code.

**Nothing AI-made reaches students unreviewed.** A draft carries the tag `ai draft` until a person saves it. The chain may validate the draft on its own, but a contest refuses a problem still tagged `ai draft`, and only validated problems can join a contest at all.

**Model solve was removed in v3 (V3-4).** It sent the statement to a model and judged the answer; v3 keeps the wizard to drafting problems only.

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

**Problems are pinned.** A contest takes only validated problems, and adding one records its content hash and its subtask points; the board scores against the pinned points and clamps any earned value to them, so an edit to a problem never changes a contest's board until the problem is added again. Only owners and admins choose a contest's problems and publish it: the problem bank is theirs, and co-organisers (often student TAs) never see it. A problem is ready only while both its content hash and its validated hash still equal the pin. Publishing, running, submitting and rejudging all require that: a test edited mid-contest stops judging of that problem (`409 not_ready`) until it is validated and added again, so no student is ever judged against tests nobody checked. From v1.2.0 the pin is a problem revision, and "Update contest to revision N" replaces adding the problem again (see "Problem revisions and pins").

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

Organisers always see the full board, and either variant on request. The reveal opens once every window has closed: one Reveal button on Results, after one confirmation, shows students the full board. Before then the page says when it opens and offers no button.

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

**Integrity (D47).** A second event stream opened from another browser or machine while one is open is recorded as a `second_session` event for every contest whose window is open for that student, at most once per 10 minutes per contest. A device is the browser plus the network address, except for a phone or tablet browser, which is the browser alone: a phone moving from wifi to cellular keeps its old stream open until TCP gives up, and would otherwise look like a second machine (council C1-051). Two phones of the same model and browser version are therefore one device to this check. A retention job, run at start-up and every 12 hours, deletes integrity events and clears submissions' client hash, typed and pasted counts and focus losses 180 days after a contest ended for everyone, except for students in an `open` or `awaiting_student` review case for that contest (see "Integrity" below).

## Problem revisions and pins

v1.2.0 (P2, decision D61). Before it, a contest pinned a content hash, which leaves the statement out, and the workspace read the statement live: an edit mid-contest changed what students saw and left no record. Now the history is in data.

| Table (0010) | Holds |
|---|---|
| `problem_revisions` | One row per version of a problem: number, content hash, title, statement and its hash, limits, checker and its source hash, the language and SHA-256 of the reference, brute force, validator and generator, samples, open tests, subtasks (points, titles, ranges), author, time and reason. Programs are kept by hash only; tests are listed in `revision_tests` by blob hash, so a revision costs a few hundred bytes plus its statement. |
| `revision_tests` | Each test of a revision with its kind: `sample` or `hidden` (v3 dropped `open`, V3-5). |
| `contest_pin_history` | Every change of the revision a contest label pins, from when, by whom. |
| `revision_views` | The served log: the first time the contest workspace served a revision to a student. |
| `contest_problems.revision_id`, `submissions.revision_id`, `jobs.revision_id` | What a contest pins and what each submission and job was judged against. `pinned_hash` stayed for one release (expand, D68); migration 0012 (v1.3.0) dropped it, and the pinned content hash is read from the pinned revision. |

**Every write makes its revision (B2.2).** The `problems` row stays the editable working copy. Every write of a problem, its subtasks or its tests goes through two functions, `insert` and `save`, which write a revision in the same transaction when anything a revision records changed; a save that changes nothing (or only tags, slug, difficulty or the generator plan) writes none. Generated inputs and outputs are saves too, with no author and a fixed reason. A test parses every Go file of the server and fails when anything else writes those tables, and `problems.CheckRevisions` (run by the tests and by `podium-server check`) fails when a working copy differs from its newest revision. While a live contest pins the problem, a change needs a reason, kept on the revision.

**Pins by revision (B2.3).** Adding a problem to a contest pins its newest revision, which is the working copy. The workspace serves the pinned revision's title, statement, limits and samples, never the working copy, so an edit in the bank reaches students only through "Update contest to revision N". That action accepts only a revision that is the validated content of the problem now (its content hash equals the problem's, and a passing validation covers it); it moves the pin and the subtask points in one transaction with a pin history row and a contest audit row, and sends a `statement` event. A statement-only update needs no rejudge; any other needs validation first and a rejudge after, and the console says which. Judging still reads the working copy, which equals the pinned revision by hash whenever the problem is ready (an unready problem is not judged, as before), and a rejudge records the current pin's revision on its new jobs while the old job keeps its own.

**What X saw at T (B2.4).** The pin history says which revision was pinned at any moment; the served log says when each student first opened each revision. One insert per student per revision, about 700 times the number of problems per contest; a repeat serve is a read, never a write. `podium-server saw` answers the question on the VM.

**Who sees history (B2.5, B2.6).** Owners and admins see every revision of a problem on the problem routes. Organisers of a contest, co-organisers included, see its pin history. Students see only the titles and statements of revisions pinned to a contest they are in, from the one in force at the start onward, as a line diff (a longest-common-subsequence diff of lines after trimming the common start and end, coarse beyond a million line pairs). No history route returns a test file, its content or its hash (D47).

**The upgrade (backfill).** Migration 0010 makes revision 1 of every problem from its working copy. A contest pin whose hash is older than the working copy cannot be rebuilt in full, so it becomes a *reconstructed* revision, numbered before the working copy: tests and limits from the newest judging job of a contest that pinned it (a contest only judges a problem whose content equals its pin), the title of the working copy, no statement. It is marked "reconstructed: tests only, statement not recorded", and the workspace serves the working copy's statement for it, as v1.1.0 did. The source hashes are computed in SQL by `podium_sha256`, a function the db package registers on every connection, so the backfill stays in the migration file.

**Append only, with a retention cutoff (D69).** `problem_revisions`, `revision_tests`, `contest_pin_history`, `revision_views`, `site_audit` and `contest_audit` refuse every update and every delete of a row newer than their table's cutoff in `retention_cutoffs`, a table only the retention job writes (v1.3.0, "Retention, erasure and the rollover"). No row means nothing may be deleted, and a trigger refuses a cutoff newer than 90 days ago (draft revisions) or a year ago (everything else), so a stray statement cannot open the tables. A rejudge keeps each submission's own reveal decision (D115), so it never shows a hidden test to someone who did not have it. Foreign keys keep a pinned or judged revision from being deleted at all. The blob collection keeps the test files of every pinned or judged revision.

## Audit logs

Two append-only tables, written in the same transaction as the change they record.

| Log | Records | Read on |
|---|---|---|
| `contest_audit` (0008) | Every organiser action that can change a contest's results: settings, problems and pins, revision updates, the roster with the emails added and removed, extensions, extend, pause and resume, rejudges, the reveal, the editorial, co-organisers, AI confirmations, forced deploys | The console's "Changes" feed, filterable by action and actor; the JSON export |
| `site_audit` (0011) | People and the site: roles granted and revoked, domains added and removed, accounts disabled and enabled, sessions ended, Google links reset, run mode changes (the server's own, with no actor), forced deploys; from v1.3.0 site settings changes, contact message deletions, erasures and the semester rollover | The People page's "History", owners only, filterable by action, email and time |

Each row keeps the actor's email and role as they were. The actor rides on the request context from the session check, so a change records whoever made it without each handler passing it on. A row's details are fixed fields (roles, domains, counts, emails); nothing from a request body, header or cookie is copied into it, so no AI key, session token, cookie or OAuth code can land there, and a test sends fake ones through every audited path and searches both tables and the database file for them. A forced deploy (`deploy-check -force`, D112) writes one row per refusing contest and one site row; `GET /api/me` returns its time for 12 hours so the web app can say "The site was updated at 14:02". On a database older than 0011 (deploy-check runs before the upgrade migrates) only the contest rows are written.

### Slug history

`slug_history` (0011, D65): renaming a published contest records its old slug (a draft has no links to keep). `GET /contests/<old>/...` and `GET /api/contests/<old>/...` answer `301` to the same path under the current slug, checked only after no contest has the slug and only for a signed-in visitor (a contest's current slug is contest data, and nothing before sign-in shows any); other methods are not redirected. A signed-out visitor gets the app, signs in, and the API path redirects. No other contest may take a slug in the history, so an old link never lands on a different contest; a contest may take back its own.

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

**Why there is no AI score.** No tool reliably tells AI-written code from a student's own: detectors are wrong often enough to accuse honest students, and a number on a screen gets quoted as proof. Podium relies on prevention (fixed-start contests in a lab, the honour code), indirect evidence (large pastes, a full score far faster than the class, clusters of near-identical code) and verification (the student explains or changes the code in front of an organiser). Signals are worded as strength, for similar code with its reason ("Short programs, identical" is weak however high the score, because honest pairs on small problems match completely), the raw numbers in the details, and nothing happens without a person.

**Why on the server (D49).** The check runs in one bounded background goroutine in `podium-server`, not as a judge job: its inputs are trusted text (sources, verdicts, output hashes) and nothing is executed, so the sandbox adds nothing, while sending every source to the judge would widen what the judge holds. One check runs at a time per server (`409 busy` otherwise), with a 10-minute limit; the request returns at once with the run's id and the page polls. A check left running by a restart is marked failed at start-up. Revisit if a large contest's check takes long enough to hurt request latency.

### Fixes after the integrity review

- **Signal and run ids are never reused** (`AUTOINCREMENT`), and a case's compare view reads the case's own copy of the evidence through its own route. A re-run used to free ids that SQLite handed out again, so a case could open another pair of students.
- **The case state machine is enforced on the server.** An allowed-transitions table; action needs every student's turn to be over (an explanation after the latest share, or the 7-day response window, `integrity.ResponseWindow`); a reopen ends the turn and needs a new share. Clearing is always allowed because it never hurts a student. Reason: D47 says students explain before any decision; the review showed a case could be actioned the instant it was shared.
- **Explanations are versioned** in `integrity_explanations`; a decision carries the versions the reviewer read and is refused (409) if a newer one exists. The decision row stores them (`explanations_seen`).
- **The decision log is append only in the database**: triggers refuse UPDATE and DELETE on `integrity_decisions`, and `case_id` and `by_user` are `ON DELETE RESTRICT`. The triggers also block cascades, on purpose: nothing deletes users, cases, or contests that have cases (the clone rollback deletes only a contest created a moment earlier, which has none), so a cascade into the log would only ever be a mistake.
- **False-positive guards found by the review:**
  - Hard-coded answers skip expected outputs that appear in the reference, the brute force or the statement, or in the programs of more than max(3, 10%) of the students; each distinct source is squashed once (was once per submission per output, about 8.5 s per problem in the worst case).
  - Probing ignores all-SK attempts, compares near-identity with the first attempt of a run, and counts only flips (AC back to a failure, or one failure to another), not forward progress.
  - Similar code: strength needs an amount of shared fingerprints as well as the score; the common-fingerprint limit is counted per language. The shared base is the reference and the brute force only; the unused `Settings.ExtraBase` hook for organiser templates was removed in v1.1.0 (B1.11), since nothing could fill it.
  - Same wrong output: the common-mistake limit is max(3, 2% of the students).
- **The lexer reads hidden code**: the body of a C++ `#define` (only `#include` lines are dropped; line continuations join lines) and a Python string literal passed straight to `exec` or `eval`.

## Students: manual review, outcomes, Standing and progress (v1.4.0)

Migration `0013_students.sql` (expand only) adds the tables of the v2 students milestone; the code is `server/internal/integrity` (`manual.go`, `outcomes.go`, `deadlines.go`, `rating.go`), `server/internal/notifications`, `server/internal/contests/summary.go` and `adjust.go`, and the routes `httpapi/review.go`, `students.go` and `progress.go`. The reviewer's guide is `docs/INTEGRITY.md`.

- **Manual cases and private notes.** A reviewer writes a note on a contest submission (`submission_notes`, append only, deleted at 30 days unless a case is opened from the submission) or opens a case from it (`integrity_cases.source = 'manual'`, `submission_id`). The case copies the submission and the notes into its evidence, so the case keeps them after the notes' own retention. Why: the owner asked to "flag" what a person spots by reading; a note is a hunch for reviewers, and only a case reaches the student, which keeps the promise that students never see a flag.
- **Two-person rule.** An outcome (`warning`, `zero_problem`, `referred`) is a row in `integrity_proposals` that a second reviewer confirms; the confirmation writes the actioned decision with the outcome, the students it applies to, the proposal id and the sole-organiser reason when the owner confirmed alone. It applies to every outcome, not only manual cases, because an outcome now changes the board and the rating (D167). A confirmed outcome changes only through an appeal (`integrity_appeals`, `integrity_appeal_rulings`, append only; the appeal text is emptied with the case evidence).
- **Zero for problem changes the board (D77 amended).** `outcome_adjustments (contest, user, problem, decision)` is read by the pure `Compute` on every call: that student's submissions to that problem are dropped before scoring, so a rejudge (which rewrites verdicts) cannot bring the score back, and ranks and the tie-break follow. An upheld appeal deletes the row. The decision stores each student's rank, total and score from just before, and the contest audit gets a `board_adjusted` row, because boards are computed, not stored. The public `Cell` has no mark; the standings carry `board_updated_at` so the results page can say the board changed; a frozen board ignores decisions until the contest is over for everyone.
- **Clocks.** Reply 7 days from first opening (`integrity_case_users.first_opened_at`), capped at 21 days from availability (`notice_available_at`, the end of the student's live contest when the share happened during one); a reminder on day 5 (`reminded_at`); 14 days to decide, 14 to appeal, 14 to rule. A one-minute sweep in `podium-server` sends reminders and writes summaries.
- **Notices** (`notifications`) store only a kind and ids; the sentences are written at read time from fixed templates with a contest title and a problem label, so no notice can carry code, evidence, test data or a name. While the reader is in a live contest the inbox is empty and the unread count zero. The package imports nothing of the server's own; contests and integrity insert rows in their own transactions.
- **Rating and Standing band (D110).** `student_rating` holds tenths (0 to 100, 100 when absent); every change goes through one function that clamps and writes `rating_history` (append only, 4 years) inside the caller's write transaction, so the write gate serialises concurrent changes. Causes: a confirmed outcome (5, 15, 25), a manual edit (a band-lowering one waits in `rating_proposals` for a second owner or admin), an upheld appeal (adds back the recorded change), and the semester rollover (`podium-server semester-rollover LABEL`, 5 per student with no outcome since the previous rollover, once per label in `rating_rollovers`). Students see only the band and the dated lines. A test parses the standings, export, summary, notices and progress code and fails if any names `rating.go` or queries a rating table: the rating is shown, never used.
- **Record views (D97).** Every read of a record page, a case or a submission's notes by someone other than the student adds a `record_views` row (append only, 2 years); the owner reads it with the per-reviewer oversight counts.
- **The student screens (web, v1.4.0 frontend).** "Your progress" (`/progress`, loaded on first visit only so it never weighs on the contest bundle) draws from the progress response alone: the web app recomputes the topic, difficulty, loss and retry lists only when the server leaves one out (`lib/progress.ts`), and it never asks for progress about a contest whose board is still frozen or held. The chart is inline SVG drawn at the measured width with a table view as the keyboard path; no chart library. Notices (`/notices`) and the unread count come from `GET /api/me` and the inbox list, read again after a stream reconnect. The contest workspace reads the contest again on every reconnect and every fallback poll, since events sent while the stream was down are lost (announcements, answers, statement updates, time). Standing shows the band and its lines; no student type in the web app has a field for the rating, and a Playwright test feeds a stray rating into the mocks and checks no student page prints it. A case has its own student page, which posts `opened` when it shows the case, because the reply clock starts there.
- **The organiser screens (web, v1.4.0 frontend).** Students (`/admin/students`), a student's page and the record page are loaded on first visit, like progress. The rating number lives only in `web/src/lib/students.ts`, and a guardrail test fails if a student screen imports it (D222). The case page decides by proposal and confirmation, never by a single "Take action" (D226); appeals are ruled where the decision lives, the case page for an outcome and the record page for an adjustment (D227). Notes and manual cases start from the console feed; the notes dialog shows the submission's program beside the line ranges, since the notes route sends its source (D228 as changed by council run 1). A board a decision changed says so in one line on every results view, and no cell carries a mark (D231). People looks any account up by email with `GET /api/admin/accounts` before an account action, which answers the same 403 to anyone below admin whatever the address (D232, D235).
- **Summaries and progress.** `student_contest_summary` holds one row per student on a final board (score, percent, rank, per-problem score, attempts, first full-score minute, best verdict, subtask points, the zero mark). It is written when the contest turns final (every window closed, judging drained), again after a rejudge, a pin change or extra time, and after a review decision; never during a live contest. Results-final notices go once students see the final board (after the reveal when it is held). Progress, the students table and the class view read summaries, so nothing recomputes a board per request; 700 students build in about half a second on the desktop (calibration.md). Progress leaves out held contests, shows exam-mode contests as totals only, and never shows a test.

## What students can see

Decision D47 sets the rule: hidden tests never show their input, output or checker comment, and the figures a student sees are coarse enough that they cannot be used to read hidden tests.

| Boundary | Rule |
|---|---|
| Judge report | A `submit` report carrying any output text, or a checker message on a hidden test, is refused. The text never reaches the server, so no later bug can display it. The one exception (D115) is a payload marked `reveal`, which the server sets only for submissions that never count: then the output of a failing hidden test (at most 4 KB) is accepted. Test verdicts are limited to `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` and `IE`. |
| Submission view | Verdicts, times and scores per test, never input, output or messages. Sample results come with the statement anyway. |
| Coarse view for students | No memory figure on hidden tests, maximum memory rounded up to 16 MB steps, times rounded (10 ms; 100 ms in contests). Applied on every path a student can reach: reading a submission, the workspace list, the answer to a submit or a retried key, and live events. |
| Progress while judging | Students see per-test verdicts only inside finished blocks (the samples, a whole subtask), and no running maximum time or memory, so polling shows no more than the stream. |
| Live events for students | Sent when the sample block or a whole subtask finishes, never per hidden test. |
| Exam feedback mode | Until the student's window closes, only "Submitted": no verdict, score, tests, times or compile output, on every path. Nothing the student can observe changes when judging finishes: no live submission or standings events, and no "still being judged" refusal (a new submission queues behind the pending one), since the moment judging ends depends on how long the program chose to run on hidden tests. A judge failure is shown, because it is never the program's doing and the student must resubmit. |
| Runs | Show output and the expected output on samples and the student's own input. The first failing sample is revealed with its input. |
| Revealed hidden test (D115, owner's decision) | A submission that never counts (practice after the window, unless another contest that pins the problem is still open, V3-6; the organisers' workspace) shows its first failing hidden test to its author and to owners and admins, once judged. Never on a submission that counts in a graded contest, in either feedback mode; never while exam mode hides the result; never to a co-organiser whose site role is student; never in feeds, exports, the board or the standings. The consequence for setters is in `docs/AUTHORING.md` (checklist item 6). |
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
4. The server creates or updates the user and starts a session: 32 random bytes in the cookie, only the SHA-256 in the `sessions` table. The cookie is HttpOnly and `SameSite=Lax`; with an `https` base URL it is also `Secure` and `__Host-` prefixed (host-only, whole site). A session ends 12 hours after sign-in however much it is used (`retention.Sessions`, since the security council fixes below); an hourly sweep deletes expired ones.

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

**A box belongs to one lane.** Lane n uses isolate box n, and the judge refuses to initialise a box it already holds. When isolate answers `--init` with "This box is currently in use by another process" (its lock is taken while another isolate call on that box runs), the judge waits 0.25, 0.5, 1 and 2 s between five tries and then fails the job as a judge error that names the likely cause; it never cleans up a box it did not get. isolate's lock covers only a running call, not an idle box another process initialised, so box ids must belong to one judge per host: a second judge or the sandbox suite beside it must use other ids (the self-test uses box 80, the sandbox suite 90 and 91). On the walk of 2026-10-21 a sibling stack held box 1 and one submission became a judge error that "Judge again" fixed (D325).

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

## Public pages: head, manifest and 404s

v1.3.0 (P3, D100). The signed-in app stays client-rendered; no crawler can reach it. What a crawler reads is delivered by the server, and the words come from the frontend:

| Piece | Owner | Where |
|---|---|---|
| Route table with `public`, `title`, `description` | frontend | `web/src/lib/routing.ts`, built to `dist/routes.json` (contract C2) |
| The marker pairs in `index.html` and their default `noindex` | frontend | `web/index.html` |
| Filling the head per route, the 404 status, `X-Robots-Tag`, `robots.txt`, `sitemap.xml`, the Organization JSON-LD, absolute canonical and card URLs | backend | `server/internal/webui/head.go` |
| The approved privacy text as `dist/privacy-body.html`, the 1200 x 630 card as `dist/og.png`, icons and `site.webmanifest` | frontend | `web/` |

The server reads the manifest once at start and matches paths the way `routing.ts` does. A public route gets a full head (title, description, canonical from `PODIUM_BASE_URL`, Open Graph, Twitter card, `index`); every other route keeps the block as built, which is `noindex`, and the response adds `X-Robots-Tag: noindex`; a path no route matches gets the same app with status 404 and `noindex`, so the app's own not-found page still renders with its header. `/privacy` also gets the build's privacy text in its body block, so it reads without JavaScript, and `/contact` gets one sentence with the address to write to (the organisation's contact email, else the owner's), since its form needs JavaScript. A test walks both checked-in manifests and asserts one robots rule and one title per route, one canonical equal to `og:url` on each public route and none elsewhere, and a Go crawl follows every same-site link of the public pages, `robots.txt` and the sitemap.

**Why default closed.** The two tracks merge separately. A missing manifest, one of another version, or a route the frontend has not marked public all leave the page `noindex`: a slipped merge can hide a public page, never expose a private one or give a page two robots rules. The block is a marker pair rather than a Go template because the built `index.html` carries hashed asset names the server cannot know in advance (D100).

**Why the 404 is never indexed.** The manifest marks the not-found page public so its title and description are the frontend's, but a 404 has no preferred URL, so it gets no canonical and keeps `noindex`.

## Retention, erasure and the rollover

v1.3.0 (P3). How long each kind of data is kept is one Go file, `server/internal/retention/periods.go`; the session lifetime, the integrity retention, the daily retention job and `GET /api/auth/config` read it, and the privacy page renders its table from that response. `docs/OPERATIONS.md` ("Retention") holds the same table and a test compares the two, so the privacy page, the docs and the code cannot say different things.

The daily job (`retention.Job`, started by the server a minute after start-up) applies the periods no older job applies: run payloads (30 days), contact messages (180 days), unused problem revisions (90 days), submissions (4 years after the contest) and the audit logs (4 years). It works in batches of 2,000 rows per transaction, so a pass never holds the single write gate for long while a contest runs. Before a contest's submissions go, its final standings (rank, user id, totals, no names) are kept in `contest_results`. The append-only tables accept a delete only below the cutoff row the job writes, and their triggers refuse a cutoff newer than 90 days or a year, every period being at least that long (a test checks it), so a bug in the job cannot reach recent history.

From v1.4.0 (P4) it also applies: private notes after 30 days, unless a manual case was opened from their submission (then they go with the case's evidence, 2 years after its decision); notices after 180 days; record views after 2 years; the rating history and band-lowering rating proposals after 4 years, through their cutoff rows, without changing any current rating; and the per-student contest summaries with their contest's submissions, 4 years after the contest. The full table, with how each period ends, is in `docs/OPERATIONS.md`, "Retention", and a test keeps it equal to `periods.go`.

**Erasure** is a command on the VM, not a route: `podium-server forget-user` deletes what identifies a person (sessions, drafts, integrity events, contact messages, browser hashes, extension reasons, clarification questions), replaces the name and email with `Removed user N` in the account and the rosters (a roster entry with no account is erased too), and keeps scores so past standings stay correct. It is a dry run by default and writes a site audit row naming the account by id only. Audit rows written earlier keep the address until the audit retention removes them: append-only means nothing, not even erasure, rewrites history.

**The rollover** is an owner action on People (the route `POST /api/admin/rollover`) that archives the term's contests and disables student accounts unseen for 180 days, after a preview with the counts. Disabled, never deleted. The rating's own rollover is the separate command `podium-server semester-rollover` (v1.4.0).

## Site settings and the contact form

v1.3.0 (P3, D71 as amended). The organisation's name, contact email, phone and privacy contact live in the `settings` table, entered by an owner, cached in memory and returned before sign-in by `GET /api/auth/config` with the retention periods; no contest data is ever on that route (owner, 2026-10-19). Until the contact email is entered the pages fall back to `PODIUM_OWNER_EMAIL`. Nothing on a public page is hard-coded.

The contact form is for signed-in people only: sessions identify the sender, so the honeypot and the per-address limit of the first design went, and someone locked out uses the organiser address on `/signin`. Messages are plain text in `contact_messages`, at most 5 per person and 100 in all per 24 hours (one statement counts and inserts, so two sends cannot both take the last place), read by owners and admins and answered from their own mail. The server runs no mail server.

**Before sign-in.** The routes a visitor reaches without a session have per-address token buckets (5,000 at once and 50 a second for the config and health check, 2,000 and 20 a second for the sign-in steps; IPv6 by /64; the VM itself exempt), sized so a class of 700 behind one college NAT can sign in within a minute (a test proves it), and each Google sign-in attempt finishes once: the callback remembers each state until it expires, so a replay costs no outbound call (P0 security review, finding 4). Both tables are bounded in memory; a full address table is swept at most once a second and otherwise makes room by dropping one entry, so a stranger never shares a budget someone else is draining.

**Queue health (C10, D103).** The organiser contest response carries the contest's waiting and judging jobs, the oldest wait, its judge errors and unanswered clarifications from one statement over partial indexes, and when a judge last asked for work, which the jobs service holds in memory.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). Writes take turns in Go before they reach SQLite (`server/internal/db/writegate.go`): every transaction that is not read-only, and every statement outside a transaction that is not a read, waits on one in-process gate, so writers never meet SQLite's busy handler, which sleeps and polls (1 ms rising to 100 ms) while holding a pooled connection. The 700-student load test showed why: with that hand-off, four connections all sat in waiting writers, reads queued behind them, and judging stalled for 90 s (M7, measured on a desktop; the rerun on the VM is in the P0 drill notes). Reads run in parallel on the pool's other connections (8 in all). A write that waits 10 s for its turn fails with "database is busy", as `SQLITE_BUSY` did before; other processes (the sqlite3 shell, Litestream) still use the busy timeout. `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction. Litestream replicates the database to a bucket, uploading changes every 10 s, and a nightly job copies the database (SQLite's online backup) and hard-links the test files into `/var/backups/podium` (`docs/OPERATIONS.md`, "Backups"). `podium-server check` verifies a database and its files read-only: integrity and foreign key checks, migrations against the binary, and every recorded file present with its size (or, with `-hash`, its content).

Schema changes are numbered files in `server/internal/db/migrations` (`0001_v3.sql`, then one file per change), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

**v3 squash (V3-3, 2026-10-27).** Nothing was installed, so v2's migrations 0001 to 0015 were replaced by one file, `0001_v3.sql`, built from the schema they produced minus the tables and columns the v3 cuts left unread: the contact form, co-organisers, pin history, results snapshots, retention cutoffs, the served log, slug history, the toolchain registry, `contests.graded`, `open_tests`, `jobs.toolchain_id` and a few unread columns. It adds `json_valid` checks on the JSON columns scoring reads (`contest_problems.points`, `submissions.earned`, `contests.languages`) and the view `contest_final`, the one SQL statement of "over for everyone" (`Contest.Final` in Go). The append-only triggers on the audit and revision tables no longer read a retention cutoff: v3 deletes none of those rows. `TestSchemaTables` in `server/internal/db` lists the 31 tables. The reasoning of the v2 migrations (the `writable_schema` CHECK widening of 0007, the revision back-fill of 0010) is in git history before this commit.

Times are Unix milliseconds. Sessions are stored by the SHA-256 of the cookie value, so a copy of the database cannot be replayed as logins.

## Unused files on the server

`PODIUM_TESTS_DIR` keeps every file a job may fetch. Tests and submitted programs are referenced from the database for as long as their rows live; reference solutions, checkers, generators and custom inputs are written again each time a job needs them and are only referenced by job payloads. Without a clean-up, every edited reference solution, replaced test and custom input would stay forever. The 12-hourly retention pass removes them (`problems.Blobs.GC`):

| Kept | Why |
|---|---|
| Any test of any problem, any submission's or run's source | still referenced |
| Anything a job that is waiting or running names, and anything a job less than 7 days old names | the judge may still fetch it; a recent run still shows its samples' expected output |
| The tests of a revision a contest pins or pinned, or a submission was judged against (v1.2.0) | the record of what judged it; other revisions' files go like any unused blob |
| A blob recorded in the last 24 hours, or a file with no row written in the last 24 hours | a save or an upload in progress; 24 hours is longer than any job may run |

Removal and saving cannot race: every save records its blob (or refreshes the row's time) inside the transaction that adds the reference, and checks the file is still there inside that transaction. The collection deletes each batch of rows and files inside one write transaction, and deletes a row only if its time is still old, so a blob saved again after the candidates were read is skipped, and a save that lands just after a removal writes the file back (or, for test uploads written before their transaction, fails with "try again" instead of leaving a row without a file). `testcases` also references `blobs` by foreign key, so a test can never lose its row. Job payloads are scanned as text for 64-digit hex runs, so every hash any job kind could fetch is seen whatever the payload's shape.

## Frontend structure

The web app is one Svelte single-page app with no component library. Since v2 P1 every screen is built from a few shared pieces, so a pattern is changed in one place:

| Piece | Where | Job |
|---|---|---|
| Tokens | `web/src/styles/tokens.css` | Every colour, size, measure and z-index. Type roles `--fs-page` (28 px on phones, 34 px from 768 px), `--fs-section`, `--fs-sub`; `--fs-6` only for the student's own place on standings (D80); `--mono-optical` sets mono digits one step down beside the Grotesk |
| Controls | `web/src/styles/controls.css` | Buttons with hover and pressed states, fields, tables (`.table-dense` for logs and feeds, `.table-stack` for stacked phone rows with a `.row-link` that covers its row), `.back`, `.state-word`, `.page-section`, the dialog shell |
| `PageHeader` | `web/src/components/PageHeader.svelte` | The top of every route: breadcrumbs on organiser screens or one back link on student screens, the page's only `h1` (focused after each client-side navigation), state word, actions, lead line and the contest sub-nav |
| `Breadcrumbs` | `web/src/components/Breadcrumbs.svelte` | `nav aria-label="Breadcrumb"`, an ordered list, the current page last with `aria-current` |
| `PageState` | `web/src/components/PageState.svelte` | Loading (shown after 300 ms), error with Try again, forbidden with the organisers' contact email, missing with a link to the parent, empty with its next action |
| Announcer | `web/src/lib/announce.svelte.ts` | Two always-present live regions in `App.svelte`, polite and assertive. Each message clears its region first, so the same sentence said twice is spoken twice. Widgets with a running text (the contest clock) keep their own region |
| Breakpoints | `web/src/lib/breakpoints.ts` | Tiers at 768, 1024, 1280 (`desk`, the page split, D181) and 1440 px plus a short height tier. CSS writes `(min-width: 768px)` or its exact complement `(width < 768px)`, never `max-width: 767px`, so no zoomed width falls between two layouts; a guardrail test fails on any other width |
| Icons | `web/src/lib/icons.ts`, `web/src/components/Icon.svelte` | Lucide shapes as data, 20 px beside words and never instead of them |
| Errors and forms | `web/src/lib/api.ts` `errorText`, `web/src/lib/focus.ts` | `errorText(e)` is the one fallback: the server's sentence, or "Something on this page failed. Reload the page and try again." (a guardrail test forbids a second copy). Required controls carry `aria-required` and the form says "All fields are required." where true; after a field error, from the page or from the server, `focusFirstInvalid()` moves focus to the first marked field (X7) |
| Route table | `web/src/lib/routing.ts` | Each route's pattern, sign-in need, `public`, `title` and `description`. Page titles read it, and the build writes it to `dist/routes.json` for the server's head, 404 and robots (contract C2) |

### Organiser screens (v2 P3)

The organiser screens use the P3 working layout (`.page-split`): the form or feed on the left and a sticky context column from 1280 px (D181).

| Screen | Context column | Notes |
|---|---|---|
| Problem editor (`routes/ProblemEditor.svelte`) | `ReadinessPanel` and the preview | The route keeps loading, saving, the polling and the dialogs (under 700 lines); the sections are `components/problem/*`: `EditorBar`, `StatementSection`, `SolutionsSection`, `TestsSection`, `GeneratorPanel`, `JudgingSection`, `EditorFooter`, `ModelSolve`, `RevisionHistory`, with shared classes in `editor.css` and field error lookups in `lib/problemFields.ts`. `lib/problems.ts` `judgedFieldsChanged` mirrors `server/internal/problems/hash.go`, so the panel can say a save needs a new validation; keep the two in step |
| Contest editor | "Before you publish" (`lib/contests.ts` `publishChecks`) | Publishing is in the title row with a confirmation; problems save on each change; rules fold under a summary; leaving with unsaved settings asks first |
| Console | Questions and announcements | The opening sentence (`consoleSentence`, stage C2) reads the C10 queue health and the judge's last request for work on the contest response and the started count from the stats, refreshed every 10 s with the feed; banners appear only for what needs action, and Extend is refused by the server once the contest has closed (it would reopen it); End now uses the settings update with the end set to the server's now, and a freeze that keeps the frozen moment where it was (none if the board was not frozen yet), because the freeze counts back from the end and would otherwise hide results students already saw |

Organiser state words come from `organiserState` in `lib/contests.ts`: "Draft" while unpublished and "Ended, practice open" after the end, so a contest that is over never reads "Practice" to its organisers. The AI key form is one component (`components/ai/KeyForm.svelte`) on the settings page and inline in the wizard.

State is a shape: a selected tab, the current sub-nav link and the next reveal row are drawn with borders, and components that draw fills (subtask strip, rail bars) carry a `forced-colors` block, so Windows high-contrast mode keeps every state visible.

`web/index.html` carries two marker pairs the server fills per route (D100): `<!--podium:head-->` around the default `noindex` robots meta, and `<!--podium:body-->` inside a fallback `<main>` that `main.ts` clears before the app mounts.

### The shell by role (v2 P2)

Navigation is named by job (D81, D146). Organisers do not share the students' landing page:

| Role | Header nav | "/" lands on | Account menu |
|---|---|---|---|
| Signed out | none; a Theme control | Sign in | none |
| Student | Contests; Questions once an organiser has shared an integrity case | Your contests | Signed in as {email}, the role word, Theme and colours, Keyboard shortcuts, Sign out |
| Admin | Contests (`/admin/contests`), Problem bank, Inbox (P3) | `/admin/contests` | same |
| Owner | Contests, Problem bank, People, Inbox (P3) | `/admin/contests` | same |

- One account control (`components/AccountMenu.svelte`) holds identity, "Theme and colours" (a dialog the shell owns, `ThemeDialog`, with `ThemePicker`), Keyboard shortcuts and Sign out. The header fits one row from 768 px: the button names the person from 1024 px and reads "Account" below that. Under 768 px organisers get a Menu panel with the account items inline; students keep their links and the account button in the row.
- Sign out first runs every save registered with `beforeSignOut()` (`lib/session.svelte.ts`, at most 3 s), so the workspace's last edits reach the server before the session ends. During a live contest the workspace also registers `guardSignOut()`, and the shell asks "Sign out during the contest?" first (P5, D216).
- The contest sub-nav ends with "Student view" and shows the unanswered question count after Console; it wraps instead of scrolling at 320 px.
- The footer names the organisers ("Contact the organisers: {email}" from `GET /api/auth/config`). `/privacy` exists but nothing links it until the owner approves its text (D85): one constant, `PRIVACY_LINKED` in `web/src/lib/site.ts`, turns on the footer and sign-in links.
- Organiser page titles read page first ("Console · Week 6 contest · podium"), so browser tabs differ when cut short.

### Page layout and controls (v2 P3, D181 to D184)

The 2026-10-20 usability audit found the screens unfinished at the layout level: one narrow column on wide screens, every section at the same weight, and links, buttons and mono text mixed with no system. P3 adds five shared pieces and every screen uses them:

| Piece | Where | Rule |
|---|---|---|
| `.page-split` | `styles/base.css` | From 1280 px the work sits at `--measure-form` and a sticky context column (`--measure-aside`) beside it: rules beside the start button, messages beside the result, other ways to write beside the contact form. Below 1280 px the two stack in DOM order |
| `.panel` | `styles/base.css` | One task area on `--surface` with a `--line` border, square, no shadow, as tall as its content; never a grid of equal tiles |
| `Badge` and `.badge` | `components/Badge.svelte`, `lib/badges.ts`, `styles/controls.css` | Verdict and state words on a tint, sentence case. `.state-word` is drawn the same way, so every state word on every screen reads alike |
| Controls | `styles/controls.css` | Every button is `--control-h`; `.btn-compact` only narrows padding and type, inside rows and toolbars; one primary per view; actions in bars, rails and rows are quiet buttons (`.btn-quiet`, or `.btn-link` in the action colour) with no underline; underlines stay for prose links and breadcrumbs |
| `MonoDigits` | `components/MonoDigits.svelte` | Mono for digits only: dates, times and "40 of 100" keep their words in Grotesk |

The editor well gets a `--well-edge` border, the well's own colour in light themes and `--line-strong` in dark ones, where the dark well would otherwise vanish into the dark page. The own place on standings and on the contest result is one component, `PlaceFigure`.

**Why 1280 px.** The form measure (48rem) and a useful context column (about 26rem) with the gutters need about 1,250 px; at 1024 the context column would squeeze the form. The workspace already used 1280 for its wider split, so the tier was added to `lib/breakpoints.ts` rather than kept as an exception (D181).

### Public pages and contact (v2 P3, FP.1 to FP.5)

- `lib/mode.svelte.ts` loads `GET /api/auth/config` once per page and holds the run mode, the contact email, the organisation (C5) and the retention periods; the sign-in page, the footer, `PageState`, `/contact` and `/privacy` read that one copy. A field the owner has not entered shows nothing.
- The privacy notice is Markdown in `web/src/content/privacy.md` with a `<!-- retention -->` marker. `/privacy` renders it with the retention table from the config, never typed by hand. The build (`vite.config.ts`, `lib/privacyBody.ts`) renders the same Markdown with marked and DOMPurify on jsdom to `dist/privacy-body.html` for the server's body block, with a `<!--podium:retention-->` block in place of the table, which the server fills with a table built from `retention.Periods()` when it loads the build, so a reader without JavaScript sees every period too. Until the owner approves the text (D85) the route is not public and the page shows a placeholder; `/privacy?draft` shows the draft.
- `/contact` (signed in) posts to `POST /api/contact`; `/admin/contact` is the organisers' inbox with "Reply by email" as a `mailto:` link. Messages are plain text everywhere.
- `web/public` holds the 1200 by 630 social card `og.png`, `favicon.ico`, the touch icon, two manifest icons and `site.webmanifest`; `tests/links.spec.ts` checks them, crawls every link and presses every button on the mocked screens.
- After an emergency deploy, students see "The site was updated at 14:02" once per update (D112, `lib/siteUpdate.ts`); the shell reads `/api/me` again whenever a live stream reconnects.

### The workspace (v2 P5, D211 to D220)

`components/Workspace.svelte` is one screen at three sizes. What P5 changed, and why:

| Where | What | Why |
|---|---|---|
| Under 1024 px | One sticky strip: the contest clock with links to the previous and next problem (the same `adjacentLabel()` as the "[" and "]" keys), then the tabs with Submit at the end. Submit lives only there; the letters with scores stay in the title area and the limits open the Problem tab | Students on phones had Submit on another tab and the statement 460 px down (audit S7) |
| While the strip sticks | `html` gets `scroll-padding-top` from the strip's measured height (`--sticky-bar-h`, any element with `data-sticky-bar`) | A focused field or the editor's caret never lands under the strip (X3) |
| From 1024 px | The workspace is sized from the shell's measured header, with no 32rem floor; a `role="separator"` between editor and result sets the result's share (20 to 80 percent) by drag or keyboard and "Hide result" folds it to one line that keeps the verdict; both are kept per device | Short laptops scrolled the page and got a 118 px editor (F7.3, F7.5, D87) |
| Short tier | 1024 px and wider, at most 45rem tall: tighter title row, a 30 percent first result share, and under 1280 px a 10rem rail and a wider code column; a code column under 36rem drops the toolbar icons, so the toolbar keeps one row | 1366 by 768 at 125 percent is a 1093 px window about 610 px tall (D214) |
| Focus | A tab that switches by itself after Submit or Run moves focus to the result's heading; time running out moves it to the page's h1 | Nothing pressed, nothing announced otherwise (F7.4, D215) |
| Touch | `interactive-widget=resizes-content` in the viewport meta; editor and custom input at least 16 px on coarse pointers | The keyboard resizes the layout, so the strip stays visible, and iOS does not zoom on focus |
| Verdict | VerdictPanel's headline and subtask strip stick to the top of the result pane while the tests scroll | The verdict landed below the fold (S5) |
| Revealed test (D115) | `TestDetail` shows a revealed test's input, expected output, output, first difference and "Run on this input", with the line "This test is shown because this is practice." or "because this was a run."; the run headline counts samples and open tests | The server decides what is revealed; the screen only shows what the response carries (D217) |
| Editor | Each CodeMirror language mode is its own chunk, loaded when chosen; the editor's keyboard hint is linked by `aria-describedby`; "Reset to starter" in Editor settings confirms and saves at once | 34 percent less editor code for a C++ student (`calibration.md`); X2; F7.12 |

### What a page loads, and the size budget (v2 P6, D249 to D251)

Contest start is several hundred students on campus Wi-Fi in the same minute, so the first load carries only what a student needs:

| Loaded | What | How |
|---|---|---|
| Every page | The shell and the student screens (Home, a contest and its workspace, standings, notices, questions, contact, privacy, sign-in) | The entry script and the chunks `dist/index.html` preloads: 332 kB, 112 kB gzipped (calibration.md, 2026-10-21) |
| First visit to an organiser screen | Every admin route, and Progress | `import()` from a table in `App.svelte`; a loaded module renders at once on the next visit. While one loads, the shell waits for its `h1` and focuses it if focus is still on the pressed link |
| A statement or Markdown text | marked and DOMPurify (`lib/markdown.ts`) | `import()` from the components that render Markdown |
| Maths in that text | KaTeX, its stylesheet and fonts (`lib/katex.ts`) | `import()` only when a placeholder exists; the TeX source is the placeholder's text until KaTeX draws it |
| The editor | CodeMirror core, then the chosen language mode | `import()` (P5) |

`web/scripts/size-budget.mjs` (`npm run size`, run in CI after `make build`) gzips the entry script, its preloads and the stylesheets and fails above 120 kB of JS or 16 kB of CSS. A budget is raised only with a measurement and a reason in `calibration.md`. Fonts: all four faces (Schibsted Grotesk 400, 500, 600 and Fragment Mono 400) are preloaded, because the first screen uses each (D91); a guardrail test keeps the preloads equal to the `@font-face` list.

### Announcements to screen readers (X1)

Everything spoken goes through the one announcer (`lib/announce.svelte.ts`): submission results and the submit itself, every stage of a run ("Running on samples.", "Passed 1 of 2 samples.", "Compile error."), pacing refusals, contest announcements and answers in the workspace banner, a statement change, info toasts, the start of a contest the student is waiting for, and a change of the student's own place on the standings ("You are now 12th with 240 points."). Only the contest clock keeps its own region, for its 30, 10, 5 and 1 minute milestones. Error toasts stay `role="alert"`.

### History screens (contracts C3 and C4)

`web/src/lib/history.ts` is the one adapter for the revision, pin and audit routes, so a renamed field is a one-file change. The screens:

- Students: when an organiser moves a problem of a running contest to a new revision, the workspace says "Statement updated 14:32. See what changed" (from `statement_updated_at` on load and from the `statement` event), and the open problem's statement reloads without touching the editor. "See what changed" opens `StatementDiff`, a line diff of the statements only; the route never carries test data (D47).
- Problem editor: a History section lists the revisions with who, when, why and where each is pinned, and compares a revision with the one before (`components/problem/RevisionHistory.svelte`). While a running contest pins the problem, a save asks for a reason.
- Console: "Problem versions" shows each pinned revision and offers "Update contest to revision N" when a newer one is validated. The confirmation says what students get and whether judging again is needed; afterwards the server's prefilled announcement waits in the announcement box for the organiser to post. A rejudge names the revision it judges against. The audit log is now "Changes", with filters by kind and by who.
- People: "History" is the site audit (roles, domains, accounts, run mode, emergency deploys), newest first, with a filter by kind and a search by email.

Diffs are drawn by `DiffLines`, which marks lines with "+" and "-" and hidden words, never by colour alone, and shows statements as plain text.

## Themes

The web app ships four colour themes plus "System", which follows the device's light or dark setting (v3 cut ten to four, V3-8):

| Group | Themes |
|---|---|
| Light | Light |
| Dark | Dark |
| Accessible | High contrast light, High contrast dark (all text at 7:1 or more) |

All colours live in `web/src/styles/tokens.css`. A theme changes surfaces, ink, rules and the one action colour; it never changes what a verdict colour means. Light themes share one set of verdict colours and dark themes another. A separate setting, "Colour-blind safe verdict colours", replaces green and red with teal, purple and orange in any theme; the three stay apart under simulated protanopia and deuteranopia. The editor well stays dark in every theme.

The choice belongs to the account (`users.theme`, `users.verdicts`; `PUT /api/me/preferences`), because students share lab machines: a high-contrast theme chosen for low vision follows the person, and signing out returns the machine to the system theme. The browser keeps a copy in localStorage only so the first frame is painted right.

Every theme is checked by tests rather than by eye: text at 4.5:1 (7:1 in the high-contrast themes) on every surface, control borders at 3:1, verdicts at 4.5:1 on their tints, and a minimum OKLab distance between the action colour and each verdict and between the verdicts themselves. A browser test confirms the CSS cascade produces exactly the values checked.

Projector reveal mode (M4) ignores personal themes and uses High contrast light.

## Safe to run: deploys, backups and alerts

**Deploy guard (D112).** A deploy restarts the server, so none starts in the 48 hours before a published contest, while one runs, or while its submissions are still being judged. `podium-server deploy-check` reads the published contests straight from the database rather than asking the running server, because the question must be answerable when the server is down, and `/healthz` is public. `install.sh upgrade` runs the new release's own guard, so even the first upgrade from an older build is guarded. A fix that cannot wait uses `--force "<reason>"`; the reason goes in the audit log of each contest that refused, under the owner's account, before anything is installed. After a forced deploy every signed-in student sees "The site was updated at 14:02" once. The other direction of D112, a warning when a contest is published or moved within 48 hours of a deploy and the release recorded on the contest, is not built: the owner decides whether it is (council C1-016).

**Upgrade and rollback.** An upgrade drains the judge, backs up, keeps the running binaries and units, installs, and verifies. Rollback across a migration is a restore, because an older server refuses a database a newer one migrated (`db.Migrate`); it names what is lost and needs a typed confirmation. Configuration files are never written by the scripts: an upgrade stops when the new release needs a variable the VM's files do not name.

**Graceful judge stop.** Jobs run under a context of their own, separate from the one that ends claiming, so SIGTERM lets each lane finish and report its job instead of killing isolate and waiting out the lease. `KillMode=mixed` keeps systemd from signalling the sandboxes directly. A forced stop still loses nothing: the job's lease runs out and the server hands it out again.

**Backups off the VM.** Litestream streams the database to a bucket every 10 seconds. The test files only exist on the VM, so each night's backup is packed, encrypted with `age` to a public key whose private half never leaves the owner's machine, and uploaded to a separate B2 bucket. A stolen VM or B2 key therefore yields no readable test files or student code.

**Alerts without a monitoring stack (D70, D113).** Nothing stays resident: systemd's `OnFailure=` starts a one-shot unit that posts the failed unit's name to an ntfy topic, and a 15-minute timer checks free space, WAL size, Litestream's last upload and a stalled judge. `podium-check restore-proof` restores the bucket's newest database into a scratch folder and runs `podium-server check -hash` on it; v3 runs it by hand once a term instead of weekly (V3-10). A dead-man heartbeat after each good backup and an external uptime check on `/healthz` cover the cases where the VM cannot report itself.

## Memory budget on the 2 GB VM

| Component | Approx. RSS |
|---|---|
| Kernel, systemd, sshd, journald | 200 to 250 MB |
| Caddy | 30 to 40 MB |
| `podium-server` (capped at 300 MB by systemd, Go heap limit 220 MB) | 50 to 80 MB |
| `podium-judge` worker (capped at 256 MB, Go heap limit 96 MB; inputs copied into tmpfs boxes count here) | 20 to 60 MB |
| Two sandbox lanes plus one JVM (all boxes together capped at 1100 MB on `isolate.slice`, 1024 MB budget in the runner) | up to about 1 GB |

isolate places every box under `isolate.service`, not under the `podium-judge` unit, which is why the sandbox cap sits on `isolate.slice`. A 1 GB swap file protects the server from a transient spike; boxes cannot swap (isolate sets `memory.swap.max=0` on every box with a memory limit).

## Decisions of the v2 plan

What each decision of the v2 plan means for the people who use podium, as built. The records with their alternatives are in the planning notes, not in this repository; the reasons that matter are repeated in the sections above. "None for users" marks a decision about how the work was done.

### Backend and platform (D60 to D73)

| Decision | What it means for users |
|---|---|
| D60 SQLite stays | Nothing changes: one database file, one process, Litestream copies. Postgres comes back only if measured writes or size call for it |
| D61 Immutable revisions | What a student was shown is on record. A bank edit reaches a running contest only through "Update contest to revision N", and students see "Statement updated" with a diff of the statement |
| D62 Site audit | Owners see who changed a role, a domain, an account or a site setting, or erased a person, on People, "History" |
| D63 Hidden tests leave the VM only encrypted | A stolen bucket key or VM disk yields no readable tests; restoring needs the owner's private key, kept off the VM |
| D64 Protocol and toolchain per verdict | A half-upgraded install stops with a clear message instead of failing jobs; the judge reports its compilers at start and the server logs them |
| D65 Renamed slugs redirect | Old contest links pasted in class chats keep working |
| D66 Server-filled head for public pages (as amended) | Sign-in, privacy, contact and the 404 page have real titles, previews and status codes; private pages are never indexed |
| D67 Tagged, checksummed releases | Organisers can ask the VM what it runs (`-version`); a damaged tarball installs nothing. No tag exists yet (D236) |
| D68 Expand, then contract | A release only adds; the next removes. Rolling back across a schema change is a restore, and the rollback says so before it starts |
| D69 One deletion path for history | The retention periods on the privacy page are kept, and nothing else can delete history |
| D70 Monitoring without a resident process | The owner gets a phone alert for a failed unit, a full disk, a stale backup or a stalled judge, at no memory cost |
| D71 Contact form without a mail server (as amended) | Signed-in people write to the organisers from `/contact`; replies come from the organisers' own mail. Signed-out visitors use the address on the sign-in page |
| D72 Deploy windows | No deploy in the 48 hours before a contest, during one, or while its queue drains (enforced by D112) |
| D73 Not in v2 | No public standings, profiles or archives; no email; no `/v1` API prefix |

### Students (D74 to D79, D97)

| Decision | What it means for users |
|---|---|
| D74 Standing, not a trust score (amended by D110) | Students see a band ("Good standing" and the dated lines behind it), never a number; organisers see a rating out of 10 |
| D75 Manual review from a submission | A reviewer can write a private note on a submission or open a case from it; the student only ever hears of a case, never of a note |
| D76 Time limits and held notices | A student has 7 days from first opening a notice to reply (21 at most), with a reminder on day 5; reviewers have 14 days. No notice arrives during the student's live contest |
| D77 Outcomes, two people, appeals (amended) | An outcome needs a second reviewer; a zero for a problem changes the final board, and the results page says the board was updated; an appeal goes to someone who took no part |
| D78 In-app notices | An inbox with an unread count; no email, no push; no names, code or test data in any notice |
| D79 Progress from summaries | Students see their own progress after each final board; organisers see a name-sorted students table, a student page and a class view. Nothing is computed during a live contest |
| D97 Record view log | Every read of a student's record, case or notes by someone else is logged, and the owner sees per reviewer what they opened and decided |

### Frontend (D80 to D96)

| Decision | What it means for users |
|---|---|
| D80 Visual direction | Direction A (one page header, one type scale, calmer links) with one display figure: the student's own place on standings |
| D81 Organiser nav by job | Organisers see Contests, Problem bank, People (owner) and Inbox, and land on the contest list |
| D82 Token changes | Small text is 14 px; reveal rows are sized for a projector |
| D83 Theme picker in a dialog | The themes sit under "Theme and colours" in the account menu; v3 keeps four (V3-8) |
| D84 "Live, standings frozen" | Students no longer read "Frozen" as "the contest stopped"; organisers keep "Frozen" |
| D85 Privacy page drafted from the code | `/privacy` exists and lists what is stored and for how long, but nothing links it until the owner approves the text |
| D86 Typographic word-mark | "podium" in the interface face; no logo |
| D87 Resizable verdict panel | On a laptop the result pane can be resized or folded to one line; phone standings pin rank and name |
| D88 Workspace changes between contests | Workspace behaviour changes go live in a gap between contests, announced in the next contest's description (for the whole release, under D236) |
| D89 v1 bugs first | Public clarification answers reach every student; the console filters "Waiting or judging" and "Judge error" |
| D90 Frontend only, except progress | None for users |
| D91 Preload the 500 weight | Buttons and links keep their weight from the first paint: the 500 face and Fragment Mono are preloaded with 400 and 600 (P6 F8.2) |
| D92 One progress line on Your contests | After a contest ends, Home shows one line with the points so far and a link to "Your progress" |
| D93 Who sees progress | A student sees their own; owners and admins see any student's; co-organisers and the public see none |
| D94 No comparison on the progress page | No class median, percentile or classmates on a student's own progress (the organisers' student page has the median) |
| D95 Rank only in the table | Points and problems lead; rank is small text in the contests table |
| D96 Tags after the contest | Topic tags show on "Your progress" for ended contests only, without "ai draft" |

### The unified plan (D98 to D106)

| Decision | What it means for users |
|---|---|
| D98, D99, D101, D102 | None for users: phases, tracks, ownership, numbering, calibration notes |
| D100 Crawlers read what the server writes | Search engines and chat previews see only the public pages, each with one title, description and canonical address |
| D103 Queue health (the next-contest field was dropped) | The console shows waiting and judging jobs, the oldest wait, judge errors and unanswered questions. Nothing about contests is shown before sign-in (owner, 2026-10-19) |
| D104 Workspace polish in P2 | "Submitted, 3rd in queue", pacing as a status line and "Submit again" arrived before the behaviour changes |
| D105 Every deploy a tag | Not yet in force: nothing is tagged until the P0 drills pass (D236) |
| D106 Councils | None for users |

### Later v2 decisions that changed what users see

| Decision | What it means for users |
|---|---|
| D110 Rating and band | Organisers see and may edit a rating out of 10 with a reason; a band-lowering edit needs a second organiser; students see the band and dated lines and may appeal an edit. `podium-server semester-rollover` adds 0.5 a term to every student with no outcome |
| D111 Three AI providers | The wizard works with an Anthropic, OpenAI or Gemini key, with the provider's live model list or a typed model id |
| D112 Deploy guard and emergency override | Deploys wait for a gap between contests; a forced fix shows students "The site was updated at 14:02". The warning when publishing soon after a deploy is not built (council C1-016) |
| D114 What enters v2 from the college platform | A Rules link in the workspace rail, status words in its score cell, "N of M tried", and "Run on this input" on a failing sample |
| D115 Open tests | v3 removed open tests (V3-5): Run checks the samples and shows the first failing one in full; practice submissions show their first failing hidden test; a contest submission never does |
| D163 Term rollover on People | The owner archives a term's contests and disables accounts unseen for 180 days, after a preview |
| D167 Two people for every outcome | No single reviewer can action a case, whatever opened it |
| D226 to D228 | Cases are decided by proposal and confirmation; appeals are ruled where the decision lives; notes start from the console feed |
| D231 | A board a decision changed says so on every results view, with no mark on any cell |
| Council run 1 | A phone that changes network is not a second session; problems whose hidden tests practice has shown are marked in the bank and named in the publish confirmation |
| D259 No contract migration in P6 | None for users: the schema stays at 0014 |
| D276 Involvement rule on every path | A reviewer who is a student of a case with a classmate cannot read, write or copy the classmate's notes in that contest, read a manual case about them, or change their rating |
| D277 Drill builds and deploys | Drills use builds `v1.4.90` to `v1.4.92`; a deploy names its `vX.Y.Z` release before it builds; the VM is reset before the real release installs fresh |
| D278 Backups first | A broken copy off the VM never costs the nightly local backup or blocks an upgrade; backup folders follow `PODIUM_BACKUP_DIR` |
| D279 Shown problems stay | A problem whose hidden tests a published contest has shown stays in that contest, so the bank keeps warning before a graded exam |
| D281 Rollover as previewed | Applying a term rollover can disable only the accounts its preview listed |
| D283 Phone device id | Two phones of one model on one account count as two devices |
| D284 Tags only on green CI | A release tag builds only a commit whose CI passed, and never with the placeholder upgrade path |

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
