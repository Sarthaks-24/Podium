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

This file describes v3. The first release will be tagged `v3.0.0` and installed on a fresh VM; nothing earlier was ever installed, so there is no upgrade from v2 (V3-12). "The v3 choices" near the end says what was cut from v2 and why.

## Services

| Service | Runs as | Responsibility |
|---|---|---|
| `podium-server` | `podium` | Serves the embedded Svelte app, the JSON API, server-sent events, sign-in, scoring and the leaderboard cache. Owns the SQLite database. |
| `podium-judge` | `judge` | `podium-judge work` claims jobs from the server's judge API (runs, submissions, validations and generation), compiles and runs them inside isolate, reports per-test results. Cannot read the database directory. |
| `isolate` | setuid root, invoked by `judge` | The sandbox: namespaces, control groups, seccomp. |
| Caddy | `caddy` | Automatic HTTPS, compression, reverse proxy. |

Server packages (`server/internal`):

| Package | Responsibility |
|---|---|
| `config` | environment variables, run modes and the start-up validator |
| `db` | SQLite and migrations |
| `httpapi` | the route table with the role each route needs, and the middleware: sign-in and sessions, rate limits, the cross-site check, security headers; the event stream |
| `httpx` | what every handler package shares: JSON answers and errors, body decoding, the client address, request logging and panic recovery |
| `ops` | the health check and the organisers' metrics |
| `webui` | the embedded web app; the prerendered landing page at `/` for a request without a session cookie (v4), every other page `noindex`; `robots.txt` and `sitemap.xml` |
| `auth` | Google sign-in, the list of people who may sign in, sessions, the four roles and who may change whose account (`rules.go`), the sign-in switch, testing-mode sign-in; the site audit, site settings, account lookup and erasure (`forget-user`) |
| `problems` | problems, pack format, content hash, validation rules and summary, output generation, generators and the generate inputs chain, AI drafts, revisions |
| `submissions` | runs, submissions, drafts, rate limits, scoring, what each viewer may see, integrity counts |
| `events` | the server-sent event hub: streams per user, fan-out, slow clients dropped |
| `jobs` | the one queue of judge work: leases, results, uploads, sweeps, change notifications |
| `jobs/judgeserver` | the judge worker API, on its own listener |
| `judgeapi` | request and response types shared by server and judge |
| `judge` | sandbox driver, verdicts, built-in and testlib checkers |
| `judge/calib` | host calibration and the hostile-program suite |
| `worker` | the judge's claim loop, blob cache, compile cache, and the validate, run, submit, generate and generate_inputs jobs |
| `contests` | schedules, access, standings, clarifications, export, integrity retention; the contest audit, pins by revision, queue health |
| `integrity` | the similarity check after a contest (lexers, winnowing, signals), the pairs export, one decision per student, and the retention of checks |
| `retention` | how long each kind of data is kept (`periods.go`) and the run payload step of the retention pass |
| `schedule` | the one scheduler that runs the server's background jobs (job sweep, session sweep, AI key sweep, retention) |
| `ai` | the AI wizard's model side: the in-memory key store, the Anthropic, OpenAI and Gemini clients, the problem schema and prompts, answer checks; the testing-mode fake is in `ai/fakeai`, built only with the tag `fakeai` |
| `buildinfo` | what a build is: version, commit, date (set by `make release`), schema and judge protocol, for `-version` and the release `MANIFEST` |

Handlers live next to their domain: `auth`, `problems`, `submissions`, `contests`, `integrity` and `ai` each keep theirs in `http.go` (and `http_*.go`), and `httpapi` mounts them. A domain service owns its SQL; no handler queries the database.

## Run modes

`PODIUM_MODE` (decision D48) sets how strict both services are:

| Mode | What it is for | What changes |
|---|---|---|
| `testing` | trying podium, automated end-to-end tests | No Google: a form signs anyone in as any role. The base URL and listen address must be on this machine, so the server refuses to start anywhere else. No secret is needed: the session secret is random per run and the judge uses a public testing token. The web app shows a banner on every page. |
| `dev` | local work with real Google sign-in | Google client and session secret required; http base URL allowed; the judge may use the testing token. |
| `production` (default) | the VM | Everything required, the base URL must be https, the testing judge token is refused. |

Why: before modes, running podium at all needed a Google OAuth client, which blocked quick trials and made browser tests against a real server impossible without real accounts. Testing mode removes every key but is only safe on loopback, so the validator enforces that instead of trusting a warning. Production is the default so a missing setting fails closed. The judge listener is loopback-only in every mode, which is also why a public testing token is acceptable outside production.

## Judge protocol and jobs

One protocol carries every kind of judge work (D44).

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
| Claim rules | Strict priority with three exceptions. After a run is claimed, the next claim takes other work first when any is waiting, so students pressing Run (priority 10) cannot starve submissions: at least every other claim is a submission. Jobs of a paused contest are skipped. While a contest is live, at most one validation or generation job (outputs or inputs) is leased at a time, so setters' work never takes more than one lane from contest judging. |
| Upload budget | A generate job's upload slot is reserved, under the queue's lock, before the body is read and given back if the upload fails, so concurrent uploads under one lease cannot exceed the budget. The body streams to a temporary file in the blob directory while it is hashed and is renamed into place only if the hash matches, so uploads cost disk, not memory. When the job closes or its lease is swept, uploads that no problem recorded (and no other open job holds) are deleted. |
| Results | One `job_results` row per compile or test result, so a report is an insert, and a unique key on (job, kind, test, role) refuses a result reported twice. |
| Leases | A claim returns a random lease, stored only as its SHA-256. It lasts 60 s and every report renews it; the worker sends an empty report every 20 s during long runs. A sweep every 10 s requeues jobs whose lease expired and drops their partial results; the third expiry fails the job. A report with a stale lease gets 409 and the worker drops the job. |
| Deadlines | Set at the first claim from the job's size (for validation: 4 min plus 3 min and six times the time limit per test, at most 2 h), per attempt: a worker that keeps renewing cannot hold a lane forever, and a job requeued after a judge crash gets a fresh deadline at its next claim instead of failing at once. |
| Blob scoping | A blob is served only with a current lease on a job that needs that file. A leaked token alone does not expose the problems. The judge re-hashes every download. |
| The server decides | Every report is checked against the job's payload (known tests and roles, possible numbers, sizes). The final verdict on a problem is computed by the server from the stored results, never taken from the judge. |
| Loopback listener | The judge API is a second listener, `PODIUM_JUDGE_LISTEN`, which must be a loopback address and is never proxied by Caddy. A judge on another machine reaches it through an SSH or WireGuard tunnel. Bearer token of at least 32 characters, compared in constant time on SHA-256 digests so the comparison does not leak its length. |

Alternatives rejected: the server running isolate itself (breaks the rule that the server never runs untrusted code; setter and AI-written code is untrusted); an offline `podium-judge validate` over SSH (fellow teachers have no shell, and the server could not verify an uploaded result). Judge workers on lab PCs were also rejected: the token reports verdicts and never goes on machines students can reach.

## Versions, toolchains and the judge protocol

**What a build is.** Both binaries print five lines with `-version`: the release, the commit, the build date, the schema (the highest migration it carries) and the judge protocol. `make release` writes the same facts into `MANIFEST` in the tarball, with the list of migrations and the oldest release this one upgrades from, and a `SHA256SUMS` of every file. `install.sh` checks the sums before installing, and `upgrade` refuses an installed release older than `upgrades_from`, and a downgrade (rollback is the way back). `/healthz` adds the version and schema only for a caller on the VM itself: through Caddy the release stays private, so a scanner cannot match it against known bugs.

**Judge protocol.** `judgeapi.ProtocolVersion` (3, which added the revealed test; a claim without the field is 1) travels on every claim and toolchain report. The server answers any other version with `426` and "judge protocol 3 needed, this judge speaks 2: install the same release on both". The judge stops on that with exit status 3, which its unit does not restart, because no retry can fix it; a judge newer than the server reads the old server's 400 (unknown fields) or 404 (no toolchain route) the same way. Without this, a half-done upgrade would leave a judge looping on claims it cannot read, and every submission queued. A refused token (401) is treated the same way after five in a row: the judge exits with status 4, also not restarted, so a token mismatch fails the unit and alerts instead of retrying silently.

**Toolchain report.** At start the judge reports the first line of `g++ --version`, `javac -version`, `python3 --version`, `node --version` and `isolate --version`, by the same paths the language table runs, and the kernel release. The server checks the report, logs the versions and answers with their hash. v3 removed the registry that stored each report and flagged problems validated on another compiler (V3-4): one college VM changes compilers only when the owner upgrades, and `install.sh hold` stops apt from moving the compilers, runtimes and isolate during a contest season. The judge protocol is unchanged (V3-9).

**Pack format.** `problem.yaml` carries `format: 1`, or `format: 2` (the v2 format with open tests; v3 imports them as samples and exports format 1); a missing line means 1 and a higher number is refused with both versions named, before its unknown fields are listed, so a pack from a newer Podium fails with the cause rather than a list of symptoms.

**Sandbox proof on the host.** `podium-judge selftest` runs the hostile-program suite and a hello world per language in box 80 and prints PASS or FAIL per program; it is the sandbox tests shipped in the binary, for the VM where there is no Go toolchain. CI runs the same suite and `selftest` on GitHub's ubuntu-24.04 runner, with isolate installed by `install.sh`'s own steps.

## Problems and validation

Problems live in SQLite (`problems`, `subtasks`, `testcases`, `blobs`, `jobs`, `job_results`, `validations`). Sources and statements are columns; test files and, when a validation is queued, program sources are content-addressed files in `PODIUM_TESTS_DIR/<first two hex digits>/<sha256>`, written once and never changed, so the judge caches them by name forever.

**"Validated" is a content hash (D45).** The server hashes a canonical manifest of everything validation depends on: limits, checker, `epsilon`, checker source, reference, brute force and validator (language and source hash), samples, subtask points and ranges, and every test's input and output hashes. A problem is validated when this hash equals the one recorded by its last passing validation. Any edit to those fields changes the hash, so the problem shows as not validated without anyone clearing a flag; title, slug, statement, tags, difficulty and subtask titles are left out, so fixing a typo keeps a problem validated. The judge build is recorded with each result but not hashed, otherwise every judge upgrade would unvalidate every problem; instead, a failing re-run of exactly the validated content withdraws the validation. Contest setup checks the hash.

**The gate.** A validation compiles and runs the reference, an optional brute force, an optional input validator and a custom checker on every test, inside isolate. It passes when everything compiles, the reference is accepted on every test, the validator accepts every input, the brute force never disagrees, and the reference's slowest test takes at most half of its time limit (with its language factor). That last rule turns the authoring guideline "limit at least 3 times the reference" into a check: fail above a half, warn above a third. One open validation per problem: asking again for the same content returns the waiting run, and a queued run for older content is replaced.

