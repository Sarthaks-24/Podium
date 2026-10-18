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

Common error codes: `bad_request`, `signed_out`, `forbidden`, `not_found`, `cross_site`, `conflict`, `internal`. The problem routes add `bad_pack`, `not_ready`, `too_large` and `busy`; the workspace routes add `not_ready` and `rate_limited`; the event stream adds `busy` and `unsupported`; the judge API adds `unauthorized`, `lease`, `bad_report`, `bad_upload` and `too_large`.

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

An `{id}` that is not a problem gives `404 not_found` on every route.

### Shapes

`ProblemSummary`:

```json
{ "id": 3, "slug": "a-plus-b", "title": "A plus B", "difficulty": "easy", "tags": ["math"],
  "origin": "import", "archived": false, "test_count": 6, "points": 100,
  "validated": true, "validated_at": 1792183192359, "updated_at": 1792183100000 }
```

- `difficulty` is `easy`, `medium`, `hard` or `""`. `origin` is `manual` or `import`.
- `validated` is true when the last passing validation covers the problem as it is now (its content hash equals the problem's). `validated_at` is `null` whenever `validated` is false.
- Times are Unix milliseconds.

`Problem` is `ProblemSummary` plus:

```json
{ "statement_md": "Read two integers...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": "tokens", "epsilon": 1e-6, "checker_source": "",
  "reference": { "language": "cpp17", "source": "#include <cstdio>..." },
  "brute": { "language": "python3", "source": "..." },
  "validator": null,
  "samples": [1, 2],
  "subtasks": [ { "idx": 1, "points": 40, "title": "Small numbers", "tests": "3-4" } ],
  "tests": [ { "idx": 1, "sample": true, "subtask_idx": null, "input_size": 8, "output_size": 3,
               "input_preview": "2\n1 2\n", "output_preview": "3\n", "output_missing": false } ],
  "content_hash": "9f2c...", "validated_hash": "9f2c...",
  "problems": [], "warnings": ["No input validator: validation cannot check inputs against the constraints."] }
```

- `language` is `cpp17`, `java21`, `python3` or `node22`. `brute` and `validator` are `null` when the problem has none.
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

## Workspace, runs and submissions

The workspace is the page where code is written, run and submitted. In M3 only organisers reach it, to try a problem as a student would ("Try as a student"); submissions made there count in no contest. M4 mounts the same shapes under contests for students.

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

`PUT .../draft` stores the editor's code for this user and problem, replacing the last one. The web app saves every 10 s and when the editor or window loses focus. Errors: `400 bad_request` (unknown language, code over 64 KB, not UTF-8), `404 not_found`.

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

Times are rounded to 10 ms. A run uses the problem's limits and checker. Running the samples on a problem without samples is `400 bad_request` ("Run it on your own input instead").

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

Coarse figures for students: a student's own live events (see "Live updates") show no memory figure for hidden tests (`memory_kb` 0), `max_memory_kb` rounded up to the next 16 MB, and rounded times. In M3 only organisers submit, and the `GET` routes and the workspace list return the full figures.

### Submit rules

| Rule | Value | Refusal |
|---|---|---|
| Pending submissions | one per user per problem | `429 rate_limited`: "Your last submission for this problem is still being judged. Wait for its result." |
| Gap between submits | 5 s per user, across all problems (organisers trying problems; contests set their own in M4) | `429 rate_limited`: "Submissions are limited to one every 5 seconds. Try again in 3 s." |
| Code | at most 64 KB, UTF-8, a known language | `400 bad_request` |
| `client_key` | optional, at most 64 characters | `400 bad_request` |
| Problem | tests, samples and subtasks fit together and every test has an expected output; validation is not required to try a problem | `409 not_ready` with `problems` |

- `client_key` is a random value the browser makes per press of Submit and reuses on a retry. Keys are per user and problem (the same key on another problem is a new submission). The same key from the same user on the same problem returns the first submission with `200` and counts nothing again, so a retry after a timeout never trips the rate limit.
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
| 204 | stored; also when this user already has 5,000 events on this problem, in which case the new ones are dropped |
| 400 `bad_request` | an unknown `kind`, or more than 100 events |
| 404 `not_found` | no such problem |

## Live updates

### `GET /api/events` (signed in)

A `text/event-stream` for the signed-in user, read with `EventSource`. Not JSON: `Cache-Control: no-store` and `X-Accel-Buffering: no`, and the connection stays open.

| Event | Data | When |
|---|---|---|
| `clock` | `{"server_now":1792183192400}` | at once on connecting, then every 25 s (it also keeps proxies from closing an idle stream) |
| `submission` | a `Submission` | when one of the user's submissions changes in a way the user can see |
| `run` | a `Run` | when one of the user's runs changes: a judge starts it, each input gets a verdict, and it finishes |

```
event: clock
data: {"server_now":1792183192400}

event: submission
data: {"id":80,"state":"running","verdict":"",...}
```

- Events for one run or submission arrive in order, and only when something visible changed. For an organiser that is each test; for a student it is the sample block finishing and each whole subtask finishing, never a single hidden test.
- Each user may hold 4 streams (tabs); a fifth replaces the oldest, which the server closes.
- The server holds at most 5,000 streams. Beyond that: `503 busy` with `Retry-After: 30`, and the page polls instead.
- A stream that falls 16 events behind, or does not accept a write within 10 s, is closed; the browser reconnects.
- `501 unsupported` if the server cannot stream; the page polls instead.

The web app reconnects with jitter, refetches the user's submissions after every reconnect, and falls back to polling every 15 s after three failed attempts.

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

The next job is the queued one with the lowest priority number, then the oldest.

| Kind | Priority | What the worker does |
|---|---|---|
| `run` | 10 | compile, run each input, check samples against their expected output, report an output excerpt |
| `submit` | 20 plus the number of this user's judged submissions on this problem, at most 30 | compile, judge the samples, then each subtask in order (rules below) |
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
| 409 `lease` | the lease is not current, the job is not a `generate` job, or the job has used its upload budget (one per input, plus 2 for retries) |
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
