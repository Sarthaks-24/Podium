// Hostile: starts 100 worker threads, more than the process limit allows.
const { Worker } = require("node:worker_threads");
const workers = [];
for (let i = 0; i < 100; i++) {
  workers.push(new Worker("setInterval(() => {}, 1000)", { eval: true }));
}
console.log("started", workers.length);
process.exit(0);
