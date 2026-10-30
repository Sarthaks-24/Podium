# HTTP API

All API routes live under `/api/` and speak JSON. Every other path serves the embedded web app, which renders its own pages (see "Static files"). The judge worker has its own routes on a separate listener ("Judge worker API").

The routes are registered in `server/internal/httpapi/httpapi.go`, the judge routes in `server/internal/jobs/judgeserver`. `server/internal/httpapi/route_roles_test.go` reads every registration from the source and holds the role each route needs, so a new route without a row there fails the tests; this document lists the same routes, grouped by area. Each heading or table row names the role: public, student (anyone signed in), organiser, owner, or judge.

## Conventions

- Responses are `application/json; charset=utf-8` with `Cache-Control: no-store`.
- Request bodies are JSON sent with `Content-Type: application/json` (anything else is `415 bad_request`), at most 64 KB unless a route says otherwise, with no unknown fields and nothing after the value.
- Every response carries a strict Content-Security-Policy (`script-src 'self'`, no inline scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin` and a `Permissions-Policy` that turns off the camera, microphone and location.
- **Time.** Every time is Unix milliseconds. Answers that a page uses to show a clock carry `server_now`, and every rule about time is applied with the server's clock, never the browser's.

### Errors

Errors use one shape, with a stable machine code and a sentence for people. The web app shows `message` as it is.

```json
{ "error": { "code": "not_found", "message": "This API route does not exist." } }
```

Some errors carry more. `fields` maps a form field to what is wrong with it; `problems` lists everything wrong with an upload or a request to validate, so it can be fixed in one pass; `retry_after_s` comes with every 429.

```json
{ "error": { "code": "bad_request", "message": "Some fields need fixing.", "fields": { "title": "Enter a title of at most 120 characters." } } }
```

| Code | Status | Meaning |
|---|---|---|
| `bad_request` | 400, or 415 for a wrong content type | the request cannot be used as sent |
| `signed_out` | 401 | no session, or it has ended |
| `forbidden` | 403 | signed in, but the role is too low for the route |
| `not_allowed` | 403 | the role reaches the route, but not this person: an admin acting on an admin, an owner, `PODIUM_OWNER_EMAIL` or themself, or giving the admin or owner role |
| `not_found` | 404 | no such route, or a thing that does not exist or that the caller may not know about |
| `cross_site` | 403 | a write from another origin |
| `conflict` | 409 | the change clashes with the current state |
| `rate_limited` | 429 | too many requests; wait `retry_after_s` |
| `busy` | 429 | the one slot for this kind of request is taken; also carries `Retry-After` and `retry_after_s` |
| `internal` | 500 | the server failed; the cause is in its log |
| `testing_mode_local` | 403 | testing mode, and the request did not come from this machine |

Areas add their own codes, listed with their routes: `disabled` (sign-in), `bad_pack`, `not_ready`, `too_large` (problems), `no_key`, `no_model`, `bad_key`, `provider_error` (AI wizard), `not_started`, `contest_over`, `too_early`, `contest_ended`, `not_participant`, `ai_unconfirmed` (contests), `stream_limit`, `unsupported` (event stream), `contest_live`, `already_running` (similar code), and `unauthorized`, `protocol`, `lease`, `bad_report`, `bad_upload` (judge).

### Sign-in, roles and requests

- **Sessions.** Signing in sets an HttpOnly session cookie, `SameSite=Lax`, with no `Expires` or `Max-Age`, so the browser forgets it when it closes, which matters on shared lab PCs. The server ends every session 12 hours after sign-in however much it is used (sessions never renew), at sign-out, when an owner ends the person's sessions or disables the account, and when the server starts in a different `PODIUM_MODE` than last time. With an `https` base URL the cookie is `__Host-podium_session` and marked `Secure`; with a local `http` base URL it is `podium_session`.
- **Roles.** `student` < `organiser` < `admin` < `owner`. An admin organises like an organiser and also manages students and organisers (below, "Settings"). Before v4 the organiser role was stored as `admin` (migration `0002_v4_roles` renamed it); site audit rows written before that migration are read with `organiser` in place of `admin`. A route needs its role or a higher one: without a session it answers `401 signed_out`, below the role `403 forbidden`. Roles are read from the database on every request, so removing an organiser takes effect at once.
- **Cross-site requests.** `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` are refused with `403 cross_site` unless the request's `Origin` matches `PODIUM_BASE_URL` (or, without an `Origin` header, `Sec-Fetch-Site` is `same-origin` or absent).
- **Testing mode is local only.** With `PODIUM_MODE=testing`, a request whose `Host` is not `localhost`, `127.0.0.1` or `[::1]` (any port), or that carries `X-Forwarded-For`, `Forwarded`, `X-Real-IP` or `X-Forwarded-Host`, is refused with `403 testing_mode_local`: a proxy in front means the browser is not on this machine.

### Limits

| Limit | Value | Answer |
|---|---|---|
| Requests open at once per session cookie | 8 (the event stream is not counted) | `429 rate_limited`, `Retry-After: 1` |
| Heavy reads per user: the contest list, a contest, a contest problem, the standings, the organiser's problem workspace, account lookup and the similar-code reads | 10 a second on average, bursts of 20 | `429 rate_limited` with the wait |
| Before sign-in, per client address | see "Before sign-in" | `429 rate_limited`, or a redirect for the Google routes |
| Upload bodies (a pack, a test zip) | 120 s to arrive; every other body must arrive within the server's 15 s read timeout | the connection is closed |

## Health and metrics

### `GET /healthz` (public)

| Status | Body |
|---|---|
| 200 | `{"status":"ok","db":"ok"}` |
| 503 | `{"status":"degraded","db":"unreachable"}` |

A request from the VM itself (a loopback peer with no forwarding headers) also gets the release and the judge queue, for `install.sh verify` and `podium-check`: `{"status":"ok","db":"ok","version":"v3.0.0","schema":"0001_v3","jobs_queued":2,"oldest_queued_s":40,"judge_last_claim_s":1}`. `judge_last_claim_s` is `null` when no judge has asked for work since the server started. Through Caddy the answer never carries these, so a scanner cannot match the version against known bugs.

### `GET /api/admin/metrics` (organiser)

What an organiser watches on contest day; also read by the load test (`tools/loadtest`). Cheap enough to poll every few seconds: two indexed queries and in-memory counters.

```json
{ "server_now": 1792183192400, "uptime_s": 5400, "version": "v3.0.0",
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

- `queue.kinds` lists every job kind, with zeros when idle. `queued` includes jobs held back by a paused contest (`queued_paused`).
- `finished_*`, the `submit_*` percentiles (Submit to first claim, and Submit to verdict, over submissions closed in the last 10 minutes) and `failed_last_5_min` live in memory and restart at zero with the server.
- `db.waits` and `db.wait_ms` count, since the start, requests that waited for a database connection.

### `GET /api/admin/live` (admin)

The Dashboard's Live tab (V4-17): what an admin watches during a run. From memory and two stat calls, so the tab reads it every 5 s while it is open.

```json
{ "server_now": 1792183192400, "signed_in_now": 42, "judge_last_seen_s": 3, "queued": 2, "submit_wait_p50_ms": 2400,
  "disk_free_mb": 18432, "database_mb": 54, "last_backup_at": 1792120000000 }
```

- `signed_in_now` counts people with a page open (an event stream), not every valid session.
- `judge_last_seen_s` is how long ago a judge last asked for work; `null` when none has since the server started.
- `disk_free_mb` is the space left where the database lives, `-1` when the server cannot tell (any system but Linux); `database_mb` is the database file and its write-ahead log.
- `last_backup_at` is the time of the newest nightly backup, read from the `latest` link `podium-backup` keeps in `PODIUM_BACKUP_DIR`; `null` when there is none.

## Sign-in and the signed-in person

### `GET /api/auth/config` (public)

What the sign-in page, the footer and the privacy page need before anyone signs in. No contest data, no counts, no version.

```json
{ "contact_email": "podium@college.edu",
  "mode": "production",
  "organisation": { "name": "Example College of Engineering", "contact_email": "podium@college.edu",
                    "phone": "+91 80 1234 5678", "privacy_contact": "privacy@college.edu" },
  "retention": [
    { "id": "sessions", "data": "Sign-in sessions", "amount": 12, "unit": "hours", "after": "sign_in", "kept": "12 hours after sign-in" } ] }
```

| Field | Meaning |
|---|---|
| `contact_email` | the address to show as the organisers' contact: `organisation.contact_email` once an owner has entered one, otherwise `PODIUM_OWNER_EMAIL` |
| `mode` | `testing`, `dev` or `production`. In `testing` the web app shows the test sign-in form and the testing-mode banner instead of "Continue with Google" |
| `organisation` | the site settings an owner enters under Settings, Organisation: `name`, `contact_email`, `phone`, `privacy_contact`, each `""` until entered; a page shows nothing for an empty field |
| `retention` | every retention period the server applies, in the order the privacy page lists them, from `server/internal/retention/periods.go`. The privacy page renders its table from this list |

A `retention` row is `{id, data, amount, unit, after, kept}`. The ids are `sessions` (12 hours after sign-in), `integrity_events` (180 days after the contest), `run_payloads` (30 days after the run) and `backups` (14 days); `unit` is `hours`, `days` or `years`; `after` is `sign_in`, `created` or `contest_end`; `kept` is the period as one phrase. The table with its reasons is in `docs/OPERATIONS.md`, "Retention".

### `POST /api/auth/test-signin` (public, testing mode only)

Registered only when `PODIUM_MODE=testing`, which serves loopback alone; in other modes the path is `404 not_found`. Signs in as anyone with any role, without Google. The request must also carry an `Origin` equal to the base URL.

Request: `{"email":"asha@college.edu","name":"Asha Rao","role":"organiser","next":"/admin/problems"}`. `name` and `next` may be empty; `role` is `student`, `organiser`, `admin` or `owner`. The account is created if needed, otherwise its name and role are updated. `PODIUM_OWNER_EMAIL` always keeps the owner role.

| Status | Body |
|---|---|
| 200 | `{"next":"/admin/problems"}` with the session cookie set; anything in `next` that is not a path on this site becomes `/` |
| 400 `bad_request` | not an email address, or an unknown role |
| 403 | `cross_site` (no `Origin`, or not the base URL), `testing_mode_local`, or `disabled` (an owner disabled the account) |

Any session the browser already had ends. Nothing made here survives a change of mode: when the server next starts in `dev` or `production`, every session ends and every organiser or owner without a linked Google account (other than `PODIUM_OWNER_EMAIL`) becomes a student again.

### `GET /api/auth/google/start` (public, browser navigation)

Not registered in testing mode, nor is the callback. `?next=/path` is where to land afterwards; it must be a path on this site (starts with `/`, not `//`), else `/`.

Redirects (302) to Google's consent screen with PKCE, a nonce and a state value held in a short-lived signed cookie. It shows the account chooser and asks for a sign-in from the last 5 minutes (`prompt=select_account`, `max_age=300`), so on a shared PC the previous student's Google session is not one click away.

### `GET /api/auth/google/callback` (public, Google redirects here)

Verifies the state, exchanges the code, verifies the ID token (signature, issuer, audience and authorised party, expiry, nonce, and `auth_time` no older than 5 minutes when present) and requires `email_verified`. An email may sign in only when it is on the list of people (every account is one row of it, V4-15) and the account is not disabled; `PODIUM_OWNER_EMAIL` is always on it, because the server puts it back at every start. Addresses are compared trimmed and in lower case, and for `gmail.com` and `googlemail.com` without the dots of the local part (Gmail ignores them); nothing else is rewritten. A refused address is recorded (address, first and last time, count; the 500 most recent are kept) so People can let it in. Each account is bound to the Google account (`sub`) that first signed in with it; a different Google account with the same address is refused. On success it updates the listed account (name and picture from Google; the address stays as it was listed), ends any session the browser had, starts a new one and redirects to `next`.

On failure it redirects to `/signin?error=<code>`:

| Code | Meaning |
|---|---|
| `not_listed` | the email is not on the list of people; the page says "Your email is not on the list for this site. Ask the organiser to add it." (a v3 `domain` link reads the same) |
| `unverified` | Google has not verified the address |
| `disabled` | an owner disabled the account |
| `account` | the address is linked to a different Google account (an owner can reset the link) |
| `expired` | the attempt took too long, was started in another tab, or was already finished once |
| `google` | Google refused, or the person cancelled, or the answer could not be verified |
| `server` | the server failed while recording the sign-in |
| `rate_limited` | too many sign-ins from this network at once; wait a few seconds |

### Before sign-in: the per-address limit

The routes a visitor reaches without a session have budgets per client address, each a token bucket:

| Routes | Burst | Refill |
|---|---|---|
| `GET /healthz`, `GET /api/auth/config`, `POST /api/auth/logout` | 2,500 | 50 a second |
| `GET /api/auth/google/start`, `GET /api/auth/google/callback`, `POST /api/auth/test-signin` | 2,000 | 20 a second |
| session cookies the database does not know | 1,000 | 10 a second |

Past a budget the JSON routes answer `429 rate_limited` with `Retry-After`; the two Google routes, being navigations, redirect to `/signin?error=rate_limited`. The budgets are large because a whole college signs in from few addresses behind its NAT; they exist so a script cannot turn the server into a flood of queries or of outbound calls to Google. The address is the peer's, or behind Caddy the last `X-Forwarded-For` entry; IPv6 addresses share a budget per /64. Requests from the VM itself are not counted. A session the server issued or accepted in the last minute skips the unknown-cookie budget, so a flood of forged cookies from a campus NAT cannot lock out the real students behind it.

Each sign-in attempt can be finished once: the callback remembers the state of every attempt until it expires (10 minutes), and a second callback with the same state gets `/signin?error=expired` without calling Google.

### `POST /api/auth/logout` (public)

Ends the current session on the server (the row is deleted, so a copied cookie is useless), forgets the session's AI keys and clears the cookie. `204`, also when already signed out.

### `GET /api/me` (student)

`200 {"id":1,"email":"a@college.edu","name":"Asha Rao","avatar_url":"https://...","role":"student","theme":"","verdicts":"standard"}`. `theme` is a display theme id, or `""` to follow the system; `verdicts` is `standard` or `signal` (colour-blind safe verdict colours).

### `PUT /api/me/preferences` (student)

Saves the display preferences, so they follow the account to another machine. Request: `{"theme":"dark","verdicts":"signal"}`, both required. `theme` is `""` or 1 to 32 characters of `a-z`, `0-9` and `-`; the server checks only the shape, and the web app treats an id it does not know as `""`. Response `200`: the saved preferences. Errors: `400 bad_request` for a malformed theme or an unknown verdict set.

## Settings: people, sign-in and organisation

The routes behind the Settings page. Reading people, giving and removing roles, ending sessions, disabling, enabling and reading the site audit need the admin role; resetting a Google link and the site settings need the owner role; the account lookup needs the organiser role.

An admin acts only on students and organisers, and gives only the organiser role: acting on an admin, an owner, `PODIUM_OWNER_EMAIL` or themself, or giving the admin or owner role, is `403 not_allowed` with a sentence that says an owner must do it (`not_allowed`, not `forbidden`, so a page can tell a refused change from a lost role). The check runs inside the change itself (`server/internal/auth/rules.go`), so every route that changes an account applies it. Every change here (roles, domains, disabling and enabling, ended sessions, Google link resets, site settings) writes a site audit row in the same transaction as the change.

`Account` is `{id, email, name, role, disabled, last_login_at}`; `last_login_at` is `null` for someone added who never signed in.

| Route | Request | Success | Errors |
|---|---|---|---|
| `GET /api/admin/people` | | 200 `{people, list, counts, refused, disabled, organisation}` (below) | |
| `POST /api/admin/people` | `{"email":"ta@college.edu","role":"organiser"}`; `role` is `organiser`, `admin` or `owner`; the person need not have signed in, and the role is theirs at first sign-in | 200 the person, as in `people` | `400 bad_request` (email or role), `403 not_allowed` (an admin out of bounds), `409 conflict` (the configured owner, or the change would leave no owner) |
| `DELETE /api/admin/people/{id}` | | 204: the person becomes a student and their sessions end | `404 not_found`, `403 not_allowed` (an admin out of bounds), `409 conflict` (the configured owner, or the last owner) |
| `POST /api/admin/accounts/reset-google` | `{"email":"asha@college.edu"}` | 204: unlinks the Google account and ends their sessions; the next Google account to sign in with the address is linked | `404 not_found` |
| `POST /api/admin/accounts/end-sessions` | `{"email"}` | 200 `{"account": Account, "ended": 2}`: signs the person out everywhere; their open event streams close at the next clock tick (25 s at most) | `404 not_found`; `403 not_allowed` (an admin out of bounds) |
| `POST /api/admin/accounts/disable` | `{"email"}` | 200 `{"account": Account, "ended": 1}`: ends every session; the account is refused at sign-in and at every signed-in route | `404 not_found`; `403 not_allowed` (an admin out of bounds); `409 conflict` for `PODIUM_OWNER_EMAIL` and for disabling yourself |
| `POST /api/admin/accounts/enable` | `{"email"}` | 200 `{"account": Account, "ended": 0}`: they may sign in again | `404 not_found`; `403 not_allowed` (an admin out of bounds) |
| `GET /api/admin/accounts` (organiser) | `?email=` | 200 `{"account": Account, "server_now"}` | `400 bad_request` without an email; `404 not_found` "Nobody with that email has an account."; anyone below organiser gets the same `403` whether or not the address exists |
| `POST /api/admin/people/import` | `{"text":"a@college.edu, b@college.edu","dry_run":true}`: addresses separated by commas, semicolons, spaces or new lines, at most 500 | 200 `{added, duplicates, invalid}`, each a list of addresses; with `dry_run` nothing is saved, so the page shows the counts first; the second call saves the same answer. Added people are students. A duplicate is an address already on the list or earlier in the paste (compared as sign-in compares) | `400 bad_request` (more than 500) |
| `POST /api/admin/people/remove` | `{"email"}` | 200 `{"deleted": true}` for someone who never signed in (the row goes); `{"deleted": false}` for someone who did: the account is disabled and every session ends, because their submissions point at it | `404 not_found`, `403 not_allowed` (an admin out of bounds), `409 conflict` (`PODIUM_OWNER_EMAIL`, yourself, the last owner) |
| `POST /api/admin/people/let-in` | `{"email"}` | 204: the address is on the list as a student (if it was not) and its refused row is gone | `400 bad_request`, `403 not_allowed` |
| `PUT /api/admin/site-settings` | `Organisation`, every field present (`""` clears one) | 200 the saved `Organisation` | `400 bad_request` with `fields` |
| `GET /api/admin/site-audit` | `?page=&action=&q=&since=&until=` | 200 `{entries, page, has_more, server_now}` (below) | |

`GET /api/admin/people` answers:

```json
{ "people": [ { "id": 1, "email": "owner@college.edu", "name": "Priya Raman", "role": "owner",
                "last_login_at": 1792183192359, "locked": true, "disabled": false } ],
  "list": [ { "id": 1, "email": "owner@college.edu", "name": "Priya Raman", "role": "owner",
              "last_login_at": 1792183192359, "disabled": false, "locked": true },
            { "id": 9, "email": "ravi@college.edu", "name": "", "role": "student",
              "last_login_at": null, "disabled": false, "locked": false } ],
  "counts": { "on_list": 2, "signed_in": 1, "never_came": 1 },
  "refused": [ { "email": "stranger@example.org", "count": 2, "first_at": 1792183000000, "last_at": 1792183100000 } ],
  "disabled": [ { "id": 7, "email": "asha@college.edu", "name": "Asha Rao", "role": "student",
                  "disabled": true, "last_login_at": 1792183100000 } ],
  "organisation": { "name": "", "contact_email": "", "phone": "", "privacy_contact": "" } }
```

`people` is everyone with the organiser, admin or owner role, owners first, then admins. `list` is everyone on the sign-in list, students included, by role then name; `counts` leaves disabled accounts out. `refused` is the refused addresses, newest first. `locked` is true for `PODIUM_OWNER_EMAIL`, whose owner role comes from the server configuration. `disabled` lists every disabled account, by email. The account lookup matches the address as sign-in stores it (trimmed, lower case) and never logs it.

`Organisation`, shown in the footer, on the sign-in page and on the privacy page, stored in the server's `settings` table and never in an environment variable:

| Field | Rule |
|---|---|
| `name` | at most 120 characters, one line |
| `contact_email` | an email address, at most 254 characters; shown as a `mailto:` link |
| `phone` | at most 32 characters of digits, spaces, `+`, `-`, `(`, `)` and `.`, with at least 6 digits; shown as a `tel:` link |
| `privacy_contact` | who to ask about personal data: a name or an address, at most 200 characters, one line |

Leading and trailing spaces are trimmed. A change writes one `settings_changed` audit row naming the fields that changed, never the values.

### The site audit

Every change to people, the sign-in list and the site, newest first, 100 per page. It opens from the Organisation tab of Settings.

```json
{ "entries": [ { "id": 31, "at": 1792311120000, "action": "role_granted",
                 "actor": { "user_id": 1, "email": "owner@college.edu", "role": "owner" },
                 "subject_email": "ta@college.edu", "details": { "role": "organiser", "from_role": "student" } } ],
  "page": 1, "has_more": false, "server_now": 1792311200000 }
```

- Filters: `action` keeps one action; `q` keeps rows whose actor or subject email contains the text (any case); `since` and `until` are Unix ms (inclusive, exclusive).
- `actor` is `null` for a change the server made itself (a change of run mode). The actor's email and role are as they were at the time.
- The table is append only: database triggers refuse every update and delete. It never holds an AI key, a session token, a cookie or an OAuth code; a test writes fake ones through every audited path and searches the table for them.

| `action` | `subject_email` | `details` |
|---|---|---|
| `role_granted` | the person | `{role, from_role}` |
| `role_revoked` | the person | `{from_role, sessions_ended}` |
| `person_added` | the person | `{}` |
| `list_imported` | `""` | `{added, duplicates, invalid}` (counts) |
| `person_removed` | the person | `{deleted, sessions_ended}` |
| `let_in` | the person | `{}` |
| `domain_added`, `domain_removed` | `""` | `{domain}`, `{domain, sessions_ended}`: only in rows from before v4 |
| `account_disabled` | the person | `{sessions_ended}` |
| `account_enabled` | the person | `{}` |
| `sessions_ended` | the person | `{count}` |
| `google_reset` | the person | `{sessions_ended}` |
| `mode_changed` | `""` | `{from, to, sessions_ended, demoted, disabled}`: the server started in another run mode; `disabled` counts the accounts that signed in only through testing mode (V4-15) |
| `settings_changed` | `""` | `{fields}`: the names of the site settings that changed |
| `user_forgotten` | `""` | `{user_id, placeholder, role}` and a count per thing erased: `podium-server forget-user`; the person is named by id only |

Show an unknown action by its name.

## Problems (organiser)

Every route here needs the organiser role. The pack format and the rules for tests, subtasks, checkers and generators are in `docs/AUTHORING.md`. An `{id}` that is not a problem is `404 not_found` on every route.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/problems` | `?archived=1` to include archived problems | 200 `{"problems":[ProblemSummary]}`, most recently changed first |
| `POST /api/admin/problems` | `{"title":"Two sum"}` | 201 `Problem` with a starter statement and C++ reference; the slug is made from the title (`two-sum`, then `two-sum-2` when taken) |
| `POST /api/admin/problems/import` | the pack as a zip body, at most 32 MB | 201 `Problem`; `warnings` names files in the pack podium ignores |
| `GET /api/admin/problems/{id}` | | 200 `Problem` |
| `PUT /api/admin/problems/{id}` | `ProblemEdit`, at most 512 KB | 200 `Problem` |
| `DELETE /api/admin/problems/{id}` | | 204: archives the problem, kept with its tests |
| `PUT /api/admin/problems/{id}/tests` | a zip of `NN.in` files, each with an optional `NN.out`; `?reason=` as for a save | 200 `Problem` |
| `GET /api/admin/problems/{id}/tests/{idx}/{part}` | `{part}` is `input` or `output` | 200 the file, `text/plain`, as an attachment `test-NN.in` or `test-NN.out`; 404 for an output not made yet |
| `GET /api/admin/problems/{id}/export` | | 200 the pack, `application/zip`, as `<slug>.zip` (format 1); outputs not made yet are left out |
| `POST /api/admin/problems/{id}/validate` | no body | 202 `Validation` |
| `GET /api/admin/problems/{id}/validation` | | 200 the latest `Validation`; 404 when never validated |
| `POST /api/admin/problems/{id}/generate` | `{"all":false}`, `{"all":true}` or `{"then_validate":true}` | 202 `Generation` |
| `GET /api/admin/problems/{id}/generation` | | 200 the latest `Generation`; 404 when never run |
| `POST /api/admin/problems/{id}/generate-inputs` | no body | 202 `InputGeneration` |
| `GET /api/admin/problems/{id}/input-generation` | | 200 the latest `InputGeneration`; 404 when never run |

### Shapes

`ProblemSummary`:

```json
{ "id": 3, "slug": "a-plus-b", "title": "A plus B", "difficulty": "easy", "tags": ["math"],
  "origin": "import", "archived": false, "test_count": 6, "points": 100,
  "validated": true, "validated_at": 1792183192359, "updated_at": 1792183100000,
  "shown_in_practice": false }
```

- `difficulty` is `easy`, `medium`, `hard` or `""`. `origin` is `manual`, `import` or `ai` (the AI wizard). A wizard draft carries the tag `ai draft` until a person changes its statement, reference or brute force.
- `validated` is true when the last passing validation covers the problem as it is now (its content hash equals the problem's); `validated_at` is `null` otherwise.
- `shown_in_practice` is true when a published contest that pins the problem has shown students its first failing hidden test in practice after its end. Such a contest keeps the problem (`PUT .../problems` refuses to drop it), so the mark stays.

`Problem` is `ProblemSummary` plus:

```json
{ "statement_md": "Read two integers...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": "tokens", "epsilon": 1e-6, "checker_source": "",
  "reference": { "language": "cpp17", "source": "#include <cstdio>..." },
  "brute": { "language": "python3", "source": "..." }, "validator": null,
  "generator": { "language": "python3", "source": "import random, sys..." },
  "plan": [ { "idx": 3, "subtask": 1, "seed": 3, "args": ["n=10"] } ],
  "samples": [1, 2],
  "subtasks": [ { "idx": 1, "points": 40, "title": "Small numbers", "tests": "3-4", "generator_args": ["n=10"] } ],
  "tests": [ { "idx": 1, "sample": true, "subtask_idx": null, "input_size": 8, "output_size": 3,
               "input_preview": "2\n1 2\n", "output_preview": "3\n", "output_missing": false } ],
  "content_hash": "9f2c...", "validated_hash": "9f2c...",
  "revision": { "id": 14, "rev": 3 }, "live_contests": [ { "contest_id": 4, "slug": "week-6", "title": "Week 6", "label": "B" } ],
  "problems": [], "warnings": ["No input validator: validation cannot check inputs against the constraints."] }
```

- `language` is `cpp17`, `java21`, `python3` or `node22`. `brute`, `validator` and `generator` are `null` when absent.
- There are two kinds of test: examples (`samples`, shown to students) and hidden tests, each in a subtask. The editor shows each test's role as Example, Subtask N or Not in a subtask, and saves the same `samples` and `subtasks[].tests` lists.
- `plan` lists the tests the generator makes: test `idx` of subtask `subtask`, made by running the generator with argv `seed` followed by `args`. `subtasks[].generator_args` are the arguments proposed for new planned tests of that subtask.
- `checker` is `tokens`, `exact`, `float` or `custom`. `epsilon` matters only for `float`; `checker_source` is the testlib checker for `custom`, `""` otherwise.
- `tests[].subtask_idx` is `null` for an example. Previews are the first 200 bytes, cut at a whole UTF-8 character. `output_missing` is true while a test has no expected output; Validate makes it first.
- `revision` is the problem's newest revision. `live_contests` are the published contests that pin the problem and are running now; while there are any, a save needs a `reason`.
- `problems` lists what stops validation and judging (a test in no subtask, a range naming a missing test, a test with no expected output); `warnings` is advice that does not (points not adding up to 100, no brute force, no validator, more than 10 hidden tests).

`ProblemEdit` replaces every editable field at once:

```json
{ "title": "A plus B", "slug": "a-plus-b", "statement_md": "...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "checker": "tokens", "epsilon": 1e-6, "checker_source": "",
  "reference": { "language": "cpp17", "source": "..." }, "brute": null, "validator": null,
  "tags": ["math"], "difficulty": "easy", "samples": [1, 2],
  "subtasks": [ { "points": 40, "title": "Small numbers", "tests": "3-4" } ], "reason": "" }
```

- A `brute` or `validator` with empty source counts as none. Subtasks are numbered in the order sent; each `tests` range list is stored in its shortest form (`"3, 4, 5"` becomes `"3-5"`).
- `generator` (`{language, source}` or `null`) and `plan` may be sent too. Leave both out to keep the generator, the plan and each subtask's `generator_args`, so an editor that does not show them cannot erase them. Send either to replace them: the one left out keeps its stored value. `null`, or empty source, removes the generator, and the plan must then be empty.
- `reason` (at most 500 characters) is required, `400` with `fields.reason`, when the save changes a problem that `live_contests` names; it is recorded on the new revision.
- A save that changes the statement, the reference or the brute force removes the `ai draft` tag and withdraws "validated". A change to the reference or the brute force also marks the problem as having a person's solution, which a contest needs before it takes an AI problem without a reason. The generator and the plan are not part of the content hash.

`Validation`:

```json
{ "id": 12, "state": "failed", "content_hash": "9f2c...", "current": true,
  "queued_at": 1792183192359, "finished_at": 1792183230000, "queue_position": null,
  "compile": [ { "role": "reference", "verdict": "OK", "output": "" } ],
  "tests": [ { "idx": 4, "role": "reference", "verdict": "WA", "time_ms": 3, "memory_kb": 1024,
               "message": "Token 1 differs: expected \"7\", found \"8\"." } ],
  "summary": { "passed": false, "reference_passed": 5, "reference_total": 6,
               "brute_agreed": 6, "brute_total": 6, "brute_unchecked": 0, "brute_unchecked_tests": [],
               "validator_passed": 6, "validator_total": 6, "max_time_ms": 3, "time_limit_ms": 1000,
               "reference_language": "cpp17", "time_factor": 1,
               "problems": ["Test 4: reference solution got wrong answer. Token 1 differs: expected \"7\", found \"8\"."],
               "warnings": [] },
  "error": null }
```

- `state`: `queued` (`queue_position` counts from 1), `running`, `passed`, `failed`, or `error` (the run could not finish; `error` says why and `summary` is `null`). `current` is false when the problem changed since this validation was asked for.
- `role` is `reference`, `brute`, `validator` or `checker` (only in `compile`). Compile verdicts are `OK` or `CE`; test verdicts are `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `IE` or `SK` (a brute force that ran out of time). `message` is a checker comment or the end of a validator's error output, at most 256 bytes.
- The server computes `summary` from the recorded results. `time_limit_ms` there is the problem's limit times the reference language's time factor. `brute_unchecked` counts the tests the brute force did not finish, so nothing cross-checked their output; when fewer than half of the tests were cross-checked, validation fails.

### Errors

| Route | Status and code | When |
|---|---|---|
| create, update | 400 `bad_request` with `fields` | a field is invalid. Keys are the JSON names, plus `subtasks.N.points`, `subtasks.N.title`, `subtasks.N.tests` and `subtasks.N.generator_args` (N from 0) |
| import, tests | 415 `bad_request` | `Content-Type` is not `application/zip` or `application/x-zip-compressed` |
| import, tests | 413 `too_large` | the zip is larger than 32 MB |
| import, tests | 400 `bad_pack` with `problems` | anything wrong with the archive or its contents |
| import | 409 `conflict` | a problem with the pack's slug exists |
| import, tests, export | 429 `busy` (`Retry-After: 5`) | another pack is being processed; one runs at a time |
| validate | 409 `not_ready` with `problems` | no tests, no subtask, tests outside the subtasks, or tests without an expected output |
| generate | 409 `not_ready` with `problems` | no tests, no reference, a generation still queued or running, or (with `all` false) no output missing; with `then_validate`, also anything else that stops validation. 400 when `all` and `then_validate` are both set |
| generate-inputs | 409 `not_ready` with `problems` | no generator, no planned tests, no reference, or an input or output generation still queued or running |

- Asking to validate again while a validation of the same content is queued or running returns that one. A queued validation of older content ends in `error`: "A newer version of the problem was sent for validation."
- A failing validation of exactly the validated content withdraws "validated" (for example after a judge upgrade made the reference slower).

### Output generation

`POST .../generate` runs the reference solution on the problem's inputs on the judge and makes what it prints the expected outputs. `{"all":false}` fills only missing outputs; `{"all":true}` replaces every output (the editor's "Regenerate all outputs" asks first); `{"then_validate":true}` is the editor's Validate on a problem with missing outputs: it fills them and, when all arrived, queues the validation (`summary.validating` is then true). The job uses the problem as stored.

`Generation`: `{id, state, all, queued_at, finished_at, results: [{idx, verdict, time_ms}], compile_output, summary: {applied, problems, validating?}, error}`. `state` is `queued`, `running`, `done` or `error`. A result `OK` means its output was uploaded. `summary.problems` says, one sentence each, what was not applied.

- One generation per problem at a time. The job records the content hash when queued; if the problem changed before it finished, nothing is applied. Outputs of the tests that passed are applied even when others failed. Applied outputs change the content hash, so the problem must be validated again.

### Input generation

`POST .../generate-inputs` runs the problem's generator on the judge once per planned test (argv: seed, then the test's args) and stores what it prints as that test's input. When every planned test got an input, output generation is queued, and when every output arrived, validation: one press runs the whole chain.

