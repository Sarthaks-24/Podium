// Fixtures and the API mock of integrity.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type Page, type Route } from '@playwright/test';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-integrity/`;
}
export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;
export const MIN = 60_000;

export type Json = Record<string, unknown>;

export const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };
export const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'organiser', theme: '', verdicts: 'standard' };

export const USERS: Record<string, { name: string; email: string }> = {
  '7': { name: 'Asha Rao', email: 'asha@college.edu' },
  '8': { name: 'Ravi Kumar', email: 'ravi@college.edu' },
  '9': { name: 'Zoë Fernandes', email: 'zoe@college.edu' },
  '10': { name: 'Meera Iyer', email: 'meera@college.edu' },
  '11': { name: 'Kabir Shah', email: 'kabir@college.edu' },
  '12': { name: 'Neha Gupta', email: 'neha@college.edu' },
  '13': { name: 'Rohan Das', email: 'rohan@college.edu' },
  '14': { name: 'Sara Thomas', email: 'sara@college.edu' },
};
export const PROBLEMS = { '11': { label: 'A', title: 'A plus B' }, '12': { label: 'B', title: 'Interval covering' } };

export const SOURCE_A = [
  '#include <bits/stdc++.h>',
  'using namespace std;',
  '// अंतराल: sort by the right end 🙂',
  'int main() {',
  '    int n; cin >> n;',
  '    vector<pair<int,int>> v(n);',
  '    for (auto &p : v) cin >> p.second >> p.first;',
  '    sort(v.begin(), v.end());',
  '    int last = INT_MIN, count = 0;',
  '    for (auto &p : v) if (p.second > last) { last = p.first; count++; }',
  '    cout << count << "\\n";',
  '}',
  '',
].join('\n');

export const SOURCE_B = [
  '#include <bits/stdc++.h>',
  'using namespace std;',
  '/* greedy, très simple */',
  'int main() {',
  '    int m; cin >> m;',
  '    vector<pair<int,int>> seg(m);',
  '    for (auto &s : seg) cin >> s.second >> s.first;',
  '    sort(seg.begin(), seg.end());',
  '    int end = INT_MIN, ans = 0;',
  '    for (auto &s : seg) if (s.second > end) { end = s.first; ans++; }',
  '    cout << ans << "\\n";',
  '}',
  '',
].join('\n');

export const utf8 = (s: string) => new TextEncoder().encode(s).length;

/** Byte offsets of `from` up to the end of `to` in `source`. */
export function span(source: string, from: string, to: string): { start: number; end: number } {
  const i = source.indexOf(from);
  const j = source.indexOf(to, i) + to.length;
  return { start: utf8(source.slice(0, i)), end: utf8(source.slice(0, j)) };
}

export const SPANS_A = [span(SOURCE_A, 'vector<pair', 'p.first;'), span(SOURCE_A, 'sort(v', 'count++; }')];
export const SPANS_B = [span(SOURCE_B, 'vector<pair', 's.first;'), span(SOURCE_B, 'sort(seg', 'ans++; }')];

