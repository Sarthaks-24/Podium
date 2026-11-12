// The problem list of the contest editor (v2.1.0 stage V): the save queue,
// the problem bank picker and the letter field. Answered by the mocks of
// mocks/contests.ts.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { apiError, json, mockApi, shots, THEMES, WIDTHS } from './mocks/contests';

test('a quick Add then Move sends the second save only after the first has been answered', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  const puts: { problem_id: number; label: string }[][] = [];
  await page.route('**/api/admin/contests/4/problems', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    puts.push(route.request().postDataJSON());
    if (puts.length === 1) await held;
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();

  // The first answer is held; the Move must still be waiting behind it.
  await page.waitForTimeout(500);
  expect(puts).toHaveLength(1);
  release();
  await expect.poll(() => puts.length).toBe(2);
  expect(puts[0].map((p) => p.label)).toEqual(['A', 'B']);
  expect(puts[1].map((p) => `${p.label}:${p.problem_id}`)).toEqual([`A:${puts[0][1].problem_id}`, `B:${puts[0][0].problem_id}`]);
});

test('a failed load of the problem bank says so and Retry loads it', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let failing = true;
  await page.route('**/api/admin/problems', async (route) => {
    if (route.request().method() !== 'GET' || !failing) return route.fallback();
    return apiError(route, 500, 'internal', 'Something went wrong on our side.');
  });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  const alert = page.getByRole('alert').filter({ hasText: 'The problem bank did not load' });
  await expect(alert).toBeVisible();
  failing = false;
  await alert.getByRole('button', { name: 'Retry' }).click();
  await expect(alert).toHaveCount(0);
  await expect(page.getByLabel('Add a validated problem').getByRole('option', { name: /Interval covering/ })).toHaveCount(1);
});

test('an empty problem bank gets a hint, not a bare picker', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.route('**/api/admin/problems', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return json(route, 200, { problems: [] });
  });
  await page.goto('/admin/contests/4');
  await expect(page.getByText('The problem bank is empty.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Go to the problem bank' })).toBeVisible();
});

test('a bad problem letter is tied to its input and the input is marked invalid', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.goto('/admin/contests/4');
  const letter = page.getByLabel('Letter for A plus B');
  await letter.fill('1');
  await letter.press('Tab');
  const message = page.getByText('Each letter must be one to three capital letters.');
  await expect(message).toBeVisible();
  await expect(letter).toHaveAttribute('aria-invalid', 'true');
  await expect(letter).toHaveAccessibleDescription('Each letter must be one to three capital letters.');
});

const VIEWS = {
  'contest-bank-error': async (page: Page) => {
    await page.route('**/api/admin/problems', (route) => (route.request().method() === 'GET' ? apiError(route, 500, 'internal', 'Something went wrong on our side.') : route.fallback()));
  },
  'contest-bank-empty': async (page: Page) => {
    await page.route('**/api/admin/problems', (route) => (route.request().method() === 'GET' ? json(route, 200, { problems: [] }) : route.fallback()));
  },
  'contest-letter-error': async () => {},
} as const;

for (const theme of THEMES) {
  for (const [name, prepare] of Object.entries(VIEWS)) {
    test(`${name} holds at every width, ${theme} theme, axe clean`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await mockApi(page, { who: 'organiser', phase: 'upcoming' });
      await prepare(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/contests/4');
      await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
      if (name === 'contest-letter-error') {
        await page.getByLabel('Letter for A plus B').fill('1');
        await page.getByLabel('Letter for A plus B').press('Tab');
        await expect(page.getByLabel('Letter for A plus B')).toHaveAttribute('aria-invalid', 'true');
      } else {
        await expect(page.locator('#add-problem')).toBeVisible();
        await expect(name === 'contest-bank-error' ? page.getByText('The problem bank did not load') : page.getByText('The problem bank is empty.')).toBeVisible();
      }
      const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`)).toEqual([]);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
        await page.locator('#add-problem').scrollIntoViewIfNeeded();
        const dir = process.env.PODIUM_SHOTS ?? shots();
        mkdirSync(dir, { recursive: true });
        await page.screenshot({ path: `${dir}${name}-${width}-${theme}.png`, fullPage: true });
      }
    });
  }
}
