// Kept answers (v4 U2): back on a screen seen before, its last answer shows
// at once while a fresh one loads behind it; a refresh that fails keeps it
// and says it may be out of date. Sign out forgets every kept answer.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';

test('back to the contest list shows its rows at once, with no loading state', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.goto('/');
  const row = page.getByRole('link', { name: /Week 6/ }).first();
  await expect(row).toBeVisible();
  await row.click();
  await expect(page).toHaveURL(/\/contests\/week-6$/);

  // The fresh list is held back: the kept one must show without it.
  let release = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/contests', async (route) => {
    await gate;
    await route.fallback();
  });
  await page.goBack();
  await expect(row).toBeVisible();
  await expect(page.getByTestId('skeleton')).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Loading contests' })).toHaveCount(0);
  release();
});

test('a refresh that fails keeps the list and says it may be out of date; Try again clears it', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.goto('/');
  const row = page.getByRole('link', { name: /Week 6/ }).first();
  await expect(row).toBeVisible();
  await row.click();
  await expect(page).toHaveURL(/\/contests\/week-6$/);

  await page.route('**/api/contests', (route) => route.abort());
  await page.goBack();
  await expect(row).toBeVisible();
  const note = page.getByRole('status').filter({ hasText: 'This may be out of date: the latest could not load.' });
  await expect(note).toBeVisible();

  await page.unroute('**/api/contests');
  await note.getByRole('button', { name: 'Try again' }).click();
  await expect(note).toHaveCount(0);
  await expect(row).toBeVisible();
});
