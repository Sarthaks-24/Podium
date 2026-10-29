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
  /** Interval covering came from the AI wizard: the contest asks for a reason. */
  aiGate?: boolean;
  phase?: Phase;
  secondSession?: boolean;
  holdReveal?: boolean;
  /** The server hides each submission's result from the student (state "submitted", `hidden`). */
  hideResults?: boolean;
  /** After the end, students still get the frozen board (held for the reveal). */
  studentFrozen?: boolean;
  /** The student's window ends this many ms after the page loads. */
  endsInMs?: number;
  /** Whether the organiser's contest starts out published (default true: an unpublished contest past its start is a draft, C2-015). */
  published?: boolean;
  /** GET of a problem's workspace answers 403 with this code and Go's sentence (contests.go contestError, C2-017). */
  problemRefusal?: 'not_started' | 'contest_over';
  /** Students saw this contest's hidden tests in practice (D115), so its problems stay. */
  shownHere?: boolean;
  /** Bank problems whose hidden tests a contest showed in practice (D115, C1-015). */
  shownInPractice?: number[];
  /** A second live contest in the student's list, under the pinned one (C1-031). */
  secondLive?: boolean;
  /** PUT settings may publish (by default it refuses with a changed problem, P3). */
  publishOk?: boolean;
  /** Queue health on the contest response (C10): calm by default, or a slow queue, and a silent judge. */
  queueHealth?: 'calm' | 'trouble';
  /** GET /api/contests lists past contests only, the newest one the student never entered (D324). */
  missedNewest?: boolean;
  /** Problem B moved to a new revision after the start, changing its statement (C3). */
  statementUpdated?: boolean;
  /** Problem A has a newer validated revision to update the contest to (C4). */
  updateAvailable?: boolean;
  /** A review decision set Asha's problem B to zero (C9, D77). */
  zeroB?: boolean;
  /** The contest's results are not final yet: the class view answers 409 not_final. */
  notFinal?: boolean;
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

/** A submission whose result the server hides: everything withheld (m4-api-deviations.md). */
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

/** The program the notes route sends with submission 301; one line holds a tag, which must show as text. */
export const NOTES_SOURCE = '#include <cstdio>\nint main() {\n  long long a, b;\n  scanf("%lld %lld", &a, &b);\n  printf("%lld\\n", a + b); // <b>sum</b>\n}\n';

