# Changelog

Dated entries per release. Unreleased work is listed at the top.

## Unreleased

### v4 stage O: the owner dashboard (2026-10-30)

- The stored role `admin` is now `organiser` (migration `0002_v4_roles`), so a new admin role can sit between organiser and owner. Nothing changes for anyone: organisers keep every right, their sessions keep working, and site audit rows from before the migration read "organiser".
- A fourth role, admin, between organiser and owner. An admin organises like an organiser, and also reads People and the site audit, gives and removes the organiser role, and ends sessions, disables and enables students and organisers; never an admin, an owner, the configured owner or themself (403). One rule function in `auth` decides every such change; organiser live events reach admins too.
- Sign-in is decided by the list of people alone (V4-15): every account is a row of the list, and only a row that is not disabled may sign in. The allowed email domains, their Settings form and `PODIUM_ALLOWED_DOMAINS` are gone (migration `0003_v4_signin`), and with them the Google Workspace (`hd`) check; the binding to the first Google account stays. Gmail addresses match however their dots are placed, and `googlemail.com` reads as `gmail.com`. A refused address sees "Your email is not on the list for this site. Ask the organiser to add it." and is kept (the 500 most recent) for a one-click Let in. New routes import a pasted list (a dry run first), remove a person (deleted if they never signed in, otherwise disabled and signed out) and let a refused address in. Remove the `PODIUM_ALLOWED_DOMAINS` line from your `server/.env`; it is ignored.
- Settings is now the Dashboard (V4-17), at `/admin/dashboard` for admins and owners (`/admin/people` still opens it). People shows everyone on the sign-in list with three counts (on the list, signed in, never came) and a search; each row's role select offers only the roles you may give, and Remove deletes someone who never came or signs out and disables someone who did, asking first. "Add people" takes one address or a pasted list, checks it (new, already on the list, not an address), then adds with "Add N people". Refused sign-ins have Let in. Admins do not see the Organisation tab or Reset Google link. A refusal of one person's change is `403 not_allowed`, so the page shows it instead of closing.
- Sign-in can be closed from the Dashboard's Sign-in tab (it asks first): nobody below admin starts a new session, people already signed in stay, and the sign-in page says "Sign-in is closed right now." Opening it is one press. Both are in the site audit.
- The Dashboard's Live tab shows how many people have a page open, when the judge last asked for work, the queue and its median wait, disk free and the database's size, and the newest nightly backup, reading `GET /api/admin/live` every 5 seconds while it is open and never while hidden. The server reads the backup folder from the new optional `PODIUM_BACKUP_DIR` (default `/var/backups/podium`, as in `backup.env`).
- Adding a problem to a contest no longer pulls focus back to the picker when the save ends after you moved on.
- A switch from testing mode to dev or production also disables every account that signed in only through testing mode, so a testing-mode visitor does not stay on the sign-in list.
- People History says "Made x an organiser" instead of "Changed x from none to organiser" for someone who had no account yet.
- A migration can ask to run with foreign keys off (its first line says `-- podium: foreign_keys off`), for SQLite's table rebuild; it commits only if no reference is broken, on a connection that is then thrown away.

### v4 stage H: site hygiene (2026-10-30)

- A path the app has no screen for answers 404 instead of 200, still showing the "Page not found" page. A Go test keeps the server's list of app paths in step with the router.
- A trailing slash redirects to the same page without it, and the v2 pages (`/progress`, `/notices`, `/contact`, `/integrity`) redirect home.
- The privacy notice describes v3 as built: no progress page, standing, rating, notices or contact form; drafts on the server, run fingerprints, the device number, similar-code decisions and extra time are named. It stays unlinked until the owner approves the text.
- Five messages now say what happened and what to do: a theme not saved to the account, a sign-out the server did not confirm, a validation that could not start for a network fault, a reveal whose board could not reload, and a Run refused for pacing while Submit counts down (the last was only spoken, never shown).
- The link crawl covers every organiser screen, and Newer, Older and Add problem say why they are disabled. A Playwright check holds every route to a title no other route uses.

### v3: what the final agent pilot found, second part (2026-10-30)

- A duplicated contest is titled "{title} (copy)", so two drafts no longer look the same.
- The live view's time-check announcement fills in the real minutes left, and the overview names each problem beside its letter.
- Messages' "About" starts on the problem you have open; "The contest in general" is one choice away.
- The Organisation history says what changed ("Changed the organisation's name") and names an erased account in words.
- The standings read a total's time as "at 7:17 from the start", and in testing mode an invite says to sign in with the email on the form.

### v3: what the final agent pilot found (2026-10-30)

- The live view measures the judge's silence at the moment the server answered and refreshes as soon as its tab shows again, so a background tab no longer says "Judge not seen" while judging works. Student view's back link opens the live view.
- Nothing said about one problem stays under the next; a refused paste shows one line that goes at the next key, Run or Submit.
- Run and Submit wait while a language change or a reset asks its question.
- A run that did not compile gives the same advice as a submission, Java's class Main included. Run results say "word" instead of "token", and a checker's comment reads "The checker said: ...".
- Changing the reference solution's language in the problem editor swaps an untouched starter for the new language's.

### v3 stage D: golden API fixtures (2026-10-30)

- The mini-contest acceptance test records 14 core-path responses into `web/tests/fixtures/` (`PODIUM_GOLDEN=1` rewrites them; a stale file fails `go test`). A Vitest check holds the web's API types to those files, and the mocks are typed against them.
- The types lost the fields the server never sends (`asker_name`, `honour_accepted_at`, `problem_count`, old feed fields) and now require what it always sends.

### v3 stage D: web tests and leftovers (2026-10-30)

- The mocked Playwright specs find elements by role, label or test id; 24 class locators remain, each a measured layout box, CodeMirror internals or the class under test (was 171).
- A Vitest case checks the editor's keymap and extensions directly (brackets, completion, wrap, find, folding).
- The sentence "This test is shown because this was a run" is gone; test names carry no plan ids.

### v3 stage D: handlers next to their domain (2026-10-30)

- The HTTP handlers moved out of `httpapi` into their domain packages (`auth`, `problems`, `submissions`, `contests`, `integrity`, `ai`, `jobs/judgeserver`, `ops`), with shared helpers in `httpx`. `httpapi` keeps the route table, sign-in, limits and the event stream: 1,248 lines, was 5,370. No handler queries the database directly. Routes, bodies and codes are unchanged.

### v3 stage D: web structure (2026-10-30)

- Every screen loads through one loader that keeps only the newest answer and stops on unmount; no screen keeps its own load state.
- Twelve unused exports and their tests deleted; long component header comments cut to one paragraph; plan ids in comments replaced by the reason in words.
- Part of the class locators in the mocked specs became role locators (171 to 138 lines).

### v3 stage D: server structure (2026-10-30)

- Dead code from the cuts deleted (the live contest helper, the no-reveal view option, the co-organiser branch, a duplicate audit writer, unused stream, run and worker entry points). Archiving a contest drops its cached boards.
- Test files and test names say what they test instead of the plan phase; production comments give the reason in words instead of plan ids.
- `busy` is always 429 with `Retry-After` and `retry_after_s`; a similarity check already running is `409 already_running`; a full stream cap is `503 stream_limit`.
- `TestRouteRoles` probes every route as signed out, student, organiser and owner, reading the route list from the source so a new route without a row fails.
- One scheduler runs the background jobs, with one retention pass every 12 hours; shutdown waits for a running batch before closing the database. Services take their dependencies in constructors (`cmd/podium-server/wire.go`).
- The test AI provider is only in builds made with `-tags fakeai` (`make dev`, `make dev-test`); the Anthropic client uses plain net/http and go.mod drops `anthropic-sdk-go` (indirect modules 20 to 9).
- `PODIUM_JUDGE_BOX_BASE` moves the judge's isolate boxes; start-up refuses a range that covers the self-test box.

### v3 stage C6: the problem editor (2026-10-30)

- The editor has three tabs, Statement, Tests and Checking, with one status line in the title row ("Validated, 5 tests, slowest 212 ms"). At 1440 px each tab of a five-test problem is under 1,200 px (the editor was 3,987).
- Validate is the one primary action until it passes, then "Add to contest" picks a draft or upcoming contest and adds the problem under the next letter. Validate makes missing expected outputs first; the Generate outputs button is gone and "Regenerate all outputs" sits in the More menu with Download pack and Archive.
- Each test has a role select (Example, Subtask N, Not in a subtask) instead of typed ranges; saving writes the same lists the server always read.
- The generator, brute force, validator and custom checker sit under "Optional extra checks" with one line each when used.
- The statement is written in the same editor as code (plain text), with Preview.
- The nav says Problems. A new problem's address is made from its title; the slug field is gone.

### v3 stage C7: exam options (2026-10-30)

- Setup has four exam options under Advanced, all off by default: full screen on start, count tab switches and focus loss, block paste into the editor, block copy out of the statement.
- Nothing blocks silently: before Start, and in Help's rules, one line says which measures are on. A blocked paste or copy shows a line saying so; leaving full screen offers "Return to full screen".
- When an option is on, the console's submissions feed shows what was recorded beside each submission, and the similar-code page shows a table per student. Full-screen exits are a new integrity event (`fullscreen_exit`) and a count on each submission.
- The measures apply only during a student's own contest time, not in practice or the organiser's student view. Blocking happens in the browser; the server records and counts.

### v3 stage C5: Results (2026-10-30)

- Results lead with one line on where the reveal stands, then Reveal (only once there is something to reveal), Similar code, Download CSV and Download JSON. Reveal asks once; nothing says "release" any more. The step-by-step reveal overlay is deleted.
- The board spans the page and one statistics table follows.
- The CSV writes the browser's local time (`?tz=`, the server's zone otherwise) and a dash for a problem not tried; the download shows a line with the file name.
- Similar code is the page's name, and strength reads in words with the reason under it ("Weak", "Short programs, identical"), the numbers in the details.

### v3 stage C4: Settings (2026-10-29)

- People is now Settings, for owners, with three tabs: People, Sign-in and Organisation. The tab is in the address, so a reload keeps it. At 1440 px each tab is under 1,000 px of content (the page was 2,611).
- Two roles show in words: organiser and owner. The history of changes opens from the Organisation tab.
- The first-contest checklist appears once, on the contests home, with a step to add another organiser for owners.
- The route table owns each screen's role and section; anyone a route turns away sees one forbidden page.

### v3 stage C3: contest setup (2026-10-29)

- Setup saves every field as it changes, with one "Saved" line in the title row and no Save button; a refused field is marked and the rest still save.
- Six inputs show: title, problems, start, length in minutes, hidden minutes at the end, languages. The rest folds under Advanced with a one-line summary. At 1440 px the page is 922 px tall (was 2,326 with 25 inputs).
- Publish stays in the title row; Duplicate and Archive sit in a More menu. Duplicate asks only for the new start.
- A contest's tabs are Setup, Live and Results, and a contest opens on the tab that fits its state, from the list too.
- The contests home shows live and next first, then one table; "Needs you" appears once a contest has run.
- Back with an unsaved change asks first, like a link. Organiser screens load through one loader that keeps only the newest answer.

