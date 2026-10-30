// The core path (v3 S1-02): the few things a student and an organiser must
// always be able to do, written with role and name locators only so the
// stage 4 redesign can move things without breaking it. No CSS classes,
// no full sentences: a test fails only when a job stops working.
import AxeBuilder from '@axe-core/playwright';
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
      results: [{ kind: 'sample', label: 'Example 1', verdict: 'AC', time_ms: 10, memory_kb: 3200, output: '3', expected: '3', message: '' }],
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

  // B3: the board opens over the editor; the problem stays.
  await page.getByRole('button', { name: 'Standings' }).first().click();
  const board = page.getByRole('dialog', { name: 'Standings' });
  await expect(board.getByRole('table')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(board).toBeHidden();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/A plus B/);
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

test('student: the language chosen on A is still chosen on B, and typed code is kept per language (P5)', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  const editor = codeEditor(page);
  const language = page.getByLabel('Language');
  await expect(editor).toHaveAttribute('data-language', /.+/);

  // Untouched starter: the switch swaps it without asking.
  await language.selectOption('python3');
  await expect(editor).toContainText('sys.stdin');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n# mine in python');

  // Typed code: the switch asks first; Cancel keeps the code under the new language.
  await language.selectOption('cpp17');
  const ask = page.getByRole('dialog', { name: 'Replace your code with the C++17 starter?' });
  await expect(ask).toContainText('Your Python 3 code is kept and comes back when you switch back.');
  await ask.getByRole('button', { name: 'Use the starter' }).click();
  await expect(editor).not.toContainText('# mine in python');
  await expect(language).toHaveValue('cpp17');
  // Back to Python: its code comes back.
  await language.selectOption('python3');
  await expect(editor).toContainText('# mine in python');
  await expect(page.getByRole('dialog')).toHaveCount(0);

  // Problem B opens in Python, the language chosen last.
  await page.getByRole('navigation', { name: 'Problems' }).getByRole('link', { name: /^Problem B/ }).click();
  await expect(page).toHaveURL(/\/problems\/B$/);
  await expect(language).toHaveValue('python3');
  await expect(editor).toContainText('sys.stdin');
  // One word for the draft state, and the cursor under the editor.
  await expect(page.getByText(/^(Saved|Saving|Not saved to the server, kept on this device)$/)).toBeVisible();
  await expect(page.getByText(/^Ln \d+, Col \d+$/)).toBeVisible();
});

test('student: running selects the Run tab and submitting selects Submissions; the run stays under Run (B2)', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
  const result = page.getByRole('region', { name: 'Result' });
  const tab = (name: string) => result.getByRole('tab', { name: new RegExp(`^${name}`) });
  await expect(result.getByRole('tab')).toHaveText(['Run', /^Submissions/, 'Your input']);

  const run = page.getByRole('button', { name: 'Run', exact: true }).first();
  await run.click();
  await expect.poll(() => api.runs.length).toBe(1);
  await expect(tab('Run')).toHaveAttribute('aria-selected', 'true');
  // Focus stays on the button pressed (P8).
  await expect(run).toBeFocused();
  api.stream.push({
    type: 'run',
    data: {
      id: 70,
      state: 'done',
      compile_output: '',
      results: [{ kind: 'sample', label: 'Example 1', verdict: 'WA', time_ms: 10, memory_kb: 3200, output: '4', expected: '3', message: '' }],
    },
  });
  await expect(result.getByRole('tabpanel', { name: 'Run' })).toContainText('The example failed', { timeout: 10_000 });

  const submit = page.getByRole('button', { name: 'Submit', exact: true });
  await submit.click();
  await expect.poll(() => api.submits.length).toBe(1);
  await expect(tab('Submissions')).toHaveAttribute('aria-selected', 'true');
  // The button now counts down ("Submit in 30 s") and keeps focus.
  await expect(page.getByRole('button', { name: /^Submit/ })).toBeFocused();
  // The run did not go anywhere: it is still under Run.
  await tab('Run').click();
  await expect(result.getByRole('tabpanel', { name: 'Run' })).toContainText('The example failed');
  // Your input holds the box with its own Run.
  await tab('Your input').click();
  await expect(result.getByRole('textbox', { name: 'Your input' })).toBeVisible();
  await expect(result.getByRole('tabpanel', { name: 'Your input' }).getByRole('button', { name: 'Run', exact: true })).toBeVisible();
});

test('student: standings open over problem B, Escape closes them, and B is still there (B3)', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/B');
  await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
  const open = page.getByRole('button', { name: 'Standings' });
  await open.click();
  const board = page.getByRole('dialog', { name: 'Standings' });
  await expect(board.getByRole('table')).toBeVisible();
  await expect(board.getByText(/are hidden until the end|Live\.|Final standings\./)).toBeVisible();
  const results = await new AxeBuilder({ page }).include('dialog[open]').withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze();
  expect(results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => v.id)).toEqual([]);
  await page.keyboard.press('Escape');
  await expect(board).toBeHidden();
  await expect(page).toHaveURL(/\/problems\/B$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/Interval covering/);
  await expect(open).toBeFocused();
});

test('student: a question can be sent in practice after the end (B3)', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'practice' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/B');
  await expect(codeEditor(page)).toHaveAttribute('data-language', /.+/);
  const open = page.getByRole('button', { name: /Messages/ });
  await open.click();
  const messages = page.getByRole('dialog', { name: 'Messages' });
  await messages.getByRole('textbox', { name: 'Question' }).fill('Why does my B time out?');
  await messages.getByRole('button', { name: 'Send question' }).click();
  await expect.poll(() => api.asked.length).toBe(1);
  await page.keyboard.press('Escape');
  await expect(open).toBeFocused();
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
  await page.getByRole('tab', { name: 'Your input' }).click();
  await page.getByRole('textbox', { name: 'Your input' }).fill('5 7');
  await page.getByRole('tabpanel', { name: 'Your input' }).getByRole('button', { name: 'Run', exact: true }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  expect(api.runs[0]).toMatchObject({ input: '5 7' });
  api.stream.push({
    type: 'run',
    data: {
      id: 70,
      state: 'done',
      compile_output: '',
      results: [{ kind: 'custom', label: 'Your input', verdict: 'OK', time_ms: 8, memory_kb: 3200, output: '12', expected: '', message: '' }],
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

  // Two more: "Dismiss all" clears both at once (S1-09.4).
  for (const [id, body] of [
    [3, 'Five minutes left.'],
    [4, 'Submissions close at the end.'],
  ] as const) {
    api.announcements.push({ id, body_md: body, created_at: Date.now() + id * 1000 });
    api.stream.push({ type: 'announcement', data: { contest: 4, id, body_md: body } });
  }
  await expect(page.getByText(/1 more message waiting/)).toBeVisible({ timeout: 10_000 });
  await page.getByRole('button', { name: 'Dismiss all' }).click();
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
  const created = await contests.mockApi(page, { who: 'organiser', phase: 'upcoming', published: false, publishOk: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title', { exact: true }).fill('Week 9');
  await page.getByLabel('Starts', { exact: true }).fill('2026-11-01T18:00');
  await page.getByLabel('Length in minutes', { exact: true }).fill('120');
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