export async function mockApi(page: Page, options: Options = {}) {
  await longLivedEvents(page);
  const now = nowMs();
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
    /** Draft saves on the contest's workspace routes (P5 sign-out guard). */
    draftPuts: [] as Json[],
    /** The last draft saved per problem label, served back like the server does. */
    drafts: {} as Record<string, Json>,
    runs: [] as Json[],
    asked: [] as Json[],
    nextId: 40,
    // Organiser side.
    settingsPuts: [] as Json[],
    problemPuts: [] as unknown[],
    pauses: 0,
    resumes: 0,
    extends: [] as Json[],
    extensions: [] as { user: string; body: Json }[],
    rejudges: [] as Json[],
    posted: [] as Json[],
    answers: [] as { id: string; body: Json }[],
    paused: false,
    adminProblems: [{ label: 'A', problem_id: 11, title: 'A plus B', ready: true, max_score: 100 }] as Json[],
    holdReveal: false,
    revealedAt: null as number | null,
    releases: 0,
    adminDeletes: [] as Json[],
    feedQueries: [] as string[],
    creates: [] as Json[],
    aiGate: options.aiGate ?? false,
    publicClarifications: [] as Json[],
    statementAt: options.statementUpdated ? now - STATEMENT_CHANGED_MIN * MIN : (null as number | null),
    revisionQueries: [] as string[],
    pinPuts: [] as { label: string; body: Json }[],
    pinnedRev: { A: options.updateAvailable ? 2 : 3 } as Record<string, number>,
    auditQueries: [] as string[],
    notes: [
      { id: 19, submission_id: 301, by: 3, by_name: 'Ravi Kumar', text: 'The loop matches the editorial draft closely.', lines: [{ from: 8, to: 10 }], created_at: Date.now() - 3_600_000, expires_at: Date.now() + 29 * 86_400_000 },
    ] as Json[],
    notePosts: [] as Json[],
    casePosts: [] as Json[],
    manualCase: null as number | null,
    /** Questions students asked after the page loaded, shown in the console (stage C2). */
    newQuestions: [] as Json[],
    /** The judge stops asking for work: its last request is 3 minutes old (stage C2). */
    judgeLate: false,
    /** The contest has closed, as on the server: extend answers 409 contest_ended (walk bug B3). */
    closed: phase === 'after' || phase === 'practice',
    /** Standings requests on the student route, with their query (Student view asks as=student, P10). */
    boardQueries: [] as string[],
  };

  state.holdReveal = options.holdReveal ?? false;
  if (phase === 'after' || phase === 'practice') {
    state.submissions.push(submission(30, 11, 'done', 100, startsAt + 20 * MIN), submission(31, 12, 'done', 40, startsAt + 50 * MIN));
  }

  const myWindow = () => {
    if (options.endsInMs !== undefined) return { started_at: startsAt, ends_at: now + options.endsInMs };
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
    problem_count: 3,
    state: contestState(),
    my_score: phase === 'after' || phase === 'practice' ? 140 : null,
    max_score: 300,
    my_window: state.who === 'admin' ? null : myWindow(),
  });

  const contest = () => ({
    ...summary(),
    id: 4,
    description_md: 'Three problems on sums and intervals. Good luck.',
    rules: {
      min_gap_s: 30,
      max_per_problem: 15,
      languages: ['cpp17', 'python3', 'java21'],
      freeze_minutes: 30,
    },
    honour_text:
      'This contest counts. By starting you agree to these rules:\n\n- Work alone.\n- Do not use AI assistants.\n- Podium records your submissions, the characters you type and paste, and when you leave the page. It is deleted 180 days after the contest.',
    problems:
      state.accepted || state.who === 'admin' || phase === 'after' || phase === 'practice'
        ? PROBLEMS.map((p) => ({
            label: p.label,
            title: p.title,
            my_score: state.scores[p.label],
            max_score: p.max,
            my_submission_count: state.submissions.filter((s) => s.problem_id === p.id).length,
            revision_id: 10 + p.id,
            statement_updated_at: p.label === 'B' ? state.statementAt : null,
            ...(options.zeroB && p.label === 'B' ? { my_score: 0, zero_by_decision: true, zero_case_id: 12 } : {}),
          }))
        : null,
    accepted: state.who === 'admin' ? false : state.accepted,
    organiser: state.who === 'admin',
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
      draft: state.drafts[label] ?? null,
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
    published: options.published ?? true,
    practice_after_end: true,
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
    queue: options.queueHealth === 'trouble' ? { waiting: 14, judging: 2, errors: 1, oldest_wait_s: 95 } : { waiting: 0, judging: 1, errors: 1, oldest_wait_s: 0 },
    judge_last_claim_at: options.queueHealth === 'trouble' || state.judgeLate ? nowMs() - 3 * MIN : nowMs() - 4000,
    unanswered: state.answers.length ? 0 : 1,
    close_at: endsAt,
    default_honour_text: 'This contest counts. By starting you agree to these rules.',
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
    };
  }

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
        if (options.missedNewest) {
          return json(route, 200, {
            live: [],
            upcoming: [],
            past: [
              { ...summary(), slug: 'week-8', title: 'Week 8 contest', state: 'practice', starts_at: now - 86_400_000, ends_at: now - 86_400_000 + 2 * 3_600_000, my_score: null, my_window: null },
              { ...summary(), slug: 'week-5', title: 'Week 5 contest', state: 'ended', starts_at: now - 7 * 86_400_000, ends_at: now - 7 * 86_400_000 + 2 * 3_600_000, my_score: 240, my_window: null },
            ],
            server_now: nowMs(),
          });
        }
        return json(route, 200, {
          live:
            phase === 'ready' || phase === 'during'
              ? [summary(), ...(options.secondLive ? [{ ...summary(), slug: 'lab-2', title: 'Lab 2' }] : [])]
              : [],
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
        if (body.accept_honour !== true) return apiError(route, 400, 'bad_request', 'Read the rules and agree to them to start.', { fields: { accept_honour: 'Read the rules and agree to them to start.' } });
        state.accepted = true;
        return json(route, 200, contest());
      }
      if (path === '/api/contests/week-6/standings' && method === 'GET') {
        state.boardQueries.push(url.search);
        const updated = options.zeroB ? { board_updated_at: now - 3_600_000, board_update_reason: 'review_decision' } : {};
        return json(route, 200, { ...(phase === 'during' || phase === 'ready' || options.studentFrozen ? frozenBoard() : fullBoard()), ...updated });
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
        if (!prob[2] && method === 'GET' && options.problemRefusal) {
          const refusals = {
            not_started: 'Accept the rules on the contest page to start.',
            contest_over: "Your time is up. Practice opens when every student's time is up.",
          };
          return apiError(route, 403, options.problemRefusal, refusals[options.problemRefusal]);
        }
        if (!prob[2] && method === 'GET') return json(route, 200, workspace(label));
        if (prob[3] === 'draft' && method === 'PUT') {
          const body = req.postDataJSON() as Json;
          state.draftPuts.push(body);
          state.drafts[label] = { language: body.language, source: body.source, updated_at: nowMs() };
        }
        if (prob[3] === 'draft' || prob[3] === 'events') return json(route, 204);
        if (prob[3] === 'run') {
          state.runs.push({ ...(req.postDataJSON() as Json), label });
          return json(route, 202, { id: 70, state: 'queued', queue_position: 1, compile_output: '', results: [], server_now: nowMs() });
        }
        if (prob[3] === 'submit') {
          const body = req.postDataJSON() as Json;
          state.submits.push({ ...body, label });
          const s = options.hideResults ? hidden(state.nextId++, p.id) : submission(state.nextId++, p.id, 'queued');
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
      if (path.startsWith('/api/admin/') && me.role === 'student') return apiError(route, 403, 'forbidden', 'You do not have access to this. Ask an owner if you need it.');
      if (path === '/api/admin/problems' && method === 'GET') {
        const shown = (p: Json) => ({ ...p, shown_in_practice: (options.shownInPractice ?? []).includes(Number(p.id)) });
        return json(route, 200, {
          problems: ([
            { id: 11, slug: 'a-plus-b', title: 'A plus B', difficulty: 'easy', tags: [], origin: 'import', archived: false, test_count: 6, points: 100, validated: true, validated_at: now, updated_at: now },
            { id: 12, slug: 'interval-covering', title: 'Interval covering', difficulty: 'medium', tags: [], origin: 'import', archived: false, test_count: 15, points: 100, validated: true, validated_at: now, updated_at: now },
            { id: 14, slug: 'draft-problem', title: 'Unfinished draft', difficulty: '', tags: [], origin: 'manual', archived: false, test_count: 0, points: 0, validated: false, validated_at: null, updated_at: now },
          ] as Json[]).map(shown),
        });
      }
      if (path === '/api/admin/contests' && method === 'GET') {
        return json(route, 200, {
          contests: [
            { ...adminContest(), published: true, problems: PROBLEMS.map((p) => ({ label: p.label, problem_id: p.id, title: p.title, ready: true, max_score: 100 })) },
            { ...adminContest(), id: 5, slug: 'week-7', title: 'Week 7 contest', starts_at: now + 3 * 86_400_000, ends_at: now + 3 * 86_400_000 + 2 * 3_600_000, close_at: now + 3 * 86_400_000 + 2 * 3_600_000, state: 'upcoming', published: false, problems: [] },
          ],
          server_now: nowMs(),
        });
      }
      if (path === '/api/admin/contests' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.creates.push(body);
        if (Number(body.ends_at) - Number(body.starts_at) > 14 * 86_400_000) {
          return apiError(route, 400, 'bad_request', 'A contest can last at most 14 days.', { fields: { ends_at: 'A contest can last at most 14 days.' } });
        }
        // Go derives the slug from the title (contests.Slugify), numbered when taken.
        const slug = String(body.title).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
        return json(route, 201, { ...adminContest(), id: 9, title: body.title, slug: slug === 'week-6-contest' ? `${slug}-2` : slug });
      }
      if (path === '/api/admin/contests/4' && method === 'GET') return json(route, 200, adminContest());
      if (path === '/api/admin/contests/4' && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.settingsPuts.push(body);
        if (String(body.title).length > 120) return apiError(route, 400, 'bad_request', 'Enter a title of at most 120 characters.', { fields: { title: 'Enter a title of at most 120 characters.' } });
        // Go refuses publishing (not saving an already published contest) with 400 and fields (contests.go contestError).
        const stale = 'Problem B changed or lost its validation since it was added. Validate it and add it again.';
        if (body.published === true && options.published === false && !options.publishOk) return apiError(route, 400, 'bad_request', stale, { fields: { published: stale } });
        // Update keeps the slug (V3-15).
        return json(route, 200, { ...adminContest(), ...body, slug: 'week-6' });
      }
      if (path === '/api/admin/contests/4/problems' && method === 'PUT') {
        const body = req.postDataJSON() as { problem_id: number; label: string; ai_reason?: string }[];
        state.problemPuts.push(body);
        // contests/store.go (C2-014): a published contest that showed hidden tests keeps its problems.
        const dropped = state.adminProblems.find((p) => !body.some((b) => b.problem_id === p.problem_id));
        if (options.shownHere && options.published !== false && phase !== 'upcoming' && dropped) {
          const why = `Problem ${dropped.label} stays in this contest: students have seen its hidden tests here in practice, and the problem bank shows that from this contest.`;
          return apiError(route, 400, 'bad_request', why, { fields: { problems: why } });
        }
        const unconfirmed = body.filter((b) => state.aiGate && b.problem_id === 12 && (b.ai_reason ?? '').length < 10);
        if (unconfirmed.length > 0) {
          return apiError(route, 409, 'ai_unconfirmed', `Problem ${unconfirmed[0].label} came from the AI wizard and no person has written a solution for it.`, {
            problems: unconfirmed.map((b) => b.label),
          });
        }
        state.adminProblems = body.map((b) => ({ ...b, title: PROBLEMS.find((p) => p.id === b.problem_id)?.title ?? 'Problem', ready: true, max_score: 100 }));
        return json(route, 200, adminContest());
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
        // Go answers 200 with the contest (docs/API.md), not 204.
        return json(route, 200, adminContest());
      }
      if (path === '/api/admin/contests/4/resume' && method === 'POST') {
        state.resumes++;
        state.paused = false;
        return json(route, 200, adminContest());
      }
      if (path === '/api/admin/contests/4/extend' && method === 'POST') {
        if (state.closed) return apiError(route, 409, 'contest_ended', 'The contest has ended, so it cannot be extended. Extending it now would reopen it for every student.');
        state.extends.push(req.postDataJSON() as Json);
        return json(route, 200, adminContest());
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
          { id: 301, user_id: 7, name: 'Asha Rao', email: 'asha@college.edu', problem_label: 'B', language: 'cpp17', state: 'done', verdict: 'WA', score: 40, max_score: 100, created_at: now - 2 * MIN, practice: false, rejudged: false, note_count: state.notes.length, case_id: state.manualCase },
          { id: 300, user_id: 101, name: 'Arjun Mehta', email: 'arjun@college.edu', problem_label: 'A', language: 'python3', state: 'done', verdict: 'AC', score: 100, max_score: 100, created_at: now - 5 * MIN, practice: false, rejudged: false, note_count: 0, case_id: null },
          { id: 299, user_id: 102, name: 'Ravi Kumar', email: 'ravi@college.edu', problem_label: 'C', language: 'java21', state: 'running', verdict: '', score: 0, max_score: 100, created_at: now - 6 * MIN, practice: false, rejudged: false, note_count: 0, case_id: null },
          { id: 298, user_id: 103, name: 'Meera Iyer', email: 'meera@college.edu', problem_label: 'B', language: 'cpp17', state: 'error', verdict: '', score: 0, max_score: 100, created_at: now - 7 * MIN, practice: false, rejudged: false, note_count: 0, case_id: null },
          { id: 297, user_id: 104, name: 'Kiran Das', email: 'kiran@college.edu', problem_label: 'C', language: 'cpp17', state: 'done', verdict: 'WA', score: 0, max_score: 100, created_at: now - 9 * MIN, practice: false, rejudged: false, note_count: 0, case_id: null },
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
            ...state.newQuestions,
          ],
        });
      }
      const clar = path.match(/^\/api\/admin\/contests\/4\/clarifications\/(\d+)$/);
      if (clar && method === 'PUT') {
        state.answers.push({ id: clar[1], body: req.postDataJSON() as Json });
        return json(route, 200, {});
      }
      if (path === '/api/admin/contests/4/standings' && method === 'GET') {
        const updated = options.zeroB ? { board_updated_at: now - 3_600_000, board_update_reason: 'review_decision' } : { board_updated_at: null, board_update_reason: null };
        return json(route, 200, url.searchParams.get('variant') === 'frozen' ? { ...frozenBoard(), me: null, ...updated } : { ...fullBoard(), me: null, final: true, ...updated });
      }
      if (path === '/api/admin/contests/4/class' && method === 'GET') {
        if (options.notFinal) return apiError(route, 409, 'not_final', 'The class view opens when the results are final.');
        return json(route, 200, {
          contest_id: 4,
          participants: 30,
          problems: [
            { label: 'A', title: 'A plus B', max_score: 100, attempted: 27, full: 21, median_score: 100, subtasks: [{ idx: 0, points: 40, solved: 24, solve_rate: 80 }, { idx: 1, points: 60, solved: 21, solve_rate: 70 }], most_failed_test: { subtask_idx: 1, test: 3, failed: 5 } },
            { label: 'B', title: 'Interval covering', max_score: 100, attempted: 19, full: 4, median_score: 40, subtasks: [{ idx: 0, points: 40, solved: 12, solve_rate: 40 }, { idx: 1, points: 60, solved: 1, solve_rate: 13.3 }], most_failed_test: null },
          ],
          server_now: nowMs(),
        });
      }
      const notes = path.match(/^\/api\/admin\/submissions\/(\d+)\/notes$/);
      if (notes && method === 'GET') {
        if (notes[1] !== '301') return json(route, 200, { submission: { id: Number(notes[1]) }, notes: [], case_id: null, server_now: nowMs() });
        return json(route, 200, { submission: { id: 301, contest_id: 4, user_id: 7, problem_label: 'B', language: 'cpp17', practice: false, created_at: now - 2 * MIN }, source: NOTES_SOURCE, notes: state.notes, case_id: state.manualCase, server_now: nowMs() });
      }
      if (notes && method === 'POST') {
        const body = req.postDataJSON() as { text: string; lines: { from: number; to: number }[] };
        state.notePosts.push(body);
        const note = { id: 20 + state.notes.length, submission_id: Number(notes[1]), by: 2, by_name: 'Arjun Mehta', text: body.text, lines: body.lines, created_at: Date.now(), expires_at: Date.now() + 30 * 86_400_000 };
        state.notes.push(note);
        return json(route, 201, note);
      }
      const manual = path.match(/^\/api\/admin\/submissions\/(\d+)\/case$/);
      if (manual && method === 'POST') {
        const body = req.postDataJSON() as { title: string; note: string };
        state.casePosts.push(body);
        if ([...body.note.trim()].length < 20) return apiError(route, 400, 'bad_request', 'Write a reason of at least 20 characters (at most 4,000).');
        state.manualCase = 14;
        state.notes = state.notes.map((n) => ({ ...n, expires_at: null }));
        return json(route, 201, { id: 14, contest_id: 4, state: 'open', title: body.title, source: 'manual', submission_id: Number(manual[1]), evidence: [], users: [], decisions: [], created_at: Date.now(), updated_at: Date.now() });
      }
      if (path === '/api/admin/contests/4/stats' && method === 'GET') {
        return json(route, 200, {
          started: 30,
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
          // v1.4.0: the row names the student by id and says only whether a reason was written.
          ...state.extensions.map((x) => ({ action: 'extension', details: { user_id: Number(x.user), minutes: x.body.minutes, reason_given: String(x.body.reason ?? '').trim() !== '' } })),
          ...(options.zeroB ? [{ action: 'board_adjusted', details: { decision_id: 31, problem_label: 'B', user_id: 7, rank_before: 2, total_before: 240, score_before: 40, reversed: false } }] : []),
          ...state.rejudges.map((r) => ({ action: 'rejudge', details: { ...r, queued: 1 } })),
          ...state.pinPuts.map((x) => ({ action: 'revision', details: { label: x.label, from_rev: 2, to_rev: 3, statement_changed: true, judging_changed: false } })),
        ].map((e, i) => ({ id: i + 1, actor, created_at: now, ...e }));
        const want = url.searchParams.get('action') ?? '';
        const who = (url.searchParams.get('actor') ?? '').toLowerCase();
        const shown = entries.filter((e) => (want === '' || e.action === want) && (who === '' || e.actor.email.includes(who)));
        return json(route, 200, { entries: shown.reverse(), page: 1, has_more: false, server_now: nowMs() });
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
      // The language's highlighting is its own chunk (P5 F7.8); wait for it.
      await expect(page.locator('.cm-content')).toHaveAttribute('data-language', /.+/);
      await expect(page.locator('.statement .katex').first()).toBeVisible();
    },
  },
  {
    name: 'contest-after',
    path: '/contests/week-6',
    options: { phase: 'practice' },
    title: 'Week 6 contest · podium',
    ready: async (page) => {
      await expect(page.locator('.md :is(h3, h4)', { hasText: 'A plus B' })).toBeVisible({ timeout: 10_000 });
      // The place and the newest submission arrive after the page (S9).
      await expect(page.locator('.result .place-figure')).toBeVisible();
      await expect(page.locator('details[open] .list')).toBeVisible();
    },
  },
  {
    name: 'standings-frozen',
    path: '/contests/week-6/standings',
    options: { phase: 'during' },
    title: 'Standings · Week 6 contest · podium',
    ready: (page) => expect(page.locator('tr.me')).toBeVisible(),
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
    ready: (page) => expect(page.getByRole('heading', { name: 'Clone' })).toBeVisible(),
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
