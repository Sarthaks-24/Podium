from fractions import Fraction
import sys

data = list(map(int, sys.stdin.read().split()))
n = data[0]
distance = sum(data[1:1 + 2 * n:2])
hours = sum(data[2:2 + 2 * n:2])
print("%.9f" % float(Fraction(distance, hours)))
