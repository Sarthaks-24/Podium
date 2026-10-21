# Contest-day runbook

What to check before a contest, what to watch and do during it, and what to finish afterwards, with the commands. Installation, configuration and backups are in `docs/OPERATIONS.md`; the routes behind every button are in `docs/API.md`. The last two sections are the restore procedure and the dry run with real students that should come before the first large contest.

Two people make contest day easy: one at the contest's live console in the browser (clarifications, announcements, the board), one with an SSH session on the VM (the metrics, the logs, the services). One person can do both for a small class.

## A week before

- [ ] Every problem in the contest shows "Validated" with a time after its last edit, and was added to the contest after that validation (publishing refuses anything else).
- [ ] Time limits are at least 3 times the C++ reference's time, and the validation report shows no warnings about test count (6 to 10 hidden tests), maximum-size tests (at most 2 per subtask) or output size (under 1 MB). Each extra second of tests costs every submission.
- [ ] A second organiser has read each statement; the samples match the Input and Output sections.
- [ ] The contest is set up: schedule, start mode, freeze, scoring and feedback modes, languages, rate limits (30 s gap and 15 per problem unless the class is small), access (domain or roster), co-organisers.
- [ ] For a large class, prefer the flexible window (for example 2 hours inside a 3 hour window): it spreads the end-of-contest rush and leaves almost no judging tail. Say so in the announcement.
- [ ] A practice contest with two easy problems is open, so first-timers learn the editor, Run and Submit before it counts.
- [ ] Backups are healthy: `systemctl status podium-litestream` is active and `journalctl -u podium-litestream -n 20` shows recent syncs; `sudo ls -l /var/backups/podium` shows last night's backup, and `journalctl -u podium-backup -n 20` shows `encrypted copy uploaded as podium-<time>.tar.age` (the copy that carries the test files off the VM). `sudo /usr/local/lib/podium/install.sh verify` ends with "everything checked is in order", and no alert is open on your phone.
- [ ] Before the first graded contest, and after any change to the backup setup: the timed restore onto a second VM in "Restoring onto a new VM" below. The weekly restore proof (`podium-check`) covers the database every week in between.
- [ ] No deploy is planned inside the 48 hours before the contest: `sudo -u podium podium-server deploy-check -db /var/lib/podium/db/podium.db` says until when deploys are allowed. Publishing or moving the contest is what starts that window.
- [ ] Every problem passes a live-judge check: submit its reference solution and one known-wrong solution from "Try as a student" and confirm AC and the expected failure. For every problem that came from the AI wizard (`origin` ai), add a solution written by a person that passes all tests.
- [ ] Labs: if students use shared lab PCs, ask the lab to use guest or kiosk browser profiles, and tell students to sign out of podium and of Google when they leave. A browser left signed in hands the next student the previous one's session.
- [ ] The VM is not in testing mode and has no leftover accounts from a test: `systemctl show podium-server -p Environment` and `grep PODIUM_MODE /etc/podium/server.env` show nothing or `production`, and `sudo -u podium sqlite3 /var/lib/podium/db/podium.db "SELECT email, role FROM users WHERE role <> 'student'"` lists only the people you expect.
- [ ] The edge is closed: `sudo ufw status` shows only ssh, 80 and 443; `ss -ltnp` shows podium on `127.0.0.1` only.

## The day before

- [ ] Validate every problem of the contest again (catches a compiler or runtime change since). If any problem changed, add it to the contest again.
- [ ] Disk: `df -h / /var/lib/podium` shows more than 2 GB free.
- [ ] Memory and services: `systemctl status podium-server podium-judge podium-litestream caddy` all active; `free -m` shows the swap file.
- [ ] Sandbox: `sudo systemctl stop podium-judge && sudo -u judge podium-judge calibrate; sudo systemctl start podium-judge` passes (after any kernel or isolate upgrade this is required).
- [ ] Take a backup by hand: `sudo systemctl start podium-backup`, then `journalctl -u podium-backup -n 5` ends with `done`.
- [ ] Freeze the problem bank for the contest window: no pack imports, AI wizard runs or problem edits from now until the board is final, and rejudges only for a confirmed bad test (below).
- [ ] Announce the link, the start time, the format (fixed or window), the languages, and how to ask questions.

