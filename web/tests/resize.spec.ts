// Resizable panes from 1024 px (v2.0.1 stage D): drag and keyboard on every
// splitter (statement and code, editor and result, the box and output in
// "Your input", the organiser's Write and Preview), Reset layout, stored
// sizes clamped on load, and nothing resizable under 1024 px. Every /api/
// call is mocked, as in the other specs.
import { expect, test, type Page } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';
import { HISTORY, mockApi, openWorkspace } from './mocks/workspace';

const WIDTHS = [1024, 1280, 1440] as const;
const THEMES = ['light', 'dark'] as const;
const KEYS = ['podium-workspace-statement', 'podium-workspace-result', 'podium-workspace-input', 'podium-workspace-input-tab', 'podium-organiser-write'];

const sep = (page: Page, name: string) => page.getByRole('separator', { name: `${name} panel size` });
const value = async (page: Page, name: string) => Number(await sep(page, name).getAttribute('aria-valuenow'));

/** Drags a splitter's grip by (dx, dy) pixels with the mouse. */
async function drag(page: Page, name: string, dx: number, dy: number) {
  const box = (await sep(page, name).boundingBox())!;
  const x = box.x + box.width / 2;
  // A tall grip (the organiser's) may reach below the window: grab it near its top.
  const y = box.y + Math.min(box.height / 2, 150);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y + dy, { steps: 6 });
  await page.mouse.up();
}

const shots = () => `${test.info().project.testDir}/../../cache/screenshots-v201-d/`;

