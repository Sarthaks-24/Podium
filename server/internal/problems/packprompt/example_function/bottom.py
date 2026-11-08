data = sys.stdin.buffer.read().split()
n = int(data[0])
print(array_max([int(x) for x in data[1:1 + n]]))
