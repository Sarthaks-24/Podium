// What is left of the v2 history screens after v3 removed revision lists
// and diffs (V3-4): the reason a save needs while a contest runs, and the
// rejudge naming the revision it judges against.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';

test('while a contest runs with the problem, a save asks for the reason and sends it', async ({ page }) => {
  const api = await problems.mockApi(page);
  const live = [{ contest_id: 4, slug: 'week-6', title: 'Week 6 contest', label: 'A' }];
  (api.problems[1] as { live_contests?: unknown }).live_contests = live;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await expect(page.locator('.live-note')).toContainText('Week 6 contest is live with this problem.');
  // "Running" is for judging; a contest is live (design.md section 8, C1-038).
  await expect(page.locator('.live-note')).not.toContainText('running');
  // The new revision reaches students from the console, one link away (walk 2026-10-21).
  await expect(page.locator('.live-note').getByRole('link', { name: 'Open the console of Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4/console#versions-title');
  await page.locator('#title').fill('A plus B, edited');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#reason-error')).toHaveText('Say why you changed the problem while a contest is live.');
  await expect(page.getByLabel('Reason for this change')).toBeFocused();
  await page.getByLabel('Reason for this change').fill('Fix a typo in the title.');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.puts.at(-1)?.reason).toBe('Fix a typo in the title.');
});

test('a rejudge names the revision it judges against', async ({ page }) => {
  await contests.mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await page.getByLabel('Judge every submission to a problem again').selectOption('A');
  await expect(page.getByText('Judged against revision 3, the one students are served.')).toBeVisible();
  await page.getByRole('button', { name: 'Judge problem again' }).click();
  await expect(page.getByRole('dialog', { name: 'Judge problem A again?' })).toContainText(
    'Every submission to problem A is judged again against revision 3, the one students are served. New submissions still go first',
  );
});