Security checks (security council, 2026-10-18):

- Check the server's mode and organisers: `systemctl show podium-server -p Environment` (no `PODIUM_MODE=testing`) and `sqlite3 /var/lib/podium/db/podium.db "SELECT email, role, google_sub IS NOT NULL AS linked FROM users WHERE role <> 'student'"`.
- Setter checklist (`docs/AUTHORING.md`, "Before a graded contest"): reference and a known-wrong solution judged live for every problem, at least half the tests cross-checked, a person's solution for every AI problem.
- Appoint student co-organisers freely: they cannot change rules or grading once the contest starts, and see results as students do.
- After the contest, store the JSON export (it carries the audit log) and its SHA-256 off the server.

## One hour before

- [ ] Sign in as an organiser, open the contest's live console. Copy your session cookie for the metrics (`docs/OPERATIONS.md`, "Metrics") and start the watch loop in the SSH session.
- [ ] Metrics: `queue.queued` and `queue.leased` are 0, `queue.failed_last_5_min` is 0, `events.connections` is small.
- [ ] Submit the reference solution of problem A from "Try as a student" in the problem editor: it is judged within a few seconds.
- [ ] The rules are right: stop on a failing sample on, 30 s gap, 15 per problem, the right languages.
- [ ] Post a welcome announcement: how to ask a clarification, where the standings are, and that results become final a few minutes after the end (every submission made before the end counts).

## During the contest

### What to watch

Every 10 minutes, or whenever something feels slow: the metrics watch loop (`docs/OPERATIONS.md`, "Metrics") and the console's feed.

| Figure | Normal | Act when |
|---|---|---|
| `queue.queued` | under 50, rising toward the end of a fixed-start contest | over 400, or rising for 15 minutes in the middle of the contest: raise the gap (below) |
| `per_min` (sum of `finished_last_minute`) | the judge's throughput | 0 while `queued` is above 0: the judge is down (below) |
| `wait_p95_s` (`queue.submit_wait_p95_ms`) | a few seconds; minutes at the end of a fixed-start contest | over 10 minutes in the middle of the contest |
| `queue.failed_last_5_min` | 0 | above 0: read the judge's log |
| `streams` (`events.connections`) | about one per student with the contest open | drops to near 0 suddenly: the server restarted or a proxy is cutting streams |
| `heap_mb` | under 100 | over 200 (the server unit is capped at 300 MB): note it and tell the developer after the contest |

The live console's feed shows submissions as they arrive and their verdicts; the board header shows `judging_remaining`.

### The queue is too long

In order:

1. Announce it ("Judging is running a few minutes behind. Every submission counts; please do not resubmit the same code.").
2. Raise the gap between submits: contest settings, `min_gap_s` to 60 (or 120). It applies at once and to everyone.
3. Lower `max_per_problem` only if students are clearly spamming one problem.
4. Do not pause the contest for this: pausing stops judging for the contest entirely.

### Pausing judging

Pause in the live console holds the contest's submissions and runs in the queue; students can keep submitting and nothing is lost. Resume lets them through in order. Use it while a test is being fixed (below), never to slow the queue.

### Extending

- Everyone lost time (power cut, network down, the server down for minutes): Extend by N minutes in the live console. Extensions add up; open pages get the new end at once. Announce it.
- One student lost time (crashed laptop, accommodation): set their extension in the console with a reason (0 removes it). Only their end moves.

### A bad test or checker found mid-contest

