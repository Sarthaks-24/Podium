# Operations

How to install, configure, check, back up and upgrade podium on its VM, and how to run it on your own machine. What to do on a contest day, and the dry run with real students, are in `docs/RUNBOOK.md`.

## Install

Target: Ubuntu 24.04 LTS, cgroup v2 (the default), 2 GB RAM or more.

```sh
tar xzf podium-<version>.tar.gz && cd podium-<version>
sudo ./install.sh
```

`install.sh` is idempotent and runs these steps in order. Name steps to run only those, for example `sudo ./install.sh packages isolate`.

| Step | What it does |
|---|---|
| `packages` | g++, OpenJDK 21, Python 3, Node 22 (NodeSource repository), sqlite3, jq, rsync |
| `isolate` | isolate from its upstream apt repository; mounts its box root `/var/lib/isolate` on a 1200 MB tmpfs (so files a program writes count against its memory limit); caps all sandboxes at 1100 MB through `isolate.slice`; enables `isolate.service`; runs `isolate-check-environment` |
| `testlib` | `testlib.h` (release 0.9.41) into `/usr/local/include` for custom checkers. Downloaded from GitHub at a pinned commit and refused unless its SHA-256 matches the one in the script. Skipped when the installed file already matches. |
| `users` | system users `podium` and `judge`; adds `judge` to group `isolate` (the isolate binary is executable only by that group) |
| `dirs` | `/var/lib/podium/{db,tests}` (podium, 750), `/var/cache/podium-judge` (judge, 750), `/etc/podium`; creates `server.env` and `judge.env` from the examples if missing (root, 600), with the absolute VM paths for the database and tests |
| `app` | installs both binaries and systemd units; starts the services only once `server.env` no longer contains placeholder values |
| `firewall` | `ufw`: inbound only ssh (the port sshd uses), 80 and 443 (TCP, and UDP for HTTP/3); ssh is allowed before the firewall is switched on. Warns when ssh still accepts passwords (see "Firewall and ssh") |
| `caddy` | installs Caddy and writes `/etc/caddy/Caddyfile` for the host in `PODIUM_BASE_URL`: admin API off, request bodies capped at 33 MB, then restarts Caddy |
| `swap` | 1 GB swap file, `vm.swappiness=10` |
| `litestream` | Litestream 0.5.16 into `/usr/local/bin`, downloaded from its GitHub release and refused unless its SHA-256 matches the one in the script; `/etc/litestream.yml`; creates `/etc/podium/litestream.env` from the example if missing (root, 600); the `podium-litestream` unit, started only once that file has no placeholder values (see "Backups") |
| `backup` | the nightly backup script in `/usr/local/lib/podium`, `/var/backups/podium` (podium, 750), and `podium-backup.timer` (about 03:15 UTC) |

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
| `PODIUM_LISTEN` | loopback only | optional | optional, loopback only | `127.0.0.1:8080` | Address the HTTP server binds; Caddy proxies to it. Production refuses any address that is not loopback: Caddy is the only public listener, and the server trusts `X-Forwarded-For` only from a loopback peer (see "Behind Caddy"). |
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

The AI wizard's provider key is deliberately not a variable: an admin pastes it in the web app and the server holds it in memory only (see "AI wizard keys").

### Google sign-in

Needed in dev and production mode only.

