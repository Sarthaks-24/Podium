# HTTP API

All API routes live under `/api/` and speak JSON. Every other path serves the web app, which renders its own pages; from v1.3.0 the server answers a path no route matches with status 404 and fills each page's head (see "The page head, 404s, robots and the sitemap").

## Conventions

- Responses are `application/json; charset=utf-8` with `Cache-Control: no-store`.
- Errors use one shape, with a stable machine code and a sentence for people. The web app shows `message` as is.

```json
{ "error": { "code": "not_found", "message": "This API route does not exist." } }
```

- Request bodies are JSON, sent with `Content-Type: application/json` (anything else gets `415 bad_request`), at most 64 KB unless a route says otherwise, with no unknown fields.
- Every response carries a strict Content-Security-Policy (`script-src 'self'`, no inline scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: same-origin`.
- **Sessions.** Signing in sets an HttpOnly session cookie, `SameSite=Lax`, with no `Expires` or `Max-Age`: the browser forgets it when it closes, which matters on shared lab PCs. The server ends every session 12 hours after sign-in however much it is used (a fixed limit, `auth.SessionTTL`; sessions never renew), at sign-out, when an owner ends the person's sessions or disables the account, and when the server starts in a different `PODIUM_MODE` than last time. With an `https` base URL the cookie is named `__Host-podium_session` and marked `Secure`; with a local `http` base URL it is named `podium_session`.
- **Testing mode is local only.** With `PODIUM_MODE=testing`, a request whose `Host` is not `localhost`, `127.0.0.1` or `[::1]` (any port), or that carries `X-Forwarded-For`, `Forwarded`, `X-Real-IP` or `X-Forwarded-Host`, is refused with `403 testing_mode_local`: a proxy such as Caddy in front means the browser is not on this machine.
- **Cross-site requests.** `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` are refused with `403 cross_site` unless the request's `Origin` matches `PODIUM_BASE_URL` (or, without an `Origin` header, `Sec-Fetch-Site` is `same-origin` or absent).
- **Roles.** `student` < `admin` < `owner`. Routes marked "owner" return `401 signed_out` without a session and `403 forbidden` for lower roles. Roles are read from the database on every request, so removing an admin takes effect immediately.

- Some errors carry more detail. `fields` maps a form field to what is wrong with it; `problems` lists everything wrong with an upload or a request to validate, so it can be fixed in one pass.

```json
{ "error": { "code": "bad_request", "message": "Some fields need fixing.", "fields": { "slug": "Use lowercase letters, digits and single hyphens, like two-sum." } } }
```

Common error codes: `bad_request`, `rate_limited` (before sign-in, v1.3.0), `signed_out`, `forbidden`, `not_found`, `cross_site`, `conflict`, `internal`, and in testing mode `testing_mode_local`. Test sign-in adds `disabled`. The problem routes add `bad_pack`, `not_ready`, `too_large` and `busy`; the AI wizard adds `no_key`, `no_model`, `bad_key`, `provider_error` and `busy`; the workspace routes add `not_ready` and `rate_limited`; the contest routes add `not_on_roster`, `not_started`, `contest_over`, `board_closed` and `too_early`; the organiser contest routes add `locked_after_start`, `confirm_ungraded`, `results_hidden`, `not_participant` and `ai_unconfirmed`; the event stream adds `busy` and `unsupported`; the students routes (v1.4.0) add `proposal_needed`, `proposal_pending`, `two_person_rule`, `uninvolved_reviewer`, `rating_changed` and `not_final`; the judge API adds `unauthorized`, `protocol` (426), `lease`, `bad_report`, `bad_upload` and `too_large`.

## Health

### `GET /healthz`

| Status | Body |
|---|---|
| 200 | `{"status":"ok","db":"ok"}` |
| 503 | `{"status":"degraded","db":"unreachable"}` |

A request from the VM itself (a loopback peer with no `X-Forwarded-For`, `Forwarded`, `X-Real-IP` or `X-Forwarded-Host`) also gets the release and the judge queue: `{"status":"ok","db":"ok","version":"v1.1.0","schema":"0014_results_notice","jobs_queued":2,"oldest_queued_s":40,"judge_last_claim_s":1}` (`judge_last_claim_s` is `null` when no judge has asked for work since the server started; `podium-check` alerts on it, council C1-048). Through Caddy the answer never carries them (v1.1.0, B1.1).

### `GET /api/admin/metrics` (admin)

What an organiser watches on contest day; also read by the load test (`tools/loadtest`). Owners and admins only (`403 forbidden` for co-organisers and students). Not rate limited: it costs two indexed queries and in-memory counters.

```json
{ "server_now": 1792183192400, "uptime_s": 5400, "version": "v1.0.0",
  "queue": { "kinds": { "submit": { "queued": 40, "leased": 2, "finished_last_minute": 31, "finished_last_5_min": 150 },
                        "run": { "queued": 3, "leased": 0, "finished_last_minute": 12, "finished_last_5_min": 55 } },
             "queued": 43, "leased": 2, "queued_paused": 0, "oldest_queued_s": 81.5,
             "leased_jobs": [ { "id": 9120, "kind": "submit", "worker": "vm-1", "attempts": 1, "running_s": 2.4 } ],
             "submit_wait_p50_ms": 30500, "submit_wait_p95_ms": 78000, "submit_total_p50_ms": 33800, "submit_total_p95_ms": 84100,
             "submits_judged_last_10_min": 290, "failed_last_5_min": 0 },
  "events": { "connections": 712, "users": 698 },
  "db": { "max_open": 8, "open": 8, "in_use": 1, "idle": 7, "waits": 120, "wait_ms": 340 },
  "process": { "goroutines": 1480, "heap_mb": 41, "sys_mb": 78, "gc_cycles": 902 } }
```

- `queue.kinds` lists every job kind the server knows, with zeros when idle. `queued` includes jobs held back by a paused contest (`queued_paused`).
- `finished_*`, the `submit_*` percentiles (Submit to first claim, and Submit to verdict, over submissions closed in the last 10 minutes) and `failed_last_5_min` come from memory and restart at zero with the server.
- `db.waits` and `db.wait_ms` count, since the start, requests that waited for one of the SQLite connections.

## Sign-in

### `GET /api/auth/config` (public)

What the sign-in page, the footer and the privacy page need before anyone signs in (contract C5). No contest data, no counts, no version (owner, 2026-10-19).

```json
{ "allowed_domains": ["college.edu"],
  "contact_email": "podium@college.edu",
  "mode": "production",
  "organisation": { "name": "Example College of Engineering", "contact_email": "podium@college.edu",
                    "phone": "+91 80 1234 5678", "privacy_contact": "privacy@college.edu" },
  "retention": [
    { "id": "sessions", "data": "Sign-in sessions", "amount": 12, "unit": "hours", "after": "sign_in", "kept": "12 hours after sign-in" },
    { "id": "submissions", "data": "Submissions, their verdicts and what each contest showed", "amount": 4, "unit": "years",
      "after": "contest_end", "kept": "4 years after the contest" } ] }
```

| Field | Meaning |
|---|---|
| `allowed_domains` | every allowed sign-in domain, sorted |
| `contact_email` | the address to show as "Contact the organisers": `organisation.contact_email` when an owner has entered one, otherwise `PODIUM_OWNER_EMAIL` |
| `mode` | the server's `PODIUM_MODE`: `testing`, `dev` or `production`. In `testing` the web app shows the sign-in form and the testing-mode banner instead of "Continue with Google" |
| `organisation` | the site settings an owner enters on People (v1.3.0): `name`, `contact_email`, `phone`, `privacy_contact`. Each is a string, `""` until entered; a page shows nothing for an empty field and never a made-up value |
| `retention` | every retention period the server applies, in the order the privacy page lists them, from `server/internal/retention/periods.go` (v1.3.0). The privacy page renders its table from this list and types none of it |

A `retention` row is `{id, data, amount, unit, after, kept}`: `id` is stable (`sessions`, `integrity_events`, `case_evidence`, `run_payloads`, `contact_messages`, `draft_revisions`, `submissions`, `audit`, `backups`; P4 adds its own); `data` names what is kept; `amount` and `unit` (`hours`, `days` or `years`) are the period; `after` is what it counts from (`sign_in`, `created`, `contest_end`, `decision`); `kept` is the whole period as one phrase. The full table is in `docs/OPERATIONS.md`, "Retention".

The route is limited per address like the other routes before sign-in (see "Before sign-in: the per-address limit").

### `POST /api/auth/test-signin` (public, testing mode only)

Registered only when `PODIUM_MODE=testing`, which runs on loopback alone; in other modes it is `404 not_found`. Signs in as anyone, with any role, without Google. Besides the local-only rule above, the request must carry an `Origin` header equal to the base URL (the sign-in page sends one; a script elsewhere has no reason to).

Request: `{"email":"asha@college.edu","name":"Asha Rao","role":"admin","next":"/admin/problems"}`. `name` and `next` may be empty; `role` is `student`, `admin` or `owner`. The account is created if needed, otherwise its name and role are updated. `PODIUM_OWNER_EMAIL` always keeps the owner role, and the account's Google link is left untouched.

| Status | Body |
|---|---|
| 200 | `{"next":"/admin/problems"}`, with the session cookie set; `next` is made safe as for Google sign-in (anything that is not a path on this site becomes `/`) |
| 400 | `bad_request`: not an email address, or an unknown role |
| 403 | `cross_site`: no `Origin`, or not the base URL; `testing_mode_local`; `disabled`: an owner disabled the account |

Any session the browser already had ends, as with Google sign-in. Nothing made here survives a change of mode: when the server next starts in `dev` or `production`, every session ends and every admin or owner without a linked Google account (other than `PODIUM_OWNER_EMAIL`) becomes a student again, with a log line giving the counts.

### `GET /api/auth/google/start?next=/path` (public, browser navigation)

Not registered in testing mode, nor is the callback below.

Redirects (302) to Google's consent screen with PKCE, a nonce and a state value held in a short-lived signed cookie. `next` must be a path on this site (it starts with `/` and not `//`); anything else becomes `/`. When exactly one domain is allowed it is passed to Google as a hint. It shows the account chooser and asks Google to accept only a sign-in from the last 5 minutes (`prompt=select_account`, `max_age=300`; Google does not support `prompt=login`), so on a shared PC the previous student's Google sign-in is not one click away.

### `GET /api/auth/google/callback` (Google redirects here)

Verifies the state, exchanges the code, verifies the ID token (signature, issuer, audience and authorised party, expiry, nonce, and `auth_time` no older than 5 minutes when present) and requires `email_verified`. The email may sign in when its domain is on the allowed list, or when the account already has the admin or owner role, or when it is `PODIUM_OWNER_EMAIL`. Each account is bound to the Google account (`sub`) that first signed in with it; a different Google account with the same address is refused. On success it creates the user if needed, ends any session the browser already had, starts a new one and redirects to `next`.

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
| `rate_limited` | Too many sign-ins from this network at once (v1.3.0, see "Before sign-in"); wait a few seconds and try again. |

### Before sign-in: the per-address limit (v1.3.0)

The routes a visitor reaches without a session have two budgets per client address, each a token bucket:

| Routes | Burst | Refill |
|---|---|---|
| `GET /healthz`, `GET /api/auth/config`, `POST /api/auth/logout` | 5,000 | 50 a second |
| `GET /api/auth/google/start`, `GET /api/auth/google/callback`, `POST /api/auth/test-signin` | 2,000 | 20 a second |

Past a budget, the JSON routes answer `429 rate_limited` with `Retry-After` and `retry_after_s`; the two Google routes, being browser navigations, redirect to `/signin?error=rate_limited` with `Retry-After`. The budgets are large because a whole college signs in from few addresses behind its NAT (a class of 700 loads the config about three times each and makes one start and one callback; a test proves 700 sign-ins from one address in a minute pass); they exist so that a script cannot turn the server into a flood of outbound calls to Google or of queries. The address is the peer's, or behind Caddy on the same host the last `X-Forwarded-For` entry; IPv6 addresses share a budget per /64. Requests from the VM itself (loopback, no forwarding headers) are not counted, so the load test and `install.sh verify` are never limited.

Each sign-in attempt can be finished once: the callback remembers the state of every attempt it has seen until the attempt expires (10 minutes), and a second callback with the same state cookie gets `/signin?error=expired` without calling Google.

### `POST /api/auth/logout`

Ends the current session on the server (the session row is deleted, so a copy of the cookie is useless) and clears the cookie. `204` with no body, also when already signed out.

### `GET /api/me`

| Status | Body |
|---|---|
| 200 | `{"id":1,"email":"a@college.edu","name":"Asha Rao","avatar_url":"https://...","role":"student","theme":"","verdicts":"standard"}` |
| 401 | `{"error":{"code":"signed_out",...}}` |

`theme` is the display theme id, or `""` to follow the system. `verdicts` is `standard` or `signal` (colour-blind safe verdict colours).

**v1.2.0 (D112 banner):** the body also carries `site_updated_at`: the time (Unix ms) of the last emergency deploy (`install.sh upgrade --force`, recorded as `deploy_forced` in the site audit) when it was within the last 12 hours, else `null`. The web app shows "The site was updated at 14:02" to a signed-in student while it is set and newer than what that browser has already dismissed. The forced deploy restarts the server, so every live stream reconnects; the web app reads `/api/me` again after a reconnect to pick it up.

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

Everyone with the admin or owner role, the allowed domains, and the disabled accounts.

```json
{
  "people": [
    { "id": 1, "email": "owner@college.edu", "name": "Priya Raman", "role": "owner",
      "last_login_at": 1792183192359, "locked": true, "disabled": false }
  ],
  "domains": [ { "domain": "college.edu", "created_at": 1792183192359 } ],
  "disabled": [ { "id": 7, "email": "asha@college.edu", "name": "Asha Rao", "role": "student",
      "disabled": true, "last_login_at": 1792183100000 } ]
}
```

`last_login_at` is Unix milliseconds or `null` (added but never signed in). `locked` is true for `PODIUM_OWNER_EMAIL`, whose owner role comes from the server configuration and cannot be changed here. `disabled` lists every disabled account (`Account`), by email.

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

### `POST /api/admin/accounts/end-sessions`

Signs a person out everywhere, for a lost laptop or a shared PC left signed in: every session of the account is deleted. Their next request gets `401 signed_out`, and their open live streams close at the next clock tick (25 s at most). They can sign in again.

Request: `{"email":"asha@college.edu"}`. Response `200`: `{"account": Account, "ended": 2}`. Errors: `404 not_found`.

### `POST /api/admin/accounts/disable` and `POST /api/admin/accounts/enable`

Disable ends every session as above, and the account is refused at sign-in (`/signin?error=disabled`) and at every signed-in route until an owner enables it again. Enable lets them sign in again (it starts no session).

Request: `{"email":"asha@college.edu"}`. Response `200`: `{"account": Account, "ended": 1}` (`ended` is 0 for enable). Errors: `404 not_found`; `409 conflict` for `PODIUM_OWNER_EMAIL` (the recovery account) and for disabling yourself.

### `GET /api/admin/accounts?email=` (owner, admin; v1.4.0)

Finds one account by its email, students included, so People's Account help can say whose account an action is about before it runs. The address is matched as sign-in stores it (trimmed, lower case) and is never logged.

Response `200`: `{"account": Account, "server_now": 1792300000000}`. Errors: `400 bad_request` without `email`; `404 not_found` "Nobody with that email has an account."; `403 forbidden` for anyone below admin, the same answer whether or not the address exists, so the route tells nobody else which addresses have accounts; `429 rate_limited` on the per-user read limit.

`Account` is `{id, email, name, role, disabled, last_login_at}`.

Every change on this page (roles, domains, disabling and enabling, ended sessions, Google link resets) is written to the site audit in the same transaction as the change (v1.2.0, see "Site audit: People History").

### `POST /api/admin/domains`

Request: `{"domain":"college.edu"}` (lowercased; a leading `@` is ignored). Response `201`: `{"domain":"college.edu","created_at":...}`. `200` with the existing entry if it was already there. `400 bad_request` for an invalid domain.

### `DELETE /api/admin/domains/{domain}`

Stops sign-ins from the domain and ends the sessions of students on it; organisers on it keep access. `204`. `404 not_found` when the domain is not on the list.

### `PUT /api/admin/site-settings` (owner, v1.3.0)

The organisation details the public pages show (B3.7): the sign-in page, the footer, the privacy page and the Organization JSON-LD on `/signin`. Stored in the server's `settings` table, never in an environment variable. `GET /api/auth/config` returns them as `organisation`, and `GET /api/admin/people` carries the same object for the People form.

Request, every field present (send `""` to clear one):

```json
{ "name": "Example College of Engineering", "contact_email": "podium@college.edu",
  "phone": "+91 80 1234 5678", "privacy_contact": "privacy@college.edu" }
```

| Field | Rule |
|---|---|
| `name` | at most 120 characters, one line |
| `contact_email` | an email address, at most 254 characters; shown as a `mailto:` link |
| `phone` | at most 32 characters of digits, spaces, `+`, `-`, `(`, `)` and `.`, with at least 6 digits; shown as a `tel:` link |
| `privacy_contact` | who to ask about personal data: a name or an address, at most 200 characters, one line |

Leading and trailing spaces are trimmed. Response `200`: the saved object, in the request's shape. Errors: `400 bad_request` with `fields` naming each field that needs fixing. A change writes one `settings_changed` row to the site audit naming the fields that changed (the values are public, but the row carries only the names).

`GET /api/admin/people` gains `organisation` in the same shape (v1.3.0).

### `POST /api/admin/rollover` (owner, v1.3.0)

The semester rollover (B3.3): archives the term's contests and disables student accounts not seen for 180 days. Disabled, not deleted: an owner can enable an account again (`POST /api/admin/accounts/enable`). Without `apply` it is a preview that changes nothing.

Request: `{"label": "2026 odd semester", "ended_before": 1798761600000, "apply": false}`. `label` names the term (one line, 1 to 80 characters) and goes in the site audit; `ended_before` (Unix ms, optional, default now) picks the contests whose last moment, the end plus every extension, is before it and before now; `apply` defaults to false.

```json
{ "applied": false, "label": "2026 odd semester", "ended_before": 1798761600000, "idle_days": 180,
  "contests": [ { "id": 12, "slug": "week-12", "title": "Week 12", "ends_at": 1797000000000 } ],
  "accounts": [ { "id": 41, "email": "asha@college.edu", "name": "Asha Rao", "role": "student", "disabled": false,
                  "last_login_at": 1781000000000 } ],
  "server_now": 1798700000000 }
```

`accounts` are enabled student accounts whose last sign-in (or creation, when they never signed in) is more than `idle_days` ago; organisers and `PODIUM_OWNER_EMAIL` are never included. With `apply: true` the same lists come back as done: each contest is archived with an `archive` row in its audit log (`{"rollover": label}`), each account is disabled with its sessions ended, and one `semester_rollover` row goes to the site audit. Errors: `400 bad_request` with `fields.label`, `403 forbidden` for anyone but an owner.

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
  "validated": true, "validated_at": 1792183192359, "updated_at": 1792183100000,
  "shown_in_practice": false }