const stored = (page: Page, key: string) => page.evaluate((k) => localStorage.getItem(k), key);

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test.describe(`${width} px, ${theme}`, () => {
      test.use({ colorScheme: theme, viewport: { width, height: width === 1024 ? 640 : 900 } });

      test('statement and code on a page outside a contest: drag, keys, hide', async ({ page }) => {
        await mockApi(page, { submissions: HISTORY });
        await openWorkspace(page);
        const split = sep(page, 'Statement');
        await expect(split).toHaveAttribute('aria-controls', 'ws-problem');
        await expect(split).toHaveAttribute('aria-valuenow', '40');
        // The grip is at least 24 px wide (WCAG 2.5.8).
        expect((await split.boundingBox())!.width).toBeGreaterThanOrEqual(24);
        const statement = page.getByRole('region', { name: 'Statement' });
        const code = page.getByRole('region', { name: 'Code' });

        // A drag to the right widens the statement and keeps the size after a reload.
        const before = (await statement.boundingBox())!.width;
        await drag(page, 'Statement', 80, 0);
        expect((await statement.boundingBox())!.width).toBeGreaterThan(before + 40);
        const dragged = await value(page, 'Statement');
        expect(dragged).toBeGreaterThan(40);
        await page.reload();
        await expect(sep(page, 'Statement')).toHaveAttribute('aria-valuenow', String(dragged));

        // Keys: arrows 5, Page keys 15, Home and End the ends, all clamped.
        await sep(page, 'Statement').focus();
        await page.keyboard.press('Home');
        await expect(split).toHaveAttribute('aria-valuenow', '20');
        await page.keyboard.press('ArrowRight');
        await expect(split).toHaveAttribute('aria-valuenow', '25');
        await page.keyboard.press('PageUp');
        await expect(split).toHaveAttribute('aria-valuenow', '40');
        await page.keyboard.press('PageDown');
        await expect(split).toHaveAttribute('aria-valuenow', '25');
        await page.keyboard.press('ArrowLeft');
        await expect(split).toHaveAttribute('aria-valuenow', '20');
        await page.keyboard.press('End');
        await expect(split).toHaveAttribute('aria-valuenow', '80');
        await page.keyboard.press('PageUp');
        await expect(split).toHaveAttribute('aria-valuenow', '80');
        // The code column never drops under its minimum, even with the statement at 80 percent.
        expect((await code.boundingBox())!.width).toBeGreaterThanOrEqual(340);
        await page.keyboard.press('Home');

        // Enter hides the statement and the code takes the width; the button in the title brings it back.
        const narrow = (await code.boundingBox())!.width;
        await page.keyboard.press('Enter');
        await expect(statement).toBeHidden();
        expect((await code.boundingBox())!.width).toBeGreaterThan(narrow + 150);
        await page.screenshot({ path: `${shots()}resize-statement-hidden-${width}-${theme}.png` });
        await page.getByRole('button', { name: 'Show statement' }).click();
        await expect(statement).toBeVisible();
        await expect(sep(page, 'Statement')).toHaveAttribute('aria-valuenow', '20');
      });

      test('editor and result: drag and keys', async ({ page }) => {
        await mockApi(page, { submissions: HISTORY });
        await openWorkspace(page);
        const result = page.getByRole('region', { name: 'Result' });
        expect(await value(page, 'Result')).toBe(width === 1024 ? 30 : 35);
        const before = (await result.boundingBox())!.height;
        await drag(page, 'Result', 0, -80);
        expect((await result.boundingBox())!.height).toBeGreaterThan(before + 40);
        await sep(page, 'Result').focus();
        await page.keyboard.press('Home');
        await expect(sep(page, 'Result')).toHaveAttribute('aria-valuenow', '20');
        await page.keyboard.press('PageUp');
        await expect(sep(page, 'Result')).toHaveAttribute('aria-valuenow', '35');
      });

      test('Your input fills the result pane and a row splitter divides it', async ({ page }) => {
        await mockApi(page, { submissions: HISTORY });
        // Room for the box to grow: the 640 px tier's pane is at its minimum.
        await page.setViewportSize({ width, height: 900 });
        await openWorkspace(page);
        await page.getByRole('tab', { name: 'Your input' }).click();
        const box = page.getByRole('textbox', { name: 'Your input' });
        await expect(box).toBeVisible();
        await page.screenshot({ path: `${shots()}resize-input-default-${width}-${theme}.png` });
        // The box and output share the pane's height, so give the pane room first.
        await sep(page, 'Result').focus();
        await page.keyboard.press('End');
        const out = sep(page, 'Output');
        await expect(out).toHaveAttribute('aria-valuenow', '25');
        const pane = (await page.getByRole('region', { name: 'Result' }).boundingBox())!;
        const h0 = (await box.boundingBox())!.height;
        // The box takes a share of the pane, not a fixed four rows.
        expect(h0).toBeGreaterThan(pane.height * 0.2);
        await out.focus();
        await page.keyboard.press('ArrowDown');
        await expect(out).toHaveAttribute('aria-valuenow', '20');
        expect((await box.boundingBox())!.height).toBeGreaterThan(h0);
        await page.keyboard.press('PageUp');
        await expect(out).toHaveAttribute('aria-valuenow', '35');
        await page.keyboard.press('Home');
        await page.keyboard.press('ArrowDown');
        // The output is never hidden: Enter and a step below the smallest do nothing.
        await expect(out).toHaveAttribute('aria-valuenow', '20');
        await page.keyboard.press('Enter');
        await expect(out).toHaveAttribute('aria-valuenow', '20');
        await expect(page.locator('#ws-input-out')).toBeVisible();
        await drag(page, 'Output', 0, -40);
        expect(await value(page, 'Output')).toBeGreaterThan(20);
        await page.screenshot({ path: `${shots()}resize-input-${width}-${theme}.png` });
      });

      test('Your input has its own result share and the box shows enough lines', async ({ page }) => {
        await page.setViewportSize({ width, height: { 1024: 640, 1280: 800, 1440: 900 }[width] });
        await mockApi(page, { submissions: HISTORY });
        await openWorkspace(page);
        const res = sep(page, 'Result');
        const own = width === 1024 ? '30' : '35';
        await expect(res).toHaveAttribute('aria-valuenow', own);
        await page.getByRole('tab', { name: 'Your input' }).click();
        await expect(res).toHaveAttribute('aria-valuenow', '75');
        const lines = await page.getByRole('textbox', { name: 'Your input' }).evaluate((el) => {
          const cs = getComputedStyle(el);
          return (el.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom)) / parseFloat(cs.lineHeight);
        });
        expect(lines).toBeGreaterThanOrEqual(width === 1024 ? 5 : 8);
        await page.getByRole('tab', { name: 'Run' }).click();
        await expect(res).toHaveAttribute('aria-valuenow', own);
      });

      test('in a contest the same splitter works and Enter hides the statement', async ({ page }) => {
        await contests.mockApi(page, { phase: 'during' });
        await page.goto('/contests/week-6/problems/A');
        await expect(page.locator('.cm-content')).toHaveAttribute('data-language', /.+/);
        const split = sep(page, 'Statement');
        await expect(split).toHaveAttribute('aria-valuenow', '40');
        await split.focus();
        await page.keyboard.press('PageUp');
        await expect(split).toHaveAttribute('aria-valuenow', '55');
        await page.keyboard.press('Enter');
        await expect(page.getByRole('region', { name: 'Statement' })).toBeHidden();
        await page.screenshot({ path: `${shots()}resize-contest-hidden-${width}-${theme}.png` });
      });

      test('Reset layout puts every size back and removes only its own keys', async ({ page }) => {
        await mockApi(page, { submissions: HISTORY });
        await page.addInitScript(() => localStorage.setItem('podium-editor-wrap', '1'));
        await openWorkspace(page);
        await sep(page, 'Statement').focus();
        await page.keyboard.press('PageUp');
        await sep(page, 'Result').focus();
        await page.keyboard.press('PageUp');
        await page.getByRole('tab', { name: 'Your input' }).click();
        await sep(page, 'Output').focus();
        await page.keyboard.press('PageUp');
        await sep(page, 'Result').focus();
        await page.keyboard.press('PageDown');
        await expect.poll(() => stored(page, 'podium-workspace-input-tab')).not.toBeNull();
        await expect.poll(() => stored(page, 'podium-workspace-statement')).not.toBeNull();
        await expect.poll(() => stored(page, 'podium-workspace-input')).not.toBeNull();

        await page.getByRole('button', { name: 'Settings' }).click();
        await page.getByRole('button', { name: 'Reset layout' }).click();
        await expect(sep(page, 'Statement')).toHaveAttribute('aria-valuenow', '40');
        await expect(sep(page, 'Result')).toHaveAttribute('aria-valuenow', '75');
        await expect(sep(page, 'Output')).toHaveAttribute('aria-valuenow', '25');
        await page.getByRole('tab', { name: 'Run' }).click();
        await expect(sep(page, 'Result')).toHaveAttribute('aria-valuenow', width === 1024 ? '30' : '35');
        for (const key of KEYS) expect(await stored(page, key), key).toBeNull();
        expect(await stored(page, 'podium-editor-wrap')).toBe('1');
        await page.reload();
        await expect(sep(page, 'Statement')).toHaveAttribute('aria-valuenow', '40');
      });

      test('a bad stored size is clamped and never hides a pane', async ({ page }) => {
        await page.addInitScript(() => {
          localStorage.setItem('podium-workspace-statement', JSON.stringify({ share: 3, collapsed: false }));
          localStorage.setItem('podium-workspace-result', JSON.stringify({ share: 900, collapsed: false }));
          localStorage.setItem('podium-workspace-input', 'not json');
          localStorage.setItem('podium-organiser-write', JSON.stringify({ share: -40, collapsed: false }));
        });
        await mockApi(page, { submissions: HISTORY });
        await openWorkspace(page);
        await expect(sep(page, 'Statement')).toHaveAttribute('aria-valuenow', '20');
        await expect(sep(page, 'Result')).toHaveAttribute('aria-valuenow', '80');
        await expect(page.getByRole('region', { name: 'Statement' })).toBeVisible();
        await expect(page.getByRole('region', { name: 'Code' })).toBeVisible();
        expect((await page.getByRole('region', { name: 'Code' }).boundingBox())!.width).toBeGreaterThan(300);
        await page.getByRole('tab', { name: 'Your input' }).click();
        await expect(sep(page, 'Output')).toHaveAttribute('aria-valuenow', '25');
        await problems.mockApi(page);
        await page.goto('/admin/problems/1');
        await expect(sep(page, 'Write')).toHaveAttribute('aria-valuenow', '20');
      });

      test('the organiser Write and Preview resize, and the code boxes have a height grip', async ({ page }) => {
        await problems.mockApi(page);
        await page.goto('/admin/problems/1');
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
        const split = sep(page, 'Write');
        await expect(split).toHaveAttribute('aria-valuenow', '50');
        const write = page.locator('#statement-panel-write');
        const before = (await write.boundingBox())!.width;
        await drag(page, 'Write', 100, 0);
        expect((await write.boundingBox())!.width).toBeGreaterThan(before + 50);
        await split.focus();
        await page.keyboard.press('Home');
        await expect(split).toHaveAttribute('aria-valuenow', '20');
        await page.keyboard.press('Enter');
        await expect(split).toHaveAttribute('aria-valuenow', '20');
        await expect(write).toBeVisible();
        await page.keyboard.press('End');
        await expect(split).toHaveAttribute('aria-valuenow', '80');
        await expect(page.getByRole('heading', { name: 'Preview' })).toBeVisible();
        expect(await stored(page, 'podium-organiser-write')).toContain('80');
        await page.screenshot({ path: `${shots()}resize-organiser-${width}-${theme}.png` });

        // The statement box and a program box change height from the corner grip.
        expect(await page.locator('.statement-editor').evaluate((el) => getComputedStyle(el).resize)).toBe('vertical');
        await page.getByRole('tab', { name: 'Tests' }).click();
        await page.getByRole('tab', { name: 'Checking' }).click();
        const box = page.locator('textarea.textarea-code').first();
        await expect(box).toBeVisible();
        expect(await box.evaluate((el) => getComputedStyle(el).resize)).toBe('vertical');
        expect(await box.evaluate((el) => parseFloat(getComputedStyle(el).minHeight))).toBeGreaterThanOrEqual(96);
        await page.screenshot({ path: `${shots()}resize-codebox-${width}-${theme}.png` });
      });
    });
  }
}

for (const width of [375, 768]) {
  test(`under 1024 px nothing is resizable and the panes stay tabs (${width} px)`, async ({ page }) => {
    await mockApi(page, { submissions: HISTORY });
    await page.setViewportSize({ width, height: 900 });
    await openWorkspace(page);
    await expect(page.getByRole('separator')).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Problem' })).toHaveAttribute('aria-selected', 'true');
    await page.screenshot({ path: `${shots()}resize-none-${width}.png` });
    await problems.mockApi(page);
    await page.goto('/admin/problems/1');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
    await expect(page.getByRole('separator')).toHaveCount(0);
  });
}
