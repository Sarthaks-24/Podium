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
// filtering signals, the pair view, opening, sharing and deciding a case,
// and the student's notice, evidence and explanation.
import { expect, test } from '@playwright/test';
import { activate, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, mockApi, settle, SCREENS } from './mocks/integrity';

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
  const api = await mockApi(page, { live: true, unchecked: true, window: true });
  await page.goto('/admin/contests/4/integrity');
  const check = page.getByRole('button', { name: 'Check integrity' });
  await expect(check).toBeDisabled();
  await expect(check).toHaveAccessibleDescription('Check integrity after the contest ends for everyone.');
  await expect(page.getByText('it leaks by design', { exact: false })).toBeVisible();
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
  await expect(strong).toContainText('Strong 0.94');
  await expect(strong).toContainText('2 shared passages, both in C++17');
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
  await expect(page.getByRole('table', { name: 'Signals on A. A plus B' }).getByRole('row')).toHaveCount(2);
  await page.getByLabel('Strength').selectOption({ label: 'Strong' });
  await page.getByLabel('Kind').selectOption({ label: 'Probing' });
  await expect(page.getByText('No signals match these filters.')).toBeVisible();
});

test('signals open a case with a title, a counted reason and the chosen signals', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const open = page.getByRole('button', { name: 'Open a case' });
  await expect(open).toBeDisabled();
  await expect(open).toHaveAccessibleDescription('Choose one or more signals first.');

  // The checkbox is reachable and toggled from the keyboard.
  const first = page.getByRole('checkbox', { name: 'Choose similar code, Asha Rao and Ravi Kumar' });
  await first.focus();
  await page.keyboard.press('Space');
  await expect(first).toBeChecked();
  await page.getByRole('checkbox', { name: 'Choose hard-coded answers, Neha Gupta' }).check();
  await expect(page.getByText('2 signals chosen.')).toBeVisible();
  await open.click();

  const dialog = page.getByRole('dialog', { name: 'Open a case' });
  await expect(dialog.getByLabel('Title')).toHaveValue('Several signals on B');
  await expect(dialog.getByRole('list', { name: '2 signals' }).getByRole('listitem')).toHaveCount(2);
  await dialog.getByLabel('Reason').fill('Too short');
  await expect(dialog.getByText('9 of at least 20 characters')).toBeVisible();
  await dialog.getByRole('button', { name: 'Open the case' }).click();
  await expect(dialog.getByText('Write a reason of at least 20 characters.')).toBeVisible();
  expect(api.caseBodies).toHaveLength(0);

  await dialog.getByLabel('Reason').fill('The greedy loop is the same line for line in both programs.');
  await expect(dialog.getByText('59 characters')).toBeVisible();
  await dialog.getByRole('button', { name: 'Open the case' }).click();
  await expect(page).toHaveURL(/\/admin\/integrity\/cases\/3$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Several signals on B' })).toBeFocused();
  expect(api.caseBodies).toEqual([
    { title: 'Several signals on B', note: 'The greedy loop is the same line for line in both programs.', signal_ids: [31, 34] },
  ]);
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
  await expect(trail.getByRole('listitem')).toHaveText(['Contests', 'Week 6 contest', 'Integrity', 'Asha Rao and Ravi Kumar']);
  await expect(trail.locator('[aria-current="page"]')).toHaveText('Asha Rao and Ravi Kumar');
  await expect(trail.getByRole('link', { name: 'Integrity' })).toHaveAttribute('href', '/admin/contests/4/integrity');

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

  await page.getByRole('button', { name: 'Open a case' }).click();
  const dialog = page.getByRole('dialog', { name: 'Open a case' });
  await expect(dialog.getByLabel('Title')).toHaveValue('Similar code on B');
  await expect(dialog.getByRole('list', { name: 'Signal' })).toContainText('B. Interval covering: Similar code, Asha Rao and Ravi Kumar, strong 0.94');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('button', { name: 'Open a case' })).toBeFocused();
});

