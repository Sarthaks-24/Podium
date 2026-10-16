# Changelog

Dated entries per release. Unreleased work is listed at the top.

## Unreleased

### Milestone M0: foundations (2026-10-17)

- Go module with `podium-server` and `podium-judge`, cross-compiled for Linux with `CGO_ENABLED=0`.
- Start-up validator for environment variables; `server/.env.example` and `judge/.env.example`.
- SQLite in WAL mode with numbered migrations; first migration holds users, allowed domains and sessions.
- HTTP server with `/healthz`, JSON errors, security headers, and the embedded web app.
- Judge sandbox driver on isolate 2.7: fresh box per compile and per test on a tmpfs box root, link-safe reads of box files, a memory budget across lanes, verdict classification with size-based output limits, streaming `tokens`, `exact` and `float` checkers, language table for C++17, Java 21, Python 3 and Node 22.
- Hostile-program suite (16 programs) and `podium-judge calibrate`; all contained in the local WSL2 run.
- Svelte 5 web scaffold with design tokens, light and dark themes, self-hosted Schibsted Grotesk and Fragment Mono.
- `deploy/install.sh`, systemd units, Caddyfile, Makefile and CI.
