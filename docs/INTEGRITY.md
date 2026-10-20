# Integrity

How Podium looks for copying and other cheating, what it does not do, and a policy template for the owner to fill in. The reasoning lives in `docs/ARCHITECTURE.md`; this page is for organisers and reviewers.

## The short version

- Podium finds **reasons to look**, never proof. Every signal is reviewed by a person, the student sees the evidence and explains, and a person decides with a written reason. Nothing is automatic.
- Students never see a signal or a flag. They see evidence only when a reviewer shares a case with them.
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

Integrity records are deleted 180 days after the contest ends, except for students in an open case. The checks' results (runs and signals) go at the same time; a case keeps its own copy of the signals it was opened from.

## Organisers and the record

Integrity is also about the people running the contest. A dispute about a grade has to be settled from facts, so:

- **The audit log.** Every organiser action that can change results is written to the contest's audit log with who did it and when: settings (each field, from and to), problem pins, the roster, extensions, extend, pause and resume, rejudges, the reveal, the editorial, co-organisers, and an admin's reason to grade an AI problem as written. Nothing can edit or delete a row, not even the server (database triggers refuse it). The console shows the log, and the JSON export carries it. Store the export, and its SHA-256, off the server after each graded contest.
- **Student co-organisers (TAs).** A co-organiser whose own role is student can answer questions, post announcements and edit the contest before it starts. They cannot change its rules or schedule once it starts, nor replace the roster, give extra time, extend, pause, resume or rejudge: those are for owners and admins. They see results as a student would (no verdicts in exam mode until every student's time is up, the frozen board during the freeze, no integrity counts, and their own submissions under the contest's limits), so the role is no window onto hidden tests or classmates' results. Extra time can only go to a student on the roster or one who took part.
- **Shared lab PCs.** Sign-in accepts only a Google sign-in from the last 5 minutes, Podium's cookie ends when the browser closes, and every session ends 12 hours after sign-in at the latest. Still: give each lab slot a browser guest profile (or a kiosk profile that is wiped on exit), and ask students to sign out of Podium and of Google before they leave. An owner can end anyone's sessions, or disable an account, on the People page; a disabled student's next request is refused and their open contest page loses its live updates within 25 seconds.

## The checks

An organiser presses **Check integrity** on a contest's results page after the contest has ended for everyone. It takes under a minute for a few hundred students and replaces the previous check's signals; open cases keep their own copy of the evidence.

| Signal | What it finds | Strength |
|---|---|---|
| Similar code | Two students' programs share long passages after names, literals, comments and layout are ignored. Code in the reference solution or the brute force does not count, nor code shared by more than a tenth of the students who wrote in that language (at least 5), and programs under 40 tokens are skipped (everyone writes A plus B the same way). | strong at 0.9 or more of the shorter program shared **and** at least 40 shared fingerprints; moderate at 0.75 and 25; anything else listed is weak (listed from 0.6) |
| Same wrong output | Two students printed the same wrong output on the same hidden tests. Works across languages. Mistakes more than max(3, 2% of the students) share (an overflow everyone makes) and empty output are ignored. | strong on 4 or more tests, moderate on 2 or 3 |
| Hard-coded answers | A program contains a hidden test's expected output. Short outputs and small numbers are ignored, and so is any output the reference, the brute force or the statement contains (a fixed word such as IMPOSSIBLE, a constant such as 1000000000), or that the programs of more than max(3, 10% of the students) contain. | strong on 2 or more tests or a long output, moderate on one |
| Probing | Nearly identical versions of a program drew results that went back and forth on the hidden tests: someone learning the tests by trial. Compile errors and failed samples are ignored, versions are compared with the first of the run, and only flips count (a test going from accepted back to a failure, or from one failure to another); fixes that move forward never count. | strong at 10 different results, moderate at 6 |

The similarity score is the share of the shorter program found in the other, not a probability. The review page shows beside it how much code is behind it: the shared fingerprints and the shorter program's length in tokens. A high score from a short program or a textbook algorithm is weak evidence, which is why strength also needs an amount of shared code.

The page also shows each student's timeline: submissions with typed and pasted counts, large pastes, focus losses, second sessions, and other students who used the same browser and network. These are context, never a signal on their own: lab machines are shared, and pasting your own template is allowed.

Window-mode contests leak by design: a student who finished can tell a later one about the problems. The review page says so for those contests.

## Reviewing

