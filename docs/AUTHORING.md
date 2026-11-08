# Writing problems

Admins and owners write problems in the problem editor (Problems, in the header) or import them as a problem pack. Validation then runs the problem on the real judge, and only a validated problem can go into a contest. The AI wizard can also write a draft (see "The AI wizard").

## Input and output

Every problem reads standard input and writes standard output. A problem is either a whole program (the student writes all of it) or a function problem in the style of LeetCode (the student writes only a function, and locked code around it reads the input and prints the answer; see "Function problems").

## Limits and languages

The time limit you set applies to C++. Other languages get a multiple of it, and extra memory for their runtime:

| Language | Id | Time | Memory |
|---|---|---|---|
| C++17 | `cpp17` | 1.0 x | limit |
| Java 21 | `java21` | 2.0 x | limit + 256 MB (heap capped at the limit) |
| Python 3 | `python3` | 3.0 x | limit + 64 MB |
| JavaScript (Node 22) | `node22` | 2.0 x | limit + 128 MB |

Java sources must declare `public class Main`. In a function problem that class is in the locked bottom, and the student writes a non-public `class Solution`.

Guidelines that keep a 700-student contest moving on one VM:

- 6 to 10 hidden tests per problem, at most 2 maximum-size tests per subtask.
- Time limit 1 s for C++, and at least 3 times your reference solution's time. Validation enforces at least twice and warns below three times (see "Reference timing").
- Outputs under 1 MB.
- Python is about 5 times slower than C++ on heavy input (calibration: sorting a million integers took 141 ms in C++ and 697 ms in Python). If Python solutions should pass, write and time a Python reference too.

## The problem pack

A pack is a zip file. Zipping the folder itself is fine: a zip whose entries all sit in one top-level folder is read as if it were zipped from inside it.

```
problem.yaml        settings (required)
statement.md        the statement in Markdown
solution.cpp        reference solution (any name; problem.yaml names it)
brute.py            brute force solution (optional)
validator.py        input validator (optional)
generator.py        test generator (optional; see "Tests from a generator")
tests.yaml          the generator's planned tests (optional; this name is fixed)
checker.cpp         testlib checker (required when checker is custom; this name is fixed)
tests/01.in         test inputs and their expected outputs (outputs may be left out)
tests/01.out
tests/02.in
tests/02.out
...
```

Source files must sit at the top of the pack, with a name of up to 40 letters, digits, `-` or `_`, and an extension that sets the language: `.cpp` C++17, `.java` Java 21, `.py` Python 3, `.js` JavaScript. Any other file is ignored, and the import names it in a warning.

### problem.yaml

**Pack format.** Podium reads formats 1 to 4. Format 2 added `open`, the open tests; v1.2 has none (V3-5) and imports them as samples. Format 3 (v1.3) added `starters`. Format 4 (v2.0.2) added function problems: `mode`, `locked_top` and `locked_bottom`. Exports write format 4 for a function problem, format 3 for a whole problem with starter code and format 1 otherwise, so an older Podium still reads every whole problem; a pack without a `format` line is format 1. Podium v2.0.1 and older refuse a format 4 pack ("this server reads format 3 and older").

Parsed strictly: an unknown key is an error, so a misspelt key never silently falls back to a default. Comments are allowed.

```yaml
format: 1                       # pack format; exports write 1
title: Pair with sum            # shown to students; up to 120 characters
slug: pair-with-sum             # lowercase letters, digits and single hyphens; unique among problems
time_limit_ms: 1000             # for C++; 100 to 10000; default 1000
memory_limit_mb: 256            # 16 to 1024; default 256
checker: custom                 # tokens (default), exact, float or custom
# epsilon: 1e-6                 # float only: allowed absolute or relative error, above 0 and at most 0.1
tags: [hashing, two-pointers]   # up to 10, each up to 32 characters; stored lowercase
difficulty: medium              # easy, medium, hard, or leave it out
reference: solution.cpp         # required: the setter's correct solution
brute: brute.js                 # optional: a slow, obviously correct solution
validator: validator.py         # optional: checks every input against the constraints
generator: generator.py         # optional: makes the inputs planned in tests.yaml
starters: [starter.cpp, starter.py]  # optional, format 3: what a student's editor opens with, one file per language
# mode: function                # format 4: the student writes only a function (see "Function problems")
# locked_top: [top.cpp, top.py]        # format 4, with mode: function: the locked code before the student's, one file per language
# locked_bottom: [bottom.cpp, bottom.py]  # format 4, with mode: function: the locked code after it
samples: [1, 2]                 # test numbers shown with the statement; they carry no points
subtasks:                       # at least one; scored all or nothing
  - points: 30                  # 1 to 1000; the subtasks should add up to 100
    title: Small n              # optional
    tests: "3-4"                # range list, see "Samples and subtasks"
    generator_args: [n=100]     # optional: proposed arguments for this subtask's planned tests
  - points: 70
    title: Large n
    tests: "5-6"
```

Quote every `tests` value, so YAML reads it as text.

### Tests

- `tests/NN.in` and `tests/NN.out`, with `NN` from 1 to 999 and any zero padding (`1.in`, `01.in` and `001.in` are all test 1, so do not mix them).
- An expected output may be left out. Such a test shows "Made on Validate" in the editor's Tests tab: Validate first makes every missing output from the reference solution, then validates (see "Expected outputs from the reference"). Judging waits until every test has one.
- An output without its input is refused.
- At most 16 MB per file.
- A test planned in `tests.yaml` needs no files: Generate inputs makes its input, then its output is made from the reference. A pack must have at least one test file or one planned test.

