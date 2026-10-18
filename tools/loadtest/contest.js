// k6 load test for podium: one virtual user per student, through a whole
// contest, against a server in testing mode (sign-in from the form).
//
// Each student signs in, opens the contest, accepts the rules, holds an
// event stream (GET /api/events) for the whole session, opens problem
// workspaces, saves drafts every 10 to 20 s, polls the standings, sends
// integrity event batches every 30 s, presses Run, and submits at a
// configurable rate that rises to a spike at the end of their time. One
// more virtual user signs in as the owner and polls GET /api/admin/metrics
// every 5 s, through the end and until the judge queue has drained.
//
// Fixed mode: everyone starts within RAMP_MINUTES and the contest ends for
// everyone at MINUTES, so the spike hits together. Window mode: the
// contest window is MINUTES long, each student presses Start at a random
// time and has WINDOW_MINUTES, so the spike is spread out.
//
// See README.md in this folder for how to run it and read the results.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { setTimeout } from 'k6/timers';
import { Counter, Gauge, Trend } from 'k6/metrics';

function num(v, def) {
  const n = Number(v);
  return v === undefined || v === '' || Number.isNaN(n) ? def : n;
}

const BASE = (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
// The server's PODIUM_BASE_URL. Testing mode accepts the sign-in form only
// from that origin, and state-changing requests are checked against it.
const ORIGIN = __ENV.ORIGIN || 'http://localhost:5173';
const VUS = num(__ENV.VUS, 100);
const MODE = __ENV.MODE === 'window' ? 'window' : 'fixed';
const MINUTES = num(__ENV.MINUTES, 15);
const WINDOW_MINUTES = num(__ENV.WINDOW_MINUTES, 10);
const RAMP_MINUTES = num(__ENV.RAMP_MINUTES, 2);
// Aggregate rates across all students, per minute. The defaults scale
// PLAN.md section 4 (700 students: about 30 submissions a minute on
// average, 100 to 150 a minute in the last 15 minutes) to VUS.
const SUBMIT_PER_MIN = num(__ENV.SUBMIT_PER_MIN, (30 * VUS) / 700);
const SPIKE_PER_MIN = num(__ENV.SPIKE_PER_MIN, (120 * VUS) / 700);
const SPIKE_MINUTES = num(__ENV.SPIKE_MINUTES, 3);
const RUN_PER_MIN = num(__ENV.RUN_PER_MIN, SUBMIT_PER_MIN * 1.5);
// Share of submissions that send exactly the code last run on the samples.
const SAME_AS_RUN = num(__ENV.SAME_AS_RUN, 0.5);
const STANDINGS_EVERY_S = num(__ENV.STANDINGS_EVERY_S, 10);
const DRAFT_EVERY_S = num(__ENV.DRAFT_EVERY_S, 15);
const MIN_GAP_S = num(__ENV.MIN_GAP_S, 30);
const DRAIN_MAX_MINUTES = num(__ENV.DRAIN_MAX_MINUTES, 30);
const RESULTS = __ENV.RESULTS || 'results';
const SLUGS = (__ENV.PROBLEMS || 'a-plus-b,average-speed,pair-with-sum').split(',');
const LABELS = SLUGS.map((_, i) => String.fromCharCode(65 + i));

export const options = {
  scenarios: {
    students: {
      executor: 'per-vu-iterations',
      exec: 'student',
      vus: VUS,
      iterations: 1,
      maxDuration: `${Math.ceil(MINUTES + 3)}m`,
      gracefulStop: '30s',
    },
    monitor: {
      executor: 'per-vu-iterations',
      exec: 'monitor',
      vus: 1,
      iterations: 1,
      maxDuration: `${Math.ceil(MINUTES + DRAIN_MAX_MINUTES + 3)}m`,
    },
  },
  thresholds: Object.assign({
    'http_req_duration{kind:api}': ['p(95)<300'],
    'http_req_failed{kind:api}': ['rate<0.01'],
  // A threshold that always holds makes k6 keep each route's own figures
  // for the summary.
  }, Object.fromEntries(['signin', 'me', 'contests', 'contest', 'start', 'workspace', 'draft', 'standings', 'integrity', 'run', 'submit']
    .map((n) => [`http_req_duration{name:${n}}`, ['max>=0']]))),
  setupTimeout: '120s',
  discardResponseBodies: true,
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max', 'count'],
};

// 429 is a limit doing its job and is counted on its own; 403 is the
// window-mode board answering "closed until your window ends".
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 399 }, 403, 429));

