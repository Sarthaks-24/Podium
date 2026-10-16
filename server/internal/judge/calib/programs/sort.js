// Reads n then n integers, sorts them, prints the minimum, median, maximum.
const data = require("node:fs").readFileSync(0, "latin1").split(/\s+/);
const n = Number(data[0]);
const a = new Int32Array(n);
for (let i = 0; i < n; i++) a[i] = Number(data[i + 1]);
a.sort();
console.log(a[0], a[n >> 1], a[n - 1]);