1. Read the signal: for similar code, the side-by-side view highlights what matched. Look at the timeline of both students.
2. If it deserves a closer look, **open a case** from the signals, with a title and a reason (at least 20 characters). The title and the reason are for reviewers only: students see the contest's name and their own evidence, never the case title or the notes.
3. **Share the case** with the students. Each sees their own evidence (never the other student's name or code) and writes an explanation.
4. Decide: **clear** the case, or **propose an outcome** for a second reviewer to confirm (see "Outcomes and the two-person rule"). Every change is kept in the case's log with the reviewer's name; the database refuses to edit or delete it.

The rules a case follows:

| From | To | Allowed when |
|---|---|---|
| Open | Shared with the students | Always. Each student gets a turn: 7 days to explain from first opening the case, never more than 21 days (see "Time limits"). |
| Open | Cleared | Always: clearing never hurts a student. |
| Shared | Cleared | Always. |
| Shared | Action taken | Only by a confirmed outcome: a proposal once every student explained after the latest share or ran out of time, and a second reviewer's confirmation. |
| Shared | Open | Always. |
| Cleared | Open (reopen) | Always. The students' turn ends: the case must be shared again, and each student gets a new turn. |
| Action taken | Open (reopen) | Only a case closed before v1.4.0 without an outcome. A confirmed outcome is changed only through an appeal. |

- Every version of an explanation is kept and shown with its time. A student can change it until the case is decided.
- If a student saves a new version after you loaded the case, your decision is refused with "A student changed their explanation. Reload before deciding." Reload, read it, and decide again; your note is kept. The log records which version of each explanation you had read.
- The compare view on a case shows the case's own copy of the evidence, so a later check never puts other students' code under it.
- A reviewer who is one of a case's students (a student later made an admin) cannot see or decide that case.

Signals are weakest on short problems and on problems with one natural solution. A strong similarity on a 50-line problem from two students who sit together means more than a moderate one on a textbook algorithm.

## Manual review (v1.4.0)

A reviewer who reads a submission and wants to look closer does not need a signal.

- **Private note.** On any Submit of a contest (practice after the contest included), a reviewer writes a note of up to 2,000 characters and may highlight line ranges. Notes are for reviewers only: the student is never told about one, because a hunch is not a case. A note is deleted 30 days after it was written unless a case is opened from its submission.
- **Open a case from a submission.** Title and a reason of at least 20 characters, as for a case from signals. The case's student is the submission's author; its evidence is the submission and its notes, copied into the case. The student, once the case is shared, sees their own code with the highlighted lines and "An organiser asked you to explain this submission", never the notes' text.
- Who may: the owner, admins, and co-organisers of that contest whose own role is not student. Student co-organisers (TAs) cannot write notes, open cases or see them.

## Outcomes and the two-person rule (v1.4.0)

"Take action" now means a named outcome, proposed by one reviewer and confirmed by another.

| Outcome | What it does |
|---|---|
| Warning | A line on the student's standing; their rating drops by 0.5 |
| Zero for problem | The student's score on that problem becomes 0 on the final board, for every board computed from then on (a rejudge cannot bring it back); a line on their standing; their rating drops by 1.5. The board shows no mark on the cell; the results page says "Board updated on <date> after a review decision"; the organiser exports carry a "zero by decision" column; the student's own contest page says "Zero on this problem by decision. See your case" |
| Referred | Referred to the department's academic integrity process; a line on the standing; the rating drops by 2.5 |

1. **Propose.** Once every student replied after the latest share or ran out of time, and the contest is over for everyone, a reviewer proposes the outcome, the students it is for (a pair case may action one and clear the other), a note for reviewers and a **reason the student will read** on their standing. One proposal waits at a time.
2. **Confirm.** A second reviewer, who neither opened the case nor made the proposal and is not one of its students, confirms it with their own note. The decision then closes the case, applies the outcome and tells the students.
3. **Sole organiser.** When no other owner or admin account exists, the owner may confirm their own proposal with a written reason (at least 20 characters), recorded on the decision as "sole organiser". Add a second admin as soon as you can. The owner could create this situation by disabling or demoting the other admins first; that shows in the site audit (People, History) next to the decision's sole-organiser reason, and is an owner power, not a gap in the rule.
4. Any later decision (clearing, reopening) supersedes a waiting proposal. A case that has had a confirmed outcome is never reopened, even after an upheld appeal cleared it: it changes only through an appeal, and new evidence goes in a new case.
5. **Policy hint.** When a student already has a confirmed outcome above a warning in the last 2 years, the case shows "Policy: referral suggested". A person still decides.

The decision records the student's rank, total and score on that problem just before a zero, and the contest audit log records the board change (`board_adjusted`), so the earlier standings can be stated in a dispute. Student co-organisers do not see these audit rows.

## Appeals (v1.4.0)

- A student may appeal an outcome, or a manual adjustment of their standing, once, in writing, within **14 days** of the decision or the adjustment.
- An owner or admin who took no part (did not open the case, propose, confirm or decide anything on it, and is not one of its students; for an adjustment, neither made nor confirmed it) rules within **14 days**, with a reason the student reads. The sole-organiser exception applies as above.
- **Upheld:** the outcome leaves the student's standing, the rating gets back what the outcome took (never above 10), a semester rollover the outcome blocked is applied, a zero leaves the board (the board and the summaries are rewritten), and the case becomes cleared when no student keeps an outcome. For an adjustment, its change is reversed.
- **Rejected:** the reason is recorded and shown to the student.

## Time limits (v1.4.0)

| Clock | Limit |
|---|---|
| Reply | 7 days from the student first opening the case, never later than 21 days after the notice became available; a reminder on day 5 |
| Decide | 14 days from when every student replied or ran out of time; after that the case shows as overdue to the owner |
| Appeal | 14 days from the decision or adjustment |
| Ruling | 14 days from the appeal; then overdue |

A notice for a case shared while the student is in a live contest becomes available when their window closes, and the case appears on their page only then. Cases shared before v1.4.0 keep the old rule of 7 days from the share. The limits are constants in `server/internal/integrity/cases.go` and `deadlines.go`.

## Standing and the rating (v1.4.0, D110)

- Every student has a **rating** from 0 to 10, in steps of 0.1, starting at 10. Only owners and admins see the number (record page, students table). Only four things move it, each with a dated history row: a confirmed outcome (warning 0.5, zero for problem 1.5, referred 2.5), a manual edit with a written reason, an upheld appeal, and the semester rollover. Every change keeps it within 0 to 10.
- Students never see the number. They see their **Standing band** with the dated lines behind it on their integrity page: 9.0 to 10 "Good standing", 6.0 to 8.9 "Outcomes on record", below 6.0 "Repeated outcomes". A line shows each confirmed outcome no appeal overturned, and each manual adjustment ("Standing adjusted by an organiser" with the reason), for 2 years; an open appeal shows as "Under appeal".
- **Manual edit.** An owner or admin sets a new value with a reason of at least 20 characters, which the student reads. An edit that lowers the student's band waits for a second owner or admin to confirm (the sole-organiser exception applies). Co-organisers cannot edit. Every applied edit can be appealed.
- **Semester rollover.** When a term ends, the owner runs `podium-server semester-rollover -dry-run LABEL`, reads the counts, then runs it without `-dry-run`. Every student with no confirmed outcome since the previous rollover gains 0.5, up to 10. A label runs once.
- The rating is shown, never used: standings, results, progress, exports and notices never read it, and a test checks their code. Nothing changes it automatically.
- Every read of a student's record page, a case, or the notes on a submission is logged; the owner reads the log and, per reviewer, how many manual cases they opened, cleared, actioned and left overdue (D97).

## Notices (v1.4.0)

Students get a notice when a case is shared, on day 5 without a reply, when a case is decided, when an appeal is ruled on, when an organiser adjusts their standing, and when a contest's results are final. A notice says what happened, what to do and by when, and the asking ones end with "Nothing affects your marks until a person decides." It never contains code, evidence, test data or anyone's name, and never uses "flag", "trust" or "suspicious". Nothing is delivered during the student's live contest. Notices are kept 180 days. There is no email.

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
2. **Contest format.** Graded contests use [fixed start in a lab with an invigilator]. Window-mode contests are used only for [practice].
3. **Evidence threshold.** A case is opened when [a strong signal, or two moderate signals of different kinds, after a reviewer has read the code].
4. **Reviewers.** Cases are reviewed by [the owner and admins], never by the setter of the student's own section. Every outcome is proposed by one reviewer and confirmed by a second who did not open the case (Podium enforces this); the owner alone only when no other owner or admin exists, with a written reason.
5. **Student's side.** The student sees the evidence and has [7 days] from first opening the case, and at most [21 days] from the notice, to explain before any outcome is proposed; clearing needs no wait. They may ask to [explain in person]. Podium enforces the days; to change them, the owner changes `ResponseWindow` and `ReplyCap` in `server/internal/integrity/` (and the help text in the web app) and rebuilds.
6. **Outcomes.** Three outcomes: **warning**, **zero for the problem** (changes the board), **referred** to [the department's academic integrity process]. For a first finding: [warning, or zero for the problem when the copying is clear]. Repeated (a second outcome above a warning within 2 years): [referred]. Standing bands: 9.0 to 10 "Good standing", 6.0 to 8.9 "Outcomes on record", below 6.0 "Repeated outcomes".
7. **Appeals.** Within [14 days] of an outcome or an adjustment, ruled on within [14 days] by a reviewer who took no part, [or the course coordinator].
8. **Retention.** Integrity records are kept [180 days] after the contest; case records [2 years] after the decision; private notes [30 days] unless a case is opened; the rating history [4 years].
9. **Organisers during a contest.** Setters [may not] submit to a live graded contest's problems; co-organisers who are students [may not] see problems before the contest.
