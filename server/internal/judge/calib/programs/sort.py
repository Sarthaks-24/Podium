# Reads n then n integers, sorts them, prints the minimum, median, maximum.
import sys

data = sys.stdin.buffer.read().split()
n = int(data[0])
a = sorted(map(int, data[1 : n + 1]))
print(a[0], a[n // 2], a[n - 1])