## Function problems

In a function problem the student writes only a function. The file has three parts:

| Part | Who writes it | What it holds |
|---|---|---|
| Locked top | You | Common imports or includes only, no declarations. Never empty: a comment is enough if the language needs nothing. |
| Your code | The student | The function. It opens with the language's starter, the stub you write. |
| Locked bottom | You | Reads the input, calls the student's function and prints what it returns. |

The judge compiles the three parts as one file (the composed file): each part is cut after its last line and given one newline, then they are joined in order. Students see the locked code and cannot change it. The judge, the checker and the hidden tests work exactly as for a whole program.

**Terms used in the editor and in this guide.**

| Term | Meaning |
|---|---|
| Function problem | A problem where the student writes one function; the locked code around it makes the program. The opposite is a whole-program problem, the default. |
| Locked top | The locked code before the student's code: common imports only. |
| Your code | The student's part. It starts as the starter, and it is all a submission stores. |
| Locked bottom | The locked code after the student's code: reads the input, calls the function, prints the answer. |
| Composed file | The three parts joined in order. This is the one file the compiler sees. |
| Tombstone | What Delete leaves behind: the problem's row, revisions, submissions and contest rows, with its tests, solutions, checker, generator and starters cleared. See "Deleting a problem". |
| Problems to finish | The list on a student's profile (/me) of problems in fully ended contests where neither the contest score nor the best practice score is the maximum. |

**Making one.** In the editor, on the Statement tab, open "What students write" and choose "One function, between locked code". For each language you offer, fill in the locked top and the locked bottom there, and the stub under "Starter code". In a pack, use format 4 (see `problems/array-sum`):

```yaml
format: 4
mode: function
starters: [starter.cpp, starter.java, starter.js, starter.py]
locked_top: [top.cpp, top.java, top.js, top.py]
locked_bottom: [bottom.cpp, bottom.java, bottom.js, bottom.py]
```

Rules, checked on every save and import:

- A language is offered only when it has a locked top, a locked bottom and a starter. A student can choose only the offered languages, and Submit, Run and drafts in another language are refused ("This problem takes C++17 or Python 3.").
- The reference solution and the brute force are bare functions, like a student's answer, written in offered languages: each is judged composed with its own language's top and bottom. The validator, the checker and the generator are whole programs, as always.
- Each locked top and each locked bottom is at most 4 KB.
- The mode cannot change once a student has a submission, a run or a draft on the problem: "This problem already has submissions or drafts, so its mode cannot change. Make a new problem instead." Locked code can still be edited.

**Java.** The locked top holds the imports (`import java.util.*;`). The student writes `class Solution { ... }`, not public. The locked bottom holds `public class Main`, reads the input with `Scanner` and calls `new Solution().yourFunction(...)`. The judge always compiles `Main.java` and runs `Main`.

**Imports.** Imports cannot be hidden, so the locked top supplies the common ones and students may add their own at the start of their code (a Java file allows more imports before the first class). C++, Java and Python accept an import twice. A JavaScript top declares nothing a student might declare again; `'use strict';` is a good top. Students see: "Common imports are already added. Add more if you need them."

**Changing locked code.** The mode and the locked code are judging content, like the reference solution: a change needs Validate again, and a contest that pins the problem keeps judging with the pinned locked code until "Update students now". Starters are not judging content, so a stub edit needs no new validation.

**Compile errors.** The compiler sees the composed file, so Podium rewrites the line numbers in compile output before anyone sees them: a line in the student's code is counted from their first line, and a line in locked code reads "locked code, line N", counted within that block. Each submission keeps the number of locked top lines it was judged with, so an old submission still shows the right lines after you edit the top. A compile error in locked code usually means the student renamed the function or changed its parameters. Runtime stack traces are not rewritten; for JavaScript, which has no compile step, the first line of an error (the place) is.

**What locked code is not.** It is a teaching aid, not an integrity control. A student can still read standard input or print from their own code, redefine things in C++, or close the function early and write more code. Everything runs in the sandbox, and the checker and the hidden tests decide the verdict, so such tricks only help a student who could solve the problem anyway.

**Renamed functions in Python and JavaScript.** In C++ and Java a student who renames the function gets a compile error and the hint "Your code no longer matches the function the locked code calls." In Python and JavaScript there is no compile step, so the same mistake is a runtime error, and the student sees "Runtime error on test 1" with no hint (Run shows the error text, Submit does not).

**AI.** The AI wizard stays whole-program: it writes whole-program problems only, and neither the wizard nor the Preferences box can ask it for a function problem. To make a function problem with AI, use "Packs from another AI" and ask for a format 4 pack, or convert a draft by hand.

## Statement

`statement.md` is Markdown, rendered as students will see it:

- Use `##` headings for sections such as Input, Output and Constraints. Heading levels are shifted to fit the page.
- Maths is TeX between dollars, drawn with KaTeX: `$1 \le n \le 10^5$` inline, and `$$ ... $$` on its own lines for a display formula. A dollar followed by a space, or a closing dollar followed by a digit, is plain text, so "costs $5 and $10" stays as written. Write `\$` for a dollar sign that would otherwise start maths.
- Raw HTML is shown as text, never rendered. Inline styles are removed, and links open in the same tab.
- Packs cannot carry image files yet, and the page's security policy blocks images from other sites.
- At most 64 KB. Put large data in tests, not in the statement.
- Tags help organisers find problems in the list; write them as plain words ("two pointers", "prefix sums"), not internal shorthand.
- When an image is used (a later pack format may carry them), give it a description in its alt text, `![a 3 by 3 grid with the start in the top left](grid.png)`, so a student who cannot see it still has the problem; the editor's readiness panel lists images without one.

