# Exits non-zero when the input breaks the constraints. Strict about
# layout: n on the first line, then exactly n integers on the second.
import re
import sys

NUM = re.compile(r"-?(0|[1-9][0-9]*)")

lines = sys.stdin.read().split("\n")
if lines and lines[-1] == "":
    lines.pop()


def fail(msg):
    sys.stderr.write(msg + "\n")
    sys.exit(1)


if len(lines) != 2:
    fail("expected 2 lines, found %d" % len(lines))
if not NUM.fullmatch(lines[0]):
    fail("first line must be n")
n = int(lines[0])
if not 1 <= n <= 20000:
    fail("n out of range")
parts = lines[1].split(" ")
if len(parts) != n or not all(NUM.fullmatch(p) for p in parts):
    fail("second line must be n integers separated by single spaces")
if any(abs(int(p)) > 10**9 for p in parts):
    fail("a number is out of range")
