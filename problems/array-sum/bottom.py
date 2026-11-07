data = sys.stdin.buffer.read().split()
n = int(data[0])
print(array_sum([int(x) for x in data[1:1 + n]]))