`InputGeneration`: `{id, problem_id, state, planned, queued_at, finished_at, results: [{idx, verdict, message?}], compile_output, summary: {applied, generating_outputs, problems}, error, server_now}`. `summary.applied` is 0 or `planned`: nothing is applied unless every planned test got an input. `message` is the end of the generator's error output.

- Inputs replace any test with the same number and clear its expected output until output generation fills it. The job records the content hash and a plan hash (generator plus plan) when queued; if either changed before it finished, nothing is applied.
- Generator limits: 5 s per test before the language factor, 256 MB, 16 MB of output per test.
- Each change of the job also reaches the organiser who queued it as an `input_generation` event.

## AI wizard (organiser)

The wizard drafts a problem from a brief or from pasted text, with Anthropic, OpenAI or Google Gemini. The routes are mounted when the server has the wizard's state (always in a normal build). Every answer carries `server_now`. A provider key is pasted per session and held in the server's memory only, for 2 hours, tied to the session and the provider: never stored, logged, sent to the judge or shown back. A session may hold one key per provider. Signing out, signing in again in the same browser, or a restart forgets them. Bodies of the key route are never logged.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/ai/providers` | | 200 `Providers` |
| `GET /api/admin/ai/models` | `?provider=openai`, and `&refresh=1` to list again instead of using the session's copy | 200 `Models` |
| `GET /api/admin/ai/key` | | 200 `KeyStatus` |
| `PUT /api/admin/ai/key` | `{"provider":"openai","key":"...","model":"gpt-..."}` | 200 `KeyStatus` |
| `DELETE /api/admin/ai/key` | `?provider=openai` forgets that provider's key; without it, every key of the session | 204 |
| `POST /api/admin/ai/wizard` | `WizardRequest`, at most 256 KB | 202 `{"wizard_id":"3f9c...","server_now":...}` |
| `GET /api/admin/ai/wizard/{id}` | | 200 `Wizard` |

`Providers`: `{providers: [{id, name, has_key, model, active_until}], active_provider, server_now}`. The order is fixed: `anthropic`, `openai`, `gemini`; a build made with the `fakeai` tag adds `fake` in testing mode, which needs no key and answers with canned problems. `model` is the model id saved with the key, `""` when none was chosen. `active_provider` is the one whose key or model was saved last, `null` without a key.

`Models`: `{provider, models: [{id, name}], listed_at, server_now}`: the provider's own list, filtered to models that can write a problem (Anthropic: structured outputs, adaptive thinking and effort `high`; OpenAI: `gpt-` and `o`-digit ids that are not audio, image, embedding or similar; Gemini: `generateContent` models that are not embedding, image, audio, live or Gemma). The list is a hint: the key form takes any model id the provider's own lookup finds.

`PUT /api/admin/ai/key`: `provider` is required. `key` is required unless the session already holds one for the provider; left out, the held key stays and only the model changes. `model` is at most 128 characters of letters, digits and `. _ - :` (a leading `models/` is dropped). Empty means "no model yet": the key is checked by listing models. With a model, the key and the model are checked together by the provider's model lookup. Neither check costs tokens. A successful save makes this provider the active one.

`KeyStatus`: `{active, provider, model, active_until, providers: [{id, name, has_key, model, active_until, models}], server_now}`; without a key, `active` is false and `provider` is `anthropic`.

`WizardRequest`: `{"mode":"brief","brief":{"topic","difficulty","constraints","subtasks"}}` or `{"mode":"paste","text":"..."}`. `difficulty` is `easy`, `medium` or `hard`; `topic` is required; each brief field is at most 2,000 characters and `text` at most 20,000. An optional `provider` picks which of the session's keys to use.

`Wizard` (the organiser's own runs, kept in memory for an hour after the end): `{id, kind: "generate", state, problem_id?, provider, model, started_at, server_now, error?: {code, message, reasons}}`. `state` is `thinking`, then `draft_created` or `error`. On success the draft is a new problem (origin `ai`, tag `ai draft`) and input generation is queued for it at once. The same object reaches the organiser as a `wizard` event when the run ends. The call runs in the background, at most 3 minutes with its one retry.

| Status and code | When |
|---|---|
| 400 `bad_request` | an unknown provider, mode or difficulty; an empty topic or text; a field over its limit; a malformed model id; no key when the session holds none for the provider |
| 400 `bad_key` | the key check failed; `message` is a sentence for people, never the key or the provider's raw answer |
| 409 `no_key` | the session holds no key for the provider |
| 409 `no_model` | the key was saved without a model |
| 429 `busy` (`Retry-After: 10`) | "One AI request at a time. Wait for the current one to finish." |
| 502 `provider_error` | the provider refused the model listing |
| 404 `not_found` | another organiser's run, an unknown id, or a run that ended over an hour ago or before a restart |

A run's `error.code` is one of `key_refused`, `no_quota`, `no_access`, `model_not_found`, `unsupported_model`, `rate_limited`, `unavailable`, `bad_request`, `refused` (the model declined), `cut_off` (too long, after one shorter retry), `invalid_answer` (with `reasons`, after one retry with them), `timeout`, `canceled`, `input`, `not_found` or `internal`. The messages name the provider and say what to do, for example "OpenAI refused the key. Check it and paste it again."

## Workspace, runs and submissions

The workspace is where code is written, run and submitted. Under `/api/workspace/` only organisers reach it, to try a problem as a student would; submissions there count in no contest. Students reach the same shapes under a contest ("Contests: students").

| Route | Request | Success |
|---|---|---|
| `GET /api/workspace/{problem}` (organiser) | | 200 `Workspace` |
| `PUT /api/workspace/{problem}/draft` (organiser) | `{"language":"cpp17","source":"..."}` | 204 |
| `POST /api/workspace/{problem}/run` (organiser) | `{"language","source","input"}`; leave out `input` to run the examples | 202 `Run` |
| `POST /api/workspace/{problem}/submit` (organiser) | `{"language","source","client_key","typed_chars","pasted_chars","focus_losses","fullscreen_exits"}` | 202 `Submission`; 200 with the first submission for a repeated `client_key` |
| `POST /api/workspace/{problem}/events` (organiser) | `{"events":[{"kind":"paste","size":120,"at":1792183192359}]}` | 204 |
| `GET /api/runs/{id}` (student) | | 200 `Run` |
| `GET /api/submissions/{id}` (student) | | 200 `Submission` |

- `{problem}` is a problem id. Run and submission reads are for the author and for admins. A missing id, a run asked for on the submissions route (or the reverse), and someone else's run or submission all give `404 not_found`, so ids reveal nothing.
- Run, submit and draft bodies may be up to 160 KB: code at most 64 KB and custom input at most 64 KB, both valid UTF-8. `language` is `cpp17`, `java21`, `python3` or `node22`.

### `Workspace`

```json
{ "problem_id": 3, "title": "A plus B", "statement_md": "...", "time_limit_ms": 1000, "memory_limit_mb": 256,
  "languages": [ { "id": "cpp17", "name": "C++17", "time_factor": 1 }, { "id": "java21", "name": "Java 21", "time_factor": 2 },
                 { "id": "python3", "name": "Python 3", "time_factor": 3 }, { "id": "node22", "name": "JavaScript (Node 22)", "time_factor": 2 } ],
  "samples": [ { "idx": 1, "input": "2\n1 2\n", "output": "3\n", "truncated": false } ],
  "draft": { "language": "cpp17", "source": "...", "updated_at": 1792183192359 },
  "submissions": [], "server_now": 1792183192400 }
