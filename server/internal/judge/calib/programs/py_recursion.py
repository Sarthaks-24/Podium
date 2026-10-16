# Hostile: lifts the recursion limit and recurses until the C stack or the
# memory limit gives out.
import sys

sys.setrecursionlimit(10**8)


def f(n):
    return f(n + 1) + 1


print(f(0))
