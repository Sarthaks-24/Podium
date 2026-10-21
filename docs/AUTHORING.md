# Writing problems

Admins and owners write problems in the problem editor (Problems, in the header) or import them as a problem pack. Validation then runs the problem on the real judge; from M4, only a validated problem can go into a contest. The AI assistant arrives in M5.

## Input and output

Every problem reads standard input and writes standard output. Problems in the style of LeetCode (a function signature) are rewritten into this form.

## Limits and languages

The time limit you set applies to C++. Other languages get a multiple of it, and extra memory for their runtime:

| Language | Id | Time | Memory |
|---|---|---|---|
| C++17 | `cpp17` | 1.0 x | limit |
| Java 21 | `java21` | 2.0 x | limit + 256 MB (heap capped at the limit) |
| Python 3 | `python3` | 3.0 x | limit + 64 MB |
| JavaScript (Node 22) | `node22` | 2.0 x | limit + 128 MB |

Java sources must declare `public class Main`.

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

**Pack format.** Podium reads formats 1 and 2 (format 2 since phase P2). Format 2 adds `open`, the open tests (see "Samples, open tests and subtasks"); everything else is format 1, and a pack without the line is format 1, so every pack written before still imports. A format 1 pack that lists `open` is refused ("problem.yaml lists open tests, which need format: 2."). Exports write `format: 2` only for a problem with open tests and `format: 1` otherwise, so a problem without them still imports into a server that reads format 1 only. A pack of a newer format is refused with both numbers named ("problem.yaml is pack format 3, and this server reads format 2 and older. Upgrade Podium, or export the problem again from a server of this version."), rather than read with its new fields missing.

Parsed strictly: an unknown key is an error, so a misspelt key never silently falls back to a default. Comments are allowed.

```yaml
format: 1                       # pack format: 1, or 2 when the pack lists open tests (exports write it)
title: Pair with sum            # shown to students; up to 120 characters
slug: pair-with-sum             # lowercase letters, digits and single hyphens; unique in the bank
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
samples: [1, 2]                 # test numbers shown with the statement; they carry no points
# open: [3]                     # format 2 only: open tests, run by Run after the samples; no points
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
- An expected output may be left out. Such a test shows "Output missing" in the editor, and the problem lists "Test 3 has no expected output." until you choose Generate outputs (see "Generating outputs"). Validation and judging wait until every test has one.
- An output without its input is refused.
- At most 16 MB per file.
- A test planned in `tests.yaml` needs no files: Generate inputs makes its input and Generate outputs its output. A pack must have at least one test file or one planned test.

## Statement

`statement.md` is Markdown, rendered as students will see it:

- Use `##` headings for sections such as Input, Output and Constraints. Heading levels are shifted to fit the page.
- Maths is TeX between dollars, drawn with KaTeX: `$1 \le n \le 10^5$` inline, and `$$ ... $$` on its own lines for a display formula. A dollar followed by a space, or a closing dollar followed by a digit, is plain text, so "costs $5 and $10" stays as written. Write `\$` for a dollar sign that would otherwise start maths.
- Raw HTML is shown as text, never rendered. Inline styles are removed, and links open in the same tab.
- Packs cannot carry image files yet, and the page's security policy blocks images from other sites.
- At most 64 KB. Put large data in tests, not in the statement.
- Tags are shown to students on their progress page, where results are grouped by tag, so write them as plain words a student would recognise ("two pointers", "prefix sums"), not internal shorthand.
- When an image is used (a later pack format may carry them), give it a description in its alt text, `![a 3 by 3 grid with the start in the top left](grid.png)`, so a student who cannot see it still has the problem; the editor's readiness panel lists images without one.

Statement, title, tags, difficulty and subtask titles can change at any time without affecting validation.

**During a live contest** (one that pins the problem and is between its start and its close plus the longest personal extension), a save that changes the problem needs a reason: the editor asks for one, and "Replace tests" takes it too. The reason is kept with the new revision. Students keep seeing the revision the contest pins until an organiser chooses "Update contest to revision N" on the console; see "Revisions" below.

## Samples, open tests and subtasks

Every test is exactly one of three kinds (D115):

| Kind | Who sees it | Rules |
|---|---|---|
| Sample (`samples`) | Everyone, with the statement | Test numbers, each once; no points. Run checks every sample; a Submit judges them first and, with "Stop on a failing sample", stops there. |
| Open test (`open`, pack format 2) | A student whose Run fails it: its input, the expected output and their output | Test numbers, each once; in no subtask; no points. Run checks them after the samples, in order, only while everything before passed, and stops at the first failure. Make them easier than the hidden set: they are a learning aid, not a second samples list. |
| Hidden test (in a subtask) | Nobody, during a contest | See the subtasks below. Only Submit judges them. |