Statement, title, tags, difficulty and subtask titles can change at any time without affecting validation.

**During a live contest** (one that pins the problem and is between its start and its close plus the longest personal extension), a save that changes the problem needs a reason: the editor asks for one, and "Replace tests" takes it too. The reason is kept with the new revision. Students keep seeing the revision the contest pins until an organiser chooses "Update students now" on the contest's Live tab; see "Revisions" below.

## Samples and subtasks

Every test is exactly one of two kinds (V3-5):

| Kind | Who sees it | Rules |
|---|---|---|
| Sample (`samples`) | Everyone, with the statement | Test numbers, each once; no points. Run checks every sample; a Submit judges them first and stops at a failing one. |
| Hidden test (in a subtask) | Nobody, during a contest | See the subtasks below. Only Submit judges them. |

| Item | Rules |
|---|---|
| `samples` | Test numbers, each once. Shown with the statement; no points. |
| `starters` | Format 3. One file per language, the extension names it (`.cpp`, `.java`, `.py`, `.js`), at most 4 KB each: the code a student's editor opens with in that language before they type. A language without one gets Podium's own starter, which reads input the fast way. Statement-like: it makes a revision, is outside the content hash (no revalidation, no rejudge), and reaches a contest only through "Update students now". |
| `subtasks` | Up to 50. Each gets 1 to 1000 points and a `tests` range list. A student scores a subtask's points only by passing all of its tests. |
| Range list | Comma-separated test numbers and ranges: `"3-5"`, `"3-5, 8"`, `"7"`. Ranges run upwards, numbers are 1 to 999, and no test may appear twice. It is stored in its shortest form (`"3, 4, 5"` becomes `"3-5"`). |

Validation refuses to start while a test is in no subtask (and is not a sample), in two places, or a range names a test that does not exist. Points that do not add up to 100 are a warning, not an error. Validation runs the reference, the brute force and the validator over samples like every other test, and they need expected outputs the same way.