```

- `samples` are the examples, each input and output at most 4 KB cut at a whole UTF-8 character (`truncated` says so). An example whose output is not made yet is left out.
- `time_factor` multiplies `time_limit_ms` for that language on the judge, so the editor shows the limit a program really gets.
- `draft` is `null` when nothing was saved. `submissions` is the viewer's newest 20 on this problem, newest first. Starter code lives in the web app.

### `Run`

```json
{ "id": 77, "state": "done", "compile_output": "",
  "results": [ { "kind": "sample", "label": "Example 1", "verdict": "AC", "time_ms": 10, "memory_kb": 3120,
                 "output": "3\n", "output_truncated": false, "expected": "3\n", "message": "" } ],
  "time_step_ms": 10, "server_now": 1792183192400 }
```

| Field | Meaning |
|---|---|
| `state` | `queued`, `running`, `done` or `error`; on `error`, `error` holds a sentence |
| `results` | one row per input from the start: every example in order ("Example 1", "Example 2", ...), or one "Your input" (`kind` `sample` or `custom`). `verdict` is `""` until that input has run |
| `verdict` | for an example, the checker's verdict (`AC`, `WA`) or a limit verdict (`TLE`, `MLE`, `RE`, `OLE`); for your own input, `OK` when the program ran without error, otherwise the limit verdict |
| `output`, `output_truncated` | the start of what the program printed, at most 4 KB |
| `stderr` | the end of what the program printed to stderr, at most 4 KB; absent when empty. Runs only: a submission never carries it, since a program could print a hidden input there |
| `expected` | the example's expected output, at most 4 KB; `null` for your own input |
| `message` | the checker's comment on an example |
| `revealed` | present only when an example failed: the first one, with `idx`, `kind`, `label`, `input`, `expected`, `output`, `stderr` and `truncated` |

Times are rounded to 10 ms, or 100 ms for a student in a contest; `time_step_ms` says which, so a 0 reads as "under 5 ms" or "under 50 ms". A run older than 30 days keeps its verdicts but not its inputs or output (`results` is `[]`). Running the examples of a problem without examples is `400 bad_request`.

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
  "hidden": false, "time_step_ms": 100, "server_now": 1792183195100 }
```