export function signals(): Json[] {
  return [
    { id: 31, problem_id: 12, kind: 'similar', strength: 'strong', score: 0.94, user_a: 7, user_b: 8, submission_a: 301, submission_b: 302, evidence: { shared: 41, fingerprints: 44, tokens: 212, spans_a: SPANS_A, spans_b: SPANS_B, language_a: 'cpp17', language_b: 'cpp17' } },
    { id: 34, problem_id: 12, kind: 'hardcoded', strength: 'strong', score: 2, user_a: 12, submission_a: 310, evidence: { tests: [7, 9] } },
    { id: 32, problem_id: 12, kind: 'similar', strength: 'moderate', score: 0.78, user_a: 8, user_b: 9, submission_a: 302, submission_b: 303, evidence: { shared: 30, spans_a: [], spans_b: [], language_a: 'cpp17', language_b: 'java21' } },
    { id: 33, problem_id: 11, kind: 'wrong_output', strength: 'moderate', score: 3, user_a: 10, user_b: 11, evidence: { tests: [3, 5, 7] } },
    { id: 35, problem_id: 11, kind: 'probing', strength: 'moderate', score: 8, user_a: 13, evidence: { distinct: 8, attempts: 14 } },
    { id: 36, problem_id: 11, kind: 'similar', strength: 'weak', score: 0.62, user_a: 10, user_b: 14, submission_a: 320, submission_b: 321, evidence: { shared: 12, spans_a: [], spans_b: [], language_a: 'python3', language_b: 'python3' } },
    // Pilot P11: two short programs that match completely, weak however high the score.
    { id: 37, problem_id: 11, kind: 'similar', strength: 'weak', score: 1, user_a: 11, user_b: 12, submission_a: 322, submission_b: 323, evidence: { shared: 15, fingerprints: 15, tokens: 48, spans_a: [], spans_b: [], language_a: 'python3', language_b: 'python3' } },
  ];
}

export interface Options {
  who?: 'student' | 'organiser';
  /** The contest is still live. */
  live?: boolean;
  /** The contest was never checked before the test. */
  unchecked?: boolean;
  /** The student's case was decided. */
  decided?: boolean;
  /** The student has no cases. */
  noCases?: boolean;
  /** A re-run after the case opened: live signal 31 now names other students. */
  rerun?: boolean;
  /**
   * v1.4.0 case page: both students replied and Priya proposed a zero for
   * B, which the viewer (Arjun, who opened nothing) may confirm; or the
   * case closed with that outcome and Asha appealed it.
   */
  proposed?: boolean;
  appealed?: boolean;
  /** The viewer took part in the decision, so they may not rule on the appeal. */
  involved?: boolean;
  /**
   * Both students replied and nothing is proposed yet, but Priya proposes
   * first: the viewer's proposal answers 409 proposal_pending (C2-025).
   */
  raceProposal?: boolean;
  /** The contest had full screen on and counted focus losses (V3-25): the page lists what was recorded. */
  exam?: boolean;
}

export type CaseUserJson = Json & { user_id: number; shared_at: number | null; asked_at: number | null; respond_by: number | null; explanations: { text: string; created_at: number }[] };

export const DAY = 86_400_000;

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

export function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