const rateLimited = new Counter('rate_limited');
const sseOpened = new Counter('sse_opened');
const sseReconnects = new Counter('sse_reconnects');
const submitsAccepted = new Counter('submits_accepted');
const runsAccepted = new Counter('runs_accepted');
const apiErrors = new Counter('api_errors');
const queueDepth = new Trend('queue_depth');
const queueDepthNow = new Gauge('queue_depth_now');
const drainSeconds = new Gauge('drain_seconds');
const sseServer = new Trend('sse_connections_server');

// Programs students submit: the packs' own solutions and plausible wrong
// ones, per problem slug. open() reads at init, relative to this file.
const P = '../../problems/';
const SOURCES = {
  'a-plus-b': [
    { w: 35, lang: 'cpp17', src: open(P + 'a-plus-b/solution.cpp') },
    { w: 15, lang: 'python3', src: open(P + 'a-plus-b/brute.py') },
    // Passes the samples and the small subtask, overflows on the large one.
    { w: 20, lang: 'cpp17', src: '#include <cstdio>\nint main(){int t;scanf("%d",&t);while(t--){int a,b;scanf("%d %d",&a,&b);printf("%d\\n",a+b);}}\n' },
    { w: 20, lang: 'cpp17', src: '#include <cstdio>\nint main(){puts("0");}\n' },
    { w: 10, lang: 'cpp17', src: '#include <cstdio>\nint main( { return 0 }\n' },
  ],
  'average-speed': [
    { w: 30, lang: 'java21', src: open(P + 'average-speed/solution.java') },
    { w: 20, lang: 'python3', src: open(P + 'average-speed/brute.py') },
    // Integer division: right when the speed is whole, wrong otherwise.
    { w: 25, lang: 'cpp17', src: '#include <cstdio>\nint main(){int n;long long d=0,h=0,x,y;scanf("%d",&n);for(int i=0;i<n;i++){scanf("%lld %lld",&x,&y);d+=x;h+=y;}printf("%lld.000000000\\n",d/h);}\n' },
    { w: 20, lang: 'python3', src: 'print(0)\n' },
    { w: 5, lang: 'java21', src: 'public class Main { public static void main(String[] a) { int x = } }\n' },
  ],
  'pair-with-sum': [
    { w: 35, lang: 'cpp17', src: open(P + 'pair-with-sum/solution.cpp') },
    // Tries every pair: right on small inputs, too slow on large ones.
    { w: 20, lang: 'node22', src: open(P + 'pair-with-sum/brute.js') },
    { w: 25, lang: 'cpp17', src: '#include <cstdio>\nint main(){puts("-1");}\n' },
    { w: 15, lang: 'python3', src: 'n, s = map(int, input().split())\nprint(1, 2)\n' },
    { w: 5, lang: 'cpp17', src: 'int main() { return x; }\n' },
  ],
};

function pick(list) {
  let total = 0;
  for (const v of list) total += v.w;
  let r = Math.random() * total;
  for (const v of list) {
    r -= v.w;
    if (r < 0) return v;
  }
  return list[list.length - 1];
}

// Every student writes their own code: a unique comment defeats the judge's
// compile cache the way real, slightly different programs do.
function program(slug, who, n) {
  const v = pick(SOURCES[slug]);
  const mark = v.lang === 'python3' ? `\n# ${who} ${n}\n` : `\n// ${who} ${n}\n`;
  return { language: v.lang, source: v.src + mark };
}

function expMs(meanMs) {
  return -Math.log(1 - Math.random()) * meanMs;
}

function jitter(ms) {
  return ms * (0.5 + Math.random());
}

const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function params(name, extra) {
  return Object.assign({ tags: { kind: 'api', name }, headers: { 'Content-Type': 'application/json', Origin: ORIGIN } }, extra || {});
}

// call sends one API request and counts refusals. It never throws.
async function call(method, path, body, name, extra) {
  const res = await http.asyncRequest(method, BASE + path, body === null ? null : JSON.stringify(body), params(name, extra));
  if (res.status === 429) rateLimited.add(1, { name });
  else if (res.status >= 400 && res.status !== 403) apiErrors.add(1, { name, status: String(res.status) });
  return res;
}

function retryAfterMs(res, fallbackS) {
  const h = Number(res.headers['Retry-After']);
  return 1000 * (Number.isFinite(h) && h > 0 ? h : fallbackS);
}

async function signIn(email, role, name) {
  const res = await call('POST', '/api/auth/test-signin', { email, name, role, next: '/' }, 'signin');
  check(res, { 'signed in': (r) => r.status === 200 });
  return res.status === 200;
}