**Packs are untrusted input (D46, as amended).** They come from setters and from a model, and the server parses them outside any sandbox. Caps: 32 MB zipped, 128 MB unzipped, 2,000 entries, 16 MB per file, `problem.yaml` 64 KB and parsed strictly. Sizes are counted on the bytes actually decompressed rather than limited by a compression ratio, which would refuse legitimate repetitive tests. The entry count is read from the zip's end record before the archive is decoded, because Go's zip reader allocates every entry up front and a small zip can declare hundreds of thousands. Absolute paths, `..`, backslashes and non-regular files are refused. Only one pack operation runs at a time, since each can hold over 100 MB on a 2 GB VM.

**Output generation.** A pack or test zip may carry inputs without outputs; such a test has `output_sha` NULL and the problem lists it under `problems`, so neither validation nor judging can start. Validate makes the missing outputs first ("Regenerate all outputs" in the editor's More menu replaces them all): it queues a `generate` job: the judge compiles the reference, runs it on each input in isolate with the problem's limits, and uploads each output with `PUT /api/judge/blob/{sha}`. This is the one route where data flows from the judge into test data, so it is fenced in:

| Rule | Why |
|---|---|
| Uploads only for a `generate` job, under its current lease | A leaked token alone cannot write into the store. |
| Budget per job: one upload per input, plus 2 | A worker cannot fill the disk through one job. |
| At most 16 MB per output, stored only if it hashes to its name | The same cap as a test file in a pack; the name is the content. |
| A report may name only outputs this job uploaded | Otherwise a worker could point a test at any file already in the store, such as another problem's hidden output. |
| Applied only if the problem's content hash is unchanged since queuing | Outputs computed for an older reference or older inputs are never mixed in. |
| One generation per problem at a time; "Regenerate all outputs" asks to confirm | Replacing outputs is the one action here that destroys setter data. |
| Applied outputs change the content hash | The problem must pass validation again before a contest can use it. |

**Tests from a generator.** A problem may carry a generator (setter code in any of the four languages, usually Python) and a plan: a list of tests, each with its subtask, a seed and arguments (`problems.generator_language`, `generator_source`, `generator_plan`, and `subtasks.generator_args` for the arguments the editor proposes). "Generate inputs" queues a `generate_inputs` job (priority 85): the judge compiles the generator, runs it in isolate once per planned test with argv seed then arguments and an empty stdin, and uploads each output through the same `PUT /api/judge/blob/{sha}` route and budget rules as output generation (one per planned test, plus 2). When every planned test got an input, the job's `Finish` stores them as tests and, in the same transaction, queues output generation from the reference with `then_validate` set; that job's `Finish` queues validation when every output arrived. One press runs the whole chain, and each step reports its own failures by test, subtask and seed.

Arguments are limited to letters, digits and `_ . , : = + -` (64 characters, 16 per test) and seeds to a signed 32-bit integer. Braces are excluded because the judge substitutes `{mem}` in run commands; the arguments reach the program as argv after isolate's `--`, never through a shell. The worker checks them again before building a command line.

**The generator is not part of the content hash (D53).** The content hash covers the tests the generator made, which is what students are judged on. Hashing the generator too would unvalidate a problem when someone tidies the generator without making the tests again, although nothing a student sees changed. Instead a separate plan hash (generator language and source hash, plus the plan) travels in the `generate_inputs` payload, next to the content hash; `Finish` applies nothing if either changed while the job ran, so inputs from an older generator are never mixed in. Editing a generator therefore leaves validated tests alone until someone presses Generate inputs again.

## AI wizard

The `ai` package (three providers, D111) is the model side: the key store, the provider clients, the problem schema and prompts, and the checks that turn a model's answer into a draft. `ai/http.go` holds the routes' handlers and the in-memory wizard runs; `problems` turns a checked answer into a draft and runs the generator chain.

```
admin picks a provider, pastes key --> PUT /api/admin/ai/key --> ai.NewSecret at once
    no model yet: ListModels (the check, and the picker's list)   with a model: Check (model lookup)   neither costs tokens
    --> ai.Keys (memory, per session and provider, 2 h)
admin: brief or text --> POST /api/admin/ai/wizard --> slot (one per admin) --> goroutine, 3 min:
    ai.Generate: one structured call, checks, at most one retry
    --> problems.CreateDraft (origin ai, tag "ai draft") --> RequestInputGeneration
    --> generate_inputs --> generate (then_validate) --> validate          (all on the judge, in isolate)
```

**Three providers, one interface (D111).** `ai.Provider` has `Generate`, `Check` and `ListModels`. All three clients are `net/http` and the standard library: Anthropic (the Messages API, streamed), OpenAI (the Responses API) and Google Gemini (`generateContent`), because an SDK would add megabytes and dependencies to a 2 GB VM for a few HTTP calls; the Anthropic SDK went in v3 and took 13 indirect modules with it. The clients share one helper: a JSON request with a bounded answer, two retries on 5xx (and on OpenAI's plain rate limit, not on a used-up quota), no redirects followed (the key is a header and must not travel to another host), and errors that never carry the body or the URL.

| | Anthropic | OpenAI | Gemini |
|---|---|---|---|
| Key header | `x-api-key` | `Authorization: Bearer` | `x-goog-api-key`, never in the URL |
| Structured output | `output_config.format`, JSON schema | `text.format` `json_schema`, `strict: true` (every property required, `additionalProperties: false` on every object) | `responseSchema`, an OpenAPI subset: types upper case, no `additionalProperties`, every property required and ordered with `propertyOrdering` |
| Model filter (no ids in code) | `capabilities`: structured outputs, adaptive thinking, effort `high` | id family: `gpt-*` and `o<digit>*`, without pre-structured-output and audio, image, embedding, search, moderation models | `supportedGenerationMethods` has `generateContent`, without embedding, image, speech, live, Gemma and AQA ids |
| Refusal | `stop_reason` `refusal` with its category; server-side fallback where offered | a `refusal` content item, or `incomplete` for `content_filter` | `promptFeedback.blockReason`, or `finishReason` `SAFETY`, `PROHIBITED_CONTENT` and the like |
| Cut off | `max_tokens` | `incomplete`, `max_output_tokens` | `MAX_TOKENS` |
| Not stored by the provider | | `store: false` | |

The wizard's schema is written once as JSON schema (`PackSchema`) and translated per provider. Each provider's failures map to one sentence each for a refused key, a used-up quota or credit, an unknown model and no access, in the provider's own words (`docs/API.md`, "AI wizard (admin)"). A model id is free text the admin types, checked against `^[A-Za-z0-9._:-]+$` so it cannot change the path of a request, and proved by the provider's own model lookup; the filtered list is only a hint, because providers name new models faster than any list in code. The list is cached in the session's key entry, so the picker costs one listing per key.

**The Anthropic client (checked 2026-10-20 against the Messages API).** Requests stream with `max_tokens` 64,000, send `thinking: {type: "adaptive", display: "omitted"}` explicitly (leaving it out means no thinking on some models the list offers, such as Opus 4.8) and effort `high` (Opus 5.5 and Haiku 5.5 default to medium). No deprecated parameter is sent: no `budget_tokens`, sampling parameters or `output_format`, and no assistant prefill (the retry ends with a user turn). The fixed system prompt, which carries the pack format rules, has a cache breakpoint (`cache_control: ephemeral`); a prompt below the model's minimum cacheable length silently does not cache, which costs nothing. Billing errors (402, or a 400 about the credit balance) are the no-credit sentence. The owner's answer to input 27 is applied: no default model; the admin chooses from the live list or types an id.

**The key never touches disk (D50).** The key is pasted by an admin, never configured: there is no environment variable, so the owner never has to put a provider key on the VM, and an organisation with no key loses nothing but the wizard. On the key route the request body is decoded into a local string, wrapped in `ai.Secret` and the string cleared before anything else runs. `Secret` prints, formats, marshals to JSON or text and logs (`slog.LogValuer`) as `[redacted]`; only the `ai` package can read the value, to set the one key header of a request (`X-Api-Key` for Anthropic, `Authorization` for OpenAI, `x-goog-api-key` for Gemini); nothing is read from the environment. Keys sit in a map keyed by the SHA-256 of the session cookie and then by provider, for 2 hours each; a sweep every 10 minutes drops expired ones, signing out (or signing in again in the same browser) drops the session's key, and a restart drops them all. Provider errors are mapped to fixed sentences: the response body is read for its error code only, since a careless proxy could echo the key in it, and is never wrapped, formatted or logged. Job payloads hold only the generator, the plan and hashes. Request bodies are never logged. An end-to-end test runs the wizard with a real-looking key for each of the three providers against stand-ins for their APIs and then searches every log line (debug included), the SQLite database and WAL files and the blob store for it.

**One structured call.** Each provider makes one call per attempt; the Anthropic one streams, and the client puts the stream back together. The answer's shape is enforced with structured outputs: `output_config.format` carries a JSON schema for the whole pack (statement, samples, subtasks with generator arguments and test counts, C++ reference, Python brute force, generator and validator, checker, limits, tags, difficulty) with `additionalProperties: false` everywhere. Lengths and ranges are not expressible there, so the server decodes strictly and then checks the wizard's rules (one to five samples, points adding up to 100, at most 30 planned tests) and the editor's own field rules on the draft the answer would become. A failing answer is sent back once with the list of reasons; a cut-off answer (`stop_reason` `max_tokens`) is retried once with a request for a shorter answer; a refusal (`stop_reason` `refusal`) is reported with its category and not retried.

**Refusal fallbacks (D54).** For the models that accept it (Fable 5.1, Opus 5.5, Opus 5, Sonnet 5.5; a table of a feature, not a choice of model) requests carry the beta `server-side-fallback-2026-07-01` with `fallbacks: "default"`, so a request refused by the safety classifier for a reason that does not apply to a fallback model is answered by that model instead of failing (contest problems about "attacks" on a grid or "exploiting" a recurrence are ordinary). A mid-stream fallback restarts the text, and the client keeps only the fallback's answer. Haiku 5.5 has no fallback. There is no default model: the admin picks one from the live list or types its id (D111).

**Prompt injection.** The system prompt is fixed text. The brief or pasted text goes into the user message inside one `<brief>` or `<problem_text>` block, the system prompt says that everything inside it is data, and any tag in the data that could close the block early is defused. The worst a hostile text can do is make the model write a bad problem, which the checks, the judge and the admin's review catch: the model's code is never run on the server, only on the judge in isolate, like any setter's code.

**Nothing AI-made reaches students unreviewed.** A draft carries the tag `ai draft` until a person saves it. The chain may validate the draft on its own, but a contest refuses a problem still tagged `ai draft`, and only validated problems can join a contest at all.

**Model solve was removed in v3 (V3-4).** It sent the statement to a model and judged the answer; v3 keeps the wizard to drafting problems only.

**Limits.** One AI request per admin at a time (a second is refused, not queued, so a double click cannot spend twice), 3 minutes per request with its retry, 20,000 characters of pasted text. Wizard runs live in memory for an hour after they end. In testing mode a `fake` provider answers with two canned packs whose programs really work on the judge, so the whole pipeline is tested without a network or a key. It lives in `internal/ai/fakeai`, which tests and builds with the tag `fakeai` (`make dev`, `make dev-test`) link; the release binary does not, so neither the provider nor its packs ship to the VM.

**Cost on the VM.** No provider SDK and nothing at rest: no goroutine runs until a request does. All three providers stay, although each organiser uses one: not every organiser has a key at the same provider (owner, 2026-10-26, V3-7 rejected).

## Submissions and scoring

`submissions` holds one row per Run or Submit, with its job, state, verdict, score, the full result as JSON once done, the client key and the integrity counts; `drafts`, `generations` and `integrity_events` sit beside it. Outside contests the workspace is reached only by organisers ("Try as a student"), so the problems are never exposed to students; contests mount the same workspace for students with contest rules (see "Contests"). A run row keeps only the hash of its code and no copy of its custom input: both are in the blob store, and 128 KB inline per run would make every scan of a user's rows walk overflow pages while holding SQLite's single writer lock.

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

**Judging order and stop rules.** The payload lists the samples first, then each subtask's tests in subtask order. A failing sample stops the whole submission (the payload flag `stop_on_sample_fail`, always set: v3 cut the per-contest switch, V3-15); a failing test stops the rest of its subtask, since the subtask is lost anyway. Every test not run is reported as `SK`, so the result always has a row per test.

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
| Gap between submits | per user and problem: 5 s for organisers trying problems, a contest's own gap otherwise (V3-17) | A student who finished A should not wait to submit B; the per-problem attempt limit still bounds the total. |
| Pending runs, gap between runs | one per user; 2 s | Runs have the highest priority, so they need their own brake. Both checks read only the `submissions_by_kind (user_id, kind, state, created_at)` index. |
| Code and custom input | 64 KB each | Keeps payloads, blobs and compile times small. |
| Draft saves | one per user every 2 s | Each save takes the writer lock; a script saving in a loop would hold up judge reports for everyone. |
| Integrity events | one batch per user every 5 s, 100 per request, 5,000 per user per problem | Bounded storage; beyond that more events tell a reviewer nothing new. Kinds, rate and cap are checked before the write lock is taken. |

Every refusal says how long to wait, in the message and as `retry_after_s`. The browser makes a `client_key` per press of Submit and reuses it on a retry; the server looks the key up inside the same transaction as the rate checks, scoped to the user, problem and contest, and answers a repeat with the first submission (200), shown as the viewer may see it. A retry after a lost answer therefore never counts twice and never trips the limit it would otherwise hit.

**Integrity counts (D47).** Each submission stores the typed and pasted character counts and focus losses since the previous one; paste, blur and focus events go to `integrity_events` in batches, outside the submit path, so they never slow or block a submit. Counts only: no keystroke timing and no replay. The client hash is an HMAC of IP address and user agent keyed by the session secret, enough to notice two people on one browser, not reversible by trying campus address ranges. Nothing here scores or flags anyone; it is evidence for the organiser who reads Similar code after the contest. A contest with the exam option "count tab switches and focus loss" or "full screen on start" also shows the counts beside each submission in the console feed, and `fullscreen_exit` events count the times a student left full screen (V3-25).

## Contests

`contests` holds the schedule, the rules and the exam options; `contest_problems` the label, order, pinned revision and points of each problem; `contest_starts` when each student accepted the honour code and started; then `contest_extensions`, `clarifications` and `announcements`. The code is `server/internal/contests`; the contest workspace reuses `server/internal/submissions` with a scope built from the contest.

**Problems are pinned.** A contest takes only validated problems ("Add to contest" in the problem editor, or the problem list in Setup), and adding one pins its newest revision and its subtask points; the board scores against the pinned points and clamps any earned value to them, so an edit to a problem never changes a contest's board by itself. Only owners and admins organise contests. A problem is ready only while both its content hash and its validated hash still equal the pin. Publishing, running, submitting and rejudging all require that: a test edited mid-contest stops judging of that problem (`409 not_ready`) until it is validated and the contest is updated with "Update students now" (see "Problem revisions and pins").

**Addresses.** A contest's address is made from its title on create and on duplicate, numbered when taken, and never changes after that, so links shared in class always work and no redirect is needed (V3-4, V3-15).

**Time.** Every rule uses the server's clock, and every answer carries `server_now` so the page can show a correct countdown from a wrong laptop clock. Everyone starts together: a student's time runs from the start to the end, plus any contest-wide extension, plus their own extra time. v3 cut the per-student start window (V3-15): it was one of the 25 inputs on a setup page measured at 2,326 px, and no first contest needs it.

Submissions count while the student's own time is open. After it they are practice (judged at priority 60, never on the board) when the contest allows practice and the contest is over for everyone (the close plus the longest personal extension), so nobody tests solutions against hidden tests, with full feedback, while a classmate with extra time is still competing. Otherwise run and submit are refused. Problems open once the student has accepted the honour code and the contest has started, and stay readable after it closes. Every contest counts: v3 removed the "graded" switch and live ungraded contests (V3-6), which closed a security finding about practice revealing tests of a problem still in use and settled three open questions of v2.

**Access.** Anyone who can sign in may enter a published, unarchived contest: sign-in already requires a place on the list of people, so that list is who takes part (V4-15; v3 cut the roster and its CSV, V3-15). Organisers, admins and owners organise it and are never on the board. Every id in a contest route is resolved through the contest in the URL (a label to a problem id, a clarification or submission id checked against the contest), so ids from another contest give `404`, and students get `403` on organiser routes before any contest is read.

**Scoring.**

| Rule | Detail |
|---|---|
| Best per subtask | Per problem, the sum over subtasks of the best points earned on that subtask across the student's non-practice submissions. Two partial submissions can make a full score. v3 cut the "best single submission" option (V3-15). |
| Rank | Total, highest first; ties broken by the earliest time, from the start, at which the student reached their final total; equal totals and times share a rank. |
| Freeze | The frozen board counts only submissions made before the freeze: one moment for everyone, the close minus `freeze_minutes`. A personal extension never moves it, or a student's first minutes of the freeze would show to everyone. Later submissions show as "+N" pending in their cell. End now during the freeze keeps the frozen moment: the remaining freeze is rounded up to whole minutes, never down to zero (V3-18). |
| Excluded students | A student an organiser excluded from the contest (see "Integrity") leaves the board and the export; the others' ranks close up. |

**Which board a student sees.** The full board until the freeze; the frozen board from the freeze until the contest is over for everyone; then the full board. With `hold_reveal`, the frozen board stays until an organiser reveals it. Organisers always see the full board, and the students' board on request. The reveal opens once the contest is over for everyone: one Reveal button on Results, after one confirmation, shows students the full board (V3-16 hid the step-by-step reveal walk and the editorial: they are not jobs for a first contest).

**The board cache.** Standings are a pure function of the contest, its problems and its submissions (`contests.Compute`, tested against hand-computed boards), cached in memory per contest and variant. Anything that changes a board (a verdict, a rejudge, a pin change, an extension, a new participant, an integrity decision) bumps the contest's version; a new submission does not, since it changes nothing until its verdict. Archiving a contest drops its cached boards. The work is bounded however many people look at once:

| Rule | Effect |
|---|---|
| One computation at a time per contest and variant | Readers arriving meanwhile get the last board, or wait for the running computation when there is no board yet or it is already for the current version. Measured: 100 concurrent reads after a change compute once. |
| At most one recompute every 2 s per contest | Inside that time the last board is served and one refresh is scheduled; a burst of verdicts costs one recompute. |
| Refresh, then tell | After a change the server recomputes the boards that are being watched, then sends the `standings` event, so 700 browsers refetch a board already in the cache. |
| Index-only read | Each judged submission stores its earned points per subtask in a small `earned` column, written when judging finishes; the board reads only the `submissions_by_board` index, never the rows with their inline code and full results. Measured: about 34 ms for 300 students with 40 submissions each (it was 0.87 s reading the rows). |

Why in memory and not a `standings_cache` table: one server, a few thousand submissions per contest, and a recompute measured in milliseconds. A table would add writes on the judging path for no gain, and a stale row would be a correctness risk after a restart.

**Feedback.** Students see subtask results and per-test verdicts with coarse figures; displayed times round to 100 ms for a student in a contest and 10 ms otherwise (D47). Hidden tests never show input or output. v3 cut the exam feedback mode, which showed only "Submitted" until the end (V3-15).

**Exam options (V3-25).** Four switches under Advanced in Setup, all off by default: full screen on start (exits are counted), count tab switches and focus loss, block paste into the editor, and block copy out of the statement. Each is for an examination; a contest with none on is an ordinary contest. Blocking happens in the browser, and the server records and counts. Nothing is blocked silently: before Start and in Help one line says which measures are on. They apply only during a student's own contest time, not in practice or in the organisers' student view. Why: the owner asked to keep lockdown in the organisers' hands as options, so the editor stays ordinary by default and the measures are there when an exam wants them.

**Running a contest.** Pause stops the judge taking the contest's jobs (they queue; nothing is lost). Extend adds minutes to everyone's end, and is refused once the contest has closed (`409 contest_ended`), since it would reopen it; extra time adds minutes for one student. Rejudge queues a problem's submissions (or one) again at priority 50 with the current tests and clears their results, so the board counts each again once judged; a waiting rejudge never blocks the student's own next submission. Submissions still waiting or being judged on the old tests are included: their job is cancelled and replaced, and a job's Finish updates its submission only while it is still that submission's job, so a replaced job can never overwrite the new result. Targets are chosen inside the same write transaction, and one already waiting on exactly the current tests is skipped, so two rejudges at once do the work once. Clarifications are private to the asker until an organiser makes the answer public; students never see who asked. Announcements and answers arrive as live events.

**Second sessions (D47).** A second event stream opened from another browser or machine while one is open is recorded as a `second_session` event for every contest that is live for that student, at most once per 10 minutes per contest. A device is the browser plus the network address, except for a phone or tablet browser, which is the browser alone: a phone moving from wifi to cellular keeps its old stream open until TCP gives up, and would otherwise look like a second machine. A phone that sends its browser's `?device=` id is told apart by it, so two phones of one model on one account are two devices. The retention pass, run a minute after start-up and every 12 hours, deletes integrity events and clears submissions' client hash, typed and pasted counts, focus losses and full-screen exits 180 days after a contest ended for everyone.

## Problem revisions and pins

A contest pins a problem revision, not the editable problem, so an edit in Problems mid-contest never changes what students see until an organiser says so.

| Table | Holds |
|---|---|
| `problem_revisions` | One row per version of a problem: number, content hash, title, statement and its hash, limits, checker and its source hash, the language and SHA-256 of the reference, brute force, validator and generator, samples, subtasks (points, titles, ranges), author, time and reason. Programs are kept by hash only; tests are listed in `revision_tests` by blob hash, so a revision costs a few hundred bytes plus its statement. |
| `revision_tests` | Each test of a revision with its kind: `sample` or `hidden`. |
| `contest_problems.revision_id`, `submissions.revision_id`, `jobs.revision_id` | What a contest pins and what each submission and job was judged against. |

**Every write makes its revision.** The `problems` row stays the editable working copy. Every write of a problem, its subtasks or its tests goes through two functions, `insert` and `save`, which write a revision in the same transaction when anything a revision records changed; a save that changes nothing (or only tags, slug, difficulty or the generator plan) writes none. Generated inputs and outputs are saves too, with no author and a fixed reason. A test parses every Go file of the server and fails when anything else writes those tables, and `problems.CheckRevisions` (run by the tests and by `podium-server check`) fails when a working copy differs from its newest revision. While a live contest pins the problem, a change needs a reason, kept on the revision.

**"Update students now".** The workspace serves the pinned revision's title, statement, limits and samples, never the working copy. After a validated edit, the console's banner offers "Update students now", which accepts only a revision that is the validated content of the problem now (its content hash equals the problem's, and a passing validation covers it); it moves the pin and the subtask points in one transaction with a `revision` row in the contest audit, sends students a `statement` event, and offers the organiser a prefilled announcement to post. A statement-only update needs no rejudge; any other needs validation first and a rejudge after, and the console says which. Judging reads the working copy, which equals the pinned revision by hash whenever the problem is ready, and a rejudge records the current pin's revision on its new jobs while the old job keeps its own. v3 removed the revision lists, statement diffs, pin history and served log (V3-4): each served a case no single-college contest has met yet, and students learn of a change from the announcement.

**Append only.** `problem_revisions`, `revision_tests`, `site_audit` and `contest_audit` have triggers that refuse every update and delete; v3 deletes none of those rows. Foreign keys keep a pinned or judged revision from being deleted, and the blob collection keeps the test files of every pinned or judged revision.

## Audit logs

Two append-only tables, written in the same transaction as the change they record.

| Log | Records | Read on |
|---|---|---|
| `contest_audit` | Every organiser action that can change a contest's results: settings, problems, "Update students now" (`revision`), extra time, extend, pause and resume, rejudges, the reveal, the editorial, archive, AI confirmations and integrity decisions | The console's "History of changes", filterable by action and actor; the JSON export |
| `site_audit` | People and the site: roles granted and revoked, people added to and removed from the sign-in list, refused addresses let in, the sign-in switch (and, in rows from before v4, domains added and removed), accounts disabled and enabled, sessions ended, Google links reset, run mode changes (the server's own, with no actor), site settings changes and erasures (`user_forgotten`) | Settings, opened from the Organisation tab, owners only, filterable by action, email and time |

Each row keeps the actor's email and role as they were. The actor rides on the request context from the session check, so a change records whoever made it without each handler passing it on. A row's details are fixed fields (roles, domains, counts, emails); nothing from a request body, header or cookie is copied into it, so no AI key, session token, cookie or OAuth code can land there, and a test sends fake ones through every audited path and searches both tables and the database file for them. An extra time row names the student by id only; its reason stays in `contest_extensions`.

## Integrity

`integrity_runs` holds one row per "Check integrity" press with its counts, `integrity_signals` the signals, and `integrity_cases` one decision per student per contest. The code is `server/internal/integrity`; the route handlers are in `server/internal/integrity/http.go`; the organisers' screen is Similar code, opened from Results after the end. The organisers' guide and the policy template are `docs/INTEGRITY.md`.

**What is checked.** An owner or admin starts a check once the contest is over for everyone (the close plus the longest personal extension). It reads each problem's judged, non-practice contest submissions by students; organisers' submissions are never evidence.

| Signal | Method |
|---|---|
| Similar code | Each student's best submission per problem and their last one below full marks. A lexer per language turns code into tokens with identifiers and literals collapsed and comments and layout dropped; winnowing (k=5, w=4, FNV-64) picks fingerprints. Fingerprints of the reference and brute force, and those more than a tenth of the students (at least 5) share, are dropped. Score is containment: shared fingerprints over the smaller program's. Programs under 40 tokens or 8 remaining fingerprints are skipped. Pairs at 0.6 or more are reported (weak; moderate at 0.75, strong at 0.9), with matched byte spans for the side-by-side view, and joined into clusters. |
| Same wrong output | Students grouped by problem, hidden test and the SHA-256 of their wrong output (reported by the judge). Outputs more than 3 students printed, or empty output, are ignored. Two shared tests are moderate, four strong. Works across languages. |
| Hard-coded answers | A hidden test's expected output (8 bytes to 4 KB, not a single small number) found in a source with whitespace normalised. |
| Probing | Six or more distinct verdict vectors on hidden tests from consecutive attempts whose code barely changed (ten is strong). |

The timeline (submissions with typed and pasted counts, pastes, focus changes, full-screen exits, second sessions, other students on the same browser and network) is context for the organiser and never a signal: lab machines are shared, and pasting your own template is allowed. Similar pairs download as CSV.

**Calibration.** The calibration test in `server/internal/integrity/similar_test.go` has six independent solutions to one problem in different styles and three disguised copies (renamed identifiers, reordered functions, changed comments and layout). The copies score 0.94 to 0.98; the most similar independent pair scores 0.27 against the 0.6 threshold, and the test fails if any independent pair comes within 0.2 of it. Short programs (A plus B) and programs everyone shares produce nothing.

**One decision per student.** An organiser records No action, Warning or Exclude from this contest for a student who took part, with a reason of at least 10 characters; a later decision replaces it. Exclude hides the student from the board and the export; a later No action or Warning brings them back. Each decision writes an `integrity_decision` row in the contest audit (the student by id and the decision; the reason stays in the case). There is no second reviewer, no deadline, no score of the student's conduct and no ruling after the decision: the college's own process handles the rest. Why: no comparable platform runs a tribunal, the v2 system was about 19 tables and 18,000 lines of code, and it was the source of the worst word clashes; the owner kept one small record of a decision and its reason (V3-2, owner answer B).

**Retention.** Integrity events, the per-submission counts and the checks with their signals go 180 days after the contest ended for everyone. Decisions are kept: their reason says what they rested on. Submissions themselves are not deleted.

**Why there is no AI score.** No tool reliably tells AI-written code from a student's own: detectors are wrong often enough to accuse honest students, and a number on a screen gets quoted as proof. Podium relies on prevention (fixed-start contests in a lab, the honour code, the exam options when wanted), indirect evidence (large pastes, a full score far faster than the class, clusters of near-identical code) and verification (the student explains or changes the code in front of an organiser). Signals are worded as strength, for similar code with its reason ("Short programs, identical" is weak however high the score, because honest pairs on small problems match completely), the raw numbers in the details, and nothing happens without a person.

**Why on the server (D49).** The check runs in one bounded background goroutine in `podium-server`, not as a judge job: its inputs are trusted text (sources, verdicts, output hashes) and nothing is executed, so the sandbox adds nothing, while sending every source to the judge would widen what the judge holds. One check runs at a time per server (`409 already_running` otherwise), with a 10-minute limit; the request returns at once with the run's id and the page polls. A check left running by a restart is marked failed at start-up.

**Guards against false positives.**

- Signal and run ids are never reused (`AUTOINCREMENT`): a re-run used to free ids that SQLite handed out again.
- Hard-coded answers skip expected outputs that appear in the reference, the brute force or the statement, or in the programs of more than max(3, 10%) of the students; each distinct source is squashed once.
- Probing ignores all-SK attempts, compares near-identity with the first attempt of a run, and counts only flips (AC back to a failure, or one failure to another), not forward progress.
- Similar code: strength needs an amount of shared fingerprints as well as the score; the common-fingerprint limit is counted per language. The shared base is the reference and the brute force only.
- Same wrong output: the common-mistake limit is max(3, 2% of the students).
- The lexer reads hidden code: the body of a C++ `#define` (only `#include` lines are dropped; line continuations join lines) and a Python string literal passed straight to `exec` or `eval`.
- An organiser named in a signal (a student since made an admin) never sees it.

## What students can see

Decision D47 sets the rule: hidden tests never show their input, output or checker comment, and the figures a student sees are coarse enough that they cannot be used to read hidden tests.

| Boundary | Rule |
|---|---|
| Judge report | A `submit` report carrying any output text, or a checker message on a hidden test, is refused. The text never reaches the server, so no later bug can display it. The one exception (D115) is a payload marked `reveal`, which the server sets only for submissions that never count: then the output of a failing hidden test (at most 4 KB) is accepted. Test verdicts are limited to `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` and `IE`. |
| Submission view | Verdicts, times and scores per test, never input, output or messages. Sample results come with the statement anyway. |
| Coarse view for students | No memory figure on hidden tests, maximum memory rounded up to 16 MB steps, times rounded (10 ms; 100 ms in contests). Applied on every path a student can reach: reading a submission, the workspace list, the answer to a submit or a retried key, and live events. |
| Progress while judging | Students see per-test verdicts only inside finished blocks (the samples, a whole subtask), and no running maximum time or memory, so polling shows no more than the stream. |
| Live events for students | Sent when the sample block or a whole subtask finishes, never per hidden test. |
| Runs | Show output, stderr and the expected output on samples and the student's own input. The first failing sample is revealed with its input. |
| Revealed hidden test (D115, owner's decision) | A submission that never counts (practice after the contest, unless another contest that pins the problem has not closed, drafts and upcoming ones included, V3-6; the organisers' workspace) shows its first failing hidden test to its author and to owners and admins, once judged. Never on a submission that counts; never in feeds, exports, the board or the standings. The consequence for setters is in `docs/AUTHORING.md` (checklist item 6). |
| Box output | Whatever a program does to its own output file (removes it, replaces it with a link or a FIFO, makes it unreadable) gives empty or cut-short output, so a verdict, never a judge error. Otherwise a program could choose, from the hidden input, which tests fail the whole job: a one-bit channel. |

Why coarse: a program can encode a few bits of a hidden test's values in how much memory it allocates or how long it runs, then read them back from exact figures, and repeat over many submissions until the test is known. The per-test event timing leaks the same way, since when an event arrives says how long that test took. Rounding to steps far wider than the signal, dropping per-test memory on hidden tests, and batching events per subtask close those channels while keeping what a student needs: which subtask failed, with which verdict, and roughly how close to the limits.

Owners and admins see every figure.

## Live updates

One `GET /api/events` stream per open tab, served by the `events` hub. The jobs service calls a change hook after every committed claim, report, close and sweep; the submissions service then reads the run or submission as its author may see it and publishes it to that user's streams.

| Piece | Design |
|---|---|
| Ordering | Updates for one job are serialised, so they arrive in order. |
| Only visible changes | An update is sent only when its signature (state, verdict, number of finished tests, or for students finished subtasks) differs from the last one sent, which also keeps a 50-test submission from producing 50 identical reads. Users with no open stream are skipped before any read. |
| Never blocks judging | Each stream has a buffer of 16 events. A stream that is full is dropped, not waited for; the browser reconnects and refetches. Every write has a 10 s deadline, so a reader that stops reading frees its goroutine. |
| Limits | 4 streams per user and 5,000 in total (then `503 stream_limit`, and the page polls). A fifth stream replaces the oldest, which first gets `event: replaced` so that tab stops reconnecting; without it, five tabs evicted each other in an endless reconnect loop. |
| Session | Checked again on every clock tick; a stream ends with its session (sign-out, expiry, disabled account). |
| Contest events | Standings versions, announcements, clarifications and new end times go to the connected users who belong to the contest, worked out with one query per event against the hub's list of connected users. There is no topic registry: membership (publication, the sign-in list, the organiser role) is read at send time, so a change of access applies to the next event. Standings events follow the board refresh, at most about one per contest every 2 s. |
| Setter events | `wizard` goes to the admin who started an AI request when it ends. `input_generation` goes to the admin who queued a generate inputs job on each change, sent by one goroutine fed by a buffered channel from the change hook, so events keep their order and a full buffer drops an event rather than blocking judging (the editor also polls). |
| Clock | `event: clock` with `server_now` on connect and every 25 s, instead of a comment ping: it keeps proxies from timing out an idle stream and lets the page keep an offset to the server's clock for contest timers. |
| Fallback | After three failed connections the page polls every 15 s. |

The public server has no write timeout so streams can stay open for hours; the read deadline is cleared on the stream itself. Caddy passes `text/event-stream` responses through without buffering, and the shipped Caddyfile also sets `flush_interval -1` and leaves `/api/events` out of compression.

## Identity

Sign-in is Google OpenID Connect, run entirely by the server (`server/internal/auth`):

1. `/api/auth/google/start` creates a random state, a nonce and a PKCE verifier, keeps them in a 10-minute HttpOnly cookie signed with `PODIUM_SESSION_SECRET` (named `__Host-podium_signin` on https, so a sibling subdomain or a plain http response cannot plant one and finish an attacker's sign-in in a victim's browser), and redirects to Google.
2. Google redirects to `/api/auth/google/callback`. The server checks the state against the cookie (and deletes the cookie), exchanges the code with the PKCE verifier and client secret, and verifies the ID token itself: RS256 signature against Google's published keys (cached, refetched for an unknown key id at most once a minute), issuer, audience, expiry, and the nonce.
3. The verified email must have `email_verified`. It may sign in only when it is on the list of people (every row of `users` is one person on it, V4-15) and the account is not disabled; while sign-in is closed (the Dashboard's switch), only admins and owners. Addresses compare trimmed and in lower case, and Gmail addresses without the dots of the local part. A refused address is kept (the 500 most recent, `refused_signins`) so People can let it in. Each account is bound to the Google subject id (`sub`) that first signed in with it, and a different subject with the same address is refused: addresses get reassigned (a graduate's address given to someone new, or recreated as a consumer Google account), subject ids do not. An owner can reset the link from the Dashboard.
4. The server updates the listed account (name and picture from Google; the address stays as listed) and starts a session: 32 random bytes in the cookie, only the SHA-256 in the `sessions` table. The cookie is HttpOnly and `SameSite=Lax`; with an `https` base URL it is also `Secure` and `__Host-` prefixed (host-only, whole site). A session ends 12 hours after sign-in however much it is used (`retention.Sessions`, since the security council fixes below); an hourly sweep deletes expired ones.

Every authenticated request reads the user and role from the database, so disabling an account takes effect on the next request. Removing an organiser's role ends their sessions, and removing someone from the list disables a used account and ends its sessions (an unused row is deleted), so nobody keeps access that the current rules would not grant. State-changing API requests must come from the site's own origin (`Origin` equal to `PODIUM_BASE_URL`), on top of the `SameSite` cookie.

| Rule | Why |
|---|---|
| Sign-in by a list of people, not by email domain (V4-15) | A test run or a contest must admit exactly the people who registered, not everyone at the college or on the internet; one table for students and organisers means one door. The domain rule, its Google Workspace (`hd`) check and `PODIUM_ALLOWED_DOMAINS` were removed in v4; the binding to the first Google account stays. |
| Gmail dots are ignored when matching | Gmail ignores them, and the address someone types on a form often differs from their account's spelling. Nothing else (a `+tag`) is rewritten. |
| Refused addresses are kept, capped at 500 | So an owner or an admin can let a late volunteer in with one click; the cap stops strangers on the internet from filling the disk. |
| Four roles: student < organiser < admin < owner (V4-16) | An admin runs the Dashboard for a run without holding the owner's keys: they manage students and organisers, never an admin, an owner, `PODIUM_OWNER_EMAIL` or themself. One pure function (`auth/rules.go`) decides every change to someone's account inside its transaction, so no route can skip it; the screen only hides what it would refuse. The stored value `admin` meant organiser until migration `0002_v4_roles` renamed it first, so no organiser silently became an admin. |
| A sign-in switch | Closing sign-in when a run is full keeps late strangers out without touching the list; admins and owners always get in, so they can open it again. |
| Leaving testing mode disables testing-made accounts | Testing mode signs anyone in as anyone; with every row on the sign-in list, an account made that way must not stay usable in production (`RecordMode`). |
| `PODIUM_OWNER_EMAIL` is always an owner | A permanent recovery path: `Bootstrap` puts it back on the list, enabled, at every start, so if every other owner is removed or the list locks you out, setting it and restarting restores access. The Dashboard shows this owner as locked. |
| ID token signature is verified, not only taken from TLS | The token arrives over TLS from Google's token endpoint, which OpenID Connect accepts on its own; verifying the signature too costs one cached key fetch and removes any dependence on that reasoning. Uses only the Go standard library plus `golang.org/x/oauth2` for the code exchange. |

**Security council fixes (2026-10-18).** Sessions are browser-session cookies with a fixed 12-hour server-side life and no renewal; Google sign-in accepts only a sign-in from the last 5 minutes (`max_age`; Google does not support `prompt=login`) and the server checks `auth_time`. Owners can end a person's sessions and disable accounts. Testing mode is refused through any proxy and is undone when the server next starts in another mode (the mode is stored in the `settings` table). Every lever that changes results (extra time, extend, pause, resume, rejudge), and every settings change, writes an append-only `contest_audit` row in the same transaction as the change, so a failed row rolls the change back; the console shows the log and the JSON export carries it. Validation reports the tests the brute force did not finish and fails when fewer than half were cross-checked, the `ai draft` tag needs a real edit and a new validation to clear, and a contest takes an AI problem only with a person's solution or an admin's recorded reason.

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

**A box belongs to one lane.** Lane n uses isolate box n, and the judge refuses to initialise a box it already holds. When isolate answers `--init` with "This box is currently in use by another process" (its lock is taken while another isolate call on that box runs), the judge waits 0.25, 0.5, 1 and 2 s between five tries and then fails the job as a judge error that names the likely cause; it never cleans up a box it did not get. isolate's lock covers only a running call, not an idle box another process initialised, so box ids must belong to one judge per host: a second judge or the sandbox suite beside it must use other ids (the self-test uses box 80, the sandbox suite 90 and 91). On the walk of 2026-10-21 a sibling stack held box 1 and one submission became a judge error that "Judge again" fixed.

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

`tokens` (default) compares whitespace-separated tokens; a single token longer than 4 MB in the student's output is a wrong answer. `exact` compares bytes after turning CRLF into LF and dropping trailing newlines, streaming both sides so the judge never holds a whole output in memory. `float` compares numeric tokens with absolute or relative error 1e-6 by default. `custom` runs a testlib checker, compiled as C++17 with a 30 s compile budget (testlib is a large header), in a fresh box like any submission: 10 s CPU, 256 MB, one process. Exit 0 is accepted; wrong answer, presentation error and partial scores are wrong answer, since a test earns nothing on its own (subtasks are all or nothing); `_fail`, a crash or a limit is an internal error. `testlib.h` is installed by `install.sh` at a pinned commit, checked against its SHA-256.

## Pages and crawlers

The web app is client-rendered: every path without a file extension gets the same `index.html`, with `X-Robots-Tag: noindex`, and the server fills nothing per route. A missing file with an extension (an old hashed chunk after a deploy) is a real 404, so a browser never runs `index.html` as a script. v4 adds one exception (V4-6 to V4-8): `npm run build` prerenders the landing page (`web/scripts/prerender.mjs`, `svelte/server` `render()`) into `landing.html`, which the server sends for `/` to a request without a session cookie, its `%BASE_URL%` filled at start-up, with its own description, canonical link and Open Graph tags; `robots.txt` allows `/` alone and names `sitemap.xml`. Otherwise Podium is a college tool behind sign-in, so v3 removed the server-filled page head, Open Graph and social cards, JSON-LD, the route manifest and the web manifest (V3-4): each served a case no single-college contest has met. A path no route matches gets the app's own "Page not found" page, with status 404 (V4-9): the server knows the app's paths (`/`, `/signin`, `/privacy` and the prefixes `/contests/` and `/admin/`), so crawlers and link checkers see a typo as missing. A Go test reads the router's table to keep the two lists in step.

## Retention and erasure

How long each kind of data is kept is one Go file, `server/internal/retention/periods.go`: sessions (12 hours after sign-in), integrity events with their counts and similar-code checks (180 days after the contest ended for everyone), the custom input and output of a run (30 days) and backups (14 days). The session lifetime, the integrity retention, the retention pass and `GET /api/auth/config` read it, and the privacy page renders its table from that response. `docs/OPERATIONS.md` ("Retention") holds the same table and a test compares the two, so the privacy page, the docs and the code cannot say different things. v3 dropped the 4-year retention machinery and the semester archive (V3-4): nothing is installed yet, and they served a case no single-college contest has met.

The retention pass (one task of the server's scheduler, a minute after start-up and every 12 hours; shutdown waits for a batch in progress to commit) empties run payloads, deletes integrity events and checks and clears the per-submission counts past their period, and collects unused test files. It works in batches of 2,000 rows per transaction, so a pass never holds the single write gate for long while a contest runs.

**Erasure** is a command on the VM, not a route: `podium-server forget-user` deletes what identifies a person (sessions, drafts, integrity events, browser hashes, extension reasons, clarification questions), replaces the name and email with `Removed user N`, disables the account, and keeps scores so past standings stay correct. It is a dry run by default and writes a `user_forgotten` site audit row naming the account by id only, with the counts. Audit rows written earlier keep the address: append-only means nothing, not even erasure, rewrites history.

## Site settings and sign-in limits

The organisation's name, contact email, phone and privacy contact live in the `settings` table, entered by an owner on the Organisation tab of the Dashboard, cached in memory and returned before sign-in by `GET /api/auth/config` with the retention periods; no contest data is ever on that route (owner, 2026-10-19). Until the contact email is entered the pages fall back to `PODIUM_OWNER_EMAIL`. Nothing on a public page is hard-coded. The footer email is the way to reach the organisers: v3 removed the contact form and its inbox (V3-4).

**Before sign-in.** The routes a visitor reaches without a session have per-address token buckets (2,500 at once and 50 a second for the config, health check and sign-out, 2,000 and 20 a second for the sign-in steps; IPv6 by /64; the VM itself exempt), sized so a class of 700 behind one college NAT can sign in within a minute (a test proves it), and each Google sign-in attempt finishes once: the callback remembers each state until it expires, so a replay costs no outbound call. Session cookies the database does not know count against a third budget (1,000 at once, 10 a second) before the database is read; a session the server issued or accepted in the last minute skips that refusal, so a flood of forged cookies from one campus address cannot lock out the real students behind it (V3-19). The tables are bounded in memory; a full address table is swept at most once a second and otherwise makes room by dropping one entry, so a stranger never shares a budget someone else is draining.

**Queue health.** The organiser contest response carries the contest's waiting and judging jobs, the oldest wait, its judge errors and unanswered clarifications from one statement over partial indexes, and when a judge last asked for work, which the jobs service holds in memory. The console's opening sentence reads it.

## Data

SQLite in WAL mode, opened once per process with a 5 s busy timeout, foreign keys on, and transactions that begin `IMMEDIATE` (read-only work should use read-only transactions or none, so it does not queue behind writers). Writes take turns in Go before they reach SQLite (`server/internal/db/writegate.go`): every transaction that is not read-only, and every statement outside a transaction that is not a read, waits on one in-process gate, so writers never meet SQLite's busy handler, which sleeps and polls (1 ms rising to 100 ms) while holding a pooled connection. The 700-student load test showed why: with that hand-off, four connections all sat in waiting writers, reads queued behind them, and judging stalled for 90 s (measured on a desktop; the rerun on the VM waits for the VM). Reads run in parallel on the pool's other connections (8 in all). A write that waits 10 s for its turn fails with "database is busy", as `SQLITE_BUSY` did before; other processes (the sqlite3 shell, Litestream) still use the busy timeout. `synchronous=NORMAL` is safe against crashes of the process; a power cut can lose the last committed transaction. Litestream replicates the database to a bucket, uploading changes every 10 s, a nightly job copies the database (SQLite's online backup) and hard-links the test files into `/var/backups/podium`, and with the offsite settings each checked nightly copy also leaves the VM as one archive to a second bucket at another provider (`docs/OPERATIONS.md`, "Backups"). `podium-server check` verifies a database and its files read-only: integrity and foreign key checks, migrations against the binary, and every recorded file present with its size (or, with `-hash`, its content).

Schema changes are numbered files in `server/internal/db/migrations/`, each applied once in its own transaction; a committed file is never edited (V4-23). SQLite cannot change a `CHECK` in place, so a file that must rebuild a table others reference (v4's `0002_v4_roles`, which renamed the stored organiser role from `admin` to `organiser` to make room for an admin role) starts with `-- podium: foreign_keys off`: the runner applies it on a connection of its own with foreign keys off, as SQLite's 12-step rebuild requires, refuses to commit if `PRAGMA foreign_key_check` finds a broken reference, and throws that connection away. With foreign keys on, dropping `users` would run every child table's `ON DELETE` action.

Schema changes are numbered files in `server/internal/db/migrations` (`0001_v3.sql`, then one file per change), applied at start-up, each in its own transaction that also re-checks whether it already ran. The server refuses to start when the database records a migration the binary does not know, which means an older binary is running against a newer schema. After the first release the files are append-only. Nothing alters tables at run time.

**v3 squash (V3-3, 2026-10-27).** Nothing was installed, so v2's migrations 0001 to 0015 were replaced by one file, `0001_v3.sql`, built from the schema they produced minus the tables and columns the v3 cuts left unread (the review system, the contact form, per-contest organisers, the pin history and served log, the old slugs, results snapshots, retention cutoffs, the toolchain registry, the start window, the roster, the graded flag and open tests). Why: nothing is installed, so there is no data to migrate, and a feature hidden behind a flag stays mounted, reachable and untested (V3-3). It adds `json_valid` checks on the JSON columns scoring reads (`contest_problems.points`, `submissions.earned`, `contests.languages`) and the view `contest_final`, the one SQL statement of "over for everyone" (`Contest.Final` in Go). The append-only triggers on the audit and revision tables no longer read a retention cutoff: v3 deletes none of those rows. `TestSchemaTables` in `server/internal/db` lists the 30 tables: 29 from `0001_v3.sql` and `schema_migrations`, which the migration runner makes. The reasoning of the v2 migrations is in git history before the squash.

Times are Unix milliseconds. Sessions are stored by the SHA-256 of the cookie value, so a copy of the database cannot be replayed as logins.

## Unused files on the server

`PODIUM_TESTS_DIR` keeps every file a job may fetch. Tests and submitted programs are referenced from the database for as long as their rows live; reference solutions, checkers, generators and custom inputs are written again each time a job needs them and are only referenced by job payloads. Without a clean-up, every edited reference solution, replaced test and custom input would stay forever. The 12-hourly retention pass removes them (`problems.Blobs.GC`):

| Kept | Why |
|---|---|
| Any test of any problem, any submission's or run's source | still referenced |
| Anything a job that is waiting or running names, and anything a job less than 7 days old names | the judge may still fetch it; a recent run still shows its samples' expected output |
| The tests of a revision a contest pins, or a submission was judged against | the record of what judged it; other revisions' files go like any unused blob |
| A blob recorded in the last 24 hours, or a file with no row written in the last 24 hours | a save or an upload in progress; 24 hours is longer than any job may run |

Removal and saving cannot race: every save records its blob (or refreshes the row's time) inside the transaction that adds the reference, and checks the file is still there inside that transaction. The collection deletes each batch of rows and files inside one write transaction, and deletes a row only if its time is still old, so a blob saved again after the candidates were read is skipped, and a save that lands just after a removal writes the file back (or, for test uploads written before their transaction, fails with "try again" instead of leaving a row without a file). `testcases` also references `blobs` by foreign key, so a test can never lose its row. Job payloads are scanned as text for 64-digit hex runs, so every hash any job kind could fetch is seen whatever the payload's shape.

## Frontend structure

The web app is one Svelte single-page app with no component library. Every screen is built from a few shared pieces, so a pattern is changed in one place:

| Piece | Where | Job |
|---|---|---|
| Tokens | `web/src/styles/tokens.css` | Every colour, size, measure and z-index. Type roles `--fs-page` (28 px on phones, 34 px from 768 px), `--fs-section`, `--fs-sub`; `--fs-6` only for the student's own place on standings (D80); `--mono-optical` sets mono digits one step down beside the Grotesk |
| Controls | `web/src/styles/controls.css` | Buttons with hover and pressed states, fields, tables (`.table-dense` for logs and feeds, `.table-stack` for stacked phone rows with a `.row-link` that covers its row), `.back`, `.state-word`, `.page-section`, the dialog shell |
| `PageHeader` | `web/src/components/PageHeader.svelte` | The top of every route: breadcrumbs on organiser screens or one back link on student screens, the page's only `h1` (focused after each client-side navigation), state word, actions, lead line and the contest tabs |
| `Breadcrumbs` | `web/src/components/Breadcrumbs.svelte` | `nav aria-label="Breadcrumb"`, an ordered list, the current page last with `aria-current` |
| `PageState` | `web/src/components/PageState.svelte` | Loading (shown after 300 ms), error with Try again, forbidden with the organisers' contact email, missing with a link to the parent, empty with its next action |
| `Dialog`, `ConfirmDialog`, `Tabs`, `Splitter` | `web/src/components/` | Every dialog is built on `Dialog`: focus returns to the control that opened it, or to the page heading when that control is gone. `Tabs` and `Splitter` are the one tab strip and the one draggable divider |
| Loader | `web/src/lib/loader.svelte.ts` | Organiser screens load through one loader that keeps only the newest answer |
| Live events | `web/src/lib/stream.ts` | Every page in a tab shares one event stream through `LiveSubscription` |
| Announcer | `web/src/lib/announce.svelte.ts` | Two always-present live regions in `App.svelte`, polite and assertive. Each message clears its region first, so the same sentence said twice is spoken twice. Widgets with a running text (the contest clock) keep their own region |
| Breakpoints | `web/src/lib/breakpoints.ts` | Tiers at 768, 1024, 1280 (`desk`, the page split, D181) and 1440 px plus a short height tier. CSS writes `(min-width: 768px)` or its exact complement `(width < 768px)`, never `max-width: 767px`, so no zoomed width falls between two layouts; a guardrail test fails on any other width |
| Icons | `web/src/lib/icons.ts`, `web/src/components/Icon.svelte` | Lucide shapes as data, 20 px beside words and never instead of them |
| Errors and forms | `web/src/lib/api.ts` `errorText`, `web/src/lib/focus.ts` | `errorText(e)` is the one fallback: the server's sentence, or "Something on this page failed. Reload the page and try again." (a guardrail test forbids a second copy). A 401 from any request signs the page out in one place. Required controls carry `aria-required`; after a field error, from the page or from the server, `focusFirstInvalid()` moves focus to the first marked field |
| Route table | `web/src/lib/routing.ts` | Each route's pattern, name, sign-in need, title, role and nav section. Page titles and the nav read it, and anyone a route turns away sees one forbidden page |

### The shell by role

Navigation is named by job (D81). Organisers do not share the students' landing page:

| Role | Header nav | "/" lands on | Account menu |
|---|---|---|---|
| Signed out | none; a Theme control | Sign in | none |
| Student | Contests | Your contests | Signed in as {email}, the role word, Theme and colours, Help, Sign out |
| Organiser | Contests (`/admin/contests`), Problems | `/admin/contests` | the same, plus "Forget key", which drops every AI provider key this sign-in holds |
| Admin | Contests, Problems, Dashboard | `/admin/contests` | as for an organiser |
| Owner | Contests, Problems, Dashboard | `/admin/contests` | as for an organiser |

- One account control (`components/AccountMenu.svelte`) holds identity, "Theme and colours", Help and Sign out. The header fits one row from 768 px: the button names the person from 1024 px and reads "Account" below that. Under 768 px organisers get a Menu panel with the account items inline.
- Sign out first runs every save registered with `beforeSignOut()` (`lib/session.svelte.ts`, at most 3 s), so the workspace's last edits reach the server before the session ends. During a live contest the shell asks "Sign out during the contest?" first. Opening Podium without a session removes every draft left in the browser, so a lab PC does not keep the last student's code.
- The footer names the organisers ("Contact the organisers: {email}" from `GET /api/auth/config`). `/privacy` exists but nothing links it until the owner approves its text (D85): one constant, `PRIVACY_LINKED` in `web/src/lib/site.ts`, turns on the footer and sign-in links.
- `lib/mode.svelte.ts` loads `GET /api/auth/config` once per page and holds the run mode, the contact email, the organisation and the retention periods; the sign-in page, the footer, `PageState` and `/privacy` read that one copy. The privacy notice is Markdown in `web/src/content/privacy.md` with a `<!-- retention -->` marker that `/privacy` fills with the retention table from the config, never typed by hand.
- Organiser page titles read page first ("Live · Week 6 contest · podium"), so browser tabs differ when cut short.

### Organiser screens

| Screen | Route | Shape |
|---|---|---|
| Contests | `routes/AdminContests.svelte` | Live and next first, then one table; the first-contest checklist appears once here (with a step to add another organiser, for owners); "Needs you" appears once a contest has run (V3-20) |
| Setup | `routes/ContestEditor.svelte` | Saves every field as it changes, with one "Saved" line in the title row. Six inputs show (title, problems, start, length in minutes, hidden minutes at the end, languages); the rest, the exam options included, folds under Advanced with a one-line summary. Publish stays in the title row; Duplicate and Archive sit in a More menu |
| Live | `routes/ContestConsole.svelte`, `components/console/` | Opens with one sentence on how it is going (`consoleSentence`, from the queue health, the judge's last request for work and the started count, refreshed every 10 s with the feed), then Announce, Extend, Extra time, Pause judging, History of changes and If something goes wrong. Banners appear only when something needs action, such as "Update students now" after a validated edit. Questions sit beside the work from 1024 px; under 768 px the console is three tabs, Questions first. End now uses the settings update with the end set to the server's now and keeps the frozen moment (V3-18) |
| Results | `routes/ContestResults.svelte` | One line on where the reveal stands, then Reveal (once there is something to reveal), Similar code (after the end), Download CSV and Download JSON; the board spans the page and one statistics table follows |
| Similar code | `routes/ContestIntegrity.svelte`, `routes/IntegrityPair.svelte` | "Check integrity", the signals with strength in words and the reason under it, the pair view, and the decision dialog (`components/DecisionDialog.svelte`) |
| Problems | `routes/ProblemBank.svelte` | The list, "New problem", "Import pack" and "New problem with AI" |
| Problem editor | `routes/ProblemEditor.svelte`, `components/problem/` | Three tabs, Statement, Tests and Checking, with one status line in the title row ("Validated, 5 tests, slowest 212 ms"). Validate is the one primary action until it passes, then "Add to contest" picks a draft or upcoming contest. Each test has a role select (Example, Subtask N, Not in a subtask). The generator, brute force, validator and custom checker sit under "Optional extra checks". `lib/problems.ts` `judgedFieldsChanged` mirrors `server/internal/problems/hash.go`, so the editor can say a save needs a new validation; keep the two in step |
| Dashboard | `routes/Dashboard.svelte`, `components/settings/` | Admins and owners, at `/admin/dashboard` (`/admin/people` still opens it), tabs in the address: People (the sign-in list, Add people, refused sign-ins), Live (polls `GET /api/admin/live` every 5 s while shown), Sign-in (the switch and account help), Audit (the history of changes) and, for owners, Organisation |

A contest's tabs are Setup, Live and Results, then "Student view", which shows the problems and the board as students see them; a contest opens on the tab that fits its state. Organiser state words come from `organiserState` in `lib/contests.ts`: "Draft" while unpublished and "Ended, practice open" after the end. The AI key form is one component (`components/ai/KeyForm.svelte`), inside the wizard ("Change key or model").

State is a shape: a selected tab, the current contest tab and the next reveal row are drawn with borders, and components that draw fills (subtask strip, rail bars) carry a `forced-colors` block, so Windows high-contrast mode keeps every state visible.

### Page layout and controls (D181 to D184)

| Piece | Where | Rule |
|---|---|---|
| `.page-split` | `styles/base.css` | From 1280 px the work sits at `--measure-form` and a sticky context column (`--measure-aside`) beside it. Below 1280 px the two stack in DOM order |
| `.panel` | `styles/base.css` | One task area on `--surface` with a `--line` border, square, no shadow, as tall as its content; never a grid of equal tiles |
| `Badge` and `.badge` | `components/Badge.svelte`, `lib/badges.ts`, `styles/controls.css` | Verdict and state words on a tint, sentence case. `.state-word` is drawn the same way, so every state word on every screen reads alike |
| Controls | `styles/controls.css` | Every button is `--control-h`; `.btn-compact` only narrows padding and type, inside rows and toolbars; one primary per view; actions in bars, rails and rows are quiet buttons (`.btn-quiet`, or `.btn-link` in the action colour) with no underline; underlines stay for prose links and breadcrumbs |
| `MonoDigits` | `components/MonoDigits.svelte` | Mono for digits only: dates, times and "40 of 100" keep their words in Grotesk |

**Why 1280 px.** The form measure (48rem) and a useful context column (about 26rem) with the gutters need about 1,250 px; at 1024 the context column would squeeze the form (D181).

### The workspace

`components/Workspace.svelte` is one screen at every size, for students in a contest and for organisers at `/admin/problems/{id}/try`. The code and where it is kept live in `lib/draft.svelte.ts`, submitting, running and the result tabs in `lib/judging.svelte.ts`, and the grid, rail, title, sticky strip, toolbar, status line and result pane in `components/workspace/`.

| Where | What | Why |
|---|---|---|
| From 1024 px, in a contest | One contest bar replaces the site header and the rail: Leave, the title, the letters with their scores, the clock, Messages, Standings and Help. A column splitter sets the statement and code shares, and "Hide statement" gives the editor the whole width | One layout, no per-contest switch, so every width and theme is checked once (V3-14) |
| Under 1024 px | One sticky row: each problem's letter and score, the clock and a Menu; then the Problem, Code and Result tabs. The editor is read-only and Run and Submit are not offered: "Use a computer to edit, run or submit." | The owner: on a phone students view code but do not write it (V3-14 as amended) |
| Result area | Three fixed tabs: Run, Submissions (with a count) and Your input. Nothing swaps silently; Run and Submit keep focus | Students lost their place when a tab switched by itself |
| Standings and Messages | Panels over the editor column during a contest; Escape closes them and the problem stays | Opening the board no longer leaves the problem |
| Revealed test (D115) | `TestDetail` shows a revealed test's input, expected output, output, first difference and "Run on this input" | The server decides what is revealed; the screen only shows what the response carries |
| Editor | CodeMirror 6 with the VS Code keys (`@replit/codemirror-vscode-keymap`), find and replace, completion, multiple cursors, folding; Podium's Ctrl+Enter (Submit) and Ctrl+' (Run) win. Each language mode is its own chunk, loaded when chosen. The shortcut table in `web/src/lib/shortcuts.ts` is the contract that `tests/editor-keys.spec.ts` presses row by row | V3-13: Monaco is 600 to 800 KB gzipped per browser against 300 first loads in one minute on campus Wi-Fi, and gives no real C++ or Java completion either |
| Exam options | Full screen on start, blocked paste and blocked copy are applied here during the student's own time, each with a line saying so | V3-25 |

### What a page loads, and the size budget (D249 to D251)

Contest start is several hundred students on campus Wi-Fi in the same minute, so the first load carries only what a student needs:

| Loaded | What | How |
|---|---|---|
| Every page | The shell and the student screens (Home, a contest and its workspace, standings, privacy, sign-in) | The entry script and the chunks `dist/index.html` preloads |
| First visit to an organiser screen | Every admin route | `import()` from a table in `App.svelte`; a loaded module renders at once on the next visit. While one loads, the shell waits for its `h1` and focuses it if focus is still on the pressed link |
| A statement or Markdown text | marked and DOMPurify (`lib/markdown.ts`) | `import()` from the components that render Markdown |
| Maths in that text | KaTeX, its stylesheet and fonts (`lib/katex.ts`) | `import()` only when a placeholder exists; the TeX source is the placeholder's text until KaTeX draws it |
| The editor | CodeMirror core, then the chosen language mode | `import()` |

`web/scripts/size-budget.mjs` (`npm run size`, run in CI after `make build`) gzips the entry script, its preloads and the stylesheets and fails above 120 kB of JS or 16 kB of CSS. A budget is raised only with a measurement and a reason. Fonts: all four faces (Schibsted Grotesk 400, 500, 600 and Fragment Mono 400) are preloaded, because the first screen uses each (D91); a guardrail test keeps the preloads equal to the `@font-face` list.

### Announcements to screen readers

Everything spoken goes through the one announcer (`lib/announce.svelte.ts`): submission results and the submit itself, every stage of a run, pacing refusals, contest announcements and answers, a statement change, info toasts, the start of a contest the student is waiting for, and a change of the student's own place on the standings ("You are now 12th with 240 points."). Only the contest clock keeps its own region, for its 30, 10, 5 and 1 minute milestones. Error toasts stay `role="alert"`.

## Themes

The web app ships four colour themes plus "System", which follows the device's light or dark setting (v3 cut ten to four, V3-8):

| Group | Themes |
|---|---|
| Light | Light |
| Dark | Dark |
| Accessible | High contrast light, High contrast dark (all text at 7:1 or more) |

All colours live in `web/src/styles/tokens.css`. A theme changes surfaces, ink, rules and the one action colour; it never changes what a verdict colour means. Light themes share one set of verdict colours and dark themes another. A separate setting, "Colour-blind safe verdict colours", replaces green and red with teal, purple and orange in any theme; the three stay apart under simulated protanopia and deuteranopia. The editor well stays dark in every theme.

The choice belongs to the account (`users.theme`, `users.verdicts`; `PUT /api/me/preferences`), because students share lab machines: a high-contrast theme chosen for low vision follows the person, and signing out returns the machine to the system theme. The browser keeps a copy in localStorage only so the first frame is painted right.

Every theme is checked by tests rather than by eye: text at 4.5:1 (7:1 in the high-contrast themes) on every surface, control borders at 3:1, verdicts at 4.5:1 on their tints, and a minimum OKLab distance between the action colour and each verdict and between the verdicts themselves. A browser test confirms the CSS cascade produces exactly the values checked. v3 cut ten themes to four (V3-8): each theme multiplies the contrast tests and the screenshot checks.

## Safe to run: deploys, backups and alerts

**When to deploy.** A deploy restarts the server, so the rule is in `docs/RUNBOOK.md`: no deploy in the 48 hours before a published contest, while one runs, or while its submissions are still being judged. v3 removed the guard that enforced it in code, the forced-deploy override and the "site was updated" banner (V3-4): each served a case no single-college contest has met yet.

**Upgrade and rollback.** An upgrade drains the judge, backs up, keeps the running binaries and units, installs, and verifies. Rollback across a migration is a restore, because an older server refuses a database a newer one migrated (`db.Migrate`); it names what is lost and needs a typed confirmation. Configuration files are never written by the scripts: an upgrade stops when the new release needs a variable the VM's files do not name.

**Graceful judge stop.** Jobs run under a context of their own, separate from the one that ends claiming, so SIGTERM lets each lane finish and report its job instead of killing isolate and waiting out the lease. `KillMode=mixed` keeps systemd from signalling the sandboxes directly. A forced stop still loses nothing: the job's lease runs out and the server hands it out again.

**Backups at two providers (V3-10, V3-30).** Litestream streams the database to a bucket (Backblaze B2) every 10 seconds. A nightly job copies the database and hard-links the test files on the VM, and with the offsite settings streams each checked nightly copy as one archive to a second bucket at another provider (Oracle Object Storage), with a key that cannot delete; the bucket's lifecycle rule does the retention. Why two providers: on Oracle's free tier the VM can be deleted, and the test files had no copy off the VM. v3 removed the age-encrypted second copy, the rsync copy and the weekly automatic restore: two backup systems of bash that had never run.

**Alerts without a monitoring stack (D70).** Nothing stays resident: systemd's `OnFailure=` starts a one-shot unit that posts the failed unit's name to an ntfy topic, and a 15-minute timer checks free space, WAL size, Litestream's last upload, the offsite copy's age and a stalled judge. `podium-check restore-proof` restores the bucket's newest database into a scratch folder and runs `podium-server check -hash` on it; it is run by hand once a term. A dead-man heartbeat after each good backup and the watchdog's check of `/healthz` cover the cases where the VM cannot report itself.

**Hosting (V3-28).** The target is Oracle Always Free: one Ampere A1 VM (arm64, 2 OCPU, 12 GB), so releases build for arm64 as well as amd64, and isolate is built from a pinned upstream commit on arm64 because its apt repository is amd64 only. The frontend stays embedded in the server: a split frontend would need cross-site cookies and breaks server-sent events behind a proxy. A watchdog in GitHub Actions starts the VM if Oracle stops it for being idle.

## Memory budget on the 2 GB VM

| Component | Approx. RSS |
|---|---|
| Kernel, systemd, sshd, journald | 200 to 250 MB |
| Caddy | 30 to 40 MB |
| `podium-server` (capped at 300 MB by systemd, Go heap limit 220 MB) | 50 to 80 MB |
| `podium-judge` worker (capped at 256 MB, Go heap limit 96 MB; inputs copied into tmpfs boxes count here) | 20 to 60 MB |
| Two sandbox lanes plus one JVM (all boxes together capped at 1100 MB on `isolate.slice`, 1024 MB budget in the runner) | up to about 1 GB |

isolate places every box under `isolate.service`, not under the `podium-judge` unit, which is why the sandbox cap sits on `isolate.slice`. A 1 GB swap file protects the server from a transient spike; boxes cannot swap (isolate sets `memory.swap.max=0` on every box with a memory limit).

## The v3 choices

v3 refactored v2 in place and deleted what a first contest at one college does not need. Each row is a choice that is hard to reverse, with its reason; the ids point to the owner's decision records, which are not in this repository.

| Choice | Reason |
|---|---|
| Delete the review system; keep the similarity check, the pair view, a CSV of similar pairs and one decision per student (No action, Warning, Exclude from this contest) with a reason (V3-2, owner answer B) | No comparable platform runs a tribunal; the v2 system was about 19 tables and 9,000 lines each of Go and web code, and the source of the worst word clashes. The owner kept one small record of a decision and its reason: no conduct score, no second reviewer, no ruling after the decision, no deadlines |
| Delete code instead of hiding it behind flags, and squash the v2 migrations into one `0001_v3.sql` (V3-3) | Nothing is installed, so there is no data to migrate; hidden routes stay mounted at student role and are untested attack surface |
| Also delete: the contact form and inbox; the page head for crawlers and the social cards (every page is `noindex`); the revision lists, diffs and served log (keeping the pin and "Update students now"); redirects from old contest addresses; per-contest organisers and student assistants; the toolchain registry; the check that refused deploys near a contest, and its override; the encrypted second backup, the rsync copy and the weekly automatic restore; the 4-year retention machinery and the term archive (keeping `forget-user` and the 30-day run payload cleanup); "Can a model solve this?"; student progress pages (V3-4) | Each serves a case no single-college contest has met yet |
| Two kinds of test: examples, which students see, and hidden tests; v2's open tests import as examples (V3-5) | A third kind confused both audits and adds a code path everywhere |
| Every contest counts; no "graded" switch and no live ungraded contest. Practice after the end may show the first failing hidden test, except while the same problem is in a contest that has not closed (V3-6) | Closes a security finding (practice could reveal the tests of a problem still in use) and removes three open questions |
| Keep all three AI providers over plain `net/http` (V3-7 rejected) | Not every organiser has a key at the same provider |
| Four themes: light, dark, high contrast light and dark, plus System (V3-8) | Ten themes multiplied the contrast tests and the screenshot checks |
| Leave the proven core alone: the judge protocol, worker, sandbox driver, compile and blob caches, the jobs queue, the event hub, standings, the pack parser limits and the content hash (V3-9) | Every review agreed they are the part that works |
| Backups: Litestream plus the nightly copy, and that copy also at a second provider; a timed restore by hand once a term (V3-10, V3-30) | Two v2 backup systems of bash had never run; on Oracle's free tier the VM can be deleted, and the test files had no copy off the VM |
| Tag `v3.0.0` and install on a fresh VM (V3-12) | Nothing is installed, so there is no upgrade path to keep |
| The editor stays CodeMirror 6 with the VS Code keys (V3-13) | Monaco is 600 to 800 KB gzipped per browser against 300 first loads in one minute on campus Wi-Fi, and gives no real C++ or Java completion either |
| One workspace layout; under 1024 px students read the statement, standings, messages and their own code, but do not edit, run or submit (V3-14) | A per-contest switch doubles every width and theme check; the owner: on a phone they view code but do not write it |
| Cut per-student start windows, exam feedback, best single submission scoring, the switch to keep judging after a failed example, the AI settings page, the roster, typed contest addresses and the Integrity tab on every contest (V3-15) | Each was one of the 25 inputs on a setup page measured at 2,326 px, listed for simplifying without a decision |
| Cut the Students page; hide the editorial and the animated reveal; Reveal is one button that shows the full board (V3-16) | There was nothing to open on Students; the editorial and the reveal walk are not first-contest jobs |
| The gap between submissions is per problem (V3-17) | A student who finished A was refused on B for 16 s with no explanation |
| End now keeps the moment the board froze (V3-18) | Ending 52 s into a 6-minute freeze stored no freeze and released the held board |
| The forged-cookie limit never refuses a session seen valid in the last minute (V3-19) | The budget is per campus address, so anyone on campus Wi-Fi could otherwise lock every student out |
| Exam options, per contest, all off by default: full screen on start, count tab switches and focus loss, block paste, block copy (V3-25) | The owner keeps lockdown in the organisers' hands as options: the editor is ordinary by default and the measures are there for an examination |
| Host on Oracle Always Free: one Ampere A1 VM (arm64), backups at two providers, the frontend embedded (V3-28) | Pay nothing, domain included; the free AMD VM has 1 GB, under the 2 GB the design targets; a split frontend needs cross-site cookies and breaks server-sent events behind a proxy |

Earlier decisions that still shape the code keep their ids in the sections above: D44 (one judge protocol), D45 (validated is a content hash), D46 (packs are untrusted), D47 (what students can see), D49 (the integrity check on the server), D50 (the AI key in memory only), D53 (the generator outside the hash), D111 (three providers) and D115 (the revealed test).

## Why these choices

| Choice | Instead of | Reason |
|---|---|---|
| isolate | Docker per submission, Judge0, a home-made sandbox | Docker is a packaging tool and needs many flags set correctly to be a sandbox, and container start-up is slow on 1 to 2 vCPUs. Judge0 brings Rails, Postgres, Redis and Docker, heavier than this whole app. isolate is the IOI sandbox, with simple resource reporting. |
| isolate from the upstream apt repository on amd64 | building from source | Upstream publishes signed packages for current Ubuntu LTS releases; security fixes arrive with `apt upgrade`. On arm64, where upstream publishes no package, it is built from a pinned upstream commit with the apt package's paths. |
| SQLite (WAL) | Postgres | Postgres costs 150 to 200 MB of RAM and a second process to run. A few writes per second is far inside SQLite's range. The pure-Go driver lets the server cross-compile from Windows without cgo. |
| A table as the queue | Redis or a broker | No extra process; the queue survives restarts because it is the database; remote workers need only HTTP and a token. |
| One `jobs` table for every kind of judge work | a submissions table as the queue, or one queue per kind | Validation, generation and judging share one protocol and one queue, so there is no second queue to keep in step. |
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
| One decision per student, with its reason, and an audit row per change | a case workflow with reviewers, deadlines and later rulings | The college has its own process for the rest; the reason says what the decision rested on even after the signals age out (V3-2). |
| A client key per press of Submit | relying on the rate limit alone | A retry after a timeout returns the first submission instead of being refused or counted twice. |
| In a contest, a gap per problem plus a cap on attempts per problem | one gap per student across problems | A student who finished A should not wait to submit B (V3-17); the attempt cap keeps spreading submits over problems from becoming a flood. |
| Fair queue priority | first come, first served | One student resubmitting in a loop cannot push everyone else's first attempt back. |
| Compile cache keyed by language, command and source | compiling every job | Run then Submit is the common pattern, and compiles hold the scarce compile mutex. Load-dependent failures are not cached, so a busy moment never becomes a permanent CE. |
| Output generation on the judge, uploaded under lease | running the reference on the server, or accepting any hash | The server never runs untrusted code; tying uploads to the job and its budget keeps the one judge-to-test-data path closed to everything else. |
| Generators run on the judge, chained into outputs and validation | making inputs on the server, or one button per step | Generator code is setter or model code and runs only in isolate; one press for the whole chain is what an AI draft needs to reach "Validated" with at most one edit. |
| The generator outside the content hash, a plan hash in the payload (D53) | hashing the generator with the tests | Students are judged on the tests, not the generator; a tidy-up should not unvalidate a problem, yet inputs from an older generator must never be applied. |
| AI key pasted per session, in memory only (D50) | an environment variable or a stored key | Nothing to leak from the disk, the database, backups or logs; the owner never handles a provider key, and a deployment without one loses only the wizard. |
| Structured outputs plus server-side checks | asking for JSON in the prompt | The provider enforces the shape; the server still checks every rule the schema cannot express, and the answer passes the editor's own field rules before it becomes a draft. |
| `net/http` for all three providers | the official Anthropic Go SDK | The SDK cost about 5 MB of binary and 13 indirect modules for two endpoints; the request, the stream and the errors are a few hundred lines that share the OpenAI and Gemini helper. |
| Refusal fallbacks on for Opus and Sonnet (D54) | failing on any refusal | Contest problems use words like "attack" and "exploit" innocently; a fallback model answers instead of the request failing. |
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