```

- `difficulty` is `easy`, `medium`, `hard` or `""`. `origin` is `manual`, `import` or `ai` (made by the AI wizard). A wizard draft carries the tag `ai draft` until a person changes its statement, reference or brute force.
- `validated` is true when the last passing validation covers the problem as it is now (its content hash equals the problem's). `validated_at` is `null` whenever `validated` is false.
- `shown_in_practice` is true when a contest that pins the problem has shown students its hidden tests (D115): it is ungraded and has started, or it has practice after the end and its end has passed. Its hidden tests may be known, so the web app marks it in the bank and the contest editor, and names it when a graded contest with it is published.
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
- **v1.2.0 open tests (D115):** `open` lists the open tests (test numbers, always an array): not scored, in no subtask, run by Run after the samples, and revealed when they fail. `tests[].open` is true for one; its `subtask_idx` is `null` like a sample's. A test is a sample, an open test or in exactly one subtask. Revision fields (`revision`, `revisions`, `revisions_total`, `live_contests`) are under "History and audit".
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
- **v1.2.0:** `ProblemEdit` may carry `open` (test numbers, like `samples`; left out keeps the stored list) and `reason` (at most 500 characters). `reason` is required, `400 bad_request` with `fields.reason`, when the save changes the problem while a live contest pins it (`live_contests` in `Problem` is not empty); it is recorded on the new revision. `PUT .../tests` takes it as `?reason=`.
- A save that changes the statement, the reference or the brute force removes the `ai draft` tag and withdraws "validated" (the person's version must pass again); a save that changes none of them keeps the tag, even when `tags` leaves it out. A save that changes the reference or the brute force also marks the problem as having a person's solution, which graded contests need for problems whose origin is `ai`. The generator and the plan are not part of the content hash, so changing them does not withdraw "validated" (D53).

`Validation`:

```json
{ "id": 12, "state": "failed", "content_hash": "9f2c...", "current": true,
  "queued_at": 1792183192359, "finished_at": 1792183230000, "queue_position": null,
  "compile": [ { "role": "reference", "verdict": "OK", "output": "" } ],
  "tests": [ { "idx": 4, "role": "reference", "verdict": "WA", "time_ms": 3, "memory_kb": 1024,
               "message": "Token 1 differs: expected \"7\", found \"8\"." } ],
  "summary": { "passed": false, "reference_passed": 5, "reference_total": 6,
               "brute_agreed": 6, "brute_total": 6, "brute_unchecked": 0, "brute_unchecked_tests": [],
               "validator_passed": 6, "validator_total": 6,
               "max_time_ms": 3, "time_limit_ms": 1000,
               "problems": ["Test 4: reference solution got wrong answer. Token 1 differs: expected \"7\", found \"8\"."],
               "warnings": [] },
  "error": null }
```

- `state`: `queued` (waiting for a worker; `queue_position` counts from 1), `running` (a worker holds it), `passed`, `failed`, or `error` (the run could not finish; `error` says why and `summary` is `null`).
- `current` is false when the problem has changed since this validation was requested.
- `role` is `reference`, `brute`, `validator` or `checker` (the checker appears only in `compile`). Compile verdicts are `OK` or `CE`; test verdicts are `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `IE` or `SK` (a brute force that ran out of time). `message` is a checker comment or the end of a validator's error output, at most 256 bytes.
- `summary` is computed by the server from the recorded results once the job closes. `time_limit_ms` there is the problem's limit times the reference language's time factor. `passed` is true when `problems` is empty.
- `brute_unchecked` counts the tests the brute force did not finish (`TLE` or `SK`), so nothing cross-checked their expected output, and `brute_unchecked_tests` names them; a warning says so. When a problem has a brute force, fewer than half of its tests cross-checked (brute force `AC` or `WA`) is a problem, so validation fails.

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

Every route needs the admin or owner role and answers with `server_now`. Provider keys are pasted per session and held in the server's memory only, for 2 hours, tied to the session and the provider: never stored, logged, sent to the judge or shown back (decisions D50 and D111, `docs/ARCHITECTURE.md`). A session can hold one key for each provider at once. Signing out, signing in again in the same browser, or a restart forgets them all. Request bodies of the key route are never logged.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/ai/providers` | | 200 `Providers` (C11) |
| `GET /api/admin/ai/models?provider=openai` | `&refresh=1` lists again instead of using the session's copy | 200 `Models` (C11) |
| `GET /api/admin/ai/key` | | 200 `KeyStatus` |
| `PUT /api/admin/ai/key` | `{"provider":"openai","key":"...","model":"gpt-..."}` | 200 `KeyStatus` (C11) |
| `DELETE /api/admin/ai/key` | `?provider=openai` forgets that provider's key; without it, every key of the session | 204 |
| `POST /api/admin/ai/wizard` | `WizardRequest` (at most 256 KB) | 202 `{"wizard_id":"3f9c...","server_now":...}` |
| `GET /api/admin/ai/wizard/{id}` | | 200 `Wizard` |
| `POST /api/admin/problems/{id}/solve` | no body | 202 `{"wizard_id":"...","server_now":...}` |

### Providers and models (contract C11)

`Providers`, the three providers and what this session holds for each:

```json
{ "providers": [
    { "id": "anthropic", "name": "Anthropic", "has_key": true, "model": "claude-opus-5-5", "active_until": 1792190392359 },
    { "id": "openai", "name": "OpenAI", "has_key": true, "model": "", "active_until": 1792190500000 },
    { "id": "gemini", "name": "Google Gemini", "has_key": false, "model": "", "active_until": null } ],
  "active_provider": "anthropic",
  "server_now": 1792183192359 }
```

- The order is fixed: `anthropic`, `openai`, `gemini`. In testing mode a fourth entry follows, `{"id":"fake","name":"Test provider (canned problems)",...}`: it needs no key and answers with one of two canned problems (a maximum subarray problem when the brief or text mentions "subarray" or "stretch", a pair sums problem otherwise).
- `has_key`: this session holds an unexpired key for the provider. `model` is the model id saved with it, `""` when none was chosen yet. `active_until` is `null` without a key.
- `active_provider` is the provider the wizard and "Can a model solve this?" use: the one whose key or model was saved last. `null` when the session holds no key.

`Models`, the provider's own list, filtered to models that can write a problem:

```json
{ "provider": "gemini",
  "models": [ { "id": "gemini-x-pro", "name": "Gemini X Pro" }, { "id": "gemini-x-flash", "name": "Gemini X Flash" } ],
  "listed_at": 1792183100000, "server_now": 1792183192359 }
```

- The server asks the provider's models endpoint with the session's key and keeps the filtered list for the session until the key expires or `refresh=1` is sent. No model id is written in the server; the filter rules are:

