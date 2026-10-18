# Operations

How to install, configure, check and upgrade podium on its VM, and how to run it on your own machine. The contest-day runbook is added here once it has been through a dry-run contest (milestone M7).

## Install

Target: Ubuntu 24.04 LTS, cgroup v2 (the default), 2 GB RAM or more.

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh
```

`install.sh` is idempotent and runs these steps in order. Name steps to run only those, for example `sudo ./install.sh packages isolate`.

| Step | What it does |
|---|---|
| `packages` | g++, OpenJDK 21, Python 3, Node 22 (NodeSource repository), sqlite3 |
| `isolate` | isolate from its upstream apt repository; mounts its box root `/var/lib/isolate` on a 1200 MB tmpfs (so files a program writes count against its memory limit); caps all sandboxes at 1100 MB through `isolate.slice`; enables `isolate.service`; runs `isolate-check-environment` |
| `testlib` | `testlib.h` (release 0.9.41) into `/usr/local/include` for custom checkers. Downloaded from GitHub at a pinned commit and refused unless its SHA-256 matches the one in the script. Skipped when the installed file already matches. |
| `users` | system users `podium` and `judge`; adds `judge` to group `isolate` (the isolate binary is executable only by that group) |
| `dirs` | `/var/lib/podium/{db,tests}` (podium, 750), `/var/cache/podium-judge` (judge, 750), `/etc/podium`; creates `server.env` and `judge.env` from the examples if missing (root, 600), with the absolute VM paths for the database and tests |
| `app` | installs both binaries and systemd units; starts the services only once `server.env` no longer contains placeholder values |
| `caddy` | installs Caddy and writes `/etc/caddy/Caddyfile` for the host in `PODIUM_BASE_URL` |
| `swap` | 1 GB swap file, `vm.swappiness=10` |

`isolate-check-environment` may warn about simultaneous multithreading, address-space randomisation, transparent huge pages and the core file pattern. These affect timing reproducibility, not safety. For the steadiest timings, disable transparent huge pages (`echo never > /sys/kernel/mm/transparent_hugepage/enabled`) and set `kernel.core_pattern=core`; leave address-space randomisation on.

The judge unit runs `podium-judge work`, which pulls jobs from the server's judge API. It needs `PODIUM_JUDGE_TOKEN` in both `server.env` and `judge.env` (see "Judge token" below).

## Verify the sandbox on the VM

After installing, and after every isolate or kernel upgrade, with the worker stopped (calibrate has its own memory budget and must not overlap the worker's boxes):

```sh
sudo systemctl stop podium-judge
sudo -u judge podium-judge calibrate
sudo systemctl start podium-judge
```

It runs the 16 hostile programs (fork bomb, 2 GB allocation, endless loop and output, `/etc/shadow`, network, compile bomb, Java and Python memory, Node threads, symlink and FIFO tricks on the output file, filling `/tmp`) and prints timing tables. It exits non-zero if any program escapes its limits. Do not run contests until it passes.

On a development machine, `make sandbox-test` runs the same suite plus the M2 acceptance test: it validates the three example packs in `problems/` on real isolate, breaks one expected output and checks that validation fails naming that test, and checks that a hostile custom checker is contained.

## Run modes

Both services read `PODIUM_MODE`. A missing value means `production`, so a forgotten setting is strict, never lax.

| | `testing` | `dev` | `production` (default) |
|---|---|---|---|
| Use it for | trying podium, automated end-to-end tests, demos on your own machine | local work with real Google sign-in | the VM |
| Sign-in | a form: any email, any role, no password | Google | Google |
| Google client | not read | required | required |
| `PODIUM_BASE_URL` | optional, default `http://localhost:5173`, must be a localhost address | required, http allowed | required, must be `https` |
| `PODIUM_LISTEN` | must be loopback | any | any |
| `PODIUM_OWNER_EMAIL` | optional, default `owner@podium.test` | required | required |
| `PODIUM_SESSION_SECRET` | optional, random for each run | required | required |
| `PODIUM_JUDGE_TOKEN` | optional, public testing token when empty | optional, public testing token when empty | required, testing token refused |
| Banner in the web app | "Testing mode: sign-in is off..." on every page | none | none |

Testing mode lets anyone who can reach the server sign in as an owner, which is why the server refuses to start in that mode unless both the base URL and the listen address are on this machine. The public testing judge token is in the source code; it pairs a local server and judge without setup, the judge API only ever listens on loopback, and production refuses it.

