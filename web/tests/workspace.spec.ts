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
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import { shots, WIDTHS, THEMES, type Json, submission, mockApi, HISTORY, editorText, openWorkspace } from './mocks/workspace';
import { codeEditor } from './locators';

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
          await expect(codeEditor(page)).toBeVisible();
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
  // From 1024 px the only tabs are the result's: Run, Submissions, Your input (B2).
  await expect(page.getByRole('tablist')).toHaveCount(1);
  await expect(page.getByRole('tablist', { name: 'Result' }).getByRole('tab')).toHaveText(['Run', 'Submissions', 'Your input']);

  await page.setViewportSize({ width: 800, height: 900 });
  await expect(page.getByRole('tablist', { name: 'Workspace' }).getByRole('tab')).toHaveCount(2);
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(page.getByRole('tablist', { name: 'Workspace' }).getByRole('tab')).toHaveText(['Problem', 'Code', 'Result']);
  await expect(page.getByText('You are trying this problem as a student.')).toBeVisible();
});

test('submitting fills the subtask strip as results stream in', async ({ page }) => {
  const api = await mockApi(page, { submissions: [submission(7, 'partial')] });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Result' });
  await expect(panel.locator('.headline').first()).toHaveText('Submitted, 3rd in queue');
  expect(api.submits).toHaveLength(1);
  expect(api.submits[0]).toMatchObject({ language: 'cpp17', typed_chars: 0, pasted_chars: 0 });
  expect(String(api.submits[0].client_key)).toMatch(/^[0-9a-f-]{36}$/);

  const id = 20;
  api.stream.push({ type: 'submission', data: submission(id, 'samples') });
  await expect(panel.locator('.headline').first()).toHaveText('Submitted, judging', { timeout: 10_000 });
  api.stream.push({ type: 'submission', data: submission(id, 'subtask1') });
  await expect(panel.locator('.strip .fill').first()).toHaveClass(/tone-ok/, { timeout: 10_000 });
  await page.screenshot({ path: `${shots()}workspace-judging-1440-light.png` });
  api.stream.push({ type: 'submission', data: submission(id, 'partial') });
  await expect(panel.locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });
  await expect(panel.getByText('Wrong answer on test 5').first()).toBeVisible();
  // X1: the app's polite announcer speaks the result as a sentence.
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Partial score. Wrong answer on test 5. Score 40 of 100.');
  await expect(panel.locator('.strip')).toHaveAttribute('aria-label', 'Subtask 1, 40 points, passed; Subtask 2, 60 points, failed');
  await expect(panel.getByText('Check edge cases', { exact: false }).first()).toBeVisible();
  await page.screenshot({ path: `${shots()}workspace-partial-1440-light.png` });

  // The list holds both, newest first, and a row opens in place.
  const rows = panel.locator('.list > li > .row');
  await expect(rows).toHaveCount(2);
  await rows.nth(1).click();
  await expect(rows.nth(1)).toHaveAttribute('aria-expanded', 'true');
});

test('the subtask fill grows by transform, and reduced motion stills it (M4)', async ({ page }) => {
  await mockApi(page, { submissions: [submission(7, 'partial')] });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  const fill = page.locator('.strip .fill').first();
  await expect(fill).toHaveClass(/tone-ok/);
  const moving = await fill.evaluate((el) => {
    const st = getComputedStyle(el);
    return { property: st.transitionProperty, transform: st.transform };
  });
  expect(moving.property).toContain('transform');
  expect(moving.property).not.toContain('width');
  expect(moving.transform).toMatch(/^matrix\(/);

  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(Number.parseFloat(await fill.evaluate((el) => getComputedStyle(el).transitionDuration))).toBeLessThan(0.001);
  await page.screenshot({ path: `${shots()}workspace-reduced-motion-1440-light.png` });
});

test('running on samples shows the first difference', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByText('1st in queue')).toBeVisible();
  expect(api.runs[0]).toMatchObject({ language: 'cpp17' });
  expect(api.runs[0].input).toBeUndefined();

  api.stream.push({
    type: 'run',
    data: {
      id: 51,
      state: 'done',
      compile_output: '',
      time_step_ms: 100,
      results: [
        { label: 'Example 1', verdict: 'AC', time_ms: 0, memory_kb: 3200, output: '3\n', expected: '3\n', message: '' },
        { label: 'Example 2', verdict: 'WA', time_ms: 6, memory_kb: 3200, output: '12 \n', expected: '2\n', message: 'Expected 2, found 12' },
      ],
    },
  });
  const panel = page.getByRole('region', { name: 'Result' });
  await expect(panel.getByText('1 of 2 examples failed')).toBeVisible({ timeout: 10_000 });
  await expect(panel.getByText('First difference on line 1')).toBeVisible();
  await expect(panel.locator('.diff dd').nth(0)).toHaveText('2');
  await expect(panel.locator('.diff dd').nth(1)).toHaveText('12·');
  await expect(panel.getByText('Expected 2, found 12')).toBeVisible();
  // A graded contest's 100 ms rounding: a 0 is under 50 ms, never "0 ms" (D322).
  await expect(panel.getByText('under 50 ms')).toHaveCount(2);
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('1 of 2 examples failed.');
  await page.screenshot({ path: `${shots()}workspace-run-1440-light.png` });

  // D114: "Run on this input" on the failing sample fills the custom box, opens it and runs it.
  const fromSample = panel.getByRole('button', { name: 'Run on this input' });
  await expect(fromSample).toHaveCount(1);
  await fromSample.click();
  await expect(page.getByRole('textbox', { name: 'Your input' })).toHaveValue('-5 7\n');
  // B2: the input goes in the Your input tab, which is selected.
  await expect(page.getByRole('tab', { name: 'Your input' })).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => api.runs.length).toBe(2);
  expect(api.runs[1].input).toBe('-5 7\n');
  api.runs.length = 1;

  // Custom input runs that input.
  await page.getByRole('textbox', { name: 'Your input' }).fill('4 5\n');
  await page.getByRole('tabpanel', { name: 'Your input' }).getByRole('button', { name: 'Run', exact: true }).click();
  await expect.poll(() => api.runs.length).toBe(2);
  expect(api.runs[1].input).toBe('4 5\n');
});