test("a case is shared with a note, an outcome waits for the students' turn, and a proposal waits for a second reviewer", async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/integrity/cases/3');
  await expect(page.getByRole('heading', { level: 1, name: 'Similar code on B: Asha Rao and Ravi Kumar' })).toBeVisible();
  const trail = page.getByRole('navigation', { name: 'Breadcrumb' });
  await expect(trail.getByRole('link', { name: 'Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4');
  await expect(trail.getByRole('link', { name: 'Integrity' })).toHaveAttribute('href', '/admin/contests/4/integrity');
  const students = page.getByRole('table', { name: 'Students in this case' });
  await expect(students.getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('Not shared yet');
  await expect(page.getByRole('table', { name: 'Evidence in this case' })).toContainText('B. Interval covering');

  const action = page.getByRole('button', { name: 'Propose an outcome' });
  await expect(action).toBeDisabled();
  await expect(action).toHaveAccessibleDescription('An outcome can be proposed once the case is shared with every student, so they can explain first.');

  // A note is required before anything changes.
  await page.getByRole('button', { name: 'Share with the students' }).click();
  await expect(page.getByText('Write a note of at least 20 characters for the log first.')).toBeVisible();
  await expect(page.getByLabel('Note for the log')).toBeFocused();
  expect(api.puts).toHaveLength(0);

  await page.getByLabel('Note for the log').fill('Asking both students how they wrote the loop.');
  await page.getByRole('button', { name: 'Share with the students' }).click();
  const confirmShare = page.getByRole('dialog', { name: 'Share with the students?' });
  await expect(confirmShare).toContainText("never another student's name or code");
  await expect(confirmShare.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmShare.getByRole('button', { name: 'Share with the students' }).click();
  await expect(page.locator('.page-header .state-word')).toHaveText('Shared with the students');
  await expect(students.getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('Shared');
  await expect(students.getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('Can explain until');
  expect(api.puts).toEqual([{ state: 'awaiting_student', note: 'Asking both students how they wrote the loop.', seen_explanations: {} }]);
  await expect(page.getByLabel('Note for the log')).toHaveValue('');

  // Shared is not enough: each student explains first, or their 7 days run out.
  await expect(action).toBeDisabled();
  await expect(action).toHaveAccessibleDescription(/^An outcome can be proposed when every student has explained since the case was shared, or when their 7 days to explain end on /);

  // Both students explain while the page is open; Asha writes twice.
  api.explain(7, 'I wrote it in the lab from the lecture notes.');
  api.explain(8, 'A friend shared their notes with me before the contest.');
  await page.waitForTimeout(5);
  api.explain(7, 'I wrote it in the lab from the lecture notes, then fixed the loop.');
  await page.getByLabel('Note for the log').fill('Both explanations describe the same shared notes from a friend.');
  await page.getByRole('button', { name: 'Clear the case' }).click();
  await expect(page.getByText('A student changed their explanation. Reload before deciding.')).toBeVisible();
  await page.getByRole('button', { name: 'Reload the case' }).click();
  await expect(page.getByText('A student changed their explanation.')).toHaveCount(0);
  // Every version is shown with its time; the note is kept.
  const versions = page.getByRole('list', { name: "Asha Rao's explanations, oldest first" });
  await expect(versions.getByRole('listitem')).toHaveCount(2);
  await expect(versions.getByRole('listitem').first()).toContainText('Version 1, written');
  await expect(versions.getByRole('listitem').last()).toContainText('Latest version, written');
  await expect(versions).toContainText('then fixed the loop.');
  await expect(page.getByLabel('Note for the log')).toHaveValue('Both explanations describe the same shared notes from a friend.');

  // Every student has now replied: an outcome can be proposed, never set by one person.
  await expect(action).toBeEnabled();
  await action.click();
  const form = page.getByRole('form', { name: 'Propose an outcome' });
  await expect(form.getByRole('heading', { name: 'Propose an outcome' })).toBeFocused();
  await form.getByRole('button', { name: 'Propose the outcome' }).click();
  await expect(form.getByText('Choose an outcome.')).toBeVisible();
  await expect(form.getByText('Write the reason the student reads first.')).toBeVisible();
  await form.getByLabel(/Zero for one problem/).check();
  await form.getByRole('button', { name: 'Propose the outcome' }).click();
  await expect(form.getByText('Choose the problem that scores zero.')).toBeVisible();
  await form.getByLabel('Problem', { exact: true }).selectOption('B');
  await form.getByLabel('Ravi Kumar').uncheck();
  await form.getByLabel('Reason, for the student').fill('Your program for problem B shares its whole loop with another submission.');
  expect(api.proposals).toEqual([]);
  await form.getByRole('button', { name: 'Propose the outcome' }).click();
  await expect(page.getByRole('group', { name: 'Waiting for a second reviewer' })).toContainText('You proposed zero for problem B for Asha Rao');
  await expect(page.getByText('You proposed this. A second reviewer who did not open the case confirms it.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Confirm the outcome' })).toHaveCount(0);
  expect(api.proposals).toHaveLength(1);
  const proposal = api.proposals[0] as { outcome: string; problem_label: string; user_ids: number[]; note: string; seen_explanations: Record<string, number> };
  expect(proposal).toMatchObject({ outcome: 'zero_problem', problem_label: 'B', user_ids: [7], note: 'Both explanations describe the same shared notes from a friend.' });
  expect(Object.keys(proposal.seen_explanations).sort()).toEqual(['7', '8']);
  // The old one-person action is gone: no PUT with actioned was sent.
  expect(api.puts.some((b) => b.state === 'actioned')).toBe(false);
  await page.screenshot({ path: `${shots()}integrity-case-proposed-1440-light.png`, fullPage: true });
});

test('a second reviewer confirms a proposal, and the log records the outcome', async ({ page }) => {
  const api = await mockApi(page, { proposed: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/integrity/cases/3');
  const decide = page.getByRole('complementary', { name: 'Decision' });
  await expect(decide).toContainText('Policy: referral suggested.');
  await expect(decide).toContainText('Asha Rao replies by');
  await expect(decide).toContainText('Decide by');
  const pending = page.getByRole('group', { name: 'Waiting for a second reviewer' });
  await expect(pending).toContainText('Priya Nair proposed zero for problem B for Asha Rao');
  await expect(pending).toContainText('You did not open this case or propose this outcome, so you can confirm it.');
  await expect(page.getByRole('table', { name: 'Students in this case' }).getByRole('row').filter({ hasText: 'Asha Rao' })).toContainText('1 earlier outcome');
  await page.screenshot({ path: `${shots()}integrity-case-confirm-1440-light.png`, fullPage: true });

  await page.getByRole('button', { name: 'Confirm the outcome' }).click();
  await expect(page.getByText('Write a note of at least 20 characters for the log first.')).toBeVisible();
  await page.getByLabel('Note for the log').fill('Agreed: the loop matches line for line, renamed.');
  await page.getByRole('button', { name: 'Confirm the outcome' }).click();
  const sure = page.getByRole('dialog', { name: 'Confirm zero for problem B for Asha Rao?' });
  await expect(sure).toContainText('Problem B scores zero on the board from now on, and the ranks follow.');
  await expect(sure).toContainText('The other students in the case are cleared.');
  await sure.getByRole('button', { name: 'Confirm the outcome' }).click();
  await expect(page.locator('.page-header .state-word')).toHaveText('Action taken');
  await expect(page.getByText('This case closed with a confirmed outcome. An outcome is changed only through an appeal.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reopen the case' })).toHaveCount(0);
  await expect(page.locator('.log li').last()).toContainText('Outcome confirmed: Zero for problem B for Asha Rao, by you');
  expect(api.confirms).toEqual([{ id: 4, note: 'Agreed: the loop matches line for line, renamed.', seen_explanations: { '7': expect.any(Number), '8': expect.any(Number) } }]);
});

test('a reviewer who loses the race to propose sees the other proposal at once (C2-025)', async ({ page }) => {
  await mockApi(page, { raceProposal: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/integrity/cases/3');
  await page.getByRole('button', { name: 'Propose an outcome' }).click();
  const form = page.getByRole('form', { name: 'Propose an outcome' });
  await form.getByLabel(/Zero for one problem/).check();
  await form.getByLabel('Problem', { exact: true }).selectOption('B');
  await form.getByLabel('Reason, for the student').fill('Your program for problem B shares its whole loop with another submission.');
  await page.getByLabel('Note for the log').fill('The loops match line for line in both programs.');
  await form.getByRole('button', { name: 'Propose the outcome' }).click();
  // The refusal stays, the case is read again: the other proposal and its Confirm button show, the form goes.
  await expect(page.getByText('A proposal is waiting for a second reviewer. Confirm it, or clear or reopen the case first.')).toBeVisible();
  await expect(page.getByRole('group', { name: 'Waiting for a second reviewer' })).toContainText('Priya Nair proposed zero for problem B for Asha Rao');
  await expect(page.getByRole('button', { name: 'Confirm the outcome' })).toBeVisible();
  await expect(page.getByRole('form', { name: 'Propose an outcome' })).toHaveCount(0);
  await expect(page.getByLabel('Note for the log')).toHaveValue('The loops match line for line in both programs.');
});

test('an appeal on a case is ruled on by a reviewer who took no part', async ({ page }) => {
  const api = await mockApi(page, { appealed: true });
  await page.goto('/admin/integrity/cases/3');
  const appeal = page.getByRole('article', { name: 'Appeal by Asha Rao against zero for problem B' });
  await appeal.getByLabel(/The decision stands/).check();
  await appeal.getByLabel('Reason for the ruling, for the student').fill('The slides give a different loop; yours matches another submission.');
  await appeal.getByRole('button', { name: 'Rule on the appeal' }).click();
  await expect(appeal).toContainText('The decision stands, by Arjun Mehta');
  expect(api.rulings).toEqual([{ id: 66, upheld: false, reason: 'The slides give a different loop; yours matches another submission.' }]);
});

test('a reviewer who took part cannot rule on the appeal', async ({ page }) => {
  await mockApi(page, { appealed: true, involved: true });
  await page.goto('/admin/integrity/cases/3');
  await expect(page.getByText('You took part in this decision, so another owner or admin rules on the appeal.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Rule on the appeal' })).toHaveCount(0);
});

test("a case's evidence opens the case's own copy, whatever a re-run changed", async ({ page }) => {
  const api = await mockApi(page, { rerun: true });
  await page.goto('/admin/integrity/cases/3');
  const evidence = page.getByRole('table', { name: 'Evidence in this case' });
  await expect(evidence).toContainText('41 of 44 fingerprints shared, shorter program 212 tokens');
  await expect(page.getByText('The score is the share of the shorter program found in the other, not a probability.')).toBeVisible();
  const link = evidence.getByRole('link', { name: 'Compare code of Asha Rao and Ravi Kumar' });
  await expect(link).toHaveAttribute('href', '/admin/contests/4/integrity/signals/31?case=3&evidence=0');
  await link.click();
  await expect(page.getByRole('heading', { level: 1, name: 'Asha Rao and Ravi Kumar' })).toBeFocused();
  await expect(page.getByRole('link', { name: 'Similar code on B: Asha Rao and Ravi Kumar' })).toHaveAttribute('href', '/admin/integrity/cases/3');
  await expect(page.getByRole('region', { name: /^Asha Rao's program, 12 lines, 2 shared passages$/ })).toBeVisible();
  await expect(page.getByRole('region', { name: /^Ravi Kumar's program, 12 lines, 2 shared passages$/ })).toBeVisible();
  await expect(page.getByText('Evidence as it was when the case opened', { exact: false })).toBeVisible();
  await expect(page.getByText('41 of 44 fingerprints shared, shorter program 212 tokens', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open a case' })).toHaveCount(0);
  // Rohan and Sara now hold live signal 31; their code never appears here.
  await expect(page.locator('main')).not.toContainText('return 13;');
  expect(api.casePairGets).toBe(1);
  expect(api.livePairGets).toBe(0);
});

test('a student sees a quiet notice, their own evidence, and saves an explanation', async ({ page }) => {
  const api = await mockApi(page, { who: 'student' });
  await page.goto('/');
  const notice = page.getByText('An organiser asked you to explain a submission.');
  await expect(notice).toBeVisible();
  await page.getByRole('link', { name: 'Write your explanation' }).click();
  await expect(page).toHaveURL(/\/integrity$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Questions and standing' })).toBeFocused();
  await expect(page.getByText('Waiting for your explanation. Nothing has been decided.')).toBeVisible();
  // P4: each question has its own page.
  await page.getByRole('link', { name: 'Open the question' }).click();
  await expect(page).toHaveURL(/\/integrity\/cases\/3$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeFocused();
  await expect(page.getByText("Your submission shares long passages with another student's submission.", { exact: false })).toBeVisible();
  // Never another student's name, never a strength or a signal word.
  await expect(page.locator('main')).not.toContainText('Ravi Kumar');
  await expect(page.locator('main')).not.toContainText(/strong|signal/i);

  const code = page.getByRole('region', { name: /^Your program for B\. Interval covering/ });
  await expect(code.locator('mark')).toHaveCount(5);
  await expect(code.locator('mark').first()).toHaveText('vector<pair<int,int>> v(n);');

  await page.getByRole('button', { name: 'Save explanation' }).click();
  await expect(page.getByText('Write your explanation first.')).toBeVisible();
  await page.getByLabel('Explain how you wrote this, in your own words').fill('I wrote it in the lab after the lecture on greedy algorithms.');
  await page.getByRole('button', { name: 'Save explanation' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved. You can change it until a decision is made.' })).toBeVisible();
  expect(api.explanations).toEqual([{ explanation: 'I wrote it in the lab after the lecture on greedy algorithms.' }]);
  await expect(page.getByText('Your explanation is saved.', { exact: false })).toBeVisible();
});

test('a decided case reads only for the student', async ({ page }) => {
  await mockApi(page, { who: 'student', decided: true });
  await page.goto('/integrity');
  await expect(page.getByText('Decided: cleared. No action will be taken.')).toBeVisible();
  await page.getByRole('link', { name: 'Open the question' }).click();
  await expect(page.getByText('Decided: cleared. No action will be taken.')).toBeVisible();
  await expect(page.getByText('I wrote it in the lab from the lecture notes.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save explanation' })).toHaveCount(0);
  await expect(page.getByText('An organiser has decided, so you can no longer change your explanation.')).toBeVisible();
});

test('a student with no cases gets no notice', async ({ page }) => {
  await mockApi(page, { who: 'student', noCases: true });
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Week 6 contest' })).toBeVisible();
  await expect(page.getByText('An organiser asked you to explain a submission.')).toHaveCount(0);
  await page.goto('/integrity');
  await expect(page.getByText('No questions. If an organiser asks you about a submission', { exact: false })).toBeVisible();
});

test('students cannot open the organiser screens', async ({ page }) => {
  await mockApi(page, { who: 'student' });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Only organisers can review integrity.', { exact: false })).toBeVisible();
  await page.goto('/admin/integrity/cases/3');
  await expect(page.getByText('Only organisers can review integrity.', { exact: false })).toBeVisible();
});

test('results and the contest nav lead to the integrity page', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link', { name: 'Integrity' })).toHaveAttribute('aria-current', 'page');
  await expect(nav.getByRole('link', { name: 'Results' })).toHaveAttribute('href', '/admin/contests/4/results');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await nav.getByRole('link', { name: 'Results' }).click();
  await page.getByRole('link', { name: 'Check integrity' }).click();
  await expect(page).toHaveURL(/\/admin\/contests\/4\/integrity$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Week 6 contest' })).toBeFocused();
});

test('keyboard only: an organiser checks a contest, opens a case, and a student explains', async ({ page }) => {
  const api = await mockApi(page, { unchecked: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/integrity');
  await expect(page.getByText('Not checked yet.', { exact: false })).toBeVisible();

  // The button keeps focus while the check runs.
  const check = page.getByRole('button', { name: 'Check integrity' });
  await activate(page, check);
  await expect(page.getByRole('button', { name: 'Checking' })).toBeFocused();
  await expect(page.getByText('Checked 30 students, 84 submissions and 2 problems.', { exact: false })).toBeVisible({ timeout: 10_000 });

  // Choose a signal with Space, open the case dialog, and a short reason takes focus back.
  await activate(page, page.getByRole('checkbox', { name: 'Choose similar code, Asha Rao and Ravi Kumar' }), 'Space');
  await activate(page, page.getByRole('button', { name: 'Open a case' }));
  const dialog = page.getByRole('dialog', { name: 'Open a case' });
  await expect(dialog).toBeVisible();
  await tabTo(page, dialog.getByLabel('Reason'));
  await page.keyboard.type('Too short');
  await activate(page, dialog.getByRole('button', { name: 'Open the case' }));
  await expect(dialog.getByLabel('Reason')).toBeFocused();
  await page.keyboard.type(', the greedy loop is the same line for line.');
  await activate(page, dialog.getByRole('button', { name: 'Open the case' }));
  await expect(page).toHaveURL(/\/admin\/integrity\/cases\/3$/);
  expect(api.caseBodies).toHaveLength(1);
});

test('keyboard only: a student reads the integrity question and saves an explanation', async ({ page }) => {
  const api = await mockApi(page, { who: 'student' });
  await page.goto('/');
  await activate(page, page.getByRole('link', { name: 'Write your explanation' }));
  await expect(page.getByRole('heading', { level: 1, name: 'Questions and standing' })).toBeFocused();
  await expect(page.getByText('This is not a finding', { exact: false })).toBeVisible();
  await activate(page, page.getByRole('link', { name: 'Open the question' }));
  await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeFocused();

  const field = page.getByLabel('Explain how you wrote this, in your own words');
  await activate(page, page.getByRole('button', { name: 'Save explanation' }));
  await expect(page.getByRole('alert').filter({ hasText: 'Write your explanation first.' })).toBeVisible();
  await tabTo(page, field, { back: true });
  await page.keyboard.type('I wrote it in the lab after the lecture on greedy algorithms.');
  await activate(page, page.getByRole('button', { name: 'Save explanation' }));
  await expect(page.getByRole('status').filter({ hasText: 'Saved. You can change it until a decision is made.' })).toBeVisible();
  expect(api.explanations).toHaveLength(1);
});

test('checking again asks first, and the pair view puts Open a case after the timelines (P3 C3, C7)', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/admin/contests/4/integrity');
  const again = page.getByRole('button', { name: 'Check again' });
  await again.click();
  const dialog = page.getByRole('dialog', { name: 'Check again?' });
  await expect(dialog).toContainText('Cases already open keep their own copy of the evidence.');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(api.runPosts).toBe(0);
  await again.click();
  await dialog.getByRole('button', { name: 'Check again' }).click();
  await expect.poll(() => api.runPosts).toBe(1);

  await page.goto('/admin/contests/4/integrity/signals/31');
  const timelines = await page.getByRole('heading', { name: 'Timelines' }).boundingBox();
  const open = await page.getByRole('button', { name: 'Open a case' }).boundingBox();
  expect(open!.y).toBeGreaterThan(timelines!.y);
});
