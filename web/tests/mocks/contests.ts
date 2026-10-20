// Fixtures and the API mock of contests.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type Page, type Route } from '@playwright/test';
import { longLivedEvents } from '../sse';

let frozenNow: number | null = null;
/** The mock's clock: real time, or a fixed instant for pixel baselines (pair it with page.clock.setFixedTime). */
export function freezeClock(at: number | null): void {
  frozenNow = at;
}
function nowMs(): number {
  return frozenNow ?? Date.now();
}

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m4/`;
}
export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;
export const MIN = 60_000;

export type Json = Record<string, unknown>;

export const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };
export const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'admin', theme: '', verdicts: 'standard' };

export const PROBLEMS = [
  { label: 'A', title: 'A plus B', id: 11, max: 100 },
  { label: 'B', title: 'Interval covering', id: 12, max: 100 },
  { label: 'C', title: 'Shortest detour', id: 13, max: 100 },
];

export const STATEMENT = 'Given two integers $a$ and $b$, print $a + b$.\n\n## Input\n\nOne line with $a$ and $b$.\n\n## Output\n\nThe sum.';

export type Phase = 'upcoming' | 'ready' | 'during' | 'after' | 'practice';

export interface Options {
  who?: 'student' | 'admin';
  /** Interval covering came from the AI wizard: the graded contest asks for a reason. */
  aiGate?: boolean;
  phase?: Phase;
  feedback?: 'subtask' | 'exam';
  startMode?: 'fixed' | 'window';
  boardClosed?: boolean;
  secondSession?: boolean;
  holdReveal?: boolean;
  /** After the end, students still get the frozen board (held for the reveal). */
  studentFrozen?: boolean;
  /** The student's window ends this many ms after the page loads. */
  endsInMs?: number;
  /** The organiser's contest starts out published. */
  published?: boolean;
  /** PUT settings may publish (by default it refuses with a changed problem, P3). */
  publishOk?: boolean;
  /** Queue health on the contest response (C10): calm by default, or a slow queue, a silent judge and toolchain drift. */
  queueHealth?: 'calm' | 'trouble';
  /** Problem B moved to a new revision after the start, changing its statement (C3). */
  statementUpdated?: boolean;
  /** Problem A has a newer validated revision to update the contest to (C4). */
  updateAvailable?: boolean;
}

/** When B's statement changed, minutes before the page loads (the C3 fixture). */
export const STATEMENT_CHANGED_MIN = 12;

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

export function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

export function submission(id: number, problemId: number, state: 'queued' | 'done', score = 0, created = nowMs()): Json {
  const done = state === 'done';
  return {
    id,
    problem_id: problemId,
    language: 'cpp17',
    state,
    verdict: done ? (score === 100 ? 'AC' : 'WA') : '',
    score,
    max_score: 100,
    max_time_ms: done ? 30 : 0,
    max_memory_kb: done ? 3300 : 0,
    created_at: created,
    judged_at: done ? created + 4000 : null,
    queue_position: done ? null : 2,
    compile_output: '',
    samples: [{ idx: 1, verdict: done ? 'AC' : '', time_ms: 4, memory_kb: 3200 }],
    subtasks: [
      { idx: 1, points: 40, earned: done && score >= 40 ? 40 : 0, verdict: done ? (score >= 40 ? 'AC' : 'WA') : '', tests: [{ idx: 2, verdict: done ? (score >= 40 ? 'AC' : 'WA') : '', time_ms: 10, memory_kb: 3300 }] },
      { idx: 2, points: 60, earned: done && score === 100 ? 60 : 0, verdict: done ? (score === 100 ? 'AC' : 'WA') : '', tests: [{ idx: 3, verdict: done ? (score === 100 ? 'AC' : 'WA') : '', time_ms: 30, memory_kb: 3300 }] },
    ],
  };
}

/** An exam-mode submission before the end: everything withheld (m4-api-deviations.md). */
export function hidden(id: number, problemId: number): Json {
  return {
    id, problem_id: problemId, language: 'cpp17', state: 'submitted', hidden: true, verdict: '', score: 0, max_score: 100,
    max_time_ms: 0, max_memory_kb: 0, created_at: nowMs(), judged_at: null, queue_position: null, compile_output: '', samples: [], subtasks: [],
  };
}

/**
 * A board of `count` students sorted the way the server ranks: total down,
 * then the earlier time, competition ranks. The student generated at
 * `meIndex` is Asha Rao (user 7), with two submissions to B after the
 * freeze; the third row has one pending on C.
 */
export function boardRows(count: number, meIndex: number, frozen: boolean) {
  const names = ['Priya Raman', 'Arjun Mehta', 'Ravi Kumar', 'Meera Iyer', 'Kabir Shah', 'Neha Gupta', 'Rohan Das', 'Sara Thomas'];
  const rows: { rank: number; user_id: number; name: string; total: number; last_improvement_s: number; cells: Json[] }[] = [];
  for (let i = 0; i < count; i++) {
    const me = i === meIndex;
    // Asha's cells match her own scores in the contest (A 100, B 40, C 0);
    // her B points came after the freeze, so the frozen board has none.
    const a = me ? 100 : Math.max(0, 100 - i * 4);
    const b = me ? (frozen ? 0 : 40) : i < 3 ? 100 : i % 3 === 0 ? 40 : 0;
    const c = me ? 0 : i < 2 ? 100 : i === 2 ? 40 : 0;
    rows.push({
      rank: 0,
      user_id: me ? 7 : 100 + i,
      name: me ? 'Asha Rao' : `${names[i % names.length]}${i >= names.length ? ` ${Math.floor(i / names.length) + 1}` : ''}`,
      total: a + b + c,
      last_improvement_s: 600 + i * 97,
      cells: [
        { label: 'A', score: a, max: 100, pending: 0 },
        { label: 'B', score: b, max: 100, pending: me ? 2 : 0 },
        { label: 'C', score: c, max: 100, pending: 0 },
      ],
    });
  }
  rows.sort((x, y) => y.total - x.total || x.last_improvement_s - y.last_improvement_s);
  rows.forEach((r, i) => {
    const prev = rows[i - 1];
    r.rank = prev && prev.total === r.total && prev.last_improvement_s === r.last_improvement_s ? prev.rank : i + 1;
  });
  (rows[2].cells[2] as Json).pending = 1;
  return rows;
}

export async function mockApi(page: Page, options: Options = {}) {
  await longLivedEvents(page);
  const now = nowMs();
  const startMode = options.startMode ?? 'fixed';
  const phase = options.phase ?? 'ready';
  const startsAt = phase === 'upcoming' ? now + 2 * 3_600_000 + 5 * MIN : phase === 'after' || phase === 'practice' ? now - 4 * 3_600_000 : now - 30 * MIN;
  const endsAt = phase === 'after' || phase === 'practice' ? now - 2 * 3_600_000 : startsAt + 2 * 3_600_000;
  const state = {
    who: options.who ?? 'student',
    accepted: phase === 'during' || phase === 'after' || phase === 'practice',
    scores: { A: phase === 'after' || phase === 'practice' ? 100 : null, B: phase === 'after' || phase === 'practice' ? 40 : null, C: null } as Record<string, number | null>,
    submissions: [] as Json[],
    announcements: [] as Json[],
    clarifications: [] as Json[],
    stream: [] as { type: string; data: Json }[],
    starts: 0,
    submits: [] as Json[],
    asked: [] as Json[],
    nextId: 40,
    // Organiser side.
    settingsPuts: [] as Json[],
    problemPuts: [] as unknown[],
    rosterPuts: [] as string[],
    pauses: 0,
    resumes: 0,
    extends: [] as Json[],
    extensions: [] as { user: string; body: Json }[],
    rejudges: [] as Json[],
    posted: [] as Json[],
    answers: [] as { id: string; body: Json }[],
    editorials: [] as Json[],
    paused: false,
    adminProblems: [{ label: 'A', problem_id: 11, title: 'A plus B', ready: true, max_score: 100 }] as Json[],
    holdReveal: false,
    revealedAt: null as number | null,
    releases: 0,
    adminDeletes: [] as Json[],
    feedQueries: [] as string[],
    aiGate: options.aiGate ?? false,
    publicClarifications: [] as Json[],
    statementAt: options.statementUpdated ? now - STATEMENT_CHANGED_MIN * MIN : (null as number | null),
    revisionQueries: [] as string[],
    pinPuts: [] as { label: string; body: Json }[],
    pinnedRev: { A: options.updateAvailable ? 2 : 3 } as Record<string, number>,
    auditQueries: [] as string[],
  };

  state.holdReveal = options.holdReveal ?? false;
  if (phase === 'after' || phase === 'practice') {
    state.submissions.push(submission(30, 11, 'done', 100, startsAt + 20 * MIN), submission(31, 12, 'done', 40, startsAt + 50 * MIN));
  }

  const myWindow = () => {
    if (options.endsInMs !== undefined) return { started_at: startsAt, ends_at: now + options.endsInMs };
    if (startMode === 'window') return state.accepted ? { started_at: now - 20 * MIN, ends_at: phase === 'after' || phase === 'practice' ? now - 10 * MIN : now + 40 * MIN } : null;
    return { started_at: startsAt, ends_at: endsAt };
  };

  const contestState = () => {
    if (phase === 'upcoming') return 'upcoming';
    if (phase === 'practice') return 'practice';
    if (phase === 'after') return 'ended';
    return 'live';
  };

  const summary = () => ({
    slug: 'week-6',
    title: 'Week 6 contest',
    starts_at: startsAt,
    ends_at: endsAt,
    start_mode: startMode,
    duration_minutes: startMode === 'window' ? 60 : 0,
    problem_count: 3,
    state: contestState(),
    my_score: phase === 'after' || phase === 'practice' ? 140 : null,
    max_score: 300,
    my_window: myWindow(),
  });

  const contest = () => ({
    ...summary(),
    id: 4,
    description_md: 'Three problems on sums and intervals. Good luck.',
    rules: {
      feedback_mode: options.feedback ?? 'subtask',
      scoring_mode: 'best_subtask',
      min_gap_s: 30,
      max_per_problem: 15,
      languages: ['cpp17', 'python3', 'java21'],
      freeze_minutes: 30,
      graded: true,
    },
    honour_text:
      'This contest is graded. By starting you agree to these rules:\n\n- Work alone.\n- Do not use AI assistants.\n- Podium records your submissions, the characters you type and paste, and when you leave the page. It is deleted 180 days after the contest.',
    problems:
      state.accepted || phase === 'after' || phase === 'practice'
        ? PROBLEMS.map((p) => ({
            label: p.label,
            title: p.title,
            my_score: state.scores[p.label],
            max_score: p.max,
            my_submission_count: state.submissions.filter((s) => s.problem_id === p.id).length,
            revision_id: 10 + p.id,
            statement_updated_at: p.label === 'B' ? state.statementAt : null,
          }))
        : null,
    accepted: state.accepted,
    announcements: state.announcements,
    my_clarifications: state.clarifications,
    public_clarifications: state.publicClarifications,
    editorial_md: phase === 'after' || phase === 'practice' ? '## A plus B\n\nUse 64-bit integers.' : null,
    second_session: options.secondSession ?? false,
    server_now: nowMs(),
  });

  const workspace = (label: string) => {
    const p = PROBLEMS.find((x) => x.label === label)!;
    return {
      problem_id: p.id,
      label,
      title: p.title,
      statement_md: STATEMENT,
      time_limit_ms: 1000,
      memory_limit_mb: 256,
      languages: [
        { id: 'cpp17', name: 'C++17' },
        { id: 'java21', name: 'Java 21' },
        { id: 'python3', name: 'Python 3' },
      ],
      samples: [{ idx: 1, input: '1 2\n', output: '3\n', truncated: false }],
      draft: null,
      submissions: state.submissions.filter((s) => s.problem_id === p.id).sort((a, b) => Number(b.id) - Number(a.id)),
      server_now: nowMs(),
    };
  };

  const fullBoard = () => ({
    variant: 'full',
    version: 7,
    frozen_at: null,
    final: phase === 'after',
    judging_remaining: 0,
    rows: boardRows(28, 23, false).map((r) => ({ ...r, cells: r.cells.map((c) => ({ ...c, pending: 0 })) })),
    me: 7,
    labels: ['A', 'B', 'C'],
  });

  const frozenBoard = () => ({
    variant: 'frozen',
    version: 7,
    frozen_at: endsAt - 30 * MIN,
    final: false,
    judging_remaining: 2,
    rows: boardRows(28, 23, true),
    me: 7,
    labels: ['A', 'B', 'C'],
  });

  const adminContest = () => ({
    ...summary(),
    id: 4,
    description_md: 'Three problems.',
    freeze_minutes: 30,
    scoring_mode: 'best_subtask',
    access: 'roster',
    feedback_mode: 'subtask',
    graded: true,
    published: options.published ?? false,
    practice_after_end: true,
    stop_on_sample_fail: true,
    min_gap_s: 30,
    max_per_problem: 15,
    languages: ['cpp17', 'python3'],
    honour_text: '',
    paused: state.paused,
    extended_minutes: 0,
    editorial_md: '## A\n\nUse long long.',
    editorial_published: false,
    problems: state.adminProblems.map((p) => pinFields(p)),
    pin_history: [
      ...state.pinPuts.map((x, i) => ({ label: x.label, problem_id: 11, revision_id: Number(x.body.revision_id), rev: 3, from_at: now + i, actor: { user_id: 2, email: 'arjun.mehta@college.edu', role: 'admin' } })).reverse(),
      { label: 'A', problem_id: 11, revision_id: 12, rev: 2, from_at: startsAt - 60 * MIN, actor: { user_id: 1, email: 'priya@college.edu', role: 'owner' } },
    ],
    roster_count: 1,
    queue: options.queueHealth === 'trouble' ? { waiting: 14, judging: 2, errors: 1, oldest_wait_s: 95 } : { waiting: 0, judging: 1, errors: 1, oldest_wait_s: 0 },
    judge_last_claim_at: options.queueHealth === 'trouble' ? nowMs() - 3 * MIN : nowMs() - 4000,
    unanswered: state.answers.length ? 0 : 1,
    close_at: endsAt,
    default_honour_text: 'This contest is graded. By starting you agree to these rules.',
    hold_reveal: state.holdReveal,
    revealed_at: state.revealedAt,
    extensions: state.extensions.map((x) => ({ user_id: Number(x.user), email: 'asha@college.edu', name: 'Asha Rao', minutes: x.body.minutes, reason: x.body.reason })),
    admins: [{ user_id: 3, email: 'ta@college.edu', name: 'Meera Iyer' }],
    server_now: nowMs(),
  });

  /** C4 pin fields: A is pinned to revision 2 and, with updateAvailable, revision 3 is validated. */
  function pinFields(p: Json): Json {
    const rev = state.pinnedRev[String(p.label)] ?? 1;
    const latest = String(p.label) === 'A' ? 3 : rev;
    return {
      ...p,
      revision_id: 10 + rev,
      rev,
      latest_revision_id: 10 + latest,
      latest_rev: latest,
      update_available: latest > rev,
      update_statement_only: true,
      reconstructed: false,
      toolchain_drift: options.queueHealth === 'trouble' && String(p.label) === 'A',
    };
  }

  const revealBoards = () => {
    const row = (user_id: number, name: string, rank: number, a: number, b: number, t: number, pa = 0, pb = 0) => ({
      rank,
      user_id,
      name,
      total: a + b,
      last_improvement_s: t,
      cells: [
        { label: 'A', score: a, max: 100, pending: pa },
        { label: 'B', score: b, max: 100, pending: pb },
      ],
    });
    return {
      frozen: {
        variant: 'frozen', version: 3, frozen_at: endsAt - 30 * MIN, final: false, judging_remaining: 0, me: null, labels: ['A', 'B'],
        rows: [row(1, 'Priya Raman', 1, 100, 100, 3000), row(2, 'Arjun Mehta', 2, 100, 40, 2000, 0, 1), row(3, 'Asha Rao', 3, 100, 0, 1000, 0, 2), row(4, 'Ravi Kumar', 4, 40, 0, 900)],
      },
      full: {
        variant: 'full', version: 3, frozen_at: null, final: true, judging_remaining: 0, me: null, labels: ['A', 'B'],
        rows: [row(1, 'Priya Raman', 1, 100, 100, 3000), row(3, 'Asha Rao', 2, 100, 100, 5000), row(2, 'Arjun Mehta', 3, 100, 40, 2000), row(4, 'Ravi Kumar', 4, 40, 0, 900)],
      },
    };
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      const me = state.who === 'admin' ? ADMIN : STUDENT;

      if (path === '/api/me') return json(route, 200, me);
      if (path === '/api/auth/config') return json(route, 200, { mode: 'production', allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' });
      if (path === '/api/auth/logout') return json(route, 204);
      if (path === '/api/events') {
        const next = state.stream.shift();
        let body = `event: clock\ndata: ${JSON.stringify({ server_now: nowMs() })}\n\n`;
        if (next) body += `event: ${next.type}\ndata: ${JSON.stringify(next.data)}\n\n`;
        return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body });
      }

      // Student routes.
      if (path === '/api/contests' && method === 'GET') {
        return json(route, 200, {
          live: phase === 'ready' || phase === 'during' ? [summary()] : [],
          upcoming: [
            { ...summary(), slug: 'week-7', title: 'Week 7 contest', state: 'upcoming', starts_at: now + 3 * 86_400_000, ends_at: now + 3 * 86_400_000 + 2 * 3_600_000, my_window: null, my_score: null },
            ...(phase === 'upcoming' ? [summary()] : []),
          ],
          past: [
            { ...summary(), slug: 'week-5', title: 'Week 5 contest', state: 'ended', starts_at: now - 7 * 86_400_000, ends_at: now - 7 * 86_400_000 + 2 * 3_600_000, my_score: 240, my_window: null },
            ...(phase === 'after' || phase === 'practice' ? [summary()] : []),
          ],
          server_now: nowMs(),
        });
      }
      if (path === '/api/contests/week-6' && method === 'GET') return json(route, 200, contest());
      if (path === '/api/contests/nope' && method === 'GET') return apiError(route, 404, 'not_found', 'There is no contest with that slug.');
      if (path === '/api/contests/week-6/start' && method === 'POST') {
        state.starts++;
        const body = req.postDataJSON() as Json;
        if (body.accept_honour !== true) return apiError(route, 400, 'bad_request', 'Read the rules.', { fields: { accept_honour: 'Read the rules and agree to them to start.' } });
        state.accepted = true;
        return json(route, 200, contest());
      }
      if (path === '/api/contests/week-6/standings' && method === 'GET') {
        if (options.boardClosed) return apiError(route, 403, 'board_closed', 'The board opens when the window closes.');
        return json(route, 200, phase === 'during' || phase === 'ready' || options.studentFrozen ? frozenBoard() : fullBoard());
      }
      if (path === '/api/contests/week-6/clarifications' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.asked.push(body);
        const c = { id: 90 + state.asked.length, problem_label: body.problem_label, question: body.question, answer: null, public: false, created_at: nowMs(), answered_at: null };
        state.clarifications.push(c);
        return json(route, 201, { clarification: c, server_now: nowMs() });
      }
      const revs = path.match(/^\/api\/contests\/week-6\/problems\/([A-Z])\/revisions$/);
      if (revs && method === 'GET') {
        state.revisionQueries.push(url.search);
        const at = state.statementAt;
        if (!at) {
          return json(route, 200, { label: revs[1], current_revision_id: 21, statement_updated_at: null, revisions: [{ revision_id: 21, rev: 1, pinned_at: startsAt - 60 * MIN, title: 'Interval covering', statement_changed: false, statement_recorded: true }], diff: null, server_now: nowMs() });
        }
        const fromOld = url.searchParams.get('from') === '19';
        return json(route, 200, {
          label: revs[1],
          current_revision_id: 22,
          statement_updated_at: at,
          revisions: [
            { revision_id: 22, rev: 3, pinned_at: at, title: 'Interval covering', statement_changed: true, statement_recorded: true },
            { revision_id: 21, rev: 2, pinned_at: startsAt - 60 * MIN, title: 'Interval covering', statement_changed: true, statement_recorded: true },
            { revision_id: 19, rev: 1, pinned_at: startsAt - 120 * MIN, title: 'Intervals', statement_changed: false, statement_recorded: true },
          ],
          diff: {
            from_revision_id: fromOld ? 19 : 21, to_revision_id: 22, from_rev: fromOld ? 1 : 2, to_rev: 3,
            title_from: fromOld ? 'Intervals' : 'Interval covering', title_to: 'Interval covering', coarse: false,
            lines: [
              { op: 'same', text: 'Cover the line with the fewest intervals.' },
              { op: 'del', text: '1 <= n <= 1000' },
              { op: 'add', text: '1 <= n <= 100000' },
              { op: 'same', text: 'Intervals may touch at an end.' },
            ],
          },
          server_now: nowMs(),
        });
      }
      const prob = path.match(/^\/api\/contests\/week-6\/problems\/([A-Z])(\/(draft|run|submit|events))?$/);
      if (prob) {
        const label = prob[1];
        const p = PROBLEMS.find((x) => x.label === label);
        if (!p) return apiError(route, 404, 'not_found', 'No such problem.');
        if (!prob[2] && method === 'GET') return json(route, 200, workspace(label));
        if (prob[3] === 'draft' || prob[3] === 'events') return json(route, 204);
        if (prob[3] === 'run') return json(route, 202, { id: 70, state: 'queued', queue_position: 1, compile_output: '', results: [], server_now: nowMs() });
        if (prob[3] === 'submit') {
          const body = req.postDataJSON() as Json;
          state.submits.push({ ...body, label });
          const s = options.feedback === 'exam' ? hidden(state.nextId++, p.id) : submission(state.nextId++, p.id, 'queued');
          state.submissions.push(s);
          return json(route, 202, { ...s, server_now: nowMs() });
        }
      }
      const sub = path.match(/^\/api\/submissions\/(\d+)$/);
      if (sub) {
        const s = state.submissions.find((x) => x.id === Number(sub[1]));
        return s ? json(route, 200, s) : apiError(route, 404, 'not_found', 'No such submission.');
      }

      // Organiser routes.
      if (path.startsWith('/api/admin/') && me.role === 'student') return apiError(route, 403, 'forbidden', 'Only admins can do that.');
      if (path === '/api/admin/problems' && method === 'GET') {
        return json(route, 200, {
          problems: [
            { id: 11, slug: 'a-plus-b', title: 'A plus B', difficulty: 'easy', tags: [], origin: 'import', archived: false, test_count: 6, points: 100, validated: true, validated_at: now, updated_at: now },
            { id: 12, slug: 'interval-covering', title: 'Interval covering', difficulty: 'medium', tags: [], origin: 'import', archived: false, test_count: 15, points: 100, validated: true, validated_at: now, updated_at: now },
            { id: 14, slug: 'draft-problem', title: 'Unfinished draft', difficulty: '', tags: [], origin: 'manual', archived: false, test_count: 0, points: 0, validated: false, validated_at: null, updated_at: now },
          ],
        });
      }
      if (path === '/api/admin/contests' && method === 'GET') {
        return json(route, 200, {
          contests: [
            { ...adminContest(), published: true, problems: PROBLEMS.map((p) => ({ label: p.label, problem_id: p.id, title: p.title, ready: true, max_score: 100 })) },
            { ...adminContest(), id: 5, slug: 'week-7', title: 'Week 7 contest', starts_at: now + 3 * 86_400_000, ends_at: now + 3 * 86_400_000 + 2 * 3_600_000, close_at: now + 3 * 86_400_000 + 2 * 3_600_000, start_mode: 'window', duration_minutes: 90, state: 'upcoming', published: false, problems: [] },
          ],
          server_now: nowMs(),
        });
      }
      if (path === '/api/admin/contests' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        return json(route, 201, { ...adminContest(), id: 9, title: body.title, slug: body.slug });
      }
      if (path === '/api/admin/contests/4' && method === 'GET') return json(route, 200, adminContest());
      if (path === '/api/admin/contests/4' && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.settingsPuts.push(body);
        if (body.slug === 'Week 6') return apiError(route, 400, 'bad_request', 'Check the highlighted fields.', { fields: { slug: 'Use lowercase letters, digits and single hyphens, like week-6.' } });
        if (body.published === true && !options.publishOk) return apiError(route, 409, 'conflict', 'The contest cannot be published yet.', { fields: { published: 'Problem B changed since it was added; validate it and add it again.' } });
        return json(route, 200, { ...adminContest(), ...body });
      }
      if (path === '/api/admin/contests/4/problems' && method === 'PUT') {
        const body = req.postDataJSON() as { problem_id: number; label: string; ai_reason?: string }[];
        state.problemPuts.push(body);
        const unconfirmed = body.filter((b) => state.aiGate && b.problem_id === 12 && (b.ai_reason ?? '').length < 10);
        if (unconfirmed.length > 0) {
          return apiError(route, 409, 'ai_unconfirmed', `Problem ${unconfirmed[0].label} came from the AI wizard and no person has written a solution for it.`, {
            problems: unconfirmed.map((b) => b.label),
          });
        }
        state.adminProblems = body.map((b) => ({ ...b, title: PROBLEMS.find((p) => p.id === b.problem_id)?.title ?? 'Problem', ready: true, max_score: 100 }));
        return json(route, 200, adminContest());
      }
      if (path === '/api/admin/contests/4/roster' && method === 'GET') {
        return json(route, 200, { roster: [{ email: 'asha@college.edu', name: 'Asha Rao', roll_number: '21CS001', section: 'A' }], count: 1 });
      }
      if (path === '/api/admin/contests/4/roster' && method === 'PUT') {
        state.rosterPuts.push(req.postData() ?? '');
        return json(route, 200, { roster: [], count: 1 });
      }
      if (path === '/api/admin/contests/4/admins') {
        if (method === 'DELETE') state.adminDeletes.push(req.postDataJSON() as Json);
        return json(route, 200, adminContest());
      }
      if (path === '/api/admin/contests/4/reveal' && method === 'POST') {
        state.releases++;
        state.revealedAt = nowMs();
        return json(route, 200, adminContest());
      }
      if (path === '/api/admin/contests/4/pause' && method === 'POST') {
        state.pauses++;
        state.paused = true;
        return json(route, 204);
      }
      if (path === '/api/admin/contests/4/resume' && method === 'POST') {
        state.resumes++;
        state.paused = false;
        return json(route, 204);
      }
      if (path === '/api/admin/contests/4/extend' && method === 'POST') {
        state.extends.push(req.postDataJSON() as Json);
        return json(route, 204);
      }
      const ext = path.match(/^\/api\/admin\/contests\/4\/extensions\/(\d+)$/);
      if (ext && method === 'PUT') {
        state.extensions.push({ user: ext[1], body: req.postDataJSON() as Json });
        return json(route, 200, adminContest());
      }
      const pin = path.match(/^\/api\/admin\/contests\/4\/problems\/([A-Z])\/revision$/);
      if (pin && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.pinPuts.push({ label: pin[1], body });
        state.pinnedRev[pin[1]] = Number(body.revision_id) - 10;
        return json(route, 200, {
          contest: adminContest(),
          announcement_md: `The statement of problem ${pin[1]} was updated. Open the problem and choose "See what changed".`,
          statement_changed: true,
          rejudge_needed: false,
          server_now: nowMs(),
        });
      }
      if (path === '/api/admin/contests/4/rejudge' && method === 'POST') {
        state.rejudges.push(req.postDataJSON() as Json);
        return json(route, 202, { queued: 1, server_now: nowMs() });
      }
      if (path === '/api/admin/contests/4/submissions' && method === 'GET') {
        state.feedQueries.push(url.search);
        const rows = [
          { id: 301, user_id: 7, name: 'Asha Rao', email: 'asha@college.edu', problem_label: 'B', language: 'cpp17', state: 'done', verdict: 'WA', score: 40, max_score: 100, created_at: now - 2 * MIN, practice: false, rejudged: false },
          { id: 300, user_id: 101, name: 'Arjun Mehta', email: 'arjun@college.edu', problem_label: 'A', language: 'python3', state: 'done', verdict: 'AC', score: 100, max_score: 100, created_at: now - 5 * MIN, practice: false, rejudged: false },
          { id: 299, user_id: 102, name: 'Ravi Kumar', email: 'ravi@college.edu', problem_label: 'C', language: 'java21', state: 'running', verdict: '', score: 0, max_score: 100, created_at: now - 6 * MIN, practice: false, rejudged: false },
          { id: 298, user_id: 103, name: 'Meera Iyer', email: 'meera@college.edu', problem_label: 'B', language: 'cpp17', state: 'error', verdict: '', score: 0, max_score: 100, created_at: now - 7 * MIN, practice: false, rejudged: false },
          { id: 297, user_id: 104, name: 'Kiran Das', email: 'kiran@college.edu', problem_label: 'C', language: 'cpp17', state: 'done', verdict: 'WA', score: 0, max_score: 100, created_at: now - 9 * MIN, practice: false, rejudged: false },
        ];
        const problem = url.searchParams.get('problem');
        const user = url.searchParams.get('user');
        // As the server does (export.go): "pending" and "error" are states, anything else a verdict.
        const verdict = url.searchParams.get('verdict');
        const matches = (r: (typeof rows)[number]) =>
          !verdict || (verdict === 'pending' ? r.state === 'queued' || r.state === 'running' : verdict === 'error' ? r.state === 'error' : r.verdict === verdict);
        const shown = rows.filter((r) => (!problem || r.problem_label === problem) && (!user || String(r.user_id) === user) && matches(r));
        return json(route, 200, { submissions: shown, page: 1, has_more: false, server_now: nowMs() });
      }
      if (path === '/api/admin/contests/4/announcements' && method === 'GET') return json(route, 200, { announcements: state.posted.map((p, i) => ({ id: i + 1, body_md: p.body_md, created_at: now })) });
      if (path === '/api/admin/contests/4/announcements' && method === 'POST') {
        state.posted.push(req.postDataJSON() as Json);
        return json(route, 201, { id: state.posted.length, body_md: 'x', created_at: now });
      }
      if (path === '/api/admin/contests/4/clarifications' && method === 'GET') {
        return json(route, 200, {
          clarifications: [
            { id: 51, problem_label: 'B', asked_by: { user_id: 7, email: 'asha@college.edu', name: 'Asha Rao' }, question: 'Can intervals have zero length?', answer: state.answers.length ? 'Yes.' : null, public: false, created_at: now - 3 * MIN, answered_at: null },
            { id: 50, problem_label: null, asked_by: { user_id: 101, email: 'arjun@college.edu', name: 'Arjun Mehta' }, question: 'Is Java allowed?', answer: 'Yes, Java 21.', public: true, created_at: now - 9 * MIN, answered_at: now - 8 * MIN },
          ],
        });
      }
      const clar = path.match(/^\/api\/admin\/contests\/4\/clarifications\/(\d+)$/);
      if (clar && method === 'PUT') {
        state.answers.push({ id: clar[1], body: req.postDataJSON() as Json });
        return json(route, 200, {});
      }
      if (path === '/api/admin/contests/4/standings' && method === 'GET') {
        return json(route, 200, url.searchParams.get('variant') === 'frozen' ? { ...frozenBoard(), me: null } : { ...fullBoard(), me: null, final: true });
      }
      if (path === '/api/admin/contests/4/reveal' && method === 'GET') return json(route, 200, revealBoards());
      if (path === '/api/admin/contests/4/stats' && method === 'GET') {
        return json(route, 200, {
          problems: [
            { label: 'A', title: 'A plus B', max_score: 100, solved: 21, attempted: 27, median_attempts: 2, subtasks: [], histogram: [{ score: 0, students: 6 }, { score: 40, students: 3 }, { score: 100, students: 21 }] },
            { label: 'B', title: 'Interval covering', max_score: 100, solved: 4, attempted: 19, median_attempts: 4, subtasks: [], histogram: [{ score: 0, students: 11 }, { score: 40, students: 8 }, { score: 100, students: 4 }] },
          ],
        });
      }
      if (path === '/api/admin/contests/4/audit' && method === 'GET') {
        state.auditQueries.push(url.search);
        const actor = { user_id: 1, email: 'priya@college.edu', role: 'owner' };
        const entries = [
          ...Array.from({ length: state.pauses }, () => ({ action: 'pause', details: {} })),
          ...state.extends.map((e) => ({ action: 'extend', details: e })),
          ...state.extensions.map((x) => ({ action: 'extension', details: { email: 'asha@college.edu', ...x.body } })),
          ...state.rejudges.map((r) => ({ action: 'rejudge', details: { ...r, queued: 1 } })),
          ...state.pinPuts.map((x) => ({ action: 'revision', details: { label: x.label, from_rev: 2, to_rev: 3, statement_changed: true, judging_changed: false } })),
        ].map((e, i) => ({ id: i + 1, actor, created_at: now, ...e }));
        const want = url.searchParams.get('action') ?? '';
        const who = (url.searchParams.get('actor') ?? '').toLowerCase();
        const shown = entries.filter((e) => (want === '' || e.action === want) && (who === '' || e.actor.email.includes(who)));
        return json(route, 200, { entries: shown.reverse(), page: 1, has_more: false, server_now: nowMs() });
      }
      if (path === '/api/admin/contests/4/editorial' && method === 'PUT') {
        state.editorials.push(req.postDataJSON() as Json);
        return json(route, 204);
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
    name: 'home-student',
    path: '/',
    options: { phase: 'during' },
    title: 'Your contests · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Week 6 contest' })).toBeVisible(),
  },
  {
    name: 'contest-before',
    path: '/contests/week-6',
    options: { phase: 'ready' },
    title: 'Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toBeEnabled();
      await expect(page.locator('.honour .md li').first()).toBeVisible();
    },
  },
  {
    name: 'contest-during',
    path: '/contests/week-6/problems/B',
    options: { phase: 'during' },
    title: 'B. Interval covering · Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.locator('.cm-content')).toBeVisible();
      await expect(page.locator('.statement .katex').first()).toBeVisible();
    },
  },
  {
    name: 'contest-after',
    path: '/contests/week-6',
    options: { phase: 'practice' },
    title: 'Week 6 contest · podium',
    ready: (page) => expect(page.locator('.md :is(h3, h4)', { hasText: 'A plus B' })).toBeVisible({ timeout: 10_000 }),
  },
  {
    name: 'standings-frozen',
    path: '/contests/week-6/standings',
    options: { phase: 'during' },
    title: 'Standings · Week 6 contest · podium',
    ready: (page) => expect(page.getByRole('button', { name: 'Jump to you' })).toBeVisible(),
  },
  {
    name: 'standings-closed',
    path: '/contests/week-6/standings',
    options: { phase: 'during', startMode: 'window', boardClosed: true },
    title: 'Standings · Week 6 contest · podium',
    ready: (page) => expect(page.getByText('The standings open when the contest closes.')).toBeVisible(),
  },
  {
    name: 'admin-contests',
    path: '/admin/contests',
    options: { who: 'admin', phase: 'during' },
    title: 'Contests · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Week 7 contest', exact: true })).toBeVisible(),
  },
  {
    name: 'contest-editor',
    path: '/admin/contests/4',
    options: { who: 'admin', phase: 'during' },
    title: 'Contest settings · Week 6 contest · podium',
    ready: (page) => expect(page.getByRole('heading', { name: 'Co-organisers' })).toBeVisible(),
  },
  {
    name: 'contest-console',
    path: '/admin/contests/4/console',
    options: { who: 'admin', phase: 'during' },
    title: '(1) Console · Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.getByText('Can intervals have zero length?')).toBeVisible();
      await expect(page.getByRole('cell', { name: 'Partial, 40 of 100' })).toBeVisible();
    },
  },
  {
    name: 'contest-results',
    path: '/admin/contests/4/results',
    options: { who: 'admin', phase: 'after' },
    title: 'Results · Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.getByRole('region', { name: /Standings for Week 6 contest/ })).toBeVisible();
      await expect(page.getByText('Interval covering').first()).toBeVisible();
    },
  },
];
