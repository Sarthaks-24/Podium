# Operations

How to install, configure, check and upgrade podium on its VM. The contest-day runbook is added here once it has been through a dry-run contest (milestone M7).

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
| `users` | system users `podium` and `judge`; adds `judge` to group `isolate` (the isolate binary is executable only by that group) |
| `dirs` | `/var/lib/podium/{db,tests}` (podium, 750), `/var/cache/podium-judge` (judge, 750), `/etc/podium`; creates `server.env` and `judge.env` from the examples if missing (root, 600), with the absolute VM paths for the database and tests |
| `app` | installs both binaries and systemd units; starts the services only once `server.env` no longer contains placeholder values |
| `caddy` | installs Caddy and writes `/etc/caddy/Caddyfile` for the host in `PODIUM_BASE_URL` |
| `swap` | 1 GB swap file, `vm.swappiness=10` |

`isolate-check-environment` may warn about simultaneous multithreading, address-space randomisation, transparent huge pages and the core file pattern. These affect timing reproducibility, not safety. For the steadiest timings, disable transparent huge pages (`echo never > /sys/kernel/mm/transparent_hugepage/enabled`) and set `kernel.core_pattern=core`; leave address-space randomisation on.

## Verify the sandbox on the VM

After installing, and after every isolate or kernel upgrade, with the worker stopped (calibrate has its own memory budget and must not overlap the worker's boxes):

```sh
sudo systemctl stop podium-judge
sudo -u judge podium-judge calibrate
sudo systemctl start podium-judge
```

It runs the 16 hostile programs (fork bomb, 2 GB allocation, endless loop and output, `/etc/shadow`, network, compile bomb, Java and Python memory, Node threads, symlink and FIFO tricks on the output file, filling `/tmp`) and prints timing tables. It exits non-zero if any program escapes its limits. Do not run contests until it passes.

## Configuration

All configuration comes from environment variables. On the VM they live in `/etc/podium/server.env` and `/etc/podium/judge.env` (root-owned, mode 600), read by systemd through `EnvironmentFile=`. Locally they live in `server/.env` and `judge/.env`, which git ignores. Each service checks every variable before opening a port and exits with a message naming what is missing and which file to fix. Logs show each variable as `set`, `default` or `missing`, never its value.

### podium-server (`/etc/podium/server.env`)

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `PODIUM_BASE_URL` | yes | | Public URL students open, for example `https://contest.example.edu`. Used for sign-in redirects, cookie flags (`https` makes the session cookie `Secure` and `__Host-` prefixed), the cross-site check and the Caddy host. |
| `PODIUM_LISTEN` | no | `127.0.0.1:8080` | Address the HTTP server binds; Caddy proxies to it. |
| `PODIUM_DB_PATH` | no | `data/podium.db` | SQLite file. On the VM use `/var/lib/podium/db/podium.db`. |
| `PODIUM_TESTS_DIR` | no | `data/tests` | Test files by SHA-256. On the VM use `/var/lib/podium/tests`. |
| `PODIUM_OWNER_EMAIL` | yes | | Your Google account. Always an owner, may sign in from any domain, and is shown as the organiser to contact. The recovery path if every other owner is removed. |
| `PODIUM_ALLOWED_DOMAINS` | no | none | Comma-separated email domains, read only on the very first start to seed the list. Afterwards owners manage it on the People page. |
| `PODIUM_SESSION_SECRET` | yes | | At least 32 random characters (`openssl rand -base64 48`). Signs the sign-in state cookie. Changing it only interrupts sign-ins in progress. Secret. |
| `GOOGLE_CLIENT_ID` | yes | | OAuth client ID from the Google Cloud console (see below). |
| `GOOGLE_CLIENT_SECRET` | yes | | That client's secret. Secret. |
| `PODIUM_LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error`. |

### Google sign-in

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

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `PODIUM_ISOLATE_PATH` | no | `/usr/bin/isolate` | The isolate binary. |
| `PODIUM_JUDGE_LANES` | no | `2` | Sandboxes judged in parallel (1 to 16). Two fit a 2 GB VM. |
| `PODIUM_JUDGE_CACHE_DIR` | no | `/var/cache/podium-judge` | Cached tests and compiled programs. |
| `PODIUM_LOG_LEVEL` | no | `info` | As above. |

Variables added by later milestones (judge token, backups) appear here when their code lands.

## Health

- `curl -fsS http://127.0.0.1:8080/healthz` returns `{"status":"ok","db":"ok"}`, or HTTP 503 when the database is unreachable.
- Logs: `journalctl -u podium-server -u podium-judge -f`. They are JSON lines.

## Upgrade

```sh
tar xzf podium-<new>.tar.gz && cd podium-<new>
sudo ./install.sh app
```

Database migrations run automatically when the server starts. Configuration files are never overwritten.

## Backups

Litestream replication and nightly snapshots are set up in milestone M7. Until then, copy the database safely with `sqlite3 /var/lib/podium/db/podium.db ".backup /var/backups/podium-$(date +%F).db"`.
