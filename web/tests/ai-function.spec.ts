// The AI wizard for function problems (v2.1.0 stage F7): the "Students write"
// choice in both modes, the request each choice sends (a whole program sends
// exactly what it always did), the extra progress step "Checking each
// language", the Languages block of the result with its reasons, the banner
// for a draft whose own solution failed, and polling of GET .../proof that
// stops when the round ends, when the page is left and on an error. Every
// /api/ call is answered by tests/mocks/ai.ts (its `proof` option walks a
// round). Screenshots at 375, 768, 1024 and 1440 px in light and dark go to
// $PODIUM_SHOTS (default cache/v2.1.0/shots/F/).
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { mockApi, THEMES, WIDTHS, type MockOptions } from './mocks/ai';

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.1.0/shots/F/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const OPTS: MockOptions = { key: 'active', events: 'down' };

async function chooseFunction(page: Page) {
  await page.getByLabel('One function, between locked code').check();
}

async function runBrief(page: Page, opts: MockOptions) {
  const state = await mockApi(page, { ...OPTS, ...opts });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await chooseFunction(page);
  await page.getByRole('button', { name: 'Generate draft' }).click();
  return state;
}

const progress = (page: Page) => page.getByRole('region', { name: 'Progress' });
const languages = (page: Page) => page.getByRole('region', { name: 'Languages' });
const proofRequests = (urls: string[]) => urls.filter((u) => u.includes('/api/admin/problems/20/proof')).length;

test('a whole program is the default and sends the request it always sent', async ({ page }) => {
  const state = await mockApi(page, { ...OPTS, chain: true });
  await page.goto('/admin/ai/new');
  const write = page.getByRole('group', { name: 'Students write' });
  await expect(write.getByLabel('A whole program')).toBeChecked();
  await expect(page.getByRole('group', { name: 'In Java, students may use' })).toHaveCount(0);
  await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible({ timeout: 10_000 });
  expect(state.wizardBodies).toEqual([{ mode: 'brief', brief: { topic: 'Maximum subarray sum', difficulty: 'medium', constraints: '', subtasks: '' } }]);
  await expect(progress(page).getByRole('listitem')).toHaveCount(4);
  expect(proofRequests(state.requestedUrls)).toBe(0);
});

test('a whole program from pasted text sends only the mode and the text, even after the Java choice was touched', async ({ page }) => {
  const state = await mockApi(page, OPTS);
  await page.goto('/admin/ai/new');
  await chooseFunction(page);
  await page.getByLabel('Either, the student picks').check();
  await page.getByLabel('A whole program').check();
  await page.getByLabel('From pasted text').check();
  await page.getByLabel('Problem text').fill('Two sum. Given an array, print two numbers.');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(progress(page)).toBeVisible();
  expect(state.wizardBodies).toEqual([{ mode: 'paste', text: 'Two sum. Given an array, print two numbers.' }]);
});

test('one function sends both choices, from a brief and from pasted text', async ({ page }) => {
  const state = await mockApi(page, OPTS);
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await chooseFunction(page);
  const lists = page.getByRole('group', { name: 'In Java, students may use' });
  await expect(lists.getByLabel('int[] only')).toBeChecked();
  await expect(page.getByText('C++, Python and JavaScript use their usual list whatever you choose.')).toBeVisible();
  await lists.getByLabel('ArrayList only').check();
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(progress(page)).toBeVisible();
  expect(state.wizardBodies[0]).toEqual({
    mode: 'brief',
    brief: { topic: 'Sum of an array', difficulty: 'medium', constraints: '', subtasks: '' },
    students_write: 'function',
    java_lists: 'arraylist',
  });

  const again = await mockApi(page, OPTS);
  await page.goto('/admin/ai/new');
  await page.getByLabel('From pasted text').check();
  await chooseFunction(page);
  await page.getByLabel('Problem text').fill('Array sum, rewrite it.');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(progress(page)).toBeVisible();
  expect(again.wizardBodies[0]).toEqual({ mode: 'paste', text: 'Array sum, rewrite it.', students_write: 'function', java_lists: 'array' });
});

test('the Java question offers three answers and Either sends either', async ({ page }) => {
  const state = await mockApi(page, OPTS);
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await chooseFunction(page);
  const lists = page.getByRole('group', { name: 'In Java, students may use' });
  await expect(lists.getByRole('radio')).toHaveCount(3);
  await expect(lists.getByLabel('int[] only')).toBeChecked();
  // From the keyboard: arrow down twice from the first radio.
  await lists.getByLabel('int[] only').focus();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  await expect(lists.getByLabel('Either, the student picks')).toBeChecked();
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(progress(page)).toBeVisible();
  expect(state.wizardBodies[0]).toEqual({
    mode: 'brief',
    brief: { topic: 'Sum of an array', difficulty: 'medium', constraints: '', subtasks: '' },
    students_write: 'function',
    java_lists: 'either',
  });
});