**When hidden tests are shown.** A submission that never counts shows its first failing hidden test (input, expected output, the program's output, at most 4 KB each): practice after the end when "practice after the end" is on and no other contest that pins the problem is still open, drafts and upcoming ones included, and your own tries in the workspace. A submission that counts never shows one. Name subtasks by purpose ("Correctness", "Time complexity"), since their titles are shown.

## Revisions

Every save that changes a problem (its statement, title, starter code, tests, limits, checker, solutions, samples, subtasks or generator) makes a new numbered revision; a save that changes nothing makes none. Tags, slug, difficulty and the generator plan are details of the problem's entry and make none. Generated inputs and outputs make revisions too, without an author.

- A contest pins one revision per problem when the problem is added. Students are served that revision's title, statement, starter code and samples, never the editable problem, so an edit changes nothing in a contest until an organiser chooses "Update students now" on the contest's Live tab, where a banner names the edited problem.
- The update is accepted when the revision is the validated content of the problem. When only the statement or title changed, no rejudge is needed, and Live offers a prefilled announcement to post, which is how students learn of the change. When the tests, limits or checker changed, validate first, update, then rejudge.
- **"Update students now" on an ended contest can change final scores.** When the new revision's subtask points differ from the pinned ones, every student's total changes. Podium refuses at first (409 `ended_scores_change`) and the Live tab's dialog lists each problem with its old and new points and how many students are affected. Only a second confirmation applies it, and the history of changes records "Final scores changed by an update". Lowered points apply at once; raised points show in scores only after you press "Judge again" for that problem, and the second dialog says so. If no points differ, it applies at once. A contest that holds a problem changed since it was added will not publish, and an ended contest keeps practice closed for that problem, until the update is made.
- Revisions are kept as the record of what students were served and judged against; there is no list of them in the editor. v1.2 removed the revision lists, statement diffs and the log of what each student was served (V3-4): each served a case no single-college contest has met yet.

## Deleting a problem

Archive hides a problem and you can restore it. **Delete** is for a problem you never want again, and it cannot be undone. Open the problem, choose More, then Delete, and type the problem's slug to confirm.

- An **admin** can delete a problem that was never used: no contest holds it and nobody submitted or ran code on it. The **owner** can also delete a problem that was used.
- Nobody can delete a problem while a contest that holds it has not ended (a draft, a scheduled or a running one). The dialog names the contests; take the problem out of them or wait.
- Before you delete a used problem the dialog offers Download pack and a link to each contest's results, and names the ended contests where practice for the problem closes.
- The problem leaves the bank, and its tests, solutions, checker and generator are removed. The list at Problems, Deleted problems (admins and the owner) shows it with who deleted it and when. Its slug is free to use again.
- **Ended contests keep everything students and you saw**: the standings, each student's code and verdicts, and the statement as it was pinned. The problem shows there with the label Deleted problem and no link. Practice for it closes, and Submit, Run and judging again are refused with "This problem was deleted. Your earlier submissions are still listed." An ended contest keeps the deleted problem; it cannot be taken out of a contest whose practice already showed students its hidden tests.
- **Problems to finish.** Once a contest has fully ended, each student sees the problems they have not finished on their profile (/me), with a practice link; a problem you changed after the contest shows there as "Practice closed", and the workspace says "This problem was changed after the contest ended, so practice for it is closed here."

## Checkers

| Checker | Use when |
|---|---|
| `tokens` (default) | The answer is a fixed sequence of tokens; spacing and line breaks do not matter. |
| `exact` | Formatting matters. Windows line endings and trailing newlines are ignored. |
| `float` | The answer has real numbers. Accepts absolute or relative error up to `epsilon` (default 1e-6). |
| `custom` | Many answers are correct. A testlib checker in `checker.cpp`. |

### Writing a testlib checker

The checker is C++17 using [testlib](https://github.com/MikeMirzayanov/testlib). `testlib.h` is installed on the judge by `install.sh` (step `testlib`, release 0.9.41), so `#include "testlib.h"` works without shipping it in the pack.

It runs in a fresh isolate box as `checker input.txt output.txt answer.txt`: the test input, the contestant's output, and the expected output from `tests/NN.out`. In testlib these are `inf`, `ouf` and `ans`. Example from `problems/pair-with-sum/checker.cpp`, which accepts any valid pair:

```cpp
#include "testlib.h"
#include <vector>

int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);
    int n = inf.readInt();
    long long s = inf.readLong();
    std::vector<long long> a(n);
    for (auto& x : a) x = inf.readLong();

    bool exists = ans.readInt() != -1;
    int i = ouf.readInt(-1, n, "i");
    if (i == -1) {
        if (exists) quitf(_wa, "a pair exists, but the output says -1");
        quitf(_ok, "no pair exists");
    }
    int j = ouf.readInt(1, n, "j");
    if (i < 1 || i >= j) quitf(_wa, "positions must satisfy 1 <= i < j, got %d %d", i, j);
    if (a[i - 1] + a[j - 1] != s) quitf(_wa, "a[%d] + a[%d] is not %lld", i, j, s);
    if (!exists) quitf(_fail, "the output found a pair the expected answer missed");
    quitf(_ok, "valid pair %d %d", i, j);
}
```

How its exit is read:

| Checker result | testlib | Verdict |
|---|---|---|
| exit 0 | `_ok` | Accepted |
| exit 1, 2, 4 or 8 | `_wa`, `_pe`, `_dirt`, unexpected end of file | Wrong answer |
| exit 7, or 50 and above | `_points`, partial scores | Wrong answer: a test earns no points on its own, since subtasks are all or nothing |
| exit 3 | `_fail` | Internal error: "The checker reported a failure" |
| crash, time or memory limit, any other exit | | Internal error |

Use `_fail` when the expected answer itself is wrong, as above: during validation that fails the problem instead of blaming a contestant later.

Limits: compiling gets 30 s of CPU (testlib is a large header; a normal compile gets 10 s). Each run gets 10 s of CPU, 20 s of wall time, 256 MB and one process. The first 256 bytes of what it prints (standard output and error together) are kept as the message shown with the result.

## Expected outputs from the reference

Write the inputs and a correct reference solution; the judge writes the expected outputs.

| Action | What it does |
|---|---|
| Validate | While any test has no output, runs the reference on those inputs only, then validates once every output arrived. The title row says "Making outputs, then validating". |
| Regenerate all outputs (in the editor's More menu) | Runs the reference on every input and replaces every output. Asks to confirm first: "Every expected output is replaced by what the reference solution prints." |

How it runs:

- Unsaved changes are saved first. The reference runs in isolate with the problem's time and memory limits (times its language factor), at a lower priority than contest work.
- Only one generation per problem runs at a time. The result appears in the editor while it waits and runs.
- A test where the reference fails keeps no output, and the result names it: "The reference solution failed on 1 test, so those outputs are still missing", with the verdict and test number. Outputs from the tests that passed are kept.
- If the reference does not compile, nothing is generated: "The reference solution did not compile, so no outputs were generated", with the first lines of the compiler's message.
- If you change the problem while outputs are being generated, nothing is applied: "Some outputs were not applied", with the reason. Generate again.
- New outputs change the problem's content, so validate again afterwards. Validation then checks the generated outputs against the brute force and the checker like any other.

Generated outputs are ordinary test files: Download pack includes them, and they can be replaced later with "Replace tests".

## Tests from a generator

Instead of writing every input by hand, give the problem a generator and a plan: the judge runs the generator once per planned test and keeps what it prints as that test's input.

### generator.py

- Any of the four languages; Python 3 with the standard library is the usual choice.
- `argv[1]` is the seed, an integer from 0 to 2147483647. The rest are the test's arguments, each up to 64 characters of letters, digits and `_ . , : = + -` (no spaces or braces), at most 16. Writing them `key=value` keeps them readable.
- Same seed and arguments, same output: seed your random generator with the seed (`random.Random(seed)`), never the clock.
- Print exactly one input on standard output. Standard input is empty.
- Limits per test: 5 s of CPU times the language factor (a Python generator gets 15 s), 256 MB, 16 MB of output.
- It runs in isolate like any setter code, never on the server.

```python
import random
import sys

seed = int(sys.argv[1])
opts = dict(arg.split("=", 1) for arg in sys.argv[2:])
max_n = int(opts.get("n", "100"))
rng = random.Random(seed)
n = max_n if seed % 2 == 0 else rng.randint(1, max_n)   # even seeds hit the maximum size
values = [rng.randint(-10**9, 10**9) for _ in range(n)]
print(n)
print(" ".join(map(str, values)))
```

### tests.yaml

A YAML list, parsed strictly like `problem.yaml`. Each entry makes one test:

```yaml
- test: 3          # the test number this input becomes; not a sample
  subtask: 1       # the subtask it is meant for (the subtask's range must list it)
  seed: 3
  args: [n=100]
- test: 4
  subtask: 1
  seed: 4
  args: [n=100]
- test: 5
  subtask: 2
  seed: 5
  args: [n=200000]
- test: 6
  subtask: 2
  seed: 6
  args: [n=200000]
```

Rules: each test number once, 1 to 999, never a sample (samples come from the statement); the subtask must exist; planned tests need a generator. A planned test that a subtask's range does not list, or that has no input yet, is a warning in the editor. `generator_args` on a subtask in `problem.yaml` are the arguments the editor proposes when you plan more tests for it.

### Generate inputs

Generate inputs in the editor runs the generator for every planned test, in one judge job. When every planned test got an input, outputs are generated from the reference, and when every output arrived, validation is queued: one press, three steps, each shown in the editor.

- Nothing is applied unless every planned test got an input. Each failure is named: "The generator failed on test 7 (subtask 2, seed 3): Runtime error.", followed by the end of the generator's error output. An empty output is named too.
- A planned input replaces any test with the same number, and that test's expected output is cleared until outputs are generated.
- If the problem, the generator or the plan changed while the job ran, nothing is applied. Generate again.
- Only one input or output generation per problem runs at a time.

## Practice sets

A practice set is a contest that exists only so students can practise its problems. There is no separate feature: use a contest.

**Recipe.**

1. Create a contest and add the problems (validated, and not tagged "ai draft").
2. Set its start and end in the past, for example last month, and turn "practice after the end" on.
3. Write the introduction in the contest's description: students see it as "About this contest" on the contest page, below their problem list.
4. Publish it. It is already ended, so students invited to it can open each problem in practice at once.

**Limits.**

- A failing practice submission shows the first failing hidden test (see "When hidden tests are shown"). Do not reuse the problems of a practice set in an exam.
- Problems can be added to a published contest but not removed from it, because practice has shown students their hidden tests.
- A judging edit (tests, limits, checker, solutions, locked code) closes practice for that problem: students see "This problem was changed after the contest ended, so practice for it is closed here." A contest that holds the changed problem cannot be saved or published until you press "Update students now" on its Live tab. Read the warning under "Revisions" first: on an ended contest that update can change final scores.
- A student who never started the contest before it ended is not listed on their profile, and "Problems to finish" lists only contests the student started. In a practice set students open the contest page from the link you send them and practise each problem there. The contest page keeps the contest score (0 for them), practice does not change it, and the profile does not list the set.

## The AI wizard

"New problem with AI" on Problems asks a model to write a whole problem: statement, samples, subtasks, a C++ reference, a Python brute force, a Python generator with its plan, a Python validator, the checker and limits. An owner or admin pastes their own provider key first: Anthropic, OpenAI or Google Gemini (`docs/OPERATIONS.md`, "AI wizard keys"). The server lists that provider's current models; choose one, or type the id of a model the list does not show. The key stays in the server's memory for 2 hours and is forgotten on sign-out. The wizard shows which provider and model wrote each draft.

The three providers get the same instructions and the same answer schema, and every answer passes the same checks, so a draft from any of them is held to the same rules. Models differ in how often their first answer passes: when one keeps failing a check, try another model before rewriting the brief.

### Custom prompt

The wizard's prompt has a fixed security part and replaceable authoring rules. An owner can replace the rules with a text file named in `PODIUM_AI_PROMPT_FILE` (`docs/OPERATIONS.md`, "Custom wizard prompt"). The fixed part (the brief or pasted text is data, rewrite never copy, answer in the schema) cannot be replaced and is always sent first. Admins and owners see "This site uses a custom prompt" on the wizard page while one is active. If a custom prompt makes drafts worse, remove the variable and restart to go back to the built-in rules.

### From a brief

Give a topic, a difficulty (easy, medium or hard), constraints and subtask ideas, each up to 2,000 characters. Concrete constraints ("n up to 2e5, values up to 1e9 in absolute value") give better generators and validators than adjectives ("large").

### From pasted text

Paste an existing problem, up to 20,000 characters. The model rewrites it in its own words as a standard input and output problem; the text goes to the model as data inside a delimited block, and instructions hidden in it are ignored.

Problem statements on LeetCode, Codeforces and similar sites are copyrighted. The rewrite keeps the idea, not the prose, but the result is still derived from someone else's work: use it in your own contests, and do not publish or redistribute such packs.

### What happens next

1. The answer is checked: every field, the sample count, subtask points adding up to 100, the planned tests, and the editor's own rules. An answer that fails is sent back once with the reasons; if the second one fails too, the wizard says "The model's answer could not be used." and lists them.
2. A draft problem is created with origin `ai` and the tag "ai draft". Its samples are tests 1 to k, then the generated tests. The tag stays until a person changes the statement, the reference or the brute force and saves. A save that changes none of them keeps the tag, and the change that ends it also withdraws the validation, so the problem must pass again as the person left it.
3. Generate inputs is queued at once, which chains Generate outputs and Validate (see "Tests from a generator"). The wizard page follows each step to the end (making the tests, validating the draft, then "Validation passed" and Open the draft); the editor shows them too. Without a key or a model, the key form appears on the wizard itself, above the brief.
4. You read every part of the draft, fix what needs fixing, save it and validate it. The aim is a draft that needs at most one manual edit.

The model's code is never run on the server: it goes to the judge, in isolate, like any setter's code. Nothing AI-made reaches students before a person has reviewed and validated it: only validated problems can be added to a contest, and a problem still tagged "ai draft" is refused even when the chain validated it ("The problem for label B is an AI draft. Read it and save it in the editor before adding it to a contest.").

**A contest needs a person's solution.** The tests of an AI problem come from the model's generator and their expected outputs from the model's reference, so validation cannot catch a wrong reference: they agree by construction. Every contest counts (V3-6), so a contest refuses an AI problem until a person has written a solution for it: an editor save that changes the reference or the brute force (write your own brute force; it is the independent check), followed by a validation that passes. An admin may instead use it as written by giving a reason of at least 10 characters when adding it; the reason is kept in the contest's audit log, and Setup asks for it.

One AI request per admin runs at a time, for at most 3 minutes. A refusal by the model, a cut-off answer, a rate limit or a provider outage is shown in words; nothing is created.

## Editing a problem with AI

The editor's More menu has "Edit with AI". Type what should change ("add a note that sums can be negative", "make the brute force simpler", "use a custom checker for ties") and press "Ask for a draft". The model sees the problem as a pack: `problem.yaml`, `statement.md`, the programs, starters, locked top and bottom of a function problem, `tests.yaml` and a custom `checker.cpp`. It does not see the tests and cannot change them. It returns only the files it changes; every other file stays as it is, and the slug stays.

The page shows what the model says it changed and a diff of each file. Read it as you would read a colleague's change. "Accept the edit" saves it as one new revision, tags the problem "ai draft", withdraws its validation and starts Validate. Until a person saves the problem in the editor and Validate passes, a contest refuses it (see "A contest needs a person's solution" above). "Reject the edit" throws the draft away.

- You cannot accept while a contest that holds the problem is running. Accept after it ends. A contest that already holds the problem keeps the revision it was pinned to until you press "Update students now".
- A change that needs new or different tests (a new sample, new generated inputs) is made in the editor: the model cannot write test data. Ask it to change the generator or `tests.yaml`, accept, then use Generate inputs.
- If the model asks for something a pack cannot carry, or its answer does not parse, the page says so and nothing changes.
- Save or discard your own unsaved changes first: the model works on the saved problem, and a draft made from an older version is refused.

## Validation

Validation runs the problem on the real judge, in isolate, exactly as a contest would. Choose Validate in the editor; the report updates every 2 seconds while the job waits and runs. Validation has the lowest priority, behind all contest work.

### What runs

Validation does not start while the structure is incomplete (see "Samples and subtasks") or any test has no expected output.

1. Compile the reference, brute force, validator and checker. If the reference or the checker does not compile, nothing else runs. A brute force or validator that does not compile is reported, the run goes on without it, and validation fails.
2. For each test, in order:
   - the validator reads the input on standard input. Exit 0 accepts it; any other exit rejects it, and the end of its standard error is shown as the reason.
   - the reference runs with the problem's limits, and its output goes through the checker.
   - the brute force runs with a longer limit, and its output goes through the checker. Running out of time is not a failure in itself (brute forces are slow on large tests), but that test's expected output was not cross-checked, and the report says so.
3. The server reads the results and decides.

| Program | CPU time | Memory |
|---|---|---|
| Reference | the time limit times its language factor | the memory limit plus its language overhead |
| Brute force | 10 times the time limit, at most 10 s | the memory limit plus its language overhead |
| Validator | 5 s | 256 MB plus its language overhead |
| Checker | 10 s (30 s to compile) | 256 MB |

The brute force and validator budgets are real CPU seconds in any language: a Python validator gets 5 s, not 15.

### When it passes

All of these must hold:

- every program compiles;
- the reference gets Accepted on every test;
- the validator accepts every input (when there is one);
- the brute force never disagrees with an expected output (when there is one);
- with a brute force, at least half of the tests were cross-checked: the brute force finished them, so their expected outputs were compared with a second solution. Fewer fails: "Only 2 of 6 tests were cross-checked against the brute force; at least half must be." Add smaller tests the brute force can finish, or make it faster;
- the reference's slowest test takes at most half of its time limit (see below).

Warnings never fail validation: the reference above a third of its limit, no brute force, no validator, tests the brute force did not finish ("2 of 6 tests were not cross-checked: the brute force ran out of time or was skipped on tests 5 and 6."), a brute force crash or memory failure, and subtask points not adding up to 100.

### Reference timing

The limit compared is the time limit times the reference's language factor. With a 1000 ms limit, a C++ reference must finish every test within 500 ms; a Java reference (factor 2) within 1000 ms. Above half the limit validation fails: "The reference solution took 640 ms on test 6, more than half the 1000 ms limit. Raise the limit or speed up the reference." Above a third it warns.

### Reading the report

| Line | Meaning |
|---|---|
| Validation passed / Validation failed | The run finished; the problem is validated only when it passed. |
| Validation could not finish | The run did not happen (the judge stopped responding three times, took too long, or isolate failed). The reason follows. Nothing about the problem changed. |
| Waiting for the judge (2nd in queue) / Running | Still in progress. Waiting for a long time means no judge is running (`docs/OPERATIONS.md`, "Validation troubleshooting"). |
| Reference solution did not compile | The compiler output follows. The same line exists for the brute force, validator and checker. |
| Reference solution passed 5 of 6 tests | Accepted tests out of all tests. Every test must pass. |
| Brute force agreed on 6 of 6 tests | Out of the tests where the brute force did not run out of time. A disagreement fails validation; a crash is only a warning. |
| 2 of 6 tests not cross-checked: the brute force did not finish them | Tests resting on the reference alone. Muted while at least half are cross-checked; red, and validation fails, when fewer are. |
| Validator accepted 6 of 6 inputs | Every input must be accepted. |
| Slowest reference run 3 ms of 1000 ms | The reference's slowest test against its limit; red above half. |
| Test 4: reference solution got wrong answer. ... | One line per failure, naming the test, the program and the verdict, with the checker's or validator's message. |
| Notes in muted text | Warnings, which do not fail validation. |

The full table of every program on every test opens on request below the report. A report for an older version of the problem says so; validate again to check the current one.

### What unvalidates a problem

"Validated" is tied to a hash of everything validation depends on. Changing any of these makes the problem show as not validated until it passes again:

- time limit, memory limit, checker, `epsilon` (float), checker source (custom);
- the reference, brute force or validator: source or language, or adding or removing one;
- samples, subtask points and test ranges;
- any test file (including "Replace tests").

Title, slug, statement, tags, difficulty and subtask titles do not. Upgrading the judge does not either; but if a later run of the very same content fails (a slower judge, a fixed judge bug), the validation is withdrawn.

Making outputs or inputs changes test files, so it unvalidates the problem too. The generator and the plan themselves are not in the hash (decision D53): editing the generator does not unvalidate the tests it already made. Generate inputs again to use the new generator.

On an AI draft, the first save that changes the statement withdraws the validation too (see "The AI wizard"), although the statement is not in the hash.

## Before a contest: the setter checklist

Validation proves the tests and the reference agree with each other. It cannot prove they are right. Before every contest, for every problem in it:

1. **Reference judged live.** Open the problem from the contest as an organiser (the workspace) and submit the reference solution on the real judge. It must get the full score on every subtask.
2. **A known-wrong solution judged live.** Submit at least one solution you know is wrong in a typical way (an `int` that should be `long long`, a missing edge case, a brute force that is too slow). It must lose points, on the subtask you expect. A wrong solution that scores full marks means the tests are too weak.
3. **Brute force cross-checked.** Validation passes with a brute force, and the report shows at least half the tests cross-checked. Look at which tests were not, and add small tests if the large ones carry subtasks of their own.
4. **AI problems.** For every problem whose origin is `ai`, write a solution of your own (a brute force is enough), save it, and validate: it must pass every test. Using an AI problem as written, with a reason, is the exception, and the reason is in the audit log.
5. **Freeze.** While the contest runs, import no packs, run no AI wizard and rejudge nothing unless a test is wrong. A rejudge is recorded in the audit log with who asked for it.
6. **Practice shows hidden tests.** In practice after the end, a failing submission shows the first failing hidden test (D115), unless another contest that pins the problem has not closed yet, drafts and upcoming contests included (V3-6). Anyone who keeps practising can read the hidden tests one by one, so a problem used that way is public: do not reuse it in another contest without new tests. Problems and the problem picker in Setup mark such a problem "Hidden tests shown in practice", and publishing a contest that holds one names it in the confirm. A problem whose hidden tests a published contest has shown stays in that contest (it cannot be removed), so the mark cannot be lost before an exam. Every contest counts: v1.2 has no ungraded contests.

## Trying a problem as a student

"Try as a student" in the editor's title area, or "Try" on a row of Problems, opens the problem in the student workspace (`/admin/problems/{id}/try`): the statement with its samples, the code editor, Run, Your input, and Submit with the subtask strip filling in as tests are judged.

- Use it to check the statement reads well, the samples copy cleanly, and a wrong solution gets the verdict and score you expect.
- These submissions are yours and count in no contest. Your draft is kept per problem.
- The problem does not need to be validated, but it must be complete: tests in subtasks and every expected output present. Otherwise Run and Submit list what to fix.
- Submits are limited to one every 5 seconds, one waiting per problem; runs to one every 2 seconds.
- You see full figures (exact memory per test). Students will see coarser figures and no hidden test data (`docs/ARCHITECTURE.md`, "What students can see").

Students get the same workspace inside contests.

### Editing the prompts and the Preferences box

The owner can edit the rules text of the wizard prompt and of the prompt for another AI in the Dashboard, tab AI prompts: the editor shows a diff of the edit against the active text, the fixed part and the built-in default read-only, the history with who saved each version and when, Restore this version, Reset to default and a preview of the full prompt. An admin or the owner can also fill the Preferences box (2,000 characters), style choices that are added to every AI-generated problem, in the wizard's prompt and in the prompt for another AI, always inside a block marked as the admin's choices. Neither changes the answer schema: the wizard writes whole programs that read standard input; it cannot write a function problem. To get a function problem from an AI, use the prompt for another AI, whose built-in rules teach function problems (a locked top and bottom per language, Java reading with `Scanner`, exact parameter and return types, array or list, imports in the top only), and import the pack.

## Packs from another AI

Any AI chat can write a pack for Import pack, not only the wizard's three providers. On Problems, "Prompt for another AI" opens a prompt to copy into that chat. Its first line names the Podium release and the pack format (for example "Podium v2.0.2 pack prompt, pack formats 1 and 4"), so a saved copy says which build it came from. "Download as file" saves it as `podium-pack-prompt.txt`. It describes the pack format the server reads (file names, the `problem.yaml` keys, the limits under "Import limits") and ends with a line where you describe the problem you want.

1. Copy the prompt, replace its last line with your problem, and send it.
2. If the AI can make files, it returns a zip. Otherwise it returns each file under its exact name: put the files in one folder and zip them (Windows: select the files, right-click, Send to, Compressed (zipped) folder; Mac: select the files, right-click, Compress). Zip the files, not a folder around them.
3. Use Import pack. If it fails, paste the error list back into the chat and ask for a corrected pack.
4. The problem still has to pass Validate before a contest can use it. The reference and the tests come from the same chat, so read them and write your own brute force, as with any AI draft.

Write the problem in your own words, and do not paste unreleased contest problems or student material into an outside AI. Expected outputs (`tests/*.out`) may be left out: Podium makes them from the reference solution.

## Import limits

Packs are parsed on the server outside any sandbox, so the server is strict:

| Limit | Value |
|---|---|
| Zip file | 32 MB |
| All files once unzipped | 128 MB, counted on the bytes actually decompressed, never on what the zip claims |
| Entries | 2,000, read from the zip's end record before anything is decoded |
| One file | 16 MB |
| `problem.yaml` | 64 KB, strict |
| Tests | 999 |
| Sources and checker | 64 KB each, UTF-8 text |
| Statement | 64 KB |

Refused entries: absolute paths, `..` or `.` segments, backslashes, colons, links, devices and duplicate names. Hidden files and `__MACOSX` folders are skipped. When something is wrong the import lists every problem it found, so the pack can be fixed in one pass. A pack whose slug is already in the bank is not imported until you choose (see "Import" below).

Only one pack import, test replacement or pack download runs at a time; a second one waits.

## The editor

| Step | How |
|---|---|
| Create | Problems, New problem: enter a title. The slug (used in pack file names) is made from it and does not change. The problem starts with a statement outline and a C++ reference template. |
| Import | Problems, Import pack: pick one or more `.zip` files. Each imported problem starts validating at once and the list shows "Validating" until the result is in, so several packs need no visit to the editor. A pack the server refuses names its problems; a problem that cannot be validated yet says why in a message. Rows that are not validated offer Validate. |
| Import, slug already used | The import stops and asks. Three choices, one button that follows the choice: **Import as new** (under another slug, filled in with a free one), **Archive and import** (the existing problem is archived and frees the slug, and the pack takes it; the dialog lists the contests that pin the old problem and have not ended), **Replace problem** (saves the pack as a new version of the existing problem; contests keep the version they use until you update them, and past results keep theirs; origin and tags come from the pack). Replace is unavailable while the problem is in a running contest: edit it from the problem page, where you can update students. When several packs are picked, the choice is asked for each pack that collides. |
| Edit | Three tabs. Statement: title, the statement (the workspace's code editor, as plain text) with a preview of it and its examples as students see them, limits, difficulty and tags. Tests: one row per test with the start of its input and output and a Role select (Example, Subtask 1, Subtask 2, ..., Not in a subtask), then each subtask's points and title. Checking: the reference solution, how answers are compared, and under "Optional extra checks" the brute force, the validator, a custom checker and the generator, each named in one line while folded; the validation report sits beside them from 1280 px. Removing the brute force, the validator or the generator can be undone until you save. Save changes. The roles are saved as the sample list and subtask ranges of the pack format; a subtask's tests need not be consecutive. |
| Replace tests | Tests tab: upload a zip of `NN.in` files, each with an optional `NN.out`, at its top level or in `tests/`. Every test is replaced; roles are kept by test number. Single test files can be downloaded from the test list. |
| Generate inputs | Runs the generator for every planned test, then generates outputs and validates (see "Tests from a generator"). |
| New problem with AI | On Problems: the AI wizard writes a draft from a brief or from pasted text (see "The AI wizard"). |
| Validate | Saves unsaved changes first, makes missing outputs, then queues the run. The title row keeps one status line ("Validated, 7 tests, slowest 286 ms", "Validation failed on test 5"), a link to the report. Under the bar, on every tab: what stops validation (a test in no subtask), and, for an unsaved change to a judged field (limits, checker, solutions, validator, examples, subtask points and tests), that saving needs a new validation; a change to the title, statement, tags or subtask titles does not, except on an AI draft, whose statement counts. Advice ("Before you validate", "Worth adding") sits with the report. |
| Add to contest | Takes Validate's place once the problem has passed and nothing is unsaved. Pick a draft or upcoming contest; the problem goes in after its other problems under the next letter. |
| Try as a student | Opens the problem in the student workspace (see "Trying a problem as a student"). |
| Download pack | The same layout as an import, ready to re-import or keep in version control. Sources are named `solution`, `brute`, `validator` and `generator` with their language's extension (a function problem's locked code goes to `top` and `bottom` files, its stubs to `starter` files), the plan is written to `tests.yaml`, and tests are numbered with two digits (three from test 100). Outputs not generated yet are left out. |
| Archive | In the More menu, with Download pack and Regenerate all outputs. Asks to confirm, then hides the problem from the list. Its tests and history are kept; "Show archived" lists it again. Archiving frees the problem's slug: the problem takes `<slug>-archived-<id>` and keeps the original, so you can import or create another problem with that slug. The dialog lists the contests that pin the problem and have not ended: it keeps working there, but you cannot update it in that contest until you restore it. An archived problem's slug cannot be changed, and Download pack writes the original slug into `problem.yaml`. |
| Restore | In the More menu of an archived problem, and on its row in the bank with "Show archived". Asks to confirm and gives the original slug back. If another problem has taken it meanwhile, it asks for a new slug with a free one filled in. Contests have the same: Show archived on the Contests page, and Restore in a contest's More menu. |

## Example packs

`problems/` in the repository holds four packs that pass validation. Zip a folder to import it.

| Pack | Shows |
|---|---|
| `a-plus-b` | `tokens` checker, C++ reference, Python brute force and validator, 2 samples, 2 subtasks |
| `average-speed` | `float` checker with `epsilon`, Java reference, Python brute force and validator, and two samples |
| `pair-with-sum` | `custom` testlib checker accepting any valid pair, C++ reference, JavaScript brute force |
| `array-sum` | A function problem (format 4) in C++17, Java 21, Python 3 and JavaScript: locked tops, stubs and locked bottoms; Java reads with `Scanner` in `public class Main`; the reference and brute force are bare functions |