### v3 stage C2: the console for contest day (2026-10-29)

- The console opens with one sentence on how it is going ("Live, 30 started, 1 question waiting, judge fine." or "Judge not seen for 3 minutes."), then Announce, Extend, Extra time, Pause judging, History of changes and If something goes wrong.
- Banners appear only when something needs action: an edited problem students have not got yet, tests that changed, a judge that is not taking work, judge errors.
- The overview shows how many started and, per problem, solved of attempted, with the queue. Questions sit beside the work from 1024 px; under 768 px the console is three tabs, Questions first. At 375 px it is 1,436 px tall (was 3,561).
- Questions: a standard answer fills an editable box, "Everyone" says what students will see, and the tab title carries the count of questions waiting.
- Extend, Extra time and Announce are dialogs that say the contest has ended after the end; the server refuses to extend a closed contest (409 `contest_ended`). Pause, resume, end now and judge again all confirm the same way.
- Student view shows the problems and the board as students see them, without the honour code or a start button (`?as=student` on standings for organisers). Results say whether the board shown is live or the students' board.
- `docs/RUNBOOK.md` has "If something goes wrong": the judge stalled, the VM is gone, sign-in is down. The console shows the same steps.
- Every page in a tab shares one event stream.

### v3 stage C1: the cuts (2026-10-29)

- Removed with their columns, routes and tests: per-student start windows, exam feedback, best single submission scoring, the switch to stop judging at a failed example (it always stops), the roster and its CSV (the allowed domains decide who takes part), and typed slugs (the address is made from the title on create and on duplicate, numbered when taken, and never changes).
- The Students page and the AI settings page are gone. The key form lives in the AI wizard ("Change key or model"), and "Forget key" is in the account menu for organisers.
- A contest's tabs are Settings, Console and Results. "Similar code" on Results after the end opens the similarity page.
- Results offer one "Reveal" button that shows students the full board; the step-by-step reveal and the editorial editor are hidden.
- `forget-user` no longer erases roster rows; an address with no account is refused. Routes: 104 to 101.

### v3 stage B: what the agent pilot found (2026-10-29)

- A save waiting to reach the server reads "Saved on this device"; only a failed save is a warning.
- Opening a contest again opens the problem left open last on this device.
- Under 1024 px the result area is the submissions alone, without Run and Your input tabs.
- The runtime error line names numbers too big for their type; the Java hints say when the class must be called Main and when to use long.
- A passing example still shows what the program printed to stderr.
- "You have problem X open" shows only while the question's topic is general.
- C++ and Java suggest their keywords as well as words already in the file.

### v3 stage B6: the contest bar (2026-10-29)

- From 1024 px a contest problem replaces the site header and the left rail with one bar: Leave, the contest's title, the letters with their scores (scrolling sideways past six, the open one kept in view, a dot on a letter whose statement changed), the clock, Messages with its count, Standings and Help.
- Leave asks while the contest is live: "Your code is saved. The clock keeps running while you are away."
- "Hide statement" gives the editor the whole width, and a column splitter between the statement and the code sets their shares (arrows, Page Up and Down, Home, End, or drag), kept on this device.
- The rail's per-problem list is gone from contests; the organiser's try page keeps its single-problem rail.

### v3 stage B4: the phone row (2026-10-29)

- Under 1024 px a contest problem drops the site header for one sticky row: each problem's letter and score, the clock, and a Menu with Standings, Messages, Help and Leave. The Problem, Code and Result tabs follow, then the title and the statement at once (the title sits about 110 px down at 375 px, was 450).
- Phones and tablets read code but do not write it (V3-14, owner answer 2): the editor is read-only with no on-screen keyboard, the Code tab says "Use a computer to edit, run or submit.", and Run and Submit are not offered under 1024 px.

### v3 stage B3: standings and messages as panels (2026-10-29)

- During a contest Standings opens as a panel over the editor column; Escape closes it and the problem stays. The standings page remains for after the end.
- Messages opens as the same kind of panel, the messages first and "Ask a question" last. The topic starts on "The contest in general" and says which problem is open. Questions stay open in practice after the end (the server now accepts them while practice is open).
- Students read "Results from 13:51 are hidden until the end." and "You are 7th of 28 so far", never "frozen"; the legend says the time is when the total was reached, from the start. The rail's Rules is Help.
- Every dialog is built on one `Dialog` component: focus returns to the control that opened it, or to the page heading when that control is gone. The account menu closes when it opens Theme or Help. After Start, focus lands on the problem's heading.

### v3 stage B2: the result tabs and the one Workspace split (2026-10-29)

- The result area is three fixed tabs: Run, Submissions (with a count) and Your input. Run on the examples selects Run, Submit selects Submissions, a run on your own input stays on Your input; nothing swaps silently and Run and Submit keep focus. Your input holds the box with its own Run and that run's result; "Run on this input" on a failing example fills it and switches there. A submission that failed on an example offers "Run to see your output".
- The compiler output starts with one line naming the first error and its line ("Line 12: expected ';' before 'return'"), a button to jump there.
- `Workspace.svelte` went from about 2,000 lines to 566: the code and its saving in `lib/draft.svelte.ts`, submitting, running and the tabs in `lib/judging.svelte.ts`, and the grid, rail, title, sticky strip, toolbar, status line and result pane in `components/workspace/`; `Tabs` and `Splitter` are shared components.
- A 401 from any request signs the page out in one place (`lib/api.ts`); the 19 copies of that code in the screens are gone.

### v3 stage B5: before the start and after the end (2026-10-29)

- Before the start: the countdown, three facts (length, languages, wait between submissions), the honour code and "I agree, start the contest", all in the first screen on a phone. The full rules and how Run and Submit work moved to Help ("Read the rules and how Run and Submit work", or "?"); the example verdict and the "How the workspace works" panel are gone.
- After the end: the place and score, then one list of problems, each with its score, opening to "Open" and the student's own submissions; the problem with the newest submission opens by itself. The separate score table, "Review" and "Practise" are gone. Messages are folded under the result.

### v3 stage B1: the editor and its surroundings (2026-10-29)

- The editor takes the VS Code keys (`@replit/codemirror-vscode-keymap`, V3-13) plus the few it leaves out: insert a line above, scroll without moving the cursor, grow and shrink the selection, F3 and Ctrl+H, and Alt+C, Alt+W, Alt+R in the find box. Find and replace, auto-closed brackets and quotes, completion (words from the file for C++ and Java), multiple cursors (Alt+Click, Ctrl+Alt+Up and Down, Shift+Alt+drag for a box), folding, word wrap and tab size 2 or 4. Podium's Ctrl+Enter and Ctrl+' still win. Escape closes a completion list only while one is shown, so Escape then Tab always leaves the editor.
- One Help dialog, opened by "?", the footer, the account menu and the contest's Rules: the rules, Run and Submit in two lines, then every shortcut. The shortcut table in `web/src/lib/shortcuts.ts` is the contract; `web/tests/editor-keys.spec.ts` presses every editor row and fails on a row it has no check for. An editor key pressed outside the editor says where it works.
- Reset sits beside the language and asks "Replace your code with the starter?". Settings holds font size, tab size, word wrap and Tab moves focus. Under the editor: Saved, Saving, or "Not saved to the server, kept on this device", then "Ln 2, Col 5".
- The language chosen last is remembered per account, across problems and contests. Switching languages keeps the code typed so far for each language on this device and brings it back; over typed code the switch asks before loading the starter.
- A run shows the end of what the program printed to stderr (at most 4 KB) for each input; a submission never carries stderr. The run labels are "Example 1" and "Your input".
- Words: examples, not samples; Run, not "Run on samples"; "Your input"; "Open contest" is the one label on the pinned contest; practice reads "Practice. Your contest score is still 60 of 100."; the clock no longer carries a freeze line; the message strip offers "Dismiss all". Examples and subtasks carry their one-line meaning where a student first meets them (V3-27).
- The statement column no longer stretches the page when a statement is long (an absolutely placed hidden line escaped the scrolling pane).

### Oracle Always Free hosting (2026-10-29)