| Field | Meaning |
|---|---|
| `state` | `queued` (`queue_position` counts from 1), `running`, `done`, or `error` (the judge could not finish, or the checker does not compile; `error` holds the sentence, and the submission is not counted) |
| `verdict` | `""` while judging, then `AC` when every test passed, `CE` when the code did not compile, otherwise the first failing test's verdict in judging order |
| `score` | the points of the subtasks passed in full; while judging, the score so far |
| `subtasks[].verdict` | `AC`, the first failing test's verdict, `SK` when its tests were skipped, `CE`, or `""` while pending |
| test verdicts | `AC`, `WA`, `TLE`, `MLE`, `RE`, `OLE`, `SK` (not run: an earlier test failed), `IE` (the judge never reported it), `""` while pending |
| `hidden` | false; true only as a fail-safe when the server cannot read the submission's contest, and then the submission shows only that it was received |
| `revealed` | practice only, below |

A submission that counts never carries the input, output or checker comment of any test. What a student sees of their own submission is the same on every path (this route, the workspace list, the answer to a submit and the live events); organisers see every figure:

- no memory figure for hidden tests (`memory_kb` 0), `max_memory_kb` rounded up to the next 16 MB, times to 100 ms in a contest and 10 ms elsewhere;
- while judging, per-test verdicts only inside finished blocks (the examples as a whole, each whole subtask), and `max_time_ms` and `max_memory_kb` stay 0 until the end, so polling shows no more than the live stream.