## Run locally without any keys

`make dev-test` starts the whole stack in testing mode: no Google client, no secrets, no `.env` file.

| Part | Where |
|---|---|
| `podium-server` | `127.0.0.1:8080`, judge API on `127.0.0.1:8081` |
| `podium-judge work` | 2 lanes, pulling from the judge API with the testing token |
| Vite dev server | http://localhost:5173, proxies `/api` and `/healthz` to the server |
| State (database, tests, judge cache) | `/var/tmp/podium-dev` in Linux (WSL2 on Windows); set `PODIUM_DEV_STATE` to move it |

How it runs:

- It cross-compiles both binaries for Linux and runs `tools/dev-stack.sh` as root (through WSL2 on Windows, `sudo` on Linux). The script drops to the `podium` user for the server and the `judge` user for the worker, as on the VM. Vite runs natively and reaches the server through WSL2's localhost forwarding.
- State stays outside the repository because SQLite's WAL needs a local Linux filesystem, not `/mnt/<drive>`. Delete `/var/tmp/podium-dev` to start again from an empty database.
- Prerequisite, once: the judge setup in WSL2 (`deploy/install.sh packages isolate testlib users`, see `README.md`).
- Ctrl+C stops everything.

Signing in: open http://localhost:5173, enter any email, an optional name and a role (Student, Admin or Owner), and choose "Sign in for testing". Signing in again with the same email changes that person's role. `owner@podium.test` (or `PODIUM_OWNER_EMAIL` if set) is always an owner. Every page shows the testing-mode banner so nobody mistakes the stack for a real contest.

Live browser tests: with `make dev-test` running and the three example packs imported and validated (zip each folder in `problems/` and use "Import pack"), run `npm run test:live` in `web/`. It signs in from the form, opens the problem bank and reads a real validation report through Vite's proxy, with nothing mocked. It reuses the Vite server on port 5173, because the testing-mode base URL is `http://localhost:5173` and the cross-site check refuses any other origin.

## Configuration

All configuration comes from environment variables. On the VM they live in `/etc/podium/server.env` and `/etc/podium/judge.env` (root-owned, mode 600), read by systemd through `EnvironmentFile=`. Locally they live in `server/.env` and `judge/.env`, which git ignores. Each service checks every variable before opening a port and exits with a message naming what is missing and which file to fix. Logs show each variable as `set`, `default` or `missing`, never its value.

### podium-server (`/etc/podium/server.env`)

Listed in the order the server reads them. The testing, dev and production columns say what each run mode needs.

| Variable | testing | dev | production | Default | Purpose |
|---|---|---|---|---|---|
| `PODIUM_MODE` | | | | `production` | `testing`, `dev` or `production` (see "Run modes"). |
| `PODIUM_BASE_URL` | optional | required | required, https | testing: `http://localhost:5173` | Public URL students open, for example `https://contest.example.edu`. Scheme and host only. Used for sign-in redirects, cookie flags (`https` makes the session cookie `Secure` and `__Host-` prefixed), the cross-site check and the Caddy host. |
| `PODIUM_LISTEN` | loopback only | optional | optional | `127.0.0.1:8080` | Address the HTTP server binds; Caddy proxies to it. |
| `PODIUM_JUDGE_LISTEN` | optional | optional | optional | `127.0.0.1:8081` | Address of the judge worker API. Must be loopback in every mode and differ from `PODIUM_LISTEN` (see "Judge API listener"). |
| `PODIUM_DB_PATH` | optional | optional | optional | `data/podium.db` | SQLite file. On the VM use `/var/lib/podium/db/podium.db`. |
| `PODIUM_TESTS_DIR` | optional | optional | optional | `data/tests` | Test files and program sources by SHA-256. On the VM use `/var/lib/podium/tests`. |
| `PODIUM_OWNER_EMAIL` | optional | required | required | testing: `owner@podium.test` | Your Google account. Always an owner, may sign in from any domain, and is shown as the organiser to contact. The recovery path if every other owner is removed. |
| `PODIUM_ALLOWED_DOMAINS` | optional | optional | optional | none | Comma-separated email domains, read only on the very first start to seed the list. Afterwards owners manage it on the People page. |
| `PODIUM_SESSION_SECRET` | optional | required | required | testing: random per run | At least 32 random characters (`openssl rand -base64 48`). Signs the sign-in state cookie. Changing it only interrupts sign-ins in progress. Secret. |
| `GOOGLE_CLIENT_ID` | not read | required | required | | OAuth client ID from the Google Cloud console (see below). |
| `GOOGLE_CLIENT_SECRET` | not read | required | required | | That client's secret. Secret. |
| `PODIUM_JUDGE_TOKEN` | optional | optional | required | testing and dev: the public testing token | Shared token for the judge API, at least 32 characters (`openssl rand -hex 32`). The same value goes in `judge.env`. Production refuses the testing token. Secret. |
| `PODIUM_LOG_LEVEL` | optional | optional | optional | `info` | `debug`, `info`, `warn` or `error`. |

