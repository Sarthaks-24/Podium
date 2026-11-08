const data = require('fs').readFileSync(0, 'utf8').trim().split(/\s+/).map(Number);
console.log(String(arraySum(data.slice(1, 1 + data[0]))));
