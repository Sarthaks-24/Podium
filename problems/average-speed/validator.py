import sys

lines = sys.stdin.read().split("\n")
if lines and lines[-1] == "":
    lines.pop()


def fail(msg):
    sys.stderr.write(msg + "\n")
    sys.exit(1)


try:
    n = int(lines[0])
except (IndexError, ValueError):
    fail("first line must be n")
if not 1 <= n <= 10**5:
    fail("n out of range")
if len(lines) != n + 1:
    fail("expected %d segment lines" % n)
for i, line in enumerate(lines[1:], start=2):
    parts = line.split(" ")
    if len(parts) != 2 or not all(p.isdigit() and 1 <= int(p) <= 1000 for p in parts):
        fail("line %d must be two integers from 1 to 1000" % i)
