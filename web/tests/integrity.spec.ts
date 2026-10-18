// Screens and flows for the integrity milestone (cache/v1/integrity-plan.md
// sections 5 and 7). Every /api/ call is answered by an in-test mock built
// from the API contract in section 7, one per page. Spans in the mocked
// pairs are UTF-8 byte offsets computed here with TextEncoder, and the
// sources carry multibyte text before and inside the spans, so a wrong
// conversion shows up as a wrong highlight.
//
// The sweep saves screenshots to cache/screenshots-integrity/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1, the
// title and no horizontal overflow. The flows cover running a check,
// filtering signals, the pair view, opening, sharing and deciding a case,
// and the student's notice, evidence and explanation.
import { expect, test, type Page, type Route } from '@playwright/test';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-integrity/`;
}
const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;
const MIN = 60_000;

type Json = Record<string, unknown>;

const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };
const ADMIN = { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', avatar_url: '', role: 'admin', theme: '', verdicts: 'standard' };

const USERS: Record<string, { name: string; email: string; roll_number: string }> = {
  '7': { name: 'Asha Rao', email: 'asha@college.edu', roll_number: 'CS21-007' },
  '8': { name: 'Ravi Kumar', email: 'ravi@college.edu', roll_number: 'CS21-008' },
  '9': { name: 'Zoë Fernandes', email: 'zoe@college.edu', roll_number: 'CS21-009' },
  '10': { name: 'Meera Iyer', email: 'meera@college.edu', roll_number: 'CS21-010' },
  '11': { name: 'Kabir Shah', email: 'kabir@college.edu', roll_number: 'CS21-011' },
  '12': { name: 'Neha Gupta', email: 'neha@college.edu', roll_number: 'CS21-012' },
  '13': { name: 'Rohan Das', email: 'rohan@college.edu', roll_number: 'CS21-013' },
  '14': { name: 'Sara Thomas', email: 'sara@college.edu', roll_number: 'CS21-014' },
};
const PROBLEMS = { '11': { label: 'A', title: 'A plus B' }, '12': { label: 'B', title: 'Interval covering' } };

const SOURCE_A = [
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

const SOURCE_B = [
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

const utf8 = (s: string) => new TextEncoder().encode(s).length;

/** Byte offsets of `from` up to the end of `to` in `source`. */
function span(source: string, from: string, to: string): { start: number; end: number } {
  const i = source.indexOf(from);
  const j = source.indexOf(to, i) + to.length;
  return { start: utf8(source.slice(0, i)), end: utf8(source.slice(0, j)) };
}

const SPANS_A = [span(SOURCE_A, 'vector<pair', 'p.first;'), span(SOURCE_A, 'sort(v', 'count++; }')];
const SPANS_B = [span(SOURCE_B, 'vector<pair', 's.first;'), span(SOURCE_B, 'sort(seg', 'ans++; }')];

function signals(): Json[] {
  return [
    { id: 31, problem_id: 12, kind: 'similar', strength: 'strong', score: 0.94, user_a: 7, user_b: 8, submission_a: 301, submission_b: 302, evidence: { shared: 41, spans_a: SPANS_A, spans_b: SPANS_B, language_a: 'cpp17', language_b: 'cpp17' } },
    { id: 34, problem_id: 12, kind: 'hardcoded', strength: 'strong', score: 2, user_a: 12, submission_a: 310, evidence: { tests: [7, 9] } },
    { id: 32, problem_id: 12, kind: 'similar', strength: 'moderate', score: 0.78, user_a: 8, user_b: 9, submission_a: 302, submission_b: 303, evidence: { shared: 30, spans_a: [], spans_b: [], language_a: 'cpp17', language_b: 'java21' } },
    { id: 33, problem_id: 11, kind: 'wrong_output', strength: 'moderate', score: 3, user_a: 10, user_b: 11, evidence: { tests: [3, 5, 7] } },
    { id: 35, problem_id: 11, kind: 'probing', strength: 'moderate', score: 8, user_a: 13, evidence: { distinct: 8, attempts: 14 } },
    { id: 36, problem_id: 11, kind: 'similar', strength: 'weak', score: 0.62, user_a: 10, user_b: 14, submission_a: 320, submission_b: 321, evidence: { shared: 12, spans_a: [], spans_b: [], language_a: 'python3', language_b: 'python3' } },
  ];
}

interface Options {
  who?: 'student' | 'admin';
  /** The contest is still live. */
  live?: boolean;
  /** Students started at different times. */
  window?: boolean;
  /** The contest was never checked before the test. */
  unchecked?: boolean;
  /** The student's case was decided. */
  decided?: boolean;
  /** The student has no cases. */
  noCases?: boolean;
}

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

async function mockApi(page: Page, options: Options = {}) {
  const now = Date.now();
  const endsAt = options.live ? now + 40 * MIN : now - 2 * 3_600_000;
  const state = {
    who: options.who ?? 'admin',
    run: options.unchecked ? null : ({ state: 'done' } as { state: string } | null),
    runGets: 0,
    runPosts: 0,
    caseBodies: [] as Json[],
    puts: [] as Json[],
    explanations: [] as Json[],
    kase: null as Json | null,
  };

  const run = () => ({
    id: 5,
    state: state.run?.state ?? 'done',
    started_at: now - 3 * MIN,
    finished_at: state.run?.state === 'running' ? null : now - 2 * MIN,
    stats: { students: 30, submissions: 84, problems: 2, signals: { similar: 3, wrong_output: 1, hardcoded: 1, probing: 1 }, clusters: 2 },
    signals: state.run?.state === 'running' ? [] : signals(),
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
    start_mode: options.window ? 'window' : 'fixed',
    duration_minutes: options.window ? 60 : 0,
    state: options.live ? 'live' : 'ended',
    published: true,
    problems: [],
    admins: [],
    extensions: [],
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
      { user_id: 7, name: 'Asha Rao', shared_at: null, explanation: '', explained_at: null },
      { user_id: 8, name: 'Ravi Kumar', shared_at: null, explanation: '', explained_at: null },
    ],
    decisions: [{ by: 2, state: 'open', note: 'Both programs share the whole greedy loop, renamed.', created_at: now - 20 * MIN }],
  });

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
  });

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
      if (path === '/api/events') return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
      if (path === '/api/contests') return json(route, 200, { live: [], upcoming: [], past: [{ slug: 'week-6', title: 'Week 6 contest', starts_at: endsAt - 2 * 3_600_000, ends_at: endsAt, start_mode: 'fixed', duration_minutes: 0, problem_count: 2, state: 'ended', my_score: 140, max_score: 200, my_window: null }], server_now: Date.now() });

      // Student routes.
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
      if (path.startsWith('/api/admin/') && me.role === 'student') return apiError(route, 403, 'forbidden', 'Only admins can do that.');
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
      const pair = path.match(/^\/api\/admin\/contests\/4\/integrity\/signals\/(\d+)\/pair$/);
      if (pair) {
        if (pair[1] !== '31') return apiError(route, 404, 'not_found', 'Only similar code has a pair.');
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
      if (path === '/api/admin/contests/4/integrity/cases' && method === 'GET') {
        return json(route, 200, { cases: state.kase ? [state.kase] : [{ ...baseCase(), id: 2, title: 'Same wrong output on A', state: 'cleared', users: [{ user_id: 10, name: 'Meera Iyer', shared_at: now - 9 * MIN, explanation: 'Same textbook.', explained_at: now - 8 * MIN }, { user_id: 11, name: 'Kabir Shah', shared_at: now - 9 * MIN, explanation: '', explained_at: null }] }], server_now: Date.now() });
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
      if (path === '/api/admin/integrity/cases/3' && method === 'PUT') {
        const body = req.postDataJSON() as { state: string; note: string };
        state.puts.push(body);
        const k = (state.kase ??= baseCase());
        const users = k.users as Json[];
        if (body.state === 'actioned' && users.some((u) => u.shared_at === null)) {
          return apiError(route, 400, 'bad_request', 'Share the case with the students and give them time to explain before taking action.');
        }
        const at = Date.now();
        state.kase = {
          ...k,
          state: body.state,
          updated_at: at,
          users: body.state === 'awaiting_student' ? users.map((u) => ({ ...u, shared_at: u.shared_at ?? at })) : users,
          decisions: [...(k.decisions as Json[]), { by: 2, state: body.state, note: body.note, created_at: at }],
        };
        return json(route, 200, state.kase);
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
    name: 'integrity',
    path: '/admin/contests/4/integrity',
    options: {},
    title: 'Week 6 contest · Integrity · podium',
    ready: async (page) => {
      await expect(page.getByRole('table', { name: 'Signals on B. Interval covering' })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Same wrong output on A' })).toBeVisible();
    },
  },
  {
    name: 'integrity-live',
    path: '/admin/contests/4/integrity',
    options: { live: true, unchecked: true, window: true },
    title: 'Week 6 contest · Integrity · podium',
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
  {
    name: 'integrity-case',
    path: '/admin/integrity/cases/3',
    options: {},
    title: 'Similar code on B: Asha Rao and Ravi Kumar · Case · podium',
    ready: (page) => expect(page.getByRole('heading', { name: 'Decision log' })).toBeVisible(),
  },
  {
    name: 'home-asked',
    path: '/',
    options: { who: 'student' },
    title: 'Your contests · podium',
    ready: (page) => expect(page.getByText('An organiser asked you to explain a submission.')).toBeVisible(),
  },
  {
    name: 'student-integrity',
    path: '/integrity',
    options: { who: 'student' },
    title: 'Questions about your submissions · podium',
    ready: (page) => expect(page.locator('mark').first()).toBeVisible(),
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

test('the check stays closed with its reason while the contest runs', async ({ page }) => {
  const api = await mockApi(page, { live: true, unchecked: true, window: true });
  await page.goto('/admin/contests/4/integrity');
  const check = page.getByRole('button', { name: 'Check integrity' });
  await expect(check).toBeDisabled();
  await expect(check).toHaveAccessibleDescription('Check integrity after the contest ends for everyone.');
  await expect(page.getByText('it leaks by design', { exact: false })).toBeVisible();
  await expect(page.getByText('Signals are reasons to look, not proof.')).toBeVisible();
  await expect(page.getByText("No tool reliably tells AI-written code from a student's own", { exact: false })).toBeVisible();
  expect(api.runPosts).toBe(0);
});

test('a check runs, polls until done, and its signals filter by kind and strength', async ({ page }) => {
  const api = await mockApi(page, { unchecked: true });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Not checked yet.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Check integrity' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Checking the contest.' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Checking' })).toBeDisabled();
  await expect(page.getByText('Checked 30 students, 84 submissions and 2 problems.', { exact: false })).toBeVisible({ timeout: 10_000 });
  expect(api.runPosts).toBe(1);
  expect(api.runGets).toBeGreaterThanOrEqual(2);

  // Grouped by problem, in label order, with headers.
  const a = page.getByRole('table', { name: 'Signals on A. A plus B' });
  const b = page.getByRole('table', { name: 'Signals on B. Interval covering' });
  await expect(a.getByRole('columnheader', { name: 'Strength' })).toBeAttached();
  await expect(b.getByRole('rowheader', { name: 'Asha Rao and Ravi Kumar' })).toBeVisible();
  const strong = b.getByRole('row').filter({ hasText: 'Asha Rao and Ravi Kumar' });
  await expect(strong).toContainText('Similar code');
  await expect(strong).toContainText('Strong 0.94');
  await expect(strong).toContainText('2 shared passages, both in C++17');
  await expect(b.getByRole('row').filter({ hasText: 'Neha Gupta' })).toContainText('Contains the expected output of tests 7 and 9');
  await expect(a.getByRole('row').filter({ hasText: 'Rohan Das' })).toContainText('8 different results from near-identical code over 14 attempts');

  // Clusters as names.
  await expect(page.getByRole('listitem').filter({ hasText: 'Asha Rao, Ravi Kumar and Zoë Fernandes' })).toBeVisible();
  await expect(page.getByRole('listitem').filter({ hasText: 'Meera Iyer and Sara Thomas' })).toBeVisible();

  await page.getByLabel('Kind').selectOption({ label: 'Same wrong output' });
  await expect(page.getByRole('table')).toHaveCount(2);
  await expect(page.getByRole('table', { name: 'Signals on B. Interval covering' })).toHaveCount(0);
  await page.getByLabel('Kind').selectOption({ label: 'All kinds' });
  await page.getByLabel('Strength').selectOption({ label: 'Weak' });
  await expect(page.getByRole('table', { name: 'Signals on A. A plus B' }).getByRole('row')).toHaveCount(2);
  await page.getByLabel('Strength').selectOption({ label: 'Strong' });
  await page.getByLabel('Kind').selectOption({ label: 'Probing' });
  await expect(page.getByText('No signals match these filters.')).toBeVisible();
});

test('signals open a case with a title, a counted reason and the chosen signals', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const open = page.getByRole('button', { name: 'Open a case' });
  await expect(open).toBeDisabled();
  await expect(open).toHaveAccessibleDescription('Choose one or more signals first.');

  // The checkbox is reachable and toggled from the keyboard.
  const first = page.getByRole('checkbox', { name: 'Choose similar code, Asha Rao and Ravi Kumar' });
  await first.focus();
  await page.keyboard.press('Space');
  await expect(first).toBeChecked();
  await page.getByRole('checkbox', { name: 'Choose hard-coded answers, Neha Gupta' }).check();
  await expect(page.getByText('2 signals chosen.')).toBeVisible();
  await open.click();

  const dialog = page.getByRole('dialog', { name: 'Open a case' });
  await expect(dialog.getByLabel('Title')).toHaveValue('Several signals on B');
  await expect(dialog.getByRole('list', { name: '2 signals' }).getByRole('listitem')).toHaveCount(2);
  await dialog.getByLabel('Reason').fill('Too short');
  await expect(dialog.getByText('9 of at least 20 characters')).toBeVisible();
  await dialog.getByRole('button', { name: 'Open the case' }).click();
  await expect(dialog.getByText('Write a reason of at least 20 characters.')).toBeVisible();
  expect(api.caseBodies).toHaveLength(0);

  await dialog.getByLabel('Reason').fill('The greedy loop is the same line for line in both programs.');
  await expect(dialog.getByText('59 characters')).toBeVisible();
  await dialog.getByRole('button', { name: 'Open the case' }).click();
  await expect(page).toHaveURL(/\/admin\/integrity\/cases\/3$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Several signals on B' })).toBeFocused();
  expect(api.caseBodies).toEqual([
    { title: 'Several signals on B', note: 'The greedy loop is the same line for line in both programs.', signal_ids: [31, 34] },
  ]);
});

test('the pair view tints the shared passages, marks them for screen readers and shows both timelines', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  await page.getByRole('link', { name: 'Compare code of Asha Rao and Ravi Kumar' }).click();
  await expect(page).toHaveURL(/\/integrity\/signals\/31$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Asha Rao and Ravi Kumar' })).toBeFocused();
  await expect(page.getByText('Similar code on problem B, Interval covering.', { exact: false })).toBeVisible();

  const asha = page.getByRole('region', { name: /^Asha Rao's program, 12 lines, 2 shared passages$/ });
  const ravi = page.getByRole('region', { name: /^Ravi Kumar's program, 12 lines, 2 shared passages$/ });
  await expect(asha).toBeVisible();
  await expect(ravi).toBeVisible();
  // The multibyte comment before the spans does not shift them.
  const marks = await asha.locator('mark').allTextContents();
  expect(marks.join('|')).toBe(
    [
      'vector<pair<int,int>> v(n);',
      '    for (auto &p : v) cin >> p.second >> p.first;',
      'sort(v.begin(), v.end());',
      '    int last = INT_MIN, count = 0;',
      '    for (auto &p : v) if (p.second > last) { last = p.first; count++; }',
    ].join('|'),
  );
  await expect(asha.getByText('Shared passage starts.')).toHaveCount(2);
  await expect(asha.getByText('Shared passage ends.')).toHaveCount(2);
  await expect(asha).toContainText('// अंतराल: sort by the right end 🙂');
  await expect(page.getByText('C++17, submitted', { exact: false }).first()).toBeVisible();

  // The well takes keyboard focus so long lines can scroll.
  await asha.focus();
  await expect(asha).toBeFocused();

  await expect(page.getByRole('list', { name: 'Asha Rao' })).toContainText('Pasted 431 characters.');
  await expect(page.getByRole('list', { name: 'Ravi Kumar' })).toContainText('Left the page.');

  await page.getByRole('button', { name: 'Open a case' }).click();
  const dialog = page.getByRole('dialog', { name: 'Open a case' });
  await expect(dialog.getByLabel('Title')).toHaveValue('Similar code on B');
  await expect(dialog.getByRole('list', { name: 'Signal' })).toContainText('B. Interval covering: Similar code, Asha Rao and Ravi Kumar, strong 0.94');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('button', { name: 'Open a case' })).toBeFocused();
});

test('a case is shared with a note, action waits for sharing, and the log grows', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/integrity/cases/3');
  await expect(page.getByRole('heading', { level: 1, name: 'Similar code on B: Asha Rao and Ravi Kumar' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Integrity for Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4/integrity');
  const students = page.getByRole('table', { name: 'Students in this case' });
  await expect(students.getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('Not shared yet');
  await expect(page.getByRole('table', { name: 'Evidence in this case' })).toContainText('B. Interval covering');

  const action = page.getByRole('button', { name: 'Take action' });
  await expect(action).toBeDisabled();
  await expect(action).toHaveAccessibleDescription('Take action opens once the case is shared with every student, so they can explain first.');

  // A note is required before anything changes.
  await page.getByRole('button', { name: 'Share with the students' }).click();
  await expect(page.getByText('Write a note of at least 20 characters for the log first.')).toBeVisible();
  await expect(page.getByLabel('Note for the log')).toBeFocused();
  expect(api.puts).toHaveLength(0);

  await page.getByLabel('Note for the log').fill('Asking both students how they wrote the loop.');
  await page.getByRole('button', { name: 'Share with the students' }).click();
  const confirmShare = page.getByRole('dialog', { name: 'Share with the students?' });
  await expect(confirmShare).toContainText("never another student's name or code");
  await expect(confirmShare.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmShare.getByRole('button', { name: 'Share with the students' }).click();
  await expect(page.locator('.head .state')).toHaveText('Shared with the students');
  await expect(students.getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('Shared');
  expect(api.puts).toEqual([{ state: 'awaiting_student', note: 'Asking both students how they wrote the loop.' }]);
  await expect(page.getByLabel('Note for the log')).toHaveValue('');

  await expect(action).toBeEnabled();
  await page.getByLabel('Note for the log').fill('Both explanations describe the same shared notes from a friend.');
  await action.click();
  const confirmAction = page.getByRole('dialog', { name: 'Take action?' });
  await confirmAction.getByRole('button', { name: 'Take action' }).click();
  await expect(page.locator('.head .state')).toHaveText('Action taken');
  await expect(page.getByRole('button', { name: 'Reopen the case' })).toBeVisible();
  const log = page.locator('.log li');
  await expect(log).toHaveCount(3);
  await expect(log.nth(2)).toContainText('Action taken, by You');
  await expect(log.nth(2)).toContainText('Both explanations describe the same shared notes from a friend.');
  await page.screenshot({ path: `${shots()}integrity-case-decided-1440-light.png`, fullPage: true });
});

test('a student sees a quiet notice, their own evidence, and saves an explanation', async ({ page }) => {
  const api = await mockApi(page, { who: 'student' });
  await page.goto('/');
  const notice = page.getByText('An organiser asked you to explain a submission.');
  await expect(notice).toBeVisible();
  await page.getByRole('link', { name: 'Write your explanation' }).click();
  await expect(page).toHaveURL(/\/integrity$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Questions about your submissions' })).toBeFocused();
  await expect(page.getByText('Waiting for your explanation. Nothing has been decided.')).toBeVisible();
  await expect(page.getByText("Your submission shares long passages with another student's submission.", { exact: false })).toBeVisible();
  // Never another student's name, never a strength or a signal word.
  await expect(page.locator('main')).not.toContainText('Ravi Kumar');
  await expect(page.locator('main')).not.toContainText(/strong|signal/i);

  const code = page.getByRole('region', { name: /^Your program for B\. Interval covering/ });
  await expect(code.locator('mark')).toHaveCount(5);
  await expect(code.locator('mark').first()).toHaveText('vector<pair<int,int>> v(n);');

  await page.getByRole('button', { name: 'Save explanation' }).click();
  await expect(page.getByText('Write your explanation first.')).toBeVisible();
  await page.getByLabel('Explain how you wrote this, in your own words').fill('I wrote it in the lab after the lecture on greedy algorithms.');
  await page.getByRole('button', { name: 'Save explanation' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved. You can change it until a decision is made.' })).toBeVisible();
  expect(api.explanations).toEqual([{ explanation: 'I wrote it in the lab after the lecture on greedy algorithms.' }]);
  await expect(page.getByText('Your explanation is saved.', { exact: false })).toBeVisible();
});

test('a decided case reads only for the student', async ({ page }) => {
  await mockApi(page, { who: 'student', decided: true });
  await page.goto('/integrity');
  await expect(page.getByText('Decided: cleared. No action will be taken.')).toBeVisible();
  await expect(page.getByText('I wrote it in the lab from the lecture notes.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save explanation' })).toHaveCount(0);
  await expect(page.getByText('A decision has been made, so the explanation can no longer change.')).toBeVisible();
});

test('a student with no cases gets no notice', async ({ page }) => {
  await mockApi(page, { who: 'student', noCases: true });
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Week 6 contest' })).toBeVisible();
  await expect(page.getByText('An organiser asked you to explain a submission.')).toHaveCount(0);
  await page.goto('/integrity');
  await expect(page.getByText('Nothing to explain.', { exact: false })).toBeVisible();
});

test('students cannot open the organiser screens', async ({ page }) => {
  await mockApi(page, { who: 'student' });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Only owners and admins can review integrity.', { exact: false })).toBeVisible();
  await page.goto('/admin/integrity/cases/3');
  await expect(page.getByText('Only owners and admins can review integrity.', { exact: false })).toBeVisible();
});

test('results and the contest nav lead to the integrity page', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link', { name: 'Integrity' })).toHaveAttribute('aria-current', 'page');
  await expect(nav.getByRole('link', { name: 'Results' })).toHaveAttribute('href', '/admin/contests/4/results');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contest admin' })).toHaveAttribute('aria-current', 'page');
  await nav.getByRole('link', { name: 'Results' }).click();
  await page.getByRole('link', { name: 'Check integrity' }).click();
  await expect(page).toHaveURL(/\/admin\/contests\/4\/integrity$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Week 6 contest' })).toBeFocused();
});
