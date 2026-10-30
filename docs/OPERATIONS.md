# Operations

How to install, configure, check, back up and upgrade podium on its VM, and how to run it on your own machine. What to do on a contest day, and the dry run with real students, are in `docs/RUNBOOK.md`.

Status (2026-10-30): podium has not run on the VM yet. The install, upgrade, rollback, restore, alert and load procedures here are written from the code and tested in WSL2; the VM drills have not run, so none of them is proven on the VM. Sections that depend on a drill say "not yet proven on the VM".

## Install

Target: Ubuntu 24.04 LTS on **amd64 or arm64**, cgroup v2 (the default), 2 GB RAM or more. A release holds binaries for one of them: `make release` builds amd64 (`podium-<version>.tar.gz`), `make release ARCH=arm64` builds arm64 (`podium-<version>-arm64.tar.gz`), and `install.sh` refuses a release built for another processor than the machine's (the `arch=` line of its `MANIFEST`). On arm64 isolate is built from source, since upstream publishes no arm64 package. For Oracle's free Ampere VM see "Oracle Always Free".

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh
```

`install.sh` is idempotent and runs these steps in order. Name steps to run only those, for example `sudo ./install.sh packages isolate`. A fresh install with nothing filled in yet always reaches the end: steps that need a value you have not set skip themselves with a message naming it, and the checklist at the end lists what to fill in.

| Step | What it does |
|---|---|
| `packages` | g++, OpenJDK 21, Python 3, Node 22 (NodeSource repository), sqlite3, jq |
| `isolate` | isolate from its upstream apt repository; mounts its box root `/var/lib/isolate` on a 1200 MB tmpfs (so files a program writes count against its memory limit); caps all sandboxes at 1100 MB through `isolate.slice`; enables `isolate.service`; makes the tuning below survive a reboot; runs `isolate-check-environment` |
| `testlib` | `testlib.h` (release 0.9.41) into `/usr/local/include` for custom checkers. Downloaded from GitHub at a pinned commit and refused unless its SHA-256 matches the one in the script. Skipped when the installed file already matches. |
| `users` | system users `podium`, `judge` and `podium-backup` (Litestream and the heartbeat, in group podium; D116); adds `judge` to group `isolate` (the isolate binary is executable only by that group) |
| `dirs` | `/var/lib/podium` and `/var/lib/podium/tests` (podium, 750), `/var/lib/podium/db` (podium, group podium, 2770, so Litestream can write; the database files are 660), `/var/cache/podium-judge` (judge, 750), `/etc/podium`; creates `server.env` and `judge.env` from the examples if missing (root, 600), with the absolute VM paths for the database and tests |
| `firewall` | `ufw`: inbound only ssh (the port sshd uses), 80 and 443 (TCP, and UDP for HTTP/3); ssh is allowed before the firewall is switched on. Warns when ssh still accepts passwords (see "Firewall and ssh") |
| `app` | installs both binaries, their systemd units and the alert unit; starts the services only once `server.env` and `judge.env` have no placeholder values left (it names the ones that remain), then waits for `/healthz` and writes `/usr/local/lib/podium/MANIFEST` |
| `swap` | 1 GB swap file, `vm.swappiness=10` |
| `litestream` | Litestream 0.5.16 into `/usr/local/bin`, downloaded from its GitHub release and refused unless its SHA-256 matches the one in the script; `/etc/litestream.yml`; creates `/etc/podium/litestream.env` from the example if missing (root, 600); the `podium-litestream` unit, started only once that file has no placeholder values (see "Backups") |
| `backup` | `rclone` from Ubuntu's archive (podium-check lists the Litestream bucket with it); the nightly backup and restore scripts in `/usr/local/lib/podium`; `/var/backups/podium` (podium, 700); creates `/etc/podium/backup.env` from the example if missing (root, 600); `podium-backup.timer` (about 03:15 UTC) |
| `alerts` | `podium-alert` and `podium-check` in `/usr/local/lib/podium`, the `podium-alert@.service` template, `podium-check.timer` (every 15 minutes); creates `/etc/podium/alert.env` from the example if missing (root, 600). See "Alerts" |
| `caddy` | last on purpose. Skips with a message while `PODIUM_BASE_URL` is a placeholder. Then checks that the host name resolves to this VM (a local address, or the public address the provider's NAT shows, asked once from `checkip.amazonaws.com`) and skips with what to fix when it does not, because Let's Encrypt allows only a few failed attempts an hour. Otherwise installs Caddy and writes `/etc/caddy/Caddyfile` for that host: admin API off, request bodies capped at 33 MB, then restarts Caddy |

Commands besides the steps:

| Command | What it does |
|---|---|
| `sudo ./install.sh upgrade` | installs this release over the running one; see "Upgrade" |
| `sudo ./install.sh rollback` | puts the previous release back; see "Rollback" |
| `sudo ./install.sh verify` | checks the installed system and prints `ok`, `warn` or `FAIL` per item; exits 1 on any `FAIL`. See "Verify the installation" |

The `app` step and every upgrade also install a copy of `install.sh` as `/usr/local/lib/podium/install.sh`, so `verify` and `rollback` work from any folder: `sudo /usr/local/lib/podium/install.sh verify`.

`isolate-check-environment` may warn about simultaneous multithreading, address-space randomisation, transparent huge pages and the core file pattern. These affect timing reproducibility, not safety. The `isolate` step settles two of them for good: `/etc/tmpfiles.d/podium-isolate.conf` turns transparent huge pages off at every boot, and `/etc/sysctl.d/91-podium-isolate.conf` sets `kernel.core_pattern=core` and `fs.protected_hardlinks=1`. Ubuntu's `apport` rewrites the core pattern at boot (and its crash reports would copy a program's memory to disk), so the step disables it. Address-space randomisation stays on: safety beats timing reproducibility. Simultaneous multithreading is the provider's choice; on a 2-vCPU VM leave it.

### Verify the installation

`sudo ./install.sh verify` reads the system and changes nothing. It prints names, never values.

| Area | Checked |
|---|---|
| Services | `podium-server`, `podium-judge`, `isolate`, the isolate tmpfs, `podium-backup.timer`, `podium-check.timer` active; `podium-litestream` configured and active; `caddy` active once `PODIUM_BASE_URL` is real |
| Endpoints | `/healthz` answers ok; the judge API answers `401` without a token |
| Network | `ufw` active and allowing only ssh, 80 and 443; ports 8080 and 8081 listen on loopback only |
| Configuration | `PODIUM_MODE` is production in both service files; every `/etc/podium/*.env` exists, is root 600 and has no placeholder value (names listed); `PODIUM_ALERT_WEBHOOK` set; `/var/backups/podium` podium 700. Warning: no heartbeat URL |
| Clock | `chrony` or `systemd-timesyncd` active and the clock synchronised: every freeze, end and extension is a clock comparison, so verify fails without it |
| Sandbox host | transparent huge pages off (warning), the installed release from `MANIFEST`, compilers and runtimes held (warning when not, see "Holding the compilers") |
| Backup keys | `podium-litestream` runs as `podium-backup`; the database folder is group podium, mode 2770 |

Run it after a fresh install, after every upgrade (the upgrade runs it itself) and before a contest.

The judge unit runs `podium-judge work`, which pulls jobs from the server's judge API. It needs `PODIUM_JUDGE_TOKEN` in both `server.env` and `judge.env` (see "Judge token" below).

## Oracle Always Free

Podium's chosen host (V3-28): one Oracle Cloud VM at no cost. Written from the code and from Oracle's pages as read on 2026-10-29; not yet proven on the VM.

| Piece | Setting |
|---|---|
| Shape | `VM.Standard.A1.Flex`, 2 OCPU and 12 GB (the whole Always Free ARM allowance), in the account's home region |
| Image | Canonical Ubuntu 24.04, aarch64, not Minimal; boot volume 100 GB, attachment paravirtualized (the default) |
| Release | `make release VERSION=vX.Y.Z ARCH=arm64`, then copy `release/podium-vX.Y.Z-arm64.tar.gz` and its `.sha256` |
| Sandbox suite | `make sandbox-binaries ARCH=arm64` builds `server/bin/calib.test` and `worker.test` for the VM ("Verify the sandbox on the VM") |
| Network | the subnet's security list: ingress 80/tcp, 443/tcp and 443/udp from `0.0.0.0/0`, beside the default 22/tcp; the image's own iptables rules are handled by `install.sh firewall` ("Firewall and ssh") |
| Name | a DuckDNS subdomain with an A record to the VM's public IP; `PODIUM_BASE_URL=https://<name>.duckdns.org` |

Replacing the image's iptables rules with ufw also drops Oracle's rules for its iSCSI boot volumes. They matter only for an iSCSI attachment; keep the boot volume paravirtualized.

### Idle reclaim and the watchdog

Oracle may reclaim an Always Free VM that is idle for 7 days: CPU at the 95th percentile under 20 percent, network under 20 percent and, on ARM, memory under 20 percent. Its page does not say whether reclaim stops or deletes the VM. Podium is idle between contests, so it qualifies. A Pay As You Go account is not reclaimed (and costs nothing inside the free limits); without one, the watchdog and the backups cover both cases:

| Case | What notices | What happens |
|---|---|---|
| Oracle stops the VM | `.github/workflows/watchdog.yml`, every 30 minutes | `tools/oci-watchdog.sh` starts it through Oracle's API and posts to ntfy |
| The site stops answering | the same run checks `/healthz` | the heartbeat service alerts |
| Oracle deletes the VM | the same run gets "not found" | ntfy and the heartbeat alert; rebuild below |
| The watchdog stops running | the heartbeat service hears nothing | it alerts. GitHub pauses schedules in a repository with no activity for 60 days: push something, or re-enable the workflow under Actions |

The watchdog signs its requests with curl and openssl, no third-party action. It needs, under the repository's Settings, Secrets and variables, Actions:

| Secret | Value |
|---|---|
| `OCI_TENANCY_OCID`, `OCI_USER_OCID`, `OCI_FINGERPRINT`, `OCI_REGION` | the watchdog user's API key details (Identity, Users, the user, API keys) and the region, such as `ap-hyderabad-1` |
| `OCI_INSTANCE_OCID` | the VM's OCID |
| `OCI_PRIVATE_KEY` | the API key's private PEM, whole |
| `WATCHDOG_PING_URL` | a healthchecks.io check: period 30 minutes, grace 1 hour, with email and ntfy integrations |
| `WATCHDOG_NTFY_URL` | optional: the ntfy topic URL for "started the VM" |
| Variable `PODIUM_URL` | `https://<name>.duckdns.org`. Set it last: the job is skipped while it is empty |

The watchdog user belongs to a group of its own with only these policy statements (in the VM's compartment):

```
Allow group podium-watchdog to read instances in compartment <compartment>
Allow group podium-watchdog to use instances in compartment <compartment> where request.permission = 'INSTANCE_POWER_ACTIONS'
```

`tools/oci-watchdog-test.sh` checks the script against a local stand-in for Oracle's API that verifies every signature (`tools/fake-oci.py`); the first manual run of the workflow (Actions, watchdog, Run workflow) is the check against Oracle itself.

### Buckets

| Copy | Provider | Settings |
|---|---|---|
| Litestream | Backblaze B2 ("Set up the bucket") | as below, with the lifecycle setting "Keep prior versions for this number of days: 7", so a replica deleted from a taken-over VM can still be brought back for a week |
| Offsite nightly | Oracle Object Storage | a private bucket in the root compartment (the S3 compatibility API's default), lifecycle rule "delete objects older than 30 days"; a user of its own whose group may only write and read objects in that bucket; a customer secret key of that user as `PODIUM_OFFSITE_ACCESS_KEY_ID` and `PODIUM_OFFSITE_SECRET_ACCESS_KEY`; endpoint `https://<namespace>.compat.objectstorage.<region>.oraclecloud.com` |

The offsite user's policy, so a taken-over VM can add archives but not delete them:

```
Allow group podium-offsite to read buckets in tenancy where target.bucket.name = 'podium-nightly'
Allow group podium-offsite to manage objects in tenancy where all {target.bucket.name = 'podium-nightly', any {request.permission = 'OBJECT_CREATE', request.permission = 'OBJECT_INSPECT', request.permission = 'OBJECT_READ'}}
```

Oracle's free tier allows 20 GB and 50,000 requests a month; one archive a night uses a few requests. Watch the bucket's size against 20 GB: 30 archives of the database and test files must fit.

### Rebuild after the VM is gone

1. Create the VM again (table above) and point the DuckDNS name at its new IP.
2. Copy the release and install: `sudo ./install.sh`.
3. Put the five env files back in `/etc/podium/` (root, mode 600) from the owner's copy.
4. `sudo systemctl stop podium-server podium-litestream`, then restore the test files and last night's database from the offsite bucket: `sudo /usr/local/lib/podium/podium-restore offsite /var/tmp/night`. For a newer database (up to 10 s before the VM went), restore it from Litestream with those test files: `sudo /usr/local/lib/podium/podium-restore litestream /var/tmp/restore /var/tmp/night/tests`. Put the result in place as in `docs/RUNBOOK.md`, "Restoring from a backup".
5. `sudo ./install.sh app caddy`, then `sudo ./install.sh verify`.
6. Update `OCI_INSTANCE_OCID` in the repository's secrets for the watchdog.

## Verify the sandbox on the VM

After installing, and after every isolate, kernel or compiler upgrade, run the self-test. It needs no Go toolchain and changes nothing:

```sh
sudo systemctl stop podium-judge
sudo -u judge env PODIUM_JUDGE_CACHE_DIR=/tmp podium-judge selftest
sudo systemctl start podium-judge
```

It prints `PASS` or `FAIL` for each of the 17 hostile programs and a hello world in each of the four languages, then `PASS: all 21 programs` and exit 0, or the count that failed and exit 1. Do not judge on the host until it passes. Stopping the worker first keeps the test's box out of the worker's memory budget; it uses box 80, clear of the lanes.

For the timing tables as well, with the worker stopped (calibrate has its own memory budget and must not overlap the worker's boxes):

```sh
sudo systemctl stop podium-judge
sudo -u judge podium-judge calibrate
sudo systemctl start podium-judge
```

It runs the same hostile programs (fork bomb, 2 GB allocation, endless loop and output, `/etc/shadow`, network, compile bomb, Java and Python memory, Node threads, symlink and FIFO tricks on the output file, filling `/tmp`) and prints timing tables. It exits non-zero if any program escapes its limits. Do not run contests until it passes.

On a development machine, `make sandbox-test` runs the same suite plus the validation acceptance test: it validates the three example packs in `problems/` on real isolate, breaks one expected output and checks that validation fails naming that test, and checks that a hostile custom checker is contained.

## Run modes

Both services read `PODIUM_MODE`. A missing value means `production`, so a forgotten setting is strict, never lax.

| | `testing` | `dev` | `production` (default) |
|---|---|---|---|
| Use it for | trying podium, automated end-to-end tests, demos on your own machine | local work with real Google sign-in | the VM |
| Sign-in | a form: any email, any role, no password | Google | Google |
| Google client | not read | required | required |
| `PODIUM_BASE_URL` | optional, default `http://localhost:5173`, must be a localhost address | required, http allowed | required, must be `https` |
| `PODIUM_LISTEN` | must be loopback | any | must be loopback |
| `PODIUM_OWNER_EMAIL` | optional, default `owner@podium.test` | required | required |
| `PODIUM_SESSION_SECRET` | optional, random for each run | required | required |
| `PODIUM_JUDGE_TOKEN` | optional, public testing token when empty | optional, public testing token when empty | required, testing token refused |
| Banner in the web app | "Testing mode: sign-in is off..." on every page | none | none |

Testing mode lets anyone who can reach the server sign in as an owner, which is why the server refuses to start in that mode unless both the base URL and the listen address are on this machine. The public testing judge token is in the source code; it pairs a local server and judge without setup, the judge API only ever listens on loopback, and production refuses it.

Testing mode refuses any request whose `Host` is not localhost, 127.0.0.1 or [::1], and any request carrying `X-Forwarded-For`, `Forwarded`, `X-Real-IP` or `X-Forwarded-Host`, so Caddy in front of it never works. Test sign-in also needs an `Origin` equal to the base URL. The server stores the mode it last ran in; when it starts in a different mode it ends every session, and when the new mode is dev or production it makes every admin and owner without a linked Google account a student again (except `PODIUM_OWNER_EMAIL`), logging `run mode differs from the database's last start` with the counts. Roles granted to people who have not yet signed in with Google are among them: grant them again.

## Run locally without any keys

`make dev-test` starts the whole stack in testing mode: no Google client, no secrets, no `.env` file.

| Part | Where |
|---|---|
| `podium-server` | `127.0.0.1:8080`, judge API on `127.0.0.1:8081` |
| `podium-judge work` | 2 lanes, pulling from the judge API with the testing token |
| Vite dev server | http://localhost:5173, proxies `/api` and `/healthz` to the server |
| State (database, tests, judge cache) | `/var/tmp/podium-dev` in Linux (WSL2 on Windows); set `PODIUM_DEV_STATE` to move it |
| CPUs | all; `PODIUM_DEV_CPUS` (a `taskset` list such as `0-1`) pins both services, to measure as on the 2-vCPU VM. A development override only, never set on the VM |

How it runs:

- It cross-compiles both binaries for Linux and runs `tools/dev-stack.sh` as root (through WSL2 on Windows, `sudo` on Linux). The script drops to the `podium` user for the server and the `judge` user for the worker, as on the VM. Vite runs natively and reaches the server through WSL2's localhost forwarding.
- State stays outside the repository because SQLite's WAL needs a local Linux filesystem, not `/mnt/<drive>`. Delete `/var/tmp/podium-dev` to start again from an empty database.
- Prerequisite, once: the judge setup in WSL2 (`deploy/install.sh packages isolate testlib users`, see `README.md`).
- Ctrl+C stops everything.

Signing in: open http://localhost:5173, enter any email, an optional name and a role (Student, Admin or Owner), and choose "Sign in for testing". Signing in again with the same email changes that person's role. `owner@podium.test` (or `PODIUM_OWNER_EMAIL` if set) is always an owner. Every page shows the testing-mode banner so nobody mistakes the stack for a real contest.

Live browser tests: with `make dev-test` running and the three example packs imported and validated (zip each folder in `problems/` and use "Import pack"), run `npm run test:live` in `web/`. It signs in from the form, opens Problems and reads a real validation report through Vite's proxy, with nothing mocked. It reuses the Vite server on port 5173, because the testing-mode base URL is `http://localhost:5173` and the cross-site check refuses any other origin.

A second stack beside your own, for example for a walkthrough or a live run that must not touch yours, uses other ports and its own state. These are development overrides, never set on the VM:

| Variable | Read by | Example |
|---|---|---|
| `PODIUM_DEV_STATE`, `PODIUM_LISTEN`, `PODIUM_JUDGE_LISTEN` | `tools/dev-stack.sh` | `/var/tmp/podium-walk`, `127.0.0.1:8097`, `127.0.0.1:8098` |
| `PODIUM_BASE_URL` | `tools/dev-stack.sh`, passed to the server | `http://localhost:5187` |
| `PODIUM_DEV_API` | `web/vite.config.ts`, the proxy target | `http://127.0.0.1:8097` |
| `PODIUM_LIVE_PORT` | `web/playwright.live.config.ts` | `5187` |

Start the stack with those variables (in WSL2: `PODIUM_DEV_STATE=... PODIUM_LISTEN=... PODIUM_JUDGE_LISTEN=... PODIUM_BASE_URL=... bash tools/dev-stack.sh server/bin/linux` as root), Vite with `PODIUM_DEV_API=... npx vite --port 5187 --strictPort`, and the live tests with `PODIUM_LIVE_PORT=5187 npm run test:live`. Both stacks share isolate's boxes 0 and 1, so judge on one at a time: a box in use by the other judge fails the submission with a judge error, and "Judge again" on the console repeats it.

## Configuration

All configuration comes from environment variables. On the VM they live in `/etc/podium/server.env` and `/etc/podium/judge.env` (root-owned, mode 600), read by systemd through `EnvironmentFile=`. Locally they live in `server/.env` and `judge/.env`, which git ignores. Each service checks every variable before opening a port and exits with a message naming what is missing and which file to fix. Logs show each variable as `set`, `default` or `missing`, never its value.

### podium-server (`/etc/podium/server.env`)

Listed in the order the server reads them. The testing, dev and production columns say what each run mode needs.

| Variable | testing | dev | production | Default | Purpose |
|---|---|---|---|---|---|
| `PODIUM_MODE` | | | | `production` | `testing`, `dev` or `production` (see "Run modes"). |
| `PODIUM_BASE_URL` | optional | required | required, https | testing: `http://localhost:5173` | Public URL students open, for example `https://contest.example.edu`. Scheme and host only. Used for sign-in redirects, cookie flags (`https` makes the session cookie `Secure` and `__Host-` prefixed), the cross-site check and the Caddy host. |
| `PODIUM_LISTEN` | loopback only | optional | optional, loopback only | `127.0.0.1:8080` | Address the HTTP server binds; Caddy proxies to it. Production refuses any address that is not loopback: Caddy is the only public listener, and the server trusts `X-Forwarded-For` only from a loopback peer (see "Behind Caddy"). |
| `PODIUM_JUDGE_LISTEN` | optional | optional | optional | `127.0.0.1:8081` | Address of the judge worker API. Must be loopback in every mode and differ from `PODIUM_LISTEN` (see "Judge API listener"). |
| `PODIUM_DB_PATH` | optional | optional | optional | `data/podium.db` | SQLite file. On the VM use `/var/lib/podium/db/podium.db`. |
| `PODIUM_TESTS_DIR` | optional | optional | optional | `data/tests` | Test files and program sources by SHA-256. On the VM use `/var/lib/podium/tests`. |
| `PODIUM_BACKUP_DIR` | optional | optional | optional | `/var/backups/podium` | The nightly backups' folder, the same as in `backup.env`. The server only reads the time of the newest backup from its `latest` link, for the Dashboard's Live tab. |
| `PODIUM_OWNER_EMAIL` | optional | required | required | testing: `owner@podium.test` | Your Google account. Always an owner and always on the sign-in list (the server puts it back at every start), and shown as the organiser to contact. The recovery path if every other owner is removed or the list locks you out. |
| `PODIUM_SESSION_SECRET` | optional | required | required | testing: random per run | At least 32 random characters (`openssl rand -base64 48`). Signs the sign-in state cookie. Changing it only interrupts sign-ins in progress. Secret. |
| `GOOGLE_CLIENT_ID` | not read | required | required | | OAuth client ID from the Google Cloud console (see below). |
| `GOOGLE_CLIENT_SECRET` | not read | required | required | | That client's secret. Secret. |
| `PODIUM_JUDGE_TOKEN` | optional | optional | required | testing and dev: the public testing token | Shared token for the judge API, at least 32 characters (`openssl rand -hex 32`). The same value goes in `judge.env`. Production refuses the testing token. Secret. |
| `PODIUM_LOG_LEVEL` | optional | optional | optional | `info` | `debug`, `info`, `warn` or `error`. |

The AI wizard's provider key is deliberately not a variable: an admin pastes it in the web app and the server holds it in memory only (see "AI wizard keys").

### Google sign-in

Needed in dev and production mode only.

1. In the [Google Cloud console](https://console.cloud.google.com/), create or pick a project, then open APIs and Services, OAuth consent screen. For a college Google Workspace choose "Internal" (only your domain's accounts can sign in); otherwise "External", and add the scopes `openid`, `email`, `profile`.
2. Credentials, Create credentials, OAuth client ID, application type "Web application".
3. Authorised redirect URI: `<PODIUM_BASE_URL>/api/auth/google/callback`, for example `https://contest.example.edu/api/auth/google/callback`. For local work add `http://localhost:5173/api/auth/google/callback` too.
4. Put the client ID and secret in `server.env` and restart the server.

**The OAuth app's publishing status must be "Internal" or "In production", never "Testing".** In Testing, Google lets only the test users you list sign in (at most 100) and the sign-ins expire after 7 days, so most students would be refused on contest day. Check it under APIs and Services, OAuth consent screen (Google Auth Platform, Audience):

| Status | Use it when | Notes |
|---|---|---|
| Internal | the college runs Google Workspace and the project belongs to that Workspace | only accounts of that Workspace can sign in; no Google review. The usual choice |
| In production (External) | students sign in with accounts outside one Workspace | choose "Publish app". With only `openid`, `email` and `profile` Google does not require a verification review, but the consent screen shows the app name you set |
| Testing | never on the VM | 100 listed test users, 7-day sign-ins |

The real-domain sign-in drill (a non-owner student account on the sign-in list signing in over HTTPS and submitting) is the proof; it is one of the drills in `docs/RUNBOOK.md`, "Proving the safety net".

Who can sign in: anyone whose verified Google email is on the list of people in Settings, People (V4-15), and is not disabled; `PODIUM_OWNER_EMAIL` is always on it. Gmail addresses match however their dots are placed. A refused address is kept (the 500 most recent) so an owner or an admin can let it in with one click. Removing someone who signed in disables the account and ends their sessions at once.

`PODIUM_BASE_URL` must be just the scheme and host (no path); podium runs at the root of its own host name.

### Sessions and shared lab PCs

- The session cookie has no expiry, so the browser forgets it when it closes. The server ends every session 12 hours after sign-in, however much it is used (a constant, `auth.SessionTTL`; there is no variable for it), and at sign-out.
- Google sign-in asks Google for a sign-in from the last 5 minutes (`prompt=select_account`, `max_age=300`) and refuses an ID token whose `auth_time` is older than 5 minutes, so the previous student's Google session on a lab PC is not one click away.
- In labs, give each slot a browser guest profile, or a kiosk profile that is wiped on exit, and ask students to sign out of Podium and of Google before they leave.
- To sign someone out everywhere (a lost laptop, a PC left signed in), use Settings, Sign-in tab, End sessions. Disable account also stops them signing in until an owner enables it. Both act at once on requests; open live updates stop within 25 s. `POST /api/admin/accounts/reset-google` is no longer the only kill switch.

### Recovering access

If you are locked out (every owner removed, or the domain list emptied), set `PODIUM_OWNER_EMAIL` to your address and restart: that account is made an owner again at start-up. Sessions can be ended for everyone with `sqlite3 /var/lib/podium/db/podium.db "DELETE FROM sessions"`.

Each account is linked to the Google account that first signed in with it. If someone sees "This email is linked to a different Google account" after their address legitimately moved to a new Google account, reset the link in Settings, Sign-in tab ("Reset Google link"). If that someone is you, the configured owner, use `sqlite3 /var/lib/podium/db/podium.db "UPDATE users SET google_sub = NULL WHERE email = 'you@college.edu'"`.

### podium-judge (`/etc/podium/judge.env`)

Listed in the order the judge reads them, with the same mode columns. `podium-judge calibrate` reads only `PODIUM_ISOLATE_PATH`, `PODIUM_JUDGE_LANES`, `PODIUM_JUDGE_CACHE_DIR` and `PODIUM_LOG_LEVEL`.

| Variable | testing | dev | production | Default | Purpose |
|---|---|---|---|---|---|
| `PODIUM_MODE` | | | | `production` | As on the server. |
| `PODIUM_SERVER_URL` | optional | optional | optional | `http://127.0.0.1:8081` | The server's judge API (its `PODIUM_JUDGE_LISTEN`) as a URL with no path. For a remote judge, the local end of the tunnel. |
| `PODIUM_JUDGE_TOKEN` | optional | optional | required | testing and dev: the public testing token | The server's `PODIUM_JUDGE_TOKEN`. Production refuses the testing token. Secret. |
| `PODIUM_ISOLATE_PATH` | optional | optional | optional | `/usr/bin/isolate` | The isolate binary. |
| `PODIUM_JUDGE_LANES` | optional | optional | optional | `2` | Sandboxes judged in parallel (1 to 16). Two fit a 2 GB VM. Lane n uses isolate box `PODIUM_JUDGE_BOX_BASE` + n; a judge that finds its box busy waits up to about 4 s and then fails the job as a judge error. |
| `PODIUM_JUDGE_BOX_BASE` | optional | optional | optional | `0` | First isolate box id (0 to 984). Give a second judge on the same host a base past the first one's boxes, so neither cleans up or waits on the other's; the boxes must keep clear of box 80, which `selftest` and `calibrate` use. |
| `PODIUM_JUDGE_CACHE_DIR` | optional | optional | optional | `/var/cache/podium-judge` | Downloaded tests and sources, per-job work files and compiled programs (see "Judge cache"). |
| `PODIUM_JUDGE_BLOB_CACHE_MB` | optional | optional | optional | `2048` | Cap on `blobs/` in the cache directory, in MB, 64 to 1048576. Above it the least recently used files no running job holds are removed after each job. Keep it above the largest problem's tests (every test of a problem is fetched to validate it). |
| `PODIUM_LOG_LEVEL` | optional | optional | optional | `info` | As above. |

### Litestream (`/etc/podium/litestream.env`)

Read by the `podium-litestream` unit; template `deploy/litestream.env.example`. A start-up check (`/usr/local/lib/podium/check-litestream-env`) logs `set` or `missing` for each variable and refuses to start while any is empty or `change-me`. All are required.

| Variable | Purpose |
|---|---|
| `LITESTREAM_ACCESS_KEY_ID` | Key ID of an application key limited to the backup bucket, read and write. Backblaze B2: Application Keys, Add a New Application Key. Secret. |
| `LITESTREAM_SECRET_ACCESS_KEY` | That key's secret (B2 shows it once). Secret. |
| `PODIUM_BACKUP_BUCKET` | The bucket: private, encrypted, "keep only the last version". |
| `PODIUM_BACKUP_PATH` | Folder inside the bucket, for example `podium`. |
| `PODIUM_BACKUP_ENDPOINT` | The bucket's S3 endpoint with `https://`, for example `https://s3.us-west-004.backblazeb2.com` (B2 shows it on the bucket's page). |
| `PODIUM_BACKUP_REGION` | The region in that endpoint, for example `us-west-004`. |

Any S3-compatible store works the same way (Cloudflare R2, Wasabi, AWS S3): set its endpoint and region.

### Nightly backup (`/etc/podium/backup.env`)

Read by `podium-backup.service`; template `deploy/backup.env.example`. Every variable is optional. At start the script prints each one as `set`, `default` or `missing`, never a value, and stops with a message naming the variable and this file when one is wrong (a bad number of days, a heartbeat URL that is not `https://`).

| Variable | Default | Purpose |
|---|---|---|
| `PODIUM_DB_PATH` | `/var/lib/podium/db/podium.db` | the live database to copy |
| `PODIUM_TESTS_DIR` | `/var/lib/podium/tests` | the live test files and sources |
| `PODIUM_BACKUP_DIR` | `/var/backups/podium` | where each night's folder goes |
| `PODIUM_BACKUP_KEEP_DAYS` | `14` | nights kept |
| `PODIUM_SERVER_BIN` | `/usr/local/bin/podium-server` | the binary whose `check` verifies each backup |
| `PODIUM_HEARTBEAT_URL` | empty: no ping | an `https://` URL pinged after every fully successful run, for a dead-man check. Secret |
| `PODIUM_OFFSITE_BUCKET` | empty: no offsite copy | a second S3-compatible bucket, at another provider than Litestream's, that receives each checked night as `<stamp>.tar.gz` (database and test files, V3-30). Set it and the four below, or none |
| `PODIUM_OFFSITE_PATH` | `podium-nightly` | folder inside that bucket |
| `PODIUM_OFFSITE_ENDPOINT` | none | the bucket's `https://` S3 endpoint |
| `PODIUM_OFFSITE_REGION` | none | the bucket's region |
| `PODIUM_OFFSITE_ACCESS_KEY_ID`, `PODIUM_OFFSITE_SECRET_ACCESS_KEY` | none | a key that may read and write objects in that bucket and nothing else, no delete. Secret |

With the offsite bucket set, a night whose upload fails keeps its local folder, sends no heartbeat and fails the unit (so the failed-unit alert fires). A bucket named without its key, or a key without a bucket, is the same failure. Nothing offsite is deleted by the script: a lifecycle rule on the bucket ages archives out (see "Oracle Always Free").

### Alerts (`/etc/podium/alert.env`)

Read by `podium-alert@.service` and `podium-check.service`; template `deploy/alert.env.example`. `podium-check` also honours `PODIUM_ALERT_BIN` (the alert script to call, default `/usr/local/lib/podium/podium-alert`), an internal override for testing the checker on another machine; never set it on the VM.

| Variable | Default | Purpose |
|---|---|---|
| `PODIUM_ALERT_WEBHOOK` | empty: alerts fail | `https://` URL that takes a plain-text POST, for example `https://ntfy.sh/<long random topic>`. The topic name is the secret. Without it the alert unit fails, which `install.sh verify` reports |

### Judge cache

`PODIUM_JUDGE_CACHE_DIR` (owned by `judge`, mode 750) holds three directories. All of it can be deleted with the judge stopped; it refills on demand.

| Directory | Holds | Size |
|---|---|---|
| `blobs/` | tests, sources (including every submitted program) and custom inputs downloaded from the server, by SHA-256, re-hashed on download | at most `PODIUM_JUDGE_BLOB_CACHE_MB` (2 GB); when a job ends above it, the least recently used files go until it is under 90 % of the cap; files a running job holds are kept |
| `jobs/` | scratch files of running jobs (program outputs), removed when each job ends | small |
| `compiled/` | the compile cache: compiled programs and checkers, keyed by language, compile command and source | at most 500 MB, least recently used removed after every job; entries in use are kept |

### Judge token

1. Generate it once: `openssl rand -hex 32`.
2. Put the same value in `/etc/podium/server.env` and `/etc/podium/judge.env` as `PODIUM_JUDGE_TOKEN` (`sudoedit` each file).
3. `sudo systemctl restart podium-server podium-judge`.

To rotate it, repeat all three steps. A job the old worker held is handed out again once its lease runs out (60 s).

### Stopping the judge

`sudo systemctl stop podium-judge` is graceful: the worker stops claiming, every lane finishes and reports the job it holds, then it exits. Jobs take seconds; the unit waits up to 20 minutes (`TimeoutStopSec`) and signals only the worker, not the isolate runs it started (`KillMode=mixed`). A second stop signal, or the timeout, abandons the running jobs: the server hands each out again once its 60 s lease runs out, as after a crash. There is no global "pause judging" (pause is per contest), so stopping the judge is how an upgrade drains the queue's running work.

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

On the VM, limit that account's key to this one forward, so it can reach neither a shell nor the web listener on 8080 (where it could set `X-Forwarded-For` as if it were Caddy). In `~tunnel/.ssh/authorized_keys`:

```
restrict,port-forwarding,permitopen="127.0.0.1:8081" ssh-ed25519 AAAA... judge-machine
```

Over WireGuard, run the same forward to the server's WireGuard address; the listener itself stays on loopback. Never run judge workers on machines students can reach: the token is enough to report verdicts.

## Rate limits

Checked by the server before anything is queued. Every refusal is `429` with the wait in the message, in `retry_after_s` and in a `Retry-After` header.

| Limit | Value | Applies to |
|---|---|---|
| Pending submissions | one per user per problem (a queued rejudge does not count) | Submit |
| Heavy reads | 10 a second per user, bursts of 20 | the contest list and page, the contest workspace, the standings, the organisers' workspace, the integrity pages and the account lookup |
| Gap between submits | per user and problem: 5 s for organisers trying a problem; in a contest, its `min_gap_s` (default 30 s) | Submit |
| Submissions per problem | the contest's `max_per_problem` (default 15); none for practice | Submit in a contest (`400`) |
| Pending runs | one per user, across problems | Run |
| Gap between runs | 2 s per user | Run |
| After a compile that ran out of time or memory | that user's Run and Submit in the same contest are refused for 60 s; kept in memory, so a restart forgets it. The judge also caches that compile error for the same language and source for 10 minutes, so resending the same code does not hold the compile lock again. Rejudges do not count | Run, Submit |
| Draft saves | one per user every 2 s | drafts |
| Code | 64 KB, UTF-8 | Run, Submit, drafts |
| Custom input | 64 KB | Run |
| Request body | 160 KB | Run, Submit, drafts |
| Integrity events | one batch per user every 5 s, 100 per request; after 5,000 per user per problem, more are dropped | the editor's paste, blur, focus and full-screen exit events |
| Clarifications | 5 unanswered questions per student per contest (`400`) | Ask |
| AI requests | one running per admin, at most 3 minutes (`429 busy` with `Retry-After: 10`) | the AI wizard |
| Event streams | 4 per user (a fifth replaces the oldest, which is told and stops reconnecting), 5,000 in total (then `503`, and the page polls) | `GET /api/events` |
| Open requests | 8 at once per browser session, event streams aside (`429` with `Retry-After: 1`) | every API route with a session cookie |
| Time to send a request | headers within 10 s, the whole request within 15 s; 120 s for the routes that take an upload | every route; the long one for pack import and test replacement |
| Request body at the edge | 33 MB (Caddy refuses more with `413` before the server sees it) | everything through Caddy |
| Before sign-in | per client address: 2,500 at once refilled at 50 a second for `/healthz`, `GET /api/auth/config` and sign-out; 2,000 at once refilled at 20 a second for Google sign-in start and callback and test sign-in (a refused Google step lands on `/signin?error=rate_limited`). IPv6 addresses share one budget per /64; requests from the VM itself are not counted | the routes before sign-in |
| Unknown session cookies | per client address, 1,000 at once refilled at 10 a second, counted before the database is read; a session the server issued or accepted in the last minute is never refused, so forged cookies from one campus address cannot lock out the students behind it | every route with a session cookie |
| Sign-in attempts | each attempt (its state cookie) finishes once; a replayed callback lands on `/signin?error=expired` without a call to Google | the Google callback |

A retried Submit with the same `client_key` returns the first submission and is not counted again, so a flaky network does not trip these limits. Only the contest limits are configurable, per contest in its settings.

## Running a contest

All of this is in the web app, on the contest's tabs Setup, Live and Results; the routes are in `docs/API.md`. Only owners and admins organise contests.

**Before.** In Setup, add validated problems (each is pinned to the version you validated; adding, removing and reordering save at once), or use "Add to contest" in the problem editor. Every field saves as it changes. Six inputs show (title, problems, start, length in minutes, hidden minutes at the end, languages); the rest, the four exam options among them, sits under Advanced. Then publish with "Publish contest" in the title row: it asks first and lists the checks beside the form (problems validated, the start ahead, a language). An unpublished contest reads "Draft" on every organiser screen. Publishing refuses a problem that changed since it was added. Who may take part is decided by the list of people in Settings: anyone who can sign in can enter a published contest. The honour code students accept lists what is recorded and for how long, and, when exam options are on, which measures apply.

**During.** Live opens with one sentence on how it is going ("Live, 30 started, 1 question waiting, judge fine." or "Judge not seen for 3 minutes."; the second means no judge is working: check `podium-judge` on the VM), then Announce, Extend, Extra time, Pause judging, History of changes and If something goes wrong. Banners appear only when something needs action: an edited problem students have not got yet, tests that changed, a judge that is not taking work, judge errors. Questions sit beside the work from 1024 px and come first on phones. Every announcement asks before it reaches every student; after Pause, Resume, Extend and Judge again a suggested announcement waits in the box and is never posted by itself.

| Situation | Action | Effect |
|---|---|---|
| A typo or an unclear sentence in a statement | Edit the problem (a reason is required while the contest runs), then "Update students now" on Live, then post the offered announcement | Students keep the old statement until the update. No validation or rejudge is needed for a statement-only change. |
| A test or the checker is wrong | Pause judging, fix the problem (with a reason), validate it, "Update students now", Judge again for the problem, Resume | Pause holds the contest's submissions in the queue (nothing is lost; students can keep submitting). Until the contest is updated, run and submit on it answer "This problem is being updated by the organisers." Judge again re-runs every submission of the problem at priority 50 with the new tests, including those still waiting for the judge on the old tests; the board recomputes as results arrive (at most every 2 s). Pressing it twice adds nothing. |
| One submission looks wrongly judged | Judge again on its row | Only it is judged again. |
| The contest must stop now (a leak, a broken problem nobody can fix in time) | End now, in Live's title row | Asks first, then sets the end to the server's now: everyone's time ends at once, queued submissions are still judged, students see Ended (and practice opens when the contest has it). Students given extra time keep it; remove it first if it must end for them too. Ended before the hidden minutes, the board is not frozen; ended during them, the frozen moment stays where students saw it (rounded up to whole minutes, never to zero). Moving the end earlier in Setup does not do this. |
| Lost time for everyone (power cut, network) | Extend by N minutes | Everyone's end moves; open pages get the new end at once. Extensions add up. Refused once the contest has ended (`409 contest_ended`). |
| One student lost time (crashed laptop, accommodation) | Extra time for that student (0 removes it) | Only their end moves; give a reason for the record. |
| A question many students will have | Answer it for Everyone, or post an announcement | Arrives on every open page as a banner. |
| The judge is behind | Watch the opening sentence and the feed | Setter work (validation, generation) is limited to one lane while a contest is live; runs never take every lane from submissions. |

**After.** Students see the frozen board from the hidden minutes until everyone's time is up (including extra time), then the full board. To keep the result for a reveal, tick "Keep the hidden results hidden after the end until you reveal them" under Advanced before the end: students stay on the frozen board until you press Reveal on Results, which asks once. Results also offer Similar code (see "Integrity checks"), Download CSV (spreadsheet-safe: cells that would start a formula get a leading `'`; times in your browser's zone) and Download JSON. Practice opens only once every student's time is up. Duplicate, in Setup's More menu, runs a contest again with a new start.

**Retention.** Integrity evidence (paste, focus and full-screen events, second sessions, each submission's typed and pasted counts, full-screen exits and client hash) is deleted 180 days after a contest ended for everyone (D47). The server applies this a minute after start-up and every 12 hours and logs `integrity data past retention removed` with counts. Scores, submissions, code and integrity decisions are kept.

**Unused test files.** The same 12-hourly pass deletes files in `PODIUM_TESTS_DIR` that nothing refers to any more: tests replaced by new ones, old copies of reference solutions, checkers and generators, and runs' custom inputs. A file is kept while any problem's test, any submission or run, or any job that is waiting, running or less than 7 days old names it, and for a day after it was last saved; the judge's own uploads that were never used go after a day too. The log line is `unused test files removed` with counts and MB. Nightly backups keep their own hard-linked copies, so a file removed here is still in the backups taken before.

## Integrity checks

The organisers' guide, what each signal means and the policy template are in `docs/INTEGRITY.md`. Owners and admins run checks and record decisions.

**Running a check.** Once the contest is over for everyone, extra time included, open Similar code from the contest's Results and press "Check integrity" (before that the page says "Check integrity after the contest ends for everyone."). The check runs on the server in the background, not on the judge (D49), so judging is unaffected; a few hundred students take well under a minute, and the page shows "Checking" until it finishes. One check runs at a time on the whole server; a second press anywhere answers `409 already_running`, "A check is already running. Try again in a minute." Running it again replaces the contest's signals, for example after a rejudge; decisions stay.

| What you see | Cause | What to do |
|---|---|---|
| "The check could not finish." | An error while reading submissions or tests | The server log has `integrity check failed` with the `run_id`, `contest_id` and the error. Fix the cause and run it again. |
| "The server restarted during the check." | The server stopped while a check ran; at start-up it marks such checks failed and logs a warning with the count | Run it again. |
| No signals on a short problem | Programs under 40 tokens are skipped, and code everyone shares does not count | Expected: short problems cannot tell copying from the one natural solution. |

**Deciding.** Read the signal and, for similar code, the side-by-side view; look at both students' timelines. Download the similar pairs as CSV for the college's own process. Then record one decision per student, No action, Warning or Exclude from this contest, with a reason of at least 10 characters; a new decision replaces the old one. Exclude takes the student off the board and out of the export at once.

The server logs `integrity check started` and `integrity decision` at info with ids only (`by`, `contest_id`, `user_id`, `run_id`, `decision`); reasons and code never go to the log.

**Retention of checks.** The 12-hour pass also deletes the integrity runs and signals of contests that ended for everyone more than 180 days ago, the same cutoff as the events; a run still `running` is left for the next pass. The server logs `integrity checks past retention removed` with counts. Decisions and their reasons stay.

## AI wizard keys

The AI wizard ("New problem with AI", `docs/AUTHORING.md`) uses the organiser's own key for Anthropic, OpenAI or Google Gemini (D111). There is no variable for any of them and nothing to configure on the VM (decision D50).

| Provider | Where to get a key | The server calls |
|---|---|---|
| Anthropic | Claude Console (console.anthropic.com), API keys; a workspace with a spending limit is a good idea | `api.anthropic.com` |
| OpenAI | OpenAI platform (platform.openai.com), API keys; a project with a budget | `api.openai.com` |
| Google Gemini | Google AI Studio (aistudio.google.com), Get API key | `generativelanguage.googleapis.com` |

Adding a key:

1. In podium, signed in as an owner or admin, open "New problem with AI" on Problems. With no key yet the key form sits on the wizard itself; later, "Change key or model" opens it. Choose the provider and paste the key.
2. The server checks it by listing the provider's models, which costs no tokens, and offers the models that can write a problem (each provider's filter is in `docs/API.md`, "AI wizard (admin)"). Choose one, or type a model id the list does not show yet; the server checks that id with the provider's model lookup, also free.
3. The form shows "Key active until 14:32". A refusal says why in one sentence ("OpenAI refused the key. Check it and paste it again.", "Google says this key has used up its Gemini quota. ..."), never repeating the key.

A session can hold one key per provider; the last one saved is the one the wizard uses. No model is preselected: model names change faster than releases, so the list comes from the provider each time. Anthropic requests to the models that offer it have server-side refusal fallbacks switched on (beta `server-side-fallback-2026-07-01`): when the safety classifier refuses a request for a reason that does not apply to a fallback model, that model answers instead, and the answer may come from a different model than the one chosen (D54). OpenAI requests are sent with `store: false`.

| Stored | Not stored |
|---|---|
| The drafts the wizard creates (origin `ai`), like any problem | The key: held in the server's memory only, tied to the session that pasted it |
| A log line per request with user id, provider, model and outcome code | The key, the brief, the pasted text or the model's answer in any log; the key in any job payload, the database, backups, `cache/` or the judge |

- The key lasts 2 hours, then the form asks for it again. Signing out, signing in again in the same browser, "Forget key" in the account menu, or restarting the server forgets it at once.
- Each admin's key is their own: another admin's session cannot use it.
- One AI request per admin at a time, at most 3 minutes each.
- The server needs outbound HTTPS to the chosen provider's host (table above). If the VM's firewall blocks it, every request ends with "The provider is unavailable. Try again in a few minutes."
- The three provider clients use the standard library only. Nothing uses memory while idle.

Testing mode (`PODIUM_MODE=testing`, `make dev-test`) also offers a "Test provider (canned problems)": no key, no network, two canned problems whose programs really work on the judge (one about a maximum subarray when the brief or text mentions "subarray" or "stretch", a pair sums problem otherwise). Use it to try the whole pipeline (draft, inputs, outputs, validation). It does not exist in dev or production, and only builds with the tag `fakeai` have it (`make dev-test` builds that way; the release binary does not).

## Behind Caddy

| Setting | Where | Why |
|---|---|---|
| Admin API off (`admin off`) | global options of the Caddyfile | nothing on the VM can reconfigure Caddy over `localhost:2019`. A change to `/etc/caddy/Caddyfile` needs `sudo systemctl restart caddy`; `systemctl reload caddy` fails by design |
| `request_body { max_size 33MB }` | the site block | the largest body the server reads is a 32 MB pack; bigger ones stop at the edge |
| `reverse_proxy 127.0.0.1:8080` | the site block | the server listens on loopback only (production refuses anything else) |
| `X-Forwarded-For` | set by Caddy | Caddy has no `trusted_proxies`, so it ignores any `X-Forwarded-For` a client sends and sets the header to the client's own address. The server uses the header's last address, and only when the connection comes from loopback; from any other peer it uses the peer address. Sessions, integrity hashes and logs use that address |

What the loopback rule relies on: only Caddy connects to `127.0.0.1:8080`. Sandboxed programs have no network, the firewall drops outside traffic, and a remote judge's tunnel key is limited to port 8081 (see "Judge API listener"). Do not run other services on the VM that forward traffic to 8080.

## Firewall and ssh

`install.sh firewall` leaves only ssh, 80 and 443 open. Check with `sudo ufw status verbose`. Then make ssh key-only, once your key works (keep a second session open while you try):

```sh
echo 'PasswordAuthentication no' | sudo tee /etc/ssh/sshd_config.d/90-podium.conf
sudo sshd -t && sudo systemctl restart ssh
```

Your provider's own firewall, if it has one, should allow the same three ports.

Some cloud images load their own iptables rules at boot. Oracle's Ubuntu images do (netfilter-persistent): ssh, then a bare `REJECT` in INPUT and FORWARD, ahead of where ufw adds its rules, so 80 and 443 stay closed whatever ufw says. Installing ufw removes netfilter-persistent (ufw declares that it breaks it), so from the next boot only ufw's rules load; `install.sh firewall` also deletes the image's two `REJECT` rules from the running system, so the site answers without a reboot. `install.sh verify` fails when a `REJECT` sits ahead of http and https in `iptables -S INPUT`.

## Event stream behind Caddy

`GET /api/events` is a server-sent event stream that stays open as long as the page does. It needs no extra Caddy configuration: Caddy passes `text/event-stream` responses through without buffering, and the shipped Caddyfile also sets `flush_interval -1` and leaves `/api/events` out of compression. The server sends `event: clock` every 25 s, which keeps the connection from looking idle to any proxy in between.

Check it from the VM with a session cookie copied from the browser:

```sh
curl -N -H 'Cookie: __Host-podium_session=<value>' https://contest.example.edu/api/events
# event: clock
# data: {"server_now":1792183192400}
```

If clock lines arrive in bursts or only when the request ends, something between the browser and Caddy (a campus proxy or a CDN) is buffering; the page then falls back to polling every 15 s, which still works but costs more requests.

## Health

- `curl -fsS http://127.0.0.1:8080/healthz` returns `{"status":"ok","db":"ok"}`, or HTTP 503 when the database is unreachable.
- The judge API has no health route. `curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8081/api/judge/claim` prints `401` when the listener is up (it asks for the token).
- Logs: `journalctl -u podium-server -u podium-judge -f`. They are JSON lines. The worker logs `job started` and `job finished` per job; the server logs `job leased` and `job closed`.
- `podium-server check` checks a database and its test files without changing them (see "Checking a database").
- `sudo /usr/local/lib/podium/podium-check status` prints the 15-minute health checks (free space, WAL, the judge queue, Litestream) without alerting; `sudo ./install.sh verify` checks the whole installation.

## Alerts

Nothing extra stays in memory: systemd starts a small unit when something fails, and a timer runs a check every 15 minutes (D70). Alerts go to `PODIUM_ALERT_WEBHOOK` in `/etc/podium/alert.env`; ntfy is the chosen channel.

| Signal | How | Repeats |
|---|---|---|
| A unit failed | `OnFailure=podium-alert@%n.service` on `podium-server`, `podium-judge`, `podium-litestream`, `podium-backup` and `podium-check`. A crash, an OOM kill, a failed backup or a stop that timed out posts "podium-judge.service failed on <host>" with where to look. The unit still restarts | at most one per unit every 10 minutes; the next says how many were held back |
| Free space | below 2 x the database (with its WAL) plus 2 GB on the database's filesystem | every 6 hours while it lasts, then "resolved" |
| WAL size | `podium.db-wal` above 256 MB | as above |
| Judge stalled | jobs waiting more than 5 minutes while no judge has asked for work for 2 minutes, read from the server's `/healthz` on loopback (`jobs_queued`, `oldest_queued_s`, `judge_last_claim_s`). The judge itself stops after 5 refused tokens in a row (exit status 4, not restarted, so the unit failed alert fires too): `PODIUM_JUDGE_TOKEN` must be the same in `server.env` and `judge.env` | as above |
| Litestream | the unit is not running, the bucket cannot be listed, or the database changed more than 10 minutes after the bucket's newest upload | as above |
| Backup missed | the nightly backup pings `PODIUM_HEARTBEAT_URL` after a fully good run; the heartbeat service alerts on silence | the service's own rule |
| Site down | an external uptime check on `https://<host>/healthz` every 5 minutes, from a free hosted checker in your own account | the checker's own rule |

Set up and prove it once:

1. Pick a long random ntfy topic (`openssl rand -hex 16`), subscribe to it in the ntfy app on your phone, and put `PODIUM_ALERT_WEBHOOK=https://ntfy.sh/<topic>` in `/etc/podium/alert.env` (`sudoedit`).
2. `sudo /usr/local/lib/podium/podium-alert test`: the phone shows "podium: test alert".
3. Fire each alert on purpose (the drill list is in `docs/RUNBOOK.md`, "Proving the safety net"): kill a unit with `sudo systemctl kill -s KILL podium-judge`, run the restore proof against a damaged copy with `sudo /usr/local/lib/podium/podium-check restore-proof --corrupt`, stop Litestream, fill a scratch file until the disk rule trips.

`podium-check restore-proof` restores the bucket's newest database into a scratch folder with `litestream restore` and runs `podium-server check -hash` on it against the live test files. Run it by hand once a term and write down how long it took (V3-10); `--corrupt` overwrites the scratch copy's schema page before the check, so it must alert.

## Metrics

`GET /api/admin/metrics` (owners and admins) answers what to watch during a contest, from two indexed queries and in-memory counters, so polling it every few seconds costs nothing noticeable. The route is in `docs/API.md`.

| Field | Meaning |
|---|---|
| `queue.queued`, `queue.leased` | jobs waiting and jobs a worker holds, all kinds; `queue.kinds.<kind>` splits them (`submit`, `run`, `validate`, `generate`, `generate_inputs`) |
| `queue.queued_paused` | waiting jobs held back because their contest is paused |
| `queue.oldest_queued_s` | age of the oldest waiting job |
| `queue.kinds.<kind>.finished_last_minute` | jobs closed in the last minute: the judge's current throughput |
| `queue.submit_wait_p95_ms`, `queue.submit_total_p95_ms` | for submissions judged in the last 10 minutes: 95th percentile from Submit to the judge picking it up, and to the verdict |
| `queue.failed_last_5_min` | jobs that failed (a judge error, three lost leases, a deadline) |
| `queue.leased_jobs` | each held job: id, kind, worker, attempts, seconds since its first claim |
| `events.connections`, `events.users` | open event streams, and people with at least one |
| `db.in_use`, `db.waits`, `db.wait_ms` | SQLite connections busy now, and how often and how long requests have waited for one since the start |
| `process.heap_mb`, `process.sys_mb`, `process.goroutines` | the server's memory and goroutines |

The throughput, latency and failure figures are kept in memory and start again from zero when the server restarts.

From the VM, with a session cookie copied from a signed-in organiser's browser (developer tools, Application, Cookies, `__Host-podium_session`):

```sh
C='paste-the-cookie-value'
watch -n 10 "curl -s -H 'Cookie: __Host-podium_session=$C' http://127.0.0.1:8080/api/admin/metrics | jq -c '{queued: .queue.queued, leased: .queue.leased, per_min: ([.queue.kinds[].finished_last_minute] | add), wait_p95_s: (.queue.submit_wait_p95_ms / 1000), streams: .events.connections, heap_mb: .process.heap_mb}'"
```

With an `http` base URL (testing mode, local development) the cookie is named `podium_session` instead. The cookie is a full sign-in: do not leave it in shell history on a shared machine, and sign out in the browser afterwards to end it.

## Retention

How long the server keeps each kind of data. The periods are constants in `server/internal/retention/periods.go`; `GET /api/auth/config` returns them as `retention`, and the privacy page renders its table from that response. A test fails when this table and the code differ, so change both together.

| Id | Data | Kept | How it ends |
|---|---|---|---|
| `sessions` | Sign-in sessions | 12 hours after sign-in | hourly sweep |
| `integrity_events` | Paste and focus events during contests, their counts and similar-code checks | 180 days after the contest | retention pass every 12 hours |
| `run_payloads` | Custom input and program output of a Run | 30 days | retention pass every 12 hours; the run's verdict row stays |
| `backups` | Backups | 14 days | `podium-backup` (`PODIUM_BACKUP_KEEP_DAYS`) and the bucket's lifecycle rule |

Everything else (submissions, scores, code, problems and their versions, audit logs, integrity decisions with their reasons) is kept until an owner erases a person with `forget-user` or deletes the database. v3 has no multi-year deletion; add one when a college policy asks for it.

The retention pass starts a minute after the server and then runs every 12 hours, in batches of 2,000 rows per transaction, and logs one line (`data past retention removed`). A run job's payload (the code and custom input it named) becomes `{}` and its output excerpts are emptied; the next blob collection then removes the custom input files. A run older than 30 days shows its verdicts and no output.

## Site settings

The organisation details the sign-in page, the footer and the privacy page show: name, contact email, phone and privacy contact. An owner enters them on the Organisation tab of Settings (`PUT /api/admin/site-settings`); they are stored in the database's `settings` table, not in an environment variable, and every change writes a `settings_changed` row to the site audit. Until they are entered the pages show nothing for them: the footer and the sign-in page fall back to `PODIUM_OWNER_EMAIL` as the contact address. Enter a role address (for example `podium@college.edu`) as the contact email so the owner's own address stops appearing on the sign-in page. That address is how people reach the organisers: v3 has no contact form (V3-4).

What a visitor can read without signing in is listed in `docs/API.md` (`GET /api/auth/config`): the contact address, run mode, these settings and the retention periods. Nothing about contests.

## Erasing a person

A request to delete someone's data (a student who left, a privacy request to the contact address) is handled on the VM with `podium-server forget-user`. It is a dry run unless `-apply` is given:

```sh
sudo -u podium podium-server forget-user -db /var/lib/podium/db/podium.db -owner owner@college.edu asha@college.edu
# read the counts, then:
sudo -u podium podium-server forget-user -db /var/lib/podium/db/podium.db -owner owner@college.edu -apply asha@college.edu
```

It deletes the account's sessions, drafts and integrity events, clears the browser hashes on its submissions and the reasons given for its extra time (an accommodation can be health data), replaces its clarification questions with a removal note (the answers stay), replaces the name and email with `Removed user N` and `removed-user-N@removed.invalid` (N is the account id), clears the Google link and disables the account. Submissions and scores stay, so past standings stay correct under the placeholder. The site audit gets one `user_forgotten` row as the owner account (`-owner`, default `PODIUM_OWNER_EMAIL`), naming the account by id only, with the counts. Refused: `PODIUM_OWNER_EMAIL` itself, another owner unless `-allow-owner` is given (better: remove the role in Settings first), an account already forgotten, an address with no account, and any run without the configured owner's address: `sudo -u podium` does not load `/etc/podium/server.env`, so pass `-owner` with the `PODIUM_OWNER_EMAIL` value.

Not touched, by design, and listed by the command: submissions, scores and code (under the placeholder); integrity decisions about them and their reasons; audit rows written before the erasure, which keep the address and text they recorded; and backups until they age out (14 days). If the person signs in again with the same Google account, a new, empty account is made.

## Checking a database

```sh
sudo -u podium podium-server check -db /var/lib/podium/db/podium.db -tests /var/lib/podium/tests
```

It opens the database read-only and prints: SQLite's `integrity_check` and `foreign_key_check`, the migrations recorded against those this binary knows (a database written by a newer server is reported, not touched), row counts, and whether every test file and program source recorded in the database is on disk with the recorded size. `-hash` re-reads every file and compares its SHA-256 with its name. It also checks that every problem's working copy is recorded by its newest revision and every contest pin names a revision (`revisions: ok`); anything else means a write skipped its revision and is a bug to report. Exit status 0 means sound, 1 means something is damaged or missing (listed), 2 means the check could not run. The nightly backup and `podium-restore` run it on every copy they make. A database from before v3 makes the server stop with "this database predates v3": move it away or restore a v3 backup.

## Rehearsing a migration on a copy

`podium-server migrate -db COPY` applies the migrations of this binary to a copy of the database and runs `check` on it (exit codes as for `check`). It refuses the live database (the `PODIUM_DB_PATH` one or `/var/lib/podium/db/podium.db`) and any database with a `-wal` file beside it, which another process may have open. Use it before every schema-changing deploy (D68), with the new release's binary on a copy of the newest nightly backup.

## Runs with a custom input

`podium-server runs-report` prints, per contest, how many runs students made and how many used a custom input, as a Markdown table. It reads the run jobs' payloads, which the retention pass empties after 30 days: run it within 30 days of the contest.

```sh
sudo -u podium podium-server runs-report -db /var/lib/podium/db/podium.db
```

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
| The editor's status stays on making outputs | No worker is claiming jobs, as for validation | As the first row. Output jobs run after contest work, before validation. |
| Generate inputs ends with "The generator failed on test 7 (subtask 2, seed 3)" | The generator crashed, ran out of time or memory, or printed nothing for that seed and those arguments | Run the generator locally with the same argv (`python3 generator.py 3 n=1000`), fix it, save, and Generate inputs again. Nothing was applied, so no test changed. |
| Run or Submit says "This problem cannot be judged yet" | Tests outside subtasks, or tests without an expected output | Fix the listed items in the editor; Validate makes missing outputs first. |

## When to deploy

A deploy restarts the server. Do not deploy in the 48 hours before a published contest, while one runs, or while its submissions are still being judged; `docs/RUNBOOK.md` has the rule. Nothing enforces it: v3 removed the guard that did, and its override, because each served a case no single-college contest has met yet (V3-4).

## Upgrade

The first v3 release, `v3.0.0`, installs on a fresh VM (V3-12): nothing earlier was ever installed, and a database from before v3 is refused at start-up. Upgrades apply from the release after it.

```sh
sha256sum -c podium-<new>.tar.gz.sha256      # the checksum beside the tarball, from the GitHub release
tar xzf podium-<new>.tar.gz && cd podium-<new>
cat MANIFEST                                 # version, commit, arch, schema, migrations, protocol, upgrades_from
sudo ./install.sh upgrade
```

**What a release is.** `make release` (and the release workflow, on a pushed `v*` tag) puts two files in the tarball: `MANIFEST`, one `key=value` per line (`release`, `version`, `commit`, `built`, `arch`, `schema`, `migrations`, `protocol`, `upgrades_from`), and `SHA256SUMS`, the checksum of every other file. `podium-server -version` and `podium-judge -version` print the same version, commit, build date, schema and protocol, so you can match a binary to its release; `curl -s 127.0.0.1:8080/healthz` on the VM shows the running version and schema (through Caddy it does not).

**The release workflow.** Push a tag (`git tag vX.Y.Z && git push origin vX.Y.Z`; for a test of the workflow use `v0.0.1` and delete it afterwards, never a real version); `.github/workflows/release.yml` first checks that the `ci` workflow passed on the tagged commit (push the commit and wait for `ci` before pushing the tag), then runs `make release` and makes a draft GitHub release with the tarball, its `.sha256`, `MANIFEST` and `SHA256SUMS`. Review the draft and publish it by hand. `UPGRADES_FROM` in the `Makefile` is the oldest release this one upgrades from; raise it in a release that drops something older releases still need (a contract migration). `make release` refuses to build `v1.5.0` or later while it is still the placeholder `v1.0.0`, and `v3.0.0` is later: set it in the `Makefile` before the first tag.

From Windows, `tools/deploy.sh HOST` (or `make deploy HOST=user@vm`) does all of it: it runs `make release VERSION=<version>`, shows the tarball and its SHA-256 and waits for you to type the host name before anything leaves the machine, copies it to a new folder in `/tmp` with `scp`, and runs `install.sh upgrade` there. The version is the `vX.Y.Z` tag on the commit; a drill build names it instead with `VERSION=vX.Y.Z tools/deploy.sh HOST`. With neither, or with uncommitted changes, it stops before it builds anything. It does not check the contest calendar: see "When to deploy".

`install.sh upgrade` runs these in order and stops at the first problem, saying what was and was not changed:

1. **Release files, path and configuration names**: every file must match `SHA256SUMS`; the release must be built for this machine's processor; the installed version (from `/usr/local/lib/podium/MANIFEST`) must be at least the new release's `upgrades_from` and not newer than the new release (a downgrade is a rollback). An upgrade that stopped half-way is refused too: when the installed `podium-server` is not the binary `MANIFEST` names (the start failed after the install), or when `previous/MANIFEST` says this same release was already installed by an upgrade, a rerun would put the new binaries in `previous/` and lose the release to roll back to, so it stops before changing anything and names `sudo /usr/local/lib/podium/install.sh rollback`. Roll back first, fix the cause, then upgrade again. Then every variable the new example files list must be named in the matching `/etc/podium/*.env`. A missing required one stops the upgrade and is named with its file; a missing optional one is listed and keeps its default. Names only, never values.
2. **Judge drain**: `systemctl stop podium-judge`, graceful (see "Stopping the judge"): running jobs finish and report.
3. **Backup**: `podium-backup` runs now; the upgrade stops (and starts the judge again) unless a new local backup folder appeared.
4. **Keep the running release**: binaries, units and `MANIFEST` go to `/usr/local/lib/podium/previous/`, with the applied migrations, the backup folder and the time.
5. **Install**: binaries, units, scripts, Litestream configuration, backup and alert files of the new release.
6. **Start**: the server (migrations run now), wait up to 60 s for `/healthz`, then the judge; keep the release's `MANIFEST` as `/usr/local/lib/podium/MANIFEST`, with the install time, the binaries' hashes and the applied migrations added. If the judge's protocol differs from the server's (a half-done upgrade), the judge stops with "judge protocol N needed, this judge speaks M: install the same release on both" in `journalctl -u podium-judge`, and does not restart until you install one release on both.
7. **Verify**: `install.sh verify`, then it prints the backup folder and the rollback command.

Configuration files are never overwritten: add a new variable with `sudoedit` before running the upgrade again.

## Holding the compilers

A gcc, JDK, Python, Node or isolate update can change how fast a reference runs or, rarely, a verdict. Before a contest season, stop apt from moving them:

```sh
sudo ./install.sh hold        # apt-mark hold on the compilers, runtimes and isolate
apt list --upgradable         # they show as held back; apt upgrade leaves them
sudo ./install.sh unhold      # outside a contest season, to take their updates
```

After an update of any of them: `sudo systemctl restart podium-judge` (it reports the new toolchain at start, in the server log), then `podium-judge selftest` (above), then validate the problems of the next contest again from Problems. Holding also holds their security updates: unhold between seasons. `install.sh verify` warns while nothing is held.

## Rollback

`sudo ./install.sh rollback` uses what the last upgrade kept in `/usr/local/lib/podium/previous/` and works once per upgrade.

| Case | What happens |
|---|---|
| Same schema (the upgrade applied no migration) | stop the judge (gracefully) and the server, put the previous binaries and units back, start, wait for `/healthz`, verify |
| Newer schema (the upgrade applied a migration the previous server does not know, so it would refuse to start) | it names the backup taken just before the upgrade and what is lost: everything written since that backup, with the count of submissions and runs, plus sign-ins, drafts, clarifications and settings. It also counts the accounts erased with `podium-server forget-user` since that backup: the restore brings their data back, so after the rollback it writes the forget-user commands to run again to `/var/tmp/podium-rollback-forget-<time>.<random>.txt` (root only, since it holds addresses) and prints the name. You type `ROLLBACK` to go on. The backup is restored into a scratch folder and checked by the previous binary; the current database folder is kept as `db.before-rollback-<time>` (the nightly backup deletes it after `PODIUM_BACKUP_KEEP_DAYS`, 14 by default); the backup goes in place (folder group podium, mode 2770, files 660, so Litestream can write), missing test files are copied back, the previous binaries start, Litestream starts a new history |

Both cases are rehearsed on the VM before the first contest and before each schema-changing release (`docs/RUNBOOK.md`, "Proving the safety net"), with builds named so they never share a name with a real release (for example `v3.0.90` installed, `v3.0.91` with the same schema, `v3.0.92` with one extra migration file that is never committed). The new-schema build is made with `make package VERSION=v3.0.92 UPGRADES_FROM=v3.0.90`: `package` is `release` without `make check`, whose migration tests list the real migrations and fail on that tree by design. Not yet proven on the VM: the drill has not run.

## Backups

Three copies, all installed by `install.sh` (steps `litestream` and `backup`). The two buckets belong at two different providers, so one account problem never takes both:

| | Litestream | Nightly backup | Offsite nightly copy (V3-30) |
|---|---|---|---|
| What | the database | the database and the test files | the same night's folder, as one `<stamp>.tar.gz` |
| Where | the bucket in `/etc/podium/litestream.env` (off the VM) | `/var/backups/podium/<date>` on the VM (podium only, 700) | the bucket in `PODIUM_OFFSITE_*` of `/etc/podium/backup.env` (off the VM) |
| How often | every change, uploaded every 10 s | every night about 03:15 UTC, and on demand | right after each nightly backup passes its check |
| Kept | 14 days of history (a daily snapshot plus every change since) | 14 days | the bucket's lifecycle rule (30 days on Oracle's free tier) |
| Restores | the newest state, or any moment in the 14 days | that night's state | that night's state, with the test files |
| Protects against | losing the VM or its disk | a bad change (a wrong rejudge, a deleted contest), a damaged database | losing the VM and its test files; losing the Litestream provider |

Test files and program sources are not in the Litestream bucket: they live in `/var/lib/podium/tests`, named by SHA-256, and the database only refers to them. The nightly backup copies them as hard links, so 14 nights cost little more than one. Without the offsite copy they stay on the VM only (V3-10): losing the VM's disk loses every test file, and the problem packs have to be imported again. With it, `podium-restore offsite` brings back the database and the test files together. Submitted code is safe either way, since the database holds every submission's source.

The offsite key works like the Litestream one: the archive is made by `tar` as podium with an empty environment and streamed to `rclone` running as `podium-backup`, which alone holds the key. The script never deletes in that bucket, so its key can be one without delete permission, and a VM taken over cannot erase the archives.

Backups hold every student's code and the session table, so they are private: the backup runs with umask 077 (`UMask=0077` in the unit, `umask 077` in the script), `/var/backups/podium` is mode 700, and the files are readable by the podium user only. The judge cannot read them.

**The bucket keys are out of the server's reach (D116).** A process can read the environment of any other process of its own user from `/proc`, so a server taken over as user podium could read keys from a backup process running as podium. Litestream therefore runs as its own user, `podium-backup` (in group podium, so it can write the database folder, which is mode 2770; the server keeps the database files mode 660). The nightly backup unit runs as root and splits the work: the copy and the check run as podium with an empty environment, and only the heartbeat ping runs as `podium-backup` with the heartbeat URL. `install.sh upgrade` creates the user and moves Litestream's state to it.

Disk snapshots from your VM provider are a fine addition but not a replacement: they usually live in the same account as the VM.

### Set up the bucket (Backblaze B2)

1. Create a B2 account, then Buckets, Create a Bucket: private, default encryption on. In the bucket's lifecycle settings choose "Keep prior versions for this number of days" with 7 (Litestream keeps its own history and deletes what it no longer needs; the 7 days keep anything deleted from a taken-over VM recoverable for a week, at the cost of a week of old files).
2. Application Keys, Add a New Application Key: access to that bucket only, Read and Write. Copy the keyID and the applicationKey (shown once).
3. `sudoedit /etc/podium/litestream.env`: the key ID, the key, the bucket, a path such as `podium`, and the endpoint and region from the bucket's page.
4. `sudo systemctl restart podium-litestream`, then `journalctl -u podium-litestream -n 30`: the start-up check lists every variable as `set`, and Litestream logs its first snapshot and then a sync every 10 s while the database changes.
5. Prove it: `sudo /usr/local/lib/podium/podium-restore litestream /var/tmp/restore-check` restores the newest copy from the bucket and checks it; then `sudo rm -rf /var/tmp/restore-check`.

Litestream has only been rehearsed against a local file replica so far; the first restore from your bucket in step 5 is the real test; repeat it once a term with `podium-check restore-proof` (see "Alerts"). If the first sync fails with an error about checksums or a header the provider does not support, note the exact message: Litestream 0.5 has per-provider options (`sign-payload`, `require-content-md5`) under the replica in `/etc/litestream.yml`.

### Nightly backup

`podium-backup.timer` runs `/usr/local/lib/podium/podium-backup` as root, which splits the work between users podium and podium-backup (see "The bucket keys are out of the server's reach" above). Each run writes `/var/backups/podium/<UTC date and time>/` with:

- `podium.db`: an online copy (SQLite's backup API, safe while the server writes);
- `tests/`: hard links to every test file and source (a real copy if `/var/backups` is on another filesystem);
- `check.txt`: the output of `podium-server check` on the copy. A copy that fails its check is discarded and the unit fails.

It then points `latest` at the new backup and deletes those older than 14 days (`PODIUM_BACKUP_KEEP_DAYS` in `/etc/podium/backup.env` changes that).

| Task | Command |
|---|---|
| Back up now (before an upgrade, before a contest) | `sudo systemctl start podium-backup` |
| See the last run | `systemctl status podium-backup` or `journalctl -u podium-backup -n 20` |
| See when it runs next | `systemctl list-timers podium-backup.timer` |
| List backups | `ls -l /var/backups/podium` |

### Restore

`podium-restore` (installed by the `backup` step in `/usr/local/lib/podium`; `tools/restore.sh` in a checkout) restores into a new scratch directory and runs `podium-server check -hash` on the result; it never touches the live data.

```sh
R=/usr/local/lib/podium/podium-restore
sudo $R nightly /var/backups/podium/latest /var/tmp/restore     # a nightly backup
sudo $R litestream /var/tmp/restore                             # newest from the bucket
sudo $R litestream-at 2026-10-18T14:30:00Z /var/tmp/restore     # the bucket, at a moment
sudo $R offsite /var/tmp/restore                                # newest nightly archive offsite
sudo $R offsite /var/tmp/restore 2026-10-29T031500Z.tar.gz      # a named one
```

The offsite form reads `PODIUM_OFFSITE_*` from `/etc/podium/backup.env` (`PODIUM_BACKUP_ENV` names another file) and is the one to use when the VM is gone: on a new VM, install, copy `backup.env`, run it, then put the result in place. `tools/offsite-test.sh` rehearses the upload and this restore against a local stand-in bucket (`tools/fake-s3.py`), on Linux or WSL2, as a normal user.

The Litestream forms take the test files from `/var/backups/podium/latest/tests` unless a third argument names another folder. `LITESTREAM_CONFIG` and `LITESTREAM_ENV` name another Litestream configuration and environment file than `/etc/litestream.yml` and `/etc/podium/litestream.env`, for a restore on a machine laid out differently; never set them on the VM. Putting a restored copy in place of the live data is in `docs/RUNBOOK.md`, "Restoring from a backup".
