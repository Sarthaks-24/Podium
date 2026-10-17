# HTTP API

All API routes live under `/api/` and speak JSON. Every other path serves the web app, which renders its own pages (including its 404 page).

## Conventions

- Responses are `application/json; charset=utf-8` with `Cache-Control: no-store`.
- Errors use one shape, with a stable machine code and a sentence for people. The web app shows `message` as is.

```json
{ "error": { "code": "not_found", "message": "This API route does not exist." } }
```

- Request bodies are JSON, sent with `Content-Type: application/json` (anything else gets `415 bad_request`), at most 64 KB, with no unknown fields.
- Every response carries a strict Content-Security-Policy (`script-src 'self'`, no inline scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: same-origin`.
- **Sessions.** Signing in sets an HttpOnly session cookie, `SameSite=Lax`, valid for 30 days and renewed while in use. With an `https` base URL it is named `__Host-podium_session` and marked `Secure`; with a local `http` base URL it is named `podium_session`.
- **Cross-site requests.** `POST`, `PUT`, `PATCH` and `DELETE` under `/api/` are refused with `403 cross_site` unless the request's `Origin` matches `PODIUM_BASE_URL` (or, without an `Origin` header, `Sec-Fetch-Site` is `same-origin` or absent).
- **Roles.** `student` < `admin` < `owner`. Routes marked "owner" return `401 signed_out` without a session and `403 forbidden` for lower roles. Roles are read from the database on every request, so removing an admin takes effect immediately.

Common error codes: `bad_request`, `signed_out`, `forbidden`, `not_found`, `cross_site`, `conflict`, `internal`.

## Health

### `GET /healthz`

| Status | Body |
|---|---|
| 200 | `{"status":"ok","db":"ok"}` |
| 503 | `{"status":"degraded","db":"unreachable"}` |

## Sign-in

### `GET /api/auth/config` (public)

What the sign-in page needs to explain who may sign in.

```json
{ "allowed_domains": ["college.edu"], "contact_email": "owner@college.edu" }
```

`contact_email` is `PODIUM_OWNER_EMAIL`, shown as the organiser to contact.

### `GET /api/auth/google/start?next=/path` (public, browser navigation)

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

## Static files

| Path | Caching |
|---|---|
| `/assets/*` (hashed build output) | `public, max-age=31536000, immutable` |
| other files (`/favicon.svg`, `/fonts/*`, `/theme-init.js`) | `public, max-age=3600` |
| missing files with an extension | `404` |
| any other path | `index.html`, `no-cache` |

Problems, submissions, server-sent events and contests are added to this document in the milestones that build them.
