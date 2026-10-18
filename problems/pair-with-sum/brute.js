// Tries every pair; fine for the first subtask, too slow for the second.
const data = require('fs').readFileSync(0, 'utf8').trim().split(/\s+/).map(Number);
const [n, s] = data;
const a = data.slice(2, 2 + n);
let answer = '-1';
search: for (let i = 0; i < n; i++) {
  for (let j = i + 1; j < n; j++) {
    if (a[i] + a[j] === s) {
      answer = `${i + 1} ${j + 1}`;
      break search;
    }
  }
}
console.log(answer);
