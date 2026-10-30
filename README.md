# podium

A self-hosted platform for weekly programming contests at a college. Students sign in with their college Google account, solve problems in C++, Java, Python or JavaScript, and get IOI-style partial scores from a sandboxed judge. A live leaderboard freezes near the end and is revealed afterwards. Admins write problems by hand, from a problem pack, or with an AI assistant that uses a temporary key.

Everything runs on one small Linux VM (Ubuntu 24.04, amd64 or arm64, 2 GB RAM is the design target) and is built to serve 700+ students in one contest.

Status (2026-10-30): v3 is built and waits for its tag, `v3.0.0`, which installs on a fresh VM; nothing is tagged or deployed yet. `CHANGELOG.md` lists what changed, and `docs/` describes v3.

## How it fits together

| Part | What it is |
|---|---|
| `server/cmd/podium-server` | Go web server: JSON API, live updates, the embedded web app, the judge queue. SQLite in WAL mode. |
| `server/cmd/podium-judge` | Go judge worker. Pulls jobs from the server's loopback judge API and runs every compile and run inside [isolate](https://github.com/ioi/isolate). Never touches the database. |
| `web/` | Svelte 5 single-page app, built to static files and embedded in the server binary. |
| `deploy/` | `install.sh`, systemd units, Caddyfile for the VM. |
| `problems/` | Example problem packs that pass validation. |
| `tools/` | Scripts behind `make dev-test`, `make sandbox-test` and `make calibrate`. |

Details and the reasoning behind each choice: `docs/ARCHITECTURE.md`.

## Run it locally (Windows)

The web app always runs natively on Windows. The judge needs Linux, so it runs in WSL2; `make dev-test` runs the server there too, `make dev` runs it on Windows. On Linux the same targets work, using `sudo` where they need root.

Prerequisites:

| Tool | Install |
|---|---|
| Go 1.27+ | `winget install GoLang.Go` |
| Node 22.12+ | `winget install OpenJS.NodeJS.LTS` |
| GNU make | `winget install ezwinports.make` |
| Git Bash | comes with Git for Windows; `make` runs its recipes in it, so start `make` from PowerShell or Git Bash. If Git is not in `C:\Program Files\Git`, pass `make GIT_BASH=<path to Git\bin\bash.exe>` |
| WSL2 Ubuntu 24.04 (for the judge) | `wsl --install -d Ubuntu-24.04` |

First, once: set up the judge in WSL2 from the repository folder:

```sh
wsl -d Ubuntu-24.04 -u root -- bash deploy/install.sh packages isolate testlib users
```

This installs the compilers, isolate, `testlib.h` for custom checkers, and the `podium` and `judge` users.

### 1. Try it with no keys: `make dev-test`

`make dev-test` runs the server, the judge and the web app in testing mode (`PODIUM_MODE=testing`): no Google client, no secrets, no `.env` file. The server and judge run in WSL2 with their state in `/var/tmp/podium-dev`; Vite runs on Windows.

1. `make dev-test`, then open http://localhost:5173.
2. Sign in from the form with any email and a role (choose Admin or Owner to see Contests and Problems). A banner on every page says this is testing mode.
3. Import a pack: zip a folder from `problems/` and choose Import pack, then Validate.
4. Choose "Try as a student" to run and submit code against it.

It listens on loopback only and is not for real contests. Details, and `npm run test:live` for browser tests against this stack: `docs/OPERATIONS.md`, "Run locally without any keys".

### 2. Develop with Google sign-in: `make dev`

1. Copy `server/.env.example` to `server/.env` and fill it in: `PODIUM_MODE=dev`, `PODIUM_BASE_URL=http://localhost:5173`, your Google address as `PODIUM_OWNER_EMAIL`, a random `PODIUM_SESSION_SECRET` (`openssl rand -base64 48`), a Google OAuth client with redirect URI `http://localhost:5173/api/auth/google/callback` (steps in `docs/OPERATIONS.md`, "Google sign-in"), and an empty `PODIUM_JUDGE_TOKEN` (dev mode then uses the testing token). The file is git-ignored; never commit it.
2. `make dev` starts the Go server on port 8080 (judge API on 8081) and the Vite dev server on port 5173 (which proxies `/api` and `/healthz` to Go). Open http://localhost:5173.

`make dev` does not start a judge, so validations wait in the queue; use `make dev-test` to exercise validation.

### 3. Production

See "Deploy to the VM" below: production mode is the default and requires every value.

### Check the sandbox

