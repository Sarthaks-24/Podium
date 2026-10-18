// Screen sweep and flows for the M5 AI wizard screens: the AI assistant
// settings at /admin/ai, "New problem with AI" at /admin/ai/new, and the
// editor's Generator section and "Can a model solve this?". Every /api/
// call is answered by an in-test mock of cache/v1/m5-plan.md section 7, one
// per page, so each test starts from the same fixtures: an admin with no
// key, a validated problem, and an AI draft with a generator and a plan.
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m5/<screen>-<width>-<theme>.png. Further tests cover
// the key flow (the key never reaches the page's state, storage or address
// after saving), the fake provider, the wizard from a brief over the live
// stream and from pasted text over polling, a wizard failure with reasons,
// the editor's generator section and input generation, and the solve check.
import { expect, test, type Page, type Route } from '@playwright/test';
import { longLivedEvents } from './sse';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m5/`;
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

const T0 = 1792183192359;
// 14:32 in Asia/Kolkata, the timezone playwright.config.ts sets.
const UNTIL = Date.UTC(2026, 9, 18, 9, 2);
// A made-up value: only its absence from the page after saving matters.
// Wizard ids are 32 hex characters (cache/v1/m5-api-deviations.md).
const WIZARD_ID = '0f3a9c2e7b1d4e8fa6c5b2d1e0f9a8b7';
const SOLVE_ID = '9b8a7f6e5d4c3b2a19087f6e5d4c3b2a';
// Split so tools/check-secrets.sh does not read the fake as a credential.
const FAKE_KEY = 'sk-' + 'ant-api03-PLAYWRIGHT-ONLY-not-a-real-key-42';

type Json = Record<string, unknown>;

const PROVIDERS = [
  {
    id: 'anthropic',
    name: 'Anthropic',
    models: [
      { id: 'claude-opus-5-5', name: 'Claude Opus 5.5 (default)' },
      { id: 'claude-sonnet-5-5', name: 'Claude Sonnet 5.5' },
      { id: 'claude-haiku-5-5', name: 'Claude Haiku 5.5' },
    ],
  },
];
const FAKE_PROVIDER = { id: 'fake', name: 'Test provider (canned problems)', models: [{ id: 'canned', name: 'Canned problems' }] };

function test_(idx: number, group: number | 'sample', input: string, output: string): Json {
  return {
    idx,
    sample: group === 'sample',
    subtask_idx: typeof group === 'number' ? group : null,
    input_size: input.length,
    output_size: output.length,
    input_preview: input,
    output_preview: output,
  };
}

function aiDraft(): Json {
  return {
    id: 20,
    slug: 'maximum-subarray',
    title: 'Maximum subarray',
    difficulty: 'medium',
    tags: ['arrays', 'ai draft'],
    origin: 'ai',
    archived: false,
    test_count: 5,
    points: 100,
    validated: false,
    validated_at: null,
    validated_hash: null,
    updated_at: T0,
    statement_md:
      'Given $n$ integers, print the largest sum of a non-empty contiguous subarray.\n\n## Input\n\nThe first line holds $n$ ($1 \\le n \\le 2 \\cdot 10^5$). ' +
      'The second holds $a_1, \\ldots, a_n$ ($|a_i| \\le 10^9$).\n\n## Output\n\nPrint the largest sum.',
    time_limit_ms: 1000,
    memory_limit_mb: 256,
    checker: 'tokens',
    epsilon: 1e-6,
    checker_source: '',
    reference: { language: 'cpp17', source: '#include <bits/stdc++.h>\nint main() {}\n' },
    brute: { language: 'python3', source: 'print(0)\n' },
    validator: { language: 'python3', source: 'import sys\n' },
    generator: {
      language: 'python3',
      source: 'import random\nimport sys\n\nseed = int(sys.argv[1])\nargs = dict(a.split("=", 1) for a in sys.argv[2:])\nrng = random.Random(seed)\nn = int(args["n"])\nprint(n)\nprint(*[rng.randint(-10**9, 10**9) for _ in range(n)])\n',
    },
    plan: [
      { idx: 2, subtask: 1, seed: 1, args: [] },
      { idx: 3, subtask: 1, seed: 2, args: ['neg=1'] },
      { idx: 4, subtask: 2, seed: 3, args: [] },
      { idx: 5, subtask: 2, seed: 4, args: ['max=1'] },
    ],
    samples: [1],
    subtasks: [
      { idx: 1, points: 30, title: 'n up to 1000', tests: '2-3', generator_args: ['n=1000'] },
      { idx: 2, points: 70, title: 'Full range', tests: '4-5', generator_args: ['n=200000'] },
    ],
    tests: [test_(1, 'sample', '5\n-2 1 -3 4 -1\n', '4\n')],
    content_hash: 'h20',
    warnings: ['4 planned tests have no input yet. Use Generate inputs.'],
  };
}

function validated(): Json {
  return {
    ...aiDraft(),
    id: 21,
    slug: 'two-sum',
    title: 'Two sum',
    tags: ['arrays'],
    origin: 'ai',
    validated: true,
    validated_at: T0 - 3600000,
    validated_hash: 'h21',
    content_hash: 'h21',
    warnings: [],
    tests: [1, 2, 3, 4, 5].map((i) => test_(i, i === 1 ? 'sample' : i < 4 ? 1 : 2, '3\n1 2 3\n', '6\n')),
  };
}

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

interface MockOptions {
  /** The key status the page starts with. */
  key?: 'none' | 'active';
  /** Testing mode: the fake provider is offered. */
  testing?: boolean;
  /** How /api/events answers: a long-lived stream of queued events, or 503 (polling). */
  events?: 'stream' | 'down';
  /** How a wizard ends. */
  wizardEnd?: 'draft' | 'error';
  /** Answer wizard GETs with "thinking" forever, so only the live event can finish it. */
  wizardOnlyByEvent?: boolean;
}

/** Installs the API mock for one page and returns its mutable state. */
async function mockApi(page: Page, opts: MockOptions = {}) {
  const state = {
    key:
      opts.key === 'active'
        ? { active: true, provider: 'anthropic', model: 'claude-opus-5-5', active_until: UNTIL }
        : { active: false, provider: 'anthropic', model: 'claude-opus-5-5', active_until: null as number | null },
    keyBodies: [] as Json[],
    wizardBodies: [] as Json[],
    wizardGets: 0,
    wizard: null as Json | null,
    events: [] as string[],
    problems: { 20: aiDraft(), 21: validated() } as Record<number, Json>,
    puts: [] as Json[],
    inputRuns: 0,
    inputGen: null as Json | null,
    inputScript: [] as Json[],
    generation: null as Json | null,
    solveGets: 0,
    submissionGets: 0,
    requestedUrls: [] as string[],
  };
  const providers = opts.testing ? [...PROVIDERS, FAKE_PROVIDER] : PROVIDERS;

  if (opts.events === 'stream') await longLivedEvents(page);

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      state.requestedUrls.push(req.url());

      if (path === '/api/me' && method === 'GET') return json(route, 200, ADMIN);
      if (path === '/api/auth/config') return json(route, 200, { mode: opts.testing ? 'testing' : 'production' });

      if (path === '/api/events') {
        if (opts.events !== 'stream') return apiError(route, 503, 'busy', 'Too many streams.');
        const body = state.events.splice(0).join('');
        return route.fulfill({ status: 200, contentType: 'text/event-stream', body: body || ': idle\n\n' });
      }

      if (path === '/api/admin/ai/key') {
        if (method === 'GET') return json(route, 200, { ...state.key, providers, server_now: T0 });
        if (method === 'PUT') {
          const body = req.postDataJSON() as Json;
          state.keyBodies.push(body);
          if (body.provider === 'anthropic' && body.key !== FAKE_KEY) {
            return apiError(route, 400, 'bad_key', 'Anthropic refused the key. Check it and paste it again.');
          }
          state.key = { active: true, provider: String(body.provider), model: String(body.model), active_until: UNTIL };
          return json(route, 200, { ...state.key, providers, server_now: T0 });
        }
        if (method === 'DELETE') {
          state.key = { active: false, provider: 'anthropic', model: 'claude-opus-5-5', active_until: null };
          return json(route, 204);
        }
      }

      if (path === '/api/admin/ai/wizard' && method === 'POST') {
        if (!state.key.active) return apiError(route, 409, 'no_key', 'Add a key in AI assistant first.');
        state.wizardBodies.push(req.postDataJSON() as Json);
        state.wizard = { id: WIZARD_ID, kind: 'generate', state: 'thinking', started_at: T0, server_now: T0 };
        state.wizardGets = 0;
        return json(route, 202, { wizard_id: WIZARD_ID });
      }
      if (path === `/api/admin/ai/wizard/${WIZARD_ID}` && method === 'GET') {
        if (!state.wizard) return apiError(route, 404, 'not_found', 'There is no such request.');
        state.wizardGets++;
        if (!opts.wizardOnlyByEvent && state.wizardGets >= 2) {
          state.wizard =
            opts.wizardEnd === 'error'
              ? {
                  id: WIZARD_ID,
                  kind: 'generate',
                  state: 'error',
                  error: {
                    code: 'invalid_answer',
                    message: "The model's answer could not be used.",
                    reasons: ['subtasks: points add up to 90, not 100.', 'reference: the C++ source is empty.'],
                  },
                }
              : { id: WIZARD_ID, kind: 'generate', state: 'draft_created', problem_id: 20 };
        }
        return json(route, 200, state.wizard);
      }
      if (path === `/api/admin/ai/wizard/${SOLVE_ID}` && method === 'GET') {
        state.solveGets++;
        return json(route, 200, state.solveGets >= 2 ? { id: SOLVE_ID, kind: 'solve', state: 'submitted', submission_id: 81, problem_id: 21 } : { id: SOLVE_ID, kind: 'solve', state: 'thinking' });
      }
      if (path === '/api/submissions/81' && method === 'GET') {
        state.submissionGets++;
        const done = state.submissionGets >= 3;
        return json(route, 200, {
          id: 81,
          problem_id: 21,
          language: 'cpp17',
          state: done ? 'done' : 'running',
          verdict: done ? 'AC' : '',
          score: done ? 100 : 0,
          max_score: 100,
          max_time_ms: 40,
          max_memory_kb: 3200,
          created_at: T0,
          judged_at: done ? T0 + 4000 : null,
          queue_position: null,
          compile_output: '',
          samples: [],
          subtasks: [],
          server_now: T0,
        });
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
          Object.assign(p, body, {
            subtasks: (body.subtasks as Json[]).map((s, i) => ({ ...s, idx: i + 1 })),
            tags: body.tags,
            content_hash: `h${state.puts.length}x`,
            updated_at: T0 + 1000,
          });
          return json(route, 200, p);
        }
        if (rest === '/validation' && method === 'GET') {
          if (id === 21) {
            return json(route, 200, {
              id: 3,
              state: 'passed',
              content_hash: 'h21',
              current: true,
              queued_at: T0,
              finished_at: T0,
              queue_position: null,
              compile: [],
              tests: [],
              summary: {
                reference_passed: 5,
                reference_total: 5,
                brute_agreed: 5,
                brute_total: 5,
                validator_passed: 5,
                validator_total: 5,
                max_time_ms: 120,
                time_limit_ms: 1000,
                problems: [],
                warnings: [],
              },
              error: null,
            });
          }
          return apiError(route, 404, 'not_found', 'This problem has not been validated yet.');
        }
        if (rest === '/generation' && method === 'GET') {
          return state.generation ? json(route, 200, state.generation) : apiError(route, 404, 'not_found', 'Outputs were never generated.');
        }
        if (rest === '/input-generation' && method === 'GET') {
          if (state.inputScript.length > 0) state.inputGen = state.inputScript.shift()!;
          return state.inputGen ? json(route, 200, state.inputGen) : apiError(route, 404, 'not_found', 'Inputs were never generated.');
        }
        if (rest === '/generate-inputs' && method === 'POST') {
          state.inputRuns++;
          const plan = (p.plan as Json[]) ?? [];
          const planned = plan.length;
          const idxs = plan.map((t) => Number(t.idx));
          const ok = state.inputRuns > 1;
          state.inputGen = { id: state.inputRuns, state: 'queued', planned, results: [], summary: null, error: null };
          state.inputScript = [
            { id: state.inputRuns, state: 'running', planned, results: idxs.slice(0, 1).map((idx) => ({ idx, verdict: 'OK' })), summary: null, error: null },
            {
              id: state.inputRuns,
              state: 'done',
              planned,
              results: idxs.map((idx, i) => (!ok && i === 2 ? { idx, verdict: 'RE', message: '' } : { idx, verdict: 'OK' })),
              summary: ok ? { applied: planned, generating_outputs: true, problems: [] } : { applied: 0, problems: [] },
              error: null,
            },
          ];
          if (ok) {
            state.generation = { id: 1, state: 'done', results: idxs.map((idx) => ({ idx, verdict: 'OK', time_ms: 30 })), summary: { applied: planned, problems: [] }, error: null };
          }
          return json(route, 202, state.inputGen);
        }
        if (rest === '/solve' && method === 'POST') {
          if (!state.key.active) return apiError(route, 409, 'no_key', 'Add a key in AI assistant first.');
          state.solveGets = 0;
          state.submissionGets = 0;
          return json(route, 202, { wizard_id: SOLVE_ID });
        }
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

/** Fails the test when the key shows up in the page, its storage or any address it asked for. */
async function expectKeyGone(page: Page, state: Awaited<ReturnType<typeof mockApi>>) {
  const found = await page.evaluate((key) => {
    const inputs = [...document.querySelectorAll('input, textarea')].some((el) => (el as HTMLInputElement).value.includes(key));
    const html = document.documentElement.outerHTML.includes(key);
    const stored = [localStorage, sessionStorage].some((s) =>
      Array.from({ length: s.length }, (_, i) => s.key(i) ?? '').some((k) => k.includes(key) || (s.getItem(k) ?? '').includes(key)),
    );
    return { inputs, html, stored, url: location.href.includes(key), title: document.title.includes(key) };
  }, FAKE_KEY);
  expect(found).toEqual({ inputs: false, html: false, stored: false, url: false, title: false });
  expect(state.requestedUrls.some((u) => u.includes(FAKE_KEY))).toBe(false);
}

interface Screen {
  name: string;
  path: string;
  title: string;
  opts?: MockOptions;
  ready: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
  {
    name: 'ai-settings',
    path: '/admin/ai',
    title: 'AI assistant · podium',
    ready: (page) => expect(page.getByLabel('API key')).toBeVisible(),
  },
  {
    name: 'ai-settings-active',
    path: '/admin/ai',
    title: 'AI assistant · podium',
    opts: { key: 'active' },
    ready: (page) => expect(page.getByText('Key active until 14:32')).toBeVisible(),
  },
  {
    name: 'ai-wizard-brief',
    path: '/admin/ai/new',
    title: 'New problem with AI · podium',
    opts: { key: 'active' },
    ready: (page) => expect(page.getByText('Using Claude Opus 5.5. Key active until 14:32.')).toBeVisible(),
  },
  {
    name: 'ai-wizard-paste',
    path: '/admin/ai/new',
    title: 'New problem with AI · podium',
    opts: { key: 'active' },
    ready: async (page) => {
      await page.getByLabel('From pasted text').check();
      await expect(page.getByText('Problems on LeetCode and similar sites are copyrighted.', { exact: false })).toBeVisible();
    },
  },
  {
    name: 'ai-wizard-created',
    path: '/admin/ai/new',
    title: 'New problem with AI · podium',
    opts: { key: 'active' },
    ready: async (page) => {
      await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
      await page.getByRole('button', { name: 'Generate' }).click();
      await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible({ timeout: 10_000 });
    },
  },
  {
    name: 'ai-editor-generator',
    path: '/admin/problems/20',
    title: 'Maximum subarray · Problems · podium',
    opts: { key: 'active' },
    ready: (page) => expect(page.getByRole('region', { name: 'Generator', exact: true })).toBeVisible(),
  },
  {
    name: 'ai-editor-solved',
    path: '/admin/problems/21',
    title: 'Two sum · Problems · podium',
    opts: { key: 'active' },
    ready: async (page) => {
      await page.getByRole('button', { name: 'Ask a model to solve it' }).click();
      await page.getByRole('dialog').getByRole('button', { name: 'Ask a model' }).click();
      await expect(page.locator('.solve-line')).toBeVisible({ timeout: 10_000 });
    },
  },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(`${screen.name}`, async ({ page }) => {
        await mockApi(page, screen.opts);
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

test('the bank links to the wizard and the settings, and marks AI drafts', async ({ page }) => {
  await mockApi(page);
  await page.route('**/api/admin/problems', (route) =>
    json(route, 200, {
      problems: [
        { id: 20, slug: 'maximum-subarray', title: 'Maximum subarray', difficulty: 'medium', tags: ['arrays', 'ai draft'], origin: 'ai', archived: false, test_count: 1, points: 100, validated: false, validated_at: null, updated_at: T0 },
      ],
    }),
  );
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /Maximum subarray/ });
  await expect(row.getByText('AI draft')).toBeVisible();
  await expect(row.locator('.tags')).toHaveText('arrays');
  await page.getByRole('link', { name: 'New problem with AI' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'New problem with AI' })).toBeFocused();
  await expect(page.getByText('No key yet.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Generate' })).toBeDisabled();
  await page.getByRole('link', { name: 'Add a key in AI assistant' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'AI assistant' })).toBeFocused();
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Problems' })).toHaveAttribute('aria-current', 'page');
});

test('saving a key checks it, clears the field and never keeps the key', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/ai');
  await expect(page.getByText('The key stays in the server\'s memory for 2 hours', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Provider')).toHaveValue('anthropic');
  await expect(page.getByLabel('Model')).toHaveValue('claude-opus-5-5');
  const field = page.getByLabel('API key');
  await expect(field).toHaveAttribute('type', 'password');
  await expect(field).toHaveAttribute('autocomplete', 'off');

  // An empty key is caught before anything is sent.
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(field).toBeFocused();
  await expect(page.getByText('Paste your API key.')).toBeVisible();
  expect(state.keyBodies).toHaveLength(0);

  // A refused key: the reason sits under the field, tied to it, and the field is empty again.
  await field.fill('sk-' + 'ant-wrong-and-typed-by-mistake');
  await page.getByRole('button', { name: 'Save key' }).click();
  const refusal = page.getByText('Anthropic refused the key. Check it and paste it again.');
  await expect(refusal).toBeVisible();
  await expect(field).toHaveValue('');
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(field).toHaveAttribute('aria-describedby', /api-key-error/);
  await expect(field).toBeFocused();

  // A good key, chosen with another model.
  await page.getByLabel('Model').selectOption('claude-sonnet-5-5');
  await field.fill(FAKE_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Key active until 14:32')).toBeVisible();
  await expect(page.getByText('Anthropic, Claude Sonnet 5.5')).toBeVisible();
  await expect(page.getByText('Key saved', { exact: true })).toBeVisible();
  await expect(field).toHaveValue('');
  expect(state.keyBodies.at(-1)).toEqual({ provider: 'anthropic', model: 'claude-sonnet-5-5', key: FAKE_KEY });
  await expectKeyGone(page, state);

  // Reloading shows the state from the server, still without the key.
  await page.reload();
  await expect(page.getByText('Key active until 14:32')).toBeVisible();
  await expectKeyGone(page, state);

  await page.getByRole('button', { name: 'Forget key' }).click();
  await expect(page.getByText('No key yet. Add one below to draft problems with AI.')).toBeVisible();
  await expect(page.getByLabel('Provider')).toBeFocused();
  await expectKeyGone(page, state);
});

test('in testing mode the fake provider needs no key', async ({ page }) => {
  const state = await mockApi(page, { testing: true });
  await page.goto('/admin/ai');
  await expect(page.getByText('Testing mode: choose the fake provider to try the wizard without a key.')).toBeVisible();
  await page.getByLabel('Provider').selectOption('fake');
  await expect(page.getByLabel('API key')).toHaveCount(0);
  await expect(page.getByText('The fake provider needs no key.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Use fake provider' }).click();
  await expect(page.getByText('Fake provider active, no key needed')).toBeVisible();
  expect(state.keyBodies).toEqual([{ provider: 'fake', model: 'canned', key: '' }]);
});

test('a wizard from a brief follows the live event to the draft', async ({ page }) => {
  const state = await mockApi(page, { key: 'active', events: 'stream', wizardOnlyByEvent: true });
  await page.goto('/admin/ai/new');
  await expect(page.getByLabel('From a brief')).toBeChecked();

  await page.getByRole('button', { name: 'Generate' }).click();
  await expect(page.getByLabel('Topic', { exact: true })).toBeFocused();
  await expect(page.getByText('Say what the problem is about', { exact: false })).toBeVisible();

  await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
  await page.getByLabel('Difficulty').selectOption('hard');
  await page.getByLabel('Constraints (optional)').fill('n up to 200000, values up to 10^9 in size');
  await page.getByLabel('Subtask ideas (optional)').fill('30 points for n up to 1000');
  await page.getByRole('button', { name: 'Generate' }).click();

  await expect(page.getByRole('heading', { name: 'Progress' })).toBeFocused();
  await expect(page.locator('.step-current')).toHaveText(/Writing the problem/);
  await expect(page).toHaveURL(new RegExp(`/admin/ai/new\\?wizard=${WIZARD_ID}$`));
  expect(state.wizardBodies).toEqual([
    {
      mode: 'brief',
      brief: { topic: 'Maximum subarray sum', difficulty: 'hard', constraints: 'n up to 200000, values up to 10^9 in size', subtasks: '30 points for n up to 1000' },
    },
  ]);
  await page.screenshot({ path: `${shots()}ai-wizard-writing-1440-light.png`, fullPage: true });

  // Only the live event can finish this wizard: GETs keep saying "thinking".
  state.events.push(`event: wizard\ndata: ${JSON.stringify({ id: WIZARD_ID, kind: 'generate', state: 'draft_created', problem_id: 20 })}\n\n`);
  await expect(page.locator('.step-current')).toHaveText(/Draft created, generating tests/);
  await expect(page.getByRole('status').filter({ hasText: 'Draft created, generating tests' })).toHaveCount(1);
  await page.getByRole('link', { name: 'Open the draft in the editor' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
});

test('a wizard from pasted text polls when live updates are down', async ({ page }) => {
  const state = await mockApi(page, { key: 'active', events: 'down' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('From pasted text').check();
  const text = page.getByLabel('Problem text');
  await expect(text).toHaveAttribute('aria-describedby', /paste-note/);
  const pasted = 'Given an array of integers and a target, return the positions of the two numbers that add up to the target.';
  await text.fill(`  ${pasted}\n`);
  await expect(page.getByText(`${pasted.length} of 20,000 characters`)).toBeVisible();
  await page.getByRole('button', { name: 'Generate' }).click();
  await expect(page.locator('.step-current')).toHaveText(/Writing the problem/);
  await expect(page.locator('.step-current')).toHaveText(/Draft created, generating tests/, { timeout: 10_000 });
  expect(state.wizardBodies).toEqual([{ mode: 'paste', text: pasted }]);
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toHaveAttribute('href', '/admin/problems/20');

  // A reload picks the request up from the address.
  await page.reload();
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible();
});

test('a wizard failure shows the reasons and keeps the request', async ({ page }) => {
  await mockApi(page, { key: 'active', wizardEnd: 'error' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Interval scheduling');
  await page.getByRole('button', { name: 'Generate' }).click();
  const alert = page.getByRole('alert').filter({ hasText: "The model's answer could not be used." });
  await expect(alert).toContainText("The model's answer could not be used.", { timeout: 10_000 });
  await expect(alert.getByRole('listitem')).toHaveText(['subtasks: points add up to 90, not 100.', 'reference: the C++ source is empty.']);
  await expect(page.locator('.step-failed')).toHaveText(/Writing the problem/);
  await expect(page.getByLabel('Topic', { exact: true })).toHaveValue('Interval scheduling');
  await page.screenshot({ path: `${shots()}ai-wizard-error-1440-light.png`, fullPage: true });
});

test('the editor edits the generator and plan, and generates inputs', async ({ page }) => {
  const state = await mockApi(page, { key: 'active' });
  await page.goto('/admin/problems/20');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
  await expect(page.locator('.action-bar').getByText('AI draft')).toBeVisible();
  await expect(page.getByText('The AI wizard wrote this draft.', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Tags')).toHaveValue('arrays');

  const section = page.getByRole('region', { name: 'Generator', exact: true });
  await expect(section.getByRole('group', { name: 'Generator program' })).toBeVisible();
  await expect(section.getByLabel('Subtask 1, n up to 1000')).toHaveValue('n=1000');
  const table = section.getByRole('table', { name: 'Planned tests' });
  await expect(table.getByRole('row')).toHaveCount(5);
  await expect(page.getByLabel('Row 4, arguments')).toHaveValue('max=1');

  // Add a row: the next free test number, the last row's subtask and the next seed.
  await section.getByRole('button', { name: 'Add planned test' }).click();
  await expect(page.getByLabel('Row 5, seed')).toBeFocused();
  await expect(page.getByLabel('Row 5, test number')).toHaveValue('6');
  await expect(page.getByLabel('Row 5, subtask')).toHaveValue('2');
  await expect(page.getByLabel('Row 5, seed')).toHaveValue('5');
  await page.getByLabel('Row 5, arguments').fill('n=200000 {bad}');
  await page.getByRole('button', { name: 'Remove planned test 2' }).click();
  await expect(section.getByRole('button', { name: 'Add planned test' })).toBeFocused();

  // A bad argument is caught before saving and tied to its field.
  await page.getByRole('button', { name: 'Save changes' }).click();
  const args = page.getByLabel('Row 4, arguments');
  await expect(args).toBeFocused();
  await expect(args).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByText('"{bad}" is not a valid argument.', { exact: false }).first()).toBeVisible();
  await page.getByRole('link', { name: /Planned test 4 arguments/ }).click();
  await expect(args).toBeFocused();
  await args.fill('n=200000 edge=1');
  await page.getByLabel('Subtask 2, Full range').fill('n=200000 neg=0');

  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  const put = state.puts.at(-1)!;
  expect(put.tags).toEqual(['arrays']);
  expect(put.plan).toEqual([
    { idx: 3, subtask: 1, seed: 2, args: ['neg=1'] },
    { idx: 4, subtask: 2, seed: 3, args: [] },
    { idx: 5, subtask: 2, seed: 4, args: ['max=1'] },
    { idx: 6, subtask: 2, seed: 5, args: ['n=200000', 'edge=1'] },
  ]);
  expect((put.subtasks as Json[]).map((s) => s.generator_args)).toEqual([['n=1000'], ['n=200000', 'neg=0']]);
  expect((put.generator as Json).language).toBe('python3');
  // The tag went with the person's save.
  await expect(page.locator('.action-bar').getByText('AI draft')).toHaveCount(0);

  // The first run names the failing test; the second succeeds and outputs follow.
  await section.getByRole('button', { name: 'Generate inputs' }).click();
  await expect(section.getByText('Generating inputs', { exact: false }).first()).toBeVisible();
  await expect(section.getByText('The generator failed on test 5 (subtask 2, seed 4): Runtime error')).toBeVisible({ timeout: 10_000 });
  await expect(section.getByText('Some inputs could not be generated, so none were applied.')).toBeVisible();
  await page.screenshot({ path: `${shots()}ai-editor-inputs-failed-1440-light.png`, fullPage: true });
  await section.getByRole('button', { name: 'Generate inputs' }).click();
  await expect(section.getByText('Generated 4 inputs. Outputs are being generated from the reference solution, then the problem is validated.')).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Generated 4 outputs.', { exact: false })).toBeVisible({ timeout: 10_000 });
});

test('the generator section is reachable from the keyboard on a phone', async ({ page }) => {
  await mockApi(page, { key: 'active' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems/20');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
  await page.getByRole('button', { name: 'Remove generator' }).focus();
  const reached: string[] = [];
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab');
    const name = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      return el ? (el.getAttribute('aria-label') || el.id || el.textContent || '').trim() : '';
    });
    reached.push(name);
    if (name === 'add-plan-row') break;
  }
  for (const name of ['generator-language', 'generator-source', 'subtask-0-args', 'subtask-1-args', 'Row 1, test number', 'Row 1, subtask', 'Row 1, seed', 'Row 1, arguments', 'Remove planned test 2', 'add-plan-row']) {
    expect(reached, `Tab did not reach "${name}"`).toContain(name);
  }
  // Rows stack: seed sits under the test number.
  const idx = await page.getByLabel('Row 1, test number').boundingBox();
  const seed = await page.getByLabel('Row 1, seed').boundingBox();
  expect(seed!.y).toBeGreaterThan(idx!.y + idx!.height - 1);
});

test('asking a model to solve a problem confirms first and shows the score', async ({ page }) => {
  await mockApi(page, { key: 'active' });
  await page.goto('/admin/problems/21');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Two sum');
  const ask = page.getByRole('button', { name: 'Ask a model to solve it' });
  await ask.click();
  const dialog = page.getByRole('dialog', { name: 'Ask a model to solve this problem?' });
  await expect(dialog).toContainText('The statement goes to Claude Opus 5.5 with your key');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(ask).toBeFocused();

  await ask.click();
  await dialog.getByRole('button', { name: 'Ask a model' }).click();
  await expect(page.getByText('Waiting for the model to answer').first()).toBeVisible();
  await expect(page.getByText("Judging the model's solution").first()).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('A model solved this zero-shot: 100 of 100').first()).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Anyone who pastes the statement into a model gets full marks.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: "See the model's submission" })).toHaveAttribute('href', '/admin/problems/21/try');
  await expect(page.getByRole('button', { name: 'Ask a model again' })).toBeEnabled();
});

test('without a key the solve check says where to add one', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/21');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Two sum');
  await expect(page.getByRole('button', { name: 'Ask a model to solve it' })).toBeDisabled();
  await expect(page.getByRole('link', { name: 'Add one in AI assistant' })).toHaveAttribute('href', '/admin/ai');
});