### Google sign-in

Needed in dev and production mode only.

1. In the [Google Cloud console](https://console.cloud.google.com/), create or pick a project, then open APIs and Services, OAuth consent screen. For a college Google Workspace choose "Internal" (only your domain's accounts can sign in); otherwise "External", and add the scopes `openid`, `email`, `profile`.
2. Credentials, Create credentials, OAuth client ID, application type "Web application".
3. Authorised redirect URI: `<PODIUM_BASE_URL>/api/auth/google/callback`, for example `https://contest.example.edu/api/auth/google/callback`. For local work add `http://localhost:5173/api/auth/google/callback` too.
4. Put the client ID and secret in `server.env` and restart the server.

Who can sign in: anyone whose verified Google email is on an allowed domain, anyone holding the admin or owner role (on any domain), and `PODIUM_OWNER_EMAIL`. Removing an admin ends their sessions at once; removing a domain ends the sessions of students on it.

`PODIUM_BASE_URL` must be just the scheme and host (no path); podium runs at the root of its own host name.

### Recovering access

If you are locked out (every owner removed, or the domain list emptied), set `PODIUM_OWNER_EMAIL` to your address and restart: that account is made an owner again at start-up. Sessions can be ended for everyone with `sqlite3 /var/lib/podium/db/podium.db "DELETE FROM sessions"`.

Each account is linked to the Google account that first signed in with it. If someone sees "This email is linked to a different Google account" after their address legitimately moved to a new Google account, reset the link on the People page ("Google account links"). If that someone is you, the configured owner, use `sqlite3 /var/lib/podium/db/podium.db "UPDATE users SET google_sub = NULL WHERE email = 'you@college.edu'"`.

### podium-judge (`/etc/podium/judge.env`)

Listed in the order the judge reads them, with the same mode columns. `podium-judge calibrate` reads only the last four.

| Variable | testing | dev | production | Default | Purpose |
|---|---|---|---|---|---|
| `PODIUM_MODE` | | | | `production` | As on the server. |
| `PODIUM_SERVER_URL` | optional | optional | optional | `http://127.0.0.1:8081` | The server's judge API (its `PODIUM_JUDGE_LISTEN`) as a URL with no path. For a remote judge, the local end of the tunnel. |
| `PODIUM_JUDGE_TOKEN` | optional | optional | required | testing and dev: the public testing token | The server's `PODIUM_JUDGE_TOKEN`. Production refuses the testing token. Secret. |
| `PODIUM_ISOLATE_PATH` | optional | optional | optional | `/usr/bin/isolate` | The isolate binary. |
| `PODIUM_JUDGE_LANES` | optional | optional | optional | `2` | Sandboxes judged in parallel (1 to 16). Two fit a 2 GB VM. |
| `PODIUM_JUDGE_CACHE_DIR` | optional | optional | optional | `/var/cache/podium-judge` | Downloaded tests and sources (`blobs/`, re-hashed on download) and per-job work files (`jobs/`). |
| `PODIUM_LOG_LEVEL` | optional | optional | optional | `info` | As above. |

Backup variables (Litestream) appear here when their code lands in M7.

### Judge token

1. Generate it once: `openssl rand -hex 32`.
2. Put the same value in `/etc/podium/server.env` and `/etc/podium/judge.env` as `PODIUM_JUDGE_TOKEN` (`sudoedit` each file).
3. `sudo systemctl restart podium-server podium-judge`.

To rotate it, repeat all three steps. A job the old worker held is handed out again once its lease runs out (60 s).

## Judge API listener

The judge worker talks to the server over a second HTTP listener, `PODIUM_JUDGE_LISTEN` (default `127.0.0.1:8081`), with a bearer token. The routes are in `docs/API.md`.

| Rule | Why |
|---|---|
| It is a separate listener, never behind Caddy | Caddy proxies only `PODIUM_LISTEN`, so the judge routes are not reachable from the internet even if a proxy rule is wrong. |
| It must be a loopback address | The token reports verdicts. Binding to loopback means nothing on the network can try it; the server refuses to start with any other address. |
| A judge on another machine uses a tunnel | Run the tunnel from the judge machine, then set `PODIUM_SERVER_URL` to its local end. |

A remote judge over SSH, run on the judge machine with an account on the VM that may only forward ports (keep it up with a systemd unit or `autossh`):

```sh
ssh -N -L 127.0.0.1:8081:127.0.0.1:8081 tunnel@contest.example.edu
# judge.env on that machine: PODIUM_SERVER_URL=http://127.0.0.1:8081
```

Over WireGuard, run the same forward to the server's WireGuard address; the listener itself stays on loopback. Never run judge workers on machines students can reach: the token is enough to report verdicts.

## Health

- `curl -fsS http://127.0.0.1:8080/healthz` returns `{"status":"ok","db":"ok"}`, or HTTP 503 when the database is unreachable.
- The judge API has no health route. `curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8081/api/judge/claim` prints `401` when the listener is up (it asks for the token).
- Logs: `journalctl -u podium-server -u podium-judge -f`. They are JSON lines. The worker logs `job started` and `job finished` per job; the server logs `job leased` and `job closed`.

## Validation troubleshooting

| What you see | Cause | What to do |
|---|---|---|
| "Waiting for the judge" and it never changes | No worker is claiming jobs: the judge is stopped, its token differs from the server's, or `PODIUM_SERVER_URL` is wrong | `systemctl status podium-judge`. In its log, `claim failed ... server answered 401` means the tokens differ; a connection error means the URL or the listener is wrong. Fix `judge.env` and restart the judge. Locally, `make dev` does not start a judge; use `make dev-test`. |
| "Validation could not finish: The judge stopped responding three times" | The worker held the job but stopped reporting three times (crash, restart, network) | Read the judge's log around that time, fix the cause, then choose Validate again. |
| "The judge took too long on this job and it was stopped" | The job passed its deadline: 4 minutes plus, per test, 3 minutes and six times the time limit, at most 2 hours, counted from the first claim | Usually a test far larger than intended, or a brute force or validator that is very slow. Check the limits and test sizes, then validate again. |
| "The judge could not run this validation: ..." | isolate failed, or a file could not be fetched | Run `sudo -u judge podium-judge calibrate` (with the worker stopped) to check isolate. |
| "The checker does not compile" with `testlib.h: No such file or directory` | The `testlib` install step has not run on this host | `sudo ./install.sh testlib`. On WSL2: `deploy/install.sh testlib` as root. |
| A custom checker takes several seconds to compile | Expected: `testlib.h` is a large header. Checkers get a 30 s CPU compile budget instead of the usual 10 s | Nothing. A checker that needs more than 30 s is reported as a compile error. |
| A second pack import waits | Imports, test replacements and pack downloads run one at a time, because each can hold over 100 MB in memory | Wait for the first to finish. |

## Upgrade

```sh
tar xzf podium-<new>.tar.gz && cd podium-<new>
sudo ./install.sh app
```

Database migrations run automatically when the server starts. Configuration files are never overwritten, so add new variables by hand.

Upgrading to M2 from M1:

1. `sudo ./install.sh testlib app` (installs `testlib.h` and the new judge unit, which runs `podium-judge work`).
2. Add `PODIUM_JUDGE_TOKEN` to both env files (see "Judge token"). Without it the server refuses to start in production mode. `PODIUM_MODE`, `PODIUM_JUDGE_LISTEN` and `PODIUM_SERVER_URL` can stay unset: their defaults are right for one VM.
3. `sudo systemctl restart podium-server podium-judge`. Migration `0003_problems.sql` runs on start.

## Backups

Litestream replication and nightly snapshots are set up in milestone M7. Until then, copy the database safely with `sqlite3 /var/lib/podium/db/podium.db ".backup /var/backups/podium-$(date +%F).db"`, and copy `/var/lib/podium/tests` too: test files and program sources live there, named by hash, and the database only refers to them.