`make sandbox-test` runs the hostile-program suite and the acceptance tests (validating the example packs, then breaking one output; submitting each verdict in every language; making outputs; making inputs for the AI wizard's drafts) inside isolate in WSL2. `make calibrate` prints timing tables for the machine.

Other targets: `make help`.

## Checks

`make check` runs what the CI `check` job runs: `gofmt`, `go mod tidy -diff`, `go vet`, Go tests, `svelte-check`, ESLint, Vitest, the `install.sh` unit checks (`tools/install-test.sh`) and the deploy script checks (`tools/deploy-test.sh`). CI also runs `go test -race`, the size budget, the mocked Playwright suite (`cd web && npx playwright test --grep-invert "pixel baselines"`) and the sandbox suite. The pixel baselines run only on the owner's machine, because their references live in the git-ignored `cache/`. Run `make check-secrets` (or install it as a hook with `make hooks`) before committing.

## Build a release

`make release` (on a commit tagged `vX.Y.Z`, or `make release VERSION=vX.Y.Z` on any other commit: a release must be named `vX.Y.Z`; `ARCH=arm64` builds for an ARM VM) runs the checks, builds the web app, embeds it, cross-compiles both binaries for Linux with `CGO_ENABLED=0`, and writes `release/podium-<version>.tar.gz` containing `bin/`, `env/` (the example env files), `install.sh`, the systemd units, the Caddyfile, `litestream.yml`, the example env files for Litestream, the nightly backup and alerts, and the backup, restore, alert and health-check scripts in `backup/`. It refuses to build when the web app's source carries placeholder copy (`tools/check-placeholders.sh`), and installs `web/node_modules` with `npm ci` first when it is missing. Building on Windows is fine: the tarball loses the execute bit on the Linux binaries, and `install.sh` sets it when it installs them.

The tarball also holds `MANIFEST` (version, commit, build date, processor, schema, the migrations it carries, the judge protocol, and the oldest release it upgrades from, `UPGRADES_FROM` in the `Makefile`) and `SHA256SUMS`, and `release/podium-<version>.tar.gz.sha256` sits beside it. `podium-server -version` and `podium-judge -version` print the same facts. Pushing a `v*` tag runs the same build in GitHub Actions (`.github/workflows/release.yml`) and makes a draft release with those four files; the owner reviews and publishes it. The first release, `v3.0.0`, installs fresh, so its upgrade path matters only from the release after it.

## Deploy to the VM

The VM is Ubuntu 24.04 on **amd64 or arm64**. `make release` builds for amd64; `make release ARCH=arm64` builds `podium-<version>-arm64.tar.gz` for an ARM VM such as Oracle's free Ampere A1 (docs/OPERATIONS.md, "Oracle Always Free", which also covers the watchdog and the offsite backup bucket). On a fresh VM, in production mode (the default):

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh                 # every step; skips what needs a value you have not set yet
openssl rand -hex 32              # the judge token: same value in server.env and judge.env
sudoedit /etc/podium/server.env   # fill in the values (docs/OPERATIONS.md, Configuration)
sudoedit /etc/podium/judge.env    # PODIUM_JUDGE_TOKEN
sudo ./install.sh app             # starts the server and the judge once no placeholder is left
sudoedit /etc/podium/litestream.env   # the database bucket and its key
sudo systemctl restart podium-litestream
sudoedit /etc/podium/backup.env   # heartbeat URL
sudo systemctl start podium-backup
sudoedit /etc/podium/alert.env    # the ntfy topic URL
sudo /usr/local/lib/podium/podium-alert test
sudo ./install.sh caddy           # once DNS points at the VM: the HTTPS certificate
sudo ./install.sh verify          # must end with "everything checked is in order"
```

Production needs an https `PODIUM_BASE_URL`, the Google client (its OAuth app Internal or In production, never Testing), the session secret and your own judge token; the server names anything missing and refuses to start. Then verify the sandbox on the VM itself (with the worker stopped): `sudo systemctl stop podium-judge && sudo -u judge podium-judge calibrate; sudo systemctl start podium-judge`. Keep a copy of every `/etc/podium/*.env` file in your password manager: a restore onto a new VM needs them.

Upgrades: `tools/deploy.sh <host>` from Git Bash (or `make deploy HOST=<host>`) builds the release, confirms before anything leaves the machine, and runs `sudo ./install.sh upgrade` on the VM. `sudo /usr/local/lib/podium/install.sh rollback` goes back. Do not deploy in the 48 hours before a published contest, during it, or while it is still being judged: nothing enforces this, so it is a rule in `docs/RUNBOOK.md`.

Full operations guide, configuration reference, backups, alerts, upgrades and rollback: `docs/OPERATIONS.md`. Contest day, restoring onto a new VM and the drills: `docs/RUNBOOK.md`.

## Load test

`tools/loadtest/` holds a k6 script that plays a whole contest (sign-in, event streams, drafts, standings, runs and submissions with an end-of-contest spike) against a testing-mode stack, a seed script and a summary script. How to run it: `tools/loadtest/README.md`.

## Documentation

| File | Contents |
|---|---|
| `docs/ARCHITECTURE.md` | Services, data flow, judge design, and why each choice was made |
| `docs/OPERATIONS.md` | Install, run modes, configuration variables, metrics, upgrades, backups and restore, validation troubleshooting |
| `docs/RUNBOOK.md` | Contest day: checklists, what to watch, if something goes wrong, restoring, the dry run with real students |
| `docs/AUTHORING.md` | Writing problems: the pack format, statements, subtasks, checkers, validation |
| `docs/INTEGRITY.md` | For organisers: what is recorded during a contest, what the similar-code check finds, how to record a decision about a student, and a policy template |
| `docs/API.md` | HTTP routes, the judge worker API, and their request and response shapes |
| `CHANGELOG.md` | Dated changes per release |
