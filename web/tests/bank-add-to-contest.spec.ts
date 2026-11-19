// Add to contest from the Problems page (v2.1.1 stage O): checkboxes on the
// problems a contest can take, the bar with the contest select, the question
// asked before a running contest changes, and the refusal texts. Answered by
// the mock of the admin problems API (tests/mocks/problems.ts); the add route
// is answered here so each test chooses what the server says.
import { expect, test, type Page } from '@playwright/test';
import { apiError, json, mockApi, problemFixtures, summaryOf, T0, THEMES, WIDTHS, type Json } from './mocks/problems';

interface Seen {
  adds: { id: number; body: Json }[];
}

/** Opens the bank with `answer` as the server's reply to an add; `contests` replaces the mock's contest list when given. */
async function openBank(page: Page, answer: (body: { problem_ids: number[] }) => Json, contests?: Json[]): Promise<Seen> {
  await mockApi(page);
  const seen: Seen = { adds: [] };
  // Two problems have passed validation here: A plus B and Interval covering.
  const list = Object.values(problemFixtures()).filter((p) => !p.archived).map((p) => summaryOf(p.id === 2 ? { ...p, validated: true, validated_at: T0 } : p));
  await page.route('**/api/admin/problems', (route) => (route.request().method() === 'GET' ? json(route, 200, { problems: list }) : route.fallback()));
  if (contests) {
    await page.route('**/api/admin/contests', (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      return json(route, 200, { contests });
    });
  }
  await page.route('**/api/admin/contests/*/problems/add', (route) => {
    const body = route.request().postDataJSON() as { problem_ids: number[] };
    const id = Number(new URL(route.request().url()).pathname.split('/')[4]);
    seen.adds.push({ id, body });
    return json(route, 200, answer(body));
  });
  await page.goto('/admin/problems');
  await expect(page.getByRole('heading', { level: 1, name: 'Problems' })).toBeVisible();
  return seen;
}

const live = (): Json => ({
  id: 9, slug: 'live-now', title: 'Live now', starts_at: T0 - 3600000, ends_at: T0 + 3600000, state: 'live', published: true, archived: false, problems: [],
});
const draft = (): Json => ({
  id: 8, slug: 'week-8', title: 'Week 8', starts_at: T0 + 14 * 86400000, ends_at: T0 + 14 * 86400000 + 7200000, state: 'upcoming', published: false, archived: false, problems: [],
});