export async function mockApi(page: Page, options: Options = {}) {
  const now = Date.now();
  const endsAt = options.live ? now + 40 * MIN : now - 2 * 3_600_000;
  const state = {
    who: options.who ?? 'organiser',
    run: options.unchecked ? null : ({ state: 'done' } as { state: string } | null),
    runGets: 0,
    runPosts: 0,
    decisionPuts: [] as Json[],
    decisions: [
      { id: 2, contest_id: 4, user_id: 10, name: 'Meera Iyer', email: 'meera@college.edu', decision: 'warning',
        reason: 'Same wrong output as Kabir on A; both said they used the same textbook.', decided_by: 'Priya Nair', decided_at: now - 9 * MIN },
    ] as (Json & { user_id: number })[],
    caseBodies: [] as Json[],
    /** The student opened case 3 (POST .../opened). */
    opened: false,
    puts: [] as Json[],
    explanations: [] as Json[],
    kase: null as Json | null,
    livePairGets: 0,
    casePairGets: 0,
    proposals: [] as Json[],
    confirms: [] as Json[],
    rulings: [] as Json[],
    /** A student in the case saves an explanation (a new version). */
    explain(userId: number, text: string) {
      const k = (state.kase ??= baseCase());
      const at = Date.now();
      k.users = (k.users as CaseUserJson[]).map((u) =>
        u.user_id === userId ? { ...u, explanation: text, explained_at: at, explanations: [...u.explanations, { text, created_at: at }] } : u,
      );
    },
  };

  // After a re-run, live signal 31 is another pair; the case keeps its copy.
  const liveSignals = () => (options.rerun ? signals().map((s) => (s.id === 31 ? { ...s, user_a: 13, user_b: 14, submission_a: 330, submission_b: 331 } : s)) : signals());

  const run = () => ({
    id: 5,
    state: state.run?.state ?? 'done',
    started_at: now - 3 * MIN,
    finished_at: state.run?.state === 'running' ? null : now - 2 * MIN,
    stats: { students: 30, submissions: 84, problems: 2, signals: { similar: 4, wrong_output: 1, hardcoded: 1, probing: 1 }, clusters: 3 },
    signals: state.run?.state === 'running' ? [] : liveSignals(),
    users: USERS,
    problems: PROBLEMS,
    server_now: Date.now(),
  });

  const contest = () => ({
    id: 4,
    slug: 'week-6',
    title: 'Week 6 contest',
    starts_at: endsAt - 2 * 3_600_000,
    ends_at: endsAt,
    close_at: endsAt,
    state: options.live ? 'live' : 'ended',
    published: true,
    problems: [],
    admins: [],
    extensions: [],
    full_screen: options.exam ?? false,
    count_focus: options.exam ?? false,
    block_paste: false,
    block_copy: false,
    server_now: Date.now(),
  });

  const baseCase = (): Json => ({
    id: 3,
    contest_id: 4,
    state: 'open',
    title: 'Similar code on B: Asha Rao and Ravi Kumar',
    evidence: [signals()[0]],
    created_at: now - 20 * MIN,
    updated_at: now - 20 * MIN,
    users: [
      { user_id: 7, name: 'Asha Rao', shared_at: null, asked_at: null, respond_by: null, explanation: '', explained_at: null, explanations: [] },
      { user_id: 8, name: 'Ravi Kumar', shared_at: null, asked_at: null, respond_by: null, explanation: '', explained_at: null, explanations: [] },
    ],
    decisions: [{ by: 5, by_name: 'Priya Nair', state: 'open', note: 'Both programs share the whole greedy loop, renamed.', created_at: now - 20 * MIN, explanations_seen: {} }],
    source: 'signals',
    submission_id: null,
    created_by: 5,
    ready_at: null,
    decide_by: null,
    overdue: false,
    policy: null,
    proposals: [],
    appeals: [],
    may_confirm: false,
    may_rule: false,
    sole_organiser: false,
    server_now: Date.now(),
  });

  // Shared eight days ago; both students replied; ready since then.
  const sharedAt = now - 8 * DAY;
  const repliedUsers = () => [
    { user_id: 7, name: 'Asha Rao', shared_at: sharedAt, asked_at: sharedAt, respond_by: sharedAt + 7 * DAY, notice_available_at: sharedAt, first_opened_at: sharedAt + 3_600_000, prior_outcomes: 1, explanation: 'I wrote it in the lab from the lecture notes.', explained_at: sharedAt + DAY, explanations: [{ text: 'I wrote it in the lab from the lecture notes.', created_at: sharedAt + DAY }] },
    { user_id: 8, name: 'Ravi Kumar', shared_at: sharedAt, asked_at: sharedAt, respond_by: sharedAt + 7 * DAY, notice_available_at: sharedAt, first_opened_at: sharedAt + 7_200_000, prior_outcomes: 0, explanation: 'A friend shared notes with me.', explained_at: sharedAt + DAY, explanations: [{ text: 'A friend shared notes with me.', created_at: sharedAt + DAY }] },
  ];
  const zeroProposal = { id: 4, by: 5, by_name: 'Priya Nair', outcome: 'zero_problem', problem_label: 'B', user_ids: [7], note: 'Asha copied the loop; Ravi wrote his own and shared his notes.', reason: 'Your program for problem B shares its whole loop with another submission.', created_at: now - 2 * DAY, state: 'pending' };
  if (options.proposed) {
    state.kase = {
      ...baseCase(),
      state: 'awaiting_student',
      users: repliedUsers(),
      ready_at: sharedAt + DAY,
      decide_by: sharedAt + 15 * DAY,
      policy: 'referral_suggested',
      proposals: [zeroProposal],
      may_confirm: true,
    };
  }
  if (options.raceProposal) {
    state.kase = {
      ...baseCase(),
      state: 'awaiting_student',
      users: repliedUsers(),
      ready_at: sharedAt + DAY,
      decide_by: sharedAt + 15 * DAY,
      policy: 'referral_suggested',
      proposals: [],
      may_confirm: false,
    };
  }
  if (options.appealed) {
    state.kase = {
      ...baseCase(),
      state: 'actioned',
      users: repliedUsers(),
      ready_at: sharedAt + DAY,
      decide_by: sharedAt + 15 * DAY,
      proposals: [{ ...zeroProposal, state: 'confirmed' }],
      decisions: [
        ...(baseCase().decisions as Json[]),
        { by: 3, by_name: 'Ravi K', state: 'actioned', note: 'Confirmed: the loop is copied line for line.', created_at: now - DAY, outcome: 'zero_problem', outcome_problem_label: 'B', outcome_users: [7], proposal_id: 4, sole_organiser: '' },
      ],
      appeals: [{ id: 66, case_id: 3, adjustment_id: null, user_id: 7, text: 'The loop is the one from the lecture slides, which everyone was given.', created_at: now - 3_600_000, decide_by: now + 14 * DAY, overdue: false, state: 'open', ruling: null, user: { name: 'Asha Rao', email: 'asha@college.edu' } }],
      may_rule: !options.involved,
    };
  }

  const studentCase = (): Json => ({
    id: 3,
    contest_id: 4,
    state: options.decided ? 'cleared' : 'awaiting_student',
    title: 'Similar code on B',
    evidence: [
      {
        problem_id: 12,
        kind: 'similar',
        text: "Your submission shares long passages with another student's submission. The shared parts are highlighted.",
        submission_id: 301,
        source: SOURCE_A,
        spans: SPANS_A,
      },
    ],
    explanation: options.decided ? 'I wrote it in the lab from the lecture notes.' : '',
    shared_at: now - 10 * MIN,
    decided: options.decided ?? false,
    // The 21-day cap until the first open, then 7 days from it (deadlines.go).
    ...(options.decided ? {} : { respond_by: state.opened ? Date.UTC(2026, 9, 28, 8, 45) : Date.UTC(2026, 10, 11, 8, 44) }),
  });

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      const me = state.who === 'organiser' ? ADMIN : STUDENT;

      if (path === '/api/me') return json(route, 200, me);
      if (path === '/api/auth/config') return json(route, 200, { mode: 'production', allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' });
      if (path === '/api/events') return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
      if (path === '/api/contests') return json(route, 200, { live: [], upcoming: [], past: [{ slug: 'week-6', title: 'Week 6 contest', starts_at: endsAt - 2 * 3_600_000, ends_at: endsAt, problem_count: 2, state: 'ended', my_score: 140, max_score: 200, my_window: null }], server_now: Date.now() });

      // Student routes.
      if (path === '/api/me/standing' && method === 'GET') return json(route, 200, { band: 'good', band_name: 'Good standing', lines: [], server_now: Date.now() });
      if (path === '/api/integrity/cases/3/opened' && method === 'POST') {
        state.opened = true;
        return json(route, 204);
      }
      if (path === '/api/integrity/cases' && method === 'GET') {
        return json(route, 200, {
          cases: options.noCases || me.role !== 'student' ? [] : [studentCase()],
          contests: { '4': { slug: 'week-6', title: 'Week 6 contest' } },
          problems: PROBLEMS,
          server_now: Date.now(),
        });
      }
      if (path === '/api/integrity/cases/3/explanation' && method === 'PUT') {
        if (options.decided) return apiError(route, 404, 'not_found', 'This case was decided, so the explanation can no longer change.');
        state.explanations.push(req.postDataJSON() as Json);
        return json(route, 204);
      }

      // Organiser routes.
      if (path.startsWith('/api/admin/') && me.role === 'student') return apiError(route, 403, 'forbidden', 'You do not have access to this. Ask an owner if you need it.');
      if (path === '/api/admin/contests/4' && method === 'GET') return json(route, 200, contest());
      if (path === '/api/admin/contests/4/integrity' && method === 'GET') {
        state.runGets++;
        if (!state.run) return apiError(route, 404, 'not_found', 'This contest has not been checked yet.');
        const body = run();
        // A running check is done on the next look.
        if (state.run.state === 'running') state.run = { state: 'done' };
        return json(route, 200, body);
      }
      if (path === '/api/admin/contests/4/integrity/run' && method === 'POST') {
        state.runPosts++;
        if (options.live) return apiError(route, 409, 'contest_live', 'Check integrity after the contest ends for everyone.');
        state.run = { state: 'running' };
        return json(route, 202, { run_id: 6, server_now: Date.now() });
      }
      if (path === '/api/admin/integrity/cases/3/evidence/0/pair' && method === 'GET') {
        state.casePairGets++;
        return json(route, 200, {
          a: { submission_id: 301, user_id: 7, language: 'cpp17', source: SOURCE_A, created_at: now - 2 * 3_600_000 - 50 * MIN, spans: SPANS_A },
          b: { submission_id: 302, user_id: 8, language: 'cpp17', source: SOURCE_B, created_at: now - 2 * 3_600_000 - 41 * MIN, spans: SPANS_B },
          signal: signals()[0],
          server_now: Date.now(),
        });
      }
      const pair = path.match(/^\/api\/admin\/contests\/4\/integrity\/signals\/(\d+)\/pair$/);
      if (pair) {
        state.livePairGets++;
        if (pair[1] !== '31') return apiError(route, 404, 'not_found', 'Only similar code has a pair.');
        if (options.rerun) {
          return json(route, 200, {
            a: { submission_id: 330, user_id: 13, language: 'cpp17', source: 'int main() { return 13; }\n', created_at: now - 3_600_000, spans: [] },
            b: { submission_id: 331, user_id: 14, language: 'cpp17', source: 'int main() { return 14; }\n', created_at: now - 3_600_000, spans: [] },
            server_now: Date.now(),
          });
        }
        return json(route, 200, {
          a: { submission_id: 301, user_id: 7, language: 'cpp17', source: SOURCE_A, created_at: now - 2 * 3_600_000 - 50 * MIN, spans: SPANS_A },
          b: { submission_id: 302, user_id: 8, language: 'cpp17', source: SOURCE_B, created_at: now - 2 * 3_600_000 - 41 * MIN, spans: SPANS_B },
          server_now: Date.now(),
        });
      }
      const timeline = path.match(/^\/api\/admin\/contests\/4\/integrity\/timeline\/(\d+)$/);
      if (timeline) {
        const start = endsAt - 2 * 3_600_000;
        const moments =
          timeline[1] === '7'
            ? [
                { at: start + 12 * MIN, kind: 'submission', problem_id: 11, text: 'cpp17 AC 100 of 100. Typed 212, pasted 0 characters, left the page 0 times.' },
                { at: start + 69 * MIN, kind: 'paste', problem_id: 12, text: 'Pasted 431 characters.' },
                { at: start + 70 * MIN, kind: 'submission', problem_id: 12, text: 'cpp17 AC 100 of 100. Typed 18, pasted 431 characters, left the page 2 times.' },
                { at: start + 71 * MIN, kind: 'shared_device', text: 'Same browser and network as Ravi Kumar.' },
              ]
            : [
                { at: start + 40 * MIN, kind: 'blur', problem_id: 12, text: 'Left the page.' },
                { at: start + 44 * MIN, kind: 'focus', problem_id: 12, text: 'Came back to the page.' },
                { at: start + 79 * MIN, kind: 'submission', problem_id: 12, text: 'cpp17 AC 100 of 100. Typed 540, pasted 0 characters, left the page 1 times.' },
              ];
        return json(route, 200, { moments, server_now: Date.now() });
      }
      if (path === '/api/admin/contests/4/integrity/counts' && method === 'GET') {
        return json(route, 200, {
          counts: [
            { user_id: 7, name: 'Asha Rao', pastes: 2, pasted_chars: 431, focus_losses: 3, fullscreen_exits: 2 },
            { user_id: 8, name: 'Ravi Kumar', pastes: 0, pasted_chars: 0, focus_losses: 1, fullscreen_exits: 0 },
          ],
          server_now: Date.now(),
        });
      }
      if (path === '/api/admin/contests/4/integrity/cases' && method === 'GET') {
        return json(route, 200, { cases: state.decisions, server_now: Date.now() });
      }
      const decide = path.match(/^\/api\/admin\/contests\/4\/integrity\/cases\/(\d+)$/);
      if (decide && method === 'PUT') {
        const body = req.postDataJSON() as { decision: string; reason: string };
        state.decisionPuts.push({ user_id: Number(decide[1]), ...body });
        const user = USERS[decide[1]];
        const c = { id: 50 + state.decisionPuts.length, contest_id: 4, user_id: Number(decide[1]), name: user.name, email: user.email,
          decision: body.decision, reason: body.reason, decided_by: ADMIN.name, decided_at: Date.now() };
        state.decisions = [c, ...state.decisions.filter((d) => d.user_id !== c.user_id)];
        return json(route, 200, { case: c });
      }
      if (path === '/api/admin/contests/4/integrity/cases' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.caseBodies.push(body);
        if (String(body.note ?? '').trim().length < 20) return apiError(route, 400, 'bad_request', 'Write a reason of at least 20 characters (at most 4,000).');
        state.kase = { ...baseCase(), title: body.title, decisions: [{ by: 2, state: 'open', note: body.note, created_at: Date.now() }] };
        return json(route, 201, state.kase);
      }
      if (path === '/api/admin/integrity/cases/3' && method === 'GET') {
        state.kase ??= baseCase();
        return json(route, 200, state.kase);
      }
      if (path === '/api/admin/integrity/cases/3/proposals' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.proposals.push(body);
        if (options.raceProposal) {
          state.kase = { ...state.kase!, proposals: [zeroProposal], may_confirm: true };
          return apiError(route, 409, 'proposal_pending', 'A proposal is waiting for a second reviewer. Confirm it, or clear or reopen the case first.');
        }
        const k = (state.kase ??= baseCase());
        k.proposals = [{ id: 9, by: 2, by_name: 'Arjun Mehta', outcome: body.outcome, problem_label: body.problem_label ?? '', user_ids: body.user_ids, note: body.note, reason: body.reason, created_at: Date.now(), state: 'pending' }];
        k.may_confirm = false;
        return json(route, 201, { ...k, server_now: Date.now() });
      }
      const confirm = path.match(/^\/api\/admin\/integrity\/cases\/3\/proposals\/(\d+)\/confirm$/);
      if (confirm && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.confirms.push({ id: Number(confirm[1]), ...body });
        const k = (state.kase ??= baseCase());
        if (!k.may_confirm) return apiError(route, 403, 'two_person_rule', 'A second reviewer who did not open the case or propose this outcome confirms it.');
        const p = (k.proposals as Json[]).find((x) => x.id === Number(confirm[1]))!;
        state.kase = {
          ...k,
          state: 'actioned',
          proposals: [{ ...p, state: 'confirmed' }],
          decisions: [...(k.decisions as Json[]), { by: 2, by_name: 'Arjun Mehta', state: 'actioned', note: body.note, created_at: Date.now(), outcome: p.outcome, outcome_problem_label: p.problem_label, outcome_users: p.user_ids, proposal_id: p.id, sole_organiser: '' }],
          may_confirm: false,
        };
        return json(route, 200, { ...state.kase, server_now: Date.now() });
      }
      const ruling = path.match(/^\/api\/admin\/appeals\/(\d+)\/ruling$/);
      if (ruling && method === 'POST') {
        const body = req.postDataJSON() as { upheld: boolean; reason: string };
        state.rulings.push({ id: Number(ruling[1]), ...body });
        const k = state.kase!;
        const a = (k.appeals as Json[])[0];
        const ruled = { ...a, state: body.upheld ? 'upheld' : 'rejected', ruling: { by: 2, by_name: 'Arjun Mehta', upheld: body.upheld, reason: body.reason, created_at: Date.now() } };
        state.kase = { ...k, appeals: [ruled], may_rule: false };
        return json(route, 200, ruled);
      }
      if (path === '/api/admin/integrity/cases/3' && method === 'PUT') {
        const body = req.postDataJSON() as { state: string; note: string; seen_explanations?: Record<string, number> };
        state.puts.push(body);
        if (body.state === 'actioned') return apiError(route, 400, 'proposal_needed', 'Propose the outcome instead: a second reviewer confirms it.');
        const k = (state.kase ??= baseCase());
        const users = k.users as CaseUserJson[];
        const at = Date.now();
        // The server's rules: a newer explanation than the reviewer read is
        // refused, and action waits for every student's turn.
        const latest = (u: CaseUserJson) => u.explanations[u.explanations.length - 1]?.created_at ?? null;
        if (users.some((u) => latest(u) !== null && (body.seen_explanations?.[String(u.user_id)] ?? 0) < latest(u)!)) {
          return apiError(route, 409, 'explanation_changed', 'A student changed their explanation. Reload before deciding.');
        }
        const turnOver = (u: CaseUserJson) => u.asked_at !== null && ((latest(u) ?? -1) >= u.asked_at || at >= (u.respond_by ?? Infinity));
        if (body.state === 'actioned' && (k.state !== 'awaiting_student' || !users.every(turnOver))) {
          return apiError(route, 400, 'bad_request', 'Every student needs to explain, or to have had 7 days since the case was last shared, before you take action.');
        }
        state.kase = {
          ...k,
          state: body.state,
          updated_at: at,
          users:
            body.state === 'awaiting_student'
              ? users.map((u) => ({ ...u, shared_at: u.shared_at ?? at, asked_at: at, respond_by: at + 7 * DAY }))
              : body.state === 'open'
                ? users.map((u) => ({ ...u, asked_at: null, respond_by: null }))
                : users,
          decisions: [...(k.decisions as Json[]), { by: 2, by_name: 'Arjun Mehta', state: body.state, note: body.note, created_at: at, explanations_seen: body.seen_explanations ?? {} }],
          server_now: at,
        };
        return json(route, 200, state.kase);
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

export async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
}

export interface Screen {
  name: string;
  path: string;
  options: Options;
  title: string | RegExp;
  ready: (page: Page) => Promise<void>;
}

export const SCREENS: Screen[] = [
  {
    name: 'integrity',
    path: '/admin/contests/4/integrity',
    options: {},
    title: 'Similar code · Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.getByRole('table', { name: 'Signals on B. Interval covering' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Decisions' }).getByRole('row', { name: /Meera Iyer/ })).toBeVisible();
    },
  },
  {
    name: 'integrity-live',
    path: '/admin/contests/4/integrity',
    options: { live: true, unchecked: true },
    title: 'Similar code · Week 6 contest · podium',
    ready: (page) => expect(page.getByText('Not checked yet.', { exact: false })).toBeVisible(),
  },
  {
    name: 'integrity-pair',
    path: '/admin/contests/4/integrity/signals/31',
    options: {},
    title: 'Asha Rao and Ravi Kumar · Similar code · podium',
    ready: async (page) => {
      await expect(page.locator('mark').first()).toBeVisible();
      await expect(page.getByText('Same browser and network as Ravi Kumar.')).toBeVisible();
    },
  },
];