export function setup() {
  const jar = http.cookieJar();
  const res = http.post(`${BASE}/api/auth/test-signin`, JSON.stringify({ email: 'owner@podium.test', name: 'Load owner', role: 'owner', next: '/' }),
    { headers: { 'Content-Type': 'application/json', Origin: ORIGIN }, tags: { kind: 'setup' } });
  if (res.status !== 200) throw new Error(`owner sign-in failed: ${res.status}; is ${BASE} a testing-mode server?`);
  const list = http.get(`${BASE}/api/admin/problems`, { responseType: 'text', tags: { kind: 'setup' } });
  const bySlug = {};
  for (const p of list.json('problems')) bySlug[p.slug] = p.id;
  for (const s of SLUGS) if (!bySlug[s]) throw new Error(`problem ${s} is missing; run seed.py first`);

  const now = Date.now();
  const slug = `load-${MODE}-${Math.floor(now / 1000)}`;
  const edit = {
    slug, title: `Load test ${MODE} ${new Date(now).toISOString()}`,
    starts_at: now - 5000, ends_at: now + MINUTES * 60000, start_mode: MODE,
    freeze_minutes: MODE === 'fixed' ? Math.floor(MINUTES / 5) : 0,
    min_gap_s: MIN_GAP_S, max_per_problem: 15,
  };
  if (MODE === 'window') edit.duration_minutes = WINDOW_MINUTES;
  const json = { headers: { 'Content-Type': 'application/json', Origin: ORIGIN }, responseType: 'text', tags: { kind: 'setup' } };
  const created = http.post(`${BASE}/api/admin/contests`, JSON.stringify(edit), json);
  if (created.status !== 201) throw new Error(`create contest: ${created.status} ${created.body}`);
  const id = created.json('id');
  const problems = SLUGS.map((s, i) => ({ problem_id: bySlug[s], label: LABELS[i] }));
  const set = http.put(`${BASE}/api/admin/contests/${id}/problems`, JSON.stringify(problems), json);
  if (set.status !== 200) throw new Error(`set problems: ${set.status} ${set.body}`);
  const pub = http.put(`${BASE}/api/admin/contests/${id}`, JSON.stringify({ published: true }), json);
  if (pub.status !== 200) throw new Error(`publish: ${pub.status} ${pub.body}`);
  jar.clear(BASE);
  console.log(JSON.stringify({ event: 'contest', slug, id, mode: MODE, starts_at: edit.starts_at, ends_at: edit.ends_at, vus: VUS }));
  return { slug, id, start: now, end: edit.ends_at };
}

// session returns when this student arrives and leaves, in Unix ms.
function session(data) {
  if (MODE === 'fixed') {
    return { from: data.start + Math.random() * RAMP_MINUTES * 60000, to: data.end - 2000 };
  }
  const spread = Math.max(0, MINUTES - WINDOW_MINUTES - 0.5) * 60000;
  const from = data.start + Math.random() * spread;
  return { from, to: Math.min(data.end - 2000, from + WINDOW_MINUTES * 60000 - 5000) };
}

// openStream holds GET /api/events until `until`, like EventSource. A
// stream that ends early (server restart, dropped as too slow, 503) is a
// reconnect: counted, then opened again after the web app's back-off.
function openStream(st, until, attempt) {
  const left = until - Date.now();
  if (left < 5000) return;
  st.streams++;
  sseOpened.add(1);
  http.asyncRequest('GET', `${BASE}/api/events`, null, {
    timeout: `${Math.round(left + 3000)}ms`,
    responseType: 'none',
    tags: { kind: 'sse', name: 'stream' },
  }).then((res) => {
    st.streams--;
    if (Date.now() < until - 1000) {
      sseReconnects.add(1, { status: String(res.status) });
      const backoff = Math.min(30000, 1000 * 2 ** attempt) * (0.5 + Math.random() / 2);
      st.streams++;
      setTimeout(() => {
        st.streams--;
        openStream(st, until, attempt + 1);
      }, backoff);
    }
  });
}

