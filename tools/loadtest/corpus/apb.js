const data = require('fs').readFileSync(0, 'utf8').trim().split(/\s+/);
const t = Number(data[0]);
const out = [];
for (let i = 0; i < t; i++) out.push((BigInt(data[1 + 2 * i]) + BigInt(data[2 + 2 * i])).toString());
console.log(out.join('\n'));
