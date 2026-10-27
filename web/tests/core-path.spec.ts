// The core path (v3 S1-02): the few things a student and an organiser must
// always be able to do, written with role and name locators only so the
// stage 4 redesign can move things without breaking it. No CSS classes,
// no full sentences: a test fails only when a job stops working.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';

test('student: open the contest, start, run, submit, see a verdict, open standings', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByRole('link', { name: 'Week 6 contest' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Week 6 contest');

  await page.getByRole('button', { name: /start the contest/i }).click();
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/A plus B/);
  await expect(page.getByRole('timer')).toBeVisible();

  const result = page.getByRole('region', { name: 'Result' });
  await page.getByRole('button', { name: /^Run/ }).first().click();
  await expect.poll(() => api.runs.length).toBe(1);
  api.stream.push({
    type: 'run',
    data: {
      id: 70,
      state: 'done',
      compile_output: '',
      results: [{ kind: 'sample', label: 'Sample 1', verdict: 'AC', time_ms: 10, memory_kb: 3200, output: '3', expected: '3', message: '' }],
    },
  });
  await expect(result.getByText(/example passed/i).first()).toBeVisible({ timeout: 10_000 });

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  const id = Number(api.submissions.at(-1)!.id);
  api.scores.A = 40;
  const judged = contests.submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.stream.push({ type: 'submission', data: judged });
  await expect(result.getByText(/40 of 100/).first()).toBeVisible({ timeout: 10_000 });

  await page.getByRole('link', { name: 'Standings' }).first().click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/Standings/);
  await expect(page.getByRole('table')).toBeVisible();
});

test('organiser: import a pack and see it validated', async ({ page }) => {
  await problems.mockApi(page);
  const choosers = problems.watchFileChoosers(page);
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import pack' }).click();
  await (await choosers.next()).setFiles(problems.zipFile('pair.zip', 'good pack'));
  const row = page.getByRole('row', { name: /Pair with sum/ });
  await expect(row.getByText('Validated', { exact: true })).toBeVisible({ timeout: 15_000 });
});

test('organiser: create a contest, add a problem, publish', async ({ page }) => {
  const created = await contests.mockApi(page, { who: 'admin', phase: 'upcoming', published: false, publishOk: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title', { exact: true }).fill('Week 9');
  await page.getByLabel('Slug', { exact: true }).fill('week-9');
  await page.getByLabel('Starts', { exact: true }).fill('2026-11-01T18:00');
  await page.getByLabel('Ends', { exact: true }).fill('2026-11-01T20:00');
  await page.getByRole('button', { name: 'Create contest' }).click();
  await expect(page).toHaveURL(/\/admin\/contests\/9/);

  // The mock serves one editable draft, contest 4.
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await expect.poll(() => created.problemPuts.length).toBe(1);

  await page.getByRole('button', { name: 'Publish contest' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Publish contest' }).click();
  await expect.poll(() => created.settingsPuts.at(-1)?.published).toBe(true);
});
