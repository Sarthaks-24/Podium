# Integrity

How Podium looks for copying and other cheating, what it does not do, and a policy template for the owner to fill in. The reasoning lives in `docs/ARCHITECTURE.md`; this page is for organisers and reviewers.

## The short version

- Podium finds **reasons to look**, never proof. Every signal is read by a person, and a person records the decision with a written reason. Nothing is automatic.
- Podium hands over evidence; the college's own process deals with the student. An organiser downloads the similar pairs as CSV and records one decision per student: **No action**, **Warning** or **Exclude from this contest**.
- Students never see a signal, a flag or a decision in Podium. An excluded student simply no longer appears in that contest's standings.
- There is **no AI detector**. No tool reliably tells AI-written code from a student's own, so Podium does not pretend to. See "AI-written code" below.

## What is recorded during a contest

Students are told all of this on the honour-code screen before they start.

| Recorded | Not recorded |
|---|---|
| Every submission and its results | Keystrokes or their timing |
| Characters typed and pasted in the editor, per submission | The text of pastes |
| Paste sizes, and when the page lost and regained focus | Webcam, microphone, screen |
| A second browser opening the same contest at the same time | Other tabs or applications |
| A keyed hash of IP address and browser (never the address itself) | The IP address in readable form |

Integrity records are deleted 180 days after the contest ends. The checks' results (runs and signals) go at the same time. Decisions and their reasons are kept.

## Organisers and the record

Integrity is also about the people running the contest. A dispute about a grade has to be settled from facts, so:

- **The audit log.** Every organiser action that can change results is written to the contest's audit log with who did it and when: settings (each field, from and to), problem pins, the roster, extensions, extend, pause and resume, rejudges, the reveal, the editorial, co-organisers, integrity decisions, and an admin's reason to grade an AI problem as written. Nothing can edit or delete a row, not even the server (database triggers refuse it). The console shows the log, and the JSON export carries it. Store the export, and its SHA-256, off the server after each graded contest.
- **Student co-organisers (TAs).** A co-organiser whose own role is student can answer questions, post announcements and edit the contest before it starts. They cannot change its rules or schedule once it starts, nor replace the roster, give extra time, extend, pause, resume or rejudge: those are for owners and admins. They see results as a student would (no verdicts in exam mode until every student's time is up, the frozen board during the freeze, no integrity counts, and their own submissions under the contest's limits), so the role is no window onto hidden tests or classmates' results. Extra time can only go to a student on the roster or one who took part.
- **Shared lab PCs.** Sign-in accepts only a Google sign-in from the last 5 minutes, Podium's cookie ends when the browser closes, and every session ends 12 hours after sign-in at the latest. Still: give each lab slot a browser guest profile (or a kiosk profile that is wiped on exit), and ask students to sign out of Podium and of Google before they leave. An owner can end anyone's sessions, or disable an account, on the People page; a disabled student's next request is refused and their open contest page loses its live updates within 25 seconds.

## The checks

An organiser presses **Check integrity** on a contest's results page after the contest has ended for everyone. It takes under a minute for a few hundred students and replaces the previous check's signals; decisions stay.

| Signal | What it finds | Strength |
|---|---|---|
| Similar code | Two students' programs share long passages after names, literals, comments and layout are ignored. Code in the reference solution or the brute force does not count, nor code shared by more than a tenth of the students who wrote in that language (at least 5), and programs under 40 tokens are skipped (everyone writes A plus B the same way). | strong at 0.9 or more of the shorter program shared **and** at least 40 shared fingerprints; moderate at 0.75 and 25; anything else listed is weak (listed from 0.6) |
| Same wrong output | Two students printed the same wrong output on the same hidden tests. Works across languages. Mistakes more than max(3, 2% of the students) share (an overflow everyone makes) and empty output are ignored. | strong on 4 or more tests, moderate on 2 or 3 |
| Hard-coded answers | A program contains a hidden test's expected output. Short outputs and small numbers are ignored, and so is any output the reference, the brute force or the statement contains (a fixed word such as IMPOSSIBLE, a constant such as 1000000000), or that the programs of more than max(3, 10% of the students) contain. | strong on 2 or more tests or a long output, moderate on one |
| Probing | Nearly identical versions of a program drew results that went back and forth on the hidden tests: someone learning the tests by trial. Compile errors and failed samples are ignored, versions are compared with the first of the run, and only flips count (a test going from accepted back to a failure, or from one failure to another); fixes that move forward never count. | strong at 10 different results, moderate at 6 |

The similarity score is the share of the shorter program found in the other, not a probability. The review page shows beside it how much code is behind it: the shared fingerprints and the shorter program's length in tokens. A high score from a short program or a textbook algorithm is weak evidence, which is why strength also needs an amount of shared code.