1. In the [Google Cloud console](https://console.cloud.google.com/), create or pick a project, then open APIs and Services, OAuth consent screen. For a college Google Workspace choose "Internal" (only your domain's accounts can sign in); otherwise "External", and add the scopes `openid`, `email`, `profile`.
2. Credentials, Create credentials, OAuth client ID, application type "Web application".
3. Authorised redirect URI: `<PODIUM_BASE_URL>/api/auth/google/callback`, for example `https://contest.example.edu/api/auth/google/callback`. For local work add `http://localhost:5173/api/auth/google/callback` too.
4. Put the client ID and secret in `server.env` and restart the server.

Who can sign in: anyone whose verified Google email is on an allowed domain, anyone holding the admin or owner role (on any domain), and `PODIUM_OWNER_EMAIL`. Removing an admin ends their sessions at once; removing a domain ends the sessions of students on it.

`PODIUM_BASE_URL` must be just the scheme and host (no path); podium runs at the root of its own host name.

### Sessions and shared lab PCs

- The session cookie has no expiry, so the browser forgets it when it closes. The server ends every session 12 hours after sign-in, however much it is used (a constant, `auth.SessionTTL`; there is no variable for it), and at sign-out.
- Google sign-in asks Google for a sign-in from the last 5 minutes (`prompt=select_account`, `max_age=300`) and refuses an ID token whose `auth_time` is older than 5 minutes, so the previous student's Google session on a lab PC is not one click away.
- In labs, give each slot a browser guest profile, or a kiosk profile that is wiped on exit, and ask students to sign out of Podium and of Google before they leave.
- To sign someone out everywhere (a lost laptop, a PC left signed in), use People, "Sessions and access", End sessions. Disable account also stops them signing in until an owner enables it. Both act at once on requests; open live updates stop within 25 s. `POST /api/admin/accounts/reset-google` is no longer the only kill switch.

### Recovering access

If you are locked out (every owner removed, or the domain list emptied), set `PODIUM_OWNER_EMAIL` to your address and restart: that account is made an owner again at start-up. Sessions can be ended for everyone with `sqlite3 /var/lib/podium/db/podium.db "DELETE FROM sessions"`.

Each account is linked to the Google account that first signed in with it. If someone sees "This email is linked to a different Google account" after their address legitimately moved to a new Google account, reset the link on the People page ("Google account links"). If that someone is you, the configured owner, use `sqlite3 /var/lib/podium/db/podium.db "UPDATE users SET google_sub = NULL WHERE email = 'you@college.edu'"`.

### podium-judge (`/etc/podium/judge.env`)

Listed in the order the judge reads them, with the same mode columns. `podium-judge calibrate` reads only `PODIUM_ISOLATE_PATH`, `PODIUM_JUDGE_LANES`, `PODIUM_JUDGE_CACHE_DIR` and `PODIUM_LOG_LEVEL`.

| Variable | testing | dev | production | Default | Purpose |
|---|---|---|---|---|---|
| `PODIUM_MODE` | | | | `production` | As on the server. |
| `PODIUM_SERVER_URL` | optional | optional | optional | `http://127.0.0.1:8081` | The server's judge API (its `PODIUM_JUDGE_LISTEN`) as a URL with no path. For a remote judge, the local end of the tunnel. |
| `PODIUM_JUDGE_TOKEN` | optional | optional | required | testing and dev: the public testing token | The server's `PODIUM_JUDGE_TOKEN`. Production refuses the testing token. Secret. |
| `PODIUM_ISOLATE_PATH` | optional | optional | optional | `/usr/bin/isolate` | The isolate binary. |
| `PODIUM_JUDGE_LANES` | optional | optional | optional | `2` | Sandboxes judged in parallel (1 to 16). Two fit a 2 GB VM. |
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
| Pending submissions | one per user per problem (a queued rejudge does not count); not in exam mode, where a new submission queues behind the pending one | Submit |
| Heavy reads | 10 a second per user, bursts of 20 | the contest list and page, the contest workspace, the standings, the organisers' workspace, the integrity pages and a student's cases |
| Gap between submits | 5 s per user, across problems, for organisers trying a problem; in a contest, its `min_gap_s` (default 30 s) | Submit |
| Submissions per problem | the contest's `max_per_problem` (default 15); none for practice | Submit in a contest (`400`) |
| Pending runs | one per user, across problems | Run |
| Gap between runs | 2 s per user | Run |
| After a compile that ran out of time or memory | that user's Run and Submit in the same contest are refused for 60 s; kept in memory, so a restart forgets it. The judge also caches that compile error for the same language and source for 10 minutes, so resending the same code does not hold the compile lock again. Rejudges do not count | Run, Submit |
| Draft saves | one per user every 2 s | drafts |
| Code | 64 KB, UTF-8 | Run, Submit, drafts |
| Custom input | 64 KB | Run |
| Request body | 160 KB | Run, Submit, drafts |
| Integrity events | one batch per user every 5 s, 100 per request; after 5,000 per user per problem, more are dropped | the editor's paste, blur and focus events |
| Clarifications | 5 unanswered questions per student per contest (`400`) | Ask |
| AI requests | one running per admin, at most 3 minutes (`429 busy` without `retry_after_s`: try again when it ends) | the AI wizard, Can a model solve this? |
| Event streams | 4 per user (a fifth replaces the oldest, which is told and stops reconnecting), 5,000 in total (then `503`, and the page polls) | `GET /api/events` |
| Open requests | 8 at once per browser session, event streams aside (`429` with `Retry-After: 1`) | every API route with a session cookie |
| Time to send a request | headers within 10 s, the whole request within 15 s; 120 s for the routes that take an upload | every route; the long one for pack import, test replacement and roster upload |
| Request body at the edge | 33 MB (Caddy refuses more with `413` before the server sees it) | everything through Caddy |

A retried Submit with the same `client_key` returns the first submission and is not counted again, so a flaky network does not trip these limits. Only the contest limits are configurable, per contest in its settings.

## Running a contest

All of this is in the contest's live console in the web app; the routes are in `docs/API.md`, "Contests: organisers".

**Before.** Add validated problems (each is pinned to the version you validated; owners and admins only), upload the roster (CSV with a header row naming at least `email`; `name`, `roll_number` and `section` are optional) if access is by roster, add co-organisers by email (they must have signed in once), then publish (owners and admins only). Co-organisers run the contest but do not see the problem bank, and their own tries get the student view. An upload replaces the whole roster: anyone left out of the new file loses access at once, and the answer says how many were added and removed, so check those numbers when changing the roster of a live contest. Publishing refuses a problem that changed since it was added. Graded contests default to a fixed start (D47); the honour code students accept lists what is recorded and for how long.

**During.**

| Situation | Action | Effect |
|---|---|---|
| A test or the checker is wrong | Pause, fix the problem, validate it, add it to the contest again, Rejudge the problem, Resume | Pause holds the contest's submissions in the queue (nothing is lost; students can keep submitting). Until the problem is added again, run and submit on it answer "This problem is being updated by the organisers." Rejudge re-runs every submission of the problem at priority 50 with the new tests, including those still waiting for the judge on the old tests; the board recomputes as results arrive (at most every 2 s). Pressing it twice adds nothing. |
| One submission looks wrongly judged | Rejudge that submission | Only it is judged again. |
| Lost time for everyone (power cut, network) | Extend by N minutes | Everyone's end moves; open pages get the new end at once. Extensions add up. |
| One student lost time (crashed laptop, accommodation) | Set their extension (0 removes it) | Only their end moves; give a reason for the record. |
| A question many students will have | Answer it and make it public, or post an announcement | Arrives on every open page as a banner. |
| The judge is behind | Watch `judging_remaining` on the board and the feed | Setter work (validation, generation) is limited to one lane while a graded contest is live; runs never take every lane from submissions. |

**After.** Students see the frozen board from the freeze until everyone's time is up (including personal extensions), then the full board. To keep the result for a projector walk, turn on "Hold the reveal" before the end: students stay on the frozen board until you choose Reveal. The reveal screen opens once every window has closed. Export the results as CSV (spreadsheet-safe: cells that would start a formula get a leading `'`) or JSON. Publish the editorial when ready; students see it, and practice opens, only once every student's time is up (including personal extensions). Clone a contest to run it again with new dates.

**Retention.** Integrity evidence (paste and focus events, second sessions, each submission's typed and pasted counts and client hash) is deleted 180 days after a contest ended for everyone (D47), except for students in an open or awaiting_student integrity case for that contest; theirs goes on the first run after the case is decided. The server applies this at start-up and every 12 hours and logs `integrity data past retention removed` with counts. Scores, submissions and code are kept.

**Unused test files.** The same 12-hourly pass deletes files in `PODIUM_TESTS_DIR` that nothing refers to any more: tests replaced by new ones, old copies of reference solutions, checkers and generators, and runs' custom inputs. A file is kept while any problem's test, any submission or run, or any job that is waiting, running or less than 7 days old names it, and for a day after it was last saved; the judge's own uploads that were never used go after a day too. The log line is `unused test files removed` with counts and MB. Nightly backups keep their own hard-linked copies, so a file removed here is still in the backups taken before.

## Integrity checks

The reviewer's guide, what each signal means and the policy template are in `docs/INTEGRITY.md`; the routes are in `docs/API.md`, "Integrity". Owners and admins review; co-organisers who are students do not.

**Running a check.** On the contest's results page, choose "Check integrity" once the contest is over for everyone, personal extensions included (before that the answer is "Check integrity after the contest ends for everyone."). The check runs on the server in the background, not on the judge (D49), so judging is unaffected; a few hundred students take well under a minute, and the page shows "running" until it finishes. One check runs at a time on the whole server; a second press anywhere answers "A check is already running. Try again in a minute." Running it again replaces the contest's signals, for example after a rejudge; open cases keep their own copy of the evidence.

| What you see | Cause | What to do |
|---|---|---|
| "The check could not finish." | An error while reading submissions or tests | The server log has `integrity check failed` with the `run_id`, `contest_id` and the error. Fix the cause and run it again. |
| "The server restarted during the check." | The server stopped while a check ran; at start-up it marks such checks failed and logs a warning with the count | Run it again. |
| No signals on a short problem | Programs under 40 tokens are skipped, and code everyone shares does not count | Expected: short problems cannot tell copying from the one natural solution. |

**Reviewing.** Read the signal and, for similar code, the side-by-side view; look at both students' timelines. Open a case from one or more signals with a title and a reason of at least 20 characters. The title is shown to the students, so describe the problem, not the people ("Problem B, similar code"). Share the case: each student sees only their own evidence and writes an explanation, which they can change until the decision. Then clear or take action, again with a reason; action is refused until every student in the case was shared it. Every change is kept in the case's log and cannot be edited.

The server logs `integrity check started`, `integrity case opened`, `integrity case decided` and `integrity explanation saved` at info with ids only (`by`, `contest_id`, `case_id`, `run_id`, `state`); notes, explanations and code never go to the log.

**Retention of cases.** A case keeps its evidence and the students' explanations for 2 years after it is cleared or actioned. Then the same 12-hourly job empties both and logs `integrity case evidence past retention removed` with the count; the case, its title and its decision log with the reasons stay. Open and awaiting_student cases are never emptied, and reopening a closed case restarts the clock at its next decision.

### After the integrity review

- Retention: the 12-hour retention pass (`integrity.Service.Retain`, already called by main) now also deletes integrity runs and signals of contests that ended for everyone more than 180 days ago (same cutoff as `contests.Retain`), and deletes the explanation versions of cases closed more than 2 years ago along with the evidence. Cases keep their own copy of signals, so open cases are unaffected. A run still in state `running` is left for the next pass. Deletions are logged at info as "integrity checks past retention removed" with counts when the service has a logger. `integrity.Service.RetainSignals` is exported if main should log it separately.
- The response window (7 days) is a constant, `ResponseWindow` in `server/internal/integrity/cases.go`; changing it needs a rebuild (and `RESPONSE_DAYS` in `web/src/lib/review.ts` for the help text).
- Migration note: `0006_integrity.sql` was edited in place (unreleased). A development database that already ran 0006 must be recreated (or the integrity tables dropped and the `0006_integrity` row removed from `schema_migrations`) to pick up the new columns, table and triggers.

## AI wizard keys

The AI wizard ("New problem with AI" and "Can a model solve this?", `docs/AUTHORING.md`) uses the organiser's own Anthropic API key. There is no variable for it and nothing to configure on the VM (decision D50).

Adding a key:

1. Get an API key from the Anthropic Console (console.anthropic.com, API keys). A workspace with a spending limit is a good idea.
2. In podium, signed in as an owner or admin, open AI assistant in the problem bank, choose a model and paste the key.
3. The server checks it with one model lookup, which costs no tokens, and shows "Key active until 14:32". A refusal says why ("Anthropic refused the key. Check it and paste it again."), never repeating the key.

Models offered: Claude Opus 5.5 (default), Claude Sonnet 5.5, Claude Haiku 5.5. Opus and Sonnet requests have server-side refusal fallbacks switched on (beta `server-side-fallback-2026-07-01`): when the safety classifier refuses a request for a reason that does not apply to a fallback model, that model answers instead of the request failing, and the answer may then come from a different model than the one chosen. Haiku has no fallback (D54).

| Stored | Not stored |
|---|---|
| The drafts the wizard creates (origin `ai`), like any problem | The key: held in the server's memory only, tied to the session that pasted it |
| A log line per request with user id, provider, model and outcome code | The key, the brief, the pasted text or the model's answer in any log |
| The solve submission, as the admin's own submission | The key in any job payload, the database, backups, `cache/` or the judge |

- The key lasts 2 hours, then the form asks for it again. Signing out, signing in again in the same browser, removing the key in the form, or restarting the server forgets it at once.
- Each admin's key is their own: another admin's session cannot use it.
- One AI request per admin at a time, at most 3 minutes each.
- The server needs outbound HTTPS to `api.anthropic.com`. If the VM's firewall blocks it, every request ends with "The provider is unavailable. Try again in a few minutes."
- The wizard's client adds about 5 MB to the server binary and no memory while idle.

Testing mode (`PODIUM_MODE=testing`, `make dev-test`) also offers a "Test provider (canned problems)": no key, no network, two canned problems whose programs really work on the judge (one about a maximum subarray when the brief or text mentions "subarray" or "stretch", a pair sums problem otherwise). Use it to try the whole pipeline (draft, generate inputs, generate outputs, validate). It does not exist in dev or production.

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

## Checking a database

```sh
sudo -u podium podium-server check -db /var/lib/podium/db/podium.db -tests /var/lib/podium/tests
```

It opens the database read-only and prints: SQLite's `integrity_check` and `foreign_key_check`, the migrations recorded against those this binary knows (a database written by a newer server is reported, not touched), row counts, and whether every test file and program source recorded in the database is on disk with the recorded size. `-hash` re-reads every file and compares its SHA-256 with its name. Exit status 0 means sound, 1 means something is damaged or missing (listed), 2 means the check could not run. The nightly backup and `podium-restore` run it on every copy they make.

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
| "Generating outputs" never finishes | No worker is claiming jobs, as for validation | As the first row. Generation runs after contest work, before validation. |
| "Generating inputs" ends with "The generator failed on test 7 (subtask 2, seed 3)" | The generator crashed, ran out of time or memory, or printed nothing for that seed and those arguments | Run the generator locally with the same argv (`python3 generator.py 3 n=1000`), fix it, save, and Generate inputs again. Nothing was applied, so no test changed. |
| Run or Submit says "This problem cannot be judged yet" | Tests outside subtasks, or tests without an expected output | Fix the listed items in the editor; choose Generate outputs for missing outputs. |

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

Upgrading to M3 from M2:

1. `sudo ./install.sh app`, then `sudo systemctl restart podium-server podium-judge`. Migration `0004_submissions.sql` runs on start; the judge creates `compiled/` in its cache directory. No new variables.
2. Reset any database created before M3. Migrations `0003_problems.sql` and `0004_submissions.sql` were changed before the first release (outputs may be missing, new job kinds), and a migration that already ran is never run again, so an older database keeps the old tables and judging fails on it. Stop the server and delete the database with its `-wal` and `-shm` files: `data/podium.db` for `make dev`, `/var/tmp/podium-dev` for `make dev-test`, `/var/lib/podium/db/podium.db` on a VM that ran a pre-release build. Then re-import problems from their packs.

Upgrading to M4 from M3:

1. `sudo ./install.sh app`, then `sudo systemctl restart podium-server podium-judge`. Migration `0005_contests.sql` runs on start. No new variables.

Adding integrity checks:

1. `sudo ./install.sh app`, then `sudo systemctl restart podium-server`. Migration `0006_integrity.sql` runs on start and only adds tables, so existing contests and submissions are untouched and can be checked at once. No new variables; the judge needs no change.

Upgrading to M5 (AI wizard):

1. Take a backup first (see "Backups"): migration `0007_wizard.sql` edits the schema text of the `jobs` table in place.
2. `sudo ./install.sh app`, then `sudo systemctl restart podium-server podium-judge`. Restart both: the server must know the new `generate_inputs` job kind before a judge claims one. No new variables.
3. Migration `0007_wizard.sql` adds the generator columns and the `input_generations` table, and widens the allowed job kinds by rewriting the `CREATE TABLE jobs` text with SQLite's `writable_schema` procedure (decision D52): a table rebuild would cascade-delete every job result, because foreign keys stay on during migrations. No row changes. To check afterwards: `sqlite3 /var/lib/podium/db/podium.db "PRAGMA integrity_check"` prints `ok`.
4. To use the wizard, an admin adds a key in the web app (see "AI wizard keys"). Nothing goes in `server.env`.

Upgrading to M7 (hardening):

1. `sudo ./install.sh packages firewall app caddy litestream backup`, then `sudo systemctl restart podium-server podium-judge`. No migration. The `firewall` step closes every inbound port but ssh, 80 and 443; the `caddy` step rewrites the Caddyfile (admin API off, body limit). The server unit now sets `GOMEMLIMIT=220MiB`, and production refuses a `PODIUM_LISTEN` that is not loopback. The judge reads one new optional variable, `PODIUM_JUDGE_BLOB_CACHE_MB` (default 2048); add it to `judge.env` only to change the cap.
2. Fill in `/etc/podium/litestream.env` (see "Litestream" and "Backups"), then `sudo systemctl restart podium-litestream`.
3. Take the first nightly backup by hand: `sudo systemctl start podium-backup`.
4. On the first start the server removes test files nothing refers to (see "Running a contest", "Unused test files"); the log says how many.

## Backups

Two layers, both installed by `install.sh` (steps `litestream` and `backup`):

| | Litestream | Nightly backup |
|---|---|---|
| What | the database | the database and the test files |
| Where | the bucket in `/etc/podium/litestream.env` (off the VM) | `/var/backups/podium/<date>` on the VM (podium only, 700), copied off the VM by rsync when `PODIUM_BACKUP_RSYNC` is set |
| How often | every change, uploaded every 10 s | every night about 03:15 UTC, and on demand |
| Kept | 14 days of history (a daily snapshot plus every change since) | 14 days |
| Restores | the newest state, or any moment in the 14 days | that night's state |
| Protects against | losing the VM or its disk | a bad change (a wrong rejudge, a deleted contest), a damaged database |

Test files and program sources are not in the bucket: they live in `/var/lib/podium/tests`, named by SHA-256, and the database only refers to them. The nightly backup copies them as hard links, so 14 nights cost little more than one, and carries them off the VM when `PODIUM_BACKUP_RSYNC` is set (below). Set it before the first graded contest: without it, losing the VM's disk loses every test file, and only the pack zips you kept would bring the problems back. Submitted code is safe either way, since the database holds every submission's source.

Backups hold every student's code and the session table, so they are private: the backup runs with umask 077 (`UMask=0077` in the unit, `umask 077` in the script), `/var/backups/podium` is mode 700, and the files are readable by the podium user only. The judge cannot read them.

### Copy the nightly backup off the VM

`/etc/podium/backup.env` (root, 600; template `deploy/backup.env.example`) is optional and read by `podium-backup.service`:

| Variable | Default | Purpose |
|---|---|---|
| `PODIUM_BACKUP_RSYNC` | empty: no copy | `user@host:/path` on another machine; after each nightly run the whole backup folder goes there with `rsync -aH --delete` over ssh, hard links kept. A failed copy fails the unit, so it shows in `systemctl --failed` |
| `PODIUM_BACKUP_KEEP_DAYS` | 14 | nights to keep, here and (through `--delete`) on the copy |

Set it up once:

1. On the VM, a key for the podium user: `sudo -u podium ssh-keygen -t ed25519 -N '' -f /var/lib/podium/.ssh/id_ed25519`, and print `/var/lib/podium/.ssh/id_ed25519.pub`.
2. On the backup machine, an account whose `authorized_keys` holds that key limited to one folder, for example with rsync's `rrsync`: `command="rrsync /srv/podium",restrict ssh-ed25519 AAAA...` (then the target is `user@host:` with the path relative to that folder, so `PODIUM_BACKUP_RSYNC=podium-backup@backup.example.edu:`).
3. Accept the host key once: `sudo -u podium ssh podium-backup@backup.example.edu true` (it will say rrsync needs a command; that is fine).
4. `sudoedit /etc/podium/backup.env`, set `PODIUM_BACKUP_RSYNC`, then `sudo systemctl start podium-backup` and check `journalctl -u podium-backup -n 5` ends with `copied to`.

Disk snapshots from your VM provider are a fine addition but not a replacement: they usually live in the same account as the VM.

### Set up the bucket (Backblaze B2)

1. Create a B2 account, then Buckets, Create a Bucket: private, default encryption on. In the bucket's lifecycle settings choose "Keep only the last version of the file" (Litestream keeps its own history and deletes what it no longer needs).
2. Application Keys, Add a New Application Key: access to that bucket only, Read and Write. Copy the keyID and the applicationKey (shown once).
3. `sudoedit /etc/podium/litestream.env`: the key ID, the key, the bucket, a path such as `podium`, and the endpoint and region from the bucket's page.
4. `sudo systemctl restart podium-litestream`, then `journalctl -u podium-litestream -n 30`: the start-up check lists every variable as `set`, and Litestream logs its first snapshot and then a sync every 10 s while the database changes.
5. Prove it: `sudo /usr/local/lib/podium/podium-restore litestream /var/tmp/restore-check` restores the newest copy from the bucket and checks it; then `sudo rm -rf /var/tmp/restore-check`.

Litestream has only been rehearsed against a local file replica so far (cache/v1/m7-restore.md); the first restore from your bucket in step 5 is the real test. If the first sync fails with an error about checksums or a header the provider does not support, note the exact message: Litestream 0.5 has per-provider options (`sign-payload`, `require-content-md5`) under the replica in `/etc/litestream.yml`.

### Nightly backup

`podium-backup.timer` runs `/usr/local/lib/podium/podium-backup` as user podium. Each run writes `/var/backups/podium/<UTC date and time>/` with:

- `podium.db`: an online copy (SQLite's backup API, safe while the server writes);
- `tests/`: hard links to every test file and source (a real copy if `/var/backups` is on another filesystem);
- `check.txt`: the output of `podium-server check` on the copy. A copy that fails its check is discarded and the unit fails.

It then points `latest` at the new backup and deletes those older than 14 days (`PODIUM_BACKUP_KEEP_DAYS` in `/etc/podium/backup.env` changes that), and copies the folder off the VM if `PODIUM_BACKUP_RSYNC` is set.

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
```

The Litestream forms take the test files from `/var/backups/podium/latest/tests` unless a third argument names another folder. Putting a restored copy in place of the live data is in `docs/RUNBOOK.md`, "Restoring from a backup".
