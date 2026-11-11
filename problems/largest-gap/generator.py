# argv: seed, then key=value options: n (count), maxv (largest absolute
# value) and shape (random, equal or spread). The same seed and options
# always print the same input.
import random
import sys

seed = int(sys.argv[1])
opts = dict(arg.split("=", 1) for arg in sys.argv[2:])
n = int(opts.get("n", "100"))
maxv = int(opts.get("maxv", "1000"))
shape = opts.get("shape", "random")
rng = random.Random(seed)
if shape == "equal":
    v = [rng.randint(-maxv, maxv)] * n
elif shape == "spread":
    step = (2 * maxv) // max(n - 1, 1)
    v = [-maxv + i * step for i in range(n)]
    rng.shuffle(v)
else:
    v = [rng.randint(-maxv, maxv) for _ in range(n)]
print(n)
print(" ".join(map(str, v)))
