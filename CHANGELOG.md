# Changelog

Dated entries per release. Unreleased work is listed at the top.

## Unreleased

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
