# Exits non-zero when the input breaks the constraints. Strict about
# layout: one number per line for t, then exactly two per line.
import re
import sys

LIMIT = 10**18
NUM = re.compile(r"-?(0|[1-9][0-9]*)")

lines = sys.stdin.read().split("\n")
if lines and lines[-1] == "":
    lines.pop()


def fail(msg):
    sys.stderr.write(msg + "\n")
    sys.exit(1)


if not lines or not NUM.fullmatch(lines[0]):
    fail("first line must be t")
t = int(lines[0])
if not 1 <= t <= 1000:
    fail("t out of range")
if len(lines) != t + 1:
    fail("expected %d pair lines, found %d" % (t, len(lines) - 1))
for i, line in enumerate(lines[1:], start=2):
    parts = line.split(" ")
    if len(parts) != 2 or not all(NUM.fullmatch(p) for p in parts):
        fail("line %d must be two integers separated by one space" % i)
    if any(abs(int(p)) > LIMIT for p in parts):
        fail("line %d: number out of range" % i)
