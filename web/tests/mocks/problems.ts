// Fixtures and the API mock of problems.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type FileChooser, type Page, type Route } from '@playwright/test';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m2/`;
}
export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;

export const ADMIN = {
  id: 2,
  email: 'arjun.mehta@college.edu',
  name: 'Arjun Mehta',
  avatar_url: '',
  role: 'admin',
  theme: '',
  verdicts: 'standard',
};
export const STUDENT = { ...ADMIN, id: 7, email: 'asha@college.edu', name: 'Asha Rao', role: 'student' };

export const T0 = 1792183192359;

export type Json = Record<string, unknown>;

// Node's Buffer, which file choosers need; the tsconfig leaves Node's types
// out so the app code cannot reach for them.
export const NodeBuffer = (globalThis as unknown as { Buffer: { from(text: string): Uint8Array } }).Buffer;

/**
 * Starts collecting file choosers before the page loads. Subscribing turns on
 * Chromium's file chooser interception asynchronously, so subscribing just
 * before the click can lose the race under load.
 */
export function watchFileChoosers(page: Page) {
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
export function zipFile(name: string, text: string) {
  return { name, mimeType: 'application/zip', buffer: NodeBuffer.from(text) };
}

export function test_(idx: number, group: number | 'sample' | null, input: string, output: string): Json {
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

export const LONG_INPUT = `200000\n${Array.from({ length: 60 }, (_, i) => (i * 7919) % 1000000).join(' ')}\n`;

export function problemFixtures(): Record<number, Json> {
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

export function summary(over: Json = {}): Json {
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

export function validationOf(state: string, hash: string, over: Json = {}): Json {
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

export function passedFor(hash: string): Json {
  return validationOf('passed', hash, {
    tests: [1, 2, 3, 4, 5].flatMap((idx) => [
      { idx, role: 'reference', verdict: 'AC', time_ms: 10 + idx * 40, memory_kb: 3200, message: '' },
      { idx, role: 'brute', verdict: 'AC', time_ms: 30, memory_kb: 9100, message: '' },
    ]),
    summary: summary(),
  });
}

export const FAILED_4 = validationOf('failed', 'h4', {
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

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: body === undefined ? '' : JSON.stringify(body),
  });
}

export function apiError(route: Route, status: number, code: string, message: string, extra: Json = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

export function summaryOf(p: Json): Json {
  const keys = ['id', 'slug', 'title', 'difficulty', 'tags', 'origin', 'archived', 'test_count', 'points', 'validated', 'validated_at', 'updated_at'];
  return Object.fromEntries(keys.map((k) => [k, p[k]]));
}

/** Installs the API mock for one page and returns its mutable state. */
export async function mockApi(page: Page, who: 'admin' | 'student' = 'admin') {
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

export interface Screen {
  name: string;
  path: string;
  title: string;
  ready: (page: Page) => Promise<void>;
}

export const SCREENS: Screen[] = [
  {
    name: 'problems-bank',
    path: '/admin/problems',
    title: 'Problem bank · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Interval covering', exact: true })).toBeVisible(),
  },
  {
    name: 'problem-editor',
    path: '/admin/problems/1',
    title: 'A plus B · Problem bank · podium',
    ready: async (page) => {
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
      await expect(page.locator('.statement .katex').first()).toBeVisible();
    },
  },
  {
    name: 'problem-editor-warnings',
    path: '/admin/problems/2',
    title: 'Interval covering · Problem bank · podium',
    ready: (page) => expect(page.getByRole('note', { name: 'Before you validate' })).toBeVisible(),
  },
  {
    name: 'problem-editor-failed',
    path: '/admin/problems/4',
    title: 'Average speed · Problem bank · podium',
    ready: (page) => expect(page.locator('.headline')).toHaveText('Validation failed'),
  },
  {
    name: 'problem-editor-missing-outputs',
    path: '/admin/problems/5',
    title: 'Pair sums · Problem bank · podium',
    ready: (page) => expect(page.getByText('2 tests have no expected output.', { exact: false })).toBeVisible(),
  },
  {
    name: 'problem-not-found',
    path: '/admin/problems/999',
    title: 'Problem not found · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Go to the problem bank.' })).toBeVisible(),
  },
];
