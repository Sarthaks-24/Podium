// Fixtures and the API mock of the organisers' students pages (v2 P4
// F9.2b, F9.4): the students table, a student's page, the record page with
// the rating, its history and edits, and the appeal ruling on an
// adjustment. Shapes follow docs/API.md "C9: organisers' students pages"
// and "C9 and C12: Standing". students.spec.ts and a11y.spec.ts use them.
import { expect, test, type Page, type Route } from '@playwright/test';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-p4-organiser/`;
}

export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;
export const DAY = 86_400_000;

type Json = Record<string, unknown>;

export const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'admin', theme: '', verdicts: 'standard' };
export const OWNER = { id: 1, email: 'priya@college.edu', name: 'Priya Raman', avatar_url: '', role: 'owner', theme: '', verdicts: 'standard' };
export const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

export function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

/** The students table, in no particular order: the page sorts by name. */
export function studentRows(now = Date.now()): Json[] {
  const row = (id: number, name: string, email: string, graded: number, pct: number | null, last: number | null, rating: number) => ({
    id,
    name,
    email,
    graded_contests: graded,
    last_graded_percent: pct,
    last_graded_slug: pct === null ? null : 'week-6',
    last_submission_at: last,
    rating_tenths: rating,
  });
  return [
    row(9, 'Zoë Fernandes', 'zoe@college.edu', 4, 0, now - 2 * DAY, 55),
    row(7, 'Asha Rao', 'asha@college.edu', 4, 80, now - 2 * DAY, 75),
    row(11, 'Kabir Shah', 'kabir@college.edu', 3, 46.7, now - 9 * DAY, 92),
    row(10, 'Meera Iyer', 'meera@college.edu', 1, null, null, 100),
    row(8, 'bilal Khan', 'bilal@college.edu', 4, 100, now - 3 * 3_600_000, 100),
  ];
}

export const LAST_GRADED = [
  { contest_id: 6, slug: 'week-6', title: 'Week 6 contest' },
  { contest_id: 5, slug: 'week-5', title: 'Week 5 contest' },
];

export function studentDetail(now = Date.now()): Json {
  const c = (id: number, slug: string, title: string, ago: number, score: number, max: number, median: number | null, graded = true, rank = 12) => ({
    contest_id: id,
    slug,
    title,
    graded,
    ended_at: now - ago * DAY,
    score,
    max_score: max,
    percent: Math.round((score / max) * 1000) / 10,
    class_median_percent: median,
    rank,
    ranked_count: 140,
    problems_attempted: 3,
    problems_full: Math.floor(score / 100),
    submissions: 9,
    accepted: Math.floor(score / 100),
  });
  return {
    student: { id: 7, name: 'Asha Rao', email: 'asha@college.edu' },
    contests: [
      c(6, 'week-6', 'Week 6 contest', 2, 240, 300, 62.5, true, 31),
      c(5, 'week-5', 'Week 5 contest', 9, 240, 300, 70),
      c(4, 'mid-sem', 'Mid-semester exam', 16, 180, 300, 55),
      c(13, 'warm-up', 'Warm-up practice', 20, 100, 100, 100, false),
      c(3, 'week-3', 'Week 3 contest', 23, 140, 300, 50, true, 55),
    ],
    server_now: now,
  };
}

/** The rating's history, newest first: a warning, a zero that crossed into "Outcomes on record", a rollover, a manual edit. */
function history(now: number): Json[] {
  const line = (id: number, ago: number, delta: number, value: number, cause: string, before: string, after: string, over: Json = {}) => ({
    id,
    at: now - ago * DAY,
    delta_tenths: delta,
    value_tenths: value,
    cause,
    reason: '',
    case_id: null,
    decision_id: null,
    appeal_id: null,
    actor: null,
    confirmer: null,
    semester_label: '',
    band_before: before,
    band_after: after,
    ...over,
  });
  return [
    line(40, 1, -10, 75, 'manual', 'on_record', 'on_record', {
      reason: 'You shared your solution to Week 5 problem C in the class group during the contest window.',
      actor: { id: 3, name: 'Ravi Kumar' },
      confirmer: null,
    }),
    line(38, 6, 5, 85, 'rollover', 'on_record', 'on_record', { semester_label: '2026 monsoon' }),
    line(35, 8, -15, 80, 'outcome', 'good', 'on_record', {
      reason: 'Your program for problem B printed fixed answers for the hidden tests instead of solving them.',
      case_id: 12,
      decision_id: 31,
      actor: { id: 3, name: 'Ravi Kumar' },
      confirmer: { id: 2, name: 'Arjun Mehta' },
    }),
    line(31, 30, -5, 95, 'outcome', 'good', 'good', { reason: 'You left a debugging print that read the test file.', case_id: 8, decision_id: 20, actor: { id: 1, name: 'Priya Raman' }, confirmer: { id: 2, name: 'Arjun Mehta' } }),
  ];
}

export interface Options {
  who?: 'admin' | 'owner' | 'student';
  /** A band-lowering change waits: proposed by someone else (the viewer may confirm), or by the viewer. */
  pending?: 'theirs' | 'mine';
  /** The record is the viewer's own: 403. */
  own?: boolean;
}

export async function mockApi(page: Page, options: Options = {}) {
  const now = Date.now();
  const me = options.who === 'owner' ? OWNER : options.who === 'student' ? STUDENT : ADMIN;
  const state = {
    rating: 75,
    history: history(now),
    pending: null as Json | null,
    ratingPuts: [] as Json[],
    confirms: [] as { pid: number; kind: string; body: Json | null }[],
    rulings: [] as Json[],
    studentQueries: [] as string[],
    recordGets: 0,
    appeals: [
      {
        id: 70,
        case_id: null,
        adjustment_id: 40,
        user_id: 7,
        text: 'I posted in the group after the window closed. The timestamps in the group show 18:40, after the end at 18:30.',
        created_at: now - 0.5 * DAY,
        decide_by: now + 13.5 * DAY,
        overdue: false,
        state: 'open',
        ruling: null,
        user: { name: 'Asha Rao', email: 'asha@college.edu', roll_number: '' },
      },
      {
        id: 66,
        case_id: 12,
        adjustment_id: null,
        user_id: 7,
        text: 'The fixed answers were a debugging branch I forgot to delete; the general solution is below it.',
        created_at: now - 7 * DAY,
        decide_by: now - 1 * DAY,
        overdue: true,
        state: 'open',
        ruling: null,
        user: { name: 'Asha Rao', email: 'asha@college.edu', roll_number: '' },
      },
    ] as Json[],
  };
  if (options.pending) {
    state.pending = {
      id: 5,
      from_tenths: 75,
      to_tenths: 55,
      reason: 'A second outcome this semester was confirmed in Week 6, so the standing drops a band.',
      by: options.pending === 'mine' ? { id: me.id, name: me.name } : { id: 3, name: 'Ravi Kumar' },
      created_at: now - 3_600_000,
      may_confirm: options.pending === 'theirs',
    };
  }

  const band = (t: number) => (t >= 90 ? 'good' : t >= 60 ? 'on_record' : 'repeated');
  const rating = () => ({ value_tenths: state.rating, band: band(state.rating), history: state.history });

  const record = () => ({
    student: { id: 7, name: 'Asha Rao', email: 'asha@college.edu', disabled: false },
    standing: {
      band: band(state.rating),
      band_name: { good: 'Good standing', on_record: 'Outcomes on record', repeated: 'Repeated outcomes' }[band(state.rating)],
      lines: [
        {
          id: 'adjustment-40',
          kind: 'adjustment',
          at: now - 1 * DAY,
          text: 'Standing adjusted by an organiser.',
          reason: 'You shared your solution to Week 5 problem C in the class group during the contest window.',
          case_id: null,
          adjustment_id: 40,
          appeal: { state: 'open', created_at: now - 0.5 * DAY },
          appeal_by: now + 13 * DAY,
          can_appeal: false,
        },
        {
          id: 'outcome-31-7',
          kind: 'outcome',
          at: now - 8 * DAY,
          text: 'Week 3 contest: zero for problem B.',
          reason: 'Your program for problem B printed fixed answers for the hidden tests instead of solving them.',
          case_id: 12,
          adjustment_id: null,
          appeal: { state: 'open', created_at: now - 7 * DAY },
          appeal_by: now + 6 * DAY,
          can_appeal: false,
        },
      ],
    },
    rating: rating(),
    pending_rating: state.pending,
    cases: [
      { id: 12, contest_id: 3, contest_title: 'Week 3 contest', state: 'actioned', source: 'manual', outcome: 'zero_problem', created_at: now - 12 * DAY, decided_at: now - 8 * DAY },
      { id: 8, contest_id: 2, contest_title: 'Week 2 contest', state: 'cleared', source: 'signals', outcome: '', created_at: now - 40 * DAY, decided_at: now - 35 * DAY },
    ],
    notes: [
      {
        id: 4,
        submission_id: 80,
        by: 3,
        by_name: 'Ravi Kumar',
        text: 'Lines 7 to 9 print the sample answers when the input matches them.',
        lines: [{ from: 7, to: 9 }],
        created_at: now - 2 * DAY,
        expires_at: now + 28 * DAY,
      },
    ],
    appeals: state.appeals,
    server_now: Date.now(),
  });

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();

      if (path === '/api/me' && method === 'GET') return json(route, 200, { ...me, notifications_unread: 0, hide_progress: false });
      if (path === '/api/auth/config') return json(route, 200, { mode: 'production', allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' });
      if (path === '/api/events') return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
      if (path === '/api/contests' && method === 'GET') return json(route, 200, { live: [], upcoming: [], past: [], server_now: Date.now() });
      if (path === '/api/integrity/cases' && method === 'GET') return json(route, 200, { cases: [], contests: {}, problems: {}, server_now: Date.now() });
      if (path === '/api/admin/contests' && method === 'GET') return json(route, 200, { contests: [], server_now: Date.now() });

      if (path.startsWith('/api/admin/') && me.role === 'student') return apiError(route, 403, 'forbidden', 'You do not have access to this. Ask an owner if you need it.');

      if (path === '/api/admin/students' && method === 'GET') {
        state.studentQueries.push(url.search);
        const filter = url.searchParams.get('filter') ?? '';
        const q = (url.searchParams.get('q') ?? '').toLowerCase();
        let rows = studentRows(now);
        if (filter === 'missed_last_two') rows = rows.filter((r) => r.id === 10);
        if (filter === 'no_accepted') rows = rows.filter((r) => r.id === 9);
        if (q) rows = rows.filter((r) => String(r.name).toLowerCase().includes(q) || String(r.email).includes(q));
        rows = rows.map((r) => (r.id === 7 ? { ...r, rating_tenths: state.rating } : r));
        return json(route, 200, { students: rows, filter, last_graded: filter === 'no_accepted' ? LAST_GRADED.slice(0, 1) : LAST_GRADED, server_now: Date.now() });
      }
      if (path === '/api/admin/students/7' && method === 'GET') return json(route, 200, studentDetail(now));
      if (path === '/api/admin/students/7/record' && method === 'GET') {
        state.recordGets++;
        if (options.own) return apiError(route, 403, 'forbidden', 'Nobody opens their own record. Another owner or admin can.');
        return json(route, 200, record());
      }
      if (/^\/api\/admin\/students\/\d+(\/record)?$/.test(path)) return apiError(route, 404, 'not_found', 'There is no such student.');

      if (path === '/api/admin/students/7/rating' && method === 'PUT') {
        const body = req.postDataJSON() as { value_tenths: number; reason: string };
        state.ratingPuts.push(body);
        if ([...String(body.reason ?? '').trim()].length < 20) return apiError(route, 400, 'bad_request', 'Write the reason the student will read: at least 20 characters (at most 1,000).');
        const from = state.rating;
        const order: Record<string, number> = { good: 2, on_record: 1, repeated: 0 };
        if (order[band(body.value_tenths)] < order[band(from)]) {
          state.pending = { id: 6, from_tenths: from, to_tenths: body.value_tenths, reason: body.reason, by: { id: me.id, name: me.name }, created_at: Date.now(), may_confirm: false };
          return json(route, 202, { rating: rating(), pending_rating: state.pending });
        }
        state.history = [
          { id: 50, at: Date.now(), delta_tenths: body.value_tenths - from, value_tenths: body.value_tenths, cause: 'manual', reason: body.reason, case_id: null, decision_id: null, appeal_id: null, actor: { id: me.id, name: me.name }, confirmer: null, semester_label: '', band_before: band(from), band_after: band(body.value_tenths) },
          ...state.history,
        ];
        state.rating = body.value_tenths;
        state.pending = null;
        return json(route, 200, { rating: rating(), pending_rating: null });
      }
      const decide = path.match(/^\/api\/admin\/students\/7\/rating\/proposals\/(\d+)\/(confirm|decline)$/);
      if (decide && method === 'POST') {
        const body = (req.postData() ? req.postDataJSON() : null) as Json | null;
        state.confirms.push({ pid: Number(decide[1]), kind: decide[2], body });
        const p = state.pending;
        if (!p || Number(decide[1]) !== p.id) return apiError(route, 404, 'not_found', 'There is no such change waiting.');
        if (decide[2] === 'confirm') {
          if (!p.may_confirm && !(body && body.sole_organiser_reason)) {
            return apiError(route, 403, 'two_person_rule', 'A second organiser who did not propose this change confirms it.');
          }
          const to = p.to_tenths as number;
          state.history = [
            { id: 51, at: Date.now(), delta_tenths: to - state.rating, value_tenths: to, cause: 'manual', reason: p.reason, case_id: null, decision_id: null, appeal_id: null, actor: p.by, confirmer: { id: me.id, name: me.name }, semester_label: '', band_before: band(state.rating), band_after: band(to) },
            ...state.history,
          ];
          state.rating = to;
        }
        state.pending = null;
        return json(route, 200, { rating: rating(), pending_rating: null });
      }
      const ruling = path.match(/^\/api\/admin\/appeals\/(\d+)\/ruling$/);
      if (ruling && method === 'POST') {
        const body = req.postDataJSON() as { upheld: boolean; reason: string };
        state.rulings.push({ id: Number(ruling[1]), ...body });
        const a = state.appeals.find((x) => x.id === Number(ruling[1]));
        if (!a) return apiError(route, 404, 'not_found', 'There is no such appeal.');
        if (Number(ruling[1]) === 66) return apiError(route, 403, 'uninvolved_reviewer', 'A reviewer who took no part in the decision rules on the appeal.');
        a.state = body.upheld ? 'upheld' : 'rejected';
        a.ruling = { by: me.id, by_name: me.name, upheld: body.upheld, reason: body.reason, created_at: Date.now() };
        if (body.upheld) state.rating = 85;
        return json(route, 200, a);
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
    name: 'students',
    path: '/admin/students',
    options: {},
    title: 'Students · podium',
    ready: (page) => expect(page.getByRole('table', { name: /^Students, sorted by name/ })).toBeVisible(),
  },
  {
    name: 'student',
    path: '/admin/students/7',
    options: {},
    title: 'Asha Rao · Student · podium',
    ready: async (page) => {
      await expect(page.getByRole('img', { name: /^Score as a percentage over 4 graded contests/ })).toBeVisible();
      await expect(page.getByRole('table', { name: 'Contests Asha Rao took part in, newest first' })).toBeVisible();
    },
  },
  {
    name: 'record',
    path: '/admin/students/7/record',
    options: {},
    title: 'Asha Rao · Record · podium',
    ready: (page) => expect(page.getByRole('heading', { level: 2, name: 'Rating history' })).toBeVisible(),
  },
  {
    name: 'record-pending',
    path: '/admin/students/7/record',
    options: { pending: 'theirs' },
    title: 'Asha Rao · Record · podium',
    ready: (page) => expect(page.getByRole('heading', { level: 3, name: 'Waiting for a second organiser' })).toBeVisible(),
  },
  {
    name: 'students-forbidden',
    path: '/admin/students',
    options: { who: 'student' },
    title: 'Students · podium',
    ready: (page) => expect(page.getByText('Only organisers can see the students.')).toBeVisible(),
  },
];
