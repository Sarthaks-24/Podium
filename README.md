# podium

A self-hosted platform for weekly programming contests at a college. Students sign in with their college Google account, solve problems in C++, Java, Python or JavaScript, and get IOI-style partial scores from a sandboxed judge. A live leaderboard freezes near the end and is revealed afterwards. Admins write problems by hand, from a problem pack, or with an AI assistant that uses a temporary key.

Everything runs on one small Linux VM (2 GB RAM is the target) and is built to serve 700+ students in one contest.

Status: milestone M0 (foundations). The judge sandbox, server skeleton and web scaffold exist; sign-in, problems, submissions and contests arrive in M1 to M4. See `CHANGELOG.md`.

## How it fits together

| Part | What it is |
|---|---|
| `server/cmd/podium-server` | Go web server: JSON API, live updates, the embedded web app, the judge queue. SQLite in WAL mode. |
| `server/cmd/podium-judge` | Go judge worker. Runs every compile and run inside [isolate](https://github.com/ioi/isolate). Never touches the database. |
| `web/` | Svelte 5 single-page app, built to static files and embedded in the server binary. |
| `deploy/` | `install.sh`, systemd units, Caddyfile for the VM. |

Details and the reasoning behind each choice: `docs/ARCHITECTURE.md`.

## Run it locally (Windows)

The server and web app run natively on Windows. The judge needs Linux, so it runs in WSL2.

Prerequisites:

| Tool | Install |
|---|---|
| Go 1.27+ | `winget install GoLang.Go` |
| Node 22.12+ | `winget install OpenJS.NodeJS.LTS` |
| GNU make | `winget install ezwinports.make` |
| Git Bash | comes with Git for Windows; run `make` from it |
| WSL2 Ubuntu 24.04 (for the judge) | `wsl --install -d Ubuntu-24.04` |

Steps:

1. Copy `server/.env.example` to `server/.env` and set `PODIUM_BASE_URL=http://localhost:5173`. The file is git-ignored; never commit it.
2. `make dev` starts the Go server on port 8080 and the Vite dev server on port 5173 (which proxies `/api` and `/healthz` to Go). Open http://localhost:5173.
3. Judge setup in WSL2, once: `wsl -d Ubuntu-24.04 -u root -- bash deploy/install.sh packages isolate users` from the repository folder. This installs the compilers, isolate, and the `judge` user.
4. `make sandbox-test` runs the hostile-program suite inside isolate (in WSL2). `make calibrate` prints timing tables for the machine.

Other targets: `make help`.

## Checks

`make check` runs everything CI runs: `gofmt`, `go vet`, Go tests, `svelte-check`, ESLint and Vitest. Run `make check-secrets` (or install it as a hook with `make hooks`) before committing.

## Build a release

`make release` runs the checks, builds the web app, embeds it, cross-compiles both binaries for Linux with `CGO_ENABLED=0`, and writes `release/podium-<version>.tar.gz` containing `bin/`, `env/` (the example env files), `install.sh`, the systemd units and the Caddyfile. Building on Windows is fine: the tarball loses the execute bit on the Linux binaries, and `install.sh` sets it when it installs them.

## Deploy to the VM

On a fresh Ubuntu 24.04 VM:

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh                 # packages, isolate, users, dirs, app, caddy, swap
sudoedit /etc/podium/server.env   # fill in the values (see docs/OPERATIONS.md)
sudo ./install.sh caddy           # once PODIUM_BASE_URL is set
sudo systemctl restart podium-server podium-judge
```

Then verify the sandbox on the VM itself: `sudo -u judge podium-judge calibrate`. Full operations guide, configuration reference and backups: `docs/OPERATIONS.md`.

## Documentation

| File | Contents |
|---|---|
| `docs/ARCHITECTURE.md` | Services, data flow, judge design, and why each choice was made |
| `docs/OPERATIONS.md` | Install, configuration variables, upgrades, backups, contest day |
| `docs/AUTHORING.md` | Writing problems: limits, languages, checkers |
| `docs/API.md` | HTTP routes and their request and response shapes |
| `CHANGELOG.md` | Dated changes per release |