**The revealed hidden test.** A practice submission carries `revealed` for its first failing hidden test, in judging order: `{idx, subtask_idx, input, expected, output, truncated}`, each text at most 4 KB. It is present only when the submission was made in practice after the contest's end (and no other contest that pins the problem is still open, drafts and upcoming ones included) or in the organisers' `/api/workspace/`; a hidden test failed; judging is done; and the viewer is the author or an admin. It is never on a submission that counts, and never in the console feed, the exports, the standings or any event but the author's own. A problem used in a contest with practice therefore has its hidden tests readable one at a time; `docs/AUTHORING.md` says what that means for setters.

### Submit and run rules

| Rule | Value | Refusal |
|---|---|---|
| Pending submissions | one per user per problem | `429 rate_limited` "Your last submission for this problem is still being judged. Wait for its result." |
| Gap between submits | per user and problem: 5 s when an organiser tries a problem, the contest's `min_gap_s` in a contest | `429 rate_limited` "Submissions are limited to one every 30 seconds. Try again in 12 s." |
| Submissions per problem | the contest's `max_per_problem`; judge failures do not count | `400 bad_request` "You have used all 15 submissions for this problem." |
| Pending runs | one per user | `429 rate_limited` "Your last run is still going. Wait for it to finish." |
| Gap between runs | 2 s per user | `429 rate_limited` |
| Compile that ran out of time | 60 s before the next Run or Submit in the same scope | `429 rate_limited` "Your last program took too long to compile. Try again in N seconds; ..." |
| Drafts | one save per user every 2 s | `429 rate_limited` |
| Code and input | 64 KB each, UTF-8, a known language (in a contest, one it allows) | `400 bad_request` |
| `client_key` | optional, at most 64 characters | `400 bad_request` |
| Problem | tests, examples and subtasks fit together and every test has an expected output | `409 not_ready` with `problems` |

- `client_key` is a random value the browser makes per press of Submit and reuses on a retry. Keys are per user, problem and contest. The same key again returns the first submission with `200` and counts nothing, so a retry after a timeout never trips the rate limit.
- `typed_chars`, `pasted_chars`, `focus_losses` and `fullscreen_exits` are the editor's counts since the last submission, clamped to 0 to 10,000,000. They never block a submit.
- The server stores with each submission an HMAC of the client's address and user agent, keyed by `PODIUM_SESSION_SECRET`, so it cannot be reversed by trying campus addresses.

### Editor events

`POST .../events` records what the editor saw: `kind` is `paste` (with `size`, the pasted length), `blur` or `focus` (window focus lost and back), or `fullscreen_exit` (in a contest with full screen on). Never keystrokes or text. At most 100 events per request and one request per user every 5 s; at most 5,000 events per user and problem, beyond which new ones are dropped. `at` is the browser's estimate of server time; the server keeps it when it lies in the last 10 minutes and uses its own clock otherwise.

| Status | When |
|---|---|
| 204 | stored; also when the cap is reached, and in a contest outside the student's own time, when nothing is recorded |
| 400 `bad_request` | an unknown `kind`, or more than 100 events |
| 404 `not_found` | no such problem |
| 429 `rate_limited` | more than one batch per user in 5 s |

## Live updates

### `GET /api/events` (student)

A `text/event-stream` for the signed-in user, read with `EventSource`; `Cache-Control: no-store` and `X-Accel-Buffering: no`. Optional `?device=<id>`, a random id of 1 to 64 letters and digits the browser keeps: on a phone or tablet it tells two devices of one model apart for the second-session signal; a desktop is told apart by its address and ignores it.

| Event | Data | When |
|---|---|---|
| `clock` | `{"server_now":1792183192400}` | on connecting, then every 25 s (it also keeps proxies from closing an idle stream) |
| `submission` | a `Submission` | one of the user's submissions changed in a way the user can see |
| `run` | a `Run` | one of the user's runs changed |
| `standings` | `{"contest","contest_id","version"}` | a contest's board changed and the new one is ready (at most about once every 2 s per contest); fetch it again |
| `announcement` | `{"contest","contest_id","id","body_md"}` | an organiser posted an announcement |
| `statement` | `{"contest","contest_id","label","revision_id","statement_changed","pinned_at"}` | an organiser moved a problem of a published contest to a newer revision ("Update students now"); fetch the contest again |
| `clarification` | `{"contest","contest_id","id"}` | a question was answered (to the asker, or everyone when public) or asked (to organisers) |
| `contest` | `{"contest","contest_id","ends_at"}` | the end moved: the whole contest was extended, or this student got extra time (`ends_at` is then their own) |
| `wizard` | a `Wizard` | an AI wizard run of this organiser ended |
| `input_generation` | an `InputGeneration` | a generate inputs job this organiser queued changed |
| `replaced` | `{}` | sent just before the server closes this stream because the user opened a fifth one; do not reconnect |

```
event: clock
data: {"server_now":1792183192400}
```

- Events for one run or submission arrive in order, and only when something visible changed: for an organiser each test, for a student the examples finishing and each whole subtask finishing.
- Contest events go to organisers and owners with a stream open, and for a published contest to every signed-in student; the list is read when each event is sent.
- On every `clock` tick the server checks the session again; a stream whose session ended is closed.
- Each user may hold 4 streams; a fifth replaces the oldest, which gets `replaced`. The web app shares one stream between the pages of a tab.
- During a contest, a student opening a stream from another browser or machine while one is open is recorded for the organisers as a second session. A phone or tablet that changes network stays the same device. Nothing is shown to the student.
- The server holds at most 5,000 streams; beyond that `503 stream_limit` with `Retry-After: 30`, and the page polls. A stream 16 events behind, or that does not accept a write within 10 s, is closed and the browser reconnects. `501 unsupported` when the server cannot stream.

## Contests: students

Every route needs a signed-in user. A published, unarchived contest is open to everyone who can sign in (the list of people decides who takes part); anything else is `404 not_found`. Organisers and owners always get in and are never on the board. Every answer carries `server_now`.

| Route | Request | Success |
|---|---|---|
| `GET /api/contests` | | 200 `{live:[ContestSummary], upcoming:[...], past:[...], server_now}` |
| `GET /api/contests/{slug}` | | 200 `Contest` |
| `POST /api/contests/{slug}/start` | `{"accept_honour":true}` | 200 `Contest` |
| `GET /api/contests/{slug}/problems/{label}` | | 200 contest `Workspace` |
| `PUT /api/contests/{slug}/problems/{label}/draft` | as the workspace | 204 |
| `POST /api/contests/{slug}/problems/{label}/run` | as the workspace | 202 `Run` |
| `POST /api/contests/{slug}/problems/{label}/submit` | as the workspace | 202 `Submission`; 200 for a repeated `client_key` |
| `POST /api/contests/{slug}/problems/{label}/events` | as the workspace | 204 |
| `GET /api/contests/{slug}/standings` | `?as=student` for an organiser | 200 `Standings`: a student gets the board for their state; an organiser the full board, or with `?as=student` the board students get now |
| `POST /api/contests/{slug}/clarifications` | `{"problem_label":"A" or null,"question":"..."}` | 201 `{clarification, server_now}` |