test('the reason names the Java form that failed', async ({ page }) => {
  await runBrief(page, { proof: 'java_form_fail' });
  const block = languages(page);
  await expect(block.getByText("Not offered: Java. The AI's Java code for ArrayList<Integer> failed 3 of 12 tests. Open the draft to fix it.")).toBeVisible({ timeout: 15_000 });
});

test('the submit button says Writing the draft while it sends', async ({ page }) => {
  await mockApi(page, OPTS);
  await page.route('**/api/admin/ai/wizard', async (route) => {
    await new Promise((r) => setTimeout(r, 600));
    await route.fallback();
  });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(page.getByRole('button', { name: 'Writing the draft' })).toBeDisabled();
});

test('the choices are reachable and operable from the keyboard', async ({ page }) => {
  await mockApi(page, OPTS);
  await page.goto('/admin/ai/new');
  await page.getByLabel('A whole program').focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByLabel('One function, between locked code')).toBeChecked();
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('int[] only')).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByLabel('ArrayList only')).toBeChecked();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByLabel('Either, the student picks')).toBeChecked();
});

test('a function run shows the language step with its count, then the Languages block with Java not offered', async ({ page }) => {
  await runBrief(page, { proof: 'java_fail' });
  await expect(progress(page).getByRole('listitem')).toHaveText([
    /Writing the problem/,
    /Draft created/,
    /Generating the tests/,
    /Checking each language/,
    /Validating the draft/,
  ]);
  await expect(progress(page).getByRole('status')).toHaveText('Checking Java, 2 of 4.', { timeout: 10_000 });
  const block = languages(page);
  await expect(block).toBeVisible({ timeout: 15_000 });
  await expect(block.getByText('Offered: C++, Python, JavaScript.')).toBeVisible();
  const line = block.getByText("Not offered: Java. The AI's Java code failed 3 of 12 tests. Open the draft to fix it.");
  await expect(line).toBeVisible();
  // A cross and words, never colour alone.
  await expect(block.locator('.not-offered svg')).toHaveCount(1);
  await expect(page.getByRole('link', { name: 'Open the draft' })).toBeVisible();
  await expect(progress(page).getByRole('listitem').nth(3)).toContainText(', done');
});

test('a Java compile error keeps the compiler lines behind Details', async ({ page }) => {
  await runBrief(page, { proof: 'java_compile' });
  const block = languages(page);
  await expect(block.getByText("Not offered: Java. The AI's Java code did not compile. Open the draft to fix it.")).toBeVisible({ timeout: 15_000 });
  const details = block.locator('details');
  await expect(details).not.toHaveAttribute('open', '');
  await details.locator('summary', { hasText: 'Details' }).click();
  await expect(details).toContainText('cannot find symbol');
});

test('every language offered lists them all and no Not offered line', async ({ page }) => {
  await runBrief(page, { proof: 'pass' });
  const block = languages(page);
  await expect(block.getByText('Offered: C++, Java, Python, JavaScript.')).toBeVisible({ timeout: 15_000 });
  await expect(block.getByText(/Not offered/)).toHaveCount(0);
  await expect(page.getByText('Validation passed', { exact: false }).first()).toBeVisible({ timeout: 10_000 });
});

test('when the AI own solution fails the banner says so and the draft is not validated', async ({ page }) => {
  const state = await runBrief(page, { proof: 'reference_fail' });
  const banner = page.getByRole('alert').filter({ hasText: "The AI's own solution failed its tests, so this draft cannot be validated." });
  await expect(banner).toBeVisible({ timeout: 15_000 });
  await expect(banner).toContainText('Open it to look, or ask again with a simpler description.');
  await expect(languages(page)).toHaveCount(0);
  await expect(progress(page).getByRole('listitem').nth(3)).toContainText(', failed');
  await expect(progress(page).getByRole('listitem').nth(4)).toContainText(', not started');
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible();
  const n = proofRequests(state.requestedUrls);
  await page.waitForTimeout(4500);
  expect(proofRequests(state.requestedUrls)).toBe(n);
});

test('polling stops when the round ends', async ({ page }) => {
  const state = await runBrief(page, { proof: 'pass' });
  await expect(languages(page)).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('Validation passed', { exact: false }).first()).toBeVisible({ timeout: 10_000 });
  const n = proofRequests(state.requestedUrls);
  expect(n).toBe(3);
  await page.waitForTimeout(4500);
  expect(proofRequests(state.requestedUrls)).toBe(n);
});

test('polling stops when the page is left', async ({ page }) => {
  const state = await runBrief(page, { proof: 'forever' });
  await expect(progress(page).getByRole('status')).toHaveText('Checking Java, 2 of 4.', { timeout: 10_000 });
  await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Problems' }).click();
  await expect(page).toHaveURL(/\/admin\/problems$/);
  const n = proofRequests(state.requestedUrls);
  await page.waitForTimeout(4500);
  expect(proofRequests(state.requestedUrls)).toBe(n);
});

