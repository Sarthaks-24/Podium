# Slow on purpose: for every value, find the next larger one by scanning.
n = int(input())
v = list(map(int, input().split()))
best = 0
for x in v:
    bigger = [y for y in v if y > x]
    if bigger:
        best = max(best, min(bigger) - x)
print(best)