| Item | Rules |
|---|---|
| `samples` | Test numbers, each once. Shown with the statement; no points. |
| `open` | Test numbers, each once, none of them a sample. Not shown in advance; revealed when they fail a Run. |
| `subtasks` | Up to 50. Each gets 1 to 1000 points and a `tests` range list. A student scores a subtask's points only by passing all of its tests. |
| Range list | Comma-separated test numbers and ranges: `"3-5"`, `"3-5, 8"`, `"7"`. Ranges run upwards, numbers are 1 to 999, and no test may appear twice. It is stored in its shortest form (`"3, 4, 5"` becomes `"3-5"`). |

Validation refuses to start while a test is in no subtask (and is neither a sample nor an open test), in two places, or a range names a test that does not exist. Points that do not add up to 100 are a warning, not an error. Validation runs the reference, the brute force and the validator over open tests like every other test, and they need expected outputs the same way.

**When hidden tests are shown.** A submission that never counts shows its first failing hidden test (input, expected output, the program's output, at most 4 KB each): practice after the window when "practice after the end" is on, every submission in an ungraded contest, and your own tries in the workspace. A submission that counts in a graded contest never shows one. Name subtasks by purpose ("Correctness", "Time complexity"), since their titles are shown.

## Revisions

Every save that changes a problem (its statement, title, tests, limits, checker, solutions, samples, open tests, subtasks or generator) makes a new numbered revision; a save that changes nothing makes none. Tags, slug, difficulty and the generator plan are bank details and make none. Generated inputs and outputs make revisions too, without an author.

- A contest pins one revision per problem when the problem is added. Students are served that revision's title, statement and samples, never the bank's working copy, so an edit in the bank changes nothing in a contest until you choose "Update contest to revision N" on the console.
- The update is accepted when the revision is the validated content of the problem. When only the statement or title changed, no rejudge is needed; students see "Statement updated 14:32. See what changed" with a line diff, and the console offers an announcement to post. When the tests, limits or checker changed, validate first, update, then rejudge.
- The editor lists the newest 50 revisions with who made each, when, the reason and the contests that pin it, and can show any revision or compare two.
- From v1.3.0 a revision older than 90 days that no contest pins or pinned, no submission was judged against and no student was served is deleted by the daily retention job; the problem's newest revision always stays. A contest's pins go 4 years after the contest, with its submissions (`docs/OPERATIONS.md`, "Retention").

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
| exit 7, or 50 and above | `_points`, partial scores | Wrong answer: partial points count as wrong until per-test scoring arrives in v2 |
| exit 3 | `_fail` | Internal error: "The checker reported a failure" |
| crash, time or memory limit, any other exit | | Internal error |

Use `_fail` when the expected answer itself is wrong, as above: during validation that fails the problem instead of blaming a contestant later.

Limits: compiling gets 30 s of CPU (testlib is a large header; a normal compile gets 10 s). Each run gets 10 s of CPU, 20 s of wall time, 256 MB and one process. The first 256 bytes of what it prints (standard output and error together) are kept as the message shown with the result.

## Generating outputs

Write the inputs and a correct reference solution; the judge writes the expected outputs.

| Button | What it does |
|---|---|
| Generate outputs | Shown while any test has no output. Runs the reference on those inputs only. |
| Regenerate all outputs | Runs the reference on every input and replaces every output. Asks to confirm first: "Every expected output is replaced by what the reference solution prints." |

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

## The AI wizard

"New problem with AI" on the problem bank asks a model to write a whole problem: statement, samples, up to three open tests, subtasks, a C++ reference, a Python brute force, a Python generator with its plan, a Python validator, the checker and limits. An owner or admin pastes their own provider key first: Anthropic, OpenAI or Google Gemini (`docs/OPERATIONS.md`, "AI wizard keys"). The server lists that provider's current models; choose one, or type the id of a model the list does not show. The key stays in the server's memory for 2 hours and is forgotten on sign-out. The wizard shows which provider and model wrote each draft.

The three providers get the same instructions and the same answer schema, and every answer passes the same checks, so a draft from any of them is held to the same rules. Models differ in how often their first answer passes: when one keeps failing a check, try another model before rewriting the brief.

### From a brief

Give a topic, a difficulty (easy, medium or hard), constraints and subtask ideas, each up to 2,000 characters. Concrete constraints ("n up to 2e5, values up to 1e9 in absolute value") give better generators and validators than adjectives ("large").

### From pasted text

Paste an existing problem, up to 20,000 characters. The model rewrites it in its own words as a standard input and output problem; the text goes to the model as data inside a delimited block, and instructions hidden in it are ignored.

Problem statements on LeetCode, Codeforces and similar sites are copyrighted. The rewrite keeps the idea, not the prose, but the result is still derived from someone else's work: use it in your own contests, and do not publish or redistribute such packs.

### What happens next

1. The answer is checked: every field, the sample count, subtask points adding up to 100, the planned tests, and the editor's own rules. An answer that fails is sent back once with the reasons; if the second one fails too, the wizard says "The model's answer could not be used." and lists them.
2. A draft problem is created with origin `ai` and the tag "ai draft". Its samples are tests 1 to k, its open tests the ones after them, then the generated tests. The tag stays until a person changes the statement, the reference or the brute force and saves. A save that changes none of them keeps the tag, and the change that ends it also withdraws the validation, so the problem must pass again as the person left it.
3. Generate inputs is queued at once, which chains Generate outputs and Validate (see "Tests from a generator"). The wizard page follows each step to the end ("Generating the tests", "Validating the draft", then "Validation passed" and Open the draft); the editor shows them too. Without a key or a model, the key form appears on the wizard itself, above the brief.
4. You read every part of the draft, fix what needs fixing, save it and validate it. The aim is a draft that needs at most one manual edit.

The model's code is never run on the server: it goes to the judge, in isolate, like any setter's code. Nothing AI-made reaches students before a person has reviewed and validated it: only validated problems can be added to a contest, and a problem still tagged "ai draft" is refused even when the chain validated it ("The problem for label B is an AI draft. Read it and save it in the editor before adding it to a contest.").

**Graded contests need a person's solution.** The tests of an AI problem come from the model's generator and their expected outputs from the model's reference, so validation cannot catch a wrong reference: they agree by construction. A graded contest therefore refuses an AI problem until a person has written a solution for it: an editor save that changes the reference or the brute force (write your own brute force; it is the independent check), followed by a validation that passes. An admin may instead grade it as written by giving a reason of at least 10 characters when adding it; the reason is kept in the contest's audit log, and the contest editor asks for it. Turning grading on for a contest that already holds such a problem is refused the same way.

One AI request per admin runs at a time, for at most 3 minutes. A refusal by the model, a cut-off answer, a rate limit or a provider outage is shown in words; nothing is created.

### Can a model solve this?

In the editor, "Can a model solve this?" sends the statement alone (no tests, no reference) to the same provider and asks for a C++ solution. The answer is judged on the problem's tests as your own submission, and the editor shows "A model solved this zero-shot: N of 100". A high score is a warning for graded contests: students with an AI assistant can likely solve it too. It runs only when you press it, and only on a problem that can be judged (every test has an output).


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

Generating outputs or inputs changes test files, so it unvalidates the problem too. The generator and the plan themselves are not in the hash (decision D53): editing the generator does not unvalidate the tests it already made. Generate inputs again to use the new generator.

On an AI draft, the first save that changes the statement withdraws the validation too (see "The AI wizard"), although the statement is not in the hash.

## Before a graded contest: the setter checklist

Validation proves the tests and the reference agree with each other. It cannot prove they are right. Before every graded contest, for every problem in it:

1. **Reference judged live.** Open the problem from the contest as an organiser (the workspace) and submit the reference solution on the real judge. It must get the full score on every subtask.
2. **A known-wrong solution judged live.** Submit at least one solution you know is wrong in a typical way (an `int` that should be `long long`, a missing edge case, a brute force that is too slow). It must lose points, on the subtask you expect. A wrong solution that scores full marks means the tests are too weak.
3. **Brute force cross-checked.** Validation passes with a brute force, and the report shows at least half the tests cross-checked. Look at which tests were not, and add small tests if the large ones carry subtasks of their own.
4. **AI problems.** For every problem whose origin is `ai`, write a solution of your own (a brute force is enough), save it, and validate: it must pass every test. Grading an AI problem as written, with a reason, is the exception, and the reason is in the audit log.
5. **Freeze.** In the contest window, import no packs, run no AI wizard and rejudge nothing unless a test is wrong. A rejudge is recorded in the audit log with who asked for it.
6. **Practice and ungraded contests show hidden tests.** In practice after the end, and in an ungraded contest, a failing submission shows the first failing hidden test (D115). Anyone who keeps submitting can read the hidden tests one by one, so a problem used that way is public: do not reuse it in a graded contest without new tests. The bank and the contest editor's picker mark such a problem "Hidden tests shown in practice", and publishing a graded contest that holds one names it in the confirm. Turning grading off once a contest has started asks first, for the same reason, and a problem whose hidden tests a published contest has shown stays in that contest (it cannot be removed), so the mark cannot be lost before a graded exam. Open tests are shown on any failing Run.

## Trying a problem as a student

"Try as a student" in the editor's title area, or "Try" on a row of the problem bank, opens the problem in the student workspace (`/admin/problems/{id}/try`): the statement with its samples, the code editor, Run on samples, a custom input, and Submit with the subtask strip filling in as tests are judged.

- Use it to check the statement reads well, the samples copy cleanly, and a wrong solution gets the verdict and score you expect.
- These submissions are yours and count in no contest. Your draft is kept per problem.
- The problem does not need to be validated, but it must be complete: tests in subtasks and every expected output present. Otherwise Run and Submit list what to fix.
- Submits are limited to one every 5 seconds, one waiting per problem; runs to one every 2 seconds.
- You see full figures (exact memory per test). Students will see coarser figures and no hidden test data (`docs/ARCHITECTURE.md`, "What students can see").

Students get the same workspace inside contests in M4.

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

Refused entries: absolute paths, `..` or `.` segments, backslashes, colons, links, devices and duplicate names. Hidden files and `__MACOSX` folders are skipped. When something is wrong the import lists every problem it found, so the pack can be fixed in one pass. A pack whose slug is already in the bank is refused; change `slug` in `problem.yaml`.

Only one pack import, test replacement or pack download runs at a time; a second one waits.

## The editor

| Step | How |
|---|---|
| Create | Problems, New problem: enter a title and a slug. The problem starts with a statement outline and a C++ reference template. |
| Import | Problems, Import pack: pick one or more `.zip` files. Each imported problem starts validating at once and the bank shows "Validating" until the result is in, so several packs need no visit to the editor. A pack the server refuses names its problems; a problem that cannot be validated yet says why in a message. Rows that are not validated offer Validate. |
| Edit | The sections follow what each needs: Statement and limits (with a preview that shows the statement and its samples as students see them), Solutions (reference and brute force), Tests (samples, subtasks, test files), Generator, then Judging (the checker in plain words, its allowed error, a custom checker, the validator). From 1280 px a column beside the form keeps the readiness panel and the preview in view. Removing the brute force, the validator or the generator can be undone until you save. Save changes. |
| Replace tests | Upload a zip of `NN.in` files, each with an optional `NN.out`, at its top level or in `tests/`. Every test is replaced; samples and subtask ranges stay as written, and the editor lists any that no longer fit. Single test files can be downloaded from the test list. |
| Generate outputs | Fills missing outputs from the reference solution; "Regenerate all outputs" replaces them all after a confirmation (see "Generating outputs"). |
| Generate inputs | Runs the generator for every planned test, then generates outputs and validates (see "Tests from a generator"). |
| New problem with AI | On the problem bank: the AI wizard writes a draft from a brief or from pasted text (see "The AI wizard"). |
| Can a model solve this? | Sends the statement to the AI provider and judges its answer (see "The AI wizard"). |
| Validate | Saves unsaved changes first, then queues the run. When it finishes, the sticky bar says how it went ("Validation passed: 6 of 6 tests, slowest 11 ms" or "Validation failed on test 5") with a link to the report. |
| Readiness | The panel beside the form lists what stops validation (a test in no subtask, a missing output), the warnings ("Before you validate"), statement images without a description, and whether the validation covers this version. An unsaved change to a judged field (limits, checker, solutions, validator, samples, subtask points and ranges) says that saving needs a new validation; a change to the title, statement, tags or subtask titles does not, except on an AI draft, whose statement counts. Once validated it offers Add to a contest. |
| Try as a student | Opens the problem in the student workspace (see "Trying a problem as a student"). |
| Download pack | The same layout as an import, ready to re-import or keep in version control. Sources are named `solution`, `brute`, `validator` and `generator` with their language's extension, the plan is written to `tests.yaml`, and tests are numbered with two digits (three from test 100). Outputs not generated yet are left out. |
| Archive problem | Asks to confirm, then hides the problem from the bank. Its tests and history are kept; "Show archived" lists it again. |

## Example packs

`problems/` in the repository holds three packs that pass validation. Zip a folder to import it.

| Pack | Shows |
|---|---|
| `a-plus-b` | `tokens` checker, C++ reference, Python brute force and validator, 2 samples, 2 subtasks |
| `average-speed` | `float` checker with `epsilon`, Java reference, Python brute force and validator, and one open test (pack format 2) |
| `pair-with-sum` | `custom` testlib checker accepting any valid pair, C++ reference, JavaScript brute force |
