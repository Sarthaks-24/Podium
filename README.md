# podium

A self-hosted platform for weekly programming contests at a college. Students sign in with their college Google account, solve problems in C++, Java, Python or JavaScript, and get IOI-style partial scores from a sandboxed judge. A live leaderboard freezes near the end and is revealed afterwards. Admins write problems by hand, from a problem pack, or with an AI assistant that uses a temporary key.

Everything runs on one small Linux VM (2 GB RAM is the target) and is built to serve 700+ students in one contest.

Status: milestone M3 (submissions). The judge sandbox, Google sign-in with the domain check, roles, the owner's People page, the problem bank with pack import and export, output generation, validation on the real judge, and the student workspace with live verdicts exist; organisers try it with "Try as a student" until contests arrive in M4. See `CHANGELOG.md`.

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
2. Sign in from the form with any email and a role (choose Admin or Owner to see Problems). A banner on every page says this is testing mode.
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

`make sandbox-test` runs the hostile-program suite and the M2 and M3 acceptance tests (validating the example packs, then breaking one output; submitting each verdict in every language; generating outputs) inside isolate in WSL2. `make calibrate` prints timing tables for the machine.

Other targets: `make help`.

## Checks

`make check` runs everything CI runs: `gofmt`, `go vet`, Go tests, `svelte-check`, ESLint and Vitest. Run `make check-secrets` (or install it as a hook with `make hooks`) before committing.

## Build a release

`make release` runs the checks, builds the web app, embeds it, cross-compiles both binaries for Linux with `CGO_ENABLED=0`, and writes `release/podium-<version>.tar.gz` containing `bin/`, `env/` (the example env files), `install.sh`, the systemd units and the Caddyfile. Building on Windows is fine: the tarball loses the execute bit on the Linux binaries, and `install.sh` sets it when it installs them.

## Deploy to the VM

On a fresh Ubuntu 24.04 VM, in production mode (the default):

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh                 # packages, isolate, testlib, users, dirs, app, caddy, swap
openssl rand -hex 32              # the judge token: same value in both files below
sudoedit /etc/podium/server.env   # fill in the values (see docs/OPERATIONS.md)
sudoedit /etc/podium/judge.env    # PODIUM_JUDGE_TOKEN
sudo ./install.sh caddy           # once PODIUM_BASE_URL is set
sudo systemctl restart podium-server podium-judge
```

Production needs an https `PODIUM_BASE_URL`, the Google client, the session secret and your own judge token; the server names anything missing and refuses to start. Then verify the sandbox on the VM itself (with the worker stopped): `sudo -u judge podium-judge calibrate`. Full operations guide, configuration reference and backups: `docs/OPERATIONS.md`.

## Documentation

| File | Contents |
|---|---|
| `docs/ARCHITECTURE.md` | Services, data flow, judge design, and why each choice was made |
| `docs/OPERATIONS.md` | Install, run modes, configuration variables, upgrades, backups, validation troubleshooting, contest day |
| `docs/AUTHORING.md` | Writing problems: the pack format, statements, subtasks, checkers, validation |
| `docs/API.md` | HTTP routes, the judge worker API, and their request and response shapes |
| `CHANGELOG.md` | Dated changes per release |