1. Pause judging for the contest.
2. Fix the test or the checker in the problem editor, then Validate it. Until the next step, Run and Submit on that problem answer "This problem is being updated by the organisers."
3. Add the problem to the contest again (Problems, same label).
4. Rejudge that problem from the live console: every submission of it is judged again with the new tests, including those still waiting on the old ones. Pressing it twice adds nothing.
5. Resume judging. Post an announcement saying what changed and that scores are being recomputed.

A single submission that looks wrongly judged: Rejudge that submission only.

### The judge stopped

Signs: `per_min` is 0 while `queued` is above 0, `leased_jobs` stuck at the same ids, or students see "Waiting for the judge".

```sh
systemctl status podium-judge
journalctl -u podium-judge -n 50 --no-pager
sudo systemctl restart podium-judge
```

- A job the dead worker held goes back to the queue once its lease runs out (60 s; the sweep runs every 10 s) and is judged from the start. A job that loses its worker three times fails: the student sees an error, it does not count, and they can submit again.
- `claim failed ... server answered 401` in the judge's log: the tokens in `server.env` and `judge.env` differ. Fix and restart both.
- If the judge restarts again and again, check memory (`free -m`, `journalctl -k | grep -i oom`) and run the calibrate check after the contest.

### Emergency deploy

The deploy guard refuses any upgrade from 48 hours before a contest until its queue is empty. When a fix cannot wait (a crash that recurs, a security fix):

1. Pause judging of the contest if the fix touches judging, and announce the restart: "The site restarts for a minute at 14:02. Your code and drafts are kept; the page reconnects by itself."
2. `tools/deploy.sh HOST --force "<what is broken and why it cannot wait>"` from your machine, or on the VM `sudo ./install.sh upgrade --force "<reason>"`. The reason goes in each contest's audit log before anything is installed.
3. Watch the event streams come back in the metrics, then resume judging.
4. If the fix makes things worse: `sudo /usr/local/lib/podium/install.sh rollback` (same schema: a minute).

If the owner cannot be reached and no deploy is possible, any admin can pause or extend the contest from its console: that needs no deploy at all.

### The server restarted (or you restarted it)

`sudo systemctl restart podium-server` takes a few seconds. What students see: the page's live connection drops and reconnects by itself with a short random delay, and the page refetches their submissions. A Submit that failed during the restart shows an error with a retry; the retry carries the same key, so a submission that did reach the server is never counted twice. Judging continues: the worker keeps its jobs and reports once the server is back. Logs: `journalctl -u podium-server -n 50 --no-pager`.

If the server does not come back: `systemctl status podium-server` and its log say why (a configuration problem names the variable and the file). `curl -fsS http://127.0.0.1:8080/healthz` answers `{"status":"ok","db":"ok"}` when it is up.

### A student is locked out

| They see | Cause | Do |
|---|---|---|
| The sign-in page refuses their account's domain | they used a personal Google account, or their domain is not allowed | ask them to use the college account; an owner can add a domain on the People page |
| "This email is linked to a different Google account" | their address moved to a new Google account | an owner resets the link on the People page ("Google account links") |
| "This contest is for the students on its list." | roster contest, their email is missing or misspelt | Roster: download it, add the row, upload it again. An upload replaces the whole roster, so check the "added" and "removed" counts it reports |
| "Accept the rules on the contest page to start." | they have not pressed Start | point them to the contest page |
| "This page stopped live updates because podium is open in another tab or window." | a fifth tab replaced this one | close the other tabs; "Resume here" takes live updates back |
| Their laptop died | | they sign in from another machine; drafts are saved every 10 s on the server. Give a personal extension for the lost time |
| "Your session has ended. Sign in again." | the session expired or was ended | sign in again; nothing is lost |

### Other trouble

| Sign | Do |
|---|---|
| Disk almost full (`df -h`) | `du -sh /var/lib/podium/* /var/cache/podium-judge/* /var/backups/podium` to see where; the judge cache can be deleted with the judge stopped (`sudo systemctl stop podium-judge; sudo rm -rf /var/cache/podium-judge/*; sudo systemctl start podium-judge`) |
| `podium-litestream` failed | replication only; the contest is unaffected. `journalctl -u podium-litestream -n 30`, fix after the contest, and take a nightly backup by hand meanwhile |
| The whole VM is unreachable | check the provider's console; after a reboot every service starts by itself. Extend the contest by the downtime |

