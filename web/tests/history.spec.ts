// What is left of the v2 history screens after v3 removed revision lists
// and diffs (V3-4): the reason a save needs while a contest runs, the
// rejudge saying which tests it uses, and the console's banner for an
// edited problem (stage C2).
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';

test('while a contest runs with the problem, a save asks for the reason and sends it', async ({ page }) => {
  const api = await problems.mockApi(page);
  const live = [{ contest_id: 4, slug: 'week-6', title: 'Week 6 contest', label: 'A' }];
  (api.problems[1] as { live_contests?: unknown }).live_contests = live;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const note = page.getByRole('paragraph').filter({ hasText: 'live with this problem' });
  await expect(note).toContainText('Week 6 contest is live with this problem.');
  // "Running" is for judging; a contest is live (design.md section 8, C1-038).
  await expect(note).not.toContainText('running');
  // The saved change reaches students from the live view, one link away (walk 2026-10-21).
  await expect(page.getByRole('link', { name: 'Open the live view of Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4/console#versions-title');
  await expect(note).not.toContainText(/revision|pin/);
  await page.locator('#title').fill('A plus B, edited');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#reason-error')).toHaveText('Say why you changed the problem while a contest is live.');
  await expect(page.getByLabel('Reason for this change')).toBeFocused();
  await page.getByLabel('Reason for this change').fill('Fix a typo in the title.');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.puts.at(-1)?.reason).toBe('Fix a typo in the title.');
});

test('a rejudge says it uses the tests students are served now', async ({ page }) => {
  await contests.mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await page.getByRole('button', { name: 'Judge problem A again' }).click();
  await expect(page.getByRole('dialog', { name: 'Judge problem A again?' })).toContainText(
    'Every submission to problem A is judged again with the tests students are served now. New submissions still go first',
  );
});

test('an edited problem is a banner on the console until students get it (C2)', async ({ page }) => {
  const api = await contests.mockApi(page, { who: 'admin', phase: 'during', updateAvailable: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console#versions-title');
  const banners = page.getByRole('region', { name: 'Needs attention' });
  await expect(banners).toContainText('Problem A was edited. Students still see the old statement.');
  await banners.getByRole('button', { name: 'Update students now for problem A' }).click();
  const confirm = page.getByRole('dialog', { name: 'Update students now to the edited problem A?' });
  await expect(confirm).toContainText('Only the statement changed, so no submission is judged again.');
  await confirm.getByRole('button', { name: 'Update students now' }).click();
  await expect.poll(() => api.pinPuts.length).toBe(1);
  // The server's announcement is ready to post, never posted by itself.
  await expect(page.getByRole('dialog', { name: 'Announce to every student' }).getByLabel('Announcement (Markdown)')).toHaveValue(/The statement of problem A was updated/);
  expect(api.posted).toHaveLength(0);
});
