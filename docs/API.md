# HTTP API

All API routes live under `/api/` and speak JSON. Every other path serves the web app, which renders its own pages (including its 404 page).

## Conventions

- Responses are `application/json; charset=utf-8` with `Cache-Control: no-store`.
- Errors use one shape, with a stable machine code and a sentence for people. The web app shows `message` as is.

```json
{ "error": { "code": "not_found", "message": "This API route does not exist." } }
```

- Request bodies are JSON, sent with `Content-Type: application/json` (anything else gets `415 bad_request`), at most 64 KB unless a route says otherwise, with no unknown fields.
- Every response carries a strict Content-Security-Policy (`script-src 'self'`, no inline scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: same-origin`.
- **Sessions.** Signing in sets an HttpOnly session cookie, `SameSite=Lax`, valid for 30 days and renewed while in use. With an `https` base URL it is named `__Host-podium_session` and marked `Secure`; with a local `http` base URL it is named `podium_session`.
- **Cross-site requests.** `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` are refused with `403 cross_site` unless the request's `Origin` matches `PODIUM_BASE_URL` (or, without an `Origin` header, `Sec-Fetch-Site` is `same-origin` or absent).
- **Roles.** `student` < `admin` < `owner`. Routes marked "owner" return `401 signed_out` without a session and `403 forbidden` for lower roles. Roles are read from the database on every request, so removing an admin takes effect immediately.

- Some errors carry more detail. `fields` maps a form field to what is wrong with it; `problems` lists everything wrong with an upload or a request to validate, so it can be fixed in one pass.

```json
{ "error": { "code": "bad_request", "message": "Some fields need fixing.", "fields": { "slug": "Use lowercase letters, digits and single hyphens, like two-sum." } } }
```

Common error codes: `bad_request`, `signed_out`, `forbidden`, `not_found`, `cross_site`, `conflict`, `internal`. The problem routes add `bad_pack`, `not_ready`, `too_large` and `busy`; the AI wizard adds `no_key`, `bad_key` and `busy`; the workspace routes add `not_ready` and `rate_limited`; the contest routes add `not_on_roster`, `not_started`, `contest_over`, `board_closed` and `too_early`; the event stream adds `busy` and `unsupported`; the judge API adds `unauthorized`, `lease`, `bad_report`, `bad_upload` and `too_large`.

## Health

### `GET /healthz`

| Status | Body |
|---|---|
| 200 | `{"status":"ok","db":"ok"}` |
| 503 | `{"status":"degraded","db":"unreachable"}` |

## Sign-in

### `GET /api/auth/config` (public)

What the sign-in page needs to explain who may sign in, and which sign-in to offer.

```json
{ "allowed_domains": ["college.edu"], "contact_email": "owner@college.edu", "mode": "production" }
```

`contact_email` is `PODIUM_OWNER_EMAIL`, shown as the organiser to contact. `mode` is the server's `PODIUM_MODE`: `testing`, `dev` or `production`. In `testing` the web app shows the sign-in form and the testing-mode banner instead of "Continue with Google".

### `POST /api/auth/test-signin` (public, testing mode only)

Registered only when `PODIUM_MODE=testing`, which runs on loopback alone; in other modes it is `404 not_found`. Signs in as anyone, with any role, without Google.

Request: `{"email":"asha@college.edu","name":"Asha Rao","role":"admin","next":"/admin/problems"}`. `name` and `next` may be empty; `role` is `student`, `admin` or `owner`. The account is created if needed, otherwise its name and role are updated. `PODIUM_OWNER_EMAIL` always keeps the owner role, and the account's Google link is left untouched.

| Status | Body |
|---|---|
| 200 | `{"next":"/admin/problems"}`, with the session cookie set; `next` is made safe as for Google sign-in (anything that is not a path on this site becomes `/`) |
| 400 | `bad_request`: not an email address, or an unknown role |

Any session the browser already had ends, as with Google sign-in.

### `GET /api/auth/google/start?next=/path` (public, browser navigation)

Not registered in testing mode, nor is the callback below.

Redirects (302) to Google's consent screen with PKCE, a nonce and a state value held in a short-lived signed cookie. `next` must be a path on this site (it starts with `/` and not `//`); anything else becomes `/`. When exactly one domain is allowed it is passed to Google as a hint.

### `GET /api/auth/google/callback` (Google redirects here)

Verifies the state, exchanges the code, verifies the ID token (signature, issuer, audience and authorised party, expiry, nonce) and requires `email_verified`. The email may sign in when its domain is on the allowed list, or when the account already has the admin or owner role, or when it is `PODIUM_OWNER_EMAIL`. Each account is bound to the Google account (`sub`) that first signed in with it; a different Google account with the same address is refused. On success it creates the user if needed, ends any session the browser already had, starts a new one and redirects to `next`.

On failure it redirects to `/signin?error=<code>`:

| Code | Meaning |
|---|---|
| `domain` | The email's domain is not on the allowed list. |
| `unverified` | Google has not verified the email address. |
| `disabled` | The account has been disabled by an organiser. |
| `account` | The address is linked to a different Google account (an owner can reset the link). |
| `expired` | The sign-in attempt took too long or was started in another tab. |
| `google` | Google refused or the response could not be verified. |
| `server` | The server failed while recording the sign-in. |

### `POST /api/auth/logout`

Ends the current session and clears the cookie. `204` with no body, also when already signed out.

### `GET /api/me`

| Status | Body |
|---|---|
| 200 | `{"id":1,"email":"a@college.edu","name":"Asha Rao","avatar_url":"https://...","role":"student","theme":"","verdicts":"standard"}` |
| 401 | `{"error":{"code":"signed_out",...}}` |

`theme` is the display theme id, or `""` to follow the system. `verdicts` is `standard` or `signal` (colour-blind safe verdict colours).

### `PUT /api/me/preferences`

Saves the signed-in person's display preferences, so they follow the account to another machine.

Request: `{"theme":"nordic-frost","verdicts":"signal"}`. `theme` is `""` (follow the system) or an id of 1 to 32 characters from `a-z`, `0-9` and `-`; the server checks only the shape, and the web app treats ids it does not know as `""`. `verdicts` is `standard` or `signal`; both fields are required.

| Status | Body |
|---|---|
| 200 | the saved preferences, as sent |
| 400 | `bad_request`: a malformed theme id or an unknown verdict set |
| 401 | `signed_out` |

## People and domains (owner)

### `GET /api/admin/people`

Everyone with the admin or owner role, plus the allowed domains.

```json
{
  "people": [
    { "id": 1, "email": "owner@college.edu", "name": "Priya Raman", "role": "owner",
      "last_login_at": 1792183192359, "locked": true }
  ],
  "domains": [ { "domain": "college.edu", "created_at": 1792183192359 } ]
}
```

`last_login_at` is Unix milliseconds or `null` (added but never signed in). `locked` is true for `PODIUM_OWNER_EMAIL`, whose owner role comes from the server configuration and cannot be changed here.

### `POST /api/admin/people`

Grants a role by email. The person does not need to have signed in yet.

Request: `{"email":"ta@college.edu","role":"admin"}` (`role` is `admin` or `owner`). Granting `admin` to an existing owner changes them to admin.
Response `200`: the person, in the shape above.
Errors: `400 bad_request` (invalid email or role), `409 conflict` (the configured owner cannot be changed, or the change would leave no owner).

### `DELETE /api/admin/people/{id}`

Removes the admin or owner role; the person becomes a student and their sessions end (they sign in again as a student if their domain is allowed). `204`.
Errors: `404 not_found`, `409 conflict` (the configured owner, or the last remaining owner).

### `POST /api/admin/accounts/reset-google`

Unlinks an account from its Google account, for when someone's address moved to a new Google account. Their sessions end; the next Google account that signs in with the address is linked to it.

Request: `{"email":"asha@college.edu"}`. Response `204`. Errors: `404 not_found` (no account with that email).

### `POST /api/admin/domains`

Request: `{"domain":"college.edu"}` (lowercased; a leading `@` is ignored). Response `201`: `{"domain":"college.edu","created_at":...}`. `200` with the existing entry if it was already there. `400 bad_request` for an invalid domain.

### `DELETE /api/admin/domains/{domain}`

Stops sign-ins from the domain and ends the sessions of students on it; organisers on it keep access. `204`. `404 not_found` when the domain is not on the list.

## Problems (admin)

Every route here needs the admin or owner role. Pack format and rules: `docs/AUTHORING.md`.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/problems` | `?archived=1` to include archived problems | 200 `{"problems":[ProblemSummary]}`, most recently changed first |
| `POST /api/admin/problems` | `{"title":"Two sum","slug":"two-sum"}` | 201 `Problem` with a starter statement and C++ reference |
| `POST /api/admin/problems/import` | the pack as a zip body | 201 `Problem` |
| `GET /api/admin/problems/{id}` | | 200 `Problem` |
| `PUT /api/admin/problems/{id}` | `ProblemEdit` | 200 `Problem` |
| `DELETE /api/admin/problems/{id}` | | 204, archives the problem (kept with its tests and history) |
| `PUT /api/admin/problems/{id}/tests` | a zip of `NN.in` files, each with an optional `NN.out` | 200 `Problem` |
| `GET /api/admin/problems/{id}/tests/{idx}/input` (or `/output`) | | 200 the file, `text/plain`, as an attachment named `test-NN.in` or `test-NN.out`; 404 for an output not generated yet |
| `GET /api/admin/problems/{id}/export` | | 200 the pack, `application/zip`, as an attachment named `<slug>.zip`; outputs not generated yet are left out |
| `POST /api/admin/problems/{id}/validate` | no body | 202 `Validation` |
| `GET /api/admin/problems/{id}/validation` | | 200 `Validation`, the latest one |
| `POST /api/admin/problems/{id}/generate` | `{"all":false}` | 202 `Generation` (see "Output generation") |
| `GET /api/admin/problems/{id}/generation` | | 200 `Generation`, the latest one |
| `POST /api/admin/problems/{id}/generate-inputs` | no body | 202 `InputGeneration` (see "Input generation") |
| `GET /api/admin/problems/{id}/input-generation` | | 200 `InputGeneration`, the latest one |
| `POST /api/admin/problems/{id}/solve` | no body | 202 `{"wizard_id","server_now"}` (see "AI wizard") |

An `{id}` that is not a problem gives `404 not_found` on every route.

### Shapes

`ProblemSummary`:

```json
{ "id": 3, "slug": "a-plus-b", "title": "A plus B", "difficulty": "easy", "tags": ["math"],
  "origin": "import", "archived": false, "test_count": 6, "points": 100,
  "validated": true, "validated_at": 1792183192359, "updated_at": 1792183100000 }
```

- `difficulty` is `easy`, `medium`, `hard` or `""`. `origin` is `manual`, `import` or `ai` (made by the AI wizard). A wizard draft carries the tag `ai draft` until a person first saves it.
- `validated` is true when the last passing validation covers the problem as it is now (its content hash equals the problem's). `validated_at` is `null` whenever `validated` is false.
- Times are Unix milliseconds.

`Problem` is `ProblemSummary` plus:

```json
{ "statement_md": "Read two integers...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": "tokens", "epsilon": 1e-6, "checker_source": "",
  "reference": { "language": "cpp17", "source": "#include <cstdio>..." },
  "brute": { "language": "python3", "source": "..." },
  "validator": null,
  "generator": { "language": "python3", "source": "import random, sys..." },
  "plan": [ { "idx": 3, "subtask": 1, "seed": 3, "args": ["n=10"] } ],
  "samples": [1, 2],
  "subtasks": [ { "idx": 1, "points": 40, "title": "Small numbers", "tests": "3-4", "generator_args": ["n=10"] } ],
  "tests": [ { "idx": 1, "sample": true, "subtask_idx": null, "input_size": 8, "output_size": 3,
               "input_preview": "2\n1 2\n", "output_preview": "3\n", "output_missing": false } ],
  "content_hash": "9f2c...", "validated_hash": "9f2c...",
  "problems": [], "warnings": ["No input validator: validation cannot check inputs against the constraints."] }
```

- `language` is `cpp17`, `java21`, `python3` or `node22`. `brute`, `validator` and `generator` are `null` when the problem has none.
- `plan` lists the tests the generator makes (always an array): test `idx` of subtask `subtask`, made by running the generator with argv `seed` followed by `args`. `subtasks[].generator_args` are the arguments the editor proposes for new planned tests of that subtask (always an array). Format and rules: `docs/AUTHORING.md`, "Tests from a generator".
- `checker` is `tokens`, `exact`, `float` or `custom`. `epsilon` matters only for `float`; `checker_source` is the testlib checker for `custom` and `""` otherwise.
- `tests[].subtask_idx` is `null` for a sample. Previews are the first 200 bytes of each file, cut at a whole UTF-8 character.
- `tests[].output_missing` is true while a test has no expected output (a pack or test zip without its `.out` file); `output_preview` is then `""`. "Generate outputs" fills it.
- `problems` lists what stops validation and judging (a test in no subtask, a range naming a missing test, "Test 3 has no expected output. Generate outputs from the reference solution."); `warnings` is advice that does not (points not adding up to 100, no brute force, no validator, more than 10 hidden tests). After an import, `warnings` also names files the pack carried that podium ignores.

`ProblemEdit` replaces every editable field at once (at most 512 KB of JSON):

```json
{ "title": "A plus B", "slug": "a-plus-b", "statement_md": "...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": "tokens", "epsilon": 1e-6, "checker_source": "",
  "reference": { "language": "cpp17", "source": "..." }, "brute": null, "validator": null,
  "tags": ["math"], "difficulty": "easy", "samples": [1, 2],
  "subtasks": [ { "points": 40, "title": "Small numbers", "tests": "3-4" } ] }
```

A `brute` or `validator` with empty source counts as none. Subtasks are numbered in the order sent, and each `tests` range list is stored in its shortest form (`"3, 4, 5"` becomes `"3-5"`).

`ProblemEdit` may also carry `generator` (`{language, source}` or `null`), `plan` (as in `Problem`) and `subtasks[].generator_args`:

- Leave out both `generator` and `plan` to keep the generator, the plan and each subtask's `generator_args` (an editor that does not know about generators cannot erase one).
- Send either to replace them: the one left out keeps its stored value, and each subtask's `generator_args` then comes from the body (left out means none). `null`, or a generator with empty source, removes the generator; the plan must then be empty.
- Field errors: `generator`, `plan` (one sentence for the first wrong entry) and `subtasks.N.generator_args`.
- Any save removes the `ai draft` tag. The generator and the plan are not part of the content hash, so changing them does not withdraw "validated" (D53).

`Validation`:

```json
{ "id": 12, "state": "failed", "content_hash": "9f2c...", "current": true,
  "queued_at": 1792183192359, "finished_at": 1792183230000, "queue_position": null,
  "compile": [ { "role": "reference", "verdict": "OK", "output": "" } ],
  "tests": [ { "idx": 4, "role": "reference", "verdict": "WA", "time_ms": 3, "memory_kb": 1024,
               "message": "Token 1 differs: expected \"7\", found \"8\"." } ],
  "summary": { "passed": false, "reference_passed": 5, "reference_total": 6,
               "brute_agreed": 6, "brute_total": 6, "validator_passed": 6, "validator_total": 6,
               "max_time_ms": 3, "time_limit_ms": 1000,
               "problems": ["Test 4: reference solution got wrong answer. Token 1 differs: expected \"7\", found \"8\"."],
               "warnings": [] },
  "error": null }
```

- `state`: `queued` (waiting for a worker; `queue_position` counts from 1), `running` (a worker holds it), `passed`, `failed`, or `error` (the run could not finish; `error` says why and `summary` is `null`).
- `current` is false when the problem has changed since this validation was requested.
- `role` is `reference`, `brute`, `validator` or `checker` (the checker appears only in `compile`). Compile verdicts are `OK` or `CE`; test verdicts are `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `IE` or `SK` (a brute force that ran out of time). `message` is a checker comment or the end of a validator's error output, at most 256 bytes.
- `summary` is computed by the server from the recorded results once the job closes. `time_limit_ms` there is the problem's limit times the reference language's time factor. `passed` is true when `problems` is empty.

### Errors

| Route | Status and code | When |
|---|---|---|
| create, update | 400 `bad_request` with `fields` | a field is invalid, or the slug is taken. Field keys are the JSON names, plus `subtasks.N.points`, `subtasks.N.title` and `subtasks.N.tests` (N from 0) |
| import, replace tests | 415 `bad_request` | `Content-Type` is not `application/zip` or `application/x-zip-compressed` |
| import, replace tests | 413 `too_large` | the zip is larger than 32 MB |
| import, replace tests | 400 `bad_pack` with `problems` | anything wrong with the archive or its contents (limits in `docs/AUTHORING.md`) |
| import | 409 `conflict` | a problem with the pack's slug exists |
| import, replace tests, export | 503 `busy` | the request ended while waiting for another pack operation; only one runs at a time |
| validate | 409 `not_ready` with `problems` | the problem has no tests, no subtask, tests outside the subtasks, or tests without an expected output |
| validation | 404 `not_found` | the problem has never been validated |
| generate | 409 `not_ready` with `problems` | no tests, no reference source, a generation still queued or running, or (with `all` false) no output missing |
| generation | 404 `not_found` | outputs have never been generated for the problem |
| generate-inputs | 409 `not_ready` with `problems` | no generator, no planned tests, no reference source, or an input or output generation still queued or running |
| input-generation | 404 `not_found` | inputs have never been generated for the problem |

Validate notes:

- Asking again while a validation of the same content is queued or running returns that validation instead of a new one.
- A queued validation of older content is replaced: it ends in `error` with "A newer version of the problem was sent for validation."
- A failing validation of exactly the content that was validated withdraws "validated" (for example after a judge upgrade made the reference slower).

### Output generation

`POST /api/admin/problems/{id}/generate` runs the reference solution on the problem's inputs, on the judge, and makes what it prints the expected outputs. Request: `{"all":false}` fills only missing outputs; `{"all":true}` replaces every output (the editor asks to confirm first). Unsaved edits are not seen: the job uses the problem as stored.

`Generation`:

```json
{ "id": 4, "state": "done", "all": false, "queued_at": 1792183192359, "finished_at": 1792183200000,
  "results": [ { "idx": 5, "verdict": "OK", "time_ms": 12 }, { "idx": 6, "verdict": "TLE", "time_ms": 1500 } ],
  "compile_output": "",
  "summary": { "applied": 1,
               "problems": ["Test 6: the reference solution got time limit exceeded, so its output was not generated."] },
  "error": null }
```

| Field | Meaning |
|---|---|
| `state` | `queued`, `running`, `done`, or `error` (the job could not finish; `error` says why and `summary` is `null`) |
| `results` | one row per input the reference ran on; `OK` means its output was uploaded |
| `compile_output` | the reference's compiler output, `""` when it compiled cleanly |
| `summary.applied` | how many outputs were written into the problem |
| `summary.problems` | what was not applied, one sentence each: the reference does not compile, it failed on a test (named with its verdict), an output did not arrive, or the problem changed while the job ran |

Rules:

- One generation per problem at a time; asking again while one is queued or running is `409 not_ready`.
- The job records the problem's content hash when queued. If the problem changed before it finished, nothing is applied: "The problem changed while outputs were being generated. Generate them again."
- Outputs from the tests that passed are applied even when other tests failed.
- Applied outputs change the content hash, so the problem must be validated again.
- Priority 85: after every contest job, before validation.

### Input generation

`POST /api/admin/problems/{id}/generate-inputs` runs the problem's generator on the judge, in isolate, once per planned test (argv: seed, then the test's args) and stores what it prints as that test's input. When every planned test got an input, output generation from the reference is queued, and when every output arrived, validation is queued: one press runs the whole chain. Priority 85, like output generation. Unsaved edits are not seen.