## After the contest

- [ ] Wait for the board header to read "Final": every submission made before the end is judged, and the tail is expected in a fixed-start contest. The metrics' `queued` reaches 0.
- [ ] If "Hold the reveal" was on, run the reveal (in the room on a projector, or just press Reveal).
- [ ] Export CSV and JSON from the results page, record their SHA-256 (`sha256sum results.csv results.json`), and keep the files and the hashes off the VM with the contest date. The hashes show later that the results were not changed.
- [ ] Take a backup: `sudo systemctl start podium-backup`, and check it was copied off the VM (`journalctl -u podium-backup -n 5`). Keep the server's log for the contest window too: `journalctl -u podium-server --since "<start>" --until "<end + 1 hour>" > podium-<date>.log`, stored with the exports (organiser actions are logged there).
- [ ] Run the integrity check from the results page (once every window, including personal extensions, has closed), and review it within a few days (`docs/INTEGRITY.md`).
- [ ] Publish the editorial when ready; practice opens for students once everyone's time is up.
- [ ] Write down every rough edge while it is fresh (the template in "Dry run with real students" works for every contest).

## Restoring from a backup

Use this when the database is damaged, a mistake must be undone, or the VM was lost. First restore into a scratch directory and check it; only then put it in place.

1. Pick the source: the bucket for the newest state, a moment in the last 14 days for undoing a mistake, or a nightly backup.

   ```sh
   R=/usr/local/lib/podium/podium-restore
   sudo $R litestream /var/tmp/restore                           # newest
   sudo $R litestream-at 2026-10-18T14:30:00Z /var/tmp/restore   # before the mistake
   sudo $R nightly /var/backups/podium/latest /var/tmp/restore   # last night
   ```

   Each ends with the `podium-server check -hash` report; `result: ok` is required to go on.
2. Optional: look at it first in a testing-mode server on another port, as the script prints, and sign in at the address it gives through an SSH tunnel.
3. Put it in place:

   ```sh
   sudo systemctl stop podium-litestream podium-server podium-judge
   sudo mv /var/lib/podium/db /var/lib/podium/db.before-restore
   sudo install -d -o podium -g podium -m 2770 /var/lib/podium/db      # group podium writes, for Litestream
   sudo install -o podium -g podium -m 660 /var/tmp/restore/podium.db /var/lib/podium/db/podium.db
   sudo cp -an /var/tmp/restore/tests/. /var/lib/podium/tests/       # adds missing files, keeps existing ones
   sudo chown -R podium:podium /var/lib/podium/tests
   sudo -u podium podium-server check -db /var/lib/podium/db/podium.db -tests /var/lib/podium/tests
   sudo systemctl start podium-server podium-judge podium-litestream
   ```

4. After a restore from the bucket to an earlier moment, Litestream starts a new history from the restored state. Keep `/var/lib/podium/db.before-restore` until the next nightly backup has run, then delete it.
5. On a new VM: follow "Restoring onto a new VM" below.

What can be lost: with the bucket, at most the last 10 s of changes; from a nightly backup, everything since that night. Test files are not in the Litestream bucket: they come from the encrypted copy of the last nightly backup, so test files added since that night (a problem imported or edited today) are lost and must be imported again from their pack zips.

## Restoring onto a new VM

When the VM is lost or cannot be trusted. **During a contest the rule is: the contest is extended or rerun, never recovered live** (D113). Announce that results are safe up to the recovery point, pause or end the contest, and restore calmly. The target is 60 minutes from the alert to a student signed in on the new VM; the P0 drill measures it.

What you need at hand: the four env files from your password manager (`server.env`, `judge.env`, `litestream.env`, `backup.env`, plus `alert.env`), the `age` private key `podium-backup.key`, the release tarball that was running, access to the DNS records, and the Google Cloud console.

