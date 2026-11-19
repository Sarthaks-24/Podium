// Save and import of an AI answer (v2.1.1 stage D): Save as file on the review
// screen for admins and the owner, Import AI answer beside Import pack, the
// kind question for a bare pasted answer, the review screen opening at
// /admin/ai/new?import=1 and the import prompt returning after a reload.
// Every /api/ call is answered by tests/mocks/ai.ts. The server side (wrapper
// checks, sizes, roles, a second database) is tested in Go.
import AxeBuilder from '@axe-core/playwright';
import { readFileSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate } from './keyboard';
import { mockApi, REVIEW_ANSWER, THEMES, WIDTHS, type MockOptions } from './mocks/ai';
import { NodeBuffer } from './mocks/problems';

const OPTS: MockOptions = { key: 'active', events: 'down', role: 'admin' };
const ANSWER = JSON.stringify(REVIEW_ANSWER);
const reviewing = (page: Page) => page.getByRole('region', { name: "Review the AI's answer" });
const importPanel = (page: Page) => page.getByRole('form', { name: 'Import an AI answer' });

async function savedFile(page: Page): Promise<Record<string, unknown>> {
  const saved = page.waitForEvent('download');
  await reviewing(page).getByRole('button', { name: 'Save as file' }).click();
  const file = await saved;
  expect(file.suggestedFilename()).toBe('podium-ai-answer.json');
  return JSON.parse(readFileSync(await file.path(), 'utf8')) as Record<string, unknown>;
}

async function importBare(page: Page, text: string, kind: 'A whole program' | 'One function') {
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import AI answer' }).click();
  await importPanel(page).getByLabel('Answer file or pasted text').fill(text);
  await importPanel(page).getByLabel(kind).check();
  await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
}

