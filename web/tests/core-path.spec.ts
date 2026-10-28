// The core path (v3 S1-02): the few things a student and an organiser must
// always be able to do, written with role and name locators only so the
// stage 4 redesign can move things without breaking it. No CSS classes,
// no full sentences: a test fails only when a job stops working.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';
import { codeEditor } from './locators';

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
  // The verdict lists each subtask with its points.
  await expect(result.getByText('Subtask 1', { exact: true })).toBeVisible();
  await expect(result.getByText('40 of 40', { exact: true })).toBeVisible();
  await expect(result.getByText('Subtask 2', { exact: true })).toBeVisible();
  await expect(result.getByText('0 of 60', { exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Standings' }).first().click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/Standings/);
  await expect(page.getByRole('table')).toBeVisible();
});

test('student: code typed in A stays in A when switching to B and back, and after a reload', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  const problemsNav = page.getByRole('navigation', { name: 'Problems' });
  const editor = codeEditor(page);
  const typeAtEnd = async (text: string) => {
    await editor.click();
    await page.keyboard.press('Control+End');
    await page.keyboard.type(text);
  };
  // Leaving a problem saves its draft at once.
  const saved = (text: string) => expect.poll(() => api.draftPuts.some((b) => String(b.source).includes(text))).toBe(true);

  await typeAtEnd('\n// mark A');
  await problemsNav.getByRole('link', { name: /^Problem B/ }).click();
  await expect(page).toHaveURL(/\/problems\/B$/);
  await saved('// mark A');
  await expect(editor).toHaveText(/\S/);
  await expect(editor).not.toContainText('// mark A');
  await typeAtEnd('\n// mark B');

  await problemsNav.getByRole('link', { name: /^Problem A/ }).click();
  await expect(page).toHaveURL(/\/problems\/A$/);
  await saved('// mark B');
  await expect(editor).toContainText('// mark A');
  await expect(editor).not.toContainText('// mark B');

  await page.reload();
  await expect(editor).toContainText('// mark A');
  await problemsNav.getByRole('link', { name: /^Problem B/ }).click();
  await expect(editor).toContainText('// mark B');
  await expect(editor).not.toContainText('// mark A');
});

test('student: an autosaved draft comes back from the server after a reload', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  const editor = codeEditor(page);
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n// autosaved');
  // No blur, no button: the 10 s timer saves it.
  await expect.poll(() => api.draftPuts.some((b) => String(b.source).includes('// autosaved')), { timeout: 15_000 }).toBe(true);
  // Only the server's copy is left to restore from.
  await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-draft:')).forEach((k) => localStorage.removeItem(k)));
  await page.reload();
  await expect(editor).toContainText('// autosaved');
});

test('student: run on their own input', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await page.getByRole('button', { name: 'Custom input' }).click();
  await page.getByLabel('Custom input').fill('5 7');
  await page.getByRole('button', { name: 'Run on this input' }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  expect(api.runs[0]).toMatchObject({ input: '5 7' });
  api.stream.push({
    type: 'run',
    data: {
      id: 70,
      state: 'done',
      compile_output: '',
      results: [{ kind: 'custom', label: 'Custom input', verdict: 'OK', time_ms: 8, memory_kb: 3200, output: '12', expected: '', message: '' }],
    },
  });
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.getByRole('heading', { name: 'Run on your input' })).toBeVisible({ timeout: 10_000 });
  await expect(result.getByText('12', { exact: true }).first()).toBeVisible();
});

test('student: announcements arrive as a banner, and Dismiss clears them one by one', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  for (const [id, body] of [
    [1, 'Problem B: sample 2 is fixed.'],
    [2, 'Ten minutes left.'],
  ] as const) {
    api.announcements.push({ id, body_md: body, created_at: Date.now() + id * 1000 });
    api.stream.push({ type: 'announcement', data: { contest: 4, id, body_md: body } });
  }
  await expect(page.getByText(/1 more message waiting/)).toBeVisible({ timeout: 10_000 });
  await page.getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(page.getByText(/more message/)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Dismiss', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Show all messages' })).toHaveCount(0);
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