`InputGeneration`:

```json
{ "id": 2, "problem_id": 7, "state": "done", "planned": 6, "queued_at": 1792183192359, "finished_at": 1792183200000,
  "results": [ { "idx": 3, "verdict": "OK" }, { "idx": 4, "verdict": "RE", "message": "Traceback ..." } ],
  "compile_output": "",
  "summary": { "applied": 0, "generating_outputs": false,
               "problems": ["The generator failed on test 4 (subtask 1, seed 4): Runtime error. Traceback ..."] },
  "error": null, "server_now": 1792183200100 }
```

| Field | Meaning |
|---|---|
| `state` | `queued`, `running`, `done`, or `error` (the job could not finish; `error` says why and `summary` is `null`) |
| `planned` | how many tests the run was asked to make |
| `results` | one row per planned test the generator ran on; `OK` means its output was uploaded as the input; `message` (left out when empty) is the end of the generator's error output |
| `compile_output` | the generator's compiler output, `""` for Python or a clean compile |
| `summary.applied` | how many inputs were written into the problem: 0 or `planned`, because nothing is applied unless every planned test got an input |
| `summary.generating_outputs` | output generation was queued next |
| `summary.problems` | why nothing was applied, one sentence per test, naming the test, subtask and seed |

Rules:

- Inputs replace any test with the same number; its expected output is cleared until output generation fills it.
- The job records the content hash and a plan hash (generator plus plan) when queued. If either changed before it finished, nothing is applied: "The problem changed while inputs were being generated. Generate them again."
- Generator limits: 5 s per test before the language factor, 256 MB, 16 MB of output per test; the worker may upload one input per planned test plus 2.
- One input or output generation per problem at a time.
- Each change of the job is also sent to the admin who queued it as an `input_generation` event (see "Live updates"). The output generation and validation that follow are read from `/generation` and `/validation` as before.

