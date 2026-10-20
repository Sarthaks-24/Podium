// Fixtures and the API mock of ai.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type Page, type Route } from '@playwright/test';
import { longLivedEvents } from '../sse';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m5/`;
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

export const T0 = 1792183192359;
// 14:32 in Asia/Kolkata, the timezone playwright.config.ts sets.
export const UNTIL = Date.UTC(2026, 9, 18, 9, 2);
// A made-up value: only its absence from the page after saving matters.
// Wizard ids are 32 hex characters (cache/v1/m5-api-deviations.md).
export const WIZARD_ID = '0f3a9c2e7b1d4e8fa6c5b2d1e0f9a8b7';
export const SOLVE_ID = '9b8a7f6e5d4c3b2a19087f6e5d4c3b2a';
// Split so tools/check-secrets.sh does not read the fake as a credential.
export const FAKE_KEY = 'sk-' + 'ant-api03-PLAYWRIGHT-ONLY-not-a-real-key-42';

export type Json = Record<string, unknown>;

export const PROVIDERS = [
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
export const FAKE_PROVIDER = { id: 'fake', name: 'Test provider (canned problems)', models: [{ id: 'canned', name: 'Canned problems' }] };

export function test_(idx: number, group: number | 'sample', input: string, output: string): Json {
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

export function aiDraft(): Json {
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

export function validated(): Json {
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

export interface MockOptions {
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
export async function mockApi(page: Page, opts: MockOptions = {}) {
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
export async function expectKeyGone(page: Page, state: Awaited<ReturnType<typeof mockApi>>) {
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

export interface Screen {
  name: string;
  path: string;
  title: string;
  opts?: MockOptions;
  ready: (page: Page) => Promise<void>;
}

export const SCREENS: Screen[] = [
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
    title: 'Maximum subarray · Problem bank · podium',
    opts: { key: 'active' },
    ready: (page) => expect(page.getByRole('region', { name: 'Generator', exact: true })).toBeVisible(),
  },
  {
    name: 'ai-editor-solved',
    path: '/admin/problems/21',
    title: 'Two sum · Problem bank · podium',
    opts: { key: 'active' },
    ready: async (page) => {
      await page.getByRole('button', { name: 'Ask a model to solve it' }).click();
      await page.getByRole('dialog').getByRole('button', { name: 'Ask a model' }).click();
      await expect(page.locator('.solve-line')).toBeVisible({ timeout: 10_000 });
    },
  },
];