### `ContestSummary` and `Contest`

```json
{ "slug": "week-6", "title": "Week 6", "starts_at": 1792310400000, "ends_at": 1792317600000,
  "problem_count": 3, "state": "live", "my_score": 140,
  "my_window": { "started_at": 1792310400000, "ends_at": 1792317600000 } }
```

| Field | Meaning |
|---|---|
| `ends_at` | when the contest closes for everyone: its end plus any contest-wide extension |
| `state` | for this student: `upcoming`, `live`, `frozen` (during the freeze), `ended`, or `practice` (ended, practice open). Extra time keeps it `live` for them |
| `my_score` | their total from their own submissions, never frozen; `null` before they accept the rules |
| `my_window` | their own time: the contest's start to its close plus their extra time |

`Contest` adds:

```json
{ "description_md": "...", "honour_text": "This contest counts. ...", "accepted": true, "organiser": false,
  "rules": { "min_gap_s": 30, "max_per_problem": 15, "languages": ["cpp17", "python3"], "freeze_minutes": 30,
             "practice_after_end": true, "full_screen": false, "count_focus": false, "block_paste": false, "block_copy": false },
  "problems": [ { "label": "A", "title": "A plus B", "my_score": 40, "max_score": 100, "my_submission_count": 3, "revision_id": 14 } ],
  "announcements": [ { "id": 3, "body_md": "...", "created_at": 1792311000000 } ],
  "my_clarifications": [], "public_clarifications": [], "editorial_md": null, "server_now": 1792311000000 }
```

- `problems` is empty until the student has accepted the rules and the contest has started; after their time is over it lists the problems again. `revision_id` is the pinned revision students are served.
- `rules` carries the four exam options, so the workspace knows which measures are on and says so before Start and in Help.
- `honour_text` is the contest's own honour code, or the default.
- `my_submission_count` counts the student's submissions as they are made, practice and judge failures excluded.
- `editorial_md` is set only when the editorial is published and every student's time is over.
- `Clarification`: `{id, problem_label, question, answer, public, created_at, answered_at}`. Students never see who asked a public question.

### Start

`POST .../start` records acceptance of the honour code; pressing it again changes nothing. Refusals are `400 bad_request` with `fields.accept_honour`: not accepted ("Read the rules and agree to them to start."), "The contest has not started yet.", or "The contest has ended."

### Contest workspace

The `Workspace` plus `{label, contest, window, can_submit, practice, refusal, revision_id}`.

- The title, statement, limits and examples are those of the revision the contest pins, never the problem's working copy, so an organiser's edit reaches students only through "Update students now". `languages` lists only the contest's languages.
- Submissions count while the student's own time is open. After it they are practice (judged at a lower priority, never on the board) when the contest allows practice and every student's time is over; otherwise run and submit are refused.
- `can_submit` and `refusal` say whether run and submit work now, and why not. An organiser can always run and submit, with a 5 s gap and the student view of the results.

| Refusal | When |
|---|---|
| `403 not_started` "The contest has not started yet. The contest page shows when it starts." | before the start (also for reading the problem) |
| `403 not_started` "Accept the rules on the contest page to start." | after the start, rules not accepted (also for reading the problem) |
| `403 contest_over` "The contest has ended." | the student's time is over and practice is off |
| `403 contest_over` "Your time is up. Practice opens when every student's time is up." | the student's time is over, but others still have extra time |
| `409 not_ready` | the problem changed since it was added to the contest; an organiser validates it and adds it again |
| `429 rate_limited`, `400 bad_request` | the submit rules above |

### `Standings`

```json
{ "variant": "frozen", "version": 12, "frozen_at": 1792315800000, "final": false, "judging_remaining": 2,
  "labels": ["A", "B"],
  "rows": [ { "rank": 1, "user_id": 7, "name": "Asha R", "total": 140, "last_improvement_s": 2400,
              "cells": [ { "label": "A", "score": 100, "max": 100, "pending": 0, "tried": true },
                         { "label": "B", "score": 40, "max": 100, "pending": 1, "tried": true } ] } ],
  "me": 7, "server_now": 1792316000000 }
```

- Rank by `total`, then by `last_improvement_s` (seconds from the start to when the student reached their total); equal totals and times share a rank.
- `pending` counts submissions made after the freeze that the frozen board does not show yet.
- `name` is the account name, else "Student {id}"; never an email on a student's board. A student excluded by a similar-code decision is not on the board.
- `final` is true on the full board once every student's time is over and nothing waits for the judge.
- A student gets the full board until the freeze, then the frozen board until every student's time is over and, with `hold_reveal`, until an organiser reveals; then the full board.

### Clarifications

At most 5 unanswered questions per student per contest and 2,000 characters each (`400` with `fields.question`); `problem_label` must be a label of the contest or null (`fields.problem_label`). Questions close when the student's time is over, unless practice is open (`403 contest_over`).

## Contests: organisers

Every route here needs the organiser role; a student gets `403 forbidden` before any contest is read. An `{id}` that is not a contest is `404 not_found`, and every id in a URL or body (a submission, a question, a label) is checked against the contest in the URL. Every change that can affect results writes a row to the contest's audit log in the same transaction.

| Route | Request | Success |
|---|---|---|
| `GET /api/admin/contests` | `?archived=1` to include archived contests | 200 `{contests:[AdminContest], server_now}`; list rows leave out `extensions` and `queue` and send `unanswered` as 0, so the list costs two queries |
| `POST /api/admin/contests` | `ContestEdit` | 201 `AdminContest`, unpublished |
| `GET /api/admin/contests/{id}` | | 200 `AdminContest` |
| `PUT /api/admin/contests/{id}` | `ContestEdit`; fields left out keep their value | 200 `AdminContest` |
| `DELETE /api/admin/contests/{id}` | | 204: archived, nothing deleted |
| `POST /api/admin/contests/{id}/clone` | `{title, starts_at, ends_at}` (`title` empty keeps the old one; `slug` is accepted and ignored) | 201 `AdminContest`: settings and problems copied, not extensions or the editorial |
| `PUT /api/admin/contests/{id}/problems` | `[{"problem_id":3,"label":"A","ai_reason":"..."}]` | 200 `AdminContest` (rules below) |
| `PUT /api/admin/contests/{id}/problems/{label}/revision` | `{"revision_id":14}` | 200 `{contest: AdminContest, announcement_md, statement_changed, rejudge_needed, server_now}`: "Update students now" |
| `PUT /api/admin/contests/{id}/extensions/{user}` | `{"minutes":15,"reason":"..."}`, 0 to 1440; 0 removes | 200 `AdminContest`; the student gets a `contest` event |
| `POST /api/admin/contests/{id}/extend` | `{"minutes":10}`, 1 to 240, added to earlier extensions | 200 `AdminContest`; everyone gets a `contest` event |
| `POST /api/admin/contests/{id}/pause` | | 200 `AdminContest` |
| `POST /api/admin/contests/{id}/resume` | | 200 `AdminContest` |
| `GET /api/admin/contests/{id}/submissions` | `?problem=A&user=7&verdict=WA&page=1` | 200 `{submissions:[FeedRow], page, has_more, server_now}` |
| `POST /api/admin/contests/{id}/rejudge` | `{"problem_label":"A"}` or `{"submission_id":80}` | 202 `{queued, revisions:[{label, revision_id, rev}], server_now}` |
| `GET /api/admin/contests/{id}/announcements` | | 200 `{announcements, server_now}` |
| `POST /api/admin/contests/{id}/announcements` | `{"body_md":"..."}`, at most 4,000 characters | 201 `Announcement` `{id, body_md, created_at}` |
| `GET /api/admin/contests/{id}/clarifications` | | 200 `{clarifications, server_now}`, each with `asked_by {user_id, email, name}` |
| `PUT /api/admin/contests/{id}/clarifications/{cid}` | `{"answer":"...","public":false}`, at most 4,000 characters | 200 `Clarification` |
| `GET /api/admin/contests/{id}/standings` | `?variant=frozen` for the frozen board | 200 `Standings`, full unless asked; each row also carries the student's `email`, so two students with one name can be told apart |
| `GET /api/admin/contests/{id}/reveal` | | 200 `{frozen: Standings, full: Standings, revealed_at, server_now}` |
| `POST /api/admin/contests/{id}/reveal` | | 200 `AdminContest`: students held on the frozen board see the full one |
| `GET /api/admin/contests/{id}/export.csv` | `?tz=Asia/Kolkata` | 200 `text/csv` attachment `<slug>-results.csv` |
| `GET /api/admin/contests/{id}/export.json` | | 200 JSON attachment `<slug>-results.json` |
| `GET /api/admin/contests/{id}/stats` | | 200 `{problems:[ProblemStats], started, server_now}`; allowed while the contest is live |
| `PUT /api/admin/contests/{id}/editorial` | `{"editorial_md":"...","published":true}`, at most 64 KB | 200 `AdminContest` |
| `GET /api/admin/contests/{id}/audit` | `?page=1&action=&actor=` | 200 `{entries:[AuditEntry], page, has_more, server_now}`, newest first, 200 per page |

### `ContestEdit` and `AdminContest`

`ContestEdit` holds the form's fields: `title`, `description_md`, `starts_at`, `ends_at`, `freeze_minutes`, `published`, `practice_after_end`, `min_gap_s` (0 to 3600), `max_per_problem` (1 to 1000), `languages`, `honour_text` (empty for the default), `hold_reveal`, and the four exam options, each false unless set:

| Option | What it does |
|---|---|
| `full_screen` | asks for full screen when the student starts and counts each exit |
| `count_focus` | shows organisers the tab switches and focus losses, which are recorded in every contest |
| `block_paste` | the editor refuses pastes |
| `block_copy` | the statement refuses copies |

The measures apply only during a student's own contest time. Blocking happens in the browser; the server records and counts.

- The address (`slug`) is made from the title when the contest is created, numbered when taken (`week-6-2`), and never changes, so links keep working. `slug` is accepted in the body and ignored.
- Defaults on create: practice after the end, a 30 s gap, 15 submissions per problem, all four languages, no freeze, no exam options.
- Field rules: a title of 1 to 120 characters; an end after the start and at most 14 days later; a freeze shorter than the contest; at least one language from the list; a description up to 32 KB and an honour code up to 8 KB. Invalid fields are `400 bad_request` with `fields`.
- Publishing needs at least one problem, and every problem must still be the validated version that was added ("Problem B changed or lost its validation since it was added. Validate it and add it again.").

