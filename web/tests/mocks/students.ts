// Fixtures and the API mock of students.spec.ts (v2 P4 student screens):
// "Your progress" (contract C8), notices (C9), Standing (C9, C12), the
// student case page and appeals. Shapes follow docs/API.md "Students
// (v1.4.0, contracts C8, C9 and C12)". a11y.spec.ts reuses SCREENS.
//
// The standing and case responses deliberately carry a stray rating
// (`value_tenths`, `rating`) as a server bug would, so the spec can check
// that no student page ever prints it.
import { expect, test, type Page, type Route } from '@playwright/test';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-p4-student/`;
}

export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;
const DAY = 86_400_000;

export const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };
export const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'admin', theme: '', verdicts: 'standard' };

/** A rating value no student page may print: 8.5 as tenths. */
export const LEAKED_RATING = 85;

type Json = Record<string, unknown>;

function problem(label: string, title: string, score: number | null, over: Json = {}): Json {
  return {
    label,
    title,
    problem_id: 10 + label.charCodeAt(0),
    score,
    max_score: 100,
    attempts: score === null ? null : 2,
    full_at_minutes: score === 100 ? 18 : null,
    best_verdict: score === null ? null : score === 100 ? 'AC' : 'WA',
    subtasks: null,
    tags: ['math'],
    difficulty: 'easy',
    practice_open: true,
    upsolved: false,
    zero_by_decision: false,
    ...over,
  };
}

function contest(id: number, slug: string, title: string, endedAt: number, problems: Json[], over: Json = {}): Json {
  const score = problems.reduce((n: number, p) => n + ((p.score as number | null) ?? 0), 0);
  const max = problems.reduce((n: number, p) => n + (p.max_score as number), 0);
  return {
    contest_id: id,
    slug,
    title,
    graded: true,
    feedback_mode: 'subtask',
    detail: 'full',
    ended_at: endedAt,
    finalised_at: endedAt + 60_000,
    score,
    max_score: max,
    percent: Math.round((score / max) * 1000) / 10,
    rank: 12,
    ranked_count: 140,
    problems,
    ...over,
  };
}

/** A semester of five final contests, newest first, with an exam-mode one and a zero by decision. */
export function semester(now = Date.now()): Json[] {
  return [
    contest(6, 'week-6', 'Week 6 contest', now - 2 * DAY, [
      problem('A', 'A plus B', 100, { tags: ['math'], difficulty: 'easy', attempts: 1 }),
      problem('B', 'Interval covering', 40, { tags: ['greedy', 'sorting'], difficulty: 'medium', best_verdict: 'TLE', attempts: 4 }),
      problem('C', 'Shortest detour', 0, { tags: ['graphs'], difficulty: 'hard', best_verdict: 'WA', attempts: 3 }),
    ], { rank: 31, ranked_count: 140 }),
    contest(5, 'week-5', 'Week 5 contest', now - 9 * DAY, [
      problem('A', 'Count the vowels', 100, { tags: ['strings'], difficulty: 'easy' }),
      problem('B', 'Paths in a grid', 70, { tags: ['dp'], difficulty: 'medium', practice_open: false }),
      problem('C', 'Bridges', 70, { tags: ['graphs'], difficulty: 'hard', best_verdict: 'RE' }),
    ], { rank: 18, ranked_count: 132 }),
    contest(4, 'mid-sem', 'Mid-semester exam', now - 16 * DAY, [problem('A', 'Two pointers', null, { max_score: 150 }), problem('B', 'Heaps', null, { max_score: 150 })], {
      detail: 'totals',
      feedback_mode: 'exam',
      score: 180,
      max_score: 300,
      percent: 60,
      rank: 40,
      ranked_count: 151,
    }),
    contest(3, 'week-3', 'Week 3 contest', now - 23 * DAY, [
      problem('A', 'Prefix sums', 100, { tags: ['math'], difficulty: 'easy' }),
      problem('B', 'Binary search the answer', 0, { tags: ['binary search'], difficulty: 'medium', zero_by_decision: true, best_verdict: 'AC' }),
      problem('C', 'Union find', 40, { tags: ['graphs'], difficulty: 'hard', upsolved: true }),
    ], { rank: 55, ranked_count: 128 }),
    contest(2, 'week-2', 'Week 2 contest', now - 30 * DAY, [
      problem('A', 'Reverse a string', 100, { tags: ['strings'], difficulty: 'easy' }),
      problem('B', 'Matrix walk', 30, { tags: ['dp'], difficulty: 'medium', best_verdict: 'WA', practice_open: false }),
    ], { rank: 44, ranked_count: 120 }),
  ];
}

/** The C8 response for the given contests, with the server's derived lists left for the web app to compute. */
export function progressFor(contests: Json[], over: Json = {}): Json {
  let points = 0;
  let max = 0;
  let full = 0;
  let partial = 0;
  let zero = 0;
  for (const c of contests) {
    points += c.score as number;
    max += c.max_score as number;
    if (c.detail === 'totals') continue;
    for (const p of c.problems as Json[]) {
      const s = p.score as number;
      if (s >= (p.max_score as number)) full++;
      else if (s > 0) partial++;
      else zero++;
    }
  }
  return {
    user: { id: 7, name: 'Asha Rao' },
    hide_progress: false,
    generated_at: Date.now(),
    totals: {
      contests_taken: contests.length,
      graded_taken: contests.length,
      points,
      max_points: max,
      problems_full: full,
      problems_partial: partial,
      problems_zero: zero,
      personal_best_percent: contests.length > 0 ? Math.max(...contests.map((c) => c.percent as number)) : null,
      upsolved: contests.reduce((n: number, c) => n + (c.problems as Json[]).filter((p) => p.upsolved).length, 0),
    },
    contests,
    server_now: Date.now(),
    ...over,
  };
}

/** One final contest: what Home sums up in its line. */
export function oneContest(now = Date.now()): Json {
  return progressFor([
    contest(5, 'week-5', 'Week 5 contest', now - 7 * DAY, [problem('A', 'A plus B', 100), problem('B', 'Interval covering', 100), problem('C', 'Shortest detour', 40)]),
  ]);
}

/**
 * The progress the contest mock (tests/mocks/contests.ts) answers: Week 5,
 * ended a week ago with 240 of 300 as on Your contests, and Week 6 too once
 * its board is final.
 */
export function contestProgress(now: number, weekSixFinal: boolean): Json {
  const week5 = contest(5, 'week-5', 'Week 5 contest', now - 7 * DAY, [problem('A', 'Count the vowels', 100), problem('B', 'Paths in a grid', 100), problem('C', 'Bridges', 40)]);
  const week6 = contest(6, 'week-6', 'Week 6 contest', now - 2 * 3_600_000, [
    problem('A', 'A plus B', 100, { attempts: 1 }),
    problem('B', 'Interval covering', 40),
    problem('C', 'Shortest detour', 0, { attempts: 0, best_verdict: null }),
  ], { rank: 6, ranked_count: 7 });
  return progressFor(weekSixFinal ? [week6, week5] : [week5]);
}

const SOURCE ='#include <bits/stdc++.h>\nusing namespace std;\n\nint main() {\n  long long a, b;\n  cin >> a >> b;\n  if (a == 3 && b == 4) { cout << 7 << "\\n"; return 0; }\n  cout << a + b << "\\n";\n}\n';

export function notices(now = Date.now()): Json[] {
  return [
    {
      id: 14,
      kind: 'case_shared',
      happened: 'An organiser asked you to explain your submission to problem B in Week 6 contest.',
      todo: 'Read the question and reply on the case page.',
      due_at: now + 6 * DAY,
      reassurance: 'Nothing affects your marks until a person decides.',
      link: '/integrity#case-12',
      case_id: 12,
      contest_id: 6,
      created_at: now - 3 * 3_600_000,
      read_at: null,
    },
    {
      id: 13,
      kind: 'results_final',
      happened: 'The results of Week 6 contest are final.',
      todo: 'See your result and the problems to practise.',
      due_at: null,
      link: '/contests/week-6',
      case_id: null,
      contest_id: 6,
      created_at: now - 2 * DAY,
      read_at: null,
    },
    {
      id: 9,
      kind: 'case_decided',
      happened: 'An organiser decided on the question about your submission in Week 3 contest.',
      todo: 'Read the decision and its reason. You can appeal if you think it is wrong.',
      due_at: now + 10 * DAY,
      link: '/integrity#case-8',
      case_id: 8,
      contest_id: 3,
      created_at: now - 4 * DAY,
      read_at: now - 3 * DAY,
    },
  ];
}

export function standing(now = Date.now(), band: 'good' | 'on_record' = 'on_record'): Json {
  if (band === 'good') return { band: 'good', band_name: 'Good standing', lines: [], value_tenths: 100, rating: { value_tenths: 100 }, server_now: now };
  return {
    band: 'on_record',
    band_name: 'Outcomes on record',
    lines: [
      {
        id: 'adjustment-40',
        kind: 'adjustment',
        at: now - 1 * DAY,
        text: 'Standing adjusted by an organiser.',
        reason: 'You shared your solution to Week 5 problem C in the class group during the contest window.',
        case_id: null,
        adjustment_id: 40,
        appeal: null,
        appeal_by: now + 13 * DAY,
        can_appeal: true,
      },
      {
        id: 'outcome-31-7',
        kind: 'outcome',
        at: now - 4 * DAY,
        text: 'Week 3 contest: zero for problem B.',
        reason: 'Your program for problem B printed fixed answers for the hidden tests instead of solving them.',
        case_id: 8,
        adjustment_id: null,
        appeal: { state: 'open', created_at: now - 2 * DAY },
        appeal_by: now + 10 * DAY,
        can_appeal: false,
      },
    ],
    // A server bug the page must ignore.
    value_tenths: LEAKED_RATING,
    rating: { value_tenths: LEAKED_RATING, history: [] },
    server_now: now,
  };
}

export function cases(now = Date.now()): Json[] {
  return [
    {
      id: 12,
      contest_id: 6,
      state: 'awaiting_student',
      title: 'Week 6 contest',
      evidence: [
        {
          problem_id: 12,
          kind: 'manual',
          text: 'An organiser asked you to explain this submission. The lines in question are highlighted.',
          submission_id: 80,
          source: SOURCE,
          lines: [{ from: 7, to: 7 }],
        },
      ],
      explanation: '',
      shared_at: now - 3 * 3_600_000,
      decided: false,
      respond_by: now + 6 * DAY,
      outcome: null,
      appeal: null,
      can_appeal: false,
    },
    {
      id: 8,
      contest_id: 3,
      state: 'actioned',
      title: 'Week 3 contest',
      evidence: [{ problem_id: 22, kind: 'hardcoded', text: 'Your program printed the expected answers of hidden tests without computing them.', submission_id: 51, source: SOURCE, spans: [] }],
      explanation: 'I wrote this during the contest. The special case was for debugging and I forgot to remove it.',
      shared_at: now - 12 * DAY,
      decided: true,
      respond_by: null,
      outcome: { kind: 'zero_problem', problem_label: 'B', decided_at: now - 4 * DAY, reason: 'Your program for problem B printed fixed answers for the hidden tests instead of solving them.', appeal_by: now + 10 * DAY },
      appeal: null,
      can_appeal: true,
      value_tenths: LEAKED_RATING,
    },
  ];
}

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

export function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

export interface Options {
  who?: 'student' | 'admin';
  /** Which progress the student has: a semester, nothing final yet, one contest, or hidden by the preference. */
  progress?: 'semester' | 'empty' | 'one' | 'hidden';
  /** The student is in a live contest: notices are held. */
  held?: boolean;
  /** No notices at all. */
  noNotices?: boolean;
  band?: 'good' | 'on_record';
  /** The student has no cases. */
  noCases?: boolean;
}

/** Installs the API mock for one page and returns its mutable state. */
export async function mockApi(page: Page, options: Options = {}) {
  const now = Date.now();
  const state = {
    who: options.who ?? 'student',
    hide: options.progress === 'hidden',
    progressRequests: [] as string[],
    preferencePuts: [] as Json[],
    reads: [] as Json[],
    opened: [] as number[],
    appeals: [] as Json[],
    explanations: [] as { id: number; body: Json }[],
    notices: options.noNotices ? [] : notices(now),
    cases: options.noCases ? [] : cases(now),
  };

  const unread = () => (options.held ? 0 : state.notices.filter((n) => n.read_at === null).length);

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      const me = state.who === 'admin' ? ADMIN : STUDENT;

      if (path === '/api/me' && method === 'GET') return json(route, 200, { ...me, notifications_unread: unread(), hide_progress: state.hide });
      if (path === '/api/auth/config') return json(route, 200, { mode: 'production', allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' });
      if (path === '/api/auth/logout') return json(route, 204);
      if (path === '/api/events') return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: `event: clock\ndata: {"server_now":${Date.now()}}\n\n` });
      if (path === '/api/contests' && method === 'GET') return json(route, 200, { live: [], upcoming: [], past: [], server_now: Date.now() });

      if (path === '/api/me/preferences' && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.preferencePuts.push(body);
        if (typeof body.hide_progress === 'boolean') state.hide = body.hide_progress;
        return json(route, 200, { theme: body.theme, verdicts: body.verdicts, hide_progress: state.hide });
      }

      if (path === '/api/me/progress' && method === 'GET') {
        state.progressRequests.push(path);
        if (state.hide) return json(route, 200, { ...progressFor([]), hide_progress: true });
        if (options.progress === 'empty') return json(route, 200, progressFor([]));
        if (options.progress === 'one') return json(route, 200, oneContest(now));
        return json(route, 200, progressFor(semester(now)));
      }
      const adminProgress = path.match(/^\/api\/admin\/users\/(\d+)\/progress$/);
      if (adminProgress && method === 'GET') {
        state.progressRequests.push(path);
        if (me.role === 'student') return apiError(route, 403, 'forbidden', 'Only owners and admins can do this.');
        if (adminProgress[1] !== '7') return apiError(route, 404, 'not_found', 'There is no such account.');
        const p = progressFor(semester(now));
        delete p.hide_progress;
        return json(route, 200, p);
      }

      if (path === '/api/me/notifications' && method === 'GET') {
        if (options.held) return json(route, 200, { notifications: [], unread: 0, held: true, has_more: false, server_now: Date.now() });
        const before = Number(url.searchParams.get('before') ?? 0);
        const list = before > 0 ? [] : state.notices;
        return json(route, 200, { notifications: list, unread: unread(), held: false, has_more: before === 0 && list.length > 0, server_now: Date.now() });
      }
      if (path === '/api/me/notifications/read' && method === 'POST') {
        const body = req.postDataJSON() as { ids?: number[]; all?: boolean };
        state.reads.push(body as Json);
        for (const n of state.notices) if (body.all || (body.ids ?? []).includes(n.id as number)) n.read_at = Date.now();
        return json(route, 204);
      }

      if (path === '/api/me/standing' && method === 'GET') return json(route, 200, standing(now, options.band ?? 'on_record'));
      if (path === '/api/integrity/cases' && method === 'GET') {
        return json(route, 200, {
          cases: state.cases,
          contests: { '6': { slug: 'week-6', title: 'Week 6 contest' }, '3': { slug: 'week-3', title: 'Week 3 contest' } },
          problems: { '12': { label: 'B', title: 'Interval covering' }, '22': { label: 'B', title: 'Binary search the answer' } },
          server_now: Date.now(),
        });
      }
      const opened = path.match(/^\/api\/integrity\/cases\/(\d+)\/opened$/);
      if (opened && method === 'POST') {
        state.opened.push(Number(opened[1]));
        return json(route, 204);
      }
      const explain = path.match(/^\/api\/integrity\/cases\/(\d+)\/explanation$/);
      if (explain && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.explanations.push({ id: Number(explain[1]), body });
        const c = state.cases.find((x) => x.id === Number(explain[1]));
        if (c) c.explanation = body.explanation;
        return json(route, 204);
      }
      if (path === '/api/me/appeals' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.appeals.push(body);
        const text = String(body.text ?? '');
        if (text.length < 20) return apiError(route, 400, 'bad_request', 'Write your appeal in at least 20 characters (at most 4,000).');
        return json(route, 201, { id: 70, case_id: body.case_id ?? null, adjustment_id: body.adjustment_id ?? null, text, created_at: Date.now(), decide_by: Date.now() + 14 * DAY, overdue: false, state: 'open', ruling: null });
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
  title: string;
  ready: (page: Page) => Promise<void>;
}

export const SCREENS: Screen[] = [
  {
    name: 'progress',
    path: '/progress',
    options: {},
    title: 'Your progress · podium',
    ready: async (page) => {
      await expect(page.getByRole('heading', { level: 2, name: 'Contests and problems' })).toBeVisible();
      await expect(page.getByRole('img', { name: /Scores over 5 contests/ })).toBeVisible();
    },
  },
  {
    name: 'progress-empty',
    path: '/progress',
    options: { progress: 'empty' },
    title: 'Your progress · podium',
    ready: (page) => expect(page.getByText('Your progress appears here after your first contest ends.')).toBeVisible(),
  },
  {
    name: 'progress-hidden',
    path: '/progress',
    options: { progress: 'hidden' },
    title: 'Your progress · podium',
    ready: (page) => expect(page.getByRole('button', { name: 'Show my progress' })).toBeVisible(),
  },
  {
    name: 'progress-organiser',
    path: '/admin/users/7/progress',
    options: { who: 'admin' },
    title: "Asha Rao's progress · podium",
    ready: (page) => expect(page.getByRole('heading', { level: 1, name: "Asha Rao's progress" })).toBeVisible(),
  },
  {
    name: 'progress-forbidden',
    path: '/admin/users/7/progress',
    options: { who: 'student' },
    title: 'Student progress · podium',
    ready: (page) => expect(page.getByText("Only organisers can see a student's progress.")).toBeVisible(),
  },
  {
    name: 'notices',
    path: '/notices',
    options: {},
    title: 'Notices · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Reply on the case page' })).toBeVisible(),
  },
  {
    name: 'notices-held',
    path: '/notices',
    options: { held: true },
    title: 'Notices · podium',
    ready: (page) => expect(page.getByText('You are in a contest now.')).toBeVisible(),
  },
  {
    name: 'standing',
    path: '/integrity',
    options: {},
    title: 'Questions and standing · podium',
    ready: async (page) => {
      await expect(page.getByText('Outcomes on record')).toBeVisible();
      await expect(page.getByRole('heading', { level: 3, name: 'Week 6 contest' })).toBeVisible();
    },
  },
  {
    name: 'standing-good',
    path: '/integrity',
    options: { band: 'good', noCases: true },
    title: 'Questions and standing · podium',
    ready: (page) => expect(page.getByText('Nothing is on your record.')).toBeVisible(),
  },
  {
    name: 'case-open',
    path: '/integrity/cases/12',
    options: {},
    title: 'Question about your submission · Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeVisible();
      await expect(page.getByLabel('Explain how you wrote this, in your own words')).toBeVisible();
    },
  },
  {
    name: 'case-decided',
    path: '/integrity/cases/8',
    options: {},
    title: 'Question about your submission · Week 3 contest · podium',
    ready: async (page) => {
      await expect(page.getByText('Zero for problem B in Week 3 contest.')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Appeal this decision' })).toBeVisible();
    },
  },
];