## AI wizard (admin)

Every route needs the admin or owner role and answers with `server_now`. The provider key is pasted per session and held in the server's memory only, for 2 hours, tied to the session: never stored, logged, sent to the judge or shown back (decision D50, `docs/ARCHITECTURE.md`). Signing out, signing in again in the same browser, or a restart forgets it. Request bodies of the key route are never logged.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/ai/key` | | 200 `KeyStatus` |
| `PUT /api/admin/ai/key` | `{"provider":"anthropic","model":"claude-opus-5-5","key":"..."}` | 200 `KeyStatus` |
| `DELETE /api/admin/ai/key` | | 204 |
| `POST /api/admin/ai/wizard` | `WizardRequest` (at most 256 KB) | 202 `{"wizard_id":"3f9c...","server_now":...}` |
| `GET /api/admin/ai/wizard/{id}` | | 200 `Wizard` |
| `POST /api/admin/problems/{id}/solve` | no body | 202 `{"wizard_id":"...","server_now":...}` |

`KeyStatus`:

```json
{ "active": true, "provider": "anthropic", "model": "claude-opus-5-5", "active_until": 1792190392359,
  "providers": [ { "id": "anthropic", "name": "Anthropic", "models": [
      { "id": "claude-opus-5-5", "name": "Claude Opus 5.5 (default)" },
      { "id": "claude-sonnet-5-5", "name": "Claude Sonnet 5.5" },
      { "id": "claude-haiku-5-5", "name": "Claude Haiku 5.5" } ] } ],
  "server_now": 1792183192359 }
```

- Without a key: `active` false, `active_until` `null`, and `provider` and `model` are the defaults (`anthropic`, `claude-opus-5-5`) for the form to preselect.
- In testing mode `providers` also lists `{"id":"fake","name":"Test provider (canned problems)","models":[{"id":"canned","name":"Canned problems"}]}`. The fake provider needs no key and answers with one of two canned problems: a maximum subarray problem when the brief or text mentions "subarray" or "stretch", a pair sums problem otherwise.

`PUT /api/admin/ai/key`: `model` empty means the default; `key` is ignored for `fake`. The key is checked with one call that costs no tokens (a model lookup) before it is kept.

| Status and code | When |
|---|---|
| 400 `bad_request` | a provider or model the server does not offer, or an empty key |
| 400 `bad_key` | the check call failed; the message says what the provider said, in words ("Anthropic refused the key. Check it and paste it again.", "Anthropic says this key cannot use claude-opus-5-5. ...", "The provider is unavailable. ..."), never echoing the key |

`WizardRequest`, from a brief or from pasted text:

```json
{ "mode": "brief", "brief": { "topic": "Maximum subarray sum", "difficulty": "medium",
  "constraints": "n up to 2e5, values up to 1e9 in absolute value", "subtasks": "n up to 1000, then full" } }
```

```json
{ "mode": "paste", "text": "The problem statement, copied from elsewhere..." }
```

`difficulty` is `easy`, `medium` or `hard`; `topic` is required; each brief field is at most 2,000 characters and `text` at most 20,000.

| Status and code | When |
|---|---|
| 400 `bad_request` | an unknown mode or difficulty, an empty topic or text, or a field over its limit |
| 409 `no_key` | "Add a key in AI assistant first." |
| 429 `busy` | "One AI request at a time. Wait for the current one to finish." (a generation or a solve of this admin is still running) |

The call runs in the background, at most 3 minutes with its one retry. When the answer passes the checks it becomes a draft problem (origin `ai`, tag `ai draft`; a taken slug gets `-2`, `-3` and so on), and input generation is queued for it at once, which chains output generation and validation.

