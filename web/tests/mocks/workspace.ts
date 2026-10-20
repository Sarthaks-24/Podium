// Fixtures and the API mock of workspace.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type Page, type Route } from '@playwright/test';
import { longLivedEvents } from '../sse';

export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m3/`;
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

export type Json = Record<string, unknown>;

export const T0 = 1792183192359;

export const STATEMENT =
  'Given two integers $a$ and $b$ with $|a|, |b| \\le 10^9$, print $a + b$.\n\n' +
  '## Input\n\nOne line with $a$ and $b$, separated by a space.\n\n' +
  '## Output\n\nPrint the sum:\n\n$$\nS = a + b\n$$\n\n' +
  'Use `long long` in C++, because $2 \\cdot 10^9$ does not fit in `int`.';

export function test_(idx: number, verdict: string, time = 10 + idx * 3): Json {
  return { idx, verdict, time_ms: time, memory_kb: verdict === '' ? 0 : 3300 };
}

/** A submission at a stage of judging, for the a-plus-b subtasks 3-4 (40) and 5 (60). */
export function submission(id: number, stage: 'queued' | 'samples' | 'subtask1' | 'partial' | 'accepted' | 'ce', over: Json = {}): Json {
  const s = (v1: string, v2: string) => [test_(1, v1), test_(2, v2)];
  const base = {
    id,
    problem_id: 1,
    language: 'cpp17',
    state: 'running',
    verdict: '',
    score: 0,
    max_score: 100,
    max_time_ms: 0,
    max_memory_kb: 0,
    created_at: T0 + id * 60000,
    judged_at: null,
    queue_position: null,
    compile_output: '',
    samples: s('', ''),
    subtasks: [
      { idx: 1, points: 40, earned: 0, verdict: '', tests: [test_(3, ''), test_(4, '')] },
      { idx: 2, points: 60, earned: 0, verdict: '', tests: [test_(5, '')] },
    ],
  };
  switch (stage) {
    case 'queued':
      return { ...base, state: 'queued', queue_position: 3, ...over };
    case 'samples':
      return { ...base, samples: s('AC', 'AC'), ...over };
    case 'subtask1':
      return {
        ...base,
        samples: s('AC', 'AC'),
        subtasks: [
          { idx: 1, points: 40, earned: 40, verdict: 'AC', tests: [test_(3, 'AC'), test_(4, 'AC')] },
          { idx: 2, points: 60, earned: 0, verdict: '', tests: [test_(5, '')] },
        ],
        ...over,
      };
    case 'partial':
      return {
        ...base,
        state: 'done',
        verdict: 'WA',
        score: 40,
        max_time_ms: 31,
        max_memory_kb: 3300,
        judged_at: T0 + id * 60000 + 5000,
        samples: s('AC', 'AC'),
        subtasks: [
          { idx: 1, points: 40, earned: 40, verdict: 'AC', tests: [test_(3, 'AC'), test_(4, 'AC')] },
          { idx: 2, points: 60, earned: 0, verdict: 'WA', tests: [test_(5, 'WA', 31)] },
        ],
        ...over,
      };
    case 'accepted':
      return {
        ...base,
        state: 'done',
        verdict: 'AC',
        score: 100,
        judged_at: T0 + id * 60000 + 5000,
        samples: s('AC', 'AC'),
        subtasks: [
          { idx: 1, points: 40, earned: 40, verdict: 'AC', tests: [test_(3, 'AC'), test_(4, 'AC')] },
          { idx: 2, points: 60, earned: 60, verdict: 'AC', tests: [test_(5, 'AC')] },
        ],
        ...over,
      };
    case 'ce':
      return {
        ...base,
        state: 'done',
        verdict: 'CE',
        judged_at: T0 + id * 60000 + 3000,
        samples: s('', ''),
        compile_output: "main.cpp: In function 'int main()':\nmain.cpp:3:5: error: expected ';' before 'return'\n    3 |     return 0;\n      |     ^~~~~~\n",
        ...over,
      };
  }
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
  draft?: { language: string; source: string } | null;
  submissions?: Json[];
  failDrafts?: boolean;
}

/** Installs the API mock for one page and returns its mutable state. */
export async function mockApi(page: Page, options: MockOptions = {}) {
  await longLivedEvents(page);
  const state = {
    draft: options.draft ? { ...options.draft, updated_at: T0 } : null as Json | null,
    submissions: structuredClone(options.submissions ?? []) as Json[],
    failDrafts: options.failDrafts ?? false,
    submitMode: 'ok' as 'ok' | 'rate' | 'abort-once' | 'offline',
    draftPuts: [] as Json[],
    submits: [] as Json[],
    runs: [] as Json[],
    integrity: [] as Json[],
    /** Events the next stream connections send, one per connection. */
    stream: [] as { type: string; data: Json }[],
    connections: 0,
    nextId: 20,
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();

      if (path === '/api/me' && method === 'GET') return json(route, 200, ADMIN);
      if (path === '/api/events' && method === 'GET') {
        state.connections++;
        const next = state.stream.shift();
        let body = `event: clock\ndata: ${JSON.stringify({ server_now: T0 + 1000 })}\n\n`;
        if (next) {
          if (next.type === 'submission') {
            const i = state.submissions.findIndex((s) => s.id === next.data.id);
            if (i >= 0) state.submissions[i] = next.data;
          }
          body += `event: ${next.type}\ndata: ${JSON.stringify(next.data)}\n\n`;
        }
        return route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' }, body });
      }
      if (path === '/api/workspace/1' && method === 'GET') {
        return json(route, 200, {
          problem_id: 1,
          title: 'A plus B',
          statement_md: STATEMENT,
          time_limit_ms: 1000,
          memory_limit_mb: 256,
          languages: [
            { id: 'cpp17', name: 'C++17' },
            { id: 'java21', name: 'Java 21' },
            { id: 'python3', name: 'Python 3' },
            { id: 'node22', name: 'JavaScript (Node 22)' },
          ],
          samples: [
            { idx: 1, input: '1 2\n', output: '3\n', truncated: false },
            { idx: 2, input: '-5 7\n', output: '2\n', truncated: false },
          ],
          draft: state.draft,
          submissions: [...state.submissions].sort((a, b) => Number(b.id) - Number(a.id)),
          server_now: T0,
        });
      }
      if (path === '/api/workspace/404' && method === 'GET') {
        return apiError(route, 404, 'not_found', 'There is no problem with that id.');
      }
      if (path === '/api/workspace/1/draft' && method === 'PUT') {
        const body = req.postDataJSON() as Json;
        state.draftPuts.push(body);
        if (state.failDrafts) return apiError(route, 500, 'internal', 'The draft could not be saved.');
        state.draft = { ...body, updated_at: Date.now() + 10_000_000 };
        return json(route, 204);
      }
      if (path === '/api/workspace/1/events' && method === 'POST') {
        state.integrity.push(req.postDataJSON() as Json);
        return json(route, 204);
      }
      if (path === '/api/workspace/1/submit' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.submits.push(body);
        if (state.submitMode === 'offline') return route.abort('internetdisconnected');
        if (state.submitMode === 'abort-once') {
          state.submitMode = 'ok';
          return route.abort('failed');
        }
        if (state.submitMode === 'rate') {
          return apiError(route, 429, 'rate_limited', 'Submissions are limited to one every 30 seconds. Try again in 12 s.', {
            retry_after_s: 12,
          });
        }
        const existing = state.submissions.find((s) => s.client_key === body.client_key);
        if (existing) return json(route, 200, existing);
        const s = { ...submission(state.nextId++, 'queued'), client_key: body.client_key, server_now: T0 + 2000 };
        state.submissions.push(s);
        return json(route, 202, s);
      }
      if (path === '/api/workspace/1/run' && method === 'POST') {
        const body = req.postDataJSON() as Json;
        state.runs.push(body);
        return json(route, 202, { id: 50 + state.runs.length, state: 'queued', queue_position: 1, compile_output: '', results: [], server_now: T0 });
      }
      const sub = path.match(/^\/api\/submissions\/(\d+)$/);
      if (sub && method === 'GET') {
        const s = state.submissions.find((x) => x.id === Number(sub[1]));
        return s ? json(route, 200, s) : apiError(route, 404, 'not_found', 'No such submission.');
      }
      const run = path.match(/^\/api\/runs\/(\d+)$/);
      if (run && method === 'GET') {
        return json(route, 200, { id: Number(run[1]), state: 'running', compile_output: '', results: [] });
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

export const HISTORY = [submission(7, 'partial'), submission(6, 'ce')];

/** The editor's text, line by line. */
export async function editorText(page: Page): Promise<string> {
  return page.locator('.cm-content').evaluate((el) => Array.from(el.querySelectorAll('.cm-line'), (l) => l.textContent ?? '').join('\n'));
}

export async function openWorkspace(page: Page) {
  await page.goto('/admin/problems/1/try');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.locator('.cm-content')).toHaveCount(1);
}
