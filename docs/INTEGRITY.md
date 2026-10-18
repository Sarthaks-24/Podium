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

Integrity records are deleted 180 days after the contest ends, except for students in an open case.

## The checks

An organiser presses **Check integrity** on a contest's results page after the contest has ended for everyone. It takes under a minute for a few hundred students and replaces the previous check's signals; open cases keep their own copy of the evidence.

| Signal | What it finds | Strength |
|---|---|---|
| Similar code | Two students' programs share long passages after names, literals, comments and layout are ignored. Code in the reference solution, the brute force or shared by more than a tenth of the class does not count, and programs under 40 tokens are skipped (everyone writes A plus B the same way). | strong at 0.9 or more of the shorter program shared, moderate at 0.75, weak at 0.6 |
| Same wrong output | Two students printed the same wrong output on the same hidden tests. Works across languages. Mistakes more than three students share (an overflow everyone makes) and empty output are ignored. | strong on 4 or more tests, moderate on 2 or 3 |
| Hard-coded answers | A program contains a hidden test's expected output. Short outputs and small numbers are ignored. | strong on 2 or more tests or a long output, moderate on one |
| Probing | Nearly identical versions of a program drew many different results on the hidden tests: someone learning the tests by trial. | strong at 10 different results, moderate at 6 |

The page also shows each student's timeline: submissions with typed and pasted counts, large pastes, focus losses, second sessions, and other students who used the same browser and network. These are context, never a signal on their own: lab machines are shared, and pasting your own template is allowed.

Window-mode contests leak by design: a student who finished can tell a later one about the problems. The review page says so for those contests.

## Reviewing

1. Read the signal: for similar code, the side-by-side view highlights what matched. Look at the timeline of both students.
2. If it deserves a closer look, **open a case** from the signals, with a title and a reason (at least 20 characters). The title and the reason are for reviewers only: students see the contest's name and their own evidence, never the case title or the notes.
3. **Share the case** with the students. Each sees their own evidence (never the other student's name or code) and writes an explanation.
4. Decide: **clear** or **take action**, again with a reason. Action is refused until every student in the case was shared it. Every change is kept in the case's log; nothing in it can be edited or deleted.

Signals are weakest on short problems and on problems with one natural solution. A strong similarity on a 50-line problem from two students who sit together means more than a moderate one on a textbook algorithm.

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
4. **Reviewers.** Cases are reviewed by [the owner and admins], never by the setter of the student's own section, and decided by [two reviewers].
5. **Student's side.** The student sees the evidence and has [7 days] to explain before any decision. They may ask to [explain in person].
6. **Outcomes.** For a first finding: [zero for the problem]. Repeated: [referred to the department's academic integrity process]. Appeals go to [the course coordinator].
7. **Retention.** Integrity records are kept [180 days] after the contest; case records [2 years] after the decision.
8. **Organisers during a contest.** Setters [may not] submit to a live graded contest's problems; co-organisers who are students [may not] see problems before the contest.
