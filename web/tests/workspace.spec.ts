// Screen sweep and flows for the M3 workspace at /admin/problems/{id}/try.
// Every /api/ call is answered by an in-test mock of the routes in
// cache/v1/m3-plan.md sections 5 to 7a, one per page. GET /api/events is
// mocked as a stream that sends what the test queued and then ends, so the
// app's EventSource reconnects (after a jittered delay under 1.5 s) and
// picks up the next queued event: that is how the subtask strip is seen to
// fill step by step.
//
// The sweep saves screenshots to cache/screenshots-m3/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1 and
// no horizontal overflow. The flows cover submitting and the strip filling,
// running on samples with the diff, compile error line jumps, the draft
// coming back after a reload, the 429 message, offline retry with the same
// idempotency key, keyboard shortcuts and integrity counts.
import { expect, test, type Page, type Route } from '@playwright/test';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m3/`;
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

type Json = Record<string, unknown>;

const T0 = 1792183192359;

const STATEMENT =
  'Given two integers $a$ and $b$ with $|a|, |b| \\le 10^9$, print $a + b$.\n\n' +
  '## Input\n\nOne line with $a$ and $b$, separated by a space.\n\n' +
  '## Output\n\nPrint the sum:\n\n$$\nS = a + b\n$$\n\n' +
  'Use `long long` in C++, because $2 \\cdot 10^9$ does not fit in `int`.';

function test_(idx: number, verdict: string, time = 10 + idx * 3): Json {
  return { idx, verdict, time_ms: time, memory_kb: verdict === '' ? 0 : 3300 };
}

/** A submission at a stage of judging, for the a-plus-b subtasks 3-4 (40) and 5 (60). */
function submission(id: number, stage: 'queued' | 'samples' | 'subtask1' | 'partial' | 'accepted' | 'ce', over: Json = {}): Json {
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
  draft?: { language: string; source: string } | null;
  submissions?: Json[];
  failDrafts?: boolean;
}

/** Installs the API mock for one page and returns its mutable state. */
async function mockApi(page: Page, options: MockOptions = {}) {
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

const HISTORY = [submission(7, 'partial'), submission(6, 'ce')];

/** The editor's text, line by line. */
async function editorText(page: Page): Promise<string> {
  return page.locator('.cm-content').evaluate((el) => Array.from(el.querySelectorAll('.cm-line'), (l) => l.textContent ?? '').join('\n'));
}

async function openWorkspace(page: Page) {
  await page.goto('/admin/problems/1/try');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.locator('.cm-content')).toHaveCount(1);
}

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('workspace sweep', async ({ page }) => {
      await mockApi(page, { submissions: HISTORY });
      await page.setViewportSize({ width: 1440, height: 900 });
      await openWorkspace(page);
      await expect(page.locator('.statement .katex').first()).toBeVisible();
      await page.evaluate(() => document.fonts.ready);

      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
        await expect(page.locator('h1')).toHaveCount(1);
        await expect(page).toHaveTitle('Try A plus B · podium');
        if (width < 1024) await expect(page.getByRole('tab', { name: 'Problem' })).toHaveAttribute('aria-selected', 'true');
        await page.screenshot({ path: `${shots()}workspace-${width}-${theme}.png`, fullPage: width < 1024 });

        if (width < 1024) {
          await page.getByRole('tab', { name: 'Code' }).click();
          await expect(page.locator('.cm-content')).toBeVisible();
          await page.screenshot({ path: `${shots()}workspace-code-${width}-${theme}.png`, fullPage: true });
          if (width < 768) {
            await page.getByRole('tab', { name: 'Result' }).click();
            await expect(page.getByRole('heading', { name: 'Latest submission' })).toBeVisible();
            await page.screenshot({ path: `${shots()}workspace-result-${width}-${theme}.png`, fullPage: true });
          }
          const overflowTab = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflowTab, `horizontal overflow in tabs at ${width}px`).toBeLessThanOrEqual(0);
          await page.getByRole('tab', { name: 'Problem' }).click();
        }
      }
    });
  });
}

test('three columns at 1024 px, tabs below', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1024, height: 800 });
  await openWorkspace(page);
  const rail = await page.getByRole('complementary', { name: 'Problem' }).boundingBox();
  const statement = await page.getByRole('region', { name: 'Statement' }).boundingBox();
  const code = await page.getByRole('region', { name: 'Code' }).boundingBox();
  const result = await page.getByRole('region', { name: 'Result' }).boundingBox();
  expect(rail && statement && code && result).toBeTruthy();
  expect(rail!.x).toBeLessThan(statement!.x);
  expect(statement!.x + statement!.width).toBeLessThanOrEqual(code!.x + 1);
  expect(result!.y).toBeGreaterThanOrEqual(code!.y + code!.height - 1);
  expect(Math.abs(result!.x - code!.x)).toBeLessThan(2);
  await expect(page.getByRole('tablist')).toHaveCount(0);

  await page.setViewportSize({ width: 800, height: 900 });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(page.getByRole('tab')).toHaveText(['Problem', 'Code', 'Result']);
  await expect(page.getByText('You are trying this problem as a student.')).toBeVisible();
});

test('submitting fills the subtask strip as results stream in', async ({ page }) => {
  const api = await mockApi(page, { submissions: [submission(7, 'partial')] });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Result' });
  await expect(panel.locator('.headline').first()).toHaveText('3rd in queue');
  expect(api.submits).toHaveLength(1);
  expect(api.submits[0]).toMatchObject({ language: 'cpp17', typed_chars: 0, pasted_chars: 0 });
  expect(String(api.submits[0].client_key)).toMatch(/^[0-9a-f-]{36}$/);

  const id = 20;
  api.stream.push({ type: 'submission', data: submission(id, 'samples') });
  await expect(panel.locator('.headline').first()).toHaveText('Judging', { timeout: 10_000 });
  api.stream.push({ type: 'submission', data: submission(id, 'subtask1') });
  await expect(panel.locator('.strip .fill').first()).toHaveClass(/tone-ok/, { timeout: 10_000 });
  await page.screenshot({ path: `${shots()}workspace-judging-1440-light.png` });
  api.stream.push({ type: 'submission', data: submission(id, 'partial') });
  await expect(panel.locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });
  await expect(panel.getByText('Wrong answer on test 5').first()).toBeVisible();
  await expect(panel.locator('[aria-live="polite"]').filter({ hasText: 'Score 40 of 100.' })).toHaveCount(1);
  await expect(panel.locator('.strip')).toHaveAttribute('aria-label', 'Subtask 1, 40 points, passed; Subtask 2, 60 points, failed');
  await expect(panel.getByText('Check edge cases', { exact: false }).first()).toBeVisible();
  await page.screenshot({ path: `${shots()}workspace-partial-1440-light.png` });

  // The list holds both, newest first, and a row opens in place.
  const rows = panel.locator('.list > li > .row');
  await expect(rows).toHaveCount(2);
  await rows.nth(1).click();
  await expect(rows.nth(1)).toHaveAttribute('aria-expanded', 'true');
});

test('running on samples shows the first difference', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('button', { name: 'Run on samples' }).click();
  await expect(page.getByText('1st in queue')).toBeVisible();
  expect(api.runs[0]).toMatchObject({ language: 'cpp17' });
  expect(api.runs[0].input).toBeUndefined();

  api.stream.push({
    type: 'run',
    data: {
      id: 51,
      state: 'done',
      compile_output: '',
      results: [
        { label: 'Sample 1', verdict: 'AC', time_ms: 4, memory_kb: 3200, output: '3\n', expected: '3\n', message: '' },
        { label: 'Sample 2', verdict: 'WA', time_ms: 6, memory_kb: 3200, output: '12 \n', expected: '2\n', message: 'Expected 2, found 12' },
      ],
    },
  });
  const panel = page.getByRole('region', { name: 'Result' });
  await expect(panel.getByText('Passed 1 of 2 samples')).toBeVisible({ timeout: 10_000 });
  await expect(panel.getByText('First difference on line 1')).toBeVisible();
  await expect(panel.locator('.diff dd').nth(0)).toHaveText('2');
  await expect(panel.locator('.diff dd').nth(1)).toHaveText('12·');
  await expect(panel.getByText('Expected 2, found 12')).toBeVisible();
  await page.screenshot({ path: `${shots()}workspace-run-1440-light.png` });

  // Custom input runs that input.
  await page.locator('summary', { hasText: 'Custom input' }).click();
  await page.getByLabel('Custom input').fill('4 5\n');
  await page.getByRole('button', { name: 'Run on this input' }).click();
  await expect.poll(() => api.runs.length).toBe(2);
  expect(api.runs[1].input).toBe('4 5\n');
});

test('a compile error line jumps to the editor', async ({ page }) => {
  const source = 'int main() {\n    int a\n    return 0;\n}\n';
  const api = await mockApi(page, { draft: { language: 'cpp17', source } });
  await page.setViewportSize({ width: 375, height: 812 });
  await openWorkspace(page);
  await page.getByRole('tab', { name: 'Code' }).click();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByRole('tab', { name: /Result/ })).toHaveAttribute('aria-selected', 'true');
  api.stream.push({ type: 'submission', data: submission(20, 'ce') });

  const jump = page.getByRole('button', { name: 'Go to line 3, column 5 in the editor' });
  await expect(jump).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Compile error', { exact: true }).first()).toBeVisible();
  await page.screenshot({ path: `${shots()}workspace-compile-error-375-light.png`, fullPage: true });
  await jump.click();
  await expect(page.getByRole('tab', { name: 'Code' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.cm-content')).toBeFocused();
  await expect(page.locator('.cm-activeLine')).toHaveText('    return 0;');
});

test('the draft comes back after a reload, from this device when the server missed it', async ({ page }) => {
  const api = await mockApi(page, { failDrafts: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  expect(await editorText(page)).toContain('ios::sync_with_stdio(false);');

  await page.locator('.cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// kept locally');
  await page.getByRole('heading', { level: 1 }).click();
  await expect.poll(() => api.draftPuts.length).toBeGreaterThan(0);
  await expect(page.getByText('Draft not saved to the server.')).toBeVisible();

  await page.reload();
  await expect(page.locator('.cm-content')).toBeVisible();
  await expect.poll(() => editorText(page)).toContain('// kept locally');
});

test('a newer server draft wins over an older local copy', async ({ page }) => {
  await mockApi(page, { draft: { language: 'python3', source: 'print(42)\n' } });
  await page.addInitScript(() => {
    localStorage.setItem('podium-draft:/api/workspace/1', JSON.stringify({ language: 'cpp17', source: 'old', updated_at: 1 }));
  });
  await openWorkspace(page);
  await expect.poll(() => editorText(page)).toBe('print(42)\n');
  await expect(page.getByLabel('Language')).toHaveValue('python3');
});

test('a rate limit says how long to wait', async ({ page }) => {
  const api = await mockApi(page);
  api.submitMode = 'rate';
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('Submissions are limited to one every 30 seconds. Try again in 12 s.');
  await page.screenshot({ path: `${shots()}workspace-rate-limit-1440-light.png` });
});

test('an unsent submission retries with the same key', async ({ page, context }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);

  // The browser says it is offline; the mock drops the request as the
  // network would.
  await context.setOffline(true);
  api.submitMode = 'offline';
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Not sent: you are offline.');
  await page.screenshot({ path: `${shots()}workspace-offline-1440-light.png` });
  await context.setOffline(false);

  api.submitMode = 'abort-once';
  await page.getByRole('button', { name: 'Retry' }).click();
  await expect(page.getByRole('alert')).toContainText('Not sent: the server could not be reached.');
  await page.getByRole('button', { name: 'Retry' }).click();
  await expect(page.locator('.headline').first()).toHaveText('3rd in queue');
  expect(api.submits).toHaveLength(3);
  expect(new Set(api.submits.map((b) => b.client_key)).size).toBe(1);

  // A new press makes a new key.
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(4);
  expect(api.submits[3].client_key).not.toBe(api.submits[2].client_key);
});

test('keyboard shortcuts submit and run, and integrity counts go with the submit', async ({ page }) => {
  const api = await mockApi(page, { draft: { language: 'cpp17', source: 'int main() {}\n' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.locator('.cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// hi');
  await page.locator('.cm-content').evaluate((el) => {
    const data = new DataTransfer();
    data.setData('text/plain', 'pasted!');
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  });
  const before = await editorText(page);

  await page.keyboard.press("Control+'");
  await expect.poll(() => api.runs.length).toBe(1);
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  expect(await editorText(page)).toBe(before);
  expect(api.submits[0]).toMatchObject({ typed_chars: 5, pasted_chars: 7 });
  await expect.poll(() => api.integrity.flatMap((b) => b.events as Json[]).filter((e) => e.kind === 'paste')).toEqual([
    expect.objectContaining({ kind: 'paste', size: 7 }),
  ]);

  // The editor's settings: Tab moves focus when asked, and the font size sticks.
  await page.getByLabel('Tab moves focus').check();
  await page.locator('.cm-content').click();
  await page.keyboard.press('Tab');
  await expect(page.locator('.cm-content')).not.toBeFocused();
  await page.getByLabel('Font size').selectOption('18');
  await page.reload();
  await expect(page.getByLabel('Font size')).toHaveValue('18');
  await expect(page.getByLabel('Tab moves focus')).toBeChecked();
  await expect(page.locator('.cm-content')).toHaveAttribute('aria-label', 'Code for A plus B, C++17');
});

test('Escape then Tab leaves the editor while Tab indents', async ({ page }) => {
  await mockApi(page, { draft: { language: 'python3', source: 'x = 1\n' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.locator('.cm-content').click();
  await page.keyboard.press('Tab');
  await expect(page.locator('.cm-content')).toBeFocused();
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  await expect(page.locator('.cm-content')).not.toBeFocused();
});

test('a missing problem says so', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/404/try');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problem not found');
  await expect(page.locator('h1')).toHaveCount(1);
});