test('only validated, unarchived problems get a checkbox', async ({ page }) => {
  await openBank(page, () => ({ added: [], refused: [] }));
  await expect(page.getByRole('checkbox', { name: 'Select A plus B' })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Select Interval covering' })).toBeVisible();
  // Average speed has not passed validation, so it cannot be added yet.
  await expect(page.getByRole('link', { name: 'Average speed', exact: true })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Select Average speed' })).toHaveCount(0);
  await expect(page.getByRole('group', { name: 'Add the selected problems to a contest' })).toHaveCount(0);
});

test('an add shows what went in and why the rest did not, by title', async ({ page }) => {
  const seen = await openBank(
    page,
    () => ({ added: [{ problem_id: 1, label: 'C' }], refused: [{ problem_id: 2, reason: 'needs_ai_reason' }] }),
    [draft()],
  );
  await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
  await page.getByRole('checkbox', { name: 'Select Interval covering' }).check();
  const bar = page.getByRole('group', { name: 'Add the selected problems to a contest' });
  await expect(bar.getByText('2 selected')).toBeVisible();
  await expect(bar.getByLabel('Contest', { exact: true })).toHaveValue('8');
  await bar.getByRole('button', { name: 'Add to contest' }).click();
  expect(seen.adds).toEqual([{ id: 8, body: { problem_ids: expect.arrayContaining([1, 2]) } }]);
  const note = page.getByRole('status').filter({ hasText: 'Added to Week 8' });
  await expect(note).toContainText('A plus B as C');
  await expect(note).toContainText('Interval covering: Add it from the contest editor, where you can give the required reason');
  await expect(note.getByRole('link', { name: 'Open the contest editor' })).toHaveAttribute('href', '/admin/contests/8');
  // The added problem is unticked; the refused one stays ticked so it can be tried again.
  await expect(page.getByRole('checkbox', { name: 'Select A plus B' })).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Select Interval covering' })).toBeChecked();
});

test('each refusal code has its own text', async ({ page }) => {
  const codes: [string, string][] = [
    ['already_added', 'Already in this contest'],
    ['not_found', 'No longer available'],
    ['deleted', 'Deleted'],
    ['not_validated', 'Validate it first'],
    ['being_checked', 'Its languages are being checked. Try again when that finishes'],
    ['ai_draft', 'Read it and save it in the editor first'],
    ['limit', 'The contest already has 26 problems'],
  ];
  for (const [code, text] of codes) {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await openBank(page, () => ({ added: [], refused: [{ problem_id: 1, reason: code }] }), [draft()]);
    await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
    await page.getByRole('button', { name: 'Add to contest' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Not added' })).toContainText(`A plus B: ${text}`);
  }
});

test('a running contest asks first and Cancel sends nothing', async ({ page }) => {
  const seen = await openBank(page, () => ({ added: [{ problem_id: 1, label: 'A' }], refused: [] }), [live(), draft()]);
  await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
  await expect(page.getByLabel('Contest', { exact: true })).toHaveValue('9');
  await page.getByRole('button', { name: 'Add to contest' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add to Live now?' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Students see the new problem as soon as it is added.');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  expect(seen.adds).toHaveLength(0);
  await page.getByRole('button', { name: 'Add to contest' }).click();
  await page.getByRole('dialog', { name: 'Add to Live now?' }).getByRole('button', { name: 'Add to contest' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Added to Live now' })).toContainText('A plus B as A');
  expect(seen.adds).toHaveLength(1);
});

test('a draft contest is added to without a question', async ({ page }) => {
  const seen = await openBank(page, () => ({ added: [{ problem_id: 1, label: 'A' }], refused: [] }), [draft()]);
  await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
  await page.getByRole('button', { name: 'Add to contest' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect.poll(() => seen.adds.length).toBe(1);
});

test('an error from the server is shown and nothing changes on the page', async ({ page }) => {
  await openBank(page, () => ({ added: [], refused: [] }), [draft()]);
  await page.route('**/api/admin/contests/*/problems/add', (route) => apiError(route, 400, 'too_many_problems', 'Add at most 26 problems at a time.'));
  await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
  await page.getByRole('button', { name: 'Add to contest' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Add at most 26 problems at a time.' })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Select A plus B' })).toBeChecked();
});

test('the whole flow works from the keyboard', async ({ page }) => {
  const seen = await openBank(page, () => ({ added: [{ problem_id: 1, label: 'A' }], refused: [] }), [draft()]);
  const box = page.getByRole('checkbox', { name: 'Select A plus B' });
  await box.focus();
  await page.keyboard.press('Space');
  await expect(box).toBeChecked();
  await expect(page.getByLabel('Contest', { exact: true })).toBeVisible();
  const add = page.getByRole('button', { name: 'Add to contest' });
  await add.focus();
  await page.keyboard.press('Enter');
  await expect.poll(() => seen.adds.length).toBe(1);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const width of WIDTHS) {
      test(`the bar and the result fit at ${width}px`, async ({ page }) => {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await openBank(
          page,
          () => ({ added: [{ problem_id: 1, label: 'A' }], refused: [{ problem_id: 2, reason: 'being_checked' }] }),
          [live(), draft()],
        );
        await page.getByRole('checkbox', { name: 'Select A plus B' }).check();
        await page.getByRole('checkbox', { name: 'Select Interval covering' }).check();
        await page.getByLabel('Contest', { exact: true }).selectOption('8');
        await page.getByRole('button', { name: 'Add to contest' }).click();
        await expect(page.getByRole('status').filter({ hasText: 'Not added' })).toBeVisible();
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow).toBeLessThanOrEqual(0);
      });
    }
  });
}