test('polling stops on an error and says the checks could not be read', async ({ page }) => {
  const state = await runBrief(page, { proof: 'error' });
  const note = page.getByRole('alert').filter({ hasText: 'Could not read the language checks.' });
  await expect(note).toBeVisible({ timeout: 10_000 });
  await expect(note).toContainText('Open the draft to see them.');
  const n = proofRequests(state.requestedUrls);
  expect(n).toBe(1);
  await page.waitForTimeout(4500);
  expect(proofRequests(state.requestedUrls)).toBe(n);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('the form with one function chosen fits, passes axe and is shot at four widths', async ({ page }) => {
      await mockApi(page, OPTS);
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/ai/new');
      await chooseFunction(page);
      await expect(page.getByRole('group', { name: 'In Java, students may use' })).toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, `overflow at ${width}px`).toBeLessThanOrEqual(0);
        await page.screenshot({ path: `${shotDir()}wizard-function-form-${width}-${theme}.png`, fullPage: true });
      }
      const results = await new AxeBuilder({ page }).include('.wizard-form').withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => v.id)).toEqual([]);
    });

    test('the result with Java not offered fits, passes axe and is shot at four widths', async ({ page }) => {
      await page.setViewportSize({ width: 1440, height: 900 });
      await runBrief(page, { proof: 'java_compile' });
      await expect(languages(page).getByText(/Not offered: Java/)).toBeVisible({ timeout: 15_000 });
      await languages(page).locator('summary', { hasText: 'Details' }).click();
      await page.evaluate(() => document.fonts.ready);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, `overflow at ${width}px`).toBeLessThanOrEqual(0);
        await page.screenshot({ path: `${shotDir()}wizard-function-result-${width}-${theme}.png`, fullPage: true });
      }
      const results = await new AxeBuilder({ page }).include('.progress').withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => v.id)).toEqual([]);
    });
  });
}

const STOP_COPY: Record<string, string> = {
  changed: 'The draft was changed while its languages were being checked, so the result was not used. Open the draft and press Check for each language.',
  gone: 'The draft was deleted while its languages were being checked.',
  apply_error: 'Java 21 could not be removed. Open the draft to see its languages.',
};

for (const kind of ['changed', 'gone', 'apply_error'] as const) {
  test(`a round that ends ${kind} stops the wizard with a banner and no language lines`, async ({ page }) => {
    const state = await runBrief(page, { proof: kind });
    const banner = page.getByRole('alert').filter({ hasText: STOP_COPY[kind] });
    await expect(banner).toBeVisible({ timeout: 15_000 });
    await expect(languages(page)).toHaveCount(0);
    await expect(page.getByText(/Offered:/)).toHaveCount(0);
    await expect(progress(page).getByRole('listitem').nth(3)).toContainText(', failed');
    await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible();
    const n = proofRequests(state.requestedUrls);
    await page.waitForTimeout(4500);
    expect(proofRequests(state.requestedUrls)).toBe(n);
  });
}

test('after ten minutes with nothing finished the page says it is slow instead of stopping silently', async ({ page }) => {
  await page.clock.install();
  const state = await runBrief(page, { proof: 'forever' });
  await expect(progress(page).getByRole('status')).toHaveText('Checking Java, 2 of 4.', { timeout: 15_000 });
  await page.clock.fastForward('11:00');
  await expect(page.getByText('This is taking longer than usual. Open the draft to see where it is.')).toBeVisible({ timeout: 15_000 });
  const n = proofRequests(state.requestedUrls);
  await page.clock.fastForward('00:30');
  await page.waitForTimeout(500);
  expect(proofRequests(state.requestedUrls)).toBe(n);
});

test('a proof answer that arrives after Start another is not written into the new run', async ({ page }) => {
  const state = await mockApi(page, { ...OPTS, proof: 'forever' });
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => (release = r));
  let held = false;
  await page.route('**/api/admin/problems/20/proof', async (route) => {
    if (held) return route.fallback();
    held = true;
    await gate;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ checking: false, languages: ['cpp17', 'java21'].map((language) => ({ language, offered: true, result: 'passed', reason: '', failed: 0, total: 4, compiler: '', tests: [], current: true, source: 'wizard' })), applied: { validating: true } }),
    });
  });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await chooseFunction(page);
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(page.getByRole('button', { name: 'Start another' })).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: 'Start another' }).click();
  await page.getByLabel('Topic', { exact: true }).fill('Another');
  await chooseFunction(page);
  release();
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(page.getByRole('button', { name: 'Start another' })).toBeVisible({ timeout: 15_000 });
  await expect(progress(page).getByRole('status')).toHaveText('Checking Java, 2 of 4.', { timeout: 15_000 });
  await expect(languages(page)).toHaveCount(0);
  expect(state.wizardBodies).toHaveLength(2);
});