The page also shows each student's timeline: submissions with typed and pasted counts, large pastes, focus losses, second sessions, and other students who used the same browser and network. These are context, never a signal on their own: lab machines are shared, and pasting your own template is allowed.

Window-mode contests leak by design: a student who finished can tell a later one about the problems. The review page says so for those contests.

## Reviewing and deciding

1. Read the signal: for similar code, the side-by-side view highlights what matched. Look at the timeline of both students.
2. Hand the evidence to the college's process if it deserves one: **Download similar pairs (CSV)** on the integrity page lists each pair with both students' names, emails, roll numbers and submission ids.
3. Record a decision about each student it concerns, from the pair view, the integrity page or a submission row in the console:

| Decision | What changes |
|---|---|
| No action | Nothing. The reason is kept. |
| Warning | Nothing on the board. The reason is kept. |
| Exclude from this contest | The student no longer appears in the standings, the results page or the results export of that contest. |

- A reason is required (a sentence or two). Each student has one decision per contest; recording a new one replaces it, so "No action" after "Exclude from this contest" brings the student back.
- Every decision is written to the contest's audit log with who made it and when.
- A reviewer who is one of the students named (a student later made an admin) never sees those signals and cannot decide about themselves.

Signals are weakest on short problems and on problems with one natural solution. A strong similarity on a 50-line problem from two students who sit together means more than a moderate one on a textbook algorithm.

## What the checks cannot see

The checks find copies that were renamed and rearranged; they do not find a copy that was rewritten in another language or hidden from the lexer. Measured on one copied 45-line C++ program (integrity review, 2026-10-18):

| Change to the copy | Score |
|---|---|
| Unused functions appended | 1.00 (extra code does not lower the score) |
| A junk statement after every line | 0.82 |
| Declarations moved | 0.98 |
| `++` rewritten as `+= 1` | 0.94 |
| Comparison operands swapped | 0.90 |
| `int` changed to `long long` | 0.78 |
| `for` loops rewritten as `while` | 0.97 |
| Translated to Java by hand | 0.70, weak |
| Translated to Python | about 0; only "same wrong output" can link it |
| The whole program inside a `#define` | was 1 token, never compared; the body of a `#define` is now read as code |
| A Python program passed to `exec('''...''')` | was 4 tokens, never compared; a string passed to `exec` or `eval` is now read as code |

Still out of reach:

- **Another language.** A program translated by hand into another language shares no fingerprints with the original.
- **Code built at run time.** A string assembled, decoded or read from input before `exec`, `eval` or a macro trick hides the code; only literals passed straight to `exec` or `eval` are read.
- **Shared templates.** Code an organiser gave the class (a starter file, a lab template) is not discounted unless it is the reference or the brute force, so students who build on the same template look alike. Podium cannot take a list of templates per contest, so read similar-code signals on templated problems with that in mind. This is a known gap. Code shared by many students (the editor's starter code among it) is dropped anyway once more than a tenth of the students in that language have it.
- **AI-written code.** See below.

## AI-written code

No tool reliably tells AI-written code from a student's own: detectors are wrong often enough to accuse honest students, and easy to fool. Podium handles it in three other ways:

- **Prevention:** graded contests default to a fixed start; run them in a lab with an invigilator for anything that counts. The honour code states the rule. When the AI wizard (M5) is available, "Can a model solve this?" shows whether a model solves a problem zero-shot, so a setter can make graded problems harder to hand off.
- **Indirect evidence:** large pastes, a full score far faster than the class, and clusters of near-identical code (students who asked the same model often submit the same program).
- **Verification:** the reliable check is a conversation. Ask the student to explain their program, or to change it in front of you.

## Policy template

The owner fills this in and keeps it with the course materials. Until then Podium runs with the defaults in brackets.

1. **Allowed resources.** Own templates: [yes]. Language documentation: [yes]. Searching the web: [no]. AI assistants and code generators: [no]. Discussing with others during the contest: [no].
2. **Contest format.** Contests that count use [fixed start in a lab with an invigilator]. Window-mode contests are used only for [practice].
3. **Evidence threshold.** A pair goes to the college's process when [a strong signal, or two moderate signals of different kinds, after a reviewer has read the code].
4. **Who decides.** [The department's academic integrity process] decides about the student. The organiser records the outcome in Podium as No action, Warning or Exclude from this contest, with the reason.
5. **Retention.** Integrity records are kept [180 days] after the contest; decisions and their reasons are kept.
6. **Organisers during a contest.** Setters [may not] submit to a live contest's problems; co-organisers who are students [may not] see problems before the contest.