export async function student(data) {
  const n = exec.scenario.iterationInTest + 1;
  const who = `student${String(n).padStart(4, '0')}`;
  const { from, to } = session(data);
  await pause(Math.max(0, from - Date.now()));
  if (!(await signIn(`${who}@load.test`, 'student', `Load student ${n}`))) return;
  const c = `/api/contests/${data.slug}`;
  await call('GET', '/api/me', null, 'me');
  await call('GET', '/api/contests', null, 'contests');
  await call('GET', c, null, 'contest');
  const started = await call('POST', `${c}/start`, { accept_honour: true }, 'start');
  check(started, { 'contest started': (r) => r.status === 200 });

  const st = { streams: 0 };
  openStream(st, to, 0);

  const perVU = (rate) => (rate > 0 ? (60000 * VUS) / rate : Infinity);
  const spikeAt = to - SPIKE_MINUTES * 60000;
  let spiking = false;
  let now = Date.now();
  let label = 0;
  let submits = 0;
  let code = program(SLUGS[0], who, 0);
  let ran = null;
  const next = {
    workspace: now,
    draft: now + jitter(DRAFT_EVERY_S * 1000),
    standings: now + expMs(STANDINGS_EVERY_S * 1000),
    events: now + jitter(30000),
    contest: now + expMs(120000),
    run: now + expMs(perVU(RUN_PER_MIN)),
    submit: now + expMs(perVU(SUBMIT_PER_MIN)),
  };
  while ((now = Date.now()) < to) {
    if (!spiking && now >= spikeAt) {
      // Memoryless: redraw the next submit and run at the spike rate.
      spiking = true;
      next.submit = now + expMs(perVU(SPIKE_PER_MIN));
      next.run = now + expMs(perVU((RUN_PER_MIN * SPIKE_PER_MIN) / Math.max(SUBMIT_PER_MIN, 1e-9)));
    }
    if (now >= next.workspace) {
      // Students move on through the problems as their time passes.
      const progress = (now - from) / Math.max(1, to - from);
      // Clamped: a timer may wake a few milliseconds before `from`.
      label = Math.max(0, Math.min(SLUGS.length - 1, Math.floor(progress * SLUGS.length + Math.random() * 0.8)));
      code = program(SLUGS[label], who, submits);
      await call('GET', `${c}/problems/${LABELS[label]}`, null, 'workspace');
      next.workspace = now + expMs(240000);
    }
    if (now >= next.draft) {
      code.source += ' ';
      await call('PUT', `${c}/problems/${LABELS[label]}/draft`, code, 'draft');
      next.draft = now + jitter(DRAFT_EVERY_S * 1000);
    }
    if (now >= next.standings) {
      await call('GET', `${c}/standings`, null, 'standings');
      next.standings = now + expMs(STANDINGS_EVERY_S * 1000);
    }
    if (now >= next.events) {
      await call('POST', `${c}/problems/${LABELS[label]}/events`,
        { events: [{ kind: 'blur', at: now - 4000 }, { kind: 'focus', at: now - 1000 }] }, 'integrity');
      next.events = now + 30000;
    }
    if (now >= next.contest) {
      await call('GET', c, null, 'contest');
      next.contest = now + expMs(120000);
    }
    if (now >= next.run) {
      ran = { language: code.language, source: code.source, label };
      const res = await call('POST', `${c}/problems/${LABELS[label]}/run`, { language: code.language, source: code.source }, 'run');
      if (res.status === 202) runsAccepted.add(1);
      next.run = res.status === 429 ? now + retryAfterMs(res, 5) + jitter(2000)
        : now + expMs(perVU(spiking ? (RUN_PER_MIN * SPIKE_PER_MIN) / Math.max(SUBMIT_PER_MIN, 1e-9) : RUN_PER_MIN));
    }
    if (now >= next.submit) {
      submits++;
      // Often the code just run on the samples is what gets submitted,
      // which the judge's compile cache turns into one compile, not two.
      if (ran && ran.label === label && Math.random() < SAME_AS_RUN) {
        code = { language: ran.language, source: ran.source };
      } else {
        code = program(SLUGS[label], who, submits);
      }
      ran = null;
      const res = await call('POST', `${c}/problems/${LABELS[label]}/submit`, {
        language: code.language, source: code.source, client_key: `${who}-${submits}`,
        typed_chars: 200 + Math.floor(Math.random() * 800), pasted_chars: 0, focus_losses: 1,
      }, 'submit');
      if (res.status === 202 || res.status === 200) {
        submitsAccepted.add(1);
        next.submit = now + expMs(perVU(spiking ? SPIKE_PER_MIN : SUBMIT_PER_MIN));
      } else if (res.status === 429) {
        // The gap, or the last submission still being judged: wait and
        // press Submit again, as a student would.
        next.submit = now + retryAfterMs(res, 20) + jitter(3000);
      } else {
        next.submit = now + expMs(perVU(spiking ? SPIKE_PER_MIN : SUBMIT_PER_MIN));
      }
    }
    await pause(jitter(1000));
  }
  // The stream ends by itself at `to`; wait for it so the VU does not
  // count as stopped early.
  while (st.streams > 0 && Date.now() < to + 10000) await pause(500);
}

