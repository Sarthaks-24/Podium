// The organiser history screens of v2 P2 (F10.1), against mocks of
// contracts C3 and C4 (docs/API.md "History and audit"): the problem
// editor's revision list and comparison, the reason a save needs while a
// contest is running, "Update contest to revision N" on the console with
// its prefilled announcement, the rejudge naming its revision, and the
// console's "Changes" feed with its filters. People "History" is in
// screens.spec.ts and the student's statement diff in workspace.spec.ts.
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m2/`;
}

test('the problem editor lists revisions and compares one with the one before', async ({ page }) => {
  const api = await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const history = page.getByRole('region', { name: 'History' });
  await expect(page.getByRole('navigation', { name: 'Sections' }).getByRole('link', { name: 'History' })).toHaveAttribute('href', '#history-heading');
  const rows = history.getByRole('listitem');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0)).toContainText('Revision 3');
  await expect(rows.nth(0)).toContainText('Current');
  // "Current" is a neutral badge, not the contest state Live (C1-030).
  await expect(rows.nth(0).locator('.badge[data-tone="neutral"]')).toHaveText('Current');
  await expect(rows.nth(0).locator('.state-word')).toHaveCount(0);
  await expect(rows.nth(0)).toContainText('Statement changed. Validated.');
  await expect(rows.nth(0)).toContainText('Reason: Fix the bound on n in the statement.');
  await expect(rows.nth(1)).toContainText('Pinned in week-6 (problem A)');
  await expect(rows.nth(2)).toContainText('made by the server');
  await expect(rows.nth(2).getByRole('button')).toHaveCount(0);

  const compare = rows.nth(0).getByRole('button', { name: 'Compare with revision 2' });
  await compare.click();
  await expect(compare).toHaveAttribute('aria-expanded', 'true');
  const diff = history.getByRole('region', { name: 'Revision 3 against revision 2' });
  await expect(diff).toContainText('Changed: statement. A contest can move to it without judging again.');
  await expect(diff.locator('.line.del')).toContainText('1 <= n <= 10');
  await expect(diff.locator('.line.add')).toContainText('1 <= n <= 100');
  expect(api.diffQueries).toEqual(['?from=2&to=3']);
  await history.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${shots()}problem-history-1440-light.png` });
  const results = await new AxeBuilder({ page }).include('#history-heading').withTags(['wcag2a', 'wcag2aa']).analyze();
  expect(results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')).toEqual([]);
});

test('while a contest runs with the problem, a save asks for the reason and sends it', async ({ page }) => {
  const api = await problems.mockApi(page);
  const live = [{ contest_id: 4, slug: 'week-6', title: 'Week 6 contest', label: 'A' }];
  (api.problems[1] as { live_contests?: unknown }).live_contests = live;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await expect(page.locator('.live-note')).toContainText('Week 6 contest is running with this problem.');
  await page.locator('#title').fill('A plus B, edited');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#reason-error')).toHaveText('Say why you changed the problem while a contest is running.');
  await expect(page.getByLabel('Reason for this change')).toBeFocused();
  await page.getByLabel('Reason for this change').fill('Fix a typo in the title.');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.puts.at(-1)?.reason).toBe('Fix a typo in the title.');
});

test('the console moves a problem to a new revision and offers the announcement', async ({ page }) => {
  const api = await contests.mockApi(page, { who: 'admin', phase: 'during', updateAvailable: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const versions = page.getByRole('region', { name: 'Problem versions' });
  await expect(versions).toContainText('A plus B, revision 2');
  await expect(versions).toContainText('Revision 3 is validated and changes only the statement.');
  await versions.getByRole('button', { name: 'Update contest to revision 3' }).click();

  const dialog = page.getByRole('dialog', { name: 'Update problem A to revision 3?' });
  await expect(dialog).toContainText(
    'Students get revision 3 of problem A instead of revision 2 the next time they open it. Only the statement changed, so no submission is judged again.',
  );
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Update contest to revision 3' }).click();
  await expect(page.getByText('Problem A now uses revision 3', { exact: true })).toBeVisible();
  expect(api.pinPuts).toEqual([{ label: 'A', body: { revision_id: 13 } }]);

  // The announcement is prefilled for the organiser to read and post; nothing is posted yet.
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(
    'The statement of problem A was updated. Open the problem and choose "See what changed".',
  );
  expect(api.posted).toEqual([]);
  await expect(versions.locator('.pin-note')).toContainText('Read the announcement in Announcements and post it');
  await expect(versions).toContainText('A plus B, revision 3');
  await versions.getByText('Pin history').click();
  await expect(versions.locator('.pins li').first()).toContainText('Problem A pinned to revision 3');
  await expect(versions.locator('.pins li').first()).toContainText('by arjun.mehta@college.edu');

  // Changes shows the move and filters by kind.
  const changes = page.getByRole('region', { name: 'Changes' });
  await expect(changes).toContainText('Moved problem A from revision 2 to revision 3 (statement changed)');
  await changes.getByLabel('Kind of change').selectOption('pause');
  await expect(changes).toContainText('No change matches. Clear the filter to see everything.');
  expect(api.auditQueries.at(-1)).toBe('?page=1&action=pause');
  await changes.getByLabel('Kind of change').selectOption('');
  await changes.getByLabel('Made by').fill('priya');
  await changes.getByRole('button', { name: 'Search' }).click();
  await expect.poll(() => api.auditQueries.at(-1)).toBe('?page=1&actor=priya');
  await page.screenshot({ path: `${shots()}console-versions-1440-light.png`, fullPage: true });
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