| Provider | Endpoint | Kept | Order | `name` |
|---|---|---|---|---|
| `anthropic` | `GET /v1/models`, every page | models whose `capabilities` support structured outputs, adaptive thinking and effort `high` | as listed (newest first) | `display_name` |
| `openai` | `GET /v1/models` | ids starting `gpt-` or `o` followed by a digit, except `gpt-3.5*`, `gpt-4`, `gpt-4-*`, and ids containing `audio`, `realtime`, `tts`, `transcribe`, `image`, `search`, `embedding`, `moderation` or `instruct` | newest `created` first | the id |
| `gemini` | `GET /v1beta/models`, following `nextPageToken` | `supportedGenerationMethods` contains `generateContent`, except ids containing `embedding`, `image`, `tts`, `audio`, `live`, `gemma` or `aqa`; the `models/` prefix is stripped | as listed | `displayName` |
| `fake` (testing) | none | `canned` | | `Canned problems` |

- The list is a hint. The key form takes any model id typed in; a model missing from the list (new, or filtered out) is still accepted when the provider's own model lookup finds it.

| Status and code | When |
|---|---|
| 400 `bad_request` | `provider` missing or not one of the providers above |
| 409 `no_key` | the session holds no key for that provider: "Add an OpenAI key first." |
| 502 `provider_error` | the provider refused the listing; `message` is the provider's sentence (table under the key form) |

### The key form

`PUT /api/admin/ai/key` with `{"provider","key","model"}`:

- `provider` is required: `anthropic`, `openai`, `gemini` (and `fake` in testing mode, which ignores `key` and `model`).
- `key` is required unless the session already holds a key for this provider; leaving it out (or `""`) then keeps the held key and changes only the model, without extending its 2 hours.
- `model` is free text, at most 128 characters of letters, digits and `. _ - :`; a leading `models/` is dropped. Empty means "no model yet": the key is checked by listing the provider's models (which fills `Models` for the session) and kept without a model, so the form can offer the list next. With a model, the key and the model are checked together by the provider's model lookup (`GET /v1/models/{id}` at Anthropic and OpenAI, `GET /v1beta/models/{id}` at Gemini). Neither check costs tokens. For Anthropic the model must also support structured outputs, adaptive thinking and effort `high`.
- A successful save makes this provider the `active_provider`.

`KeyStatus` (`GET` and `PUT /api/admin/ai/key`) describes the active provider and repeats the providers with the session's model lists:

```json
{ "active": true, "provider": "openai", "model": "gpt-x", "active_until": 1792190392359,
  "providers": [
    { "id": "anthropic", "name": "Anthropic", "has_key": false, "model": "", "active_until": null, "models": [] },
    { "id": "openai", "name": "OpenAI", "has_key": true, "model": "gpt-x", "active_until": 1792190392359,
      "models": [ { "id": "gpt-x", "name": "gpt-x" } ] },
    { "id": "gemini", "name": "Google Gemini", "has_key": false, "model": "", "active_until": null, "models": [] } ],
  "server_now": 1792183192359 }
```

- Without any key: `active` false, `provider` `"anthropic"`, `model` `""`, `active_until` `null`.
- `providers[].models` is the session's copy of `Models` for that provider, `[]` until it has been listed.

| Status and code | When |
|---|---|
| 400 `bad_request` | an unknown provider, a model id with other characters or over 128, or no key when the session holds none for this provider |
| 400 `bad_key` | the check failed; `message` is one of the sentences below, never the key or the provider's raw answer |

What each provider's failures say (also the `message` of a wizard run's `error`, and of `502 provider_error`). X is the model id:

| `error.code` in a run | Anthropic | OpenAI | Google Gemini |
|---|---|---|---|
| `key_refused` | "Anthropic refused the key. Check it and paste it again." | "OpenAI refused the key. Check it and paste it again." | "Google refused the key. Check it in Google AI Studio and paste it again." |
| `no_quota` | "Anthropic says this key's account has no credit left. Add credit in the Claude Console, or use another key." | "OpenAI says this key's account has used up its quota. Check the plan and billing on the OpenAI platform, or use another key." | "Google says this key has used up its Gemini quota. Check the plan in Google AI Studio, or try again later." |
| `model_not_found` | "Anthropic does not offer the model X to this key. Choose another model." | "OpenAI does not offer the model X to this key. Choose another model." | "Gemini does not offer the model X to this key. Choose another model." |
| `no_access` | "Anthropic says this key cannot use X. Choose another model or key." | "OpenAI says this key cannot use X. Choose another model or key." | "Google says this key cannot use X. Choose another model or key." |
| `unsupported_model` | "X cannot write problems here: it lacks structured outputs, adaptive thinking or effort. Choose another model." | | |
| `rate_limited` | "Rate limited by the provider; try again in a minute." | same | same |
| `unavailable` | "The provider is unavailable. Try again in a few minutes." | same | same |
| `bad_request` | "Anthropic could not accept the request. Try a shorter text or another model." | "OpenAI could not accept the request. Try a shorter text or another model." | "Gemini could not accept the request. Try a shorter text or another model." |

### Wizard runs

`WizardRequest`, from a brief or from pasted text:

```json
{ "mode": "brief", "brief": { "topic": "Maximum subarray sum", "difficulty": "medium",
  "constraints": "n up to 2e5, values up to 1e9 in absolute value", "subtasks": "n up to 1000, then full" } }
```

```json
{ "mode": "paste", "text": "The problem statement, copied from elsewhere..." }
```

`difficulty` is `easy`, `medium` or `hard`; `topic` is required; each brief field is at most 2,000 characters and `text` at most 20,000. An optional `"provider": "gemini"` picks which of the session's keys to use; without it the `active_provider` is used.

| Status and code | When |
|---|---|
| 400 `bad_request` | an unknown mode or difficulty, an empty topic or text, or a field over its limit |
| 409 `no_key` | "Add a key in AI assistant first." (no key for the provider asked for, or none at all) |
| 409 `no_model` | "Choose a model for OpenAI in AI assistant first." (the key was saved without a model) |
| 429 `busy` | "One AI request at a time. Wait for the current one to finish." (a generation or a solve of this admin is still running) |

The call runs in the background, at most 3 minutes with its one retry. When the answer passes the checks it becomes a draft problem (origin `ai`, tag `ai draft`; a taken slug gets `-2`, `-3` and so on), and input generation is queued for it at once, which chains output generation and validation.