`AdminContest` is every `ContestEdit` field plus `id`, `slug`, `close_at` (the end plus the contest-wide extension), `default_honour_text`, `editorial_md`, `editorial_published`, `paused`, `extended_minutes`, `revealed_at`, `archived`, `state`, `created_at`, `updated_at`, `server_now`, and:

- `problems`: `[{label, problem_id, title, pinned_hash, ready, max_score, revision_id, rev, latest_revision_id, latest_rev, update_available, update_statement_only, reconstructed}]`. `ready` is false when the problem changed since it was added. `update_available` is true when the problem has a newer validated revision than the pin, and `update_statement_only` when that revision changes only the statement. `reconstructed` is always false on a v3 database.
- `extensions`: `[{user_id, email, name, minutes, reason}]`. The reason can be health data and goes only to admins.
- Queue health, what the console's opening sentence and banners read:

| Field | Meaning |
|---|---|
| `queue.waiting` | this contest's Submit and Run jobs waiting for the judge, including those held by a pause |
| `queue.judging` | this contest's jobs a judge is running now |
| `queue.errors` | this contest's submissions that ended in a judge error and have not been judged again |
| `queue.oldest_wait_s` | seconds the oldest waiting job has waited; 0 when nothing waits |
| `judge_last_claim_at` | when any judge last asked for work, whether or not it got any; `null` when none has since the server started. A judge asks every few seconds, so a value more than a minute old means no judge is working |
| `unanswered` | this contest's questions with no answer yet |

### Choosing problems

`PUT .../problems` replaces the contest's problems: at most 26, each label a capital letter used once, each problem once. Each problem must exist, be validated and not be tagged `ai draft` (a person reads and saves an AI draft first); its content hash and subtask points are pinned. A problem whose origin is `ai` and for which no person has written a solution is `409 ai_unconfirmed` with the labels in `problems`, unless `ai_reason` (at least 10 characters) gives the organiser's reason to grade it as written; the reason goes to the audit log and is not asked again for this contest. Once a published contest has shown students its hidden tests in practice, a change that drops one of its problems is `400` with `fields.problems`. Errors name the label, never the problem's title.

### Update students now

`PUT .../problems/{label}/revision` moves the contest's pin of a label to the problem's newest validated revision, after an organiser edited a problem in a live contest.

