import sys

data = sys.stdin.read().split()
t = int(data[0])
print("\n".join(str(int(data[1 + 2 * i]) + int(data[2 + 2 * i])) for i in range(t)))
