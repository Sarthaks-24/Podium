// Screens and flows for M4 contests (cache/v1/m4-plan.md sections 4 and 5).
// Every /api/ call is answered by an in-test mock built from the shapes in
// the plan as amended by cache/v1/m4-api-deviations.md, one per page; contest times are relative to the moment the
// mock is installed, and every response's server_now is the real clock, so
// countdowns read as they would live. Events go through the long-lived
// EventSource stand-in in tests/sse.ts.
//
// The sweep saves screenshots to cache/screenshots-m4/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1, the
// title and no horizontal overflow. The flows cover the student before,
// during and after a contest, exam mode, the frozen and the closed board,
// the editor, the console and the reveal.
import { expect, test, type Page, type Route } from '@playwright/test';
import { activate, hasFocus, tabTo } from './keyboard';
import { longLivedEvents } from './sse';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m4/`;
}
const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;
const MIN = 60_000;

type Json = Record<string, unknown>;

const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };
const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'admin', theme: '', verdicts: 'standard' };

const PROBLEMS = [
  { label: 'A', title: 'A plus B', id: 11, max: 100 },
  { label: 'B', title: 'Interval covering', id: 12, max: 100 },
  { label: 'C', title: 'Shortest detour', id: 13, max: 100 },
];

const STATEMENT = 'Given two integers $a$ and $b$, print $a + b$.\n\n## Input\n\nOne line with $a$ and $b$.\n\n## Output\n\nThe sum.';

type Phase = 'upcoming' | 'ready' | 'during' | 'after' | 'practice';

interface Options {
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
}

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

function submission(id: number, problemId: number, state: 'queued' | 'done', score = 0, created = Date.now()): Json {
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
function hidden(id: number, problemId: number): Json {
  return {
    id, problem_id: problemId, language: 'cpp17', state: 'submitted', hidden: true, verdict: '', score: 0, max_score: 100,
    max_time_ms: 0, max_memory_kb: 0, created_at: Date.now(), judged_at: null, queue_position: null, compile_output: '', samples: [], subtasks: [],
  };
}

/**
 * A board of `count` students sorted the way the server ranks: total down,
 * then the earlier time, competition ranks. The student generated at
 * `meIndex` is Asha Rao (user 7), with two submissions to B after the
 * freeze; the third row has one pending on C.
 */
function boardRows(count: number, meIndex: number, frozen: boolean) {
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

async function mockApi(page: Page, options: Options = {}) {
  await longLivedEvents(page);
  const now = Date.now();
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
          }))
        : null,
    accepted: state.accepted,
    announcements: state.announcements,
    my_clarifications: state.clarifications,
    public_clarifications: [],
    editorial_md: phase === 'after' || phase === 'practice' ? '## A plus B\n\nUse 64-bit integers.' : null,
    second_session: options.secondSession ?? false,
    server_now: Date.now(),
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
      server_now: Date.now(),
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
    published: false,
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
    problems: state.adminProblems,
    roster_count: 1,
    close_at: endsAt,
    default_honour_text: 'This contest is graded. By starting you agree to these rules.',
    hold_reveal: state.holdReveal,
    revealed_at: state.revealedAt,
    extensions: state.extensions.map((x) => ({ user_id: Number(x.user), email: 'asha@college.edu', name: 'Asha Rao', minutes: x.body.minutes, reason: x.body.reason })),
    admins: [{ user_id: 3, email: 'ta@college.edu', name: 'Meera Iyer' }],
    server_now: Date.now(),
  });

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
        let body = `event: clock\ndata: ${JSON.stringify({ server_now: Date.now() })}\n\n`;
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
          server_now: Date.now(),
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
        const c = { id: 90 + state.asked.length, problem_label: body.problem_label, question: body.question, answer: null, public: false, created_at: Date.now(), answered_at: null };
        state.clarifications.push(c);
        return json(route, 201, { clarification: c, server_now: Date.now() });
      }
      const prob = path.match(/^\/api\/contests\/week-6\/problems\/([A-Z])(\/(draft|run|submit|events))?$/);
      if (prob) {
        const label = prob[1];
        const p = PROBLEMS.find((x) => x.label === label);
        if (!p) return apiError(route, 404, 'not_found', 'No such problem.');
        if (!prob[2] && method === 'GET') return json(route, 200, workspace(label));
        if (prob[3] === 'draft' || prob[3] === 'events') return json(route, 204);
        if (prob[3] === 'run') return json(route, 202, { id: 70, state: 'queued', queue_position: 1, compile_output: '', results: [], server_now: Date.now() });
        if (prob[3] === 'submit') {
          const body = req.postDataJSON() as Json;
          state.submits.push({ ...body, label });
          const s = options.feedback === 'exam' ? hidden(state.nextId++, p.id) : submission(state.nextId++, p.id, 'queued');
          state.submissions.push(s);
          return json(route, 202, { ...s, server_now: Date.now() });
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
          server_now: Date.now(),
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
        if (body.published === true) return apiError(route, 409, 'conflict', 'The contest cannot be published yet.', { fields: { published: 'Problem B changed since it was added; validate it and add it again.' } });
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
        state.revealedAt = Date.now();
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
      if (path === '/api/admin/contests/4/rejudge' && method === 'POST') {
        state.rejudges.push(req.postDataJSON() as Json);
        return json(route, 202, { queued: 1, server_now: Date.now() });
      }
      if (path === '/api/admin/contests/4/submissions' && method === 'GET') {
        state.feedQueries.push(url.search);
        const rows = [
          { id: 301, user_id: 7, name: 'Asha Rao', email: 'asha@college.edu', problem_label: 'B', language: 'cpp17', state: 'done', verdict: 'WA', score: 40, max_score: 100, created_at: now - 2 * MIN, practice: false, rejudged: false },
          { id: 300, user_id: 101, name: 'Arjun Mehta', email: 'arjun@college.edu', problem_label: 'A', language: 'python3', state: 'done', verdict: 'AC', score: 100, max_score: 100, created_at: now - 5 * MIN, practice: false, rejudged: false },
          { id: 299, user_id: 102, name: 'Ravi Kumar', email: 'ravi@college.edu', problem_label: 'C', language: 'java21', state: 'running', verdict: '', score: 0, max_score: 100, created_at: now - 6 * MIN, practice: false, rejudged: false },
          { id: 298, user_id: 103, name: 'Meera Iyer', email: 'meera@college.edu', problem_label: 'B', language: 'cpp17', state: 'error', verdict: '', score: 0, max_score: 100, created_at: now - 7 * MIN, practice: false, rejudged: false },
        ];
        const problem = url.searchParams.get('problem');
        const user = url.searchParams.get('user');
        // As the server does (export.go): "pending" and "error" are states, anything else a verdict.
        const verdict = url.searchParams.get('verdict');
        const matches = (r: (typeof rows)[number]) =>
          !verdict || (verdict === 'pending' ? r.state === 'queued' || r.state === 'running' : verdict === 'error' ? r.state === 'error' : r.verdict === verdict);
        const shown = rows.filter((r) => (!problem || r.problem_label === problem) && (!user || String(r.user_id) === user) && matches(r));
        return json(route, 200, { submissions: shown, page: 1, has_more: false, server_now: Date.now() });
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
        const actor = { user_id: 1, email: 'priya@college.edu', role: 'owner' };
        const entries = [
          ...Array.from({ length: state.pauses }, () => ({ action: 'pause', details: {} })),
          ...state.extends.map((e) => ({ action: 'extend', details: e })),
          ...state.extensions.map((x) => ({ action: 'extension', details: { email: 'asha@college.edu', ...x.body } })),
          ...state.rejudges.map((r) => ({ action: 'rejudge', details: { ...r, queued: 1 } })),
        ].map((e, i) => ({ id: i + 1, actor, created_at: now, ...e }));
        return json(route, 200, { entries: entries.reverse(), page: 1, has_more: false, server_now: Date.now() });
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

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
}

interface Screen {
  name: string;
  path: string;
  options: Options;
  title: string | RegExp;
  ready: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
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
    title: 'Contest admin · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Week 7 contest', exact: true })).toBeVisible(),
  },
  {
    name: 'contest-editor',
    path: '/admin/contests/4',
    options: { who: 'admin', phase: 'during' },
    title: 'Week 6 contest · Contest settings · podium',
    ready: (page) => expect(page.getByRole('heading', { name: 'Co-organisers' })).toBeVisible(),
  },
  {
    name: 'contest-console',
    path: '/admin/contests/4/console',
    options: { who: 'admin', phase: 'during' },
    title: '(1) Week 6 contest · Console · podium',
    ready: async (page) => {
      await expect(page.getByText('Can intervals have zero length?')).toBeVisible();
      await expect(page.getByRole('cell', { name: 'Wrong answer' })).toBeVisible();
    },
  },
  {
    name: 'contest-results',
    path: '/admin/contests/4/results',
    options: { who: 'admin', phase: 'after' },
    title: 'Week 6 contest · Results · podium',
    ready: async (page) => {
      await expect(page.getByRole('region', { name: /Standings for Week 6 contest/ })).toBeVisible();
      await expect(page.getByText('Interval covering').first()).toBeVisible();
    },
  },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(screen.name, async ({ page }) => {
        await mockApi(page, screen.options);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        await settle(page);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page).toHaveTitle(screen.title);
          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

test('the contest list puts the live contest first with a countdown', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/');
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0)).toContainText('Week 6 contest');
  await expect(rows.nth(0)).toContainText('Live');
  await expect(rows.nth(0).locator('.countdown')).toHaveText(/^Ends in \d{2}:\d{2}:\d{2}$/);
  await expect(rows.nth(1)).toContainText('Upcoming');
  await expect(rows.nth(2)).toContainText('240 of 300');
  await expect(rows.nth(2)).toContainText('Ended');
  await expect(rows.nth(1).locator('.countdown')).toHaveText(/^Starts in \d d( \d+ h)?$/);
  // Phones keep the score, on the line under the start.
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(rows.nth(2).getByText('240 of 300')).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  // Students get only the Contests link.
  await expect(page.getByRole('link', { name: 'Contests', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'Contest admin' })).toHaveCount(0);
});

test('a student agrees to the honour code, solves, reads an announcement and asks a question', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByRole('link', { name: 'Week 6 contest' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Week 6 contest');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await expect(page.getByRole('heading', { name: 'Rules' })).toBeVisible();
  await expect(page.getByText('You can submit up to 15 times per problem, at least 30 s apart.')).toBeVisible();
  await expect(page.getByText('It is deleted 180 days after the contest.', { exact: false })).toBeVisible();
  await expect(page.getByText(/^Started\. Ends in/)).toBeVisible();

  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await expect.poll(() => api.starts).toBe(1);
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(rail.locator('.problems').getByRole('link')).toHaveCount(3);
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, not tried' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  await expect(page.getByRole('timer')).toHaveText(/^\d{2}:\d{2}:\d{2}$/);
  await expect(page.getByText(/^Standings freeze in/)).toBeVisible();
  await expect(page.getByLabel('Language').locator('option')).toHaveText(['C++17', 'Java 21', 'Python 3']);

  // Submit, then the judged result comes in and the rail picks up the score.
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0].label).toBe('A');
  const id = Number(api.submissions.at(-1)!.id);
  api.scores.A = 40;
  const judged = submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.stream.push({ type: 'submission', data: judged });
  await expect(page.getByRole('region', { name: 'Result' }).locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, 40 of 100 points' })).toBeVisible({ timeout: 10_000 });

  // An announcement arrives as a quiet banner, not an alert.
  api.announcements.push({ id: 1, body_md: 'Problem B: the statement has been corrected.', created_at: Date.now() + 1000 });
  api.stream.push({ type: 'announcement', data: { contest: 4, id: 1, body_md: 'Problem B: the statement has been corrected.' } });
  const banner = page.getByRole('status').filter({ hasText: 'Problem B: the statement has been corrected.' });
  await expect(banner).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(rail.getByRole('button', { name: /Messages/ })).toContainText('1');
  await page.screenshot({ path: `${shots()}contest-banner-1440-light.png` });

  // Open the messages and ask about problem B.
  await page.getByRole('button', { name: 'Show all messages' }).click();
  const dialog = page.getByRole('dialog', { name: 'Messages' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Problem B: the statement has been corrected.')).toBeVisible();
  await expect(dialog.getByLabel('About')).toHaveValue('A');
  await dialog.getByLabel('About').selectOption('B');
  await dialog.getByRole('button', { name: 'Send question' }).click();
  await expect(dialog.getByText('Write your question first.')).toBeVisible();
  await dialog.getByLabel('Question', { exact: true }).fill('Can intervals overlap at one point?');
  await dialog.getByRole('button', { name: 'Send question' }).click();
  await expect.poll(() => api.asked).toEqual([{ problem_label: 'B', question: 'Can intervals overlap at one point?' }]);
  await expect(dialog.getByText('Waiting for an answer.')).toBeVisible();
  await expect(dialog.getByRole('status').filter({ hasText: 'Question sent.' })).toBeVisible();
  await page.screenshot({ path: `${shots()}contest-messages-1440-light.png` });
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(banner).toBeHidden();

  // The rail moves between problems, and focus goes to the new heading.
  await rail.getByRole('link', { name: /Problem B/ }).click();
  await expect(page).toHaveURL('/contests/week-6/problems/B');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('B. Interval covering');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
});

test('an upcoming fixed-start contest counts down and keeps the start button closed', async ({ page }) => {
  await mockApi(page, { phase: 'upcoming' });
  await page.goto('/contests/week-6');
  await expect(page.getByRole('timer')).toHaveText(/^02:0\d:\d{2}$/);
  await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toBeDisabled();
  await expect(page.getByText(/^You can start at/)).toBeVisible();
});

test('a window-mode contest starts the clock on agreement', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready', startMode: 'window' });
  await page.goto('/contests/week-6');
  await expect(page.getByText('Your clock starts when you agree and start, and runs 1 h', { exact: false })).toBeVisible();
  await expect(page.getByText('The standings open when the contest closes.')).toBeVisible();
  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('timer')).toHaveText(/^00:(39|40):\d{2}$/);
  // Window mode has no board to freeze, so no freeze line.
  await expect(page.getByText(/Standings freeze/)).toHaveCount(0);
});

test('exam mode shows only "Submitted"', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during', feedback: 'exam' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('.cm-content')).toBeVisible();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.getByText('Submitted', { exact: true }).first()).toBeVisible();
  await expect(result.getByText('Scores appear after the contest.')).toBeVisible();
  const id = Number(api.submissions.at(-1)!.id);
  // Even if a full result slipped through, exam mode shows nothing more.
  api.stream.push({ type: 'submission', data: hidden(id, 11) });
  api.stream.push({ type: 'submission', data: submission(id, 11, 'done', 40) });
  await page.waitForTimeout(1500);
  await expect(result.getByText(/Partial|Wrong answer|Accepted|40 of 100/)).toHaveCount(0);
  await expect(result.locator('.strip')).toHaveCount(0);
  await expect(page.locator('.ws > [aria-live="polite"]').first()).toHaveText('Submitted.');
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, submitted once, score after the contest' })).toBeVisible();
  await expect(rail.getByRole('link', { name: /Problem A/ }).locator('.score')).toHaveText('Submitted');
  await expect(rail.getByRole('link', { name: 'Problem B, Interval covering, not submitted' })).toBeVisible();
  await page.screenshot({ path: `${shots()}contest-exam-1440-light.png` });
});

test('after the contest: result, submissions, editorial and practice', async ({ page }) => {
  await mockApi(page, { phase: 'practice' });
  await page.goto('/contests/week-6');
  await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
  await expect(page.locator('.total')).toContainText('140');
  await expect(page.locator('.total')).toContainText('of 300 points');
  await expect(page.getByText(/^You are \d+(st|nd|rd|th) of 28 with \d+ points\.$/)).toBeVisible();
  await expect(page.getByText('Practice is open.', { exact: false })).toBeVisible();
  await expect(page.getByText('Use 64-bit integers.')).toBeVisible();

  await page.getByText('Problem B: Interval covering').click();
  await expect(page.locator('details[open]').getByText('40 of 100').first()).toBeVisible();

  await page.getByRole('link', { name: 'Practise problem B' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('B. Interval covering');
  await expect(page.getByText('Practice is open. Submissions you make now do not count towards the standings.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeEnabled();
});

test('after a contest without practice, the problem is read only', async ({ page }) => {
  await mockApi(page, { phase: 'after' });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.getByText('The contest has ended. Submissions are closed.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeDisabled();
});

test('the frozen board shows pending cells, tints the own row and jumps to it', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 700 });
  await page.goto('/contests/week-6/standings');
  await expect(page.getByText(/^Frozen at \d{2}:\d{2}\. Updates when the contest ends\. 2 submissions are still being judged\.$/)).toBeVisible();
  const mine = page.locator('tr.me');
  await expect(mine).toContainText('Asha Rao');
  await expect(mine).toContainText('(you)');
  await expect(mine.locator('.pending')).toHaveText('+2');
  await expect(mine.getByText('Problem B, 0 of 100, 2 pending', { exact: false })).toHaveCount(0);
  await expect(mine.getByText('Problem B, no score before the freeze, 2 pending')).toBeAttached();
  const tint = await mine.locator('th').evaluate((el) => getComputedStyle(el).backgroundColor);
  const plain = await page.locator('tbody tr').first().locator('th').evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(tint).not.toBe(plain);

  await page.getByRole('button', { name: 'Jump to you' }).click();
  await expect(mine).toBeFocused();
  await expect(mine).toBeInViewport();

  // The header stays put while rows scroll.
  const head = page.locator('.board-wrap thead th').first();
  expect(await head.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');

  // On a phone the rank and name stay pinned while the problems scroll.
  await page.setViewportSize({ width: 375, height: 812 });
  const wrap = page.locator('.board-wrap');
  await wrap.evaluate((el) => (el.scrollLeft = 400));
  const name = page.locator('tbody tr').first().locator('th.name');
  const before = await name.boundingBox();
  expect(await name.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');
  expect(before!.x).toBeGreaterThanOrEqual(0);
  await page.screenshot({ path: `${shots()}standings-phone-scrolled-375-light.png` });
});

test('on a phone the standings header stays on screen while rows scroll', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/contests/week-6/standings');
  const wrap = page.locator('.board-wrap');
  await expect(wrap.locator('tbody tr')).toHaveCount(28);
  // The box fits the screen and scrolls both ways, so its sticky header row has something to stick to.
  const box = (await wrap.boundingBox())!;
  expect(box.height).toBeLessThanOrEqual(812);
  await wrap.evaluate((el) => (el.scrollTop = 600));
  expect(await wrap.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  const head = (await wrap.locator('thead th').first().boundingBox())!;
  expect(Math.abs(head.y - box.y)).toBeLessThanOrEqual(2);
  await page.screenshot({ path: `${shots()}standings-phone-rows-scrolled-375-light.png` });
});

test('a window-mode board stays closed until the window closes', async ({ page }) => {
  await mockApi(page, { phase: 'during', startMode: 'window', boardClosed: true });
  await page.goto('/contests/week-6/standings');
  await expect(page.getByText('The standings open when the contest closes.')).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);
});

test('a second tab with the same contest shows a notice, as does a second session', async ({ page, context, browser }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('.cm-content')).toBeVisible();
  const second = await context.newPage();
  await mockApi(second, { phase: 'during' });
  await second.goto('/contests/week-6/problems/A');
  await expect(second.getByText('This contest is also open in another tab or window on this device.', { exact: false })).toBeVisible();
  await expect(page.getByText('This contest is also open in another tab or window on this device.', { exact: false })).toBeVisible();
  await second.close();

  // Another device: the server reports the second session.
  const elsewhere = await browser.newContext();
  const other = await elsewhere.newPage();
  await mockApi(other, { phase: 'during', secondSession: true });
  await other.goto('/contests/week-6/problems/B');
  await expect(other.getByText('Your account is in this contest from another session as well.', { exact: false })).toBeVisible();
  await elsewhere.close();
});

test('admins see the Contest admin link and the contest list', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'during' });
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contest admin' }).click();
  await expect(page).toHaveURL('/admin/contests');
  await expect(page.getByRole('link', { name: 'Contest admin', exact: true }).first()).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('row', { name: /Week 7 contest/ })).toContainText('Draft');
  await expect(page.getByRole('row', { name: /Week 6 contest/ })).toContainText('Published');

  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title').fill('Week 8 contest');
  await expect(page.getByLabel('Slug')).toHaveValue('week-8-contest');
  await page.getByRole('button', { name: 'Create contest' }).click();
  await expect(page).toHaveURL('/admin/contests/9');
});

test('students cannot open the contest admin', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/admin/contests');
  await expect(page.getByText('Only admins can manage contests.')).toBeVisible();
});

test('the editor shows field errors, picks problems and previews a roster', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByRole('note').filter({ hasText: 'This contest is live.' })).toBeVisible();
  await expect(page.getByRole('note').getByRole('link', { name: 'console' })).toHaveAttribute('href', '/admin/contests/4/console');

  // A refused slug shows under the field and takes focus.
  await page.getByLabel('Slug', { exact: true }).fill('Week 6');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const slug = page.getByLabel('Slug', { exact: true });
  await expect(page.locator('#f-slug-error')).toHaveText('Use lowercase letters, digits and single hyphens, like week-6.');
  await expect(slug).toHaveAttribute('aria-invalid', 'true');
  await expect(slug).toBeFocused();

  // Publishing a contest whose problem changed is refused with the reason.
  await slug.fill('week-6');
  await page.getByLabel('Published: students can see the contest').check();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#f-published-error')).toHaveText('Problem B changed since it was added; validate it and add it again.');
  await page.getByLabel('Published: students can see the contest').uncheck();
  await page.getByLabel('Python 3').uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved changes')).toBeVisible();
  expect(api.settingsPuts.at(-1)).toMatchObject({ slug: 'week-6', languages: ['cpp17'], published: false, freeze_minutes: 30 });

  // Changing the freeze of a live contest asks first.
  const puts = api.settingsPuts.length;
  await page.getByLabel('Freeze the board, minutes before the end').fill('20');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const live = page.getByRole('dialog', { name: 'Change a live contest?' });
  await expect(live.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await live.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts.length).toBe(puts);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await live.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)?.freeze_minutes).toBe(20);

  // Only validated problems are offered; adding one gives it the next letter.
  const add = page.getByLabel('Add a validated problem');
  await expect(add.locator('option')).toHaveText(['Choose a problem', 'Interval covering (100 points)']);
  await add.selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  await page.getByRole('button', { name: 'Save problems' }).click();
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 12, label: 'A' },
    { problem_id: 11, label: 'B' },
  ]);

  // The roster preview names every refused line before anything is sent.
  await page.getByLabel('Or paste the CSV').fill('email,name,roll number,section\nravi@college.edu,Ravi Kumar,21CS002,B\nnot-an-email,Nobody\nravi@college.edu,Again\n');
  await expect(page.getByText('1 line ready, 2 refused.', { exact: false })).toBeVisible();
  await expect(page.getByText('The file replaces the whole roster: it adds 1 and removes 1 student.', { exact: false })).toBeVisible();
  const preview = page.getByRole('table', { name: 'Roster preview' });
  await expect(preview.getByRole('row')).toHaveCount(4);
  await expect(preview.getByRole('row', { name: /not-an-email/ })).toContainText('Not an email address');
  await expect(preview.getByRole('row', { name: /Again/ })).toContainText('Listed twice');
  await page.screenshot({ path: `${shots()}contest-editor-roster-1440-light.png`, fullPage: true });
  await page.getByRole('button', { name: 'Replace roster' }).click();
  const replace = page.getByRole('dialog', { name: 'Replace the roster?' });
  await expect(replace).toContainText('1 student now, 1 after. The 1 not in the file lose access and are taken out of the contest at once.');
  await expect(replace.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await replace.getByRole('button', { name: 'Replace roster' }).click();
  await expect.poll(() => api.rosterPuts.length).toBe(1);
  expect(api.rosterPuts[0]).toBe('email,name,roll_number,section\nravi@college.edu,Ravi Kumar,21CS002,B\n');
  await page.getByText('Show the current roster').click();
  await expect(page.getByText('asha@college.edu')).toBeVisible();

  await page.getByRole('button', { name: 'Remove Meera Iyer' }).click();
  const remove = page.getByRole('dialog', { name: 'Remove Meera Iyer?' });
  await remove.getByRole('button', { name: 'Remove Meera Iyer' }).click();
  await expect.poll(() => api.adminDeletes).toEqual([{ email: 'ta@college.edu' }]);
});

test('the console pauses, extends, rejudges, announces and answers', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await expect(page.getByText('Judging is running.')).toBeVisible();

  await page.getByRole('button', { name: 'Pause judging' }).click();
  const dialog = page.getByRole('dialog', { name: 'Pause judging?' });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Pause judging' }).click();
  await expect(page.getByText('Judging is paused. Submissions are queuing.')).toBeVisible();
  await expect.poll(() => api.pauses).toBe(1);
  await page.getByRole('button', { name: 'Resume judging' }).click();
  await expect(page.getByText('Judging is running.')).toBeVisible();

  await expect(page.getByRole('link', { name: '1 unanswered question' })).toHaveAttribute('href', '#clar-title');
  await page.getByLabel('Minutes for everyone').fill('0');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  await expect(page.locator('#extend-error')).toHaveText('Enter the minutes to add, 1 or more.');
  await expect(page.getByLabel('Minutes for everyone')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Minutes for everyone').fill('15');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  await page.getByRole('dialog', { name: 'Extend the contest by 15 minutes?' }).getByRole('button', { name: 'Extend contest' }).click();
  await expect.poll(() => api.extends).toEqual([{ minutes: 15 }]);
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue('The contest has been extended by 15 minutes.');

  await page.getByLabel('Student', { exact: true }).first().selectOption({ label: 'Asha Rao' });
  await page.getByLabel('Extra minutes').fill('10');
  await page.getByLabel('Reason').fill('Laptop crashed');
  await page.getByRole('button', { name: 'Give extra time' }).click();
  await expect.poll(() => api.extensions).toEqual([{ user: '7', body: { minutes: 10, reason: 'Laptop crashed' } }]);
  await expect(page.locator('.extensions')).toContainText('Asha Rao: 10 min, Laptop crashed');

  // Filters go to the feed's query.
  await page.locator('#f-problem').selectOption('A');
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?problem=A&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: /Rejudge submission by Arjun Mehta to A/ }).click();
  await expect.poll(() => api.rejudges).toEqual([{ submission_id: 300 }]);
  await page.getByLabel('Rejudge every submission to').selectOption('A');
  await page.getByRole('button', { name: 'Rejudge problem' }).click();
  await page.getByRole('dialog', { name: 'Rejudge problem A?' }).getByRole('button', { name: 'Rejudge problem' }).click();
  await expect.poll(() => api.rejudges.at(-1)).toEqual({ problem_label: 'A' });

  await page.getByLabel('Start from a template').selectOption('paused');
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  await page.getByRole('button', { name: 'Post announcement' }).click();
  await expect.poll(() => api.posted.length).toBe(1);

  // A canned reply, answered for everyone.
  await page.getByRole('group', { name: 'Canned replies' }).getByRole('button', { name: 'Yes.' }).click();
  await expect(page.getByLabel('Answer', { exact: true })).toHaveValue('Yes.');
  await page.getByLabel('Answer for everyone').check();
  await page.getByRole('button', { name: 'Send answer' }).click();
  await expect.poll(() => api.answers).toEqual([{ id: '51', body: { answer: 'Yes.', public: true } }]);

  // The audit log follows every action, newest first, with who did it.
  const audit = page.getByRole('region', { name: 'Audit log' });
  await expect(audit.getByRole('listitem').first()).toContainText('Rejudged problem A (1 submission)');
  await expect(audit).toContainText('Gave asha@college.edu 10 extra minutes (Laptop crashed)');
  await expect(audit).toContainText('Extended the contest by 15 minutes');
  await expect(audit).toContainText('Paused judging');
  await expect(audit).toContainText('priya@college.edu');
  await page.screenshot({ path: `${shots()}contest-console-1440-light.png`, fullPage: true });
});

test('the console feed filters waiting or judging and judge errors', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.goto('/admin/contests/4/console');
  const verdict = page.getByLabel('Verdict', { exact: true });
  await verdict.selectOption({ label: 'Waiting or judging' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=pending&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await expect(page.locator('.feed tbody')).toContainText('Judging');
  await verdict.selectOption({ label: 'Judge error' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=error&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await expect(page.locator('.feed tbody')).toContainText('Meera Iyer');
});

test('a graded contest asks for a reason before it takes an AI problem nobody solved', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'upcoming', aiGate: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await page.getByRole('button', { name: 'Save problems' }).click();

  const reason = page.getByLabel('Reason to grade problem B as the AI wrote it');
  await expect(reason).toBeFocused();
  await expect(page.getByText('Problem B came from the AI wizard', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Grade as written' }).click();
  await expect(page.locator('#ai-reason-error')).toHaveText('Write a reason of at least 10 characters.');
  await expect(reason).toHaveAttribute('aria-invalid', 'true');

  await reason.fill('Checked the reference by hand against the statement.');
  await page.getByRole('button', { name: 'Grade as written' }).click();
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 11, label: 'A' },
    { problem_id: 12, label: 'B', ai_reason: 'Checked the reference by hand against the statement.' },
  ]);
  await expect(page.getByText('Saved problems with your reason', { exact: true })).toBeVisible();
  await expect(reason).toHaveCount(0);
});

test('results switch boards, link the exports, release a held board and save the editorial', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'after', holdReveal: true });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByText('Final standings.')).toBeVisible();
  await page.getByLabel('As students saw it frozen').check();
  await expect(page.getByText(/^Frozen at/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Download CSV' })).toHaveAttribute('href', '/api/admin/contests/4/export.csv');
  await expect(page.getByRole('link', { name: 'Download JSON' })).toHaveAttribute('href', '/api/admin/contests/4/export.json');
  await expect(page.getByText('8 students scored 40')).toBeAttached();
  await expect(page.getByText('Students still see the frozen board.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Release the full board' }).click();
  const confirmRelease = page.getByRole('dialog', { name: 'Release the full board?' });
  await expect(confirmRelease).toContainText('This cannot be undone.');
  await expect(confirmRelease.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmRelease.getByRole('button', { name: 'Release the full board' }).click();
  await expect(page.getByText('Students still see the frozen board.', { exact: false })).toBeHidden();
  await expect.poll(() => api.releases).toBe(1);
  await page.getByRole('button', { name: 'Publish editorial' }).click();
  await expect.poll(() => api.editorials).toEqual([{ editorial_md: '## A\n\nUse long long.', published: true }]);
  await expect(page.getByText('Published: students see it after the contest.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Unpublish editorial' })).toBeVisible();
});

test('the reveal enters full screen on open and leaves it on exit', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await page.getByRole('button', { name: 'Start the reveal' }).click();
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement?.tagName ?? null)).toBe('HTML');
  await reveal.getByRole('button', { name: 'Exit' }).click();
  await expect(reveal).toBeHidden();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement)).toBeNull();
});

for (const motion of ['no-preference', 'reduce'] as const) {
  test(`the reveal walks from the bottom one row per key press (${motion})`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion: motion });
    await mockApi(page, { who: 'admin', phase: 'after' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/contests/4/results');
    await page.getByRole('button', { name: 'Start the reveal' }).click();
    const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
    await expect(reveal).toBeVisible();
    const names = () => reveal.locator('.rows .row .name').allTextContents();
    expect(await names()).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
    await expect(reveal.getByText('2 rows left.', { exact: false })).toBeVisible();
    await expect(reveal.locator('.row.next-row .name')).toHaveText('Asha Rao');
    await page.screenshot({ path: `${shots()}reveal-start-1440-${motion}.png` });

    // Record each row that is marked as moving, with its stacking order, as it happens.
    await reveal.evaluate((el) => {
      const seen: string[] = [];
      (window as unknown as { movingSeen: string[] }).movingSeen = seen;
      new MutationObserver(() => {
        for (const row of el.querySelectorAll<HTMLElement>('.row.moving')) seen.push(`${row.querySelector('.name')?.textContent} ${getComputedStyle(row).zIndex}`);
      }).observe(el, { subtree: true, attributes: true, attributeFilter: ['class'] });
    });
    await page.keyboard.press('Space');
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);
    await expect(reveal.locator('[aria-live="polite"]')).toHaveText('Asha Rao moves from 3rd to 2nd with 200 points.');
    // The climbing row is drawn above the row it passes while it slides (no move to draw with reduced motion).
    const seen = () => page.evaluate(() => [...new Set((window as unknown as { movingSeen: string[] }).movingSeen)]);
    if (motion === 'no-preference') await expect.poll(seen).toEqual(['Asha Rao 1']);
    else expect(await seen()).toEqual([]);
    await expect(reveal.locator('.row.moving')).toHaveCount(0);
    if (motion === 'reduce') await expect(reveal.locator('.row.flash .name')).toHaveText('Asha Rao');
    await page.waitForTimeout(400);
    await expect(reveal.locator('.row').filter({ hasText: 'Asha Rao' }).locator('.rank')).toHaveText('2');
    await page.screenshot({ path: `${shots()}reveal-step-1440-${motion}.png` });

    // The left arrow undoes the step, the right arrow does it again.
    await page.keyboard.press('ArrowLeft');
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
    await page.keyboard.press('ArrowRight');
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);
    await page.waitForTimeout(400);

    // Leaving keeps the place; opening again resumes.
    await page.keyboard.press('Escape');
    await expect(reveal).toBeHidden();
    const resume = page.getByRole('button', { name: 'Resume the reveal' });
    await expect(resume).toBeFocused();
    await resume.click();
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);

    await page.keyboard.press('ArrowRight');
    await expect(reveal.getByText('All rows resolved.', { exact: false })).toBeVisible();
    await expect(reveal.getByRole('button', { name: 'Next' })).toBeDisabled();
    await page.screenshot({ path: `${shots()}reveal-done-1440-${motion}.png` });

    // Space on Exit exits instead of stepping.
    await reveal.getByRole('button', { name: 'Exit' }).focus();
    await page.keyboard.press('Space');
    await expect(reveal).toBeHidden();
    await resume.click();
    await reveal.getByRole('button', { name: 'Start over' }).click();
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
  });
}

test('after the end, a held board gives the place only and says when it updates', async ({ page }) => {
  await mockApi(page, { phase: 'after', studentFrozen: true });
  await page.goto('/contests/week-6');
  await expect(
    page.getByText(/^In the frozen standings you are \d+(st|nd|rd|th) of 28\. Your final place shows when the organisers release the results\.$/),
  ).toBeVisible();
  await expect(page.getByText(/points\.$/)).toHaveCount(0);
  await page.getByRole('link', { name: 'See the standings' }).click();
  await expect(page.getByText(/^Frozen at \d{2}:\d{2}\. The final standings appear when the organisers release them\./)).toBeVisible();
  await expect(page.getByText('The time under each score is when it was reached, from the start.')).toBeVisible();
  await expect(page.locator('tr.me th.name')).toContainText('Asha Rao (you)');
});

test('below 1024 px the clock sticks to the top, and time up shows on screen', async ({ page }) => {
  await mockApi(page, { phase: 'during', endsInMs: 4000 });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/contests/week-6');
  const bar = page.locator('.clock-bar');
  await expect(bar.getByRole('timer')).toBeVisible();
  expect(await bar.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');
  await page.mouse.wheel(0, 600);
  await expect(bar.getByRole('timer')).toBeInViewport();
  await page.screenshot({ path: `${shots()}contest-clock-sticky-375-light.png` });
  // The window ends: the page moves to the results and keeps saying why.
  await expect(page.getByRole('status').filter({ hasText: 'Time is up. Your submissions are saved.' })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
});

test('a missing contest says so', async ({ page }) => {
  await mockApi(page);
  await page.goto('/contests/nope');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Contest not found');
});

test('keyboard only: a student starts, solves, submits, reads the verdict, asks and checks the standings', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Week 6 contest' })).toBeVisible();

  // The first Tab shows the skip link, and it moves focus into the page.
  await page.keyboard.press('Tab');
  const skip = page.getByRole('link', { name: 'Skip to content' });
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press('Enter');
  await expect(page.locator('main')).toBeFocused();

  // Contest list, then the rules and the start button.
  await activate(page, page.getByRole('link', { name: 'Week 6 contest' }));
  await expect(page).toHaveURL('/contests/week-6');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await activate(page, page.getByRole('button', { name: 'I agree, start the contest' }));
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');

  // "]" and "[" move between problems when nothing is being typed.
  await page.locator('main h1').focus();
  await page.keyboard.press(']');
  await expect(page).toHaveURL('/contests/week-6/problems/B');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await page.keyboard.press('[');
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await page.keyboard.press('[');
  await expect(page).toHaveURL('/contests/week-6/problems/A');

  // "?" lists the shortcuts; Escape closes the list and focus comes back.
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(keys).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Solving a problem' })).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Reveal' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  // Read the statement: the sample copy buttons are reachable.
  await tabTo(page, page.getByRole('button', { name: /Copy sample 1 input/i }));

  // Write code: Tab reaches the editor, typing "]" stays in the code, and
  // Escape then Tab leaves it.
  const code = page.locator('.cm-content');
  await tabTo(page, code);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// a[i]?');
  await expect(code).toContainText('// a[i]?');
  await expect(page.getByRole('dialog', { name: 'Keyboard shortcuts' })).toBeHidden();
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  expect(await hasFocus(code)).toBe(false);

  // Run and submit from the keyboard.
  await code.focus();
  await page.keyboard.press("Control+'");
  await expect(page.getByRole('heading', { name: 'Run on samples' })).toBeVisible();
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  const id = Number(api.submissions.at(-1)!.id);
  const judged = submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.scores.A = 40;
  api.stream.push({ type: 'submission', data: judged });
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });

  // Read the verdict: the failing subtask is open, the passing one opens with Enter.
  const passing = result.locator('details.group summary').filter({ hasText: 'Subtask 1' }).first();
  // Focus is still in the editor, where Tab indents: Escape first, then Tab on.
  await page.keyboard.press('Escape');
  await tabTo(page, passing);
  await page.keyboard.press('Enter');
  await expect(result.locator('details.group').filter({ hasText: 'Subtask 1' }).first()).toHaveAttribute('open', '');

  // Messages: open, ask, close; focus returns to the button.
  const messages = page.getByRole('navigation', { name: 'Problems' }).getByRole('button', { name: /Messages/ });
  // The rail comes before the heading, so Shift+Tab reaches it (forward,
  // Tab would enter the editor, where it indents until Escape).
  await page.locator('main h1').focus();
  await tabTo(page, messages, { back: true });
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Messages' });
  await expect(dialog).toBeVisible();
  await tabTo(page, dialog.getByLabel('Question', { exact: true }));
  await page.keyboard.type('Can a and b be negative?');
  await activate(page, dialog.getByRole('button', { name: 'Send question' }));
  await expect.poll(() => api.asked.length).toBe(1);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(messages).toBeFocused();

  // Standings: the link in the rail, then Jump to you.
  await activate(page, page.getByRole('navigation', { name: 'Problems' }).getByRole('link', { name: 'Standings' }), 'Enter');
  await expect(page).toHaveURL('/contests/week-6/standings');
  await activate(page, page.getByRole('button', { name: 'Jump to you' }));
  await expect(page.locator('tr.me')).toBeFocused();
});

test('keyboard only: an organiser edits a contest, runs the console and walks the reveal', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  // Contest admin list to the editor.
  await activate(page, page.getByRole('link', { name: 'Week 6 contest', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');

  // A setting changed and saved from the keyboard.
  await tabTo(page, page.getByLabel('Seconds between submissions'));
  await page.keyboard.press('Control+a');
  await page.keyboard.type('20');
  await activate(page, page.getByRole('button', { name: 'Save changes' }));
  await expect.poll(() => api.settingsPuts.at(-1)?.min_gap_s).toBe(20);

  // Add a problem, move it with the arrow buttons; focus never drops to the page.
  await tabTo(page, page.getByLabel('Add a validated problem'));
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await activate(page, page.getByRole('button', { name: 'Add problem' }));
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  await page.getByRole('button', { name: 'Move Interval covering up' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  // At the top the up arrow is off, so focus moved to the down arrow of the same problem.
  await expect(page.getByRole('button', { name: 'Move Interval covering down' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Move Interval covering up' })).toBeFocused();
  // Removing a problem hands focus to the row that took its place.
  await page.getByRole('button', { name: 'Remove Interval covering' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Letter for Interval covering')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove A plus B' })).toBeFocused();

  // Console: the section links, and an error that takes focus.
  await activate(page, page.getByRole('link', { name: 'Console', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/console');
  await activate(page, page.getByRole('button', { name: 'Give extra time' }));
  await expect(page.locator('#ext-error')).toHaveText('Choose a student.');
  await expect(page.getByLabel('Student', { exact: true }).first()).toBeFocused();

  // Results and the reveal, driven by keys from start to end and back.
  await page.locator('main h1').focus();
  await activate(page, page.getByRole('link', { name: 'Results', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/results');
  await activate(page, page.getByRole('button', { name: 'Start the reveal' }));
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal).toBeVisible();
  await page.keyboard.press('Space');
  await page.waitForTimeout(400);
  await page.keyboard.press('Space');
  await expect(reveal.getByText('All rows resolved.', { exact: false })).toBeVisible();
  // Next is now off; focus stayed in the dialog, so the keys still work.
  await expect.poll(() => hasFocus(reveal)).toBe(true);
  await page.waitForTimeout(400);
  await page.keyboard.press('ArrowLeft');
  await expect(reveal.getByText('1 row left.', { exact: false })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(reveal).toBeHidden();
  await expect(page.getByRole('button', { name: 'Resume the reveal' })).toBeFocused();
});
