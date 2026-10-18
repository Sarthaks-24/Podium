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
checker.cpp         testlib checker (required when checker is custom; this name is fixed)
tests/01.in         test inputs and their expected outputs (outputs may be left out)
tests/01.out
tests/02.in
tests/02.out
...
```

Source files must sit at the top of the pack, with a name of up to 40 letters, digits, `-` or `_`, and an extension that sets the language: `.cpp` C++17, `.java` Java 21, `.py` Python 3, `.js` JavaScript. Any other file is ignored, and the import names it in a warning.

### problem.yaml

Parsed strictly: an unknown key is an error, so a misspelt key never silently falls back to a default. Comments are allowed.

```yaml
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
samples: [1, 2]                 # test numbers shown with the statement; they carry no points
subtasks:                       # at least one; scored all or nothing
  - points: 30                  # 1 to 1000; the subtasks should add up to 100
    title: Small n              # optional
    tests: "3-4"                # range list, see "Samples and subtasks"
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

## Statement

`statement.md` is Markdown, rendered as students will see it:

- Use `##` headings for sections such as Input, Output and Constraints. Heading levels are shifted to fit the page.
- Maths is TeX between dollars, drawn with KaTeX: `$1 \le n \le 10^5$` inline, and `$$ ... $$` on its own lines for a display formula. A dollar followed by a space, or a closing dollar followed by a digit, is plain text, so "costs $5 and $10" stays as written. Write `\$` for a dollar sign that would otherwise start maths.
- Raw HTML is shown as text, never rendered. Inline styles are removed, and links open in the same tab.
- Packs cannot carry image files yet, and the page's security policy blocks images from other sites.
- At most 64 KB. Put large data in tests, not in the statement.

Statement, title, tags, difficulty and subtask titles can change at any time without affecting validation.

## Samples and subtasks

Every test is either a sample or in exactly one subtask.

| Item | Rules |
|---|---|
| `samples` | Test numbers, each once. Shown with the statement; no points. |
| `subtasks` | Up to 50. Each gets 1 to 1000 points and a `tests` range list. A student scores a subtask's points only by passing all of its tests. |
| Range list | Comma-separated test numbers and ranges: `"3-5"`, `"3-5, 8"`, `"7"`. Ranges run upwards, numbers are 1 to 999, and no test may appear twice. It is stored in its shortest form (`"3, 4, 5"` becomes `"3-5"`). |

Validation refuses to start while a test is in no subtask, in two places, or a range names a test that does not exist. Points that do not add up to 100 are a warning, not an error.

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

## Validation

Validation runs the problem on the real judge, in isolate, exactly as a contest would. Choose Validate in the editor; the report updates every 2 seconds while the job waits and runs. Validation has the lowest priority, behind all contest work.

### What runs

Validation does not start while the structure is incomplete (see "Samples and subtasks") or any test has no expected output.

1. Compile the reference, brute force, validator and checker. If the reference or the checker does not compile, nothing else runs. A brute force or validator that does not compile is reported, the run goes on without it, and validation fails.
2. For each test, in order:
   - the validator reads the input on standard input. Exit 0 accepts it; any other exit rejects it, and the end of its standard error is shown as the reason.
   - the reference runs with the problem's limits, and its output goes through the checker.
   - the brute force runs with a longer limit, and its output goes through the checker. Running out of time is recorded as skipped, not as a failure: brute forces are expected to be slow on large tests.
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
- the reference's slowest test takes at most half of its time limit (see below).

Warnings never fail validation: the reference above a third of its limit, no brute force, no validator, a brute force that finished no test in time (so it checked nothing), a brute force crash or memory failure, and subtask points not adding up to 100.

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
| Brute force agreed on 6 of 6 tests | Out of the tests where the brute force did not run out of time; skipped tests are not counted. A disagreement fails validation; a crash is only a warning. |
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

Generating outputs changes test files, so it unvalidates the problem too.

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
| Import | Problems, Import pack: pick the `.zip`. The editor opens on the new problem, with any warnings. |
| Edit | Statement on the left with a live preview on the right; limits, checker, samples and subtasks; reference, brute force, validator and checker as a language and a source each (in a plain text field; the code editor is in the workspace). Save changes. |
| Replace tests | Upload a zip of `NN.in` files, each with an optional `NN.out`, at its top level or in `tests/`. Every test is replaced; samples and subtask ranges stay as written, and the editor lists any that no longer fit. Single test files can be downloaded from the test list. |
| Generate outputs | Fills missing outputs from the reference solution; "Regenerate all outputs" replaces them all after a confirmation (see "Generating outputs"). |
| Validate | Saves unsaved changes first, then queues the run. |
| Try as a student | Opens the problem in the student workspace (see "Trying a problem as a student"). |
| Download pack | The same layout as an import, ready to re-import or keep in version control. Sources are named `solution`, `brute` and `validator` with their language's extension, and tests are numbered with two digits (three from test 100). Outputs not generated yet are left out. |
| Archive problem | Asks to confirm, then hides the problem from the bank. Its tests and history are kept; "Show archived" lists it again. |

## Example packs

`problems/` in the repository holds three packs that pass validation. Zip a folder to import it.

| Pack | Shows |
|---|---|
| `a-plus-b` | `tokens` checker, C++ reference, Python brute force and validator, 2 samples, 2 subtasks |
| `average-speed` | `float` checker with `epsilon`, Java reference, Python brute force and validator |
| `pair-with-sum` | `custom` testlib checker accepting any valid pair, C++ reference, JavaScript brute force |