- Releases build for arm64 too (V3-28): `make release ARCH=arm64` writes `podium-<version>-arm64.tar.gz`, `MANIFEST` records `arch=`, and `install.sh` refuses a release built for another processor. On arm64 isolate 2.2.1 is built from its upstream commit with the apt package's paths, since upstream publishes no arm64 package; Litestream installs its arm64 build, pinned by SHA-256. `make sandbox-binaries ARCH=arm64` builds the sandbox suite for an ARM VM.
- `install.sh firewall` opens http and https on cloud images that ship their own iptables `REJECT` (Oracle's Ubuntu): installing ufw replaces the image's loader at the next boot, and the step removes the image's loaded `REJECT` rules now. `install.sh verify` fails when a `REJECT` still sits ahead of 80 and 443.
- Offsite nightly copy (V3-30): with `PODIUM_OFFSITE_*` set in `backup.env`, each checked nightly backup, database and test files, is streamed as `<stamp>.tar.gz` to a second bucket at another provider. The key stays in a `podium-backup` process, the script never deletes there, and a failed upload keeps the local copy and fails the unit. `podium-restore offsite` restores from it, `podium-check` alerts when its newest archive is over 36 hours old, and `tools/offsite-test.sh` rehearses both against a local stand-in bucket.
- Watchdog (`.github/workflows/watchdog.yml`, `tools/oci-watchdog.sh`): every 30 minutes it asks Oracle's API for the VM's state, starts the VM if Oracle stopped it, checks `/healthz`, and pings a heartbeat that alerts on any change and on a missed run. Requests are signed with curl and openssl, no third-party action; `tools/oci-watchdog-test.sh` checks the signatures against a local stand-in. The job is skipped until the `PODIUM_URL` variable is set.

### v3 stage A (2026-10-28)

- End the contest now during the freeze keeps the board frozen (V3-18). The remaining freeze is rounded up to whole minutes and never to zero, so the frozen moment moves at most 59 s earlier; before, an end inside the first minute of the freeze stored no freeze and released a held board at once. The confirm names the moment the board stays frozen at.
- The gap between submissions is per problem (V3-17). A student who submits A can submit B at once; before, B was refused for the rest of A's 30 s.
- Security: a flood of forged session cookies from one address no longer locks out real students behind the same address, such as a campus NAT (V3-19, security audit 2026-10-28 H1). A session the server issued or accepted in the last minute skips the per-address refusal and is checked against the database as before.
- A database from before v3 makes the server stop with "this database predates v3" and what to do (move it away or restore a v3 backup), instead of saying it was written by a newer release (walk bug B7).
- Grading correctness (stage A5, closing S1-08): a problem revision, its tags, samples or generator arguments that cannot be read are an error, never an empty list, so a pin update can no longer store no points and the AI-draft guard no longer fails open. The schema checks `json_valid` on revision samples and subtasks, problem tags and samples, subtask generator arguments and contest audit details, and revision tests are only examples or hidden. Four more `rows.Next` loops check `rows.Err`; the guard test now counts per loop and per variable. Tests prove a student's extra time keeps a contest live for the setter lane gate, the edit reason and both integrity retention passes.
- Tests pin the student workspace before stage B changes it: switching problems keeps each problem's code, an autosaved draft comes back from the server after a reload, running on your own input, the verdict's subtask rows, announcements and Dismiss. Browser tests find the editor and the toasts by role and name; the toast stack is a group named Notifications. Go tests cover End now with a personal extension and an excluded student missing from the board and the CSV.
- CI: every action is pinned to a commit SHA, CI runs on push only, the core path runs first in the browser job, the Playwright browser is cached, `make test` never reuses cached Go results, and Vitest reuses its workers (24 s to 4 s locally).
- README: the status line describes v3, the deploy steps no longer mention the removed deploy guard or the `age` key, and the load test guide says to copy `tools/dev-stack.sh` to the VM, since the release does not ship it.

### v3 stage 1 (2026-10-26)

- Integrity (V3-2, owner answer B): an organiser records one decision per student, No action, Warning or Exclude from this contest, with a reason; an excluded student leaves the board and the export. Appeals, proposals, the two-person rule, deadlines, notes, the rating and standing, notices, student progress, summaries, the class view, record views and the term rollover are removed. New: a CSV export of similar pairs. Retention keeps only sessions, integrity events, run payloads and backups (`05a31e1`).
- Removed per-contest co-organisers and student TAs: every organiser route needs the admin or owner role, and students get 403 on organiser routes before any contest is read (`4132775`).
- A published contest keeps its slug, so old links never need a redirect. Pinning a problem revision stays ("Update students now"); revision lists, statement diffs, the pin history and the served log are removed, and students learn of a change from the announcement (`06fbed7`).
- Every page is `noindex` and `robots.txt` disallows all: the server-filled page head, Open Graph, JSON-LD, the sitemap, `routes.json` and the web manifest are removed (`d4e4e2b`).
- Removed the contact form and its inbox (`/api/contact`); the footer email is the contact channel. Removed the deploy guard, `upgrade --force` and the site-updated banner; the no-deploy-within-48-hours rule is in the runbook (`52c913a`).
- Security: opening Podium without a session removes every draft left in the browser, so a lab PC does not keep the last student's code. Caddy sends HSTS. The server unit drops capabilities, devices and kernel access. A college domain that has signed in through Google Workspace once refuses Google accounts without the matching `hd` claim. Unknown session cookies count against a per-address budget before the database is read; the pre-auth burst falls from 5000 to 2500. CI checks out without a stored token, runs `go test -race`, and npm never runs install scripts.
- Removed the toolchain registry (V3-4): the server no longer stores the judge's compiler report, the console no longer flags toolchain drift, and the feed and export no longer carry `toolchain`. The judge protocol is unchanged.
- Backups are Litestream plus the nightly copy on the VM (V3-10). Removed the age-encrypted B2 copy, the rsync copy and the weekly automatic restore proof; `podium-check restore-proof` stays, run by hand once a term.
- Removed "Can a model solve this?" from the problem editor and `POST /api/admin/problems/{id}/solve` (V3-4). The AI wizard keeps drafting problems with all three providers.
- Themes: light, dark, high contrast light and high contrast dark, plus System (V3-8). A saved choice of a removed theme falls back to System.
- Two test kinds: examples and hidden tests (V3-5). Open tests are gone; a format 2 pack's open tests import as samples, exports write format 1, the shipped average-speed pack lists test 7 as a sample. The judge protocol is unchanged. The run headline reads "All 3 examples passed" or "2 of 3 examples failed" (S1-09).
- Every contest counts (V3-6): the `graded` setting, `confirm_ungraded` and live ungraded contests are gone; student times round to 100 ms in every contest. Practice after the end no longer reveals a hidden test while another contest that pins the same problem has not closed, drafts and upcoming ones included (security finding M2).
- One migration, `0001_v3.sql`, replaces v2's 0001 to 0015 (V3-3): 30 tables, down from 54, with `json_valid` checks on scoring JSON and the `contest_final` view. Not an upgrade: install fresh.
- Grading correctness (S1-08): every "over for everyone" query reads the `contest_final` view; the board refuses a score it cannot read instead of scoring zero; every `rows.Next` loop checks `rows.Err`; each organiser action writes its audit row in its own transaction, so a failed row rolls the change back; the organiser contest list no longer runs three queries per contest.

Nothing below is tagged or deployed. The v2 phases were planned as the releases v1.0.0 (P0) to v1.5.0 (P5) and v2.0.0 (P6); they reached `main` together, so they are proposed to ship as one release once the P0 drills on the VM pass. The owner decides the version and the upgrade path (D236): either one release for P0 to P6, installed fresh, named `v1.5.0` or `v2.0.0`, with `UPGRADES_FROM` set to that version, or `v1.0.0` to `v1.2.0` tagged as markers and the rest as one release with `UPGRADES_FROM` set to `v1.2.0`. Until then the `Makefile` keeps the placeholder `v1.0.0`. The phase labels below say where each change came from.

### v2 P6 (close)

#### Backend (2026-10-21)

- No contract migration: P4's migrations 0013 and 0014 kept no column for a release, so the schema stays at 0014 (D259).
- `tools/install-test.sh` checks the upgrade-path refusals: an older release than `upgrades_from`, an unversioned install and a downgrade are refused before anything changes. A release must be named `vX.Y.Z` (`make release VERSION=vX.Y.Z` on an untagged commit), so every install can be upgraded later.
- Final security and code review of the whole v2 diff: `podium-check` no longer evaluates the server's `/healthz` numbers as shell code as root; `install.sh` makes podium's folders as podium, so a planted symlink cannot make root chown another folder; a rollback refuses to run on a missing or unreadable database; the configuration check runs before the deploy guard, so a forced deploy is recorded only for an upgrade that goes on; temporary files and the deploy folder on the VM get random names; `podium-server migrate` refuses a database in use; a failed prune no longer stops the copy off the VM; `check-secrets` knows age, PGP and fine-grained GitHub keys, alert URLs and key and database file names.
- The nightly check no longer fails on the pins retention releases, which would have refused every backup from the first contest's fourth anniversary; an expired contest is never summarised again as all zeros.
- A case shared during a student's live contest stays hidden until it ends, even when cleared first. A reviewer never sees signals, notes or rating lines of a case they are a student of. A co-organiser whose site role is student no longer reads extension reasons or the held reveal's full board. A renamed contest's old web path redirects only when signed in.
- Proposals, pin updates, the rating rollover and the results notice re-check what they read inside their write; the class view's most failed test ignores skipped tests; the feed shows no note count on a reviewer's own submissions; the term rollover label refuses tabs and hidden characters; new `extension` rows in the contest audit name the student by id only, without the email.

#### Frontend (2026-10-21)

- A student's first load no longer carries the organiser screens: every admin route loads on first visit. The entry script and its preloads fell from 675 kB to 332 kB (211 kB to 112 kB gzipped); a CI step fails when they pass 120 kB gzipped or the stylesheets 16 kB (F8.1, F8.3; the step has not run on GitHub yet).
- KaTeX and its stylesheet load only when a text holds maths; until then a formula reads as its TeX source. Statement images load lazily. The 500 face and Fragment Mono are preloaded with 400 and 600 (F8.2, D91).
- Toasts fade out at the short duration; the subtask strip fills by transform; both stay still under reduced motion. "Grade as the AI wrote it" names what is graded (F8.4).
- The problem editor's preview and context column sit under the sticky bar at any height, and fields reached from the error list clear it (F8.5, R3).
- Compile-error jump buttons start their name with the location they show (X6).
- Admin and reviewer forms mark required fields, say which are optional, and move focus to a field error the server returns (X7).
- Final web audit: date and time inputs show a focus ring on their calendar button, disclosure summaries are 24 px tall, and code, test data and compiler output are never machine-translated.

### Real-stack walkthrough fixes (2026-10-21)

Found by walking the whole organiser and student path on the real server, judge and web app (testing mode, side ports), not on mocks.

- Ending a contest early no longer moves the board's frozen moment into the past: results students had seen on the live board vanished behind "pending" when the freeze was counted back from the new end. Ending before the freeze drops it; ending during it keeps the frozen moment. The confirm says which.
- A student's open workspace reads the contest again when its event stream comes back after a server restart, and on every fallback poll: announcements, public answers, statement updates and time changes sent while it was down used to be lost until a reload.
- The console's student pickers (extra time, feed filter) name students by email beside the name; accounts without a name read "Student 3". The organisers' board carries each student's email for site admins only.
- After editing a problem a live contest pins, the editor links each contest's console, the console's attention strip names the problem with a newer revision until the contest closes, and a link's `#section` scrolls once the screen has drawn it.
- A question's page shows the 7-day reply deadline its first open starts, not the 21-day cap from before it.
- Students read back the appeal they sent; a reviewer who took part is told an owner adds an uninvolved one on People; proposing "Zero for one problem" preselects the problem the evidence is on.
- Tests without outputs or a subtask are named in one line ("Tests 1-6 have no expected output") in the editor and the server's list; a validated problem's advice is headed "Worth adding"; revision 1 reads "First recorded version"; a hidden test's withheld memory is blank instead of "0 KB"; People shows "No name" for an organiser who signed in without one; the class view's screen-reader words say "1 student".
- `tools/dev-stack.sh` passes `PODIUM_BASE_URL`, Vite reads `PODIUM_DEV_API` and the live Playwright config `PODIUM_LIVE_PORT`, so a second stack runs on side ports (`docs/OPERATIONS.md`).
- `web/tests-live/history.spec.ts` (C2-055): a mid-contest statement edit with its reason, "Update contest to revision N" and the student's banner and diff, against the real server.

### Walkthrough follow-ups (2026-10-21)

The "still awkward" items of the real-stack walk that needed no owner decision.

- A proposal waiting for a second reviewer shows where they look: the contest's case list carries each viewer's `may_confirm` and marks such cases "Waiting for your confirmation" with a count above the table, and the console's attention strip links "1 case waiting for your confirmation" to it, from a new count route that names nobody (`GET /api/admin/contests/{id}/integrity/waiting`). The notices are student-only, so no organiser notice (D321).
- A run time the server rounded to 0 reads "under 50 ms" in a graded contest and "under 5 ms" elsewhere, not "0 ms"; submissions and runs carry `time_step_ms` (D322).
- The workspace's limits line gives the time the selected language gets, with the reason ("Time limit 2 s in Java 21 (twice the problem's 1 s, as it runs slower than C++)"); each language carries the judge's `time_factor`. The validation report says "of the 2000 ms Java 21 gets (twice the problem's 1000 ms)"; the summary carries `reference_language` and `time_factor` (D323).
- The student home features a live or upcoming contest, or the newest past one the student took part in, never a contest they missed (D324).
- The judge waits for an isolate box another process is using (five tries over about 4 s) instead of failing the submission at once, then fails it with a judge error that names the likely cause; it never re-initialises a box it already holds (D325).
- The live Playwright specs follow the current screens: the "Problem bank" nav, adding problems that save at once, "Publish contest" with its confirm, the proposal-based case page and "Send explanation"; m4 ends its contest afterwards (D326).

### Tidy-up (2026-10-21)

- Closing the keyboard shortcuts list returns focus to the page heading when the element that opened it was replaced while it was open (for example "?" pressed while a problem loads); focus no longer falls to the page body.
- Inline code in statements and Markdown uses the mono optical size token; small spacing and rail-width values now come from tokens (design drift found by re-deriving the design document).
- Tests: the M3 memory-limit cases run with a 4 s time limit, as node22's took up to 2.5 s under load and was judged TLE (C2-011); pixel tests allow 20 pixels at threshold 0.1 and load every font face first (C2-046); two tests that failed only under parallel load wait for the state they need.

### Council run 2 fixes (2026-10-21)

#### Backend and operations

- `tools/deploy.sh` names the release before it asks the guard or builds: the commit's `vX.Y.Z` tag, or `VERSION=vX.Y.Z` for a drill build; anything else, or uncommitted changes, stops at once instead of after the build. `tools/deploy-test.sh` covers it in `make check`.
- `make package` builds the release tarball without `make check`, for the upgrade drill's build with a never-committed migration.
- The release workflow builds a tag only when `ci` passed on its commit, and `make release` refuses a `v1.5.0` or later release that still upgrades from the placeholder `v1.0.0`.
- A semester rollover apply can name its preview's time (`as_of`), so it disables only the accounts the preview listed.
- The organiser feed and the JSON export name the compilers that judged each submission (`toolchain`).
- `GET /api/events` accepts a browser's `?device=` id, so two phones of one model on one account count as two devices.
- A problem opened before its contest starts says the contest has not started yet, instead of asking to accept rules that cannot be accepted until the start.
- A reviewer who is a student of a case with a classmate can no longer write notes on the classmate's submissions in that contest, open a case from them (which copied the notes), read a manual case about them, see their note counts in the feed, or edit, confirm or decline their rating (D262 on every path). Notes and cases on one's own submission answer 404 instead of 400.
- Opening a case (from signals or from a submission) is logged as a read of it, as every later read is (D97).
- The bank's "hidden tests shown in practice" mark counts only published contests, and a problem whose hidden tests a published contest has shown can no longer be taken out of that contest, which used to make the mark disappear.
- `install.sh` reads the backup folder from `PODIUM_BACKUP_DIR` (upgrade, rollback record and verify), and nightly backups are named to the second.
- The rsync and encrypted copies never stop the local backup: a problem with their settings warns before it and fails the unit after it; `install.sh verify` checks the rsync copy's root key.

#### Frontend

- The problem editor knows open tests: an "Open tests (optional)" field under the samples, an "Open test" group instead of a red "in no subtask", and a judged change when they change. The AI drafts and the average-speed pack open clean.
- An unpublished contest past its start is a draft, not live: it says students cannot see it until it is published. Turning grading back on after an ungraded start asks first, and a problem the server keeps in its contest says why.
- The rules say that Run also checks open tests, and that an ungraded contest shows the first failing hidden test; the workspace names its open tests; the editor's grading and practice options say what they reveal.
- A refused problem shows the server's reason, with a link to the contest page when the rules are not accepted yet or the time is up.
- "Write to the organisers" is gone from the contest workspace and for organisers, and the contact page sends live contest questions to Messages; the organiser inbox is "Inbox" everywhere.
- A first-time organiser sees the steps to a first contest with what is done, a draft as the next contest says to publish it, and the empty Students list says how students arrive.
- A reviewer who loses a race on an integrity case sees the case as it now is; account actions on People wait until the account is named; the student record and the Students filter show the latest answer; the rollover applies as of its preview; the console names the compiler that judged each submission; the event stream carries the browser's device id.
- The problem editor's context column scrolls, so a long preview's last buttons can be reached; lazy organiser screens show a loading line and a reload button.
- Copy: "Choose a contest", "Send explanation", "Only organisers can ...", examples as help text instead of placeholders, "Problem revisions", the slug's help in the editor.
- Tests: the every-button test can fail (and covers organiser screens), toast checks look inside the toast region, fixed waits became signals, the mocks answer what Go answers, and the size budget counts the preloaded fonts (99.97 kB of 108).

### Council run 1 fixes (2026-10-21)

- Rollback keeps the database group-writable for Litestream; an upgrade that stopped half-way is refused instead of overwriting the release kept for rollback; a rollback says which forget-user erasures it undoes and writes them down to apply again; old pre-rollback database folders age out with the backups.
- The rsync copy off the VM runs as root with a root-only key; `podium-check` alerts on a stalled judge, and the judge stops (exit 4) after five refused tokens in a row.
- Student summaries are rebuilt after a rejudge during a build, a review decision or a settings change; a requeued job gets a fresh deadline; a submit, run or rejudge whose pinned revision moved after the Ready check is refused; migration 0014 lets students of a contest still live at the 0013 upgrade get their "results are final" notice.
- The bank marks problems whose hidden tests practice or an ungraded contest showed, and turning grading off after the start needs a confirm.
- A phone that changes network no longer counts as a second session. The appeals list is paged and logs its reads in one commit; the students pages read the summary columns in SQL; the notes dialog shows the program.
- Error toasts are never evicted by newer info toasts; a failed inbox delete shows inside its dialog; focus rings in the workspace are drawn; the contest page after the end keeps the answers for everyone; the Submit countdown keeps text contrast; the workspace toolbar is one row from 1100 px; the student link row stays out of the phone workspace; every event stream stops at sign-out.
- The no-JavaScript privacy page has its retention table, and `/contact` names the address to write to.

### v2 P5 (planned as `v1.5.0`)

#### Frontend: workspace (2026-10-21)

Student-facing: it goes live in a slot the deploy guard allows, announced in that contest's description. Each item is its own commit.

- Under 1024 px one strip sticks to the top: the time, the previous and next problem, the Problem, Code and Result tabs and Submit, which is always one tap away. The limits open the Problem tab (F7.2, audit S7).
- The workspace fills the window with no minimum height; short laptops (1366 by 768 at 125 percent) get a tighter title row, a narrower rail and a one-row toolbar (F7.3).
- When Submit or Run switches the tab by itself, focus moves to the result; when the time runs out, to the page title. On touch screens the editor and custom input are at least 16 px, and the on-screen keyboard resizes the page (F7.4).
- The result panel can be resized with a drag or the keyboard and hidden to one line that keeps the verdict; the size is kept on the device (F7.5). The verdict and subtask strip stay at the top of the result while the tests scroll (S5).
- Sign out during a live contest asks first; the draft is saved before the session ends (F7.6).
- The editor's keyboard hint is linked to the editor for screen readers; the page leaves room for the sticky strip when something takes focus (F7.7).
- Each editor language loads when it is chosen: 34 percent less editor code for C++ (F7.8).
- Rail titles take two lines; the rail is wider from 1280 px and the back link sits under the clock (F7.9, S6).
- "Reset to starter" in Editor settings asks first and saves at once, so a reload keeps the starter (F7.12).
- A practice submission shows its first failing hidden test in full, and every run its first failing sample or open test, with "Run on this input"; a contest submission never does. Runs count open tests in their headline (F7.13, D115).

### v2 P4 (planned as `v1.4.0`)

#### Backend (2026-10-20)

Schema change (0013, expand only; 0014 corrects one of its back-fills): rolling back across it is a restore. The privacy page waits for the owner's re-approval (input 29).

- Manual review: private notes on a contest submission for reviewers (deleted after 30 days unless a case is opened), and cases opened from a submission with its notes as evidence.
- Outcomes (warning, zero for a problem, referral) are proposed by one reviewer and confirmed by a second who did not open the case; the owner alone only as the sole organiser, with a reason. Zero for a problem changes the final board for every later computation, and the results page and organiser exports say so (D77 amended).
- Appeals within 14 days, ruled on by a reviewer who took no part; an upheld appeal removes the outcome, its rating change and its zero.
- Time limits: 7 days to reply from first opening, 21 at most, a reminder on day 5, 14 days to decide, appeal and rule, with overdue marks for the owner.
- Notices (case shared, reminder, decided, appeal ruled, standing adjusted, results final) in an inbox with an unread count on `GET /api/me`; never delivered during the student's live contest; no names, code or test data.
- The rating (0 to 10, organisers only) and the Standing band students see (D110): outcomes, manual edits with a reason (a band-lowering one needs a second organiser), appeals, and `podium-server semester-rollover LABEL`.
- The record page, the record view log and reviewer oversight for the owner (D97); the students table with its two filters, the student page with the class median, and the class view of a final contest.
- Per-student contest summaries written at final and after a rejudge; `GET /api/me/progress` and the organisers' progress route (C8); `hide_progress` in the preferences.
- Retention rows for notes, notices, record views, the rating history, summaries and standing entries; erasure also removes notices and summaries.
- The contest audit's `extension` rows record `reason_given` instead of the reason text.

#### Frontend: organiser screens (2026-10-21)

Organiser screens only, plus one line on the student standings and result; needs the P4 backend routes (C8, C9, C12) and the new `GET /api/admin/accounts`.

- Students at `/admin/students`, a new item in the organiser nav: every student sorted by name, never by score, with graded contests, the latest graded percent, the last submission and the rating; sort by rating on request; filters "Missed the last 2 graded contests" and "10 or more submissions, nothing accepted", each naming the contests it used; rows stack on phones.
- A student's page at `/admin/students/{id}`: score over time beside the class median (with "Show as table") and each contest's score, median, place, problems solved and accepted submissions; links to the record and to the progress page as the student sees it.
- The record page at `/admin/students/{id}/record` (every visit logged for the owner): the rating out of 10 and its band, its history with the line that crossed a band marked, "Edit rating" with a required reason and a confirmation naming the old and new band, "Waiting for a second organiser" when the band would drop, the standing as the student sees it, cases, private notes and appeals; an appeal against an adjustment is ruled on here.
- Cases are decided by proposal: "Take action" is replaced by "Propose an outcome" (warning, zero for one problem or referral, with the reason the student reads), a second reviewer who did not open the case confirms it, and the page shows the deadlines, "Overdue", the policy line and the two-person rule as it applies to you. Appeals on an outcome are ruled on the case page by a reviewer who took no part. The case list marks overdue cases and proposals waiting for a second reviewer.
- Private notes and "Open a case" from a submission, from the console feed; feed names, board names and result names open the student's progress page.
- Results: the class view (per problem and subtask, totals only), the line "Board updated on <date> after a review decision" (also on the student standings and result; the board carries no mark), the reveal, downloads and integrity beside the board. The exports' "zero by decision" column is explained beside the downloads.
- People: Account help looks any account up by email, students included, and disables its actions for an address with no account; new sections for the organisation details shown on public pages, the semester rollover (preview, then archive and disable), and, for the owner, integrity reviewers and the record view log; a jump list from 1280 px.
- The console's Changes say whether extra time came with a reason (`reason_given`), and list review decisions that changed the board.
- Shared: a danger secondary button style, split columns that never widen a phone page, MonoDigits keeps its spaces inside a badge, one `errorText` helper for failed requests on organiser screens, and focus reaches the heading of a lazily loaded screen.
- Backend: `GET /api/admin/accounts?email=` for owners and admins.

#### Frontend: student screens (2026-10-21)

The privacy text changed and waits for the owner's re-approval (input 29); it stays unlinked until then.

- "Your progress" at `/progress`, a new item in the student nav: the figures row, score over time (inline SVG with "Show as table"), each contest's problems drawn like standings cells, topics weakest first, difficulty, where points were lost with one line of guidance, and what to practise next. Only final results; the place is small and muted, and there is no comparison with anyone (D94, D95). Owners and admins open the same page at `/admin/users/{id}/progress`.
- Your contests sums up the final results in one line with a link to progress (D92). A final result links to progress and names the problem to practise next; nothing is asked while a board is frozen or held.
- Notices at `/notices`, with the unread count beside the account button and in the account menu: what happened, what to do, by when, and on a question the line "Nothing affects your marks until a person decides". Notices wait while the student is in a contest.
- `/integrity` is now "Questions and standing": the standing band and its dated lines with reasons, never a number, with an appeal for each line, then the questions. Each question has its own page, `/integrity/cases/{case}`, with the evidence (a manual case's lines highlighted), the explanation, the decision with its reason and the appeal form.
- "Hide my progress" in the account menu. A problem a review decision set to zero says so on the student's own result, with a link to the case.
- The privacy notice lists the P4 data: contest summaries, notices, private notes, questions, decisions and appeals, the standing, the organisers' rating, the record view log and the hide-progress choice, with who sees each.

### v2 P3 (planned as `v1.3.0`)

#### Backend (2026-10-20)

Schema change (0012); the head, manifest and robots work is live-safe on its own.

- Retention (D85, PLAN section 9): every period is one constant in `server/internal/retention/periods.go`, returned by `GET /api/auth/config` for the privacy page and listed in `docs/OPERATIONS.md`. A daily job empties run payloads after 30 days, deletes contact messages after 180 days, unused problem revisions after 90 days, and submissions and audit rows 4 years after the contest, keeping each expired contest's final standings in `contest_results`.
- `podium-server forget-user EMAIL` erases one person on request (dry run unless `-apply`): sessions, drafts, integrity events and contact messages go, the name and email become "Removed user N", scores stay.
- Semester rollover (`POST /api/admin/rollover`, owners): archives the term's contests and disables student accounts unseen for 180 days, after a preview.
- Public pages (D100): the server reads the web build's route manifest, answers unknown paths with status 404, fills the head of public pages (title, description, canonical, Open Graph, Twitter card, `index`) and keeps every other page `noindex` with `X-Robots-Tag`. `/privacy` reads without JavaScript once the build carries the approved text. New `robots.txt`, `sitemap.xml`, and an Organization JSON-LD on `/signin` from the site settings.
- Site settings (`PUT /api/admin/site-settings`, owners): organisation name, contact email, phone and privacy contact, returned by `GET /api/auth/config` as `organisation` (C5). The contact email replaces the owner's own address on public pages once entered. No contest data before sign-in.
- Contact form (C6): `POST /api/contact` for signed-in people, 5 per person and 100 in total per 24 hours, and an inbox for owners and admins (`GET` and `DELETE /api/admin/contact`). No mail is sent.
- Queue health on the organiser contest response (C10): waiting and judging jobs, the oldest wait, judge errors, unanswered clarifications and when a judge last asked for work.
- Before sign-in, requests are limited per client address with budgets sized for a class behind one NAT, a refused sign-in lands on `/signin?error=rate_limited`, and each Google sign-in attempt can finish only once (P0 security review, finding 4).
- `contest_problems.pinned_hash` is gone (the contract step for v1.2.0); `pinned_hash` on the contest response is now the pinned revision's hash. A v1.2.0 binary cannot read the new schema, so rolling back is a restore (`install.sh rollback` offers it).

#### Frontend: organiser (2026-10-21)

Ships with `v1.3.0`. Live-safe: organiser screens only; no student screen changes. Needs the C10 queue health fields of the P3 backend for the console's attention strip (older servers show fewer items).

- Problem editor (F5.1, F5.2, A3 to A6; audit O2): the 2,626-line route is split into section components under `components/problem/` (the route is under 700 lines). From 1280 px a sticky column beside the form holds the readiness panel and the preview. The panel names what blocks validation, the warnings, statement images without a description, the generate chain while it runs, and whether the validation covers this version or which unsaved judged changes need a new one; once validated it offers Add to a contest. A finished validation says its result in the sticky bar with a link that focuses the report. Sections follow their dependencies (statement, solutions, tests, generator, judging), the jump links carry a state and scroll as one row on phones, the checker reads in plain words with help, an unused generator is one line, removing the brute force, validator or generator can be undone until the next save, and the preview shows the samples as students see them.
- Problem bank (audit O6): import takes several packs at once and validates each in place; status is a badge; rows that are not validated offer Validate.
- Contest editor (F6.3; audit O1, O8): Publish contest is in the title row with a status line and a confirmation that lists the checks; a sticky checklist says what is missing; problems save on every add, remove and reorder (no Save problems) and show their points and pinned revision; Basics come first with Starts and Ends on one row; rules and scoring fold under a summary; holding the board for a reveal needs a freeze; leaving with unsaved settings asks first. Unpublished contests read Draft, and ended ones with practice read Ended, practice open, on every organiser screen.
- Contest list (F6.5; audit O6, O9): the state column is the organiser state badge, rows stack below 1024 px, row links are quiet buttons, and an empty list points to the problem bank.
- Console (F6.1, F6.2; audit O3, O5): an attention strip with time, freeze, judging, queue health, judge errors, the judge's last request for work, unanswered questions and toolchain drift; questions and announcements beside the feed; outcomes as students read them (Partial, 40 of 100); minutes from the start; a quiet Judge again per row; risky actions that state their effect; an announcement preview and confirmation; one list of standard answers; a failed refresh that keeps the rows; End the contest now.
- Reveal (F6.4): room-scale rows, the action colour only on the row that just moved, Skip to the top 10, Release the full board at the end, a board that was never frozen says there is nothing to reveal, plain labels, and a FLIP loop that reads before it writes.
- AI (audit O7, A7): the key form is shown inline in the wizard when no key or model is active; the wizard follows the draft until it is validated; two copy defects fixed; Generate draft.
- People (audit O10): Account help says whose account it acts on when the page knows it, each confirmation names the person, Disable account is a danger-coloured secondary button, and Getting started counts organisers other than the configured owner.
- Integrity (F6.2, C3, C7): checking again asks first; Open a case comes after the timelines. (The Take action button described here was replaced in P4 by propose and confirm; council C1-003.)
- Dates on organiser screens set only their digits in mono (audit O9).

#### Frontend: student and styles (2026-10-21)

The workspace items (S2, S4) are student-facing; each is its own commit.

- A shared layout and control system (usability audit 2026-10-20): a two-column working layout from 1280 px with a sticky context column (`.page-split`), surface panels for task areas, badges for state and verdict words, one button system (32 px, one primary per view, quiet row and bar actions with no underline) and mono only for digits (`MonoDigits`). The editor well has a visible edge in dark themes.
- Messages that arrived while a student was away (a reload, coming back from the contest page) now show in the banner and as the count on Messages, until read on that device (S1).
- The Console count of unanswered questions shows on every organiser contest screen again (O4: the request missed the API).
- Before the start the countdown is the display figure, then the honour code with the start button; the rules and the workspace guide sit beside it (S3).
- After the end the place is the display figure, one line names the end and the score, the newest submission opens by itself, and the answers to the student's own questions are on the page (S9).
- Your contests pins the live or next contest on top with its countdown and one button (S10).
- Standings cells draw the earned share as a bar along the bottom, a tried zero reads as a zero, the legend is one line with a disclosure, and Jump to you steps aside while the own row is in view (S8).
- The workspace toolbar is one row; font size and Tab moves focus moved into Editor settings; Custom input opens in the result pane; the workspace fills the window under the header (S2). The submit gap counts down inside the button, and a submit after a run that did not compile asks once (S4).
- Sign-in and the footer show the organisation's name, email and phone (C5); `/signin?error=rate_limited` explains itself; signed-in people can write to the organisers at `/contact`, and owners and admins read the messages at `/admin/contact` (C6).
- The privacy notice is drafted and built for the no-JavaScript page; it stays unlinked and non-public until the owner approves it (D85).
- A social card, `favicon.ico`, a touch icon and a web manifest; a Playwright crawl of every link and button.
- "The site was updated at 14:02" for students after an emergency deploy, once per update (D112).

### v2 P2 (planned as `v1.2.0`)

#### Backend (2026-10-20)

Schema change (0010, 0011). Judge protocol 3: install on the server and the judge together.

- Problem revisions (D61): every save that changes a problem makes a numbered, immutable revision with its statement, limits, solutions by hash and tests by kind; saves that change nothing make none. Revision 1 is back-filled for every problem; a contest pin older than its problem becomes a "reconstructed" revision (tests only). While a live contest pins a problem, a change needs a reason.
- Contests pin a revision. Students are served the pinned revision's statement and samples, never the bank's working copy. "Update contest to revision N" (`PUT /api/admin/contests/{id}/problems/{label}/revision`) moves the pin with a prefilled announcement; the pin history is on the contest response; submissions, runs and rejudges record the revision they were judged against.
- The served log records the first time each student is served each revision; `podium-server saw` answers "what did X see at T".
- Students read the statement history of a contest problem with a line diff (`GET /api/contests/{slug}/problems/{label}/revisions`), statements only; a `statement` event and `statement_updated_at` drive the "Statement updated" banner. Organisers read revisions and diffs on the problem routes.
- Site audit (D62): role, domain, account, session, Google link, run mode and forced deploy changes, with the actor as they were, on `GET /api/admin/site-audit` (People, History). Contest roster entries in the audit name the emails added and removed; the contest audit takes `action` and `actor` filters (console, Changes). A test proves no AI key, session token, cookie or OAuth code reaches either table.
- Renamed contests: the old slug answers `301` on the web app's and the API's GET paths, and cannot be given to another contest.
- Open tests (D115), pack format 2: a third kind of test beside samples and subtasks. Run checks them after the samples and stops at the first failure, which it reveals with its input, expected output and the program's output. A practice submission (and one in an ungraded contest) reveals its first failing hidden test; a counting submission never does. The AI wizard proposes open tests; the `average-speed` example has one.
- `GET /api/me` carries `site_updated_at` for 12 hours after a forced deploy (D112), and `deploy-check -force` writes a site audit row once the table exists.
- `podium-server migrate -db COPY` rehearses an upgrade on a copy; `check` reports any problem whose working copy skipped its revision; `runs-report` gives the P2 calibration row of runs with a custom input per contest.
- Append-only tables honour a retention cutoff row (D69), which the P3 retention job will write.

#### Frontend (2026-10-20)

The history screens need the backend's C3 and C4 routes.

- Organisers get their own navigation, named by job: Contests, Problem bank and People. Signing in lands them on their contest list, with the live or next contest first, its countdown, its unanswered questions and Console first in its links. Live and frozen contests stay at the top, also when extended.
- One account menu: who is signed in and as what, Theme and colours (now a dialog), Keyboard shortcuts and Sign out. The header fits one row from 768 px. Students on phones keep their links in the header with no Menu button. Signing out saves the code being written first.
- Each contest's organiser screens link to the contest as students see it and show the unanswered questions on the Console tab; the tabs wrap on narrow phones instead of scrolling the page.
- Every page's footer names the organisers' contact email. A page that does not exist says which address failed and offers the links that fit you. Organiser tab titles start with the page ("Console · Week 6 contest").
- People: rows stack on phones; after adding an organiser the page says what address to send them, with Copy; someone who has not signed in reads "Invited"; a role changes from its row after a confirmation; account repairs share one Account help form; a fresh install starts with a three-step checklist; and History lists every change to people, domains and the site.
- Your contests: an Open contest button on live rows, digits in mono and words in the text face ("240 of 300"), the length and problem count shown at every width, and a frozen contest reads Live with "standings frozen" in its countdown.
- Before a contest starts, the rules teach the two buttons ("Run on samples ... never counts", "Submit grades your code on hidden tests", "A later, worse submission never lowers your score"), "How the workspace works" shows an example result with the real verdict panel, a late arrival reads that their time is already running, and a screen reader hears when Start opens.
- Workspace: "Submitted, 3rd in queue"; pacing refusals ("Try again in 12 s") are a muted line beside Submit, never a red alert; "Your score on this problem stays 60 of 100" after a lower score; "3 of 15 submissions used"; partial scores fill the rail in the partial colour; the open problem has a border; the last five minutes are a sentence under the clock; Run and Submit have their icons; the key hint is left out on touch screens; "Submit again" and "Run again" after a failed send; Submit reads "Submitting" while it sends.
- The rail says not tried, tried or solved (not submitted or submitted in an exam), counts "2 of 5 tried" during the contest, and has a Rules link that opens the contest's rules and what is recorded. A failing sample offers "Run on this input", which puts its input in the custom input box and runs it.
- An answer an organiser gives everyone now reaches every student as a banner ("An organiser answered a question for everyone").
- Standings: the board is as wide as its content, the legend explains the shading, "+2" and the dash, long names wrap on phones, your place is the one large figure ("7 of 24"), and a screen reader hears when your place changes.
- Screen readers hear run results, compile errors, announcements and toasts through one always-present announcer.
- History: a statement updated during a contest says so with "See what changed" and a line diff; the problem editor lists its revisions and compares them, and asks why while a contest is running with it; the console updates a contest to a new revision with a prefilled announcement, says which revision a rejudge uses, and its audit log is now Changes with filters.
- The Nordic slate theme's action colour is a calmer blue.
- `/privacy` exists for the drafted notice; nothing links it until the owner approves the text (D85).

### v2 P1 (planned as `v1.1.0`)

#### Backend (2026-10-20)

- AI wizard with three providers (D111): Anthropic, OpenAI and Google Gemini. The key form lists each provider's current models from its own models endpoint, filtered to models that can write a problem, and takes a typed model id too; no model id is written in the server. One key per provider per session, still memory-only. New routes `GET /api/admin/ai/providers` and `GET /api/admin/ai/models`; each provider's refused key, used-up quota and unknown model say what to do in one sentence.
- The Anthropic client sends adaptive thinking explicitly and puts a cache breakpoint on its fixed system prompt; billing errors read as "no credit left".
- `podium-server -version` and `podium-judge -version`: version, commit, build date, schema and judge protocol. `/healthz` adds the version and schema for callers on the VM only.
- Release `MANIFEST` and `SHA256SUMS` in the tarball; `install.sh upgrade` checks the sums and refuses an installed release older than the new one's `upgrades_from`, and a downgrade. A tag push builds a draft GitHub release.
- Judge protocol version 2: a judge of another release is answered `426` and stops with "judge protocol 2 needed, this judge speaks 1: install the same release on both" instead of retrying.
- Toolchain registry (migration 0009): the judge reports its compilers, runtimes, isolate and kernel at start; every job records which toolchain ran it, and the contest console response flags pinned problems validated on another toolchain (`toolchain_drift`).
- `install.sh hold` and `unhold` for the compilers, runtimes and isolate. `problem.yaml` gains `format: 1`; a newer format is refused with the version named.
- `podium-judge selftest` runs the hostile-program suite on the host and prints PASS or FAIL per program; CI runs the suite on GitHub's ubuntu-24.04 runner.
- Litestream and the B2 upload run as a new `podium-backup` user, so a compromised server cannot read the bucket keys (D116). The judge cleans every lane's sandbox when it stops. The unused `Settings.ExtraBase` hook is gone.

#### Frontend (2026-10-20)

Live-safe: the token pass is one commit, so reverting it restores the v1 look.

- A calmer, more even look (Direction A with one display figure kept for standings, D80): small text is 14 px, every page title, section and subsection has one size on every screen, links have a thinner underline that fills on hover, buttons show hover and pressed states, table headers recede, rows you can open show a quiet hover, and mono digits sit one optical step smaller beside the text.
- Every screen has the same header: organiser screens show where you are as a breadcrumb trail ("Contest admin, Week 6 contest, Console"), student screens keep one back link.
- Loading, error, no access, not found and empty states read the same everywhere; a screen you cannot open names the organisers' contact email.
- Dialogs share one shell: a dimmer backdrop in dark themes too, the title and close button stay in view while the dialog scrolls, and on phones the dialog opens at the top so the keyboard never covers its fields.
- On phones, the contest lists, problem bank, console feed, integrity tables and the Results statistics stack as rows, and a whole row opens what it names. The Results statistics no longer scroll sideways at 320 px.
- Windows high-contrast mode keeps the selected tab, the current contest tab, the next reveal row, the subtask strip and the rail bars visible.
- AI assistant: choose Anthropic, OpenAI or Google Gemini and keep one key for each; the model picker shows the provider's own current list, and any model id can be typed. The wizard says which provider and model made a draft.
- Contrast fixes: comments stay readable on a selection in the code editor in every theme, control borders reach 3:1 in Overcast, OLED black and Nordic slate, and the error toast's Dismiss keeps its contrast on hover.
- For the server: the build writes `routes.json` (each route's title, description and whether it is public), and `index.html` has the `podium:head` and `podium:body` blocks the server will fill in P3.

### v2 P0 (planned as `v1.0.0`)

#### Backend (2026-10-20)

- `install.sh upgrade [--force "<reason>"]`: asks the deploy guard, checks that every variable the new release's examples name exists in `/etc/podium/*.env` (names only), drains the judge, takes a backup, keeps the running release in `/usr/local/lib/podium/previous/`, installs, starts the server and waits for `/healthz`, starts the judge, verifies, and prints the backup folder and the rollback command.
- `install.sh rollback`: puts the previous binaries back; when the upgrade applied a migration, restores the pre-upgrade backup after a typed confirmation that names what is lost.
- `install.sh verify`: units, `/healthz`, judge API 401, ufw, loopback listeners, production mode, env files root 600 with no placeholder (names only), alert webhook, backup folder mode, time sync. Exits 1 on any failure.
- `podium-server deploy-check`: the deploy guard (D112). Reads the published contests from the database, so it works with the server down; refuses in the 48 hours before a contest, while it runs, and while its queue drains. `-force` records the reason in each refusing contest's audit log (`deploy_forced`).
- `tools/deploy.sh HOST` and `make deploy HOST=...`: guard, `make release`, confirmation before anything leaves the machine, `scp`, `install.sh upgrade`.
- The judge stops gracefully: the first SIGTERM stops claiming and lets running jobs finish and report; the unit waits up to 20 minutes and signals only the worker.
- Encrypted copy off the VM: each nightly backup is packed, encrypted with `age` to the owner's public key and uploaded to B2 with `rclone`; a heartbeat URL is pinged after a good run. `podium-backup` prints every variable it reads as set, default or missing.
- Alerts with nothing resident: `podium-alert@.service` on the failure of the server, judge, Litestream, backup and checker units; `podium-check.timer` every 15 minutes for free space, WAL size, Litestream running and uploading, the encrypted copy's age, and a weekly restore of the bucket's newest database checked with `podium-server check -hash`. Posts to an ntfy topic.
- Fresh installs reach the end: `caddy` runs last and skips while `PODIUM_BASE_URL` is a placeholder or DNS does not point at the VM; isolate's tuning (transparent huge pages off, core pattern, hard-link protection) survives reboots.
- `make release` on a fresh clone installs `web/node_modules`; the placeholder check reads the app's source instead of vendor chunks.
- New variables (all optional, in `/etc/podium/backup.env` and `/etc/podium/alert.env`): `PODIUM_BACKUP_AGE_RECIPIENT`, `PODIUM_BACKUP_B2_PATH`, `RCLONE_CONFIG_B2_TYPE`, `RCLONE_CONFIG_B2_ACCOUNT`, `RCLONE_CONFIG_B2_KEY`, `PODIUM_HEARTBEAT_URL`, `PODIUM_ALERT_WEBHOOK`.
- amd64 only, stated in the README and OPERATIONS, and enforced by `install.sh`.

#### Frontend (2026-10-20)

- The console's "Waiting or judging" filter works, and a new "Judge error" filter lists submissions the judge could not finish: the feed's `verdict` parameter takes `pending` (queued or being judged) and `error` as states (B2).
- On phones the standings header stays on screen while the rows scroll (B3).
- The reveal enters full screen when it opens and leaves it on exit (B4); the row climbing to its new place is drawn above the rows it passes (B5).
- The AI wizard shows its second step as done when the wizard is done (B6).
- The console's audit log names a forced deploy (`deploy_forced`, written by `podium-server deploy-check -force`) with its release and reason.
- Unpublishing (or publishing) a live contest asks first, like any other change students see at once (B7).
- Test suite: an axe-core accessibility pass over every route at 1440 and 320 px with forced-colours screenshots (`web/tests/a11y.spec.ts`, new dev dependency `@axe-core/playwright` 4.13.0), the student workspace and contest screens at more sizes and with touch emulation, pixel comparisons of the student screens (`web/tests/baseline.spec.ts`, references kept outside git), more contrast pairs in the theme test, and the specs' mocks moved to `web/tests/mocks/`.
- Docs: the console feed's `verdict` values in `docs/API.md`; unmeasured judge capacity figures removed from `docs/RUNBOOK.md` until the VM is measured.

### Security council fixes (2026-10-18)

- Co-organisers whose site role is student can no longer change a contest's schedule or rules once it starts, replace the roster, give extra time, extend, pause, resume or rejudge. They see results only when students do (no exam-mode verdicts, the frozen board during the freeze, no stats or exports until then), including on their own submissions. Extensions go only to students on the roster or taking part.
- Testing mode refuses requests from anywhere but this machine's browser (any proxy header or a public `Host`), test sign-in needs the page's own `Origin`, and starting in another mode ends every session and demotes organisers no Google account vouches for.
- Sessions end when the browser closes and at most 12 hours after sign-in; Google sign-in accepts only a sign-in from the last 5 minutes, so a lab PC's previous Google session is not one click away. Owners can end someone's sessions and disable or re-enable accounts from the People page.
- New append-only contest audit log (migration 0008) for settings, problems, roster, extensions, extend, pause, resume, rejudges, the reveal, the editorial and co-organisers; shown in the console, included in the JSON export, `GET /api/admin/contests/{id}/audit`.
- Validation reports tests the brute force did not finish and fails with fewer than half cross-checked. The `ai draft` tag stays until a person changes the statement or a solution (which withdraws the validation). Graded contests take an AI problem only with a person-written solution or an admin's reason, recorded in the audit log. New setter checklist in `docs/AUTHORING.md`.
- A student whose program takes the whole compile time limit can no longer hold the judge's compile lock by pressing Run in a loop: the judge caches that compile error for 10 minutes, and the server refuses that student's Run and Submit for 60 seconds. Judge compile reports carry `timed_out`, accepted only on a CE.

### Milestone M7: hardening (2026-10-18)

- Load test in `tools/loadtest/`: a k6 script that plays a whole contest per student (testing-mode sign-in, an event stream held for the session, workspaces, drafts every 10 to 20 s, standings, integrity batches, runs and submissions with an end-of-contest spike, retries after 429), fixed and window start modes, per-route latency, 429 counts, event stream reconnects and the judge queue every 5 s; a seed script (imports and validates the example packs, creates the students) and a summary script (queue CSV, peak, throughput, drain time). Results: the M7 desktop numbers are in the next item; that note was not kept, and the VM rerun goes in `cache/v2-plan/drills/` (not shipped).
- SQLite writes now take turns in Go before they reach SQLite (`server/internal/db/writegate.go`), and the pool has 8 connections instead of 4. At 700 students the busy handler's sleep-and-poll hand-off between writers had filled every connection: API p95 586 ms, judging stalled for 90 s and every event stream was dropped once. With the gate, the same load gives p95 10.3 ms and no dropped streams. A write that waits 10 s fails with "database is busy", as before.
- `GET /api/admin/metrics` (owners and admins): queue depth and leased jobs by kind, the oldest waiting job, jobs closed in the last minute and five minutes, submission wait and verdict percentiles over ten minutes, failures, event streams, SQLite pool use and waits, server memory. Documented in `docs/OPERATIONS.md`, "Metrics", and `docs/API.md`.
- Backups: Litestream 0.5.16 (pinned, checksum-verified by `install.sh litestream`) streams the database to an S3-compatible bucket, Backblaze B2 by default, uploading every 10 s and keeping 14 days; credentials and bucket in `/etc/podium/litestream.env` (template `deploy/litestream.env.example`), checked at start-up by the unit. A nightly `podium-backup.timer` copies the database with SQLite's online backup and hard-links the test files into `/var/backups/podium/<date>`, checks the copy, and keeps 14 days.
- `podium-server check [-db F] [-tests D] [-hash]`: a read-only check of a database and its test files (integrity and foreign key checks, migrations against the binary, row counts, every recorded file present with its size or content). Used by the nightly backup and by `podium-restore` (`tools/restore.sh` in a checkout), which restores a nightly backup or the bucket (newest, or any moment in 14 days) into a scratch directory and checks it. Restore rehearsed against a local replica in M7 (that note was not kept); the drill onto a blank VM goes in `cache/v2-plan/drills/` (not shipped).
- The judge's blob cache is bounded: `PODIUM_JUDGE_BLOB_CACHE_MB` (new, optional, default 2048) caps `blobs/`; after each job the least recently used files no running job holds are removed down to 90 % of the cap. The compile cache keeps its 500 MB cap.
- The server removes test files and sources nothing refers to any more in its 12-hourly retention pass: not a test, not a submission's or run's source, not named by a job that is waiting, running or under a week old, and not saved in the last day. Saving a blob now refreshes its row and checks the file inside the same transaction, so a removal and a save cannot leave a row without its file.
- `docs/RUNBOOK.md`: contest-day checklists, what to watch in the metrics and when to act, pausing, extending, rejudging after a bad test, a dead judge, a server restart, locked-out students, restoring from a backup, and the dry run with real students with a template for the list of fixes.
- Security council items S3, S7 and S8 (the parts outside the identity and contest code): backups are created with umask 077 in a 700 folder, and `/etc/podium/backup.env` (optional, template `deploy/backup.env.example`) can copy each night's backup off the VM with rsync over ssh (`PODIUM_BACKUP_RSYNC`, `PODIUM_BACKUP_KEEP_DAYS`). The server unit sets `GOMEMLIMIT=220MiB` under its 300 MB cap. A request must arrive whole within 15 s (was 120 s); pack import, test replacement and roster upload extend their own deadline to 120 s. Each browser session may hold 8 open API requests at once (event streams aside), then `429`. Caddy refuses bodies over 33 MB and its admin API is off (`install.sh caddy` restarts Caddy instead of reloading). New `install.sh firewall` step: ufw with only ssh, 80 and 443 inbound, and a warning while ssh accepts passwords. Production refuses a `PODIUM_LISTEN` that is not loopback. `docs/OPERATIONS.md` documents how `X-Forwarded-For` is trusted and how to restrict a remote judge's tunnel key to port 8081.
- `tools/dev-stack.sh` takes `PODIUM_LISTEN`, `PODIUM_JUDGE_LISTEN`, `PODIUM_LOG_LEVEL` and `PODIUM_DEV_CPUS` (pins both services to some CPUs, to approximate the VM). `install.sh packages` adds `jq`.

### Milestone M6: design pass (2026-10-18)

- The web app was audited against the web interface guidelines, the accessibility rules and design.md, and fixed where it fell short.
- Live regions announce from every layout; focus moves somewhere sensible when the focused button disables itself or disappears (the reveal's Next, Back and Start over among them); errors that appear without a field take focus; the editor's focus ring is drawn in the keyword colour so it meets 3:1 on every editor well.
- Copy made consistent: "Standings" and "Problem bank" everywhere, plain words on student screens.
- Keyboard shortcuts: `?` opens a list of every shortcut from anywhere except while typing; `[` and `]` open the previous and next problem in a contest, also not while typing (both are common in code). A "Keyboard shortcuts" link in the footer.
- Keyboard-only run-throughs in Playwright: a student starts a contest, solves, submits, reads the verdict, asks a question and checks the standings; an organiser edits a contest, runs the console and walks the reveal (`web/tests/keyboard.ts`).

### Milestone M5: AI wizard (2026-10-18)

- AI wizard (server): "New problem with AI" writes a whole stdin/stdout problem from a brief (topic, difficulty, constraints, subtask ideas) or from pasted text of an existing problem (at most 20,000 characters, rewritten in the model's own words): statement, samples, subtasks, C++ reference, Python brute force, generator and validator, checker and limits. The answer becomes a draft with origin `ai` and the tag "ai draft", and input generation is queued at once, which chains output generation and validation.
- Bring your own key (decision D50): an admin pastes an Anthropic key per session; it is checked with a model lookup that costs no tokens and held in server memory only, for 2 hours, forgotten on sign-out and restart. It never reaches the database, logs, errors, job payloads or the judge; an end-to-end test searches the log output, the SQLite files and the blob store for it after a full run. No new environment variable.
- Anthropic client on the official Go SDK (`anthropic-sdk-go` v1.78.0): streamed requests, structured outputs with a JSON schema for the whole problem, effort `high`, Claude Opus 5.5 by default (Sonnet 5.5 and Haiku 5.5 on offer), server-side refusal fallbacks for Opus and Sonnet (D54). Answers are checked against the wizard's rules and the editor's field rules and retried once with the reasons; refusals, cut-off answers, rate limits and outages are reported in words. The pasted text travels as delimited data and is never treated as instructions.
- "Can a model solve this?": sends the statement alone and judges the model's C++ answer as the admin's own submission.
- Tests from a generator: problems gain a generator and a plan of tests (subtask, seed, arguments); packs gain `generator.py` and `tests.yaml`, and subtasks `generator_args`. New job kind `generate_inputs` (priority 85): the judge runs the generator in isolate once per planned test and uploads each input under the M3 upload rules; on success, outputs are generated and then the problem is validated. Failures name the test, subtask and seed. The generator is not part of the content hash; a plan hash in the job stops stale inputs (D53).
- No AI draft joins a contest until a person has saved it, even when the chain validated it.
- New routes: `GET`, `PUT` and `DELETE /api/admin/ai/key`, `POST /api/admin/ai/wizard`, `GET /api/admin/ai/wizard/{id}`, `POST /api/admin/problems/{id}/solve`, `POST /api/admin/problems/{id}/generate-inputs`, `GET /api/admin/problems/{id}/input-generation`; the problem JSON gains `generator`, `plan` and `subtasks[].generator_args`; new events `wizard` and `input_generation`. One AI request per admin at a time, 3 minutes each.
- Testing mode offers a fake provider with two canned problems, so the whole pipeline runs without a key or a network.
- Migration `0007_wizard.sql`: generator columns, `input_generations`, and `generate_inputs` added to the `jobs.kind` CHECK in place with SQLite's `writable_schema` procedure, since a table rebuild would cascade into `job_results` (D52). Take a backup before upgrading.

### Integrity checks (2026-10-18)

- Integrity checks after a contest (decisions D47 and D49), run by owners, admins and co-organisers who are not students, once the contest is over for everyone. Four signals: similar code (per-language lexers, winnowing, containment with the reference, brute force and class-wide idioms subtracted, programs under 40 tokens skipped, pairs at 0.6 or more and clusters), the same wrong output on hidden tests (across languages), hard-coded expected outputs, and probing. Strength in words with the raw number beside it; no AI-likelihood score and nothing automatic. Migration `0006_integrity.sql`.
- The check runs in one background goroutine on the server, not on the judge: nothing is executed, and the judge never holds every source. One check at a time per server; a check left running by a restart is marked failed at start-up.
- Organiser routes under `/api/admin/contests/{id}/integrity` (run, latest run with `users` and `problems` maps, side-by-side pair with matched spans, a student's timeline, cases) and `/api/admin/integrity/cases/{case}` (read, change state with a note). Refusals: `409 contest_live` before every window has closed, `409 busy` while a check runs. Ids from another contest are `404`.
- Review cases: opened from signals with a reason of at least 20 characters, evidence copied at opening so a re-run cannot change it, shared with the students before any action, cleared or actioned with a reason. Every change is an append-only decision, and opens and decisions are logged by id.
- Students: `GET /api/integrity/cases` lists cases shared with them, with only their own evidence and code (never another student's name or program, nor the reviewers' notes), and `PUT /api/integrity/cases/{case}/explanation` until the decision.
- Retention: the 180-day job skips students in an open or awaiting_student case for that contest; a closed case's evidence and explanations are emptied 2 years after its decision, and its decision log is kept.
- Review fixes: Hard-coded answers no longer flag fixed words and constants the problem names (IMPOSSIBLE, 1000000000) and run in one pass per distinct program. Probing ignores compile errors and honest step-by-step fixes. Similar-code strength now needs enough shared code, the common-code limit is counted per language, and the pages show how much code is behind a score. A case's compare view shows its own evidence, and signal ids are never reused. Decisions follow the students' turn: action waits until each student explained after the latest share or had 7 days, a reopen needs a new share, and a decision is refused if an explanation changed since the page loaded; every explanation version is kept. The decision log names reviewers and the database refuses to edit it. Checks and signals are deleted 180 days after the contest. Student co-organisers no longer receive classmates' typed, pasted and focus counts in the console feed. `#define` bodies and strings passed to Python `exec` or `eval` are compared as code. Migration 0006 was edited in place (unreleased): recreate development databases.

### Milestone M4: contests (2026-10-18)

- Contests (server): fixed start or a flexible window per student, freeze, best-per-subtask or best-submission scoring, domain or roster access, subtask or exam feedback, graded flag, practice after the end, per-contest gap between submits, submissions per problem and languages, honour code with recorded acceptance, editorial. Migration `0005_contests.sql`.
- Problems are pinned to the validated content hash when added; a problem edited since is refused for publishing, running, submitting and rejudging until it is validated and added again.
- Student routes under `/api/contests/`: the list (live, upcoming, past), the contest page, Start (accepts the honour code; starts the clock in window mode), the contest workspace (the M3 shapes plus label, window and why Run or Submit is off), standings and clarifications. Every answer carries `server_now`; every time rule uses the server's clock.
- Standings computed from submissions and cached in memory per contest and variant, recomputed after any finished submission, rejudge, pin change or extension. Frozen board with "+N" pending cells; students see it from the freeze until every window has closed (window mode and exam feedback: no board until their own window closes), with an optional hold until an organiser reveals. Names on the board come from the roster or the account, never an email.
- Organiser routes under `/api/admin/contests/`: create, edit, archive, clone, problems, roster (CSV or JSON), co-organisers, personal extensions, extend for everyone, pause and resume judging, live feed, rejudge (a problem or one submission, priority 50), announcements, clarifications (answer, make public), standings, reveal (frozen and full boards), CSV and JSON export (spreadsheet formulas neutralised), per-problem stats, editorial. Co-organisers manage only their contests; everyone else gets 404.
- Live contest events on `GET /api/events`: `standings` (version only, sent once the new board is ready, at most about every 2 s), `announcement`, `clarification` (to the asker unless public) and `contest` (new end time), sent to the contest's connected members.
- Judge queue: jobs of a paused contest wait at claim; while a graded contest is live, at most one validation or generation job is leased at a time.
- Integrity (D47): a second event stream from another browser during a live contest is recorded; a retention job deletes integrity events and clears per-submission integrity counts 180 days after a contest ended.
- Security review fixes (`m3-security-2`): students get the coarse, contest-aware view on every path (submission reads, the workspace list, a repeated client key, live events), times rounded to 100 ms in graded contests, exam mode hiding results everywhere, and per-test progress hidden inside unfinished subtasks; client keys are scoped per contest; run rows no longer store code or input inline and the run checks read only a covering index; drafts (one per 2 s) and integrity batches (one per 5 s) are rate limited and events are validated before the write lock; a replaced event stream gets a final `replaced` event; at least every other claim goes to a submission when runs are queued; a program that makes its own output unreadable gets empty output instead of failing the job; upload slots are reserved atomically; event streams close when their session ends.

- M4 security review fixes (`m4-security`): standings recompute once per contest and variant at a time, at most every 2 s, from an index-only read of a new per-submission `earned` column (300 students: 34 ms, was 0.87 s), with the last board served meanwhile and the event sent once the new board is ready; only owners and admins choose a contest's problems or publish it, and co-organisers get the student view and the contest's limits; exam mode never refuses or notifies in a way that depends on judging; rejudges replace submissions still waiting on old tests, never double up, and a replaced job cannot overwrite a result; the fixed-start freeze ignores personal extensions; judge failures show in exam mode and do not count toward the per-problem limit; editorial and practice wait for every window; boards score against the subtask points pinned with each problem; the clarification cap is atomic; heavy reads are rate limited per user and the contest list loads in two queries; judge uploads stream to disk and unused uploads are deleted.
- Contest page problems carry `my_submission_count`; replacing a roster answers with how many were added and removed.

### Milestone M3: submissions (2026-10-18)

- Student workspace: statement with copyable samples, CodeMirror 6 editor in C++, Java, Python and JavaScript, Run on samples or on a custom input, Submit, and a verdict panel with a subtask strip that fills as tests are judged, per-test rows, queue position and compile errors verbatim. Three columns from 1024 px, tabs below. Ctrl+Enter submits, Ctrl+' runs.
- Until contests arrive in M4, organisers reach it with "Try as a student" in the problem editor and "Try" in the bank (`/admin/problems/{id}/try`); the bank is never shown to students.
- Drafts saved to the server every 10 s and on blur, mirrored in the browser; a refresh restores the newer copy.
- New job kinds `run`, `submit` and `generate` on the M2 judge protocol, with priorities 10, 20 to 30 (a fair queue that puts a first attempt ahead of a tenth) and 85.
- Scoring on the server from per-test results: samples first, all-or-nothing subtasks, judging stops at a failing sample and at the first failure in a subtask (the rest `SK`), unreported tests `IE`, an empty subtask earns nothing. A job the judge cannot finish leaves the submission in `error`, not counted.
- Live updates over server-sent events (`GET /api/events`): `submission`, `run` and a `clock` event every 25 s; 4 streams per user, 5,000 in total, slow clients dropped; the page falls back to polling every 15 s. No Caddy change needed.
- Hidden tests stay hidden (decision D47): submit reports carrying output text, or checker messages on hidden tests, are refused; live events for students are coarse (no per-test memory on hidden tests, memory in 16 MB steps, rounded times) and sent per subtask.
- Rate limits: one pending submission per user per problem, 5 s between an organiser's submits across problems, one pending run per user and 2 s between runs, 64 KB of code and of input. Refusals say when to try again. A `client_key` per press of Submit makes retries safe: a repeat returns the first submission with 200.
- Run and Submit refuse a problem whose structure is incomplete or that lacks expected outputs (`409 not_ready`, listing what to fix). Validate and Generate outputs now refuse with the same `409 not_ready`; M2 answered `400`.
- Integrity counts (D47): typed and pasted characters and focus losses per submission, paste, blur and focus events in batches, and an HMAC of IP and user agent keyed by the session secret. Counts only, never keystroke timing; nothing is scored or flagged.
- Output generation: packs and test zips may leave out `.out` files; "Generate outputs" fills missing ones from the reference solution on the judge and "Regenerate all outputs" replaces them after a confirmation. Outputs come back through `PUT /api/judge/blob/{sha}`, accepted only for a generate job under its lease, within a budget of one per input plus 2, hash-checked, at most 16 MB; a report may name only outputs that job uploaded, and nothing is applied if the problem changed meanwhile.
- Compile cache on the judge (`compiled/` in the cache directory): keyed by language, compile command and source, 500 MB, least recently used evicted after each job, entries in use never evicted, compile timeouts and compiler out-of-memory never cached.
- Migration `0004_submissions.sql` (submissions, drafts, generations, integrity events). `0003_problems.sql` was changed before the first release (nullable `output_sha`, new job kinds, output columns in `job_results`): delete any development database created earlier (`data/podium.db`, or `/var/tmp/podium-dev` for `make dev-test`).
- New routes under `/api/workspace/`, `/api/runs/`, `/api/submissions/`, `/api/events`, `/api/admin/problems/{id}/generate` and `/generation`, and `PUT /api/judge/blob/{sha}`.
- `make sandbox-test` also runs the M3 acceptance test: AC, WA, TLE, MLE and CE submissions in all four languages, a run on samples, and outputs generated for a pack shipped without them, then validated.

### Milestone M2: problems (2026-10-18)

- Problem bank for admins and owners: list, create, edit, archive; statement editor with a live preview (Markdown, KaTeX maths, sanitised with DOMPurify).
- Problem packs: import and export of `problem.yaml`, `statement.md`, sources, an optional testlib `checker.cpp` and `tests/NN.in`/`NN.out`; strict YAML; a refused pack lists everything wrong with it at once. Packs must contain their outputs (output generation arrives in M3).
- Pack import treated as untrusted parsing: 32 MB zipped, 128 MB counted on decompressed bytes, 2,000 entries read from the zip's end record before decoding, 16 MB per file, unsafe paths and links refused, one pack operation at a time.
- Validation on the real judge: compiles and runs the reference, an optional brute force, an optional input validator and the checker on every test in isolate; fails when the reference is not accepted everywhere, the brute force disagrees, the validator rejects an input, or the reference uses more than half its time limit. The report names each failing test with program and verdict.
- "Validated" is bound to a content hash of everything validation depends on; statement and title edits keep it, any other change clears it, and a failing re-run of the same content withdraws it.
- Custom checkers with testlib: `install.sh` step `testlib` installs `testlib.h` at a pinned commit, checked by SHA-256; checkers compile with a 30 s budget and run in a fresh box.
- Judge protocol (brought forward from M3): one `jobs` queue in SQLite, `job_results`, 60 s leases renewed by reports, three attempts, per-job deadlines, blobs served only to the lease holder and re-hashed by the judge, summary computed by the server. Served on a separate loopback-only listener, `PODIUM_JUDGE_LISTEN`, with a bearer token.
- `podium-judge work` runs the worker loop; the systemd unit now starts it.
- Migration `0003_problems.sql`; new routes under `/api/admin/problems` and `/api/judge/`.
- `make sandbox-test` also runs the M2 acceptance test: the three example packs in `problems/` validate on real isolate, and a broken output fails naming the test.
- New variables: server `PODIUM_JUDGE_LISTEN`, `PODIUM_JUDGE_TOKEN`; judge `PODIUM_SERVER_URL`, `PODIUM_JUDGE_TOKEN`.

### Run modes (2026-10-18)

- `PODIUM_MODE` for both services: `testing`, `dev` or `production` (the default).
- Testing mode needs no keys: no Google client, a sign-in form for any email and role (`POST /api/auth/test-signin`), a random session secret, the public testing judge token, loopback only, and a banner on every page.
- Dev mode: Google sign-in over http, judge testing token allowed. Production: every value required, https base URL, testing token refused.
- `GET /api/auth/config` now returns `mode`.
- `make dev-test` runs server, judge and web app in testing mode (`tools/dev-stack.sh`, state in `/var/tmp/podium-dev`); `npm run test:live` runs browser tests against it with nothing mocked.

### Themes (2026-10-18)

- Ten colour themes plus System, in Light, Dark and Accessible groups, chosen from a menu in the header with a swatch for each.
- Colour-blind safe verdict colours (teal, purple, orange) as a separate setting that works with every theme.
- Preferences belong to the account (`PUT /api/me/preferences`, migration `0002_preferences.sql`) and are cleared from the browser on sign-out.
- Every theme is tested for text contrast, control borders at 3:1, verdict contrast, and the distance between the action colour and the verdicts; a browser test checks the CSS cascade gives the tested values.
- Fixed in the existing themes: light "accepted" and "partial" colours and control borders in both themes were below the contrast floor.

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
