# HTTP API

All API routes live under `/api/` and speak JSON. Every other path serves the web app, which renders its own pages (including its 404 page).

## Conventions

- Responses are `application/json; charset=utf-8` with `Cache-Control: no-store`.
- Errors use one shape, with a stable machine code and a sentence for people:

```json
{ "error": { "code": "not_found", "message": "This API route does not exist." } }
```

- Every response carries a strict Content-Security-Policy (`script-src 'self'`, no inline scripts), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: same-origin`.

## Routes

### `GET /healthz`

Liveness and database check, used by monitoring and the dev status line.

| Status | Body |
|---|---|
| 200 | `{"status":"ok","db":"ok"}` |
| 503 | `{"status":"degraded","db":"unreachable"}` |

### `/api/*` (unknown routes)

| Status | Body |
|---|---|
| 404 | `{"error":{"code":"not_found","message":"This API route does not exist."}}` |

## Static files

| Path | Caching |
|---|---|
| `/assets/*` (hashed build output) | `public, max-age=31536000, immutable` |
| other files (`/favicon.svg`, `/fonts/*`, `/theme-init.js`) | `public, max-age=3600` |
| any other path | `index.html`, `no-cache` |

Sign-in, problems, submissions, server-sent events and contests are added to this document in the milestones that build them.