test('Import AI answer is for admins and the owner, beside Import pack', async ({ page }) => {
  await mockApi(page, { ...OPTS, role: 'organiser' });
  await page.goto('/admin/problems');
  await expect(page.getByRole('button', { name: 'Import pack' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Import AI answer' })).toHaveCount(0);
  const owner = await page.context().newPage();
  await mockApi(owner, { ...OPTS, role: 'owner' });
  await owner.goto('/admin/problems');
  await expect(owner.getByRole('button', { name: 'Import AI answer' })).toBeVisible();
});

test('a bare pasted answer needs its kind, then opens on the review screen', async ({ page }) => {
  const state = await mockApi(page, OPTS);
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import AI answer' }).click();
  const panel = importPanel(page);
  await panel.getByLabel('Answer file or pasted text').fill(ANSWER);
  await expect(panel.getByRole('group', { name: 'This answer is' })).toBeVisible();
  await panel.getByRole('button', { name: 'Open answer' }).click();
  await expect(panel.getByRole('alert')).toHaveText('Say whether the answer is a whole program or one function.');
  expect(state.openBodies).toEqual([]);
  await panel.getByLabel('One function').check();
  await panel.getByLabel('ArrayList only').check();
  await panel.getByRole('button', { name: 'Open answer' }).click();
  await expect(page).toHaveURL(/\/admin\/ai\/new\?import=1$/);
  await expect(reviewing(page)).toBeVisible();
  await expect(reviewing(page).getByLabel('Title', { exact: true }).first()).toHaveValue('Array sum');
  expect(state.openBodies).toEqual([{ text: ANSWER, kind: 'function', java_lists: 'arraylist' }]);
  expect(state.draftPosts).toBe(0);
});

test('Save as file writes the wrapper, with no key or account data, and the file imports back', async ({ page }) => {
  const state = await mockApi(page, OPTS);
  await importBare(page, ANSWER, 'One function');
  await expect(reviewing(page)).toBeVisible();
  const file = await savedFile(page);
  expect(file).toEqual({ format: 'podium-ai-answer', version: 1, kind: 'function', java_lists: 'array', answer: ANSWER });
  expect(JSON.stringify(file)).not.toMatch(/sk-|arjun|college\.edu|AIza/);

  // The same file opens from the problem list with no kind question.
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import AI answer' }).click();
  await importPanel(page).getByLabel('Answer file or pasted text').fill(JSON.stringify(file));
  await expect(importPanel(page).getByRole('group', { name: 'This answer is' })).toHaveCount(0);
  await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
  await expect(reviewing(page)).toBeVisible();
  expect(state.openBodies.at(-1)).toEqual({ text: JSON.stringify(file) });
});

test('a chosen .json file fills the box and opens', async ({ page }) => {
  await mockApi(page, OPTS);
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import AI answer' }).click();
  const saved = JSON.stringify({ format: 'podium-ai-answer', version: 1, kind: 'function', java_lists: 'array', answer: ANSWER });
  await page.getByLabel('Answer file', { exact: true }).setInputFiles({ name: 'podium-ai-answer.json', mimeType: 'application/json', buffer: NodeBuffer.from(saved) });
  await expect(importPanel(page).getByLabel('Answer file or pasted text')).toHaveValue(saved);
  await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
  await expect(reviewing(page)).toBeVisible();
});

test('a refused file keeps the panel and says why', async ({ page }) => {
  await mockApi(page, { ...OPTS, open: 'file' });
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import AI answer' }).click();
  await importPanel(page).getByLabel('Answer file or pasted text').fill('{"format":"podium-ai-answer","version":2}');
  await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
  const alert = importPanel(page).getByRole('alert');
  await expect(alert).toContainText('The file could not be opened');
  await expect(alert).toContainText('The file is version 2. This server opens version 1.');
  await expect(page).toHaveURL(/\/admin\/problems$/);
});

test('an answer that fails the checks opens with its reasons, and can be created after a correction', async ({ page }) => {
  const state = await mockApi(page, { ...OPTS, open: 'fail' });
  await importBare(page, ANSWER, 'One function');
  await expect(reviewing(page).getByRole('alert')).toContainText('Subtask points add up to 90');
  await reviewing(page).getByLabel('Title', { exact: true }).first().fill('Array sum, fixed');
  await reviewing(page).getByRole('button', { name: 'Create draft' }).click();
  await expect(page.getByRole('link', { name: /Open the draft/ })).toBeVisible({ timeout: 10_000 });
  expect(state.draftBodies).toHaveLength(1);
  expect(state.draftBodies[0]).toMatchObject({ kind: 'function', java_lists: 'array' });
  expect(String(state.draftBodies[0].answer)).toContain('Array sum, fixed');
});

test('after a reload the store is empty and the import prompt shows again', async ({ page }) => {
  await mockApi(page, OPTS);
  await importBare(page, ANSWER, 'A whole program');
  await expect(reviewing(page)).toBeVisible();
  await page.reload();
  await expect(reviewing(page)).toHaveCount(0);
  await expect(importPanel(page)).toBeVisible();
  await expect(page.getByRole('form', { name: 'Request' })).toHaveCount(0);
  await importPanel(page).getByLabel('Answer file or pasted text').fill(ANSWER);
  await importPanel(page).getByLabel('A whole program').check();
  await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
  await expect(reviewing(page)).toBeVisible();
});

test('Save as file is hidden from an organiser on a run with review', async ({ page }) => {
  await mockApi(page, { ...OPTS, role: 'organiser', review: 'pass' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(reviewing(page)).toBeVisible({ timeout: 15_000 });
  await expect(reviewing(page).getByRole('button', { name: 'Save as file' })).toHaveCount(0);
});

test('a run with review gives an admin Save as file', async ({ page }) => {
  await mockApi(page, { ...OPTS, review: 'pass' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of an array');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(reviewing(page)).toBeVisible({ timeout: 15_000 });
  expect(await savedFile(page)).toMatchObject({ format: 'podium-ai-answer', version: 1, kind: 'function', java_lists: 'array' });
});

test('the whole import path works from the keyboard alone', async ({ page }) => {
  await mockApi(page, OPTS);
  await page.goto('/admin/problems');
  await activate(page, page.getByRole('button', { name: 'Import AI answer' }));
  await page.getByLabel('Answer file or pasted text').fill(ANSWER);
  await page.getByLabel('A whole program').focus();
  await page.keyboard.press('Space');
  await page.getByRole('button', { name: 'Open answer' }).focus();
  await page.keyboard.press('Enter');
  await expect(reviewing(page)).toBeVisible();
  await expect(page.getByRole('heading', { name: "Review the AI's answer" })).toBeFocused();
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('the import panel and the imported review fit at every width', async ({ page }) => {
      await mockApi(page, OPTS);
      await page.goto('/admin/problems');
      await page.getByRole('button', { name: 'Import AI answer' }).click();
      await importPanel(page).getByLabel('Answer file or pasted text').fill(ANSWER);
      await importPanel(page).getByLabel('One function').check();
      const fits = async (label: string) => {
        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          const o = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
          expect(o.scroll, `${label} overflows at ${width}px`).toBeLessThanOrEqual(o.client);
        }
      };
      await fits('import panel');
      const axe = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(axe.violations).toEqual([]);
      await importPanel(page).getByRole('button', { name: 'Open answer' }).click();
      await expect(reviewing(page)).toBeVisible();
      await fits('review screen');
      const axeReview = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(axeReview.violations).toEqual([]);
    });
  });
}
