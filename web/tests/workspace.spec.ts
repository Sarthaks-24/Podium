// Screen sweep and flows for the M3 workspace at /admin/problems/{id}/try.
// Every /api/ call is answered by an in-test mock of the routes in
// cache/v1/m3-plan.md sections 5 to 7a, one per page. GET /api/events is
// mocked as a response that sends what the test queued and then ends; the
// page's EventSource is replaced (tests/sse.ts) by one that stays open and
// reads that route again every 250 ms, so each queued event arrives in
// turn: that is how the subtask strip is seen to fill step by step.
//
// The sweep saves screenshots to cache/screenshots-m3/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1 and
// no horizontal overflow. The flows cover submitting and the strip filling,
// running on samples with the diff, compile error line jumps, the draft
// coming back after a reload, the 429 message, offline retry with the same
// idempotency key, keyboard shortcuts and integrity counts.
import { expect, test } from '@playwright/test';
import { shots, WIDTHS, THEMES, type Json, submission, mockApi, HISTORY, editorText, openWorkspace } from './mocks/workspace';

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
  await expect(page.locator('.ws > [aria-live="polite"]').filter({ hasText: 'Score 40 of 100.' })).toHaveCount(1);
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
  await expect(page.locator('p.save')).toContainText('Draft not saved to the server.');

  await page.reload();
  await expect(page.locator('.cm-content')).toBeVisible();
  await expect.poll(() => editorText(page)).toContain('// kept locally');
});

test('a newer server draft wins over an older local copy', async ({ page }) => {
  await mockApi(page, { draft: { language: 'python3', source: 'print(42)\n' } });
  await page.addInitScript(() => {
    localStorage.setItem('podium-last-user', '2');
    localStorage.setItem('podium-draft:2:/api/workspace/1', JSON.stringify({ language: 'cpp17', source: 'old', updated_at: 1, owner: 2 }));
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

test("a draft another account left on this device is never shown or saved", async ({ page }) => {
  const api = await mockApi(page);
  await page.addInitScript(() => {
    if (sessionStorage.getItem('seeded')) return;
    sessionStorage.setItem('seeded', '1');
    // The last student on this lab machine, user 9, left code behind.
    localStorage.setItem('podium-last-user', '9');
    localStorage.setItem('podium-draft:9:/api/workspace/1', JSON.stringify({ language: 'python3', source: 'print("student nine")', updated_at: 9e15, owner: 9 }));
    localStorage.setItem('podium-draft:/api/workspace/1', JSON.stringify({ language: 'python3', source: 'print("old shape")', updated_at: 9e15 }));
  });
  await openWorkspace(page);
  expect(await editorText(page)).toContain('ios::sync_with_stdio(false);');
  expect(await editorText(page)).not.toContain('student nine');
  const left = await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-draft:')));
  expect(left.every((k) => k.startsWith('podium-draft:2:'))).toBe(true);
  await page.getByRole('heading', { level: 1 }).click();
  await page.waitForTimeout(500);
  expect(api.draftPuts.filter((b) => String(b.source).includes('student nine') || String(b.source).includes('old shape'))).toEqual([]);

  // Signing out removes this account's mirror as well.
  await page.locator('.cm-content').click();
  await page.keyboard.type('// mine');
  await page.waitForTimeout(600);
  expect(await page.evaluate(() => Object.keys(localStorage).some((k) => k.startsWith('podium-draft:2:')))).toBe(true);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/signin/);
  expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-draft:')))).toEqual([]);
});

test('a page whose live updates moved to another tab stops and can resume', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  api.stream.push({ type: 'replaced', data: {} });
  const notice = page.getByText('This page stopped live updates because Podium is open in another tab or window.');
  await expect(notice).toBeVisible();
  const after = api.connections;
  await page.waitForTimeout(1500);
  expect(api.connections, 'no reconnect after replaced').toBe(after);
  await page.screenshot({ path: `${shots()}workspace-replaced-1440-light.png` });

  await page.getByRole('button', { name: 'Resume here' }).click();
  await expect(notice).toBeHidden();
  await expect.poll(() => api.connections).toBeGreaterThan(after);
});

test('a missing problem says so', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/404/try');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problem not found');
  await expect(page.locator('h1')).toHaveCount(1);
});

test('on small screens the tabs follow the arrow keys, and Submit opens the result', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await openWorkspace(page);
  const tab = (name: string) => page.getByRole('tab', { name });

  // Arrows move and choose, Home and End jump, and the ends wrap.
  await tab('Problem').focus();
  await page.keyboard.press('ArrowRight');
  await expect(tab('Code')).toHaveAttribute('aria-selected', 'true');
  await expect(tab('Code')).toBeFocused();
  await page.keyboard.press('End');
  await expect(tab('Result')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowRight');
  await expect(tab('Problem')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowLeft');
  await expect(tab('Result')).toBeFocused();
  await page.keyboard.press('Home');
  await expect(tab('Problem')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tabpanel', { name: 'Problem' })).toBeVisible();

  // Submitting from the Code tab opens the Result tab.
  await tab('Code').click();
  await page.locator('.cm-content').click();
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  await expect(tab('Result')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tabpanel', { name: 'Result' })).toBeVisible();

  // The result is still announced while another tab is showing.
  await tab('Problem').click();
  const live = page.locator('.ws > [aria-live="polite"]').first();
  await expect(live).not.toHaveText('');
  expect(await live.evaluate((el) => el.closest('[hidden]') === null)).toBe(true);

  // On a tablet, a submit from the Problem tab opens the Code tab, where the result sits.
  await page.setViewportSize({ width: 800, height: 900 });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await tab('Problem').click();
  await page.locator('main h1').focus();
  await page.waitForTimeout(1100);
  await page.keyboard.press('Control+Enter');
  await expect(tab('Code')).toHaveAttribute('aria-selected', 'true');
});