`Wizard` (the admin's own runs only; kept in memory for an hour after it ends):

```json
{ "id": "3f9c...", "kind": "generate", "state": "draft_created", "problem_id": 12,
  "provider": "openai", "model": "gpt-x", "started_at": 1792183192359, "server_now": 1792183230000 }
```

```json
{ "id": "c3d4...", "kind": "generate", "state": "error", "started_at": 1792183192359, "server_now": 1792183230000,
  "error": { "code": "invalid_answer", "message": "The model's answer could not be used.",
             "reasons": ["Subtask points add up to 90; make them add up to 100."] } }
```

- `state`: `thinking`, then `draft_created` (generate), `submitted` (solve) or `error`. `problem_id` is the draft (generate) or the problem solved (solve); a solve adds `submission_id`. `provider` and `model` name what made the draft or the solution (C11).
- `error.code`: `key_refused`, `no_quota`, `no_access`, `model_not_found`, `unsupported_model`, `rate_limited`, `unavailable`, `bad_request`, `refused` (the model declined; the message names the category), `cut_off` (too long, after one shorter retry), `invalid_answer` (with `reasons`, after one retry with them), `timeout`, `canceled`, `input`, `not_ready` (solve: the problem cannot be judged), `not_found`, `internal`. `reasons` is always an array.
- `404 not_found` for another admin's run, an unknown id, or a run that ended over an hour ago or before a restart.
- The same object is sent as a `wizard` event to the admin who started it when the run ends.

`POST /api/admin/problems/{id}/solve` ("Can a model solve this?") sends the statement alone and asks for a C++17 solution, which is judged on the problem's tests as the admin's own submission (`client_key` `ai-solve-{wizard_id}`, listed with the admin's other submissions). The editor reads the score from `GET /api/submissions/{submission_id}`. It uses the `active_provider`. Refusals: `404 not_found`, `409 no_key`, `409 no_model`, `409 not_ready` with `problems` before any call when the problem cannot be judged (its structure, or missing outputs), `429 busy`. The one-pending-submission rule still applies: the run then ends in `error` with `rate_limited`.

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
- **v1.2.0:** `open_test_count` is how many open tests a Run checks after the samples (D115); their inputs are never listed here.

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

A run older than 30 days keeps its row and verdict but no longer its inputs or output (v1.3.0, the `run_payloads` retention period): `results` is `[]` and `compile_output` is `""`.

Times are rounded to 10 ms (100 ms for a student in a graded contest). A run uses the problem's limits and checker. Running the samples on a problem without samples is `400 bad_request` ("Run it on your own input instead").

**v1.2.0, open tests and the revealed test (D115, contract row "Run").** A Run without `input` runs every sample, then the problem's open tests in order; the open tests run only while everything before them passed, and stop at the first failure. Every Run reveals its first failing test, in practice and during a contest alike.

```json
{ "id": 78, "state": "done", "compile_output": "",
  "results": [ { "kind": "sample", "label": "Sample 1", "verdict": "AC", "time_ms": 10, "memory_kb": 3120,
                 "output": "3\n", "output_truncated": false, "expected": "3\n", "message": "" },
               { "kind": "open", "label": "Open test 1", "verdict": "WA", "time_ms": 10, "memory_kb": 3100,
                 "output": "7\n", "output_truncated": false, "expected": "8\n", "message": "" },
               { "kind": "open", "label": "Open test 2", "verdict": "SK", "time_ms": 0, "memory_kb": 0,
                 "output": "", "output_truncated": false, "expected": null, "message": "" } ],
  "revealed": { "idx": 3, "kind": "open", "label": "Open test 1", "input": "4 4\n", "expected": "8\n", "output": "7\n", "truncated": false },
  "server_now": 1792183192400 }
```

| Field | Meaning |
|---|---|
| `results[].kind` | `sample`, `open` or `custom` |
| open test rows | labelled "Open test 1", "Open test 2", ... in order; `SK` when not run because an earlier sample or open test failed; `output` is `""` and `expected` `null` except on the failing one |
| `revealed` | present only when a sample or open test failed: the first one, with its test number `idx`, `kind`, `label`, its `input`, the `expected` output and the program's `output`, each at most 4 KB cut at a whole UTF-8 character (`truncated` says one was cut). Absent for a custom input and when every test passed |

### `Submission`: the revealed hidden test (v1.2.0, D115)

A submission that never counts carries `revealed` for its first failing hidden test, in judging order:

```json
{ "revealed": { "idx": 5, "subtask_idx": 2, "input": "100000\n...", "expected": "4999950000\n", "output": "-1\n", "truncated": true } }
```

- Present only when all of these hold: the submission is practice (after the student's window, with `practice_after_end`), or was made in an ungraded contest, or in the organisers' `/api/workspace/`; a hidden test failed (`WA`, `TLE`, `MLE`, `RE` or `OLE`); judging is done; and the viewer is the author or an owner or admin. A co-organiser whose site role is student never sees it on someone else's submission.
- Never present on a submission that counts in a contest, in either feedback mode, and never while exam mode hides the result. Never in the console feed, the exports, the standings or any event other than the author's own `submission` event.
- `output` is the start of what the program printed on that test (at most 4 KB). Each field is capped at 4 KB like the samples.
- The setter checklist in `docs/AUTHORING.md` explains the consequence: a problem used in a practice-open or ungraded contest has its hidden tests readable one at a time.

The web app shows `revealed` from v2 P5 (D115, F7.13): a practice submission's first failing hidden test, and a run's first failing sample or open test, in one block that says why it is shown, with the input, the expected output, the program's output, the first difference and "Run on this input" (not offered when a text was cut at 4 KB). Passed samples and open tests show their verdict line only, and a skipped open test says why it did not run. A submission that counts in a contest never carries `revealed`, so it never shows one.

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

A submission that counts never carries the input, output or checker comment of any test (decision D47); a practice submission may carry `revealed` (D115, below). Times are rounded to 10 ms. How scoring works: `docs/ARCHITECTURE.md`, "Submissions and scoring".

**What a student sees of their own submission.** The same view on every path (`GET /api/submissions/{id}`, the workspace list, the answer to a submit or a repeated `client_key`, and live events). Owners and admins see every figure. A contest's co-organisers whose site role is student see the student view, both of their own submissions and of the contest's submissions they read: they are not trusted with what exact figures reveal about hidden tests. For them "the student's window" is the last student's: in exam mode nothing shows until every window is over, and a submission made from the freeze is hidden while students see the frozen board.

- Coarse figures: no memory figure for hidden tests (`memory_kb` 0), `max_memory_kb` rounded up to the next 16 MB, times rounded to 100 ms in graded contests and 10 ms otherwise.
- While judging: per-test verdicts only inside finished blocks (the samples as a whole, each whole subtask), and `max_time_ms` and `max_memory_kb` stay 0 until the end, so polling shows no more than the live stream.
- Exam feedback mode, until the student's window closes: `"hidden": true`, `"state": "submitted"`, an empty `verdict`, `score` 0, no `samples`, `subtasks`, times or compile output, and no live `submission` events. Nothing a student can observe changes when judging finishes. The one exception is a judge failure: `"state": "error"` with its `error` sentence, so the student knows it did not count and can submit again (it is never something the program chose). After the window closes the full student view returns.

`hidden` is `false` in every other case.

### Submit rules

| Rule | Value | Refusal |
|---|---|---|
| Pending submissions | one per user per problem; not applied in exam mode, where a new submission queues behind the pending one (a refusal that ends when judging ends would tell the student when it finished) | `429 rate_limited`: "Your last submission for this problem is still being judged. Wait for its result." |
| Gap between submits | 5 s per user, across all problems (organisers trying problems; contests set their own in M4) | `429 rate_limited`: "Submissions are limited to one every 5 seconds. Try again in 3 s." |
| Compile that ran out of time | after the user's last program (a Run or a Submit in the same contest scope) failed to compile because the compiler ran out of time or memory, 60 s before the next Run or Submit; compiles see no tests, so the same message is shown in exam mode | `429 rate_limited`: "Your last program took too long to compile. Try again in N seconds; a program that compiles for 10 seconds is usually a template or include problem." |
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
| `statement` | `{"contest","contest_id","label","revision_id","statement_changed","pinned_at"}` | v1.2.0: an organiser moved a problem of a published contest to another revision ("Update contest to revision N"); refetch the contest, and when `statement_changed` show "Statement updated 14:32. See what changed" (C3) |
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
- In a live contest, opening a stream from another browser or machine while one is open is recorded for the organisers as a "second session" (decision D47). A phone or tablet browser that changes network is the same device (docs/ARCHITECTURE.md, "Integrity (D47)"). Nothing is shown to the student.
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
| `GET /api/contests/{slug}/problems/{label}/revisions` | `?from=&to=` revision ids, optional | 200 `StatementHistory` (v1.2.0, C3; see "History and audit") |
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
- **v1.2.0 (C3):** each of `problems` also carries `revision_id` (the pinned revision students are served) and `statement_updated_at`: the time (Unix ms) of the newest pin change made after the contest started that changed the statement or title, else `null`. With it the web app shows "Statement updated 14:32. See what changed" on page load as well as on the `statement` event.

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
- **v1.2.0 (B2.3):** the title, statement, limits and samples are those of the revision the contest pins, never the problem bank's working copy, so an organiser's edit reaches students only through "Update contest to revision N". The workspace also carries `revision_id` and `statement_updated_at` (as on `Contest.problems`). The first time a student is served a revision, the server records it (the served log).

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

**Co-organisers whose site role is student** (security council S1, 2026-10-18). Below, "admin" means the owner or an admin; a co-organiser who is a student:

- cannot replace the roster, set extensions, extend, pause, resume or rejudge (`403 forbidden`);
- before the start may edit every setting except publishing; once the contest has started, a change to any of `locked_after_start` (`slug`, `starts_at`, `ends_at`, `start_mode`, `duration_minutes`, `freeze_minutes`, `scoring_mode`, `access`, `feedback_mode`, `graded`, `practice_after_end`, `stop_on_sample_fail`, `min_gap_s`, `max_per_problem`, `languages`, `hold_reveal`) is `403 locked_after_start`, with the fields in `problems`. Admins may still change them (the web app asks first);
- sees results only as a student whose window closes last would: in exam mode, until every window is over, feed rows are `"hidden": true`, `"state": "submitted"` with no verdict or score, a `verdict` filter is `403 results_hidden`, and the boards are `403 board_closed`; while students see the frozen board, the boards are frozen (whatever `variant` asks) and feed rows made from the freeze are hidden. The stats and both exports are `403 results_hidden` until students see the full board. Integrity counts are never shown to them.

Turning `graded` from true to false after the start, by anyone, also needs `"confirm_ungraded": true` in the same request: without grading every student still in their window sees the first hidden test each later submission fails (D115). Without it the answer is `400 confirm_ungraded` with `fields.graded`; the field is never stored.

Every change that can affect results is recorded in the contest's audit log (below), and an extension must name a student on the roster or one who started or submitted (`409 not_participant` otherwise; removing one with 0 minutes always works).

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/contests` (`?archived=1`) | | 200 `{contests:[AdminContest], server_now}` (a co-organiser sees their own) |
| `POST /api/admin/contests` | `ContestEdit` | 201 `AdminContest` (unpublished) |
| `GET /api/admin/contests/{id}` | | 200 `AdminContest` |
| `PUT /api/admin/contests/{id}` | `ContestEdit` (fields left out keep their value) | 200 `AdminContest` |
| `DELETE /api/admin/contests/{id}` | | 204 (archived; nothing deleted) |
| `POST /api/admin/contests/{id}/clone` | `{slug, title, starts_at, ends_at}` | 201 `AdminContest`: settings and problems copied, not the roster, co-organisers, extensions or editorial |
| `PUT /api/admin/contests/{id}/problems` (admin) | `[{"problem_id":3,"label":"A","ai_reason":"..."}]` (`ai_reason` optional) | 200 `AdminContest`; each problem must be validated and not tagged `ai draft` (a person edits AI drafts first); its content hash and subtask points are pinned. In a graded contest a problem whose origin is `ai` and for which no person has written a solution (no editor save has changed its reference or brute force) is `409 ai_unconfirmed` with the labels in `problems`, unless `ai_reason` (at least 10 characters) gives the admin's reason to grade it as written; the reason goes to the audit log, and once recorded for this contest it is not asked again. Errors name the label, never the problem's title |
| `GET /api/admin/contests/{id}/roster` | | 200 `{roster:[{email,name,roll_number,section}], count}` |
| `PUT /api/admin/contests/{id}/roster` (admin) | `text/csv` (header row naming `email`, and optionally `name`, `roll_number`, `section`) or a JSON array of the same objects; at most 2 MB and 5,000 students | 200 `{added, removed, roster_count}`. A full replace: on a roster contest, anyone left out loses access at once, so show the counts before and after |
| `POST`, `DELETE /api/admin/contests/{id}/admins` | `{"email":"ta@college.edu"}` (they must have signed in once) | 200 `AdminContest` |
| `PUT /api/admin/contests/{id}/extensions/{user}` (admin) | `{"minutes":15,"reason":"..."}` (0 to 1440; 0 removes) | 200 `AdminContest`; the student gets a `contest` event. `409 not_participant` unless the student is on the roster or started or submitted |
| `POST /api/admin/contests/{id}/extend` (admin) | `{"minutes":10}` (1 to 240, adds to earlier extensions) | 200 `AdminContest`; members get a `contest` event |
| `POST /api/admin/contests/{id}/pause`, `/resume` (admin) | | 200 `AdminContest` |
| `GET /api/admin/contests/{id}/submissions?problem=A&user=7&verdict=WA&page=1` | | 200 `{submissions:[...], page, has_more, results_hidden, server_now}`; `results_hidden` is true when some rows are hidden from this viewer. `verdict` is a verdict code (`AC`, `WA`, `TLE`, `MLE`, `RE`, `CE`), `pending` (state `queued` or `running`) or `error` (state `error`, a judge error); case does not matter |
| `PUT /api/admin/contests/{id}/problems/{label}/revision` (admin) | `{"revision_id":14}` | 200 `{contest: AdminContest, announcement_md, statement_changed, rejudge_needed, server_now}` (v1.2.0, C4: "Update contest to revision N") |
| `POST /api/admin/contests/{id}/rejudge` (admin) | `{"problem_label":"A"}` or `{"submission_id":80}` | 202 `{queued, revisions:[{label, revision_id, rev}], server_now}`; `revisions` (v1.2.0) names the pinned revision each new job judges against and records |
| `GET /api/admin/contests/{id}/audit?page=1&action=&actor=` | | 200 `{entries:[AuditEntry], page, has_more, server_now}`, newest first, 200 per page; every organiser of the contest may read it. This is the console's "Changes" feed (v1.2.0): `action` keeps one action, `actor` keeps rows whose actor email contains the text (case does not matter) |
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

`AdminContest` is every `ContestEdit` field plus `id`, `close_at` (end plus contest-wide extension), `default_honour_text`, `editorial_md`, `editorial_published`, `paused`, `extended_minutes`, `revealed_at`, `archived`, `state`, `problems [{label, problem_id, title, pinned_hash, ready, max_score, validated_toolchain, toolchain_drift}]` (`ready` is false when the problem changed since it was added; from v1.3.0 `pinned_hash` is the pinned revision's content hash, the database column having gone), `admins [{user_id, email, name}]`, `extensions [{user_id, email, name, minutes, reason}]`, `roster_count`, `judge_toolchain`, `created_at`, `updated_at`, `server_now`, `locked_after_start` (the field names above that only admins may change once the contest has started).

**Toolchain drift (v1.1.0, B1.5; shown by the console strip C1 in P3).** `judge_toolchain` is what the judge reported last, `null` before any v1.1.0 judge has started; `validated_toolchain` is the toolchain of the newest finished validation of the pinned version, `null` when that validation ran before v1.1.0. `toolchain_drift` is `true` when the two differ, `false` when they match, and `null` when either is unknown. A toolchain is `{gcc, java, python, node, isolate, kernel, first_seen}` (strings, and `first_seen` in ms). Drift is a warning, not a block: the problem stays validated (D45); revalidating it on the current toolchain clears the flag.

```json
{ "problems": [ { "label": "A", "problem_id": 3, "title": "A plus B", "pinned_hash": "9f2c...", "ready": true, "max_score": 100,
                  "validated_toolchain": { "gcc": "g++ 13.3.0", "java": "javac 21.0.8", "python": "Python 3.12.3", "node": "v22.20.0",
                                           "isolate": "The process isolator 2.7", "kernel": "6.8.0-85-generic", "first_seen": 1792100000000 },
                  "toolchain_drift": true } ],
  "judge_toolchain": { "gcc": "g++ 13.3.1", "java": "javac 21.0.8", "python": "Python 3.12.3", "node": "v22.20.0",
                       "isolate": "The process isolator 2.7", "kernel": "6.8.0-86-generic", "first_seen": 1792180000000 } }
```

Turning `graded` on is `409 ai_unconfirmed` while the contest holds an AI problem no person has written a solution for and no admin has confirmed for this contest.

**Queue health (v1.3.0, contract C10, D103).** Every `AdminContest` also carries what the console's attention strip shows:

```json
{ "queue": { "waiting": 14, "judging": 2, "errors": 1, "oldest_wait_s": 37.5 },
  "judge_last_claim_at": 1792311190000,
  "unanswered": 3 }
```

| Field | Meaning |
|---|---|
| `queue.waiting` | this contest's jobs (Submit and Run) waiting for the judge, including those held by a pause |
| `queue.judging` | this contest's jobs a judge is running now |
| `queue.errors` | this contest's submissions (Submit) that ended in `error` (a judge error, not a verdict) and have not been judged again since |
| `queue.oldest_wait_s` | seconds the oldest waiting job has waited; `0` when nothing waits |
| `judge_last_claim_at` | when any judge last asked the server for work (Unix ms), whether or not it got any; `null` when no judge has asked since the server started. A judge asks every few seconds while it runs, so a value more than a minute old means no judge is working |
| `unanswered` | this contest's clarifications with no answer yet |

The counts come from one query on the jobs, submissions and clarifications indexes per response; `judge_last_claim_at` is held in memory.

### `AuditEntry`

`{id, action, actor: {user_id, email, role}, details, created_at}`. The actor's email and site role are as they were at the time. The table is append only (database triggers refuse every update and delete), and the JSON export carries the whole log as `audit`, oldest first.

| `action` | `details` |
|---|---|
| `settings` | `{"changes": {"feedback_mode": {"from": "subtask", "to": "exam"}, ...}}`, times in Unix ms; the description and honour code are recorded as their sizes |
| `problems` | `{"from": [{label, problem_id, pinned_hash, revision_id, rev}], "to": [...]}` (`revision_id` and `rev` from v1.2.0) |
| `ai_problem` | `{label, problem_id, reason}`: an admin's reason to grade an AI problem as written |
| `roster` | `{count, added, removed, emails_added, emails_removed}`; the two email lists (sorted, from v1.2.0) name who gained and lost access |
| `extension` | `{user_id, email, from_minutes, minutes, reason}` |
| `extend` | `{minutes, extended_minutes}` |
| `pause`, `resume`, `reveal`, `archive` | `{}` |
| `rejudge` | `{problem_label}` or `{submission_id}`, with `queued` |
| `editorial` | `{published, was_published, changed, bytes}`, recorded when it is published or unpublished, or changed while published |
| `organisers` | `{email, added}` |
| `revision` | v1.2.0, "Update contest to revision N": `{label, problem_id, from_revision_id, from_rev, to_revision_id, to_rev, statement_changed, judging_changed}` |
| `deploy_forced` | an emergency deploy inside this contest's guard window (D112): `{reason, release, refused}`; the actor is the owner account. Also in the site audit from v1.2.0 |

Unknown actions may appear in later releases; show them by their name.

### Running a contest

- **Pause** stops the judge from taking the contest's submissions and runs; they still queue, and nothing is lost. **Resume** lets them through in order.
- **Rejudge** queues every submission of a problem (or one submission) again at priority 50 with the problem's current tests; each goes back to `queued` and counts again once judged. Submissions still waiting for the judge, or being judged, on the old tests are included: their old job is cancelled and replaced. A submission already waiting on exactly the current tests is left alone, so pressing Rejudge twice adds nothing (`queued` counts only what was queued). The problem must still match its pin: after editing tests, validate the problem and add it to the contest again first (`409 not_ready` otherwise). A rejudge waiting in the queue never blocks the student from submitting.
- **Feed** rows: `{id, user_id, name, email, problem_label, language, state, verdict, score, max_score, created_at, judged_at, practice, rejudged, typed_chars, pasted_chars, focus_losses}`, newest first, 50 per page.
- **Reveal** answers `409 too_early` "The reveal opens when every student's time is up." until the close plus the longest personal extension. The web app walks the frozen board from the bottom, moving each row to its place on the full board.

### Export and stats

- CSV columns: `rank, name, email, roll_number, section, started` (UTC, RFC 3339), `total`, then one column per problem label. A cell starting with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading `'`, so a spreadsheet never runs a name as a formula.
- JSON: `{contest: AdminContest, standings: [Standings row plus email, roll_number, section, started_at], submissions: [{id, user_id, problem_label, language, state, verdict, score, created_at, practice}], audit: [AuditEntry], exported_at}`. No code and no integrity data.
- `ProblemStats`: `{label, title, max_score, attempted, solved` (full score)`, subtasks [{idx, points, earned}], histogram [{score, students}]` (highest score first)`, median_attempts}`, over students on the board and their non-practice submissions.

## History and audit (v1.2.0)

Contracts C3 and C4 of the v2 plan, and the D115 fields above. Problems keep immutable revisions; contests pin a revision; the server records the first time each student is served a revision; site and contest changes go to append-only audit tables. Background: `docs/ARCHITECTURE.md`, "Problem revisions and pins" and "Audit logs".

No history route returns a hidden test's input or output, or any test file or hash, to anyone (D47). Students see statements only.

### `Revision`

A problem's revisions, as `Problem` lists them for owners and admins (`GET /api/admin/problems/{id}`):

```json
{ "revision": { "id": 14, "rev": 3 },
  "revisions_total": 3,
  "revisions": [ { "id": 14, "rev": 3, "created_at": 1792311100000,
                   "created_by": { "user_id": 2, "email": "ta@college.edu", "name": "Ravi K" },
                   "reason": "Fix the bound on n in the statement.", "content_hash": "9f2c...",
                   "statement_changed": true, "judging_changed": false, "reconstructed": false, "validated": true,
                   "pinned_in": [ { "contest_id": 4, "slug": "week-6", "label": "A" } ] } ],
  "live_contests": [ { "contest_id": 4, "slug": "week-6", "title": "Week 6", "label": "A" } ] }
```

| Field | Meaning |
|---|---|
| `revision` | the revision the working copy is now; every save that changes anything makes a new one, a save that changes nothing makes none |
| `revisions` | newest first, at most 50; `revisions_total` counts them all |
| `created_by` | `null` for revisions the server made (revision 1 at the upgrade, generated outputs, generated inputs) |
| `reason` | the setter's reason, required while `live_contests` is not empty; `""` otherwise |
| `statement_changed` | the title or statement differs from the revision before |
| `judging_changed` | anything validation depends on differs (tests, limits, checker, solutions, samples, open tests, subtasks): its `content_hash` differs from the revision before |
| `reconstructed` | made by the upgrade to v1.2.0 for a contest pin older than the working copy: its tests and limits come from the contest's judging jobs, its statement was not recorded |
| `validated` | the problem's last passing validation covers this revision's content |
| `pinned_in` | the contests that pin this revision now |
| `live_contests` | contests that pin the problem (any revision) and are running now (from the start to the close plus the longest personal extension) |

`GET /api/admin/problems/{id}/revisions/{rev}` (admin) returns one revision by its number: `Revision` plus `title`, `statement_md`, `statement_recorded`, `time_limit_ms`, `memory_limit_mb`, `checker`, `epsilon`, `samples`, `open`, `subtasks [{idx, points, title, tests}]` and `test_count`. `404 not_found` for a number the problem does not have. A reconstructed revision has `statement_md` `""` and `statement_recorded` `false` (`true` on every other), and its limits are `null` when no judging job recorded them.

`GET /api/admin/problems/{id}/revisions/diff?from=2&to=3` (admin, revision numbers) compares two revisions:

```json
{ "from_rev": 2, "to_rev": 3, "title_from": "A plus B", "title_to": "A plus B",
  "changed": ["statement"], "statement_only": true, "coarse": false,
  "lines": [ { "op": "same", "text": "Read two integers." }, { "op": "del", "text": "1 <= n <= 10" }, { "op": "add", "text": "1 <= n <= 100" } ] }
```

- `changed` lists what differs, from `title`, `statement`, `limits`, `checker`, `solutions`, `samples`, `open`, `subtasks`, `tests`. `statement_only` is true when only `title` and `statement` changed: moving a contest to `to_rev` then needs no validation and no rejudge.
- `lines` is a line diff of the statements: `op` is `same`, `del` (only in `from`) or `add` (only in `to`). A statement pair too large to compare line by line (more than a million line pairs after the common start and end) comes back `coarse`: every old line deleted, then every new line added.

### `StatementHistory` (C3, students)

`GET /api/contests/{slug}/problems/{label}/revisions` for anyone who may read the contest workspace (the student's window has opened, or organisers). Reads are rate limited like the workspace.

```json
{ "label": "A", "current_revision_id": 14, "statement_updated_at": 1792311120000,
  "revisions": [ { "revision_id": 14, "rev": 3, "pinned_at": 1792311120000, "title": "A plus B", "statement_changed": true, "statement_recorded": true },
                 { "revision_id": 12, "rev": 2, "pinned_at": 1792300000000, "title": "A plus B", "statement_changed": false, "statement_recorded": true } ],
  "diff": { "from_revision_id": 12, "to_revision_id": 14, "from_rev": 2, "to_rev": 3,
            "title_from": "A plus B", "title_to": "A plus B", "coarse": false,
            "lines": [ { "op": "same", "text": "Read two integers $a$ and $b$." },
                       { "op": "del", "text": "Print their sum." },
                       { "op": "add", "text": "Print their sum modulo $10^9+7$." } ] },
  "server_now": 1792311200000 }
```

| Field | Meaning |
|---|---|
| `revisions` | the revisions pinned to this label, newest first: for students the one in force when the contest started and every later pin; for organisers every pin. Each revision appears once, with the time it was last pinned |
| `statement_changed` | the title or statement differs from the next older entry; `false` on the oldest |
| `statement_recorded` | `false` for a revision reconstructed at the upgrade, whose statement was never recorded; a diff against it has no lines |
| `diff` | by default the newest entry against the one before; `?from=&to=` (revision ids, both in `revisions`) picks another pair, anything else is `404 not_found`. `null` when there is only one revision |
| `statement_updated_at` | as on `Contest.problems` |

Only titles and statements: never a test, an output, a limit or a hash. A student never sees a revision that was not pinned to a contest they are in.

### Pins on `AdminContest` (C4, organisers)

`AdminContest.problems[]` gains these fields, and `AdminContest` gains `pin_history`:

```json
{ "problems": [ { "label": "A", "problem_id": 3, "revision_id": 12, "rev": 2, "latest_revision_id": 14, "latest_rev": 3,
                  "update_available": true, "update_statement_only": true, "reconstructed": false } ],
  "pin_history": [ { "label": "A", "problem_id": 3, "revision_id": 12, "rev": 2, "from_at": 1792300000000,
                     "actor": { "user_id": 1, "email": "owner@college.edu", "role": "owner" } } ] }
```

| Field | Meaning |
|---|---|
| `revision_id`, `rev` | the pinned revision: what students are served and what submissions and rejudges record. The console states it before a rejudge ("Rejudge against revision 2") |
| `latest_revision_id`, `latest_rev` | the problem's newest revision |
| `update_available` | the newest revision differs from the pinned one and is validated, so "Update contest to revision N" will be accepted |
| `update_statement_only` | the newest revision differs only in its title or statement: no rejudge is needed after the update |
| `reconstructed` | the pin is a revision reconstructed at the upgrade |
| `pin_history` | every pin change, newest first, at most 200: problems added or re-added (`PUT .../problems`) and revision updates. `actor` is `null` for pins the upgrade to v1.2.0 mapped. Co-organisers see it for their own contests |

### Update contest to revision N

`PUT /api/admin/contests/{id}/problems/{label}/revision` with `{"revision_id": 14}` (owners and admins).

```json
{ "contest": { "...": "AdminContest" },
  "announcement_md": "The statement of problem A was updated. Open the problem and choose \"See what changed\".",
  "statement_changed": true, "rejudge_needed": false, "server_now": 1792311120000 }
```

- The revision must belong to the label's problem (`404 not_found` otherwise) and be the validated content of the problem now: its content hash equals the problem's, and a passing validation covers it (`409 not_ready` "Revision 3 is not validated. Validate the problem, then update the contest."). Pinning the revision already pinned is `409 conflict`.
- The pin, its subtask points and `pinned_hash` change in one transaction with a `contest_pin_history` row and a `revision` row in the contest audit. Students get a `statement` event; their next workspace load serves the new statement.
- `announcement_md` is a prefilled announcement for the console to offer (it is not posted). With `rejudge_needed` (the tests, limits or checker changed) it says submissions will be judged again, and the console should offer Rejudge; with only a statement change it points students to "See what changed".

### The served log and "what X saw at T"

The first time the contest workspace serves a revision to a student, the server records `(student, contest, revision, first_seen_at)`; serving it again records nothing. With the pin history this answers a dispute: at time T the pinned revision was the newest pin with `from_at <= T`, and the student had been served it if its `first_seen_at <= T`. `podium-server saw` prints the answer on the VM (`docs/OPERATIONS.md`).

### Site audit: People History

`GET /api/admin/site-audit?page=1&action=&q=&since=&until=` (owner only) is the People page's "History": every change to people, domains and the site, newest first, 100 per page.

```json
{ "entries": [ { "id": 31, "at": 1792311120000, "action": "role_granted",
                 "actor": { "user_id": 1, "email": "owner@college.edu", "role": "owner" },
                 "subject_email": "ta@college.edu", "details": { "role": "admin", "from_role": "student" } } ],
  "page": 1, "has_more": false, "server_now": 1792311200000 }
```

- Filters: `action` keeps one action; `q` keeps rows whose actor or subject email contains the text (case does not matter); `since` and `until` are Unix ms (inclusive, exclusive).
- `actor` is `null` for changes the server made itself (a change of run mode). The actor's email and role are as they were at the time.
- The table is append only (database triggers refuse every update, and every delete of a row newer than the retention cutoff, which the daily retention job moves to 4 years ago from v1.3.0). It never holds an AI key, a session token, a cookie or an OAuth code: a test writes fake ones through every audited path and searches the table for them.

| `action` | `subject_email` | `details` |
|---|---|---|
| `role_granted` | the person | `{role, from_role}` |
| `role_revoked` | the person | `{from_role, sessions_ended}` |
| `domain_added` | `""` | `{domain}` |
| `domain_removed` | `""` | `{domain, sessions_ended}` |
| `account_disabled` | the person | `{sessions_ended}` |
| `account_enabled` | the person | `{}` |
| `sessions_ended` | the person | `{count}` |
| `google_reset` | the person | `{sessions_ended}` |
| `mode_changed` | `""` | `{from, to, sessions_ended, demoted}`, the server's own record when it starts in another run mode |
| `deploy_forced` | `""` | `{reason, release, refused: [{contest_id, slug, reason}]}`, written by `deploy-check -force` (the actor is the owner account) |

| `settings_changed` | `""` | `{fields}`: the names of the site settings that changed (v1.3.0) |
| `contact_deleted` | the sender | `{message_id, topic}`, never the text (v1.3.0) |
| `user_forgotten` | `""` | `{user_id, placeholder, role, sessions, drafts, integrity_events, contact_messages, co-organiser_places, browser_hashes_on_submissions, roster_entries}`: `podium-server forget-user` (v1.3.0); the person is named by id only |
| `semester_rollover` | `""` | `{label, accounts_disabled, sessions_ended, emails, contests_archived: [{id, slug}], seen_before}` (v1.3.0) |

Show an unknown action by its name.

### Renamed contests

Renaming a contest (its `slug`) records the old slug. `GET /contests/<old>` and every path under it answer `301` to the same path under the new slug, keeping the query; so do `GET` requests under `/api/contests/<old>` (other methods answer `404`). Only a published contest records its old slug (a draft has no links out there), and only a published, unarchived contest is redirected to. A slug in the history cannot be given to another contest: `400 bad_request` with `fields.slug` "This slug belonged to another contest. Choose another so its old links keep working." A contest may take back its own old slug.

## Contact (v1.3.0, contract C6)

A signed-in person writes to the organisers; owners and admins read the messages in an inbox and reply by email. There is no mail server: nothing is sent anywhere (D71 as amended). Someone who cannot sign in uses the organiser address from `GET /api/auth/config` (`contact_email`) instead.

### `POST /api/contact` (signed in)

Any signed-in role. The sender is the session's account; the body names no sender.

```json
{ "topic": "contest", "message": "The statement of problem B says 1 <= n but the sample has n = 0." }
```

| Field | Rule |
|---|---|
| `topic` | `contest` (a contest or a problem), `account` (sign-in or the account), `privacy` (a copy or removal of personal data) or `other` |
| `message` | 1 to 2,000 characters after trimming; newlines are kept; other control characters are refused |

| Status | Body |
|---|---|
| 201 | `{"id": 12, "created_at": 1792311120000}` |
| 400 | `bad_request` with `fields.topic` or `fields.message` |
| 401 | `signed_out` |
| 403 | `cross_site`: the request carried no `Origin` header, or one other than the base URL (this route needs it, unlike the general rule above) |
| 429 | `rate_limited` with `retry_after_s`: 5 messages per person in 24 hours, or 100 in 24 hours from everyone together |

The two `429` messages: "You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers." and "The organisers have received too many messages today. Try again tomorrow, or email the organisers." The web app's success line is "Sent. The organisers reply by email, usually within two days."

Messages are stored as plain text and are never rendered as HTML anywhere. They are deleted 180 days after they were sent (see `retention` on the config route) and when the sender is erased with `podium-server forget-user`.

### `GET /api/admin/contact?before=` (owner, admin)

The inbox, newest first, 50 per page. `before` is a message id: the page holds messages older than it.

```json
{ "messages": [ { "id": 12, "topic": "contest", "message": "The statement of problem B says ...",
                  "created_at": 1792311120000,
                  "sender": { "user_id": 7, "email": "asha@college.edu", "name": "Asha Rao", "role": "student" } } ],
  "more": false, "server_now": 1792311200000 }
```

`sender` is the account as it is now. The inbox screen offers "Reply by email" as a `mailto:` link to `sender.email`. Co-organisers whose site role is student get `403 forbidden`.

### `DELETE /api/admin/contact/{id}` (owner, admin)

Deletes one message. `204`. `404 not_found` when there is no such message. Writes a `contact_deleted` row to the site audit with the sender as `subject_email` and `{"message_id", "topic"}` as details (never the text).

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
| `POST /api/integrity/cases/{case}/opened` | none | 204 (v1.4.0): the student opened the case; see "C9: deadlines" |
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

## Students (v1.4.0, contracts C8, C9 and C12)

Progress, manual review, notices, Standing and the organisers' students pages (v2 P4; decisions D74 as amended by D110, D77 as amended, D97, D166 to D180). Times are Unix ms and every answer carries `server_now`. "Reviewer" means who may review the contest's integrity results (the owner, admins, and a co-organiser whose site role is not student, as in "Integrity" above); "organiser" in a route below means the owner or an admin unless the row says otherwise.

Nothing in this section ever returns another student's name to a student, code to anyone but its author and reviewers, test data, or the rating's number to a student. The rating is read by the record page, the students table and the rating routes only; standings, results, progress, exports and notices never read it (a test checks the code).

### Who may do what

| Data or action | Student (own) | Student (another's) | Co-organiser, site role student | Admin | Owner |
|---|---|---|---|---|---|
| Own progress, standing, notices, appeals | yes | 404 | own only | own only | own only |
| `GET /api/admin/users/{id}/progress` | 403 | 403 | 403 | yes | yes |
| Private notes, read and write | 404 | 404 | 403 on their contest, 404 elsewhere | yes | yes |
| Open a manual case | 404 | 404 | 403 on their contest, 404 elsewhere | yes | yes |
| Propose an outcome | - | - | 404 | yes | yes |
| Confirm an outcome | - | - | 404 | yes, if they did not open the case or propose | yes, same; alone only as sole organiser |
| Rule on an appeal | - | - | 404 | yes, if they took no part | yes, same; alone only as sole organiser |
| Record page, rating, rating edit | 403 | 403 | 403 | yes | yes |
| Students table, student page | 403 | 403 | 403 | yes | yes |
| Class view of a contest | 404 | 404 | when results are not hidden from them | yes | yes |
| Reviewer oversight, record view log | 403 | 403 | 403 | 403 | yes |

Every read of a student's record page, of a case, and of the notes on a submission by someone other than that student adds a row to the record view log (D97), which only the owner reads.

### C8: progress

| Route | Who | Success |
|---|---|---|
| `GET /api/me/progress` | any signed-in person, for themselves | 200 `Progress` |
| `GET /api/admin/users/{id}/progress` | owner, admin | 200 `Progress` for that account; 404 for an unknown id |

```json
{
  "user": { "id": 7, "name": "Asha Rao" },
  "hide_progress": false,
  "generated_at": 1792183192359,
  "totals": {
    "contests_taken": 5, "graded_taken": 4,
    "points": 820, "max_points": 1500,
    "problems_full": 7, "problems_partial": 5, "problems_zero": 3,
    "personal_best_percent": 86.7,
    "upsolved": 2
  },
  "contests": [
    {
      "contest_id": 3, "slug": "week-6", "title": "Week 6 contest",
      "graded": true, "feedback_mode": "subtask", "detail": "full",
      "ended_at": 1792183192359, "finalised_at": 1792183252359,
      "score": 240, "max_score": 300, "percent": 80.0,
      "rank": 12, "ranked_count": 140,
      "problems": [
        {
          "label": "A", "title": "A plus B", "problem_id": 11,
          "score": 100, "max_score": 100,
          "attempts": 2, "full_at_minutes": 14, "best_verdict": "AC",
          "subtasks": [ { "idx": 0, "points": 40, "earned": 40 }, { "idx": 1, "points": 60, "earned": 60 } ],
          "tags": ["math"], "difficulty": "easy",
          "practice_open": true, "upsolved": false,
          "zero_by_decision": false
        }
      ]
    }
  ],
  "by_tag": [ { "tag": "graphs", "earned": 120, "available": 300, "problems": 3 } ],
  "by_difficulty": [ { "difficulty": "easy", "earned": 300, "available": 300, "problems": 3 } ],
  "losses": [ { "verdict": "WA", "problems": 4 }, { "verdict": "TLE", "problems": 2 } ],
  "retry": [ { "slug": "week-6", "label": "B", "title": "Paths", "score": 40, "max_score": 100, "practice_open": true } ],
  "server_now": 1792183300000
}
```

| Field | Meaning |
|---|---|
| `contests` | only contests the person took part in (on the final board), newest first, and only once the results are final for students: every window closed, judging done, and with `hold_reveal` only after the reveal. A contest still frozen or held is absent. A contest past retention (4 years) drops out |
| `detail` | `full`, or `totals` for an exam-mode contest: then each problem's `score`, `attempts`, `full_at_minutes`, `best_verdict` and `subtasks` are `null`, and the contest counts only in `totals.points`, `max_points` and `contests_taken`, never in `by_tag`, `by_difficulty`, `losses`, the problem counts or `retry` |
| `percent` | `score` of `max_score`, one decimal; `personal_best_percent` is the highest over graded contests, `null` without one |
| `rank`, `ranked_count` | the final board's rank and how many were on it; equal totals and times share a rank |
| `full_at_minutes` | minutes from the person's own start to the first submission that reached full score; `null` if never |
| `best_verdict` | the verdict code (`AC`, `WA`, `TLE`, `MLE`, `RE`, `CE`) of the highest-scoring submission (the earliest of equals); `null` without a judged submission |
| `subtasks` | the points each subtask earned on the board, never a test, its number or its data |
| `tags` | the problem's tags without `ai draft`; contests reach this response only after they end, so tags never spoil a live problem |
| `practice_open` | the contest is open for practice now |
| `upsolved` | the problem was below full score on the board and later reached full score in practice; `totals.upsolved` counts them |
| `zero_by_decision` | a review decision set this problem to zero on the board (D77); the person's case says why |
| `by_tag`, `by_difficulty`, `losses`, `retry` | computed by the server from the `full` contests, so the web app need not derive them. `by_difficulty` is in the order easy, medium, hard; `losses` counts problems below full score by `best_verdict`, largest first; `retry` lists problems below full score, newest contest first |
| `hide_progress` | the person's own preference (`PUT /api/me/preferences`). On `GET /api/me/progress`, when `true`, `contests`, `by_tag`, `by_difficulty`, `losses` and `retry` are empty and `totals` are zero, so the page can offer "Show my progress" and nothing else. The organisers' route ignores it and leaves the field out |

Practice never changes a contest's score, rank or the counts; it shows only as `upsolved`. There is no class median, percentile or comparison here (D94). After `podium-server forget-user` the account's progress is empty. Progress is read from the per-student contest summaries (below), which are written when a contest turns final and again after a rejudge of a final contest; there is no per-request recompute.

### C9: notices

`GET /api/me` gains two fields:

```json
{ "notifications_unread": 2, "hide_progress": false }
```

`notifications_unread` is `0` while the person is in a live contest (their own window is open in a contest they started or submitted to): notices are held, never delivered, during a contest.

`PUT /api/me/preferences` accepts `hide_progress` (boolean, optional; left out keeps the saved value) next to `theme` and `verdicts`, and answers with all three.

| Route | Request | Success |
|---|---|---|
| `GET /api/me/notifications` (`?before=ID`) | | 200 `{notifications: [Notice], unread, held, has_more, server_now}`, newest first, 50 per page |
| `POST /api/me/notifications/read` | `{"ids": [12, 14]}` or `{"all": true}` | 204; ids that are not the person's are ignored |

While the person is in a live contest the list is empty, `unread` is `0` and `held` is `true`; everything shows once their window closes. A notice made for a case shared during a contest becomes available when that contest ends for the student.

```json
{ "id": 14, "kind": "case_shared",
  "happened": "An organiser asked you to explain your submission to problem B in Week 6.",
  "todo": "Read the question and reply on the case page.",
  "due_at": 1792904800000,
  "reassurance": "Nothing affects your marks until a person decides.",
  "link": "/integrity#case-12", "case_id": 12, "contest_id": 3,
  "created_at": 1792300000000, "read_at": null }
```

| `kind` | When | `due_at` |
|---|---|---|
| `case_shared` | a reviewer shared a case with the person | the reply deadline (see "Deadlines") |
| `reminder` | 5 days after the notice was opened (or became available, if never opened), with no reply yet | the reply deadline |
| `case_decided` | the case was cleared, or an outcome was confirmed | the appeal deadline for an outcome; `null` when cleared |
| `appeal_decided` | a ruling on the person's appeal | `null` |
| `standing_adjusted` | an organiser changed the person's standing by hand | the appeal deadline |
| `results_final` | a contest the person took part in has final results | `null` |

The server writes the sentences from fixed templates with the contest's title, a problem's label and nothing else: never code, evidence, test data, a reviewer's note or any person's name. `reassurance` is set on `case_shared` and `reminder` only. The web app formats `due_at` in the reader's time zone ("Reply by 26 Oct"). `link` is a path in the web app. Notices are kept 180 days.

### C9: private notes and manual cases

| Route | Who | Request | Success |
|---|---|---|---|
| `GET /api/admin/submissions/{id}/notes` | reviewers of the submission's contest (owners and admins; a co-organiser whose role is not student is an admin) | | 200 `{submission: SubmissionRef, source, notes: [Note], case_id, server_now}`, where `source` is the submission's program as plain text, so the reviewer reads it and picks lines; logged; `404` on one's own submission |
| `POST /api/admin/submissions/{id}/notes` | same | `{"text": "...", "lines": [{"from": 12, "to": 18}]}` | 201 `Note` |
| `POST /api/admin/submissions/{id}/case` | same | `{"title": "...", "note": "..."}` | 201 `Case` |

- A note or a manual case is possible on a Submit (not a Run) of a contest, practice after the contest included. A submission outside a contest is `404`.
- `text` is 1 to 2,000 characters, plain text, never rendered as HTML. `lines` are 1-based line ranges of the submission's code to highlight, at most 20, each `from <= to`.
- `SubmissionRef`: `{id, contest_id, user_id, problem_label, language, practice, created_at}`.
- `Note`: `{id, submission_id, by, by_name, text, lines, created_at, expires_at}`. `expires_at` is when the note is deleted (30 days after it was written), `null` once a case has been opened from the submission: then it is kept with the case. Notes are append only: they cannot be edited or deleted by hand. The student is never told about a note.
- `case_id` is the manual case opened from the submission, `null` if none.
- The student sees a manual case's evidence as `kind: "manual"`, the sentence "An organiser asked you to explain this submission. The lines in question are highlighted.", their own `source` and `lines` (the notes' ranges); never the notes' text.
- Opening a case: `title` at most 120 characters, `note` (the reason, for reviewers) 20 to 4,000; the case's student is the submission's author, its `source` is `manual`, its evidence is the submission and its notes, copied. The reviewer may not be the author (`400`). The case starts `open`, as one from signals does, and is shared with `PUT /api/admin/integrity/cases/{case}` as before.
- The console feed (`GET /api/admin/contests/{id}/submissions`) gains `note_count` and `case_id` on each item for reviewers; others never see them.

### C9: cases, outcomes and the two-person rule

Every route that returns a `Case` adds:

```json
{
  "source": "manual", "submission_id": 80, "created_by": 2,
  "users": [{
    "user_id": 7, "name": "Asha Rao",
    "notice_available_at": 1792300000000, "first_opened_at": 1792310000000,
    "respond_by": 1792914800000, "prior_outcomes": 1
  }],
  "ready_at": 1792914800000, "decide_by": 1794124400000, "overdue": false,
  "policy": "referral_suggested",
  "proposals": [ Proposal ],
  "decisions": [{ "outcome": "zero_problem", "outcome_problem_label": "B", "outcome_users": [7], "proposal_id": 4, "sole_organiser": "" }],
  "appeals": [ Appeal ],
  "may_confirm": true, "may_rule": false, "sole_organiser": false
}
```

| Field | Meaning |
|---|---|
| `source` | `signals` (opened from a check) or `manual` (opened from a submission) |
| `notice_available_at` | when the student could first see the latest share: the share, or the end of a contest they were in when it was shared |
| `first_opened_at` | when the student first opened the case after the latest share |
| `respond_by` | the student's reply deadline (see "Deadlines"); `null` while no turn is open |
| `prior_outcomes` | the student's confirmed outcomes above a warning in the last 2 years, in other cases, not overturned |
| `ready_at`, `decide_by`, `overdue` | when every student had replied or run out of time; 14 days after that; whether `decide_by` has passed with the case still undecided. `null` and `false` while not shared |
| `policy` | `referral_suggested` when a student of the case has a prior outcome above a warning in the last 2 years: the decision form says "Policy: referral suggested". A person still decides |
| `may_confirm` | the viewer may confirm the pending proposal (not its proposer, not the case's opener) |
| `sole_organiser` | the viewer is the owner and no other owner or admin exists: they may confirm their own proposal or rule alone, with a written reason |
| `may_rule` | the viewer may rule on an open appeal of the case |

`Proposal`: `{id, by, by_name, outcome, problem_label, user_ids, note, reason, created_at, state}`; `state` is `pending`, `confirmed` or `superseded` (a later decision came first).

| Route | Request | Success |
|---|---|---|
| `POST /api/admin/integrity/cases/{case}/proposals` | `{"outcome": "zero_problem", "problem_label": "B", "user_ids": [7], "note": "...", "reason": "...", "seen_explanations": {"7": 1792300500000}}` | 201 `Case` |
| `POST /api/admin/integrity/cases/{case}/proposals/{id}/confirm` | `{"note": "...", "seen_explanations": {...}, "sole_organiser_reason": "..."}` | 200 `Case` |

- `outcome` is `warning`, `zero_problem` (needs `problem_label`, a label of the case's contest) or `referred`. `user_ids` are the students of the case the outcome is for (all of them when left out); the others are cleared by it. `note` (20 to 4,000) is for reviewers; `reason` (20 to 2,000) is the sentence the student reads on their standing.
- A proposal needs a shared case where every student replied after the latest share or ran out of time (the same rule as before for action), the contest over for everyone, and no other pending proposal (`409 proposal_pending`). A seen explanation that is out of date is `409 explanation_changed`.
- Confirming needs a reviewer who neither opened the case nor made the proposal, and who is not one of its students; anyone else is `403 two_person_rule`, "A second reviewer who did not open the case or propose this outcome confirms it." The exception: when no other owner or admin account exists, the owner may confirm with `sole_organiser_reason` (20 to 1,000 characters), recorded on the decision as `sole_organiser`.
- Confirming writes the decision (state `actioned`, with `outcome`, `outcome_problem_label`, `outcome_users`, `proposal_id`), closes the case, lowers each student's rating (warning 0.5, zero for problem 1.5, referred 2.5; never below 0), sends `case_decided`, and for `zero_problem` changes the board (below).
- `PUT /api/admin/integrity/cases/{case}` with `state: "actioned"` is now `400 proposal_needed`, "Propose the outcome instead: a second reviewer confirms it." Clearing, sharing and reopening are unchanged, except that a case closed with a confirmed outcome cannot be reopened (`400`, "An outcome is changed only through an appeal."). Clearing sends `case_decided`; sharing sends `case_shared`.

**Zero for problem changes the board (D77 amended).** The confirmed decision sets that student's score on that problem to 0 on every board computed from then on, so a later rejudge cannot bring it back; the rank order and tie-break follow. The public `Cell` carries no mark. `Standings` gains:

```json
{ "board_updated_at": 1792400000000, "board_update_reason": "review_decision" }
```

both `null` until a decision changed the board, so the results page can say "Board updated on <date> after a review decision" without a name. A frozen board does not change until the contest is over for everyone. The contest audit gains `board_adjusted` rows (`{decision_id, problem_label, user_id, rank_before, total_before, score_before, reversed}`) which co-organisers whose site role is student do not see. The CSV export gains a last column `zero_by_decision` (the labels set to zero by a decision, separated by `;`) and the JSON export's standings rows gain `zero_by_decision: ["B"]`, both for owners and admins only. The student's own `Contest` response marks the problem: `problems[].zero_by_decision: true` and `problems[].zero_case_id: 12` (absent for anyone else).

### C9: appeals

| Route | Who | Request | Success |
|---|---|---|---|
| `POST /api/me/appeals` | the student the outcome or adjustment is about | `{"case_id": 12, "text": "..."}` or `{"adjustment_id": 40, "text": "..."}` | 201 `Appeal` |
| `GET /api/admin/appeals` (`?state=open`, `?page=2`) | owner, admin | | 200 `{appeals: [Appeal], has_more, page, server_now}`, open first, then newest, 50 a page; every appeal listed is logged as a read of that student's record or case, the page's reads in one commit; per-user read limit (`429 rate_limited`) |
| `POST /api/admin/appeals/{id}/ruling` | owner, admin who took no part | `{"upheld": true, "reason": "...", "sole_organiser_reason": "..."}` | 200 `Appeal` |

`Appeal`: `{id, case_id, adjustment_id, user_id, text, created_at, decide_by, overdue, state, ruling}`; `state` is `open`, `upheld` or `rejected`; `ruling` is `{by, by_name, upheld, reason, created_at}` or `null`. Students get their own appeals without `by` and `by_name`; reviewers also get `user {name, email, roll_number}` (roll number empty here).

- One appeal per outcome per student and one per adjustment, within 14 days of the decision or the adjustment (`400` after: "The 14 days to appeal have passed."). `text` 20 to 4,000 characters.
- A ruling needs an owner or admin who did not open the case, propose, confirm or decide anything on it, and is not one of its students; for an adjustment, not the person who made or confirmed it. Anyone else is `403 uninvolved_reviewer`. The sole-organiser exception applies as above.
- `reason` (20 to 2,000) is shown to the student.
- **Upheld** on an outcome: the outcome leaves the student's standing, their rating gets back the amount that outcome took (never above 10), the semester rollover that outcome blocked is applied if nothing else blocks it, a zero on the board is removed (the board and the summary are rewritten), and when no student of the case keeps an outcome the case becomes `cleared` with a decision row. **Upheld** on an adjustment: the rating gets back that adjustment's change, within 0 to 10. **Rejected:** the reason is recorded. Either way `appeal_decided` is sent.

### C9: deadlines

| Clock | Rule |
|---|---|
| Reply | 7 days from the student first opening the case, never later than 21 days after the notice became available; a reminder on day 5 |
| Decide | 14 days from `ready_at`; after that the case is `overdue` for the owner |
| Appeal | 14 days from the decision or the adjustment |
| Ruling | 14 days from the appeal; then `overdue` |

Cases shared before v1.4.0 keep the old rule: 7 days from the latest share.

The student's `GET /api/integrity/cases` lists a case only once its notice is available (the shell's one call per sign-in does not count as opening). The case page calls `POST /api/integrity/cases/{case}/opened` (no body, `204`; `404` for a case not shared with the caller) when it shows the case: the first call after each share records `first_opened_at` and starts the 7 days. A client that never calls it leaves the 21-day cap in force, which never shortens a student's time. `StudentCase` gains:

```json
{ "respond_by": 1792914800000,
  "outcome": { "kind": "zero_problem", "problem_label": "B", "decided_at": 1792400000000, "reason": "...", "appeal_by": 1793609600000 },
  "appeal": Appeal, "can_appeal": true }
```

`outcome` is `null` unless the case closed with an outcome for this student; `reason` is the proposal's `reason`, written for the student.

### C9 and C12: Standing

| Route | Who | Success |
|---|---|---|
| `GET /api/me/standing` | any signed-in person, for themselves | 200 `{band, band_name, lines: [StandingLine], server_now}` |

```json
{ "band": "on_record", "band_name": "Outcomes on record",
  "lines": [
    { "id": "outcome-31-7", "kind": "outcome", "at": 1792400000000,
      "text": "Week 6: zero for problem B.", "reason": "...",
      "case_id": 12, "adjustment_id": null,
      "appeal": null, "appeal_by": 1793609600000, "can_appeal": true },
    { "id": "adjustment-40", "kind": "adjustment", "at": 1792500000000,
      "text": "Standing adjusted by an organiser.", "reason": "...",
      "case_id": null, "adjustment_id": 40,
      "appeal": { "state": "open", "created_at": 1792600000000 }, "appeal_by": 1793709600000, "can_appeal": false } ] }
```

- `band`: `good` "Good standing" (rating 9.0 to 10), `on_record` "Outcomes on record" (6.0 to 8.9), `repeated` "Repeated outcomes" (below 6.0). The number itself is never in a student's response.
- `lines`: confirmed outcomes for this person that no appeal overturned, and every manual adjustment, newest first, for 2 years after each. An open appeal shows as `appeal.state: "open"` ("Under appeal").

**Record page (C12).** `GET /api/admin/students/{id}/record` (owner, admin; logged):

```json
{ "student": { "id": 7, "name": "Asha Rao", "email": "asha@college.edu", "disabled": false },
  "standing": { "band": "on_record", "band_name": "Outcomes on record", "lines": [ StandingLine ] },
  "rating": { "value_tenths": 85, "band": "on_record",
              "history": [ { "id": 40, "at": 1792500000000, "delta_tenths": -15, "value_tenths": 85, "cause": "outcome",
                             "reason": "...", "case_id": 12, "decision_id": 31, "appeal_id": null,
                             "actor": { "id": 3, "name": "Ravi K" }, "confirmer": { "id": 2, "name": "Arjun M" },
                             "semester_label": "", "band_before": "good", "band_after": "on_record" } ] },
  "pending_rating": { "id": 5, "from_tenths": 85, "to_tenths": 55, "reason": "...", "by": { "id": 3, "name": "Ravi K" },
                      "created_at": 1792600000000, "may_confirm": true },
  "cases": [ { "id": 12, "contest_id": 3, "contest_title": "Week 6", "state": "actioned", "source": "manual",
               "outcome": "zero_problem", "created_at": 1792300000000, "decided_at": 1792400000000 } ],
  "notes": [ Note ],
  "appeals": [ Appeal ],
  "server_now": 1792700000000 }
```

- `value_tenths` is the rating in tenths, 0 to 100 (show 85 as 8.5); a student never rated yet is 100. `history` is newest first, kept 4 years; `cause` is `outcome`, `manual`, `appeal` or `rollover`; `band_before` and `band_after` differ on the line that crossed a band. `pending_rating` is `null` when nothing waits.
- `cases` are every case the student is in, for reviewers; `notes` every note on the student's submissions still kept. A viewer who is a student of a case never gets it here, nor its standing lines and appeals. Nobody opens their own record page (`403`), and an erased account is `404` on every students route.
- Every read of a case is logged: the case page, the contest's case list and the pair view, one row per student of the case; so is every appeal returned by `GET /api/admin/appeals`.

| Route | Who | Request | Success |
|---|---|---|---|
| `PUT /api/admin/students/{id}/rating` | owner, admin | `{"value_tenths": 70, "reason": "..."}` | 200 `{rating, pending_rating: null}` when applied; 202 `{rating, pending_rating}` when it waits for a second organiser |
| `POST /api/admin/students/{id}/rating/proposals/{pid}/confirm` | owner, admin other than the proposer | `{"sole_organiser_reason": "..."}` (only for the exception) | 200 `{rating, pending_rating: null}` |
| `POST /api/admin/students/{id}/rating/proposals/{pid}/decline` | owner, admin | | 200 `{rating, pending_rating: null}` |

- `value_tenths` 0 to 100 and different from the current value; `reason` 20 to 1,000 characters, shown to the student on their standing. A co-organiser is `403 forbidden`.
- An edit that lowers the student's band is a proposal: `202`, until a second owner or admin confirms it (`403 two_person_rule` for the proposer, except as sole organiser). A new edit replaces a pending one. A confirm after the rating moved is `409 rating_changed`, "The rating changed since this was proposed. Propose it again."
- Every applied edit writes a dated line on the student's standing ("Standing adjusted by an organiser" with the reason), sends `standing_adjusted`, and can be appealed.
- Writes to one student's rating go one at a time; every write keeps the value within 0 to 10.

**Semester rollover of the rating.** `podium-server semester-rollover [-db F] [-dry-run] LABEL` (on the VM, by the owner, when a term ends) adds 0.5 to every student with no confirmed outcome since the previous rollover, up to 10, with cause `rollover` and the label. A label runs once; running it again changes nothing. This is separate from `POST /api/admin/rollover`, which archives contests and disables old accounts.

### C9: organisers' students pages

| Route | Who | Success |
|---|---|---|
| `GET /api/admin/students` (`?filter=missed_last_two` or `?filter=no_accepted`, `&q=`) | owner, admin | 200 `{students: [StudentRow], filter, last_graded: [{contest_id, slug, title}], server_now}`, sorted by name |
| `GET /api/admin/students/{id}` | owner, admin | 200 `{student: {id, name, email}, contests: [StudentContest], server_now}` |
| `GET /api/admin/contests/{id}/class` | the contest's organisers (co-organisers whose site role is student once results are not hidden from them) | 200 `ClassView`; `409 not_final` "The class view opens when the results are final." |
| `GET /api/admin/integrity/oversight` | owner | 200 `{reviewers: [{user_id, name, email, manual_opened, cleared, actioned, overdue, proposals, confirmations, rulings}], server_now}` |
| `GET /api/admin/record-views` (`?subject=ID&viewer=ID&before=ID`) | owner | 200 `{views: [{id, at, kind, ref_id, viewer: {id, name, email}, subject: {id, name}}], has_more, server_now}`, newest first, 100 per page |

- `StudentRow`: `{id, name, email, graded_contests, last_graded_percent, last_graded_slug, last_submission_at, rating_tenths}`. `graded_contests` counts final graded contests the student took part in; `last_graded_percent` is their percent in the most recent one (`null` without one); `last_submission_at` is their latest Submit anywhere (practice included), `null` if none. Sorting is by name, never by score.
- Filters, over final graded contests only (practice never triggers one): `missed_last_two`, the two most recent graded contests the student could enter (domain access, or on the roster) and is on neither board; `no_accepted`, in the most recent graded contest, 10 or more submissions and none accepted. `last_graded` names the contests the filters used. `q` matches name or email.
- `StudentContest`: `{contest_id, slug, title, graded, ended_at, score, max_score, percent, class_median_percent, rank, ranked_count, problems_attempted, problems_full, submissions, accepted}`, newest first. No integrity data on this page.
- `ClassView`: `{contest_id, participants, problems: [{label, title, max_score, attempted, full, median_score, subtasks: [{idx, points, solved, solve_rate}], most_failed_test: {subtask_idx, test, failed} | null}], server_now}`: totals only, no names or user ids. `solve_rate` is the share of participants who earned the subtask's full points, one decimal; `most_failed_test` is the test (1-based within its subtask) that was the first failure in the most participants' best submissions, among those that failed one. In exam mode the subtasks and the failing test are left out.

### Changed by v1.4.0 elsewhere

- **Contest audit, `extension`:** new rows carry `reason_given` (`true` when a reason was written) instead of `reason`; the reason itself stays on the extension, where organisers read it. Rows written before v1.4.0 keep `reason` until the audit retention removes them, so read `reason_given` and fall back to `reason`.
- **`board_adjusted`** joins the contest audit actions (above).
- **Erasure:** `podium-server forget-user` also deletes the person's notices and contest summaries and resets `hide_progress`; notes, cases, appeals, the rating and its history stay until their own retention (they are the organisers' record of a decision).

## Judge worker API

Used only by `podium-judge work` (decision D44). It is served on its own listener, `PODIUM_JUDGE_LISTEN` (default `127.0.0.1:8081`, loopback only), never through Caddy, and is not part of the public API above: no session cookies, no cross-site check. The request and response types live in `server/internal/judgeapi`, shared by both programs.

- **Auth.** Every request sends `Authorization: Bearer <PODIUM_JUDGE_TOKEN>`. The server compares SHA-256 digests of the two tokens in constant time. Anything else is `401 unauthorized`.
- **Bodies.** JSON with no unknown fields; at most 64 KB for a claim and 1 MB for a report. A malformed body is `400 bad_request`. An unknown route is `404 not_found`. The one non-JSON body is an uploaded output (`PUT /api/judge/blob/{sha}`, at most 16 MB).

**Protocol versions.** 1 is the v1.0.0 judge; 2 (v1.1.0) added the toolchain report and the toolchain hash on each claim; **3 (v1.2.0, D115)** adds open tests and the revealed test: a run input may carry `"open": true` (the judge runs it only while every earlier input with an expected output passed, and reports `SK` otherwise), and a submit payload may carry `"reveal": true` (the judge then reports `output`, the first 4 KB of the program's output, on the first failing hidden test; the server refuses output on any other submit result). A judge and server of different protocols refuse each other with `426`.

### `POST /api/judge/claim`

Request: `{"kinds":["run","submit","generate","validate"],"worker":"vm-1","version":"v1.1.0","isolate":"2.7","protocol":3,"toolchain":"<sha256>"}`. `worker`, `version` and `isolate` are logged and stored with the job. `protocol` is the judge's `judgeapi.ProtocolVersion`; left out, it means 1 (the v1.0.0 judge). `toolchain` is the hash `POST /api/judge/toolchain` returned; the job records that toolchain (`jobs.toolchain_id`), or none when the hash is unknown or missing.

| Status | Body |
|---|---|
| 204 | no queued job of those kinds (unknown kinds are ignored) |
| 200 | `{"job_id":41,"kind":"validate","lease":"<secret>","lease_seconds":60,"payload":{...}}` |
| 426 `protocol` | the judge speaks another protocol: `{"error":{"code":"protocol","message":"judge protocol 3 needed, this judge speaks 2: install the same release on both"}}`. The judge stops with this message (exit status 3, which `podium-judge.service` does not restart) instead of retrying. A 401 to a claim or toolchain report, five times in a row, stops it with exit status 4 the same way |

### `POST /api/judge/toolchain`

Sent once when the judge starts (B1.4). Request: `{"protocol":3,"toolchain":{"gcc":"g++ (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0","java":"javac 21.0.8","python":"Python 3.12.3","node":"v22.20.0","isolate":"The process isolator 2.7","kernel":"6.8.0-85-generic"}}`: the first line of each tool's version output, `missing` when the tool cannot run, each field at most 200 bytes of UTF-8.

| Status | Body |
|---|---|
| 200 | `{"hash":"<sha256>"}`, the SHA-256 of the toolchain's JSON; the same report always gives the same hash and is stored once |
| 400 `bad_report` | a field over 200 bytes or not UTF-8 |
| 426 `protocol` | as for a claim |

A judge newer than the server reads an older server's `400` to a claim (unknown fields) or `404` for this route as a protocol mismatch too, and stops.

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

A renamed contest's old `/contests/<slug>` paths answer `301` first (see "Renamed contests").

### The route manifest and the page markers (contract C2, from v1.1.0)

The web build writes `web/dist/routes.json`, embedded with the app; a copy is checked in at `server/internal/webui/testdata/routes.json` for the server's tests, and a web unit test fails when the two drift. From v1.3.0 the server reads it to fill the head of each page (below).

```json
{ "version": 1,
  "routes": [ { "pattern": "/signin", "name": "signin", "auth": false, "public": true,
                "title": "Sign in", "description": "Sign in to podium with your college Google account to enter the weekly programming contests." } ],
  "notFound": { "pattern": "*", "name": "not-found", "auth": false, "public": true, "title": "Page not found", "description": "That page does not exist on podium." } }
```

| Field | Meaning |
|---|---|
| `version` | the manifest format, 1 |
| `routes[].pattern` | the client route, as `routing.ts` matches it |
| `name`, `auth` | the route's name, and whether it needs a signed-in user |
| `public` | the page may be indexed and gets a canonical URL and Open Graph tags (P3); every other page stays `noindex` |
| `title`, `description` | the page's title and description, written by the frontend |
| `notFound` | the 404 page, in the same shape |

`web/index.html` carries two marker pairs the server replaces at request time and never edits in the file: `<!--podium:head-->` ... `<!--/podium:head-->` around the robots meta (`noindex` by default), and `<!--podium:body-->` ... `<!--/podium:body-->` inside a fallback `<main>` that the app clears before it mounts (P3 fills it for `/privacy`, so the page reads without JavaScript). The frontend owns both files and the manifest's content.

### The page head, 404s, robots and the sitemap (v1.3.0, D100)

The server reads `routes.json` from the embedded build once at start and answers every path that is not an API route, a file or one of the two files below with `index.html`, filled per route:

| The path matches | Status | `<!--podium:head-->` block | `<title>` | `X-Robots-Tag` |
|---|---|---|---|---|
| a route with `public: true` | 200 | robots `index, follow`, description, canonical, Open Graph, Twitter card (below) | `{title} · podium` | none |
| any other route | 200 | left as built (`noindex`) | `{title} · podium` | `noindex` |
| no route | 404 | left as built (`noindex`) | `{notFound.title} · podium` | `noindex` |

Matching follows `routing.ts`: trailing slashes are ignored, case matters, a `:name` segment takes any one segment. The 404 page is never indexed and has no canonical URL, whatever `notFound.public` says. Without a manifest, or with a `version` other than 1, every page answers 200 with the built head and `X-Robots-Tag: noindex` (default closed), and the server logs a warning at start.

A public page's block, with `PODIUM_BASE_URL` `https://podium.example`:

```html
<meta name="robots" content="index, follow" />
<meta name="description" content="Sign in to podium with your college Google account ..." />
<link rel="canonical" href="https://podium.example/signin" />
<meta property="og:type" content="website" />
<meta property="og:site_name" content="podium" />
<meta property="og:title" content="Sign in · podium" />
<meta property="og:description" content="Sign in to podium with your college Google account ..." />
<meta property="og:url" content="https://podium.example/signin" />
<meta property="og:image" content="https://podium.example/og.png" />
<meta property="og:image:type" content="image/png" />
<meta property="og:image:width" content="1200" />
<meta property="og:image:height" content="630" />
<meta property="og:image:alt" content="podium" />
<meta name="twitter:card" content="summary_large_image" />
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","email":"podium@college.edu","name":"Example College"}</script>
```

- The canonical URL is the base URL plus the route's pattern (for a public route with parameters, plus the request's path without its query); `og:url` is the same.
- The image tags appear only when the build has `og.png` at its root (the 1200 x 630 card the frontend makes, FP.4); without it the card is `summary` and names no image.
- The JSON-LD block is on `/signin` only, from the site settings only (`name`, and `email` and `telephone` when set), and absent until an owner has entered the organisation's name.
- Every value is HTML-escaped; the JSON-LD is JSON with `<`, `>` and `&` escaped, so no setting can close the script block.

**The body block.** For the route whose pattern is `/privacy`, the server puts the build's `privacy-body.html` (the approved privacy text, rendered from Markdown and sanitised at build time, FP.1) between `<!--podium:body-->` and `<!--/podium:body-->`, so the page reads without JavaScript. Until the build has the file the block stays empty. No other page fills it.

`routes.json` and `privacy-body.html` are read by the server and never served as files (`404`).

**`GET /robots.txt`** (text):

```
User-agent: *
Allow: /
Disallow: /api/

Sitemap: https://podium.example/sitemap.xml
```

Private pages are kept out of search by `noindex`, which a crawler can only read if it may fetch the page, so they are not disallowed.

**`GET /sitemap.xml`** (XML): one `<url><loc>` per public route without parameters, absolute from the base URL. `/contact` is signed in and never listed (D109).

Both answer with `Cache-Control: public, max-age=3600`. Icons and the web manifest are files of the build: `favicon.ico` is served as `image/x-icon`, `site.webmanifest` as `application/manifest+json`, fonts as `font/woff2`.

Contests are added to this document in M4, the milestone that builds them.
