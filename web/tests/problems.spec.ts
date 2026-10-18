// Screen sweep and flows for the M2 problem screens (/admin/problems and
// /admin/problems/{id}). Every /api/ call is answered by an in-test mock of
// the admin problems API in cache/v1/m2-plan.md section 4, one per page, so
// each test starts from the same fixtures: an admin with four problems
// (validated, not validated with an unassigned test, archived, and one whose
// last validation failed).
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m2/<screen>-<width>-<theme>.png. Further tests cover the
// keyboard path through the editor, stacking at 375 px, saving with a field
// error, validation from queued to passed and to failed, import errors and
// success, creating, archiving, and the unsaved-changes guard.
import { expect, test, type FileChooser, type Page, type Route } from '@playwright/test';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m2/`;
}
const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

const ADMIN = {
  id: 2,
  email: 'arjun.mehta@college.edu',
  name: 'Arjun Mehta',
  avatar_url: '',
  role: 'admin',
  theme: '',
  verdicts: 'standard',
};
const STUDENT = { ...ADMIN, id: 7, email: 'asha@college.edu', name: 'Asha Rao', role: 'student' };

const T0 = 1792183192359;

type Json = Record<string, unknown>;

// Node's Buffer, which file choosers need; the tsconfig leaves Node's types
// out so the app code cannot reach for them.
const NodeBuffer = (globalThis as unknown as { Buffer: { from(text: string): Uint8Array } }).Buffer;

/**
 * Starts collecting file choosers before the page loads. Subscribing turns on
 * Chromium's file chooser interception asynchronously, so subscribing just
 * before the click can lose the race under load.
 */
function watchFileChoosers(page: Page) {
  const seen: FileChooser[] = [];
  page.on('filechooser', (chooser) => seen.push(chooser));
  return {
    async next(): Promise<FileChooser> {
      await expect.poll(() => seen.length, { message: 'no file chooser opened' }).toBeGreaterThan(0);
      return seen.shift()!;
    },
  };
}

/** A .zip "file" for a file chooser; the mock reads its bytes back as text. */
function zipFile(name: string, text: string) {
  return { name, mimeType: 'application/zip', buffer: NodeBuffer.from(text) };
}

function test_(idx: number, group: number | 'sample' | null, input: string, output: string): Json {
  return {
    idx,
    sample: group === 'sample',
    subtask_idx: typeof group === 'number' ? group : null,
    input_size: input.length,
    output_size: output.length,
    input_preview: input.slice(0, 200),
    output_preview: output.slice(0, 200),
  };
}

const LONG_INPUT = `200000\n${Array.from({ length: 60 }, (_, i) => (i * 7919) % 1000000).join(' ')}\n`;

function problemFixtures(): Record<number, Json> {
  const base = {
    difficulty: '',
    tags: [],
    origin: 'import',
    archived: false,
    validated: false,
    validated_at: null,
    validated_hash: null,
    checker: 'tokens',
    epsilon: 1e-6,
    checker_source: '',
    brute: null,
    validator: null,
    warnings: [],
  };
  return {
    1: {
      ...base,
      id: 1,
      slug: 'a-plus-b',
      title: 'A plus B',
      difficulty: 'easy',
      tags: ['math', 'intro'],
      test_count: 5,
      points: 100,
      validated: true,
      validated_at: T0 - 86400000,
      updated_at: T0 - 86400000,
      statement_md:
        'Given two integers $a$ and $b$ with $|a|, |b| \\le 10^9$, print $a + b$.\n\n' +
        '## Input\n\nOne line with $a$ and $b$, separated by a space.\n\n' +
        '## Output\n\nPrint the sum:\n\n$$\nS = a + b\n$$\n\n' +
        'Use `long long` in C++, because $2 \\cdot 10^9$ does not fit in `int`.',
      time_limit_ms: 1000,
      memory_limit_mb: 256,
      reference: { language: 'cpp17', source: '#include <iostream>\nint main() {\n    long long a, b;\n    std::cin >> a >> b;\n    std::cout << a + b << "\\n";\n}\n' },
      brute: { language: 'python3', source: 'a, b = map(int, input().split())\nprint(a + b)\n' },
      samples: [1, 2],
      subtasks: [
        { idx: 1, points: 40, title: 'Small numbers', tests: '3-4' },
        { idx: 2, points: 60, title: 'Full range', tests: '5' },
      ],
      tests: [
        test_(1, 'sample', '1 2\n', '3\n'),
        test_(2, 'sample', '-5 7\n', '2\n'),
        test_(3, 1, '10 20\n', '30\n'),
        test_(4, 1, '0 0\n', '0\n'),
        test_(5, 2, '1000000000 1000000000\n', '2000000000\n'),
      ],
      content_hash: 'h1',
    },
    2: {
      ...base,
      id: 2,
      slug: 'interval-covering',
      title: 'Interval covering',
      difficulty: 'medium',
      tags: ['greedy'],
      origin: 'manual',
      test_count: 7,
      points: 100,
      updated_at: T0 - 3600000,
      statement_md: 'Cover the segment $[0, L]$ with the fewest intervals.',
      time_limit_ms: 2000,
      memory_limit_mb: 256,
      checker: 'float',
      reference: { language: 'java21', source: 'class Main { public static void main(String[] a) {} }\n' },
      validator: { language: 'python3', source: 'import sys\n' },
      samples: [1],
      subtasks: [
        { idx: 1, points: 30, title: 'n up to 1000', tests: '2-4' },
        { idx: 2, points: 70, title: '', tests: '5-6' },
      ],
      tests: [
        test_(1, 'sample', '3 10\n0 4\n3 8\n7 10\n', '3\n'),
        test_(2, 1, '1 5\n0 5\n', '1\n'),
        test_(3, 1, '2 5\n0 3\n2 5\n', '2\n'),
        test_(4, 1, '2 5\n0 2\n3 5\n', '-1\n'),
        test_(5, 2, LONG_INPUT, '17\n'),
        test_(6, 2, LONG_INPUT, '18\n'),
        test_(7, null, LONG_INPUT, '19\n'),
      ],
      warnings: ['Test 7 is in no subtask. Add it to a subtask or mark it as a sample.'],
      content_hash: 'h2',
    },
    3: {
      ...base,
      id: 3,
      slug: 'old-sorting',
      title: 'Old sorting',
      archived: true,
      test_count: 3,
      points: 100,
      updated_at: T0 - 30 * 86400000,
      statement_md: 'Sort the numbers.',
      time_limit_ms: 1000,
      memory_limit_mb: 256,
      reference: { language: 'python3', source: 'print()\n' },
      samples: [],
      subtasks: [],
      tests: [],
      content_hash: 'h3',
    },
    4: {
      ...base,
      id: 4,
      slug: 'average-speed',
      title: 'Average speed',
      test_count: 15,
      points: 100,
      updated_at: T0 - 7200000,
      statement_md: 'Print the average speed with an absolute or relative error of at most $10^{-6}$.',
      time_limit_ms: 1000,
      memory_limit_mb: 256,
      checker: 'float',
      reference: { language: 'java21', source: 'class Main {}\n' },
      brute: { language: 'python3', source: 'print(2.5\n' },
      samples: [1],
      subtasks: [{ idx: 1, points: 100, title: 'All', tests: '2-15' }],
      tests: Array.from({ length: 15 }, (_, i) => test_(i + 1, i === 0 ? 'sample' : 1, '2\n10 4\n', '2.500000\n')),
      content_hash: 'h4',
    },
    5: {
      ...base,
      id: 5,
      slug: 'pair-sums',
      title: 'Pair sums',
      test_count: 5,
      points: 100,
      updated_at: T0 - 600000,
      statement_md: 'Count pairs with sum $k$.',
      time_limit_ms: 1000,
      memory_limit_mb: 256,
      reference: { language: 'cpp17', source: 'int main() {}\n' },
      samples: [1],
      subtasks: [{ idx: 1, points: 100, title: 'All', tests: '2-5' }],
      tests: [
        test_(1, 'sample', '3 4\n1 2 3\n', '1\n'),
        test_(2, 1, '2 3\n1 2\n', '1\n'),
        test_(3, 1, '2 9\n1 2\n', '0\n'),
        { ...test_(4, 1, '5 6\n1 2 3 4 5\n', ''), output_missing: true },
        { ...test_(5, 1, '1 1\n1\n', ''), output_missing: true },
      ],
      warnings: ['Tests 4 and 5 have no expected output. Generate outputs.'],
      content_hash: 'h5',
    },
  };
}

function summary(over: Json = {}): Json {
  return {
    reference_passed: 5,
    reference_total: 5,
    brute_agreed: 5,
    brute_total: 5,
    validator_passed: 0,
    validator_total: 0,
    max_time_ms: 212,
    time_limit_ms: 1000,
    problems: [],
    warnings: ['There is no validator.'],
    ...over,
  };
}

function validationOf(state: string, hash: string, over: Json = {}): Json {
  return {
    id: 1,
    state,
    content_hash: hash,
    current: true,
    queued_at: T0 - 60000,
    finished_at: state === 'queued' || state === 'running' ? null : T0,
    queue_position: null,
    compile: [],
    tests: [],
    summary: null,
    error: null,
    ...over,
  };
}

function passedFor(hash: string): Json {
  return validationOf('passed', hash, {
    tests: [1, 2, 3, 4, 5].flatMap((idx) => [
      { idx, role: 'reference', verdict: 'AC', time_ms: 10 + idx * 40, memory_kb: 3200, message: '' },
      { idx, role: 'brute', verdict: 'AC', time_ms: 30, memory_kb: 9100, message: '' },
    ]),
    summary: summary(),
  });
}

const FAILED_4 = validationOf('failed', 'h4', {
  compile: [
    { role: 'reference', verdict: 'OK', output: '' },
    { role: 'brute', verdict: 'CE', output: '  File "brute.py", line 1\n    print(2.5\n         ^\nSyntaxError: \'(\' was never closed' },
  ],
  tests: Array.from({ length: 15 }, (_, i) =>
    i === 6
      ? { idx: 7, role: 'reference', verdict: 'WA', time_ms: 31, memory_kb: 24576, message: 'Line 1: expected 2.500000, found 2.400000 (error 0.04 > 1e-06)' }
      : { idx: i + 1, role: 'reference', verdict: 'AC', time_ms: 20 + i * 3, memory_kb: 24576, message: '' },
  ),
  summary: summary({
    reference_passed: 14,
    reference_total: 15,
    brute_agreed: 0,
    brute_total: 0,
    max_time_ms: 62,
    problems: ['The brute force did not compile, so nothing was compared.'],
    warnings: ['There is no validator.'],
  }),
});

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: body === undefined ? '' : JSON.stringify(body),
  });
}

function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

function summaryOf(p: Json): Json {
  const keys = ['id', 'slug', 'title', 'difficulty', 'tags', 'origin', 'archived', 'test_count', 'points', 'validated', 'validated_at', 'updated_at'];
  return Object.fromEntries(keys.map((k) => [k, p[k]]));
}

/** Installs the API mock for one page and returns its mutable state. */
async function mockApi(page: Page, who: 'admin' | 'student' = 'admin') {
  const state = {
    problems: problemFixtures(),
    validations: { 1: passedFor('h1'), 4: structuredClone(FAILED_4) } as Record<number, Json>,
    // What later GETs of .../validation answer, one entry per poll.
    scripts: {} as Record<number, Json[]>,
    puts: [] as Json[],
    archived: [] as number[],
    imports: [] as string[],
    generates: [] as Json[],
    generations: {} as Record<number, Json>,
    genScripts: {} as Record<number, Json[]>,
    // How the next generation ends: 'ok', 'compile' (the reference fails to compile) or 'stale' (the problem changed).
    genOutcome: 'ok' as 'ok' | 'compile' | 'stale',
    nextId: 10,
    hash: 100,
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      const me = who === 'admin' ? ADMIN : STUDENT;

      if (path === '/api/me' && method === 'GET') return json(route, 200, me);
      if (path.startsWith('/api/admin/') && me.role === 'student') {
        return apiError(route, 403, 'forbidden', 'Only admins can do that.');
      }

      if (path === '/api/admin/problems' && method === 'GET') {
        const all = Object.values(state.problems);
        const list = url.searchParams.get('archived') === '1' ? all : all.filter((p) => !p.archived);
        return json(route, 200, { problems: list.map(summaryOf) });
      }
      if (path === '/api/admin/problems' && method === 'POST') {
        const body = req.postDataJSON() as { title: string; slug: string };
        if (Object.values(state.problems).some((p) => p.slug === body.slug)) {
          return apiError(route, 409, 'conflict', 'Another problem already uses this slug.', {
            fields: { slug: 'Another problem already uses this slug.' },
          });
        }
        const id = state.nextId++;
        const p = {
          ...problemFixtures()[3],
          id,
          title: body.title,
          slug: body.slug,
          archived: false,
          origin: 'manual',
          statement_md: '',
          test_count: 0,
          points: 0,
          updated_at: T0,
          reference: { language: 'cpp17', source: '#include <bits/stdc++.h>\nint main() {}\n' },
          content_hash: 'new',
        };
        state.problems[id] = p;
        return json(route, 201, p);
      }
      if (path === '/api/admin/problems/import' && method === 'POST') {
        const type = (await req.headerValue('content-type')) ?? '';
        if (type !== 'application/zip') return apiError(route, 415, 'bad_request', 'Send the pack as application/zip.');
        const text = req.postDataBuffer()?.toString('utf8') ?? '';
        state.imports.push(text);
        if (text.startsWith('bad')) {
          return apiError(route, 400, 'bad_request', 'The pack has 2 problems. Fix them and import it again.', {
            problems: ['problem.yaml: unknown key "timelimit". Did you mean time_limit_ms?', 'tests/03.out is missing.'],
          });
        }
        const id = state.nextId++;
        const p = { ...problemFixtures()[1], id, title: 'Pair with sum', slug: 'pair-with-sum', validated: false, validated_at: null, content_hash: 'imp' };
        state.problems[id] = p;
        return json(route, 201, p);
      }

      const m = path.match(/^\/api\/admin\/problems\/(\d+)(\/.*)?$/);
      if (m) {
        const id = Number(m[1]);
        const rest = m[2] ?? '';
        const p = state.problems[id];
        if (!p) return apiError(route, 404, 'not_found', 'There is no problem with that id.');

        if (rest === '' && method === 'GET') return json(route, 200, p);
        if (rest === '' && method === 'PUT') {
          const body = req.postDataJSON() as Json;
          state.puts.push(body);
          if (body.slug === 'taken') {
            return apiError(route, 400, 'bad_request', 'Some fields need attention.', {
              fields: { slug: 'Another problem already uses this slug.', 'subtasks[1].points': 'Points run from 1 to 1000.' },
            });
          }
          const subtasks: Json[] = (body.subtasks as Json[]).map((s, i) => ({ ...s, idx: i + 1 }));
          const hash = `h${state.hash++}`;
          Object.assign(p, body, {
            subtasks,
            points: subtasks.reduce((n, s) => n + Number(s.points), 0),
            content_hash: hash,
            validated: p.validated_hash === hash,
            updated_at: T0,
          });
          return json(route, 200, p);
        }
        if (rest === '' && method === 'DELETE') {
          p.archived = true;
          state.archived.push(id);
          return json(route, 204);
        }
        if (rest === '/validation' && method === 'GET') {
          const script = state.scripts[id];
          if (script && script.length > 0) {
            state.validations[id] = script.shift()!;
            const v = state.validations[id];
            if (v.state === 'passed') Object.assign(p, { validated: true, validated_at: T0, validated_hash: p.content_hash });
          }
          const v = state.validations[id];
          return v ? json(route, 200, { ...v, current: v.content_hash === p.content_hash }) : apiError(route, 404, 'not_found', 'This problem has not been validated yet.');
        }
        if (rest === '/validate' && method === 'POST') {
          if ((p.warnings as string[]).length > 0) {
            return apiError(route, 400, 'bad_request', 'This problem cannot be validated yet.', { problems: p.warnings });
          }
          const hash = p.content_hash as string;
          state.validations[id] = validationOf('queued', hash, { queue_position: 2 });
          if (id === 4) {
            state.scripts[id] = [validationOf('running', hash), { ...structuredClone(FAILED_4), content_hash: hash }];
          } else {
            state.scripts[id] = [validationOf('queued', hash, { queue_position: 1 }), validationOf('running', hash), passedFor(hash)];
          }
          return json(route, 202, state.validations[id]);
        }
        if (rest === '/generate' && method === 'POST') {
          const body = req.postDataJSON() as Json;
          state.generates.push(body);
          if (id === 4) {
            return json(route, 409, {
              error: { code: 'not_ready', message: 'The problem is not ready. Fix these first.', problems: ['Add a reference solution first.'] },
            });
          }
          const idxs = (p.tests as Json[]).filter((t) => body.all || t.output_missing).map((t) => Number(t.idx));
          const results = idxs.map((idx) => ({ idx, verdict: 'OK', time_ms: 200 + idx * 3 }));
          const done: Json =
            state.genOutcome === 'compile'
              ? { results: [], compile_output: 'main.cpp:3:5: error: expected ; before return\n', summary: { applied: 0, problems: [] } }
              : state.genOutcome === 'stale'
                ? { results, summary: { applied: 0, problems: ['The problem changed while outputs were generated. Generate them again.'] } }
                : { results, summary: { applied: idxs.length, problems: [] } };
          state.generations[id] = { id: 1, state: 'queued', results: [], error: null };
          state.genScripts[id] = [
            { id: 1, state: 'running', results: [], error: null },
            { id: 1, state: 'done', error: null, ...done },
          ];
          return json(route, 202, state.generations[id]);
        }
        if (rest === '/generation' && method === 'GET') {
          const script = state.genScripts[id];
          if (script && script.length > 0) {
            state.generations[id] = script.shift()!;
            if (state.generations[id].state === 'done' && state.genOutcome === 'ok') {
              for (const t of p.tests as Json[]) {
                if (t.output_missing) Object.assign(t, { output_missing: false, output_size: 2, output_preview: '2\n' });
              }
              p.warnings = [];
            }
          }
          const g = state.generations[id];
          return g ? json(route, 200, g) : apiError(route, 404, 'not_found', 'Outputs were never generated for this problem.');
        }
        if (rest === '/tests' && method === 'PUT') {
          p.tests = [test_(1, 'sample', '1 1\n', '2\n'), test_(2, null, '2 2\n', '4\n')];
          p.test_count = 2;
          return json(route, 200, p);
        }
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

interface Screen {
  name: string;
  path: string;
  title: string;
  ready: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
  {
    name: 'problems-bank',
    path: '/admin/problems',
    title: 'Problems · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Interval covering', exact: true })).toBeVisible(),
  },
  {
    name: 'problem-editor',
    path: '/admin/problems/1',
    title: 'A plus B · Problems · podium',
    ready: async (page) => {
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
      await expect(page.locator('.statement .katex').first()).toBeVisible();
    },
  },
  {
    name: 'problem-editor-warnings',
    path: '/admin/problems/2',
    title: 'Interval covering · Problems · podium',
    ready: (page) => expect(page.getByRole('note', { name: 'Before you validate' })).toBeVisible(),
  },
  {
    name: 'problem-editor-failed',
    path: '/admin/problems/4',
    title: 'Average speed · Problems · podium',
    ready: (page) => expect(page.locator('.headline')).toHaveText('Validation failed'),
  },
  {
    name: 'problem-editor-missing-outputs',
    path: '/admin/problems/5',
    title: 'Pair sums · Problems · podium',
    ready: (page) => expect(page.getByText('2 tests have no expected output.', { exact: false })).toBeVisible(),
  },
  {
    name: 'problem-not-found',
    path: '/admin/problems/999',
    title: 'Problem not found · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Go to the problem bank.' })).toBeVisible(),
  },
];

test('each problem route has its own title', () => {
  const titles = SCREENS.map((s) => s.title);
  expect(new Set(titles).size).toBe(titles.length);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(`${screen.name}`, async ({ page }) => {
        await mockApi(page);
        await page.setViewportSize({ width: WIDTHS[WIDTHS.length - 1], height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        await page.evaluate(() => document.fonts.ready);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));

          const overflow = await page.evaluate(() => ({
            scroll: document.documentElement.scrollWidth,
            client: document.documentElement.clientWidth,
          }));
          expect(overflow.scroll, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(overflow.client);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page).toHaveTitle(screen.title);

          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

/** Describes the focused element and whether its focus indicator is visible. */
function focusInfo(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    const style = getComputedStyle(el);
    const outlineVisible =
      style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0 && !/rgba\(.*,\s*0\)$/.test(style.outlineColor);
    return {
      name: (el.getAttribute('aria-label') ?? (['INPUT', 'SELECT', 'TEXTAREA'].includes(el.tagName) ? el.id : el.textContent) ?? '').trim(),
      id: el.id,
      tag: el.tagName.toLowerCase(),
      visible: style.boxShadow !== 'none' || outlineVisible,
    };
  });
}

for (const width of [1440, 375]) {
  test(`keyboard reaches every control on the editor at ${width}px`, async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/problems/1');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
    if (width >= 768) await expect(page.locator('.statement .katex').first()).toBeVisible();

    const reached: string[] = [];
    for (let i = 0; i < 120; i++) {
      await page.keyboard.press('Tab');
      const info = await focusInfo(page);
      if (info === null) break;
      expect(info.visible, `no visible focus on ${info.tag} "${info.name || info.id}"`).toBe(true);
      reached.push(info.name || info.id);
      if (info.name === 'Archive problem') break;
    }

    const always = [
      'Problem bank', 'Try as a student', 'Validated 16 Oct 02:09', 'Save changes', 'Validate', 'title', 'slug',
      'statement_md', 'time_limit_ms', 'memory_limit_mb', 'checker', 'tags', 'difficulty', 'samples', 'subtask-0-points',
      'Remove subtask 1', 'Remove subtask 2', 'Add subtask', 'Replace tests', 'Regenerate all outputs', 'Show all 5 tests',
      'reference-language', 'reference-source', 'Remove brute force', 'Add validator', 'Download pack', 'Archive problem',
    ];
    const wide = ['Details', 'Tests', 'Solutions', 'Validation'];
    const phone = ['Write'];
    for (const name of [...always, ...(width >= 768 ? wide : phone)]) {
      expect(reached, `Tab did not reach "${name}"`).toContain(name);
    }
  });
}

test('subtasks, saving and validating work from the keyboard', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');

  const add = page.getByRole('button', { name: 'Add subtask' });
  await add.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#subtask-2-points')).toBeFocused();
  await page.keyboard.type('10');
  await page.keyboard.press('Tab');
  await page.keyboard.type('Extra');
  await page.keyboard.press('Tab');
  await page.keyboard.type('6');
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Remove subtask 3' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('group', { name: 'Subtask 3' })).toHaveCount(0);
  await expect(add).toBeFocused();

  await page.locator('#subtask-1-points').fill('50');
  await page.getByRole('button', { name: 'Save changes' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  expect(state.puts.at(-1)?.subtasks).toEqual([
    { points: 40, title: 'Small numbers', tests: '3-4' },
    { points: 50, title: 'Full range', tests: '5' },
  ]);
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);
  await expect(page.locator('.status')).toContainText('Not validated');
  await expect(page.getByText('These results are for an earlier version. Validate again.')).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText('Waiting for the judge (2nd in queue)')).toBeVisible();
  await expect(page.locator('.headline')).toHaveText('Running', { timeout: 10_000 });
  await expect(page.locator('.headline')).toHaveText('Validation passed', { timeout: 10_000 });
  await expect(page.getByText('Reference solution passed 5 of 5 tests')).toBeVisible();
  await expect(page.getByText('Brute force agreed on 5 of 5 tests')).toBeVisible();
  await expect(page.getByText('Slowest reference run 212 ms of 1000 ms')).toBeVisible();
  await expect(page.locator('.status')).toContainText('Validated');
  await expect(page.locator('[aria-live="polite"]').filter({ hasText: 'Validation passed' })).toHaveCount(1);
  await page.screenshot({ path: `${shots()}problem-editor-validated-1440-light.png`, fullPage: true });
});

test('the editor stacks at 375 px with Write and Preview tabs for the statement', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.getByRole('region', { name: 'Preview', exact: true })).toHaveCount(0);

  const write = page.getByRole('tab', { name: 'Write' });
  await expect(write).toHaveAttribute('aria-selected', 'true');
  await write.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tabpanel', { name: 'Preview' }).locator('.katex').first()).toBeVisible();
  await expect(page.locator('#statement_md')).toBeHidden();
  await page.screenshot({ path: `${shots()}problem-editor-preview-tab-375-light.png`, fullPage: true });
  await page.getByRole('tab', { name: 'Write' }).click();
  await expect(page.locator('#statement_md')).toBeVisible();

  // Tests stack: input over output.
  await page.getByText('Show all 5 tests').click();
  const row = page.getByRole('row', { name: /^5 Subtask 2/ });
  const input = await row.locator('.file-cell').nth(0).boundingBox();
  const output = await row.locator('.file-cell').nth(1).boundingBox();
  expect(output!.y).toBeGreaterThan(input!.y + input!.height - 1);

  const boxes = [];
  for (const name of ['Details', 'Tests', 'Solutions']) {
    const box = await page.getByRole('region', { name, exact: true }).boundingBox();
    expect(box, name).not.toBeNull();
    boxes.push(box!);
  }
  for (let i = 1; i < boxes.length; i++) {
    expect(boxes[i].y, 'sections follow each other').toBeGreaterThanOrEqual(boxes[i - 1].y + boxes[i - 1].height - 1);
    expect(Math.abs(boxes[i].x - boxes[0].x)).toBeLessThan(2);
  }
});

test('server field errors appear next to their fields', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const slug = page.getByLabel('Slug', { exact: true });
  await slug.fill('taken');
  await page.getByRole('button', { name: 'Save changes' }).click();

  await expect(page.locator('#slug-error')).toHaveText('Another problem already uses this slug.');
  await expect(slug).toHaveAttribute('aria-invalid', 'true');
  await expect(slug).toHaveAttribute('aria-describedby', 'slug-help slug-error');
  await expect(slug).toBeFocused();
  await expect(page.locator('#subtasks-1-points-error')).toHaveText('Points run from 1 to 1000.');
  await expect(page.getByRole('alert').filter({ hasText: 'Some fields need attention.' })).toBeVisible();

  await slug.fill('a-plus-b');
  await page.locator('#samples').fill('1, x');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#samples-error')).toContainText('"x" is not a test number or range.');
  await expect(page.locator('#slug-error')).toHaveCount(0);
  await page.screenshot({ path: `${shots()}problem-editor-field-errors-1440-light.png`, fullPage: true });
});

test('a failed validation names the test, program and verdict', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await expect(page.locator('.headline')).toHaveText('Validation failed');
  await expect(page.locator('.status')).toHaveText('Validation failed, 1 problem');
  await expect(page.locator('.report .lead')).toHaveText('Fix the brute force: it did not compile.');

  // Grouped by program, each with a link to its source.
  const reference = page.getByRole('region', { name: 'Reference solution' });
  await expect(reference.getByText('Wrong answer on test 7')).toBeVisible();
  await expect(reference.getByText('Line 1: expected 2.500000, found 2.400000')).toBeVisible();
  await expect(reference.getByText('Reference solution passed 14 of 15 tests')).toBeVisible();
  await expect(reference.locator('.guidance')).toContainText('expected output');
  const brute = page.getByRole('region', { name: 'Brute force' });
  await expect(brute.getByText('Brute force did not compile', { exact: true })).toBeVisible();
  await expect(brute.locator('pre').filter({ hasText: "SyntaxError: '(' was never closed" })).toBeVisible();
  await brute.getByRole('link', { name: 'Go to brute force' }).click();
  await expect(page.locator('#brute-source')).toBeFocused();
  // Only failing lines are red.
  const passColour = await reference.locator('.line.tone-ok').first().evaluate((el) => getComputedStyle(el).color);
  const inkColour = await page.locator('h1').evaluate((el) => getComputedStyle(el).color);
  expect(passColour).toBe(inkColour);

  await page.getByText('All test results').click();
  await expect(page.getByRole('cell', { name: 'Wrong answer' })).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).click();
  await expect(page.getByText('Waiting for the judge (2nd in queue)')).toBeVisible();
  await expect(page.locator('.headline')).toHaveText('Validation failed', { timeout: 10_000 });
  await expect(page.getByText('Wrong answer on test 7')).toBeVisible();
});

test('validation is refused while a test is in no subtask', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  const note = page.getByRole('note', { name: 'Before you validate' });
  await expect(note).toContainText('Test 7 is in no subtask. Add it to a subtask or mark it as a sample.');
  // Named outside the table, which stays closed until asked for.
  await expect(page.getByText('7 tests: 1 sample, 5 in 2 subtasks, 1 in no subtask.')).toBeVisible();
  await expect(page.getByText('Test 7 is in no subtask. Add it to a subtask range or to the samples.')).toBeVisible();
  await page.getByText('Show all 7 tests').click();
  await expect(page.getByRole('row', { name: /^7 Not in a subtask/ })).toBeVisible();

  // Groups follow the subtasks as typed, before saving.
  await page.locator('#subtask-1-tests').fill('5-7');
  await expect(page.getByRole('row', { name: /^7 Subtask 2/ })).toBeVisible();
  await expect(page.getByText('Test 7 is in no subtask. Add it to a subtask range or to the samples.')).toHaveCount(0);
  await page.locator('#subtask-1-tests').fill('5-6');
  await expect(page.getByLabel('Allowed error')).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).click();
  const alert = page.getByRole('alert').filter({ hasText: 'This problem cannot be validated yet.' });
  await expect(alert).toContainText('Test 7 is in no subtask.');
  await expect(page.getByText('Not validated yet.')).toBeVisible();
});

test('importing a pack shows its problems, then opens the new problem', async ({ page }) => {
  const state = await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Old sorting', exact: true })).toHaveCount(0);

  const importButton = page.getByRole('button', { name: 'Import pack' });
  await importButton.focus();
  await page.keyboard.press('Enter');
  await (await choosers.next()).setFiles(zipFile('broken.zip', 'bad pack'));

  const alert = page.getByRole('alert');
  await expect(alert).toContainText('The pack has 2 problems. Fix them and import it again.');
  await expect(alert.getByRole('listitem')).toHaveText([
    'problem.yaml: unknown key "timelimit". Did you mean time_limit_ms?',
    'tests/03.out is missing.',
  ]);
  await page.screenshot({ path: `${shots()}problems-bank-import-error-1440-light.png`, fullPage: true });

  await page.locator('input[type="file"]').setInputFiles(zipFile('pair.zip', 'good pack'));
  await expect(page).toHaveURL('/admin/problems/10');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pair with sum');
  expect(state.imports).toEqual(['bad pack', 'good pack']);
});

test('the bank shows status words, archived problems on request, and creates problems', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('row', { name: /A plus B/ })).toContainText('Validated');
  await expect(page.getByRole('row', { name: /Interval covering/ })).toContainText('Not validated');

  await page.getByLabel('Show archived').check();
  await expect(page).toHaveURL('/admin/problems?archived=1');
  await expect(page.getByRole('row', { name: /Old sorting/ })).toContainText('Archived');

  await page.getByRole('button', { name: 'New problem' }).click();
  const title = page.getByLabel('Title', { exact: true });
  await expect(title).toBeFocused();
  await title.fill('Interval Covering');
  await expect(page.getByLabel('Slug', { exact: true })).toHaveValue('interval-covering');
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page.locator('#new-slug-error')).toHaveText('Another problem already uses this slug.');

  await page.getByLabel('Slug', { exact: true }).fill('interval-covering-2');
  await title.fill('Interval Covering II');
  await expect(page.getByLabel('Slug', { exact: true })).toHaveValue('interval-covering-2');
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page).toHaveURL('/admin/problems/10');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Interval Covering II');
  await expect(page.getByText('No tests yet.')).toBeVisible();
});

test('archiving asks first and returns to the bank', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'Archive problem' }).click();
  const dialog = page.getByRole('dialog', { name: 'Archive A plus B?' });
  await expect(dialog).toContainText('It leaves the problem bank. Contests that used it keep it.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  expect(state.archived).toEqual([]);

  await page.getByRole('button', { name: 'Archive problem' }).click();
  await dialog.getByRole('button', { name: 'Archive problem' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByText('Archived', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toHaveCount(0);
  expect(state.archived).toEqual([1]);
});

test('leaving with unsaved changes asks first', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.locator('#title').fill('A plus B, edited');
  await expect(page.locator('.statement').getByText('Given two integers')).toBeVisible();
  await expect(page.locator('.preview-title')).toHaveText('A plus B, edited');

  await page.getByRole('link', { name: 'Problem bank' }).click();
  const dialog = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page).toHaveURL('/admin/problems/1');
  await expect(page.locator('#title')).toHaveValue('A plus B, edited');

  await page.getByRole('link', { name: 'Problems', exact: true }).click();
  await dialog.getByRole('button', { name: 'Leave page' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problems');
});

test('replacing tests uploads a zip and lists the new tests', async ({ page }) => {
  await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'Replace tests' }).click();
  await (await choosers.next()).setFiles(zipFile('tests.zip', 'zip'));
  await expect(page.getByText('Replaced tests. 2 tests stored.')).toBeVisible();
  await page.getByText('Show all 2 tests').click();
  // Groups come from the sample and subtask lists as they stand (samples 1, 2).
  await expect(page.getByRole('row', { name: /^2 Sample/ })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Download output of test 2' })).toHaveAttribute(
    'href',
    '/api/admin/problems/1/tests/2/output',
  );
});

test('statements are sanitised and maths is rendered', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const statement = page.getByLabel('Statement', { exact: true });
  await statement.fill(
    '# Heading\n\nHi <img src=x onerror="window.__pwned=1"> [link](javascript:window.__pwned=2) and $x^2$.\n\n![figure](/fig.png)',
  );
  const preview = page.locator('.statement');
  await expect(preview.locator('.katex')).toHaveCount(1);
  await expect(preview.locator('h4')).toHaveText('Heading');
  await expect(preview.locator('img[onerror]')).toHaveCount(0);
  await expect(preview).toContainText('<img src=x onerror="window.__pwned=1">');
  expect(await preview.locator('a').getAttribute('href')).toBeNull();
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined();
  await expect(page.locator('h1')).toHaveCount(1);
});

test('students see an explanation instead of the bank and no Problems link', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/admin/problems');
  await expect(page.getByText('Only admins can manage problems.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toHaveCount(0);
});

test('admins see the Problems link and it marks the current page', async ({ page }) => {
  await mockApi(page);
  await page.goto('/');
  await page.getByRole('link', { name: 'Problems', exact: true }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await page.getByRole('link', { name: 'A plus B', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'People' })).toHaveCount(0);
});

test('missing outputs are named and generated from the reference', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/5');
  await expect(page.getByText('2 tests have no expected output. Generate outputs to fill them from the reference solution.')).toBeVisible();
  await page.getByText('Show all 5 tests').click();
  await expect(page.getByRole('row', { name: /^4 Subtask 1/ })).toContainText('Output missing');
  await expect(page.getByRole('link', { name: 'Download output of test 4' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('Generating outputs');
  expect(state.generates).toEqual([{ all: false }]);
  await expect(page.locator('.gen-line')).toHaveText('Generated 2 outputs. Slowest run 220 ms.', { timeout: 10_000 });
  await expect(page.getByText('Output missing')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Generate outputs' })).toHaveCount(0);
  await page.screenshot({ path: `${shots()}problem-editor-generated-1440-light.png`, fullPage: true });

  // Regenerating everything asks first and uses the same word.
  await page.getByRole('button', { name: 'Regenerate all outputs' }).click();
  const dialog = page.getByRole('dialog', { name: 'Regenerate all outputs?' });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Regenerate all outputs' }).click();
  await expect.poll(() => state.generates.at(-1)).toEqual({ all: true });
  await expect(page.locator('.gen-line')).toHaveText('Generated 5 outputs. Slowest run 220 ms.', { timeout: 10_000 });
});

test('a refused generation shows the reason in place', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await page.getByRole('button', { name: 'Regenerate all outputs' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Regenerate all outputs' }).click();
  const alert = page.getByRole('alert').filter({ hasText: 'The problem is not ready. Fix these first.' });
  await expect(alert).toContainText('Add a reference solution first.');
});

test('a reference that does not compile, or a problem changed mid-run, is said plainly', async ({ page }) => {
  const state = await mockApi(page);
  state.genOutcome = 'compile';
  await page.goto('/admin/problems/5');
  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('The reference solution did not compile, so no outputs were generated.', {
    timeout: 10_000,
  });
  await expect(page.locator('.gen-failures')).toContainText('expected ; before return');

  state.genOutcome = 'stale';
  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('Some outputs were not applied.', { timeout: 10_000 });
  await expect(page.locator('.gen-failures')).toContainText('The problem changed while outputs were generated.');
});

test('the sticky bar keeps status, field errors and the main actions in reach', async ({ page }) => {
  const state = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('link', { name: 'Try as a student' })).toHaveAttribute('href', '/admin/problems/1/try');
  const validate = page.getByRole('button', { name: 'Validate', exact: true });
  await expect(validate).toHaveClass(/btn-secondary/);

  await page.locator('#title').fill('A plus B, edited');
  await expect(page.getByRole('button', { name: 'Save and validate' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes' })).toHaveClass(/btn-primary/);

  // The bar stays at the top while the page scrolls.
  await page.getByRole('link', { name: 'Solutions', exact: true }).click();
  await page.mouse.wheel(0, 2000);
  const bar = await page.locator('.action-bar').boundingBox();
  expect(bar!.y).toBeLessThanOrEqual(1);
  await page.screenshot({ path: `${shots()}problem-editor-sticky-1440-light.png` });

  // Ctrl+S saves.
  await page.keyboard.press('Control+s');
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  expect(state.puts).toHaveLength(1);

  // Field errors: a count in the bar and a list of links that focus each field.
  await page.locator('#slug').fill('taken');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('.error-count')).toHaveText('2 fields need attention');
  const list = page.locator('#field-errors');
  await list.getByRole('link', { name: 'Subtask 2 points: Points run from 1 to 1000.' }).click();
  await expect(page.locator('#subtask-1-points')).toBeFocused();
  await list.getByRole('link', { name: 'Slug: Another problem already uses this slug.' }).click();
  await expect(page.locator('#slug')).toBeFocused();
});

test('a problem with no tests offers Upload tests', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/3');
  await expect(page.getByRole('button', { name: 'Upload tests' })).toBeVisible();
  await expect(page.getByText('No tests yet. Upload a .zip of numbered input and output files to add them.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Regenerate all outputs' })).toHaveCount(0);
  await expect(page.getByText('No validator. Add one to check inputs against the constraints.')).toBeVisible();
});

test('reference and brute force sit side by side at 1440 px', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const ref = await page.getByRole('group', { name: 'Reference solution' }).boundingBox();
  const brute = await page.getByRole('group', { name: 'Brute force' }).boundingBox();
  expect(Math.abs(ref!.y - brute!.y)).toBeLessThan(2);
  expect(brute!.x).toBeGreaterThan(ref!.x + ref!.width - 1);
});

test('the bank sorts by last change, filters past ten problems and links to Try', async ({ page }) => {
  const state = await mockApi(page);
  for (let i = 0; i < 8; i++) {
    const id = 20 + i;
    state.problems[id] = { ...state.problems[1], id, slug: `extra-${i}`, title: `Extra ${i}`, tags: i === 3 ? ['graphs'] : [], updated_at: T0 - (40 + i) * 86400000 };
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems');
  const titles = page.locator('tbody th.title');
  await expect(titles.first()).toHaveText('Pair sums');
  await expect(titles.nth(1)).toHaveText('Interval covering');
  await expect(page.getByRole('row', { name: /A plus B/ }).first()).toContainText('Easy');
  await expect(page.getByRole('row', { name: /A plus B/ }).first()).toContainText('math, intro');
  await expect(page.getByRole('link', { name: 'Try A plus B as a student' }).first()).toHaveAttribute('href', '/admin/problems/1/try');

  const filter = page.getByLabel('Filter by title or tag');
  await filter.fill('graphs');
  await expect(titles).toHaveText(['Extra 3']);
  await filter.fill('nothing like this');
  await expect(page.getByText('No problem matches "nothing like this". Try another word.')).toBeVisible();
});

test('bank rows read as title and status, then tests and date, on a phone', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /A plus B/ });
  const title = await row.locator('.title').boundingBox();
  const status = await row.locator('.state').boundingBox();
  const tests = await row.locator('.tests').boundingBox();
  expect(Math.abs(title!.y - status!.y)).toBeLessThan(6);
  expect(tests!.y).toBeGreaterThan(title!.y + title!.height - 1);
  await expect(row.locator('.tags')).toBeHidden();
});
