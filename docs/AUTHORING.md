# Writing problems

The problem editor, pack format, validation gate and AI assistant arrive in milestones M2 and M5; this guide grows with them. What is settled already:

## Input and output

Every problem reads standard input and writes standard output. Problems in the style of LeetCode (a function signature) are rewritten into this form.

## Limits and languages

The time limit you set applies to C++. Other languages get a multiple of it, and extra memory for their runtime:

| Language | Time | Memory |
|---|---|---|
| C++17 | 1.0 x | limit |
| Java 21 | 2.0 x | limit + 256 MB (heap capped at the limit) |
| Python 3 | 3.0 x | limit + 64 MB |
| JavaScript (Node 22) | 2.0 x | limit + 128 MB |

Guidelines that keep a 700-student contest moving on one VM:

- 6 to 10 hidden tests per problem, at most 2 maximum-size tests per subtask.
- Time limit 1 s for C++, and at least 3 times your C++ reference solution's time.
- Outputs under 1 MB.
- Python is about 5 times slower than C++ on heavy input (calibration: sorting a million integers took 141 ms in C++ and 697 ms in Python). If Python solutions should pass, write and time a Python reference too.

## Checkers

| Checker | Use when |
|---|---|
| `tokens` (default) | The answer is a fixed sequence of tokens; spacing and line breaks do not matter. |
| `exact` | Formatting matters. Windows line endings and trailing newlines are ignored. |
| `float` | The answer has real numbers. Accepts absolute or relative error up to 1e-6 by default. |
| `custom` | Many answers are correct. A testlib checker (arrives in M2). |
