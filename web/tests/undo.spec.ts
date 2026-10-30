// Undo toasts (v4 U4): before a contest starts, removing a problem acts at
// once and the toast offers Undo for 8 seconds instead of asking first;
// Undo puts the problem back where it was and saves. A live contest still asks.
import { expect, test } from '@playwright/test';
import { mockApi } from './mocks/contests';
import { toasts } from './locators';

test('removing a problem before the start offers Undo, and Undo puts it back in its place', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  const letter = page.getByLabel('Letter for A plus B');
  await expect(letter).toHaveValue('A');
  const before = await page.getByLabel(/^Letter for /).count();

  await page.getByRole('button', { name: 'Remove A plus B' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(letter).toHaveCount(0);
  await expect.poll(() => api.problemPuts.length).toBe(1);

  const toast = toasts(page).getByText('Removed A plus B', { exact: true }).locator('..');
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect(letter).toHaveValue('A');
  await expect(page.getByLabel(/^Letter for /)).toHaveCount(before);
  await expect(page.getByRole('button', { name: 'Remove A plus B' })).toBeFocused();
  await expect.poll(() => api.problemPuts.length).toBe(2);
  expect(JSON.stringify(api.problemPuts[1])).toContain('"label":"A"');
  await expect(toasts(page).getByText('Put back A plus B', { exact: true })).toBeVisible();
});

test('an Undo toast stays 8 seconds, longer than a plain toast', async ({ page }) => {
  await page.clock.install();
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByRole('button', { name: 'Remove A plus B' }).click();
  const undo = toasts(page).getByRole('button', { name: 'Undo' });
  await expect(undo).toBeVisible();
  await page.clock.runFor(7000);
  await expect(undo).toBeVisible();
  await page.clock.runFor(1500);
  await expect(undo).toHaveCount(0);
});