`Wizard` (the admin's own runs only; kept in memory for an hour after it ends):

```json
{ "id": "3f9c...", "kind": "generate", "state": "draft_created", "problem_id": 12,
  "started_at": 1792183192359, "server_now": 1792183230000 }
```

```json
{ "id": "c3d4...", "kind": "generate", "state": "error", "started_at": 1792183192359, "server_now": 1792183230000,
  "error": { "code": "invalid_answer", "message": "The model's answer could not be used.",
             "reasons": ["Subtask points add up to 90; make them add up to 100."] } }
```

- `state`: `thinking`, then `draft_created` (generate), `submitted` (solve) or `error`. `problem_id` is the draft (generate) or the problem solved (solve); a solve adds `submission_id`.
- `error.code`: `key_refused`, `no_access`, `model_not_found`, `rate_limited`, `unavailable`, `bad_request`, `refused` (the model declined; the message names the category), `cut_off` (too long, after one shorter retry), `invalid_answer` (with `reasons`, after one retry with them), `timeout`, `canceled`, `input`, `not_ready` (solve: the problem cannot be judged), `not_found`, `internal`. `reasons` is always an array.
- `404 not_found` for another admin's run, an unknown id, or a run that ended over an hour ago or before a restart.
- The same object is sent as a `wizard` event to the admin who started it when the run ends.

`POST /api/admin/problems/{id}/solve` ("Can a model solve this?") sends the statement alone and asks for a C++17 solution, which is judged on the problem's tests as the admin's own submission (`client_key` `ai-solve-{wizard_id}`, listed with the admin's other submissions). The editor reads the score from `GET /api/submissions/{submission_id}`. Refusals: `404 not_found`, `409 no_key`, `409 not_ready` with `problems` before any call when the problem cannot be judged (its structure, or missing outputs), `429 busy`. The one-pending-submission rule still applies: the run then ends in `error` with `rate_limited`.

## Workspace, runs and submissions

The workspace is the page where code is written, run and submitted. Under `/api/workspace/` only organisers reach it, to try a problem as a student would ("Try as a student"); submissions made there count in no contest. Students reach the same shapes under a contest (see "Contests: students").

| Route | Who | Request | Success |
|---|---|---|---|
| `GET /api/workspace/{problem}` | admin | | 200 `Workspace` |
| `PUT /api/workspace/{problem}/draft` | admin | `{"language":"cpp17","source":"..."}` | 204 |
| `POST /api/workspace/{problem}/run` | admin | `{"language","source","input"}`; leave out `input` to run the samples | 202 `Run` |
| `POST /api/workspace/{problem}/submit` | admin | `{"language","source","client_key","typed_chars","pasted_chars","focus_losses"}` | 202 `Submission`; 200 with the first submission for a repeated `client_key` |
| `POST /api/workspace/{problem}/events` | admin | `{"events":[{"kind":"paste","size":120,"at":1792183192359}]}` | 204 |
| `GET /api/runs/{id}` | the author, or an organiser | | 200 `Run` |
| `GET /api/submissions/{id}` | the author, or an organiser | | 200 `Submission` |

- `{problem}` is a problem id. A problem or id that does not exist, a run asked for on the submissions route (or the reverse), and someone else's run or submission all give `404 not_found`, so ids reveal nothing.
- Run, submit and draft bodies may be up to 160 KB: code at most 64 KB, custom input at most 64 KB, both valid UTF-8.
- `language` is `cpp17`, `java21`, `python3` or `node22`.
- Every `Workspace`, `Run` and `Submission` carries `server_now` (Unix ms), so the page can keep an offset to the server's clock.

### `Workspace`

```json
{ "problem_id": 3, "title": "A plus B", "statement_md": "...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "languages": [ { "id": "cpp17", "name": "C++17" }, { "id": "java21", "name": "Java 21" },
                 { "id": "python3", "name": "Python 3" }, { "id": "node22", "name": "JavaScript (Node 22)" } ],
  "samples": [ { "idx": 1, "input": "2\n1 2\n", "output": "3\n", "truncated": false } ],
  "draft": { "language": "cpp17", "source": "...", "updated_at": 1792183192359 },
  "submissions": [ ],
  "server_now": 1792183192400 }
```

- `samples` holds each sample's input and output text, at most 4 KB each, cut at a whole UTF-8 character; `truncated` is true when either was cut. A sample whose output is not generated yet is left out.
- `draft` is `null` when nothing was saved. `submissions` is the viewer's newest 20 on this problem, newest first, as `Submission` objects.
- Starter code per language lives in the web app, not here.

### Drafts

`PUT .../draft` stores the editor's code for this user and problem, replacing the last one. The web app saves every 10 s and when the editor or window loses focus. Each user may save at most one draft every 2 s (every save takes the database's single writer lock). Errors: `400 bad_request` (unknown language, a language the contest does not allow, code over 64 KB, not UTF-8), `404 not_found`, `429 rate_limited` with `retry_after_s`.

### `Run`

```json
{ "id": 77, "state": "done", "compile_output": "",
  "results": [ { "label": "Sample 1", "verdict": "AC", "time_ms": 10, "memory_kb": 3120,
                 "output": "3\n", "output_truncated": false, "expected": "3\n", "message": "" } ],
  "server_now": 1792183192400 }
```

| Field | Meaning |
|---|---|
| `state` | `queued`, `running`, `done` or `error`; on `error`, `error` holds a sentence ("The judge could not finish this run. Try again.", or the problem's checker does not compile) |
| `results` | one row per input from the start: every sample in order (labelled "Sample 1", "Sample 2", ...), or one "Custom input". `verdict` is `""` until that input has run |
| `verdict` | for a sample, the checker's verdict (`AC`, `WA`) or a limit verdict (`TLE`, `MLE`, `RE`, `OLE`); for a custom input, `OK` when the program ran without error, otherwise the limit verdict |
| `output` | the start of what the program printed, at most 4 KB, valid UTF-8; `output_truncated` says it was cut |
| `expected` | the sample's expected output (at most 4 KB); `null` for a custom input |
| `message` | the checker's comment on a sample, `""` otherwise |
| `compile_output` | the compiler's output; a compile error leaves every `verdict` empty |

Times are rounded to 10 ms (100 ms for a student in a graded contest). A run uses the problem's limits and checker. Running the samples on a problem without samples is `400 bad_request` ("Run it on your own input instead").

### `Submission`

```json
{ "id": 80, "problem_id": 3, "language": "cpp17", "state": "done", "verdict": "WA",
  "score": 40, "max_score": 100, "max_time_ms": 20, "max_memory_kb": 3200,
  "created_at": 1792183192359, "judged_at": 1792183195000, "queue_position": null, "compile_output": "",
  "samples": [ { "idx": 1, "verdict": "AC", "time_ms": 10, "memory_kb": 3100 } ],
  "subtasks": [ { "idx": 1, "points": 40, "earned": 40, "verdict": "AC",
                  "tests": [ { "idx": 3, "verdict": "AC", "time_ms": 20, "memory_kb": 3200 } ] },
                { "idx": 2, "points": 60, "earned": 0, "verdict": "WA",
                  "tests": [ { "idx": 5, "verdict": "WA", "time_ms": 10, "memory_kb": 3150 },
                             { "idx": 6, "verdict": "SK", "time_ms": 0, "memory_kb": 0 } ] } ],
  "server_now": 1792183195100 }
```

| Field | Meaning |
|---|---|
| `state` | `queued` (`queue_position` counts from 1), `running`, `done`, or `error` (the judge could not finish, or the problem's checker does not compile; `error` holds the sentence, the submission is not counted and shows no score) |
| `verdict` | `""` while judging, then `AC` when every test passed, `CE` when the code did not compile, otherwise the verdict of the first failing test in judging order |
| `score` | the points of the subtasks passed in full; while judging, the score so far from the results that have arrived |
| `subtasks[].verdict` | `AC`, the first failing test's verdict, `SK` when its tests were skipped (or it has none), `CE`, or `""` while pending |
| test verdicts | `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` (not run: an earlier test failed), `IE` (the judge never reported it), `""` while pending |
| `compile_output` | the compiler's output for the submitted code |

A submission never carries the input, output or checker comment of any test (decision D47). Times are rounded to 10 ms. How scoring works: `docs/ARCHITECTURE.md`, "Submissions and scoring".

**What a student sees of their own submission.** The same view on every path (`GET /api/submissions/{id}`, the workspace list, the answer to a submit or a repeated `client_key`, and live events). Owners and admins see every figure. A contest's co-organisers see the student view, both of their own submissions and of the contest's submissions they read: they are not trusted with what exact figures reveal about hidden tests.

- Coarse figures: no memory figure for hidden tests (`memory_kb` 0), `max_memory_kb` rounded up to the next 16 MB, times rounded to 100 ms in graded contests and 10 ms otherwise.
- While judging: per-test verdicts only inside finished blocks (the samples as a whole, each whole subtask), and `max_time_ms` and `max_memory_kb` stay 0 until the end, so polling shows no more than the live stream.
- Exam feedback mode, until the student's window closes: `"hidden": true`, `"state": "submitted"`, an empty `verdict`, `score` 0, no `samples`, `subtasks`, times or compile output, and no live `submission` events. Nothing a student can observe changes when judging finishes. The one exception is a judge failure: `"state": "error"` with its `error` sentence, so the student knows it did not count and can submit again (it is never something the program chose). After the window closes the full student view returns.

`hidden` is `false` in every other case.

### Submit rules

| Rule | Value | Refusal |
|---|---|---|
| Pending submissions | one per user per problem; not applied in exam mode, where a new submission queues behind the pending one (a refusal that ends when judging ends would tell the student when it finished) | `429 rate_limited`: "Your last submission for this problem is still being judged. Wait for its result." |
| Gap between submits | 5 s per user, across all problems (organisers trying problems; contests set their own in M4) | `429 rate_limited`: "Submissions are limited to one every 5 seconds. Try again in 3 s." |
| Code | at most 64 KB, UTF-8, a known language | `400 bad_request` |
| `client_key` | optional, at most 64 characters | `400 bad_request` |
| Problem | tests, samples and subtasks fit together and every test has an expected output; validation is not required to try a problem | `409 not_ready` with `problems` |

- `client_key` is a random value the browser makes per press of Submit and reuses on a retry. Keys are per user, problem and contest (the same key on another problem, or in another contest, is a new submission). The same key from the same user on the same problem returns the first submission with `200` and counts nothing again, so a retry after a timeout never trips the rate limit.
- `typed_chars`, `pasted_chars` and `focus_losses` are the editor's counts since the last submission (D47), clamped to 0 to 10,000,000. They never block a submit.
- The server also stores a hash of the client's IP address and user agent with each submission, an HMAC keyed by `PODIUM_SESSION_SECRET`, so it cannot be reversed by trying campus addresses.

429 bodies carry the wait, and the response has a `Retry-After` header with the same number:

```json
{ "error": { "code": "rate_limited", "message": "Submissions are limited to one every 5 seconds. Try again in 3 s.", "retry_after_s": 3 } }
```

### Run rules

| Rule | Value | Refusal |
|---|---|---|
| Pending runs | one per user, across problems | `429 rate_limited`: "Your last run is still going. Wait for it to finish." |
| Gap between runs | 2 s per user | `429 rate_limited`: "Runs are limited to one every 2 seconds." |
| Code and input | 64 KB each | `400 bad_request` |
| Problem | as for submit | `409 not_ready` with `problems` |

### Integrity events

`POST .../events` records what the editor saw (decision D47): `kind` is `paste` (with `size`, the pasted length in characters), `blur` or `focus` (window focus lost and back). At most 100 events per request. `at` is when it happened, in server Unix ms as the browser estimates it from `server_now`; events go in batches, so the server keeps `at` when it lies in the last 10 minutes and uses its own clock otherwise. Never keystrokes or text.

| Status | When |
|---|---|
| 204 | stored; also when this user already has 5,000 events on this problem, in which case the new ones are dropped, and in a contest outside the student's window, when nothing is recorded |
| 400 `bad_request` | an unknown `kind`, or more than 100 events |
| 404 `not_found` | no such problem |
| 429 `rate_limited` | more than one batch per user in 5 s; the web app sends a batch every 30 s and on submit |

Kinds, the rate and the 5,000 cap are checked before anything is written.

## Live updates

### `GET /api/events` (signed in)

A `text/event-stream` for the signed-in user, read with `EventSource`. Not JSON: `Cache-Control: no-store` and `X-Accel-Buffering: no`, and the connection stays open.

| Event | Data | When |
|---|---|---|
| `clock` | `{"server_now":1792183192400}` | at once on connecting, then every 25 s (it also keeps proxies from closing an idle stream) |
| `submission` | a `Submission` | when one of the user's submissions changes in a way the user can see |
| `run` | a `Run` | when one of the user's runs changes: a judge starts it, each input gets a verdict, and it finishes |
| `standings` | `{"contest":"week-6","contest_id":4,"version":12}` | a contest's board changed and the new board is ready (at most about once every 2 s per contest); refetch the board. In exam-mode contests it goes to organisers only |
| `announcement` | `{"contest","contest_id","id","body_md"}` | an organiser posted an announcement |
| `clarification` | `{"contest","contest_id","id"}` | a question was answered (to the asker; to everyone in the contest when public) or asked (to organisers); refetch the contest |
| `contest` | `{"contest","contest_id","ends_at"}` | the end moved: the whole contest was extended, or this student got extra time (`ends_at` is then their own end) |
| `wizard` | a `Wizard` | an AI wizard run or solve of this admin ended |
| `input_generation` | an `InputGeneration` | a generate inputs job this admin queued changed (claimed, reported, finished); the editor may also poll |
| `replaced` | `{}` | sent just before the server closes this stream because the user opened a fifth one; do not reconnect |

```
event: clock
data: {"server_now":1792183192400}

event: submission
data: {"id":80,"state":"running","verdict":"",...}
```

- Events for one run or submission arrive in order, and only when something visible changed. For an organiser that is each test; for a student it is the sample block finishing and each whole subtask finishing, never a single hidden test.
- Contest events go to the contest's members who have a stream open: organisers, and for a published contest the students who may enter it (anyone signed in for domain access, the roster otherwise). Membership is checked when each event is sent, so a student taken off the roster stops receiving them at once.
- On every `clock` tick the server checks the session again; a stream whose session has ended (signed out, expired, account disabled) is closed.
- Each user may hold 4 streams (tabs); a fifth replaces the oldest, which gets a `replaced` event and is closed. The web app then stops reconnecting that tab and offers to continue in the newer one.
- In a live contest, opening a stream from another browser or machine while one is open is recorded for the organisers as a "second session" (decision D47). Nothing is shown to the student.
- The server holds at most 5,000 streams. Beyond that: `503 busy` with `Retry-After: 30`, and the page polls instead.
- A stream that falls 16 events behind, or does not accept a write within 10 s, is closed; the browser reconnects.
- `501 unsupported` if the server cannot stream; the page polls instead.

The web app reconnects with jitter, refetches the user's submissions after every reconnect, and falls back to polling every 15 s after three failed attempts.

## Contests: students

Every route needs a signed-in user. Access: a published, unarchived contest with domain access is open to everyone who can sign in; with roster access, to the emails on its roster. Organisers (owners, admins, and the contest's co-organisers) always get in, see everything, and are never on the board. Times are Unix ms; every answer carries `server_now`, and every rule about time is applied with the server's clock.

| Route | Request | Success |
|---|---|---|
| `GET /api/contests` | | 200 `{live:[ContestSummary], upcoming:[...], past:[...], server_now}` |
| `GET /api/contests/{slug}` | | 200 `Contest` |
| `POST /api/contests/{slug}/start` | `{"accept_honour":true}` | 200 `Contest` |
| `GET /api/contests/{slug}/problems/{label}` | | 200 contest `Workspace` |
| `PUT /api/contests/{slug}/problems/{label}/draft` | as the workspace | 204 |
| `POST /api/contests/{slug}/problems/{label}/run` | as the workspace | 202 `Run` |
| `POST /api/contests/{slug}/problems/{label}/submit` | as the workspace | 202 `Submission` (200 for a repeated `client_key`) |
| `POST /api/contests/{slug}/problems/{label}/events` | as the workspace | 204 |
| `GET /api/contests/{slug}/standings` | | 200 `Standings` |
| `POST /api/contests/{slug}/clarifications` | `{"problem_label":"A" or null,"question":"..."}` | 201 `{clarification, server_now}` |

Errors: `404 not_found` for a contest that does not exist, is unpublished or archived, and for a label that is not in the contest; `403 not_on_roster` for a roster contest the student is not on.

Reads that cost several queries (the contest list, the contest page, the contest workspace, the standings, and the organisers' `GET /api/workspace/{problem}`) are limited per user to 10 a second on average with bursts of 20; beyond that `429 rate_limited` with `retry_after_s` and `Retry-After`. A page needs a handful.

### `ContestSummary` and `Contest`

```json
{ "slug": "week-6", "title": "Week 6", "starts_at": 1792310400000, "ends_at": 1792317600000, "start_mode": "fixed",
  "duration_minutes": 0, "problem_count": 3, "state": "live", "my_score": 140,
  "my_window": { "started_at": 1792310400000, "ends_at": 1792317600000 } }
```

| Field | Meaning |
|---|---|
| `ends_at` | when the contest closes for everyone: its end plus any contest-wide extension |
| `state` | for this student: `upcoming`, `live`, `frozen` (fixed start, during the freeze), `ended`, or `practice` (ended, practice open). A personal extension keeps it `live` for them; in window mode their own clock decides |
| `my_score` | their total from their own submissions (never frozen); `null` before they accept the rules, and in exam mode until their window closes |
| `my_window` | their window: fixed start, the contest's times plus their extension; window mode, from their Start for `duration_minutes` plus extensions, never past the close plus their extension; `null` in window mode before Start |

`Contest` adds:

```json
{ "description_md": "...", "honour_text": "This contest is graded. ...", "accepted": true, "organiser": false,
  "rules": { "feedback_mode": "subtask", "scoring_mode": "best_subtask", "min_gap_s": 30, "max_per_problem": 15,
             "languages": ["cpp17", "python3"], "freeze_minutes": 30, "graded": true, "practice_after_end": true },
  "problems": [ { "label": "A", "title": "A plus B", "my_score": 40, "max_score": 100, "my_submission_count": 3 } ],
  "announcements": [ { "id": 3, "body_md": "...", "created_at": 1792311000000 } ],
  "my_clarifications": [ Clarification ], "public_clarifications": [ Clarification ],
  "editorial_md": null, "server_now": 1792311000000 }
```

- `problems` is empty until the student has accepted the rules and their window has opened (fixed start: the contest has started; window mode: they pressed Start), and once their window is over. Per-problem `my_score` is `null` when it would be hidden.
- `honour_text` is the contest's honour code, or the default listing what Podium records and for how long (D47).
- `my_submission_count` is how many submissions the student made to the problem, practice and judge failures excluded. It counts submissions as they are made, never how far judging got, so in exam mode it is the one per-problem figure shown and says nothing about results.
- `editorial_md` is set only when the editorial is published and every student's window is over (the close plus the longest personal extension).
- `Clarification`: `{id, problem_label, question, answer, public, created_at, answered_at}`. Students never see who asked a public question.

### Start

`POST .../start` with `{"accept_honour": true}` records acceptance of the honour code. In window mode it also starts the student's clock; pressing it again changes nothing. It is needed in both modes before the problems open. Refusals, `400 bad_request` with `fields.accept_honour`: not accepted; "The contest has not started yet."; window mode after the close; fixed start after the end ("The contest has ended.").

### Contest workspace

The M3 `Workspace` plus:

```json
{ "label": "A", "contest": "week-6", "window": { "started_at": 1792310400000, "ends_at": 1792317600000 },
  "feedback_mode": "subtask", "can_submit": true, "practice": false, "refusal": null }
```

- `languages` lists only the contest's languages; others are refused with `400` ("This contest allows C++17 and Python 3.").
- Submissions are counted while the student's own window is open. After it, they are practice (judged at a lower priority, never on the board) when the contest allows practice and every student's window is over; otherwise run and submit are refused. A submission the judge could not finish does not count toward `max_per_problem`.
- Co-organisers may try the contest's problems at any time, with the contest's gap and per-problem limit and the student view of their results.
- `can_submit` and `refusal` say whether run and submit work now, and why not.

| Refusal | When |
|---|---|
| `403 not_started` "Accept the rules on the contest page to start." | before the student's window opens (also for reading the problem) |
| `403 contest_over` "The contest has ended." | the window is over and practice is off |
| `403 contest_over` "Your time is up. Practice opens when every student's time is up." | the student's time is up but others are still in their windows (window mode, or a personal extension) |
| `409 not_ready` | the problem changed since it was added to the contest (organisers must validate it and add it again) |
| `429 rate_limited` | one pending submit per problem, the contest's gap between submits (`min_gap_s`, across problems) |
| `400 bad_request` | the problem's `max_per_problem` submits are used ("You have used all 15 submissions for this problem.") |

### `Standings`

```json
{ "variant": "frozen", "version": 12, "frozen_at": 1792315800000, "final": false, "judging_remaining": 2,
  "labels": ["A", "B"],
  "rows": [ { "rank": 1, "user_id": 7, "name": "Asha R", "total": 140, "last_improvement_s": 2400,
              "cells": [ { "label": "A", "score": 100, "max": 100, "pending": 0, "tried": true },
                         { "label": "B", "score": 40, "max": 100, "pending": 1, "tried": true } ] } ],
  "me": 7, "server_now": 1792316000000 }
```

- Rank by `total`, then by `last_improvement_s` (seconds from the student's start to when they reached their total); equal totals and times share a rank.
- `pending` counts submissions made after the freeze (frozen board) that the board does not show yet.
- `name` is the roster name, then the account name, then "Student {id}"; never an email.
- `final` is true on the full board once every window has closed and nothing is waiting for the judge.

Which board a student gets:

| Contest | During | After |
|---|---|---|
| Window mode, or exam feedback | `403 board_closed` "The board opens when your window closes." until the student's own window closes | full (frozen while a held reveal is pending) |
| Fixed start | full until the freeze, then frozen | frozen until every student's window has closed and, with `hold_reveal`, until an organiser reveals; then full |

### Clarifications

At most 5 unanswered questions per student per contest (`400` with `fields.question`); questions close when the student's window does (`403 contest_over`). `problem_label` must be a label of the contest or null.

## Contests: organisers

Owners and admins organise every contest; a co-organiser organises only the contests they were added to. For anyone else, every route below answers `404 not_found`, so ids cannot be probed. Creating, cloning, archiving, choosing co-organisers, choosing the contest's problems and publishing or unpublishing need the admin or owner role (`403 forbidden`): problems come from the bank, which co-organisers (often student TAs) never see, and publishing hands them to students. Every id in a URL or body (a submission, a clarification, a label) is checked against the contest in the URL; one from another contest is a `404`.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/contests` (`?archived=1`) | | 200 `{contests:[AdminContest], server_now}` (a co-organiser sees their own) |
| `POST /api/admin/contests` | `ContestEdit` | 201 `AdminContest` (unpublished) |
| `GET /api/admin/contests/{id}` | | 200 `AdminContest` |
| `PUT /api/admin/contests/{id}` | `ContestEdit` (fields left out keep their value) | 200 `AdminContest` |
| `DELETE /api/admin/contests/{id}` | | 204 (archived; nothing deleted) |
| `POST /api/admin/contests/{id}/clone` | `{slug, title, starts_at, ends_at}` | 201 `AdminContest`: settings and problems copied, not the roster, co-organisers, extensions or editorial |
| `PUT /api/admin/contests/{id}/problems` (admin) | `[{"problem_id":3,"label":"A"}]` | 200 `AdminContest`; each problem must be validated and not tagged `ai draft` (a person saves AI drafts first); its content hash and subtask points are pinned. Errors name the label, never the problem's title |
| `GET /api/admin/contests/{id}/roster` | | 200 `{roster:[{email,name,roll_number,section}], count}` |
| `PUT /api/admin/contests/{id}/roster` | `text/csv` (header row naming `email`, and optionally `name`, `roll_number`, `section`) or a JSON array of the same objects; at most 2 MB and 5,000 students | 200 `{added, removed, roster_count}`. A full replace: on a roster contest, anyone left out loses access at once, so show the counts before and after |
| `POST`, `DELETE /api/admin/contests/{id}/admins` | `{"email":"ta@college.edu"}` (they must have signed in once) | 200 `AdminContest` |
| `PUT /api/admin/contests/{id}/extensions/{user}` | `{"minutes":15,"reason":"..."}` (0 to 1440; 0 removes) | 200 `AdminContest`; the student gets a `contest` event |
| `POST /api/admin/contests/{id}/extend` | `{"minutes":10}` (1 to 240, adds to earlier extensions) | 200 `AdminContest`; members get a `contest` event |
| `POST /api/admin/contests/{id}/pause`, `/resume` | | 200 `AdminContest` |
| `GET /api/admin/contests/{id}/submissions?problem=A&user=7&verdict=WA&page=1` | | 200 `{submissions:[...], page, has_more, server_now}` |
| `POST /api/admin/contests/{id}/rejudge` | `{"problem_label":"A"}` or `{"submission_id":80}` | 202 `{queued, server_now}` |
| `GET`, `POST /api/admin/contests/{id}/announcements` | `{"body_md":"..."}` (at most 4,000 characters) | 200 `{announcements, server_now}`; 201 `Announcement` |
| `GET /api/admin/contests/{id}/clarifications` | | 200 `{clarifications, server_now}`, each with `asked_by {user_id, email, name}` |
| `PUT /api/admin/contests/{id}/clarifications/{cid}` | `{"answer":"...","public":false}` | 200 `Clarification` |
| `GET /api/admin/contests/{id}/standings` (`?variant=frozen`) | | 200 `Standings` (full unless asked) |
| `GET /api/admin/contests/{id}/reveal` | | 200 `{frozen: Standings, full: Standings, revealed_at, server_now}` |
| `POST /api/admin/contests/{id}/reveal` | | 200 `AdminContest`; students held on the frozen board see the full one |
| `GET /api/admin/contests/{id}/export.csv` | | 200 `text/csv` attachment |
| `GET /api/admin/contests/{id}/export.json` | | 200 JSON attachment |
| `GET /api/admin/contests/{id}/stats` | | 200 `{problems:[ProblemStats], server_now}` |
| `PUT /api/admin/contests/{id}/editorial` | `{"editorial_md":"...","published":true}` (at most 64 KB) | 200 `AdminContest` |

### `ContestEdit` and `AdminContest`

`ContestEdit` holds the form's fields: `slug`, `title`, `description_md`, `starts_at`, `ends_at`, `start_mode` (`fixed` or `window`), `duration_minutes` (window mode, at least 10 and at most the whole window), `freeze_minutes`, `scoring_mode` (`best_subtask` or `best_submission`), `access` (`domain` or `roster`), `feedback_mode` (`subtask` or `exam`), `graded`, `published`, `practice_after_end`, `stop_on_sample_fail`, `min_gap_s` (0 to 3600), `max_per_problem` (1 to 1000), `languages`, `honour_text` (empty for the default), `hold_reveal`. Defaults on create: fixed start, best per subtask, domain access, subtask feedback, graded, practice after the end, stop on a failing sample, 30 s gap, 15 per problem, all four languages. Unknown fields are refused. Publishing checks that every problem is still the validated version that was added ("Problem B changed or lost its validation since it was added. Validate it and add it again."). Invalid fields: `400 bad_request` with `fields`.

`AdminContest` is every `ContestEdit` field plus `id`, `close_at` (end plus contest-wide extension), `default_honour_text`, `editorial_md`, `editorial_published`, `paused`, `extended_minutes`, `revealed_at`, `archived`, `state`, `problems [{label, problem_id, title, pinned_hash, ready, max_score}]` (`ready` is false when the problem changed since it was added), `admins [{user_id, email, name}]`, `extensions [{user_id, email, name, minutes, reason}]`, `roster_count`, `created_at`, `updated_at`, `server_now`.

### Running a contest

- **Pause** stops the judge from taking the contest's submissions and runs; they still queue, and nothing is lost. **Resume** lets them through in order.
- **Rejudge** queues every submission of a problem (or one submission) again at priority 50 with the problem's current tests; each goes back to `queued` and counts again once judged. Submissions still waiting for the judge, or being judged, on the old tests are included: their old job is cancelled and replaced. A submission already waiting on exactly the current tests is left alone, so pressing Rejudge twice adds nothing (`queued` counts only what was queued). The problem must still match its pin: after editing tests, validate the problem and add it to the contest again first (`409 not_ready` otherwise). A rejudge waiting in the queue never blocks the student from submitting.
- **Feed** rows: `{id, user_id, name, email, problem_label, language, state, verdict, score, max_score, created_at, judged_at, practice, rejudged, typed_chars, pasted_chars, focus_losses}`, newest first, 50 per page.
- **Reveal** answers `409 too_early` "The reveal opens when every student's time is up." until the close plus the longest personal extension. The web app walks the frozen board from the bottom, moving each row to its place on the full board.

### Export and stats

- CSV columns: `rank, name, email, roll_number, section, started` (UTC, RFC 3339), `total`, then one column per problem label. A cell starting with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading `'`, so a spreadsheet never runs a name as a formula.
- JSON: `{contest: AdminContest, standings: [Standings row plus email, roll_number, section, started_at], submissions: [{id, user_id, problem_label, language, state, verdict, score, created_at, practice}], exported_at}`. No code and no integrity data.
- `ProblemStats`: `{label, title, max_score, attempted, solved` (full score)`, subtasks [{idx, points, earned}], histogram [{score, students}]` (highest score first)`, median_attempts}`, over students on the board and their non-practice submissions.

## Integrity

Checks for copying after a contest, and the review of what they find (decisions D47 and D49; the reviewer's guide is `docs/INTEGRITY.md`). Signals are reasons to look, not proof: a person opens a case, the students explain, and a person decides with a written reason.

**Who may review.** The owner and admins, and co-organisers of that contest whose own role is not student. A co-organiser who is a student gets `403 forbidden` on the contest's integrity routes and `404` on case routes; anyone who does not organise the contest gets `404 not_found`, as on every organiser route. A signal, student or case id from another contest is a `404`, and a case cannot be opened from another contest's signals (`400`). Every answer carries `server_now`.

| Route | Request | Success |
|---|---|---|
| `POST /api/admin/contests/{id}/integrity/run` | none | 202 `{run_id, server_now}`; the check runs in the background |
| `GET /api/admin/contests/{id}/integrity` | | 200 `Run` plus `users`, `problems` and `server_now`; `404 not_found` "This contest has not been checked yet." |
| `GET /api/admin/contests/{id}/integrity/signals/{signal}/pair` | | 200 `{a: Side, b: Side, server_now}` for a `similar` signal; any other kind is a `404` |
| `GET /api/admin/contests/{id}/integrity/timeline/{user}` | | 200 `{moments: [Moment], server_now}`; `404` when the user neither accepted the rules nor submitted in this contest |
| `GET /api/admin/contests/{id}/integrity/cases` | | 200 `{cases: [Case], server_now}`, newest first |
| `POST /api/admin/contests/{id}/integrity/cases` | `{title, note, signal_ids: [n]}` | 201 `Case` |
| `GET /api/admin/integrity/cases/{case}` | | 200 `Case` |
| `PUT /api/admin/integrity/cases/{case}` | `{state, note}` | 200 `Case`; appends a decision |

Students (any signed-in user):

| Route | Request | Success |
|---|---|---|
| `GET /api/integrity/cases` | | 200 `{cases: [StudentCase], contests: {id: {slug, title}}, problems: {id: {label, title}}, server_now}`: only cases shared with the user |
| `PUT /api/integrity/cases/{case}/explanation` | `{explanation}` (1 to 8,000 characters) | 204; editable until the case is decided |

### Shapes

- `Run`: `{id, state` (`running`, `done` or `error`)`, started_at, finished_at|null, error?, stats: {students, submissions, problems, signals: {kind: n}, clusters}, signals: [Signal]}`. Signals come strongest first. `error` is a sentence for people; the cause is in the server log.
- `Signal`: `{id, problem_id, kind, strength, score, user_a, user_b?, submission_a?, submission_b?, evidence}`. `kind` is `similar`, `wrong_output`, `hardcoded` or `probing`; `strength` is `strong`, `moderate` or `weak`, with the raw `score` beside it (containment for `similar`, the number of tests for `wrong_output` and `hardcoded`, distinct results for `probing`). `evidence`: similar `{shared, spans_a, spans_b, language_a, language_b}`; wrong_output and hardcoded `{tests: [n]}`; probing `{distinct, attempts}`.
- `users`: `{id: {name, email, roll_number}}` for every user a signal names. The name and roll number come from the contest's roster when the student is on it, otherwise the account's name and an empty roll number.
- `problems`: `{id: {label, title}}` for the contest's problems.
- `Side`: `{submission_id, user_id, language, source, created_at, spans: [{start, end}]}`; spans are byte offsets into `source`.
- `Moment`: `{at, kind, problem_id?, text}`; `kind` is `submission`, `paste`, `blur`, `focus`, `second_session` or `shared_device`, oldest first. `shared_device` names the other student ("Same browser and network as ...") and is never a signal.
- `Case`: `{id, contest_id, state, title, evidence: [Signal], created_at, updated_at, users: [{user_id, name, shared_at|null, explanation, explained_at|null}], decisions: [{by, state, note, created_at}]}`. `evidence` is a copy of the signals taken when the case opened, so a later check cannot change it. The students are the users its signals name.
- `StudentCase`: `{id, contest_id, state, title, evidence: [{problem_id, kind, text, submission_id?, source?, spans?}], explanation, shared_at, decided}`. Only the student's own evidence, worded for them; `source` and `spans` are their own program for `similar`. Never another student's name, email or code, never the case title (`title` here is the contest's title, because a reviewer's title may name people) and never the decision notes.

### Rules and errors

| Status | When |
|---|---|
| 409 `contest_live` | "Check integrity after the contest ends for everyone.": before the close plus the longest personal extension, the moment practice and the editorial open |
| 409 `busy` | "A check is already running. Try again in a minute.": one check runs at a time on the server, for any contest |
| 400 `bad_request` | a title empty or over 120 characters; a note under 20 or over 4,000 characters; no signals or more than 50; a signal not in the contest's latest check; an unknown state; `actioned` before every student in the case was shared it ("Share the case with the students and give them time to explain before taking action."); an empty explanation |
| 404 `not_found` | explaining a case not shared with the user, or already `cleared` or `actioned` |
| 429 `rate_limited` | the read routes share the per-user read limit |

- States: `open`, `awaiting_student` (shared: the students see their evidence and explain), `cleared`, `actioned`. Any state can follow any other, each with a note; the decision log is append only.
- A new check replaces the contest's previous signals; cases keep their copy.
- Case opens, state changes and explanations are logged at info with ids only (`by`, `contest_id`, `case_id`, `state`), never notes, explanations or code.

### Changes after the integrity review

#### Changed: `GET /api/admin/integrity/cases/{case}` and every route that returns a Case

`users[]` gains three fields; `decisions[]` gains two.

```json
{
  "users": [{
    "user_id": 7, "name": "Asha Rao",
    "shared_at": 1792300000000,
    "asked_at": 1792300000000,
    "respond_by": 1792904800000,
    "explanation": "latest text", "explained_at": 1792300500000,
    "explanations": [{ "text": "first", "created_at": 1792300400000 }, { "text": "latest text", "created_at": 1792300500000 }]
  }],
  "decisions": [{
    "by": 2, "by_name": "Arjun Mehta", "state": "cleared", "note": "...", "created_at": 1792300600000,
    "explanations_seen": { "7": 1792300500000 }
  }]
}
```

- `shared_at`: the first share; the student sees the case from then on.
- `asked_at`: the latest share, which starts the student's turn. `null` before the first share and after a reopen.
- `respond_by`: `asked_at` plus the response window (7 days). `null` with `asked_at`.
- `explanations`: every saved version, oldest first. `created_at` is unique per case and student and names the version. `explanation` and `explained_at` stay as the latest version.
- `by_name`: the reviewer's name (or email); `""` if unknown.
- `explanations_seen`: per student id, the explanation version the reviewer had read when deciding.

A reviewer who is one of a case's students (a student later made an admin) gets 404 on the case, its pair view and its decision, and the case is left out of `GET /api/admin/contests/{id}/integrity/cases`.

#### Changed: `PUT /api/admin/integrity/cases/{case}`

Request: `{ "state": "...", "note": "...", "seen_explanations": { "7": 1792300500000 } }`.

- `seen_explanations`: per student id, the `created_at` of the latest explanation version the reviewer read (the web sends the latest of each student as loaded). If any student has a newer version, or one the map leaves out: **409** `explanation_changed`, "A student changed their explanation. Reload before deciding."
- Allowed changes (anything else is 400 `bad_request` with one of the sentences below):

| From | To |
|---|---|
| open | awaiting_student, cleared |
| awaiting_student | cleared, actioned, open |
| cleared, actioned | open (reopen) |

- `awaiting_student` sets each student's `asked_at` (and `shared_at` the first time). `open` from a decided case clears `asked_at`: a new share is needed before action.
- `actioned` needs every student to have an explanation version created at or after `asked_at`, or `asked_at` at least 7 days ago.
- Refusal sentences (400 `bad_request`):
  - "Unknown case state."
  - "The case is already open. Reload to see its latest state." (or "shared with the students", "cleared", "closed with action taken")
  - "Share the case with the students before taking action. They explain first."
  - "The case is decided. Reopen it before changing the decision."
  - "Every student needs to explain, or to have had 7 days since the case was last shared, before you take action."

#### New: `GET /api/admin/integrity/cases/{case}/evidence/{index}/pair`

The side-by-side view of a case's similar-code evidence, from the case's own copy of the signal (index from 0 in the case's `evidence` array), never the live signals. Same access as the case (404 for anyone who may not review it). The submissions must still belong to the case's contest and to the students the signal names.

```json
{ "a": Side, "b": Side, "signal": Signal, "server_now": 1792300000000 }
```

`Side` is the shape of `GET .../integrity/signals/{signal}/pair`. 404 `not_found`, "There is no such similar-code evidence in this case, or its code is no longer kept.", for a bad index, a signal that is not similar code, emptied evidence (2 years after the decision) or a submission no longer in the contest.

#### Changed: similar-code signal evidence

`evidence` of a `similar` signal gains `fingerprints` (the smaller program's fingerprints, the score's denominator) and `tokens` (the shorter program's length in tokens), next to `shared`. Strength now needs both score and amount: strong at score >= 0.9 and shared >= 40, moderate at >= 0.75 and >= 25, otherwise weak.

#### Changed: `GET /api/admin/contests/{id}/submissions` (console feed)

`typed_chars`, `pasted_chars` and `focus_losses` are present only for owners and admins. For a contest co-organiser whose site role is student they are left out of each item.

#### Changed: `PUT /api/integrity/cases/{case}/explanation`

Unchanged request and responses. Each save now adds a version; earlier versions are kept for reviewers.

## Judge worker API

Used only by `podium-judge work` (decision D44). It is served on its own listener, `PODIUM_JUDGE_LISTEN` (default `127.0.0.1:8081`, loopback only), never through Caddy, and is not part of the public API above: no session cookies, no cross-site check. The request and response types live in `server/internal/judgeapi`, shared by both programs.

- **Auth.** Every request sends `Authorization: Bearer <PODIUM_JUDGE_TOKEN>`. The server compares SHA-256 digests of the two tokens in constant time. Anything else is `401 unauthorized`.
- **Bodies.** JSON with no unknown fields; at most 64 KB for a claim and 1 MB for a report. A malformed body is `400 bad_request`. An unknown route is `404 not_found`. The one non-JSON body is an uploaded output (`PUT /api/judge/blob/{sha}`, at most 16 MB).

### `POST /api/judge/claim`

Request: `{"kinds":["run","submit","generate","validate"],"worker":"vm-1","version":"v0.3.0","isolate":"2.7"}`. `worker`, `version` and `isolate` are logged and stored with the job.

| Status | Body |
|---|---|
| 204 | no queued job of those kinds (unknown kinds are ignored) |
| 200 | `{"job_id":41,"kind":"validate","lease":"<secret>","lease_seconds":60,"payload":{...}}` |

The next job is the queued one with the lowest priority number, then the oldest, with three exceptions:

- Jobs of a paused contest are skipped (they wait in the queue until it resumes).
- After a `run` is claimed, the next claim takes the best job of any other kind when one is waiting, so runs (priority 10) can never starve submissions: at least every other claim goes to other work.
- While a graded contest is live (from its start to its close plus the longest personal extension), at most one `validate` or `generate` job is leased at a time; a worker asking only for those gets 204 until it finishes.

| Kind | Priority | What the worker does |
|---|---|---|
| `run` | 10 | compile, run each input, check samples against their expected output, report an output excerpt |
| `submit` | 20 plus the number of this user's judged submissions on this problem, at most 30 | compile, judge the samples, then each subtask in order (rules below) |
| rejudge (`submit`) | 50 | as `submit` |
| practice (`submit`) | 60 | as `submit` |
| `generate` | 85 | compile the reference, run it on each input, upload each output |
| `validate` | 90 | the M2 validation run |

The `submit` priority is the fair queue: a student's first submission on a problem goes ahead of someone's tenth.

The `validate` payload:

```json
{ "problem_id": 3, "content_hash": "9f2c...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": { "type": "custom", "epsilon": 1e-6, "source_sha": "ab12..." },
  "programs": { "reference": { "language": "cpp17", "source_sha": "cd34..." },
                "brute": { "language": "node22", "source_sha": "ef56..." }, "validator": null },
  "tests": [ { "idx": 1, "sample": true, "input_sha": "...", "output_sha": "..." } ],
  "points": 100 }
```

`checker.source_sha` is present only for `custom`. `points` is the subtask total, used only for advice in the summary.

The `submit` payload lists tests samples first, then each subtask's tests in subtask order:

```json
{ "submission_id": 80, "program": { "language": "cpp17", "source_sha": "cd34..." },
  "time_limit_ms": 1000, "memory_limit_mb": 256, "checker": { "type": "tokens", "epsilon": 1e-6 },
  "tests": [ { "idx": 1, "sample": true, "subtask": 0, "input_sha": "...", "output_sha": "..." },
             { "idx": 3, "sample": false, "subtask": 1, "input_sha": "...", "output_sha": "..." } ],
  "subtasks": [ { "idx": 1, "points": 40 } ],
  "stop_on_sample_fail": true }
```

The worker judges in that order. A failing sample stops everything when `stop_on_sample_fail` is set; a failing test stops the rest of its subtask. Every test not run is still reported, as `SK`.

The `run` payload: `submission_id`, `program`, limits, `checker`, and `inputs: [{"idx":1,"input_sha":"...","expected_sha":"..."}]`. Samples carry `expected_sha`; a custom input has `idx` 0 and none.

The `generate` payload: `problem_id`, `content_hash`, `program` (the reference), limits, `all`, and `inputs: [{"idx":5,"input_sha":"..."}]`.

### `GET /api/judge/blob/{sha}`

Headers: `X-Podium-Job: <job_id>` and `X-Podium-Lease: <lease>` (in headers so they are never logged with the URL).

| Status | Body |
|---|---|
| 200 | the file, `application/octet-stream`, with `Content-Length` |
| 404 `not_found` | the hash is malformed, the job header is missing, the lease is not current, the file is not one this job needs, or it does not exist |

A worker can fetch only the sources, checker and tests of a job it holds, so a leaked token alone does not expose the problem bank. The worker re-hashes every download before using or caching it.

### `PUT /api/judge/blob/{sha}`

Uploads one output of a `generate` job. Headers: `X-Podium-Job` and `X-Podium-Lease` as for downloads; body the raw bytes (`application/octet-stream`), at most 16 MB; `{sha}` is the SHA-256 of the body in hex.

| Status | Body |
|---|---|
| 204 | stored, and noted as uploaded by this job |
| 409 `lease` | the lease is not current, the job is not a `generate` job, or the job has used its upload budget (one per input, plus 2 for retries). The slot is reserved before the body is read and given back if the upload fails, so concurrent uploads under one lease cannot exceed the budget. The body streams to a temporary file while it is hashed (nothing is held in memory) and is renamed into place only if the hash matches. Uploads nothing ended up using are deleted when the job closes or its lease is swept |
| 413 `too_large` | the body is over 16 MB |
| 400 `bad_upload` | the body does not hash to `{sha}` |

A `generate` report names each output by its hash (`output_sha`), and the server refuses any hash this job did not upload, so a worker cannot point a test at some other file in the store.

### `POST /api/judge/report`

```json
{ "job_id": 41, "lease": "<secret>",
  "compile": [ { "role": "reference", "verdict": "OK", "output": "" } ],
  "results": [ { "idx": 1, "role": "reference", "verdict": "AC", "time_ms": 2, "memory_kb": 980, "message": "" } ],
  "final": false, "fatal": "" }
```

| Status | Body |
|---|---|
| 200 | `{"lease_seconds":60}`: the lease now lasts that long again |
| 400 `bad_report` | the results do not fit the job; `message` gives the reason |
| 409 `lease` | the lease is unknown, expired and handed to another worker, or the job is closed. Drop the job |

Rules:

- Results are appended. Each compile role, and each (test, role) pair, may be reported once; a repeat is refused.
- The server checks every result against the payload: the test exists, the role is one the job has (`checker` only in `compile`), compile verdicts are `OK` or `CE`, test verdicts are from the list above (not `OK`), time is 0 to 600,000 ms, memory 0 to 8 GB, messages at most 256 bytes and compile output at most 16 KB, all valid UTF-8.
- Results also carry `output` and `output_sha` (both may be left out). What each kind accepts:

| Kind | Compile roles | Result role | Verdicts | `output` | `output_sha` | `message` |
|---|---|---|---|---|---|---|
| `validate` | as above | as above | as above | not used | not used | allowed |
| `submit` | `program`, `checker` | `program` | `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK`, `IE` | refused: hidden tests stay hidden | optional, the hash of the program's output (kept for later integrity checks) | samples only |
| `run` | `program`, `checker` | `program` | any, including `OK` for a custom input | at most 4 KB, UTF-8 | optional | allowed |
| `generate` | `program` | `program` | any | refused | the uploaded output, which must be one this job uploaded | at most 256 bytes |
- An empty report is a heartbeat. The worker sends one every 20 s while a long compile or test runs.
- `final: true` closes the job. The server computes the summary, the score and the verdict itself from the stored results; it never takes a whole-problem or whole-submission verdict from the judge.
- A non-empty `fatal` closes the job as an error (the run could not happen, for example isolate failed). The text, up to 500 characters, is shown to the setter.

### Leases and deadlines

| Rule | Value |
|---|---|
| Lease length | 60 s from the claim or the last report |
| Expiry | a sweep every 10 s returns jobs with an expired lease to the queue and deletes their results; the next worker starts the job from the beginning |
| Attempts | the third expired lease fails the job: "The judge stopped responding three times. Check podium-judge's log." |
| Deadline | set at the first claim, kept across attempts. A job still running then fails even if its worker keeps reporting |
| `validate` deadline | 4 minutes plus, per test, 3 minutes and six times the time limit, at most 2 hours |
| `submit` deadline | 2 minutes plus, per test, 25 s and six times the time limit, at most 1 hour |
| `run` deadline | 2 minutes plus, per input, 25 s and six times the time limit, at most 30 minutes |
| `generate` deadline | 2 minutes plus, per input, 30 s and six times the time limit, at most 1 hour |
| A failed job | a submission ends in `error` and is not counted; a run ends in `error`; a generation ends in `error` and applies nothing |
| Storage | the lease is stored only as its SHA-256 |

## Static files

| Path | Caching |
|---|---|
| `/assets/*` (hashed build output) | `public, max-age=31536000, immutable` |
| other files (`/favicon.svg`, `/fonts/*`, `/theme-init.js`) | `public, max-age=3600` |
| missing files with an extension | `404` |
| any other path | `index.html`, `no-cache` |

Contests are added to this document in M4, the milestone that builds them.