- The pin and its subtask points change in one transaction with a `revision` audit row. Students of a published contest get a `statement` event and their next load serves the new statement.
- `announcement_md` is a prefilled announcement for the console to offer; it is not posted. With `rejudge_needed` (the tests, limits or checker changed) it says submissions will be judged again, and the console offers Rejudge.
- Errors: `404 not_found` (no such label, or the revision is not this problem's); `409 conflict` (the contest already uses this revision, or someone else moved the pin a moment ago); `409 not_ready` (the revision is not the validated version).

### Running a contest

- **Pause** stops the judge from taking the contest's submissions and runs; they still queue and nothing is lost. **Resume** lets them through in order.
- **Extend** answers `409 contest_ended` once the contest has closed: extending then would reopen it for every student. **Extra time** (`extensions/{user}`) answers `409 not_participant` unless the student has started or submitted; removing one with 0 minutes always works. An unknown user is `404`.
- **Rejudge** queues every submission of a problem, or one submission, again at priority 50 against the pinned revision; each goes back to `queued` and counts again once judged. Submissions still waiting on the old tests are included: their old job is cancelled and replaced; one already waiting on the current tests is left alone, so pressing Rejudge twice adds nothing. Both fields, or neither, is `400`; a label or submission not in the contest is `404`; a problem that no longer matches its pin is `409 not_ready`.
- **Feed** rows (`FeedRow`): `{id, user_id, name, email, problem_label, language, state, verdict, score, max_score, created_at, judged_at, practice, rejudged, typed_chars, pasted_chars, focus_losses, fullscreen_exits}`, newest first, 50 per page. `verdict` filters by a verdict code (`AC`, `WA`, `TLE`, `MLE`, `RE`, `CE`), `pending` (queued or running) or `error` (a judge error), in any case. An unknown `problem` label is `404`; a `user` that is not an id is `400`.
- **Reveal** answers `409 too_early` "The reveal opens when every student's time is up." until the close plus the longest extra time. The web app asks once, then shows students the full board.

### Export and stats

- CSV: `?tz=` (an IANA zone, optional) sets the zone the start times are written in; the web app sends the browser's, and a missing or unknown zone takes the server's own. Columns: `rank, name, email, started (<zone>)` (local time, `2026-10-28 20:12:05`), `total`, then one column per problem label, with `-` for a problem not tried. A cell starting with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading `'`, so a spreadsheet never runs a name as a formula.
- JSON: `{contest: AdminContest, standings: [Standings row plus email, started_at], submissions: [{id, user_id, problem_label, language, state, verdict, score, created_at, practice}], audit: [AuditEntry], exported_at}`. No code and no similar-code data.
- `ProblemStats`: `{label, title, max_score, attempted, solved, subtasks: [{idx, points, earned}], histogram: [{score, students}], median_attempts}`, over the students on the board and their submissions that count. `started` counts the students taking part, as the board does.
- Students excluded by a similar-code decision leave the board, the CSV and the JSON standings.

### `AuditEntry`

`{id, action, actor: {user_id, email, role}, details, created_at}`. The actor's email and role are as they were at the time. The table is append only (database triggers refuse every update and delete), and the JSON export carries the whole log, oldest first. `action` filters by one action; `actor` keeps rows whose actor email contains the text.

| `action` | `details` |
|---|---|
| `settings` | `{"changes": {"min_gap_s": {"from": 30, "to": 60}, ...}}`, times in Unix ms; the description and honour code are recorded as their sizes |
| `problems` | `{"from": [{label, problem_id, pinned_hash, revision_id, rev}], "to": [...]}` |
| `ai_problem` | `{label, problem_id, reason}`: an organiser's reason to grade an AI problem as written |
| `revision` | "Update students now": `{label, problem_id, from_revision_id, from_rev, to_revision_id, to_rev, statement_changed, judging_changed}` |
| `extension` | `{user_id, from_minutes, minutes, reason_given}` |
| `extend` | `{minutes, extended_minutes}` |
| `pause`, `resume`, `reveal`, `archive` | `{}` |
| `rejudge` | `{problem_label}` or `{submission_id}`, with `queued` and `revisions` |
| `editorial` | `{published, was_published, changed, bytes}`, when it is published or unpublished, or changed while published |
| `integrity_decision` | `{user_id, decision}`; the reason stays with the decision |

Show an unknown action by its name.

## Similar code (organiser)

After a contest an organiser runs a check for copied code, reads the ranked signals and the side-by-side view of a pair, downloads the similar pairs as CSV, and records one decision per student with a reason. Signals are reasons to look, not proof. The reviewer's guide is `docs/INTEGRITY.md`. Every route needs the organiser role, every answer carries `server_now`, and a signal, student or decision from another contest is a `404`. A reviewer never sees a signal, pair, timeline, count row or decision about themselves (a student since made an organiser). The read routes share the per-user read limit.

| Route | Request | Success |
|---|---|---|
| `POST /api/admin/contests/{id}/integrity/run` | no body | 202 `{run_id, server_now}`; the check runs in the background |
| `GET /api/admin/contests/{id}/integrity` | | 200 `Run` plus `users`, `problems` and `server_now`; `404` "This contest has not been checked yet." |
| `GET /api/admin/contests/{id}/integrity/pairs.csv` | | 200 `text/csv` attachment `<slug>-similar-pairs.csv`: one row per `similar` signal of the latest check, `problem, strength, score, name_a, email_a, submission_a, name_b, email_b, submission_b, checked_at` (UTC, RFC 3339) |
| `GET /api/admin/contests/{id}/integrity/signals/{signal}/pair` | | 200 `{a: Side, b: Side, server_now}` for a `similar` signal; any other kind is `404` |
| `GET /api/admin/contests/{id}/integrity/timeline/{user}` | | 200 `{moments: [Moment], server_now}`; `404` when the user neither accepted the rules nor submitted in this contest |
| `GET /api/admin/contests/{id}/integrity/counts` | | 200 `{counts: [{user_id, name, pastes, pasted_chars, focus_losses, fullscreen_exits}], server_now}`: what the editor recorded per student over the contest, most events first. Allowed at any time; the page shows it when an exam option is on |
| `GET /api/admin/contests/{id}/integrity/cases` | | 200 `{cases: [Case], server_now}`, newest first |
| `PUT /api/admin/contests/{id}/integrity/cases/{user}` | `{"decision":"warning","reason":"..."}` | 200 `{case: Case, server_now}`; replaces an earlier decision about the student |

### Shapes

- `Run`: `{id, state, started_at, finished_at, error?, stats: {students, submissions, problems, signals: {kind: n}, clusters}, signals: [Signal]}`. `state` is `running`, `done` or `error`; `error` is a sentence for people, and the cause is in the server log. Signals come strongest first. A new check replaces the contest's previous signals.
- `Signal`: `{id, problem_id, kind, strength, score, user_a, user_b?, submission_a?, submission_b?, evidence}`. `kind` is `similar`, `wrong_output`, `hardcoded` or `probing`; `strength` is `strong`, `moderate` or `weak`, with the raw `score` beside it (containment for `similar`, the number of tests for `wrong_output` and `hardcoded`, distinct results for `probing`). `evidence`: similar `{shared, spans_a, spans_b, language_a, language_b}`; wrong_output and hardcoded `{tests}`; probing `{distinct, attempts}`.
- `users`: `{id: {name, email}}` for every user a signal names; `problems`: `{id: {label, title}}`.
- `Side`: `{submission_id, user_id, language, source, created_at, spans: [{start, end}]}`; spans are byte offsets into `source`.
- `Moment`: `{at, kind, problem_id?, text}`, oldest first; `kind` is `submission`, `paste`, `blur`, `focus`, `fullscreen_exit`, `second_session` or `shared_device` (another student on the same browser and network; never a signal).
- `Case`: `{id, contest_id, user_id, name, email, decision, reason, decided_by, decided_at}`. `decision` is `no_action`, `warning` or `excluded`; the web app reads them as No action, Warning and Exclude from this contest. `excluded` takes the student off the board and the exports; a later `no_action` or `warning` brings them back. `decided_by` is the organiser's email.

### Rules and errors

| Status | When |
|---|---|
| 409 `contest_live` | "Check integrity after the contest ends for everyone.": before the close plus the longest extra time |
| 409 `already_running` | "A check is already running. Try again in a minute.": one check runs at a time on the server |
| 400 `bad_request` | an unknown decision; a reason under 10 characters ("Write the reason in a sentence or two.") or over 4,000 |
| 404 `not_found` | a decision about someone who did not take part |

A decision writes an `integrity_decision` row to the contest audit in the same transaction and refreshes the board. Check starts and decisions are logged with ids only, never reasons or code.

## Judge worker API

Used only by `podium-judge work`. It is served on its own listener, `PODIUM_JUDGE_LISTEN` (default `127.0.0.1:8081`, loopback only), never through Caddy, with no session cookies and no cross-site check. The request and response types live in `server/internal/judgeapi`, shared by both programs; the protocol, the worker and the sandbox are the proven core and v3 left them as they were.

- **Auth.** Every request sends `Authorization: Bearer <PODIUM_JUDGE_TOKEN>`. The server compares SHA-256 digests of the two tokens in constant time; anything else is `401 unauthorized`.
- **Bodies.** JSON with no unknown fields; at most 64 KB for a claim or toolchain report and 1 MB for a report; a malformed body is `400 bad_request`. An unknown route is `404 not_found`. The one non-JSON body is an uploaded output, at most 16 MB.
- **Protocol 3.** A judge and server of different protocols refuse each other with `426 protocol`. A run input may carry `"open": true` and a submit payload `"reveal": true`; the v3 server never sends `open`, and sends `reveal` only for practice and the organisers' workspace, when the judge reports `output` (the first 4 KB) on the first failing hidden test. The server refuses output on any other submit result.

### `POST /api/judge/claim` (judge)

Request: `{"kinds":["run","submit","generate","generate_inputs","validate"],"worker":"vm-1","version":"v3.0.0","isolate":"2.2.1","protocol":3,"toolchain":"<sha256>"}`. `worker`, `version` and `isolate` are logged and stored with the job. `protocol` left out means 1. `toolchain` is the hash the toolchain report returned; the server stores nothing.

| Status | Body |
|---|---|
| 204 | no queued job of those kinds (unknown kinds are ignored) |
| 200 | `{"job_id":41,"kind":"validate","lease":"<secret>","lease_seconds":60,"payload":{...}}` |
| 426 `protocol` | "judge protocol 3 needed, this judge speaks 2: install the same release on both". The judge stops with exit status 3, which `podium-judge.service` does not restart. Five `401` answers in a row stop it with exit status 4 |

The next job is the queued one with the lowest priority number, then the oldest, except that:

- jobs of a paused contest wait in the queue until it resumes;
- after a `run` is claimed, the next claim takes the best job of another kind when one waits, so runs can never starve submissions;
- while any contest is live (from its start to its close plus the longest extra time), at most one `validate`, `generate` or `generate_inputs` job is leased at a time, so setters' work never takes more than one lane from students' judging.

| Kind | Priority | What the worker does |
|---|---|---|
| `run` | 10 | compile, run each input, check examples against their expected output, report an output excerpt and stderr |
| `submit` | 20 plus the number of this user's judged submissions on the problem, at most 30 | compile, judge the examples, then each subtask in order |
| rejudge (`submit`) | 50 | as `submit` |
| practice (`submit`) | 60 | as `submit` |
| `generate`, `generate_inputs` | 85 | run the reference on each input and upload the outputs, or run the generator per planned test and upload the inputs |
| `validate` | 90 | compile and run the reference, brute force, validator and checker on every test |

The `submit` priority is the fair queue: a student's first submission on a problem goes ahead of someone's tenth.

Payloads, in `judgeapi`:

- `validate`: `{problem_id, content_hash, time_limit_ms, memory_limit_mb, checker: {type, epsilon, source_sha?}, programs: {reference, brute, validator}, tests: [{idx, sample, input_sha, output_sha}], points}`.
- `submit`: `{submission_id, program: {language, source_sha}, time_limit_ms, memory_limit_mb, checker, tests: [{idx, sample, subtask, input_sha, output_sha}], subtasks: [{idx, points}], stop_on_sample_fail, reveal?}`, examples first, then each subtask's tests in order. A failing example stops everything (`stop_on_sample_fail` is always true); a failing test stops the rest of its subtask. Every test not run is still reported, as `SK`.
- `run`: `{submission_id, program, limits, checker, inputs: [{idx, input_sha, expected_sha?}]}`; examples carry `expected_sha`, your own input has `idx` 0 and none.
- `generate`: `{problem_id, content_hash, program, limits, all, then_validate?, inputs: [{idx, input_sha}]}`.
- `generate_inputs`: `{problem_id, content_hash, plan_hash, generator, time_limit_ms, memory_limit_mb, tests: [{idx, subtask, seed, args}]}`.

### `POST /api/judge/toolchain` (judge)

Sent once when the judge starts. Request: `{"protocol":3,"toolchain":{"gcc","java","python","node","isolate","kernel"}}`: the first line of each tool's version output, `missing` when the tool cannot run, each at most 200 bytes of UTF-8.

| Status | Body |
|---|---|
| 200 | `{"hash":"<sha256>"}` of the toolchain's JSON; the server logs the versions and stores nothing |
| 400 `bad_report` | a field over 200 bytes or not UTF-8 |
| 426 `protocol` | as for a claim |

### `GET /api/judge/blob/{sha}` (judge)

Headers `X-Podium-Job: <job_id>` and `X-Podium-Lease: <lease>`, in headers so they never reach a log with the URL.

| Status | Body |
|---|---|
| 200 | the file, `application/octet-stream`, with `Content-Length` |
| 404 `not_found` | a malformed hash, no job header, a lease that is not current, a file this job does not need, or a missing file |

A worker fetches only the sources, checker and tests of a job it holds, so a leaked token alone does not expose the problems. The worker hashes every download again before using or caching it.

### `PUT /api/judge/blob/{sha}` (judge)

Uploads one output of a `generate` job, or one input of a `generate_inputs` job. Headers as for downloads; the body is the raw bytes, at most 16 MB; `{sha}` is the SHA-256 of the body in hex.

| Status | Body |
|---|---|
| 204 | stored, and noted as uploaded by this job |
| 409 `lease` | the lease is not current, the job takes no uploads, or it has used its budget (one per input or planned test, plus 2). The slot is reserved before the body is read and given back if the upload fails |
| 413 `too_large` | over 16 MB |
| 400 `bad_upload` | the body does not hash to `{sha}` |

The body streams to a temporary file while it is hashed and is renamed into place only if the hash matches. Uploads nothing used are deleted when the job closes. A report names each upload by its hash, and the server refuses any hash this job did not upload.

### `POST /api/judge/report` (judge)

```json
{ "job_id": 41, "lease": "<secret>",
  "compile": [ { "role": "reference", "verdict": "OK", "output": "" } ],
  "results": [ { "idx": 1, "role": "reference", "verdict": "AC", "time_ms": 2, "memory_kb": 980, "message": "" } ],
  "final": false, "fatal": "" }
```

| Status | Body |
|---|---|
| 200 | `{"lease_seconds":60}`: the lease lasts that long again |
| 400 `bad_report` | the results do not fit the job; `message` says why |
| 409 `lease` | the lease is unknown, expired and handed to another worker, or the job is closed: drop the job |

- Results are appended. Each compile role, and each (test, role) pair, may be reported once.
- Every result is checked against the payload: the test exists, the role is one the job has, compile verdicts are `OK` or `CE`, test verdicts are from the lists above, time is 0 to 600,000 ms, memory 0 to 8 GB, messages at most 256 bytes and compile output at most 16 KB, all valid UTF-8.
- `output` is refused on a `submit` result except the revealed test, and accepted (4 KB) on a `run`; `stderr` only on a `run`; `output_sha` on a `submit` is the hash of the program's output, and on `generate` and `generate_inputs` it must name an upload of this job.
- An empty report is a heartbeat; the worker sends one every 20 s during a long compile or test.
- `final: true` closes the job. The server computes the summary, score and verdict itself from the stored results; it never takes a whole-problem or whole-submission verdict from the judge.
- A non-empty `fatal` closes the job as an error (for example isolate failed); its text, up to 500 characters, is shown to the setter.

### Leases and deadlines

| Rule | Value |
|---|---|
| Lease | 60 s from the claim or the last report; stored only as its SHA-256 |
| Expiry | a sweep every 10 s returns jobs with an expired lease to the queue and deletes their results; the next worker starts again |
| Attempts | the third expired lease fails the job: "The judge stopped responding three times. Check podium-judge's log." |
| Deadline | set at the first claim and kept across attempts; a job still running then fails even if its worker keeps reporting |
| `validate` | 4 minutes plus, per test, 3 minutes and six times the time limit, at most 2 hours |
| `submit` | 2 minutes plus, per test, 25 s and six times the time limit, at most 1 hour |
| `run` | 2 minutes plus, per input, 25 s and six times the time limit, at most 30 minutes |
| `generate` | 2 minutes plus, per input, 30 s and six times the time limit, at most 1 hour |
| A failed job | a submission ends in `error` and is not counted; a run ends in `error`; a generation applies nothing |

## Static files

Every path that is not under `/api/` serves the embedded web app (`server/internal/webui`), for `GET` and `HEAD` only.

| Path | Answer |
|---|---|
| `/assets/*` (hashed build output) | the file, `Cache-Control: public, max-age=31536000, immutable` |
| other files of the build (`/favicon.svg`, `/fonts/*`, `/theme-init.js`) | the file, `public, max-age=3600` |
| a path ending in `/` (not `/` itself) | `301` to the same path without it, query kept |
| `/progress`, `/notices`, `/contact`, `/integrity` and `/integrity/*` (v2 pages) | `301` to `/` |
| `/robots.txt` | `User-agent: *` and `Disallow: /`, `public, max-age=3600` |
| a missing path with an extension | `404`, so an old hashed chunk after a deploy fails clearly instead of loading `index.html` as JavaScript |
| `/`, `/signin`, `/privacy`, and anything under `/contests/` or `/admin/` | `index.html`, `no-cache`, `X-Robots-Tag: noindex` |
| any other path | `index.html` with status `404`, `no-cache`, `X-Robots-Tag: noindex`; the app shows its "Page not found" page |

Podium is a college tool behind sign-in, so every page says `noindex` and `robots.txt` disallows everything. The web app routes on the client. The server's list of app paths is checked against the router's table (`web/src/lib/routing.ts`) by `TestRoutesMatchRouter`, so a new screen cannot answer 404 by mistake.
