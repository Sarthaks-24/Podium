# Tries every pair: right on small inputs, far too slow for the large ones
# in pure Python.
import sys

data = sys.stdin.read().split()
n, s = int(data[0]), int(data[1])
a = [int(x) for x in data[2:2 + n]]
for i in range(n):
    for j in range(i + 1, n):
        if a[i] + a[j] == s:
            print(i + 1, j + 1)
            sys.exit()
print(-1)
