// Screens and flows for the integrity milestone (cache/v1/integrity-plan.md
// sections 5 and 7). Every /api/ call is answered by an in-test mock built
// from the API contract in section 7, one per page. Spans in the mocked
// pairs are UTF-8 byte offsets computed here with TextEncoder, and the
// sources carry multibyte text before and inside the spans, so a wrong
// conversion shows up as a wrong highlight.
//
// The sweep saves screenshots to cache/screenshots-integrity/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1, the
// title and no horizontal overflow. The flows cover running a check,
// filtering signals, the pair view, and the one decision per student that
// v3 keeps (V3-2, owner answer B: No action, Warning or Exclude, with a
// reason).
import { expect, test } from '@playwright/test';
import { shots, WIDTHS, THEMES, mockApi, settle, SCREENS } from './mocks/integrity';
import { toasts } from './locators';

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(screen.name, async ({ page }) => {
        await mockApi(page, screen.options);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        await settle(page);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page).toHaveTitle(screen.title);
          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

test('the check stays closed with its reason while the contest runs', async ({ page }) => {
  const api = await mockApi(page, { live: true, unchecked: true });
  await page.goto('/admin/contests/4/integrity');
  const check = page.getByRole('button', { name: 'Check integrity' });
  await expect(check).toBeDisabled();
  await expect(check).toHaveAccessibleDescription('Check integrity after the contest ends for everyone.');
  await expect(page.getByText('Signals are reasons to look, not proof.')).toBeVisible();
  await expect(page.getByText("No tool reliably tells AI-written code from a student's own", { exact: false })).toBeVisible();
  expect(api.runPosts).toBe(0);
});

test('a check runs, polls until done, and its signals filter by kind and strength', async ({ page }) => {
  const api = await mockApi(page, { unchecked: true });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Not checked yet.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Check integrity' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Checking the contest.' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Checking' })).toBeDisabled();
  await expect(page.getByText('Checked 30 students, 84 submissions and 2 problems.', { exact: false })).toBeVisible({ timeout: 10_000 });
  expect(api.runPosts).toBe(1);
  expect(api.runGets).toBeGreaterThanOrEqual(2);

  // Grouped by problem, in label order, with headers.
  const a = page.getByRole('table', { name: 'Signals on A. A plus B' });
  const b = page.getByRole('table', { name: 'Signals on B. Interval covering' });
  await expect(a.getByRole('columnheader', { name: 'Strength' })).toBeAttached();
  await expect(b.getByRole('rowheader', { name: 'Asha Rao and Ravi Kumar' })).toBeVisible();
  const strong = b.getByRole('row').filter({ hasText: 'Asha Rao and Ravi Kumar' });
  await expect(strong).toContainText('Similar code');
  // Strength in words with its reason; the numbers sit in the details (pilot P11).
  await expect(strong.getByRole('cell').nth(1)).toHaveText('Strong: Long programs, nearly identical');
  await expect(strong).toContainText('2 shared passages, both in C++17');
  await expect(strong).toContainText('41 of 44 fingerprints shared, shorter program 212 tokens, score 0.94');
  const short = a.getByRole('row').filter({ hasText: 'Kabir Shah and Neha Gupta' });
  await expect(short.getByRole('cell').nth(1)).toHaveText('Weak: Short programs, identical');
  await expect(short).toContainText('15 of 15 fingerprints shared, shorter program 48 tokens, score 1.00');
  await expect(page.getByText(/Weak \d/)).toHaveCount(0);
  await expect(page.getByText('Strength weighs how much code is shared as well as how alike it is', { exact: false }).first()).toBeVisible();
  await expect(b.getByRole('row').filter({ hasText: 'Neha Gupta' })).toContainText('Contains the expected output of tests 7 and 9');
  await expect(a.getByRole('row').filter({ hasText: 'Rohan Das' })).toContainText('8 different results from near-identical code over 14 attempts');

  // Clusters as names.
  await expect(page.getByRole('listitem').filter({ hasText: 'Asha Rao, Ravi Kumar and Zoë Fernandes' })).toBeVisible();
  await expect(page.getByRole('listitem').filter({ hasText: 'Meera Iyer and Sara Thomas' })).toBeVisible();

  await page.getByLabel('Kind').selectOption({ label: 'Same wrong output' });
  await expect(page.getByRole('table')).toHaveCount(2);
  await expect(page.getByRole('table', { name: 'Signals on B. Interval covering' })).toHaveCount(0);
  await page.getByLabel('Kind').selectOption({ label: 'All kinds' });
  await page.getByLabel('Strength').selectOption({ label: 'Weak' });
  await expect(page.getByRole('table', { name: 'Signals on A. A plus B' }).getByRole('row')).toHaveCount(3);
  await page.getByLabel('Strength').selectOption({ label: 'Strong' });
  await page.getByLabel('Kind').selectOption({ label: 'Probing' });
  await expect(page.getByText('No signals match these filters.')).toBeVisible();
});

test('the pair view tints the shared passages, marks them for screen readers and shows both timelines', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  await page.getByRole('link', { name: 'Compare code of Asha Rao and Ravi Kumar' }).click();
  await expect(page).toHaveURL(/\/integrity\/signals\/31$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Asha Rao and Ravi Kumar' })).toBeFocused();
  await expect(page.getByText('Similar code on problem B, Interval covering.', { exact: false })).toBeVisible();
  // F4: the trail names the contest and ends on this page.
  const trail = page.getByRole('navigation', { name: 'Breadcrumb' });
  await expect(trail.getByRole('listitem')).toHaveText(['Contests', 'Week 6 contest', 'Similar code', 'Asha Rao and Ravi Kumar']);
  await expect(trail.locator('[aria-current="page"]')).toHaveText('Asha Rao and Ravi Kumar');
  await expect(trail.getByRole('link', { name: 'Similar code' })).toHaveAttribute('href', '/admin/contests/4/integrity');
  await expect(page.getByText('Strength: strong, long programs, nearly identical.', { exact: false })).toBeVisible();
  await expect(page.getByText('41 of 44 fingerprints shared, shorter program 212 tokens, score 0.94. The score is the share', { exact: false })).toBeVisible();

  const asha = page.getByRole('region', { name: /^Asha Rao's program, 12 lines, 2 shared passages$/ });
  const ravi = page.getByRole('region', { name: /^Ravi Kumar's program, 12 lines, 2 shared passages$/ });
  await expect(asha).toBeVisible();
  await expect(ravi).toBeVisible();
  // The multibyte comment before the spans does not shift them.
  const marks = await asha.locator('mark').allTextContents();
  expect(marks.join('|')).toBe(
    [
      'vector<pair<int,int>> v(n);',
      '    for (auto &p : v) cin >> p.second >> p.first;',
      'sort(v.begin(), v.end());',
      '    int last = INT_MIN, count = 0;',
      '    for (auto &p : v) if (p.second > last) { last = p.first; count++; }',
    ].join('|'),
  );
  await expect(asha.getByText('Shared passage starts.')).toHaveCount(2);
  await expect(asha.getByText('Shared passage ends.')).toHaveCount(2);
  await expect(asha).toContainText('// अंतराल: sort by the right end 🙂');
  await expect(page.getByText('C++17, submitted', { exact: false }).first()).toBeVisible();

  // The well takes keyboard focus so long lines can scroll.
  await asha.focus();
  await expect(asha).toBeFocused();

  await expect(page.getByRole('list', { name: 'Asha Rao' })).toContainText('Pasted 431 characters.');
  await expect(page.getByRole('list', { name: 'Ravi Kumar' })).toContainText('Left the page.');

});

test('a decision about one student is recorded with a reason and listed on the integrity page', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/contests/4/integrity/signals/31');
  const record = page.getByRole('button', { name: 'Record a decision about Asha Rao' });
  await record.click();
  const dialog = page.getByRole('dialog', { name: 'Record a decision about Asha Rao' });
  await dialog.getByLabel('Exclude from this contest').check();
  await dialog.getByRole('button', { name: 'Record the decision' }).click();
  await expect(dialog.getByLabel('Reason', { exact: true })).toBeFocused();
  expect(api.decisionPuts).toHaveLength(0);
  await dialog.getByLabel('Reason', { exact: true }).fill('Copied the whole loop from Ravi during the contest; he showed his screen.');
  await dialog.getByRole('button', { name: 'Record the decision' }).click();
  await expect(dialog).toBeHidden();
  expect(api.decisionPuts.at(-1)).toMatchObject({ user_id: 7, decision: 'excluded' });

  await page.goto('/admin/contests/4/integrity');
  const decisions = page.getByRole('region', { name: 'Decisions' });
  await expect(decisions.getByRole('row', { name: /Asha Rao/ })).toContainText('Exclude from this contest');
  await expect(decisions.getByRole('row', { name: /Meera Iyer/ })).toContainText('Warning');
});

test('students cannot open the organiser screens', async ({ page }) => {
  await mockApi(page, { who: 'student' });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Only organisers can manage contests.', { exact: false })).toBeVisible();
});

test('Similar code on Results leads to the integrity page, which has no tab of its own (V3-16)', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link', { name: 'Integrity' })).toHaveCount(0);
  await expect(nav.locator('[aria-current="page"]')).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).locator('[aria-current="page"]')).toHaveText('Similar code');
  await expect(page).toHaveTitle('Similar code · Week 6 contest · podium');
  await expect(nav.getByRole('link', { name: 'Results' })).toHaveAttribute('href', '/admin/contests/4/results');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await nav.getByRole('link', { name: 'Results' }).click();
  await page.getByRole('link', { name: 'Similar code' }).click();
  await expect(page).toHaveURL(/\/admin\/contests\/4\/integrity$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Week 6 contest' })).toBeFocused();
});

test('checking again asks first, and the pair view puts the decision after the timelines (P3 C3, C7)', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const again = page.getByRole('button', { name: 'Check again' });
  await again.click();
  const dialog = page.getByRole('dialog', { name: 'Check again?' });
  await expect(dialog).toContainText('Decisions already recorded stay.');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(api.runPosts).toBe(0);
  await again.click();
  await dialog.getByRole('button', { name: 'Check again' }).click();
  await expect.poll(() => api.runPosts).toBe(1);
  // Wait for the new check to finish before leaving: while it runs it has no
  // signals, so going to a signal too early lands on "Signal not found".
  await expect(toasts(page).getByText('The check finished', { exact: true })).toBeVisible();

  await page.goto('/admin/contests/4/integrity/signals/31');
  const timelines = await page.getByRole('heading', { name: 'Timelines' }).boundingBox();
  const open = await page.getByRole('button', { name: /^Record a decision about/ }).first().boundingBox();
  expect(open!.y).toBeGreaterThan(timelines!.y);
});