Write down the time at each step; the drill note records them.

1. **Recovery point.** From your own machine or the old VM if it still answers, note the age of the newest Litestream upload (`podium-check status` on the old VM, or the newest file's date under the Litestream path in the B2 web console). That is the most that can be lost.
2. **New VM.** Create it (Ubuntu 24.04, amd64, 2 GB). If a provider snapshot of the configured VM exists (see "After the gate: provider snapshot"), create the VM from it and skip to step 5 with the newest data.
3. **Install.** Copy the release tarball over, then `tar xzf podium-<version>.tar.gz && cd podium-<version> && sudo ./install.sh`. With nothing filled in yet it ends at the checklist.
4. **Env files.** `sudoedit` each of `/etc/podium/server.env`, `judge.env`, `litestream.env`, `backup.env`, `alert.env` and paste the saved contents. The same `PODIUM_SESSION_SECRET` keeps integrity hashes comparable.
5. **Test files**, on your own machine: download the newest `podium-<time>.tar.age` from the B2 bucket of the encrypted copy (web console, Browse Files), decrypt and pack only the tests, copy them over:

   ```sh
   age -d -i podium-backup.key podium-<time>.tar.age | tar -x -C restored/
   tar -C restored -czf tests.tgz tests && scp tests.tgz <new-vm>:/tmp/
   ```

   On the new VM: `sudo mkdir -p /var/tmp/tests-restore && sudo tar -xzf /tmp/tests.tgz -C /var/tmp/tests-restore`.
6. **Database** from the bucket, with those test files, checked: `sudo /usr/local/lib/podium/podium-restore litestream /var/tmp/restore /var/tmp/tests-restore/tests`. `result: ok` is required.
7. **Put it in place**: step 3 of "Restoring from a backup" above, then `sudo ./install.sh app` (starts the services now that the env files are real).
8. **DNS.** Point the A record (and `www` if used) at the new VM's address; with a low TTL (300 s) this takes minutes. Check from your machine: `nslookup <host>` shows the new address.
9. **Certificate.** `sudo ./install.sh caddy` once DNS answers with the new address: the step checks that first and skips with a message if the record still points elsewhere, then Caddy gets the certificate. `curl -fsS https://<host>/healthz`.
10. **Sign-in.** The OAuth redirect URI is the same (`https://<host>/api/auth/google/callback`) when the host name did not change. If it did, add the new one in the Google Cloud console first. Sign in as a student account and open the contest.
11. **Litestream and the rest**: `sudo systemctl restart podium-litestream`, `sudo systemctl start podium-backup`, `sudo /usr/local/lib/podium/install.sh verify`.
12. Delete `restored/` and `tests.tgz` on your machine: they hold every student's code.

Then extend or reschedule the contest and announce it.

## After the gate: provider snapshot

Once the go-live gate is signed, take a snapshot of the configured VM in the provider's console (with the services running is fine; the database is also in the bucket). A rebuild from it starts at step 5 above instead of step 3, which saves most of the install time. Record the snapshot id and date in the drill note, and take a new one after each minor release.

## Proving the safety net

These drills prove that the backups, alerts and upgrades work on the real VM. Each runs once before go-live and again after any change to the part it proves; each leaves a dated note.

| Drill | How | Proof |
|---|---|---|
| Alerts | `sudo /usr/local/lib/podium/podium-alert test`; `sudo systemctl kill -s KILL podium-judge`; `sudo systemctl stop podium-litestream` then `sudo /usr/local/lib/podium/podium-check`; fill the disk with `fallocate` until the 2 x database plus 2 GB rule trips; `sudo /usr/local/lib/podium/podium-check restore-proof --corrupt` | one phone notification each, then "resolved" |
| Weekly restore proof | `sudo /usr/local/lib/podium/podium-check restore-proof` | "restore proof" ok in the journal, no alert |
| Encrypted copy | decrypt last night's object on your machine (OPERATIONS.md, "Encrypted copy off the VM") | `podium.db` and `tests/` come out |
| Upgrade and rollback | `sudo ./install.sh upgrade` of a test release, then `sudo /usr/local/lib/podium/install.sh rollback`, once with the same schema and once with a newer one | both end with verify in order |
| Deploy during practice | a test student in a practice session while `install.sh upgrade` reinstalls the same release | the page reconnects, the draft is kept, a submit pressed during the restart counts once |
| Restore onto a new VM | "Restoring onto a new VM" on a second, temporary VM, timed | under 60 minutes, recovery point noted |
| Real sign-in | a student account on an allowed domain signs in over HTTPS on the real domain and submits | the OAuth app is Internal or In production (OPERATIONS.md, "Google sign-in") |


## Dry run with real students

PLAN.md requires one real contest with a small group before the first 700-student contest. The load test checks capacity; only people find confusing copy, a broken flow on a phone, or a problem statement everyone misreads. The written list of fixes is the milestone's deliverable.

### Plan it

- [ ] 10 to 30 students, ideally with a range of experience, on their own laptops and the campus network (not only the lab), plus two or three on phones.
- [ ] 60 to 90 minutes; three problems (easy, medium, one with subtasks); all four languages allowed.
- [ ] Use the format you plan for the big contest. If unsure, run fixed start, since it stresses the end of the contest most.
- [ ] Two organisers: one runs the console, one walks the room and takes notes with the template below.
- [ ] Everything from "A week before" to "One hour before" done for real, including the backup checks: the dry run tests the runbook too.

### During it, on purpose

Do each of these once and note what students saw and how long it took:

- [ ] Answer one clarification privately and one publicly; post one announcement.
- [ ] Give one student a personal extension; extend the contest for everyone by 5 minutes.
- [ ] Pause judging for one minute and resume it.
- [ ] Restart the judge (`sudo systemctl restart podium-judge`) while submissions are queued.
- [ ] Restart the server (`sudo systemctl restart podium-server`) once, mid-contest, and watch the event streams come back in the metrics.
- [ ] Fix a deliberately planted mistake: one hidden test of problem B has a wrong expected output from the start; follow "A bad test or checker found mid-contest" end to end, including the rejudge.
- [ ] Ask one student to sign in from a second machine, and one to close the laptop for 2 minutes and come back.

### Afterwards

- [ ] Export CSV and JSON; check the CSV opens cleanly in a spreadsheet and the ranks match the board.
- [ ] Run the integrity check; look at what it says about a class that did not cheat (it should be quiet).
- [ ] Restore last night's backup into a scratch directory (`podium-restore nightly ...`) and read the check report.
- [ ] A 5-minute survey for students: what confused you, what was slow, what would you change.
- [ ] Hold a 20-minute review with the organisers and fill in the list below the same day.

### List of fixes (template)

Copy into `cache/v1/dry-run-<date>.md` (not shipped) and fill one row per issue. Severity: **blocker** (must be fixed before the 700-student contest), **major** (fix before then if at all possible), **minor** (backlog).

```
# Dry run <date>: list of fixes

Group: <n> students, <format>, <duration>, problems <A, B, C>, languages used <...>
Organisers: <names>
Peak during the run (metrics): queued <n>, wait p95 <s>, streams <n>, heap <MB>
Time from end to "Final": <min>

| # | Severity | Where | What happened | Who saw it | Fix | Owner | Done |
|---|---|---|---|---|---|---|---|
| 1 | blocker | workspace, phone | Submit button hidden under the keyboard | 2 students | ... | ... | |
| 2 | minor | statement B | "n" and "N" both used | most | edit statement | ... | |

Runbook steps that were wrong or missing:
- ...

Questions students asked more than once (candidates for the welcome announcement or the help text):
- ...

Decision: ready for the 700-student contest? yes / after fixes 1, 3, 4 / no, run another dry run
```