test('after a run that did not compile, Submit asks once and the empty sample rows stay out (S4)', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByText('1st in queue')).toBeVisible();
  api.stream.push({
    type: 'run',
    data: {
      id: 51,
      state: 'done',
      compile_output: "main.cpp:3:5: error: expected ';' before 'return'\n",
      results: [
        { label: 'Example 1', verdict: '', time_ms: 0, memory_kb: 0, output: '', expected: '3\n', message: '' },
        { label: 'Example 2', verdict: '', time_ms: 0, memory_kb: 0, output: '', expected: '2\n', message: '' },
      ],
    },
  });
  const panel = page.getByRole('region', { name: 'Result' });
  await expect(panel.getByText('Compile error', { exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(panel.getByRole('heading', { name: 'Example 1' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  const ask = page.getByRole('dialog', { name: 'Your last run did not compile. Submit anyway?' });
  await expect(ask).toBeVisible();
  await ask.getByRole('button', { name: 'Cancel' }).click();
  expect(api.submissions.length).toBe(0);
  // Asked once: the next press submits.
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(ask).toBeHidden();
  await expect.poll(() => api.submissions.length).toBe(1);
});

test('a compile error line jumps to the editor', async ({ page }) => {
  const source = 'int main() {\n    int a\n    return 0;\n}\n';
  const api = await mockApi(page, { draft: { language: 'cpp17', source } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  api.stream.push({ type: 'submission', data: submission(20, 'ce') });
  const jump = page.getByRole('button', { name: 'main.cpp:3:5, go to line 3, column 5 in the editor', exact: true });
  await expect(jump).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Compile error', { exact: true }).first()).toBeVisible();
  await page.screenshot({ path: `${shots()}workspace-compile-error-1440-light.png` });
  await jump.click();
  await expect(codeEditor(page)).toBeFocused();
  await expect(page.locator('.cm-activeLine')).toHaveText('    return 0;');
});

test('the draft comes back after a reload, from this device when the server missed it', async ({ page }) => {
  const api = await mockApi(page, { failDrafts: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  expect(await editorText(page)).toContain('ios::sync_with_stdio(false);');

  await codeEditor(page).click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// kept locally');
  await page.getByRole('heading', { level: 1 }).click();
  await expect.poll(() => api.draftPuts.length).toBeGreaterThan(0);
  await expect(page.locator('p.save')).toHaveText('Not saved to the server, kept on this device');

  await page.reload();
  await expect(codeEditor(page)).toBeVisible();
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
  const box = await page.locator('.editor-box').boundingBox();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  // S4: the wait counts down inside the button; no line is added and the editor stays put.
  const counting = page.getByRole('button', { name: /^Submit in \d+ s$/ });
  await expect(counting).toHaveAttribute('aria-disabled', 'true');
  await expect(counting).toBeFocused();
  await expect(page.locator('#ws-submit-status')).toHaveCount(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
  expect((await page.locator('.editor-box').boundingBox())!.y).toBe(box!.y);
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Submissions are limited to one every 30 seconds. Try again in 12 s.');
  // Pressing it again only says how long is left.
  await counting.click({ force: true });
  await expect(page.locator('[data-announcer="polite"]')).toHaveText(/^You can submit again in \d+ s\.$/);
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
  await page.getByRole('button', { name: 'Submit again' }).click();
  await expect(page.getByRole('alert')).toContainText('Not sent: the server could not be reached.');
  await page.getByRole('button', { name: 'Submit again' }).click();
  await expect(page.locator('.headline').first()).toHaveText('Submitted, 3rd in queue');
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
  await codeEditor(page).click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// hi');
  await codeEditor(page).evaluate((el) => {
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

  // The editor's settings (S2: behind "Settings"): Tab moves focus when asked, and the font size sticks.
  const settings = page.getByRole('button', { name: 'Settings', exact: true });
  await settings.click();
  await page.getByLabel('Tab moves focus').check();
  // Into the editor beside the open panel.
  await codeEditor(page).click({ position: { x: 24, y: 8 } });
  await expect(page.getByLabel('Tab moves focus')).toBeHidden();
  await page.keyboard.press('Tab');
  await expect(codeEditor(page)).not.toBeFocused();
  await settings.click();
  await page.getByLabel('Font size').selectOption('18');
  await page.keyboard.press('Escape');
  await expect(settings).toBeFocused();
  await expect(settings).toHaveAttribute('aria-expanded', 'false');
  await page.reload();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByLabel('Font size')).toHaveValue('18');
  await expect(page.getByLabel('Tab moves focus')).toBeChecked();
  await expect(codeEditor(page)).toHaveAttribute('aria-label', 'Code for A plus B, C++17');
});

test('Escape then Tab leaves the editor while Tab indents', async ({ page }) => {
  await mockApi(page, { draft: { language: 'python3', source: 'x = 1\n' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await codeEditor(page).click();
  await page.keyboard.press('Tab');
  await expect(codeEditor(page)).toBeFocused();
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  await expect(codeEditor(page)).not.toBeFocused();
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

  // Signing out removes this account's mirror as well.
  await codeEditor(page).click();
  await page.keyboard.type('// mine');
  await expect.poll(() => page.evaluate(() => Object.keys(localStorage).some((k) => k.startsWith('podium-draft:2:')))).toBe(true);
  await page.getByRole('button', { name: 'Arjun Mehta' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/signin/);
  expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-draft:')))).toEqual([]);
  // Sign out waited for the draft save first (N2, audit 01-9).
  expect(api.draftPuts.some((b) => String(b.source).includes('// mine'))).toBe(true);
  // Saves happened, and none carried the other student's code.
  expect(api.draftPuts.filter((b) => String(b.source).includes('student nine') || String(b.source).includes('old shape'))).toEqual([]);
});

test('a page whose live updates moved to another tab stops and can resume', async ({ page }) => {
  const api = await mockApi(page);
  // Fake timers, so "no reconnect" is checked over a minute of page time, not a guessed real wait.
  await page.clock.install();
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  api.stream.push({ type: 'replaced', data: {} });
  const notice = page.getByText('This page stopped live updates because Podium is open in another tab or window.');
  await expect(notice).toBeVisible();
  const after = api.connections;
  await page.clock.fastForward(60_000);
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

test('on small screens the tabs follow the arrow keys, and the code is read, not written (B4)', async ({ page }) => {
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

  // V3-14: under 1024 px the code is read-only and Run and Submit are not offered.
  await tab('Code').click();
  await expect(page.getByText('Use a computer to edit, run or submit.')).toBeVisible();
  await expect(page.getByRole('button', { name: /^(Run|Submit)$/ })).toHaveCount(0);
  await expect(codeEditor(page)).toHaveAttribute('contenteditable', 'false');
  await page.keyboard.press('Control+Enter');
  await page.waitForTimeout(300);
  expect(api.submits).toHaveLength(0);
  await page.setViewportSize({ width: 800, height: 900 });
  await expect(page.getByRole('tablist', { name: 'Workspace' }).getByRole('tab')).toHaveCount(2);
  await expect(page.getByText('Use a computer to edit, run or submit.')).toBeVisible();
});

// F9: state is a shape. In forced colours the selected tab keeps a solid
// underline, and the subtask strip and rail fills keep a visible fill.
test('selected tabs and fills survive forced colours', async ({ page }) => {
  await mockApi(page, { submissions: HISTORY });
  await page.emulateMedia({ forcedColors: 'active' });
  await page.setViewportSize({ width: 375, height: 812 });
  await openWorkspace(page);
  const tab = page.getByRole('tab', { selected: true });
  const style = await tab.evaluate((el) => {
    const s = getComputedStyle(el);
    return { width: s.borderBottomWidth, style: s.borderBottomStyle, color: s.borderBottomColor };
  });
  expect(style.style).toBe('solid');
  expect(parseFloat(style.width)).toBeGreaterThanOrEqual(2);
  expect(style.color).not.toMatch(/rgba\(0, 0, 0, 0\)|transparent/);
  const other = await page.getByRole('tab', { selected: false }).first().evaluate((el) => getComputedStyle(el).borderBottomColor);
  expect(other).not.toBe(style.color);
  await page.screenshot({ path: `${shots()}forced-colours-tabs-375.png`, fullPage: true });
  await page.getByRole('tab', { name: 'Result' }).click();
  const fills = page.locator('.fill');
  if ((await fills.count()) > 0) {
    const adjust = await fills.first().evaluate((el) => getComputedStyle(el).forcedColorAdjust);
    expect(adjust).toBe('none');
  }
  await page.screenshot({ path: `${shots()}forced-colours-result-375.png`, fullPage: true });
});

// v2 P2 inside a contest (tests/mocks/contests.ts): S4 polish, the rail's
// status words and Rules dialog (D114), the statement diff (C3) and the
// public answer banner (B1).
test.describe('in a contest', () => {
  test("a refused problem shows the server's reason, and where to go (C2-017)", async ({ page }) => {
    await contests.mockApi(page, { phase: 'during', problemRefusal: 'not_started' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/B');
    await expect(page.getByText('Accept the rules on the contest page to start.')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Go to the contest page.' })).toHaveAttribute('href', '/contests/week-6');
    await expect(page.getByText('Ask an organiser if you think you should.')).toHaveCount(0);
  });

  test('the bar shows each score, or a dash, and nothing else (S1-09, B6)', async ({ page }) => {
    const api = await contests.mockApi(page, { phase: 'during' });
    api.scores.A = 100;
    api.scores.B = 40;
    api.submissions.push(contests.submission(30, 11, 'done', 100), contests.submission(31, 12, 'done', 40));
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/B');
    const rail = page.getByRole('navigation', { name: 'Problems' });
    await expect(rail.getByRole('link', { name: /^Problem A/ })).toContainText('100/100');
    await expect(rail.getByRole('link', { name: /^Problem B/ })).toContainText('40/100');
    await expect(rail.getByRole('link', { name: /^Problem C/ })).toHaveText(/C\s*-/);
    // The sentence for screen readers says it in words.
    await expect(rail.getByRole('link', { name: 'Problem B, Interval covering, 40 of 100 points' })).toHaveAttribute('aria-current', 'page');
    await expect(rail.getByRole('link', { name: 'Problem C, Shortest detour, not tried' })).toBeVisible();
    // Questions during a contest go to Messages, not to the contact form (C2-023).
    await expect(page.getByRole('link', { name: 'Write to the organisers' })).toHaveCount(0);
    // The open problem is marked by a border, not only a tint.
    const border = await rail.locator('[aria-current="page"]').evaluate((el) => getComputedStyle(el).borderInlineStartWidth);
    expect(parseFloat(border)).toBeGreaterThanOrEqual(2);
    // A partial score fills in the partial colour.
    await expect(rail.getByRole('link', { name: /^Problem B/ }).locator('.fill')).toHaveClass(/part/);
    await page.screenshot({ path: `${shots()}contest-bar-scores-1440-light.png` });
  });

  test('Rules in the rail repeats the rules from before the start', async ({ page }) => {
    await contests.mockApi(page, { phase: 'ready' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6');
    // Before the start the rules are in Help too (B5).
    await page.getByRole('button', { name: 'Read the rules and how Run and Submit work' }).click();
    const before = await page.getByRole('dialog', { name: 'Help' }).getByRole('region', { name: 'Rules' }).getByRole('listitem').allTextContents();
    await page.keyboard.press('Escape');
    expect(before.slice(1, 4)).toEqual([
      'Run tries your code on the examples in the statement. It never counts.',
      'Submit grades your code on hidden tests, in parts called subtasks.',
      'A later, worse submission never lowers your score.',
    ]);
    await page.getByRole('button', { name: 'I agree, start the contest' }).click();
    await expect(codeEditor(page)).toBeVisible();

    // The bar's Help; the footer has one too.
    const open = page.getByRole('button', { name: 'Help', exact: true }).first();
    await open.click();
    // Rules opens Help, which lists the rules first.
    const dialog = page.getByRole('dialog', { name: 'Help' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('heading', { name: 'Help' })).toBeFocused();
    await expect(dialog.getByRole('region', { name: 'Rules' }).getByRole('listitem')).toHaveText(before);
    const results = await new AxeBuilder({ page }).include('dialog[open]').withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze();
    expect(results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => v.id)).toEqual([]);
    await page.screenshot({ path: `${shots()}contest-rules-dialog-1440-light.png` });
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(open).toBeFocused();
  });

  test('a submission confirms the press, counts what is used, and keeps focus on Submit', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    await expect(codeEditor(page)).toBeVisible();
    // S2: at 1440 by 900 the toolbar is one row, and it stays one row while Submit counts down.
    const oneRow = async () => {
      const top = async (sel: string) => (await page.locator(sel).first().boundingBox())!.y;
      expect(Math.abs((await top('#ws-language')) - (await top('button.submit')))).toBeLessThan(8);
    };
    await oneRow();
    expect((await page.locator('.editor-box').boundingBox())!.height).toBeGreaterThan(380);
    const submit = page.getByRole('button', { name: 'Submit', exact: true });
    await expect(page.locator('#ws-submit-status')).toHaveText('0 of 15 submissions used');
    await submit.focus();
    await page.keyboard.press('Enter');
    const panel = page.getByRole('region', { name: 'Result' });
    await expect(panel.locator('.headline').first()).toHaveText('Submitted, 2nd in queue');
    await expect(page.locator('[data-announcer="polite"]')).toHaveText('Submitted. Waiting for the judge.');
    // S4: the contest's 30 s gap counts down in the same button, which keeps focus.
    const counting = page.locator('button.submit');
    await expect(counting).toHaveText(/^Submit in (30|29|28) s$/);
    await oneRow();
    await expect(counting).toBeFocused();
    await expect(counting).toHaveAttribute('aria-disabled', 'true');
    // The countdown is read, so it keeps text contrast: --ink-muted, not --ink-faint (C1-028).
    const muted = await page.evaluate(() => {
      const probe = document.createElement('span');
      probe.style.color = 'var(--ink-muted)';
      document.body.append(probe);
      const c = getComputedStyle(probe).color;
      probe.remove();
      return c;
    });
    await expect(counting).toHaveCSS('color', muted);
    await expect(page.locator('#ws-submit-status')).toHaveText('1 of 15 submissions used');
    await expect(counting.locator('svg')).toHaveCount(1);
    await expect(page.getByRole('button', { name: 'Run', exact: true }).locator('svg')).toHaveCount(1);
  });

  test('the gap between submissions is per problem: B is free right after A (V3-17, walk bug B2)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    await expect(codeEditor(page)).toBeVisible();
    await page.getByRole('button', { name: 'Submit', exact: true }).click();
    await expect(page.locator('button.submit')).toHaveText(/^Submit in \d+ s$/);
    await page.getByRole('navigation', { name: 'Problems' }).getByRole('link', { name: /^Problem B/ }).click();
    await expect(page).toHaveURL(/\/problems\/B$/);
    await expect(codeEditor(page)).toBeVisible();
    const submitB = page.locator('button.submit');
    await expect(submitB).toHaveText('Submit');
    await expect(submitB).not.toHaveAttribute('aria-disabled', 'true');
  });

  test('a lower score says the problem score stays', async ({ page }) => {
    const api = await contests.mockApi(page, { phase: 'during' });
    api.scores.B = 60;
    api.submissions.push(contests.submission(31, 12, 'done', 40));
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/B');
    const panel = page.getByRole('region', { name: 'Result' });
    await expect(panel.getByText('Your score on this problem stays 60 of 100.')).toBeVisible();
  });

  test('an answer for everyone reaches every student as a banner (B1)', async ({ page }) => {
    const api = await contests.mockApi(page, { phase: 'during' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    await expect(codeEditor(page)).toBeVisible();
    api.publicClarifications.push({ id: 50, problem_label: null, question: 'Is Java allowed?', answer: 'Yes, Java 21.', public: true, created_at: Date.now() - 60_000, answered_at: Date.now() + 1000 });
    api.stream.push({ type: 'clarification', data: { contest: 'week-6', contest_id: 4, id: 50 } });
    const banner = page.locator('.banner-slot .strip');
    await expect(banner).toContainText('An organiser answered a question for everyone', { timeout: 10_000 });
    await expect(banner).toContainText('Is Java allowed?');
    await expect(banner).toContainText('Yes, Java 21.');
    await expect(page.locator('[data-announcer="polite"]')).toContainText('An organiser answered a question for everyone: Is Java allowed? Yes, Java 21.');
    await expect(page.getByRole('button', { name: /Messages/ })).toContainText('1');
    await page.screenshot({ path: `${shots()}contest-public-answer-1440-light.png` });
  });

  test('the five-minute line is a sentence, not a bold', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during', endsInMs: 4 * 60_000 });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    await expect(page.getByText('Under 5 minutes left. Submit what you have.')).toBeVisible();
    expect(await page.locator('.clock .time').evaluate((el) => getComputedStyle(el).fontWeight)).toBe('400');
  });
});

// v2 P5 (F-V7): the phone strip, the short laptop, focus moves, the
// resizable result panel, the sign-out guard, the editor hint, lazy
// language packs, reset to starter and the revealed test (D115).
test.describe('P5 workspace', () => {
  test('under 1024 px a contest problem drops the site header for its own row (B4)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/contests/week-6/problems/B');
    await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
    await expect(page.locator('header.bar')).toBeHidden();
    await page.setViewportSize({ width: 800, height: 900 });
    await expect(page.locator('header.bar')).toBeHidden();
    // From 1024 px the contest bar takes its place (B6).
    await page.setViewportSize({ width: 1024, height: 900 });
    await expect(page.locator('header.bar')).toBeHidden();
    await expect(page.getByRole('button', { name: 'Leave' })).toBeVisible();
    // Outside the workspace the header shows on a phone too.
    await page.setViewportSize({ width: 375, height: 812 });
    await page.getByRole('button', { name: /^Menu/ }).click();
    await page.getByRole('button', { name: 'Leave' }).click();
    await page.getByRole('dialog', { name: 'Leave the contest?' }).getByRole('button', { name: 'Leave' }).click();
    await expect(page).toHaveURL('/');
    await expect(page.locator('header.bar')).toBeVisible();
  });

  test('under 1024 px one row holds the problems, the clock and the menu, and the statement starts at once (B4)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    for (const width of [375, 768]) {
      await page.setViewportSize({ width, height: width < 768 ? 812 : 1024 });
      await page.goto('/contests/week-6/problems/B');
      await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
      // The proof (B4): the problem's heading is in the first screen, no scrolling.
      await expect(page.getByRole('heading', { level: 1 })).toBeInViewport();
      const top = (await page.getByRole('heading', { level: 1 }).boundingBox())!.y;
      expect(top, `title y at ${width}`).toBeLessThan(200);
    }
    await page.setViewportSize({ width: 375, height: 812 });
    const strip = page.locator('[data-sticky-bar]');
    await expect(strip.getByRole('timer')).toBeVisible();
    const chips = strip.getByRole('navigation', { name: 'Problems' }).getByRole('link');
    await expect(chips).toHaveCount(3);
    await expect(chips.nth(1)).toHaveAttribute('aria-current', 'page');
    await expect(strip.getByRole('tab')).toHaveText(['Problem', 'Code', 'Result']);
    await expect(page.getByRole('button', { name: 'Submit', exact: true })).toHaveCount(0);
    // The letter is not repeated in the title on screen.
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(/^B\.\s*Interval covering$/);
    await page.screenshot({ path: `${shots()}workspace-strip-375-light.png` });

    // The menu: Standings, Messages, Help, Leave.
    await strip.getByRole('button', { name: /^Menu/ }).click();
    await expect(strip.locator('#strip-menu > li')).toHaveText([/Standings/, /Messages/, 'Help', 'Leave']);
    await strip.getByRole('button', { name: 'Standings' }).click();
    await expect(page.getByRole('dialog', { name: 'Standings' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(strip.getByRole('button', { name: /^Menu/ })).toBeFocused();

    // The row sticks to the top, and the page leaves room for it when something takes focus (X3).
    await page.setViewportSize({ width: 375, height: 600 });
    await strip.getByRole('tab', { name: 'Code' }).click();
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    await expect.poll(async () => (await strip.boundingBox())!.y).toBeLessThanOrEqual(1);
    const padding = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).scrollPaddingTop));
    expect(padding).toBeGreaterThanOrEqual((await strip.boundingBox())!.height);

    // A chip opens its problem, as "]" does.
    await chips.nth(2).click();
    await expect(page).toHaveURL(/\/problems\/C$/);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Shortest detour');
  });

  test('Run and Submit keep focus on the button pressed (P8)', async ({ page }) => {
    const api = await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    const run = page.getByRole('button', { name: 'Run', exact: true });
    await run.click();
    await expect.poll(() => api.runs.length).toBe(1);
    await expect(run).toBeFocused();
    await page.getByRole('button', { name: 'Submit', exact: true }).click();
    await expect.poll(() => api.submits.length).toBe(1);
    await expect(page.getByRole('button', { name: /^Submit/ })).toBeFocused();
  });

  test('when the time runs out, focus goes to the title (F7.4)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during', endsInMs: 9000 });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    // Still in the window: the clock runs and the editor takes focus.
    await expect(page.getByRole('timer')).toBeVisible();
    await codeEditor(page).click();
    await expect(codeEditor(page)).toBeFocused();
    await expect(page.locator('main h1')).toBeFocused({ timeout: 20_000 });
    await expect(page.locator('#ws-locked')).toHaveText('The contest has ended. Submissions are closed.');
    await expect(page.getByText('Time is up. Your submissions are saved.').first()).toBeVisible();
  });

  test('the result panel resizes and hides from the keyboard, and keeps its size (F7.5)', async ({ page }) => {
    await mockApi(page, { submissions: HISTORY });
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    const sep = page.getByRole('separator', { name: 'Result panel size' });
    const result = page.getByRole('region', { name: 'Result' });
    await expect(sep).toHaveAttribute('aria-valuenow', '40');
    await expect(sep).toHaveAttribute('aria-controls', 'ws-result');
    const before = (await result.boundingBox())!.height;
    await sep.focus();
    await page.keyboard.press('ArrowUp');
    await expect(sep).toHaveAttribute('aria-valuenow', '45');
    expect((await result.boundingBox())!.height).toBeGreaterThan(before);
    await page.keyboard.press('End');
    await expect(sep).toHaveAttribute('aria-valuenow', '80');
    await page.keyboard.press('Home');
    await expect(sep).toHaveAttribute('aria-valuenow', '20');
    // A step below the smallest hides the result; its verdict stays in the bar.
    await page.keyboard.press('ArrowDown');
    await expect(sep).toHaveAttribute('aria-valuenow', '0');
    await expect(sep).toHaveAttribute('aria-valuetext', 'Result hidden');
    await expect(result).toBeHidden();
    await expect(page.locator('.split-summary')).toHaveText('Partial, 40 of 100');
    const show = page.getByRole('button', { name: 'Show result' });
    await expect(show).toHaveAttribute('aria-expanded', 'false');
    expect((await page.locator('.editor-box').boundingBox())!.height).toBeGreaterThan(600);
    await expect(sep).toBeFocused();
    await page.screenshot({ path: `${shots()}workspace-result-hidden-1440-light.png` });
    // Enter shows it again at the size it had.
    await page.keyboard.press('Enter');
    await expect(result).toBeVisible();
    await expect(sep).toHaveAttribute('aria-valuenow', '20');

    // A drag moves it too.
    const grip = (await sep.boundingBox())!;
    await page.mouse.move(grip.x + grip.width / 4, grip.y + grip.height / 2);
    await page.mouse.down();
    await page.mouse.move(grip.x + grip.width / 4, grip.y - 150, { steps: 5 });
    await page.mouse.up();
    const dragged = Number(await sep.getAttribute('aria-valuenow'));
    expect(dragged).toBeGreaterThan(30);

    // The button hides it, and the size is kept on this device.
    await page.getByRole('button', { name: 'Hide result' }).click();
    await expect(result).toBeHidden();
    await page.reload();
    await expect(page.getByRole('button', { name: 'Show result' })).toBeVisible();
    await page.getByRole('button', { name: 'Show result' }).click();
    await expect(page.getByRole('separator', { name: 'Result panel size' })).toHaveAttribute('aria-valuenow', String(dragged));
  });

  test('the verdict head stays at the top of the result while the tests scroll (S5)', async ({ page }) => {
    await mockApi(page, { submissions: HISTORY });
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    const pane = page.locator('#ws-result');
    await pane.evaluate((el) => (el.scrollTop = el.scrollHeight));
    const paneBox = (await pane.boundingBox())!;
    const head = page.locator('#ws-result .head.sticky');
    await expect(head).toContainText('Partial, 40 of 100');
    await expect.poll(async () => Math.abs((await head.boundingBox())!.y - paneBox.y)).toBeLessThan(2);
    // "Show the latest run" sits on the heading's row once a run exists (S5).
    await page.screenshot({ path: `${shots()}workspace-sticky-head-1440-light.png` });
  });

  test('Leave during a live contest asks first, says the code is saved, and the draft is saved (B6, F7.6)', async ({ page }) => {
    const api = await contests.mockApi(page, { phase: 'during' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
    await codeEditor(page).click({ position: { x: 24, y: 8 } });
    await page.keyboard.press('Control+End');
    await page.keyboard.type('// before leaving');
    const leave = page.getByRole('button', { name: 'Leave' });
    await leave.click();
    const ask = page.getByRole('dialog', { name: 'Leave the contest?' });
    await expect(ask).toContainText('Your code is saved. The clock keeps running while you are away.');
    await expect(ask.getByRole('button', { name: 'Cancel' })).toBeFocused();
    await page.screenshot({ path: `${shots()}workspace-leave-1440-light.png` });
    await ask.getByRole('button', { name: 'Cancel' }).click();
    await expect(page).toHaveURL(/\/problems\/A$/);
    await expect(leave).toBeFocused();

    await leave.click();
    await ask.getByRole('button', { name: 'Leave' }).click();
    await expect(page).toHaveURL('/');
    await expect.poll(() => api.draftPuts.some((b) => String(b.source).includes('// before leaving'))).toBe(true);
  });

  test('outside a contest sign out does not ask', async ({ page }) => {
    await mockApi(page);
    await openWorkspace(page);
    await page.getByRole('button', { name: 'Arjun Mehta' }).click();
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL(/\/signin/);
  });

  test('the editor names its keyboard hint, and each language loads when chosen (X2, F7.8)', async ({ page }) => {
    const chunks: string[] = [];
    page.on('request', (r) => {
      const m = /lang-(cpp|javascript|java|python)/.exec(r.url());
      if (m) chunks.push(m[1]);
    });
    await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    const content = codeEditor(page);
    const hint = await content.getAttribute('aria-describedby');
    expect(hint).toMatch(/^editor-hint-/);
    await expect(page.locator(`#${hint}`)).toHaveText(/Tab indents\. Press Escape, then Tab, to leave the editor\.\s+Ctrl\+Enter submits and Ctrl\+' runs\. Press \? outside the editor for every shortcut\./);
    await expect(content).toHaveAttribute('data-language', 'cpp');
    expect([...new Set(chunks)]).toEqual(['cpp']);

    const limits = page.locator('.limits');
    await expect(limits).toHaveText('Time limit 1 s, memory 256 MB');
    await page.getByLabel('Language').selectOption('python3');
    await expect(content).toHaveAttribute('data-language', 'python');
    // The limit the selected language really gets, with the reason (D323).
    await expect(limits).toHaveText("Time limit 3 s in Python 3 (3 times the problem's 1 s, as it runs slower than C++), memory 256 MB");
    await page.getByLabel('Language').selectOption('node22');
    await expect(content).toHaveAttribute('data-language', 'javascript');
    expect([...new Set(chunks)].sort()).toEqual(['cpp', 'javascript', 'python']);
  });

  test('reset to starter asks, saves at once, and survives the next save and a reload (F7.10, F7.12)', async ({ page }) => {
    // A controllable clock, flowing as usual, so the 10 s save tick is skipped rather than waited for (C1-024).
    await page.clock.install();
    const api = await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    const starterText = await editorText(page);
    await codeEditor(page).click({ position: { x: 24, y: 8 } });
    await page.keyboard.press('Control+End');
    await page.keyboard.type('// mine');
    await page.getByRole('heading', { level: 1 }).click();
    await expect.poll(() => api.draftPuts.some((b) => String(b.source).includes('// mine'))).toBe(true);

    // Reset sits beside the language (audit N8), not inside Settings.
    const reset = page.getByRole('button', { name: 'Reset', exact: true });
    await reset.click();
    const ask = page.getByRole('dialog', { name: 'Replace your code with the starter?' });
    await expect(ask).toContainText('Your C++17 code for this problem is replaced with the starter, and saved.');
    await expect(ask.getByRole('button', { name: 'Cancel' })).toBeFocused();
    await page.screenshot({ path: `${shots()}workspace-reset-confirm-1440-light.png` });
    await ask.getByRole('button', { name: 'Cancel' }).click();
    await expect(reset).toBeFocused();
    expect(await editorText(page)).toContain('// mine');

    await reset.click();
    await ask.getByRole('button', { name: 'Reset', exact: true }).click();
    await expect.poll(() => editorText(page)).toBe(starterText);
    await expect.poll(() => String(api.draftPuts.at(-1)?.source)).not.toContain('// mine');
    await expect(codeEditor(page)).toBeFocused();
    await expect(page.locator('[data-announcer="polite"]')).toHaveText('Reset. The starter is back.');

    // Past one 10 s save tick, then a reload: the starter is still there.
    const puts = api.draftPuts.length;
    await page.clock.fastForward(10_500);
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(api.draftPuts.length).toBeGreaterThanOrEqual(puts);
    expect(api.draftPuts.at(-1)?.source).not.toContain('// mine');
    await page.reload();
    await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
    await expect.poll(() => editorText(page)).toBe(starterText);
  });

  // The server decides what is revealed (TestP2RejudgeNeverWidensTheReveal and the D115 tests in
  // server/internal/httpapi); this test only checks what the web app draws from each answer (C1-023).
  test('a revealed hidden test is drawn when the server sends one, and nothing when it does not (F7.13, D115)', async ({ page }) => {
    const api = await contests.mockApi(page, { phase: 'practice' });
    api.submissions.push({
      ...contests.submission(50, 11, 'done', 40, Date.now() - 60_000),
      revealed: { idx: 3, subtask_idx: 2, input: '4 4\n', expected: '8\n', output: '7\n', truncated: false },
    });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/contests/week-6/problems/A');
    const panel = page.getByRole('region', { name: 'Result' });
    const block = panel.locator('.revealed');
    await expect(block.getByRole('heading', { name: 'Test 3, in subtask 2' })).toBeVisible();
    await expect(block).toContainText('This test is shown because this is practice.');
    await expect(block.locator('pre').nth(0)).toHaveText('4 4');
    await expect(block).toContainText('First difference on line 1');
    await expect(block.locator('.diff dd').nth(0)).toHaveText('8');
    await expect(block.locator('.diff dd').nth(1)).toHaveText('7');
    await page.screenshot({ path: `${shots()}workspace-revealed-practice-1440-light.png` });
    await block.getByRole('button', { name: 'Run on this input' }).click();
    await expect(page.getByRole('textbox', { name: 'Your input' })).toHaveValue('4 4\n');
    await expect.poll(() => api.runs.length).toBe(1);
    expect(api.runs[0].input).toBe('4 4\n');

    // During the window the server sends no revealed test, and nothing shows: a rendering check only.
    await page.goto('about:blank');
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    const live = await contests.mockApi(page, { phase: 'during' });
    live.submissions.push(contests.submission(51, 11, 'done', 40));
    await page.goto('/contests/week-6/problems/A');
    await expect(page.getByRole('region', { name: 'Result' }).locator('.headline').first()).toHaveText('Partial, 40 of 100');
    await expect(page.getByText(/This test is shown because/)).toHaveCount(0);
  });

  test('a run shows its first failing sample, and an input over 4 KB cannot be rerun (F7.13, D115)', async ({ page }) => {
    const api = await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
    await page.getByRole('button', { name: 'Run', exact: true }).click();
    await expect(page.getByText('1st in queue')).toBeVisible();
    const row = (kind: string, label: string, verdict: string, output = '', expected: string | null = null) => ({
      kind, label, verdict, time_ms: verdict === 'SK' ? 0 : 10, memory_kb: verdict === 'SK' ? 0 : 3100, output, output_truncated: false, expected, message: '',
    });
    api.stream.push({
      type: 'run',
      data: {
        id: 51,
        state: 'done',
        compile_output: '',
        results: [
          row('sample', 'Example 1', 'AC', '3\n', '3\n'),
          row('sample', 'Example 2', 'AC', '2\n', '2\n'),
          row('sample', 'Example 3', 'WA', '7\n', '8\n'),
        ],
        revealed: { idx: 3, kind: 'sample', label: 'Example 3', input: '4 4\n', expected: '8\n', output: '7\n', stderr: 'debug: a=4 b=4\n', truncated: false },
      },
    });
    const panel = page.getByRole('region', { name: 'Result' });
    await expect(panel.getByText('1 of 3 examples failed')).toBeVisible({ timeout: 10_000 });
    const block = panel.locator('.revealed');
    await expect(block).toContainText('This test is shown because this was a run.');
    await expect(block.locator('pre').nth(0)).toHaveText('4 4');
    // B1: the end of what the program printed to stderr.
    await expect(block.getByText('Error output (stderr), the last lines')).toBeVisible();
    await expect(block).toContainText('debug: a=4 b=4');
    await expect(page.locator('[data-announcer="polite"]')).toHaveText('1 of 3 examples failed.');
    await page.screenshot({ path: `${shots()}workspace-revealed-run-1440-light.png` });
    await block.getByRole('button', { name: 'Run on this input' }).click();
    await expect.poll(() => api.runs.length).toBe(2);
    expect(api.runs[1].input).toBe('4 4\n');

    // Cut at 4 KB: shown, said so, and not offered as a run.
    api.stream.push({
      type: 'run',
      data: {
        id: 52,
        state: 'done',
        compile_output: '',
        results: [row('sample', 'Example 1', 'AC', '3\n', '3\n'), row('sample', 'Example 2', 'TLE', '', null)],
        revealed: { idx: 2, kind: 'sample', label: 'Example 2', input: '100000\n1 2 3', expected: '5000050000\n', output: '', truncated: true },
      },
    });
    // The run on the examples shows under Run (B2).
    await panel.getByRole('tab', { name: 'Run' }).click();
    await expect(panel.getByText('This test is longer than 4 KB', { exact: false })).toBeVisible({ timeout: 10_000 });
    await expect(panel.locator('.revealed').getByRole('button', { name: 'Run on this input' })).toHaveCount(0);
  });

  test('from 1024 px the bar holds the letters, and Hide statement gives the editor the width (B6, E5)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    for (const width of [1024, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/contests/week-6/problems/A');
      await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
      await expect(page.locator('header.bar')).toBeHidden();
      await expect(page.getByRole('complementary', { name: 'Problem' })).toHaveCount(0);
      for (const name of ['Leave', /Messages/, 'Standings']) await expect(page.getByRole('button', { name })).toBeVisible();
      await expect(page.getByRole('navigation', { name: 'Problems' }).getByRole('link')).toHaveCount(3);
      await expect(page.getByRole('timer')).toBeVisible();
    }
    // Keyboard: Tab reaches every letter, and each one opens its problem.
    const letters = page.getByRole('navigation', { name: 'Problems' }).getByRole('link');
    await page.getByRole('button', { name: 'Leave' }).focus();
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('Tab');
      await expect(letters.nth(i)).toBeFocused();
    }
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/\/problems\/C$/);

    // Hide statement: the code takes the width; Show statement brings it back.
    const code = page.getByRole('region', { name: 'Code' });
    const narrow = (await code.boundingBox())!.width;
    await page.getByRole('button', { name: 'Hide statement' }).click();
    await expect(page.getByRole('region', { name: 'Statement' })).toBeHidden();
    expect((await code.boundingBox())!.width).toBeGreaterThan(narrow + 200);
    await page.getByRole('button', { name: 'Show statement' }).click();
    await expect(page.getByRole('region', { name: 'Statement' })).toBeVisible();
    // The column splitter moves with the keyboard.
    const split = page.getByRole('separator', { name: 'Statement panel size' });
    await split.focus();
    await page.keyboard.press('ArrowRight');
    await expect(split).toHaveAttribute('aria-valuenow', '50');
  });

  // From 1100 px the toolbar is one row at any height; 1024 to 1099 px may wrap to two (decision of fixer 1, C1-042).
  test('from 1100 px the toolbar is one row at any height (audit S2, C1-042)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    for (const size of [
      { width: 1100, height: 900 },
      { width: 1180, height: 900 },
      { width: 1279, height: 1000 },
    ]) {
      await page.setViewportSize(size);
      await page.goto('/contests/week-6/problems/A');
      await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
      const top = async (sel: string) => (await page.locator(sel).first().boundingBox())!.y;
      expect(Math.abs((await top('#ws-language')) - (await top('button.submit'))), `one row at ${size.width}`).toBeLessThan(8);
    }
  });

  test('a short laptop keeps the toolbar on one row and the page still (F7.3)', async ({ page }) => {
    await contests.mockApi(page, { phase: 'during' });
    for (const size of [
      { width: 1093, height: 500 },
      { width: 1280, height: 720 },
    ]) {
      await page.setViewportSize(size);
      await page.goto('/contests/week-6/problems/A');
      await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
      const top = async (sel: string) => (await page.locator(sel).first().boundingBox())!.y;
      expect(Math.abs((await top('#ws-language')) - (await top('button.submit'))), `one row at ${size.width}`).toBeLessThan(8);
      const scroll = await page.evaluate(() => document.documentElement.scrollHeight - document.documentElement.clientHeight);
      // The workspace fills the window under the header; only the footer lies below it.
      const ws = (await page.locator('.ws').boundingBox())!;
      expect(ws.y + ws.height, `workspace inside the window at ${size.width}`).toBeLessThanOrEqual(size.height);
      const foot = await page.locator('footer').boundingBox();
      expect(scroll, `only the footer scrolls at ${size.width}`).toBeLessThanOrEqual(Math.ceil(foot?.height ?? 0) + 1);
      // A new viewer on a short laptop starts with the smaller result.
      await expect(page.getByRole('separator', { name: 'Result panel size' })).toHaveAttribute('aria-valuenow', '30');
    }
  });
});

test('the event stream carries this browser\'s device id (C2-034)', async ({ page }) => {
  await mockApi(page);
  const streams: string[] = [];
  page.on('request', (r) => {
    if (new URL(r.url()).pathname === '/api/events') streams.push(r.url());
  });
  await openWorkspace(page);
  await expect.poll(() => streams.length).toBeGreaterThan(0);
  const id = await page.evaluate(() => localStorage.getItem('podium-device'));
  expect(id).toMatch(/^[A-Za-z0-9]{32}$/);
  expect(new URL(streams[0]).searchParams.get('device')).toBe(id);
});