export async function monitor(data) {
  await signIn('owner@podium.test', 'owner', 'Load owner');
  // Window mode: nobody submits after the close either.
  const end = data.end;
  let drainedPolls = 0;
  let peak = 0;
  for (;;) {
    const now = Date.now();
    const res = await http.asyncRequest('GET', `${BASE}/api/admin/metrics`, null, { responseType: 'text', tags: { kind: 'monitor', name: 'metrics' } });
    if (res.status === 200) {
      const m = res.json();
      const q = m.queue;
      const depth = q.queued + q.leased;
      peak = Math.max(peak, q.queued);
      queueDepth.add(q.queued);
      queueDepthNow.add(q.queued);
      sseServer.add(m.events.connections);
      console.log(JSON.stringify({
        event: 'metrics', t: now, phase: now < end ? 'contest' : 'drain',
        queued: q.queued, leased: q.leased, submit_queued: q.kinds.submit ? q.kinds.submit.queued : 0,
        run_queued: q.kinds.run ? q.kinds.run.queued : 0, oldest_queued_s: q.oldest_queued_s,
        finished_last_minute: Object.values(q.kinds).reduce((a, k) => a + k.finished_last_minute, 0),
        submit_wait_p95_ms: q.submit_wait_p95_ms, submit_total_p95_ms: q.submit_total_p95_ms,
        failed_last_5_min: q.failed_last_5_min, sse: m.events.connections, db_in_use: m.db.in_use,
        db_waits: m.db.waits, db_wait_ms: m.db.wait_ms, heap_mb: m.process.heap_mb, sys_mb: m.process.sys_mb,
        goroutines: m.process.goroutines,
      }));
      if (now >= end) {
        drainedPolls = depth === 0 ? drainedPolls + 1 : 0;
        if (drainedPolls >= 2) {
          const s = Math.max(0, (now - end) / 1000);
          drainSeconds.add(s);
          console.log(JSON.stringify({ event: 'drained', t: now, drain_s: s, peak_queued: peak }));
          return;
        }
      }
    } else {
      console.log(JSON.stringify({ event: 'metrics_error', t: now, status: res.status }));
    }
    if (now > end + DRAIN_MAX_MINUTES * 60000) {
      console.log(JSON.stringify({ event: 'not_drained', t: now, peak_queued: peak }));
      return;
    }
    await pause(5000);
  }
}

function metric(data, name, stat) {
  const m = data.metrics[name];
  if (!m) return 0;
  return m.values[stat] !== undefined ? m.values[stat] : 0;
}

export function handleSummary(data) {
  const f = (v) => (typeof v === 'number' ? v.toFixed(1) : String(v));
  const api = 'http_req_duration{kind:api}';
  const lines = [
    `podium load test: ${MODE} mode, ${VUS} students, ${MINUTES} min`,
    `API latency (ms): p50 ${f(metric(data, api, 'med'))}, p95 ${f(metric(data, api, 'p(95)'))}, p99 ${f(metric(data, api, 'p(99)'))}, max ${f(metric(data, api, 'max'))}, requests ${metric(data, api, 'count')}`,
    `API failures: ${f(100 * metric(data, 'http_req_failed{kind:api}', 'rate'))} %, errors ${metric(data, 'api_errors', 'count')}, 429 ${metric(data, 'rate_limited', 'count')}`,
    `Event streams: opened ${metric(data, 'sse_opened', 'count')}, reconnects ${metric(data, 'sse_reconnects', 'count')}, server peak ${metric(data, 'sse_connections_server', 'max')}`,
    `Submissions accepted ${metric(data, 'submits_accepted', 'count')}, runs accepted ${metric(data, 'runs_accepted', 'count')}`,
    `Queue: peak ${metric(data, 'queue_depth', 'max')} queued, drained ${f(metric(data, 'drain_seconds', 'value'))} s after the end`,
  ];
  for (const [name, t] of Object.entries(data.metrics)) {
    if (name.startsWith('http_req_duration{name:')) {
      lines.push(`  ${name.slice(23, -1)}: p95 ${f(t.values['p(95)'])} ms, count ${t.values.count}`);
    }
  }
  return {
    stdout: lines.join('\n') + '\n',
    [`${RESULTS}/summary-${MODE}-${VUS}.json`]: JSON.stringify(data, null, 1),
  };
}
