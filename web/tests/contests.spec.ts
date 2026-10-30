// Screens and flows for M4 contests (cache/v1/m4-plan.md sections 4 and 5).
// Every /api/ call is answered by an in-test mock built from the shapes in
// the plan as amended by cache/v1/m4-api-deviations.md, one per page; contest times are relative to the moment the
// mock is installed, and every response's server_now is the real clock, so
// countdowns read as they would live. Events go through the long-lived
// EventSource stand-in in tests/sse.ts.
//
// The sweep saves screenshots to cache/screenshots-m4/<screen>-<width>-<theme>.png
// at 375, 768, 1024 and 1440 px in light and dark, and checks one h1, the
// title and no horizontal overflow. The flows cover the student before,
// during and after a contest, a result the server hides, the frozen board,
// the editor, the console and the reveal.
import { expect, test } from '@playwright/test';
import { activate, hasFocus, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, submission, hidden, mockApi, settle, SCREENS, freezeClock } from './mocks/contests';
import { codeEditor, contestBanner, feedRows, saveStatus, toasts } from './locators';

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

test('a second live contest opens from a secondary button: one primary per view (C1-031)', async ({ page }) => {
  await mockApi(page, { phase: 'during', secondLive: true });
  await page.goto('/');
  const row = page.locator('tbody tr').filter({ hasText: 'Lab 2' });
  await expect(row.getByRole('link', { name: 'Open contest' })).toHaveClass(/btn-secondary/);
  await expect(page.locator('.btn-primary')).toHaveCount(1);
});

test('the home features the contest the student took part in, not a newer one they missed (D324)', async ({ page }) => {
  await mockApi(page, { phase: 'after', missedNewest: true });
  await page.goto('/');
  const pinned = page.getByRole('region', { name: 'Week 5 contest' });
  await expect(pinned).toContainText('240 of 300');
  await expect(pinned.getByRole('link', { name: 'Open contest' })).toHaveAttribute('href', '/contests/week-5');
  await expect(page.getByRole('heading', { name: 'Other contests' })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText('Week 8 contest');
});

test('the contest list puts the live contest first with a countdown', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/');
  // S10: the live contest is pinned on top with its countdown and one button.
  const pinned = page.getByRole('region', { name: 'Week 6 contest' });
  await expect(pinned).toContainText('Live');
  await expect(pinned.getByText(/^Ends in\s*\d{2}:\d{2}:\d{2}$/)).toBeVisible();
  await expect(pinned.getByRole('link', { name: 'Open contest' })).toHaveAttribute('href', '/contests/week-6');
  await expect(pinned.getByRole('link', { name: 'Open contest' })).toHaveClass(/btn-primary/);
  await expect(page.locator('.btn-primary')).toHaveCount(1);
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('Upcoming');
  await expect(rows.nth(1)).toContainText('240 of 300');
  await expect(rows.nth(1)).toContainText('Ended');
  await expect(rows.nth(0).getByText(/^Starts in \d d( \d+ h)?$/)).toBeVisible();
  // Phones keep the score, on the line under the start.
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(rows.nth(1).getByText('240 of 300')).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  // Students get Contests and Your progress (P4).
  await expect(page.getByRole('link', { name: 'Contests', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link')).toHaveText(['Contests']);
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toHaveCount(0);
});

test('messages posted while the student was away still show, until read (S1)', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during' });
  const before = Date.now() - 5 * 60_000;
  api.announcements.push({ id: 5, body_md: 'Sample 2 of B is fixed.', created_at: before });
  api.clarifications.push({ id: 9, problem_label: 'A', question: 'Can a be negative?', answer: 'No, a is at least 0.', public: false, created_at: before - 60_000, answered_at: before + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(contestBanner(page)).toContainText('No, a is at least 0.');
  await expect(contestBanner(page)).toContainText('1 more message waiting.');
  await expect(page.getByRole('button', { name: /Messages/ })).toContainText('2');
  await page.getByRole('button', { name: /Messages/ }).click();
  await expect(page.getByRole('dialog', { name: 'Messages' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.reload();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  await expect(contestBanner(page)).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Messages/ })).not.toContainText('2');
});

test('a student agrees to the honour code, solves, reads an announcement and asks a question', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByRole('link', { name: 'Week 6 contest' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Week 6 contest');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  // B5: three facts, the honour code and the button; the full rules are in Help.
  const facts = page.locator('dl.facts');
  await expect(facts).toContainText('Length2 h');
  await expect(facts).toContainText('LanguagesC++17, Java 21, Python 3');
  await expect(facts).toContainText('Wait between submissions30 s');
  await expect(page.getByText('It is deleted 180 days after the contest.', { exact: false })).toBeVisible();
  await expect(page.getByText(/^The contest started at \d{2}:\d{2} and your time is running\. It ends in/)).toBeVisible();
  await page.getByRole('button', { name: 'Read the rules and how Run and Submit work' }).click();
  const help = page.getByRole('dialog', { name: 'Help' });
  await expect(help.getByRole('region', { name: 'Rules' })).toContainText('You can submit up to 15 times per problem, at least 30 s apart.');
  await expect(help.getByRole('region', { name: 'Run and Submit' })).toContainText('It never counts.');
  await page.keyboard.press('Escape');

  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await expect.poll(() => api.starts).toBe(1);
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(rail.getByRole('link', { name: /^Problem [A-C], / })).toHaveCount(3);
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, not tried' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  await expect(page.getByRole('timer')).toHaveText(/^\d{2}:\d{2}:\d{2}$/);
  // The clock says nothing about the standings (S1-09.5).
  await expect(page.getByTestId('contest-clock')).not.toContainText(/freez/i);
  await expect(page.getByLabel('Language').locator('option')).toHaveText(['C++17', 'Java 21', 'Python 3']);

  // Submit, then the judged result comes in and the rail picks up the score.
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0].label).toBe('A');
  const id = Number(api.submissions.at(-1)!.id);
  api.scores.A = 40;
  const judged = submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.stream.push({ type: 'submission', data: judged });
  await expect(page.getByRole('region', { name: 'Result' }).getByText('Partial, 40 of 100', { exact: true }).first()).toBeVisible({ timeout: 10_000 });
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, 40 of 100 points' })).toBeVisible({ timeout: 10_000 });

  // An announcement arrives as a quiet banner, not an alert.
  api.announcements.push({ id: 1, body_md: 'Problem B: the statement has been corrected.', created_at: Date.now() + 1000 });
  api.stream.push({ type: 'announcement', data: { contest: 4, id: 1, body_md: 'Problem B: the statement has been corrected.' } });
  const banner = contestBanner(page).filter({ hasText: 'Problem B: the statement has been corrected.' });
  await expect(banner).toBeVisible({ timeout: 10_000 });
  // X1: spoken once through the polite announcer.
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Announcement: Problem B: the statement has been corrected.');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Messages/ })).toContainText('1');
  await page.screenshot({ path: `${shots()}contest-banner-1440-light.png` });

  // Open the messages and ask about problem B.
  await page.getByRole('button', { name: 'Show all messages' }).click();
  const dialog = page.getByRole('dialog', { name: 'Messages' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Problem B: the statement has been corrected.')).toBeVisible();
  // The topic starts on the problem open, with general one choice away.
  await expect(dialog.getByLabel('About')).toHaveValue('A');
  await expect(dialog.getByText('Set to the problem you have open.', { exact: false })).toBeVisible();
  await dialog.getByLabel('About').selectOption('B');
  await dialog.getByRole('button', { name: 'Send question' }).click();
  await expect(dialog.getByText('Write your question first.')).toBeVisible();
  await expect(dialog.getByLabel('Question', { exact: true })).toBeFocused();
  await dialog.getByLabel('Question', { exact: true }).fill('Can intervals overlap at one point?');
  await dialog.getByRole('button', { name: 'Send question' }).click();
  await expect.poll(() => api.asked).toEqual([{ problem_label: 'B', question: 'Can intervals overlap at one point?' }]);
  await expect(dialog.getByText('Waiting for an answer.')).toBeVisible();
  await expect(dialog.getByRole('status').filter({ hasText: 'Question sent.' })).toBeVisible();
  await page.screenshot({ path: `${shots()}contest-messages-1440-light.png` });
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(banner).toBeHidden();

  // The rail moves between problems, and focus goes to the new heading.
  await rail.getByRole('link', { name: /Problem B/ }).click();
  await expect(page).toHaveURL('/contests/week-6/problems/B');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('B. Interval covering');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
});

test('an upcoming fixed-start contest counts down and keeps the start button closed', async ({ page }) => {
  await mockApi(page, { phase: 'upcoming' });
  await page.goto('/contests/week-6');
  await expect(page.getByRole('timer')).toHaveText(/^02:0\d:\d{2}$/);
  await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toBeDisabled();
  await expect(page.getByText(/^You can start at/)).toBeVisible();
  // S3: the countdown, the honour code and the button come first, above the fold.
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toBeInViewport();
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(page.getByRole('timer')).toBeInViewport();
  const order = await page.evaluate(() => {
    const at = (sel: string) => document.querySelector(sel)?.getBoundingClientRect().top ?? -1;
    return [at('[role="timer"]'), at('#honour-title'), at('#rules-title')];
  });
  expect(order[0]).toBeLessThan(order[1]);
  // B5: the button is in the first screen on a phone; the rules moved to Help.
  expect(order[2]).toBe(-1);
  await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toBeInViewport();
});

test('a result the server hides shows only "Submitted"', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during', hideResults: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.getByText('Submitted', { exact: true }).first()).toBeVisible();
  await expect(result.getByText('Scores appear after the contest.')).toBeVisible();
  const id = Number(api.submissions.at(-1)!.id);
  // The judged event still hides the result, so nothing more shows.
  api.stream.push({ type: 'submission', data: hidden(id, 11) });
  await expect.poll(() => api.stream.length, { timeout: 15_000 }).toBe(0);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
  await expect(result.getByText(/Partial|Wrong answer|Accepted|40 of 100/)).toHaveCount(0);
  await expect(result.getByRole('img', { name: /^(No subtasks|Subtask )/ })).toHaveCount(0);
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Submitted.');
  await page.screenshot({ path: `${shots()}contest-hidden-result-1440-light.png` });
});

test('after the contest: result, submissions, editorial and practice', async ({ page }) => {
  const api = await mockApi(page, { phase: 'practice' });
  api.clarifications.push({ id: 9, problem_label: 'B', question: 'Can intervals touch?', answer: 'Yes, touching counts as covered.', public: false, created_at: Date.now() - 3 * 3_600_000, answered_at: Date.now() - 3 * 3_600_000 + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  // B5: the messages are folded under the result.
  await page.getByRole('heading', { name: 'Messages', exact: true }).click();
  const questions = page.getByRole('region', { name: 'Your questions' });
  await expect(questions).toContainText('Can intervals touch?');
  await expect(questions).toContainText('Yes, touching counts as covered.');
  await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
  // S9: one lead, the place as the display figure, no repeated sentence.
  await expect(page.getByText(/^Ended \d+ [A-Z][a-z]{2}, \d{2}:\d{2}\. You scored 140 of 300\.$/)).toBeVisible();
  await expect(page.getByText(/^\s*\d+ of 28$/)).toBeVisible();
  await expect(page.getByText(/You are \d+/)).toHaveCount(0);
  await expect(page.getByText('Practice is open: new submissions do not count.')).toBeVisible();
  await expect(page.getByText('Use 64-bit integers.')).toBeVisible();
  // The problem with the newest submission opens by itself.
  // B5: one list of problems, each once, the newest one open with its submissions.
  const list = page.getByRole('list', { name: 'Your problems' });
  await expect(list.locator(':scope > li')).toHaveCount(3);
  await expect(list.locator('details[open] summary')).toHaveText([/^B\s+Interval covering\s+40 of 100$/]);
  await expect(list.locator('details[open]').getByText('40 of 100').first()).toBeVisible();
  await expect(page.getByText('Your submissions', { exact: true })).toHaveCount(0);

  await page.getByRole('link', { name: 'Open problem B' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('B. Interval covering');
  await expect(page.getByText(/^Practice\. Your contest score is still \d+ of \d+\.$/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeEnabled();
});

test('after the contest the page keeps answers for everyone, in the dialog copy (C1-010)', async ({ page }) => {
  const api = await mockApi(page, { phase: 'after' });
  api.clarifications.push({ id: 9, problem_label: 'B', question: 'Is n at most 10^5?', answer: null, public: false, created_at: Date.now() - 3 * 3_600_000 });
  api.publicClarifications.push({ id: 10, problem_label: 'A', question: 'May the sum overflow?', answer: 'Use 64-bit integers for the sum.', public: true, created_at: Date.now() - 2 * 3_600_000, answered_at: Date.now() - 2 * 3_600_000 + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  await page.getByRole('heading', { name: 'Messages', exact: true }).click();
  const everyone = page.getByRole('region', { name: 'Answers for everyone' });
  await expect(everyone).toContainText('May the sum overflow?');
  await expect(everyone).toContainText('Use 64-bit integers for the sum.');
  const questions = page.getByRole('region', { name: 'Your questions' });
  await expect(questions).toContainText('Waiting for an answer.');
  await expect(page.getByText('Not answered.')).toHaveCount(0);
});

test('after a contest without practice, the problem is read only', async ({ page }) => {
  await mockApi(page, { phase: 'after' });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.getByText('The contest has ended. Submissions are closed.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeDisabled();
});

test('the frozen board shows pending cells, tints the own row and jumps to it', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 700 });
  await page.goto('/contests/week-6/standings');
  await expect(page.getByText(/^Results from \d{2}:\d{2} are hidden until the end\. 2 submissions are still being judged\.$/)).toBeVisible();
  const mine = page.locator('tr.me');
  await expect(mine).toContainText('Asha Rao');
  await expect(mine).toContainText('(you)');
  await expect(mine.getByText('+2', { exact: true })).toBeVisible();
  await expect(mine.getByText('Problem B, 0 of 100, 2 pending', { exact: false })).toHaveCount(0);
  await expect(mine.getByText('Problem B, no score before the freeze, 2 pending')).toBeAttached();
  const tint = await mine.locator('th').evaluate((el) => getComputedStyle(el).backgroundColor);
  const plain = await page.locator('tbody tr').first().locator('th').evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(tint).not.toBe(plain);

  // S8: a partial cell is a bar along its bottom, a tried zero reads in the wrong colour.
  const partial = page.locator('td.pcell.tone-part').first();
  await expect(partial.locator('.meter-fill')).toBeAttached();
  expect(await partial.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe(plain);
  await expect(page.getByRole('button', { name: 'Jump to you' })).toBeVisible();
  await page.getByRole('button', { name: 'Jump to you' }).click();
  await expect(mine).toBeFocused();
  await expect(mine).toBeInViewport();
  // With the own row in view the button hides, keeping its place.
  await expect(page.getByRole('button', { name: 'Jump to you' })).toBeHidden();
  await expect(page.locator('tr.me .name-text')).toHaveText(/Asha Rao\s\(you\)/);

  // The header stays put while rows scroll.
  const head = page.getByRole('columnheader').first();
  expect(await head.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');

  // On a phone the rank and name stay pinned while the problems scroll.
  await page.setViewportSize({ width: 375, height: 812 });
  const wrap = page.getByRole('region', { name: /^Standings for / });
  await wrap.evaluate((el) => (el.scrollLeft = 400));
  const name = page.locator('tbody tr').first().locator('th.name');
  const before = await name.boundingBox();
  expect(await name.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');
  expect(before!.x).toBeGreaterThanOrEqual(0);
  await page.screenshot({ path: `${shots()}standings-phone-scrolled-375-light.png` });
});

test('on a phone the standings header stays on screen while rows scroll', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/contests/week-6/standings');
  const wrap = page.getByRole('region', { name: /^Standings for / });
  await expect(wrap.locator('tbody tr')).toHaveCount(28);
  // The box fits the screen and scrolls both ways, so its sticky header row has something to stick to.
  const box = (await wrap.boundingBox())!;
  expect(box.height).toBeLessThanOrEqual(812);
  await wrap.evaluate((el) => (el.scrollTop = 600));
  expect(await wrap.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  const head = (await wrap.locator('thead th').first().boundingBox())!;
  expect(Math.abs(head.y - box.y)).toBeLessThanOrEqual(2);
  await page.screenshot({ path: `${shots()}standings-phone-rows-scrolled-375-light.png` });
});

test('a second tab with the same contest shows a notice, as does a second session', async ({ page, context, browser }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  const second = await context.newPage();
  await mockApi(second, { phase: 'during' });
  await second.goto('/contests/week-6/problems/A');
  await expect(second.getByText('This contest is also open in another tab or window on this device.', { exact: false })).toBeVisible();
  await expect(page.getByText('This contest is also open in another tab or window on this device.', { exact: false })).toBeVisible();
  await second.close();

  // Another device: the server reports the second session.
  const elsewhere = await browser.newContext();
  const other = await elsewhere.newPage();
  await mockApi(other, { phase: 'during', secondSession: true });
  await other.goto('/contests/week-6/problems/B');
  await expect(other.getByText('Your account is in this contest from another session as well.', { exact: false })).toBeVisible();
  await elsewhere.close();
});

test('admins land on their contest list, named by job', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.goto('/');
  await expect(page).toHaveURL('/admin/contests');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Problems']);
  await expect(nav.getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByText('To create a contest or follow one live')).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Week 7 contest/ })).toContainText('Draft');
  // A published live contest comes first, read by its state; there is no separate visibility column (P3 O1).
  await expect(page.getByRole('region', { name: 'Week 6 contest' }).getByText(/^(Live|Frozen)$/)).toBeVisible();

  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title').fill('Week 8 contest');
  // The server makes the address from the title (V3-15).
  await expect(page.getByLabel('Slug')).toHaveCount(0);
  await page.getByRole('button', { name: 'Create contest' }).click();
  await expect(page).toHaveURL('/admin/contests/9');
  expect(api.creates).toHaveLength(1);
  expect(api.creates[0]).toMatchObject({ title: 'Week 8 contest' });
  expect(api.creates[0]).not.toHaveProperty('slug');
  // The length, two hours by default, makes the end.
  expect(Number(api.creates[0].ends_at) - Number(api.creates[0].starts_at)).toBe(120 * 60_000);
});

test('a new contest marks its required fields, and a field error from the server takes focus (X7)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.goto('/admin/contests');
  await page.getByRole('button', { name: 'New contest' }).click();
  const form = page.locator('#create-contest');
  await expect(form.getByText('All fields are required.')).toBeVisible();
  for (const name of ['Title', 'Starts', 'Length in minutes']) await expect(form.getByLabel(name, { exact: true })).toHaveAttribute('aria-required', 'true');
  await form.getByLabel('Title', { exact: true }).fill('Week 6');
  await form.getByLabel('Starts', { exact: true }).fill('2026-11-01T18:00');
  await form.getByLabel('Length in minutes', { exact: true }).fill(String(20 * 24 * 60));
  await page.getByRole('button', { name: 'Create contest' }).click();
  const length = form.getByLabel('Length in minutes', { exact: true });
  await expect(length).toBeFocused();
  await expect(length).toHaveAttribute('aria-invalid', 'true');
  await expect(page.locator('#new-length-error')).toHaveText('A contest can last at most 14 days.');
});

test('students cannot open the contest admin', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/admin/contests');
  await expect(page.getByText('Only organisers can manage contests.')).toBeVisible();
  // F3: a closed screen names where to ask, as a mailto link.
  await expect(page.getByRole('main').getByRole('link', { name: 'priya@college.edu' })).toHaveAttribute('href', 'mailto:priya@college.edu');
});

test('the Live tab counts unanswered questions on Setup (O4)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during' });
  const asked: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/clarifications')) asked.push(new URL(r.url()).pathname);
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link', { name: /^Live/ })).toContainText('1');
  await expect(nav.getByRole('link', { name: /^Live/ })).toContainText('unanswered question');
  expect(asked).toContain('/api/admin/contests/4/clarifications');
  expect(asked.filter((p) => !p.startsWith('/api/'))).toEqual([]);
});

test('hidden tests shown in practice are named (C1-015)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during', published: false, shownInPractice: [11, 12], publishOk: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByLabel('Add a validated problem').locator('option', { hasText: 'Interval covering' })).toHaveText(
    'Interval covering (100 points), hidden tests shown in practice',
  );
  await page.getByRole('button', { name: 'Publish contest' }).click();
  const publish = page.getByRole('dialog', { name: 'Publish Week 6 contest?' });
  await expect(publish).toContainText('open for practice after a contest, so students may know the hidden tests.');
  await publish.getByRole('button', { name: 'Cancel' }).click();


  // The bank marks the same problems.
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /Interval covering/ });
  await expect(row.getByText('Hidden tests shown in practice')).toBeVisible();
  await expect(page.getByRole('row', { name: /Unfinished draft/ }).getByText('Hidden tests shown in practice')).toHaveCount(0);
});

test("a problem whose hidden tests were shown stays, and the editor says why (C2-014)", async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during', shownHere: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByRole('button', { name: 'Remove A plus B' }).click();
  await page.getByRole('dialog', { name: /^Remove problem A\?$/ }).getByRole('button', { name: 'Remove problem' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Problem A stays in this contest' })).toContainText('students have seen its hidden tests here');
  await expect.poll(() => api.problemPuts.length).toBe(1);
  // The list goes back to what the server holds.
  await expect(page.getByLabel('Letter for A plus B')).toHaveValue('A');
});

test('the editor saves each field as it changes, shows field errors and keeps the address read-only', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByRole('note').filter({ hasText: 'This contest is live.' })).toBeVisible();
  await expect(page.getByRole('note').getByRole('link', { name: 'Live view' })).toHaveAttribute('href', '/admin/contests/4/console');

  // The address is made from the title, cannot be typed and sits under Advanced (V3-15).
  const address = page.getByText('Address /contests/week-6, made from the title when the contest was created. It does not change.');
  await expect(address).toBeHidden();
  await page.locator('#advanced summary').click();
  await expect(address).toBeVisible();
  await expect(page.getByLabel('Slug')).toHaveCount(0);

  // A field that cannot be saved shows its error beside it, and nothing is sent.
  const title = page.getByLabel('Title', { exact: true });
  const saved = saveStatus(page);
  await title.fill('x'.repeat(121));
  await expect(page.locator('#f-title-error')).toHaveText('Enter a title of at most 120 characters.');
  await expect(title).toHaveAttribute('aria-invalid', 'true');
  await expect(saved).toHaveText('Not saved. Fix the marked field.');
  expect(api.settingsPuts).toHaveLength(0);

  // Each change sends only itself; the server keeps the rest.
  await title.fill('Week 6 contest');
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ title: 'Week 6 contest' });
  await expect(saved).toHaveText('Saved');
  await page.getByLabel('Python 3').uncheck();
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ languages: ['cpp17'] });
  expect(api.settingsPuts.some((b) => 'slug' in b || 'published' in b)).toBe(false);
  await expect(page.getByRole('button', { name: /^Save/ })).toHaveCount(0);

  // Changing the hidden minutes of a live contest asks first; Cancel puts the value back.
  const puts = api.settingsPuts.length;
  const hidden = page.getByLabel('Hidden minutes at the end');
  await hidden.fill('20');
  const live = page.getByRole('dialog', { name: 'Change a live contest?' });
  await expect(live.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await live.getByRole('button', { name: 'Cancel' }).click();
  await expect(hidden).toHaveValue('30');
  expect(api.settingsPuts.length).toBe(puts);
  await hidden.fill('20');
  await live.getByRole('button', { name: 'Change the contest' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ freeze_minutes: 20 });

  // Only validated problems are offered; adding one gives it the next letter.
  const add = page.getByLabel('Add a validated problem');
  await expect(add.locator('option')).toHaveText(['Choose a problem', 'Interval covering (100 points)']);
  await add.selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  // Each change saves at once; there is no separate Save problems.
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 11, label: 'A' },
    { problem_id: 12, label: 'B' },
  ]);
  await expect(toasts(page).getByText('Added Interval covering as B', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save problems' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 12, label: 'A' },
    { problem_id: 11, label: 'B' },
  ]);

});

test('the console pauses, extends, judges again, announces and answers', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const sentence = page.locator('#console-sentence');
  await expect(sentence).toHaveText('Live, 30 started, 1 question waiting, judge fine.');
  const actions = page.getByRole('group', { name: 'Contest actions' });
  const announce = page.getByRole('dialog', { name: 'Announce to every student' });

  // Pausing asks first, then opens an announcement ready to post (C3); it is never posted by itself.
  await actions.getByRole('button', { name: 'Pause judging' }).click();
  const dialog = page.getByRole('dialog', { name: 'Pause judging?' });
  await expect(dialog).toContainText('their submissions wait in the queue and nothing is lost');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Pause judging' }).click();
  await expect.poll(() => api.pauses).toBe(1);
  await expect(announce.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  expect(api.posted).toHaveLength(0);
  await announce.getByRole('button', { name: 'Cancel' }).click();
  await expect(sentence).toHaveText('Live, 30 started, 1 question waiting, judging paused.');
  // Resume asks in the same confirmation as pause (walk bug B5).
  await actions.getByRole('button', { name: 'Resume judging' }).click();
  await page.getByRole('dialog', { name: 'Resume judging?' }).getByRole('button', { name: 'Resume judging' }).click();
  await expect.poll(() => api.resumes).toBe(1);
  await expect(announce.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging has resumed/);
  await announce.getByRole('button', { name: 'Cancel' }).click();
  await expect(sentence).toHaveText('Live, 30 started, 1 question waiting, judge fine.');

  // Extend is a dialog: the minutes, the new end said first, then Extend contest.
  await actions.getByRole('button', { name: 'Extend', exact: true }).click();
  const extend = page.getByRole('dialog', { name: 'Extend the contest' });
  await extend.getByLabel('Minutes to add for everyone').fill('0');
  await extend.getByRole('button', { name: 'Extend contest' }).click();
  await expect(page.locator('#extend-error')).toHaveText('Enter the minutes to add, from 1 to 240.');
  await expect(extend.getByLabel('Minutes to add for everyone')).toHaveAttribute('aria-invalid', 'true');
  await extend.getByLabel('Minutes to add for everyone').fill('15');
  await expect(extend).toContainText("Every student's contest then ends at");
  await extend.getByRole('button', { name: 'Extend contest' }).click();
  await expect.poll(() => api.extends).toEqual([{ minutes: 15 }]);
  await expect(announce.getByLabel('Announcement (Markdown)')).toHaveValue('The contest has been extended by 15 minutes.');
  await announce.getByRole('button', { name: 'Cancel' }).click();

  // Extra time for one student, listed when the dialog opens again.
  await actions.getByRole('button', { name: 'Extra time' }).click();
  const extra = page.getByRole('dialog', { name: 'Extra time for one student' });
  await extra.locator('#ext-user').selectOption({ label: 'Asha Rao' });
  await extra.getByLabel('Extra minutes').fill('10');
  await extra.getByRole('textbox', { name: 'Reason' }).fill('Laptop crashed');
  await extra.getByRole('button', { name: 'Give extra time' }).click();
  await expect.poll(() => api.extensions).toEqual([{ user: '7', body: { minutes: 10, reason: 'Laptop crashed' } }]);
  await expect(extra).toBeHidden();
  await actions.getByRole('button', { name: 'Extra time' }).click();
  await expect(extra.getByRole('listitem')).toContainText('Asha Rao: 10 min, Laptop crashed');
  await extra.getByRole('button', { name: 'Cancel' }).click();

  // Filters go to the feed's query; every Judge again asks first (walk bug B5).
  await page.locator('#f-problem').selectOption('A');
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?problem=A&page=1');
  await expect(feedRows(page)).toHaveCount(1);
  await page.getByRole('button', { name: /^Judge again: Arjun Mehta, problem A/ }).click();
  await page.getByRole('dialog', { name: "Judge Arjun Mehta's submission again?" }).getByRole('button', { name: 'Judge again' }).click();
  await expect.poll(() => api.rejudges).toEqual([{ submission_id: 300 }]);
  await page.getByRole('button', { name: 'Judge problem A again' }).click();
  await page.getByRole('dialog', { name: 'Judge problem A again?' }).getByRole('button', { name: 'Judge again' }).click();
  await expect.poll(() => api.rejudges.at(-1)).toEqual({ problem_label: 'A' });
  await expect(announce.getByLabel('Announcement (Markdown)')).toHaveValue(/Every submission to problem A is being judged again/);

  // Posting from the dialog, with a preview; the dialog says it reaches everyone and cannot be taken back (C3).
  await announce.getByLabel('Start from a template').selectOption('paused');
  await expect(announce.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  await expect(announce.getByRole('group', { name: 'Preview' })).toContainText('Judging is paused for a few minutes');
  await expect(announce).toContainText('It cannot be taken back.');
  await announce.getByRole('button', { name: 'Post announcement' }).click();
  await expect.poll(() => api.posted.length).toBe(1);
  await expect(announce).toBeHidden();

  // A standard answer fills the box to edit; Everyone says what students will see (P12).
  await page.getByLabel('Standard answer').selectOption({ label: 'Yes.' });
  const answer = page.getByLabel('Answer', { exact: true });
  await expect(answer).toHaveValue('Yes.');
  await expect(answer).toBeFocused();
  await answer.fill('Yes. Such an interval covers one point.');
  await expect(page.getByText('Only Asha Rao sees the answer, in Messages.')).toBeVisible();
  await page.getByLabel('Everyone', { exact: true }).check();
  await expect(page.getByText('Every student sees the question and this answer in Messages, without the name of who asked.')).toBeVisible();
  await page.getByRole('button', { name: 'Send to everyone' }).click();
  await expect.poll(() => api.answers).toEqual([{ id: '51', body: { answer: 'Yes. Such an interval covers one point.', public: true } }]);
  await expect(sentence).toHaveText('Live, 30 started, judge fine.');

  // The audit log is behind a link, newest first, with who did it.
  await expect(page.getByRole('region', { name: 'Changes' })).toHaveCount(0);
  await actions.getByRole('button', { name: 'History of changes' }).click();
  const audit = page.getByRole('dialog', { name: 'History of changes' }).getByRole('region', { name: 'Changes' });
  await expect(audit.getByRole('listitem').first()).toContainText('Judged problem A again (1 submission)');
  // v1.4.0 rows carry reason_given, never the reason.
  await expect(audit).toContainText('Gave Asha Rao 10 extra minutes, with a reason');
  await expect(audit).toContainText('Extended the contest by 15 minutes');
  await expect(audit).toContainText('Paused judging');
  await expect(audit).toContainText('priya@college.edu');
  await page.screenshot({ path: `${shots()}contest-console-1440-light.png`, fullPage: true });
});

test('a workspace whose stream is down still picks up announcements (walk 2026-10-21)', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during' });
  // Every stream attempt fails, as while the server restarts: the page falls back to polling.
  await page.route('**/api/events*', (route) => route.fulfill({ status: 503, body: '' }));
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.clock.install();
  await page.goto('/contests/week-6/problems/A');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  // Posted while the stream is down: no event will ever carry it.
  api.announcements.push({ id: 9, body_md: 'Posted while your stream was down.', created_at: Date.now() });
  await page.clock.runFor(20_000);
  await expect(contestBanner(page).filter({ hasText: 'Posted while your stream was down.' })).toBeVisible();
});

test('the console says partial scores as students read them and keeps rows when a refresh fails (P3 O3, C2)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.clock.install();
  await page.goto('/admin/contests/4/console');
  const feed = feedRows(page);
  await expect(feed.filter({ hasText: 'Asha Rao' })).toContainText('Partial, 40 of 100');
  await expect(feed.filter({ hasText: 'Kiran Das' })).toContainText('Wrong answer');
  await expect(feed.filter({ hasText: 'Arjun Mehta' })).toContainText('Accepted');
  // Minutes from the start, with the clock time on hover.
  const when = feed.filter({ hasText: 'Asha Rao' }).getByRole('cell').first();
  await expect(when).toHaveText(/^\+\d+ min$/);
  await expect(when).toHaveAttribute('title', /^(\d{1,2} [A-Z][a-z]{2} )?\d\d:\d\d$/); // a date too, after midnight

  await page.route('**/api/admin/contests/4/submissions*', (route) => route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'Down.' } } }));
  await page.clock.runFor(10_500);
  const panel = page.getByRole('region', { name: 'Submissions' });
  await expect(panel.getByRole('status')).toHaveText(/Could not refresh at \d\d:\d\d:\d\d\. Showing \d\d:\d\d:\d\d\. Retrying\./);
  await expect(feed).toHaveCount(5);
});

test('a late judge and a slow queue come first, and banners show only what needs action (C2, C10)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during', queueHealth: 'trouble' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await expect(page.locator('#console-sentence .trouble')).toHaveText('Judge not seen for 3 minutes. The oldest submission has waited 1 minute for the judge.');
  await expect(page.locator('#console-sentence')).toContainText('Live, 30 started, 1 question waiting.');
  const banners = page.getByRole('region', { name: 'Needs attention' });
  await expect(banners).toContainText('The judge is not taking work.');
  await expect(banners).toContainText('1 judge error. Those students see an error and can submit again.');
  // Show filters the feed to judge errors.
  await banners.getByRole('button', { name: 'Show the judge errors' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=error&page=1');
  await expect(feedRows(page)).toHaveCount(1);
  // The failure path is written into the page.
  await banners.getByRole('button', { name: 'What to do' }).click();
  const trouble = page.getByRole('dialog', { name: 'If something goes wrong' });
  await expect(trouble.getByRole('heading')).toHaveText(['If something goes wrong', 'The judge stalled', 'The VM is gone', 'Sign-in is down']);
  await expect(trouble).toContainText('sudo systemctl restart podium-judge');
  await page.screenshot({ path: `${shots()}contest-console-trouble-1440-light.png`, fullPage: true });
});

test('the sentence changes when a question arrives and when the judge is late (C2, P9)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.clock.install();
  await page.goto('/admin/contests/4/console');
  const sentence = page.locator('#console-sentence');
  await expect(sentence).toHaveText('Live, 30 started, 1 question waiting, judge fine.');
  await expect(page).toHaveTitle('(1) Live · Week 6 contest · podium');
  // A student asks: the live channel says so and the page reads the questions again.
  api.newQuestions.push({
    id: 52, problem_label: 'A', asked_by: { user_id: 104, email: 'kiran@college.edu', name: 'Kiran Das' },
    question: 'Can a be negative?', answer: null, public: false, created_at: Date.now(), answered_at: null,
  });
  api.stream.push({ type: 'clarification', data: { contest: 'week-6', contest_id: 4, id: 52 } });
  await page.clock.runFor(1000);
  await expect(sentence).toHaveText('Live, 30 started, 2 questions waiting, judge fine.');
  await expect(page).toHaveTitle('(2) Live · Week 6 contest · podium');
  // The question waiting longest stays at the top.
  await expect(page.getByRole('region', { name: 'Questions' }).getByRole('listitem')).toHaveText([/Can intervals have zero length\?/, /Can a be negative\?/]);
  // The judge stops asking for work; the next refresh says so first.
  api.judgeLate = true;
  await page.clock.runFor(10_500);
  await expect(sentence).toHaveText('Judge not seen for 3 minutes. Live, 30 started, 2 questions waiting.');
});

test('after the end, Extend says the contest has ended (C2, walk bug B3)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await expect(page.locator('#console-sentence')).toHaveText(/^Ended, 30 started/);
  await expect(page.getByRole('button', { name: 'End now' })).toHaveCount(0);
  const actions = page.getByRole('group', { name: 'Contest actions' });
  await actions.getByRole('button', { name: 'Extend', exact: true }).click();
  const extend = page.getByRole('dialog', { name: 'Extend the contest' });
  await expect(extend).toContainText('The contest has ended.');
  await expect(extend.getByRole('button', { name: 'Extend contest' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await actions.getByRole('button', { name: 'Extra time' }).click();
  await expect(page.getByRole('dialog', { name: 'Extra time for one student' })).toContainText('The contest has ended, so extra time can no longer be given.');
  expect(api.extends).toHaveLength(0);
});

test('an extend the server refuses after the end is said in the dialog (C2, walk bug B3)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await page.getByRole('group', { name: 'Contest actions' }).getByRole('button', { name: 'Extend', exact: true }).click();
  const extend = page.getByRole('dialog', { name: 'Extend the contest' });
  await extend.getByLabel('Minutes to add for everyone').fill('5');
  // The contest closes while the dialog is open: the server refuses (contest_ended).
  api.closed = true;
  await extend.getByRole('button', { name: 'Extend contest' }).click();
  await expect(extend.getByRole('alert')).toHaveText('The contest has ended, so it cannot be extended. Extending it now would reopen it for every student.');
  expect(api.extends).toHaveLength(0);
  await expect(page.getByRole('dialog', { name: 'Announce to every student' })).toBeHidden();
});

test('the console on a phone: three tabs, under 1,500 px tall at 375 px (C2)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/contests/4/console');
  await expect(page.getByText('Can intervals have zero length?')).toBeVisible();
  const tabs = page.getByRole('tablist', { name: 'Live view' });
  await expect(tabs.getByRole('tab')).toHaveText([/^Questions/, 'Submissions', 'Overview']);
  await settle(page);
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  test.info().annotations.push({ type: 'console height at 375 px', description: String(height) });
  expect(height).toBeLessThan(1500);
  await page.screenshot({ path: `${shots()}contest-console-375-light.png`, fullPage: true });
  await tabs.getByRole('tab', { name: 'Submissions' }).click();
  await expect(page.getByRole('cell', { name: 'Partial, 40 of 100' })).toBeVisible();
  await expect(page.getByText('Can intervals have zero length?')).toBeHidden();
  await tabs.getByRole('tab', { name: 'Overview' }).click();
  await expect(page.getByText('21 solved of 27 attempted')).toBeVisible();
});

test('End the contest now says what happens and sets the end to now (P3 O5)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await page.getByRole('button', { name: 'End now' }).click();
  const end = page.getByRole('dialog', { name: 'End the contest now?' });
  await expect(end).toContainText("Everyone's time ends now");
  await expect(end).toContainText('Queued submissions are still judged.');
  await expect(end).toContainText('Students see Ended, and practice opens.');
  // The freeze counts back from the end; ended before it, the board must not jump back in time (walk 2026-10-21).
  await expect(end).toContainText('The board was not frozen yet, so it stays as students see it now: the freeze and any held reveal are dropped.');
  await expect(end.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await end.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  const before = Date.now();
  await page.getByRole('button', { name: 'End now' }).click();
  await end.getByRole('button', { name: 'End the contest' }).click();
  await expect.poll(() => api.settingsPuts.length).toBe(1);
  const body = api.settingsPuts[0];
  // Every other setting is sent as it was; the end moves to now and the unstarted freeze goes.
  expect(body).toMatchObject({ title: 'Week 6 contest', freeze_minutes: 0, languages: ['cpp17', 'python3'], published: true });
  expect(Math.abs(Number(body.ends_at) - before)).toBeLessThan(15_000);
  await expect(toasts(page).getByText('Ended the contest', { exact: true })).toBeVisible();
});

test('End now in the first minute of the freeze keeps the board frozen (V3-18, walk bug B1)', async ({ page }) => {
  // The mock contest closes 90 min after its creation with a 30 min freeze, so it froze 60 min in; end 52 s after that.
  const base = Math.floor(Date.now() / 1000) * 1000; // the console ends on a whole second
  const inFreeze = base + 60 * 60_000 + 52_000;
  freezeClock(base);
  try {
    const api = await mockApi(page, { who: 'organiser', phase: 'during' });
    freezeClock(inFreeze);
    await page.clock.setFixedTime(inFreeze);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/contests/4/console');
    await page.getByRole('button', { name: 'End now' }).click();
    const end = page.getByRole('dialog', { name: 'End the contest now?' });
    // Rounded up to a whole minute: the frozen moment moves 8 s earlier, never later.
    // Formatted in the browser: its timezone is the config's (Asia/Kolkata), not the runner's.
    const hhmm = await page.evaluate((at) => {
      const d = new Date(at);
      return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
    }, inFreeze - 60_000);
    await expect(end).toContainText(`The board stays frozen as it was at ${hhmm}.`);
    await end.getByRole('button', { name: 'End the contest' }).click();
    await expect.poll(() => api.settingsPuts.length).toBe(1);
    // Rounding down sent 0 here, which released the held board at once.
    expect(api.settingsPuts[0]).toMatchObject({ freeze_minutes: 1, ends_at: inFreeze });
  } finally {
    freezeClock(null);
  }
});

test('unpublishing a live contest asks first', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during', published: true });
  await page.goto('/admin/contests/4');
  await expect(page.getByText(/^Published and live\./)).toBeVisible();
  await page.getByRole('button', { name: 'Unpublish contest' }).click();
  const unpublish = page.getByRole('dialog', { name: 'Unpublish Week 6 contest?' });
  await expect(unpublish).toContainText('The contest disappears from students at once, in the middle of it');
  await expect(unpublish.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await unpublish.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  await page.getByRole('button', { name: 'Unpublish contest' }).click();
  await unpublish.getByRole('button', { name: 'Unpublish contest' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)?.published).toBe(false);
  await expect(toasts(page).getByText('Unpublished', { exact: true })).toBeVisible();
});

test('the console feed filters waiting or judging and judge errors', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.goto('/admin/contests/4/console');
  const verdict = page.getByLabel('Verdict', { exact: true });
  await verdict.selectOption({ label: 'Waiting or judging' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=pending&page=1');
  await expect(feedRows(page)).toHaveCount(1);
  await expect(feedRows(page)).toContainText('Judging');
  await verdict.selectOption({ label: 'Judge error' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=error&page=1');
  await expect(feedRows(page)).toHaveCount(1);
  await expect(feedRows(page)).toContainText('Meera Iyer');
});

test('a contest asks for a reason before it takes an AI problem nobody solved', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'upcoming', aiGate: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();

  const reason = page.getByLabel('Reason to grade problem B as the AI wrote it');
  await expect(reason).toBeFocused();
  await expect(page.getByText('Problem B came from the AI wizard', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Grade as the AI wrote it' }).click();
  await expect(page.locator('#ai-reason-error')).toHaveText('Write a reason of at least 10 characters.');
  await expect(reason).toHaveAttribute('aria-invalid', 'true');

  await reason.fill('Checked the reference by hand against the statement.');
  await page.getByRole('button', { name: 'Grade as the AI wrote it' }).click();
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 11, label: 'A' },
    { problem_id: 12, label: 'B', ai_reason: 'Checked the reference by hand against the statement.' },
  ]);
  await expect(toasts(page).getByText('Saved problems. Problem B is graded as the AI wrote it.', { exact: true })).toBeVisible();
  await expect(reason).toHaveCount(0);
});

test('results switch boards, reveal a held board once, download the CSV and open similar code', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'after', holdReveal: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByText('Final standings.')).toBeVisible();
  // The board spans the page (the walk measured 560 px of 1440).
  const board = await page.getByRole('region', { name: /^Standings for / }).boundingBox();
  const column = await page.locator('section[aria-labelledby="board-title"]').boundingBox();
  expect(board!.width).toBeGreaterThanOrEqual(column!.width - 1);
  await page.getByLabel('As students see it').check();
  await expect(page.getByText(/^Results from \d{2}:\d{2} are hidden/)).toBeVisible();
  await expect(page.getByText('8 students scored 40')).toBeAttached();
  await expect(page.getByRole('heading', { name: 'Statistics' })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Statistics for each problem' })).toHaveCount(1);
  // One row of actions: Reveal, then Similar code beside Download CSV.
  await expect(
    page.getByRole('button', { name: 'Reveal', exact: true }).or(page.getByRole('link', { name: /^(Similar code|Download CSV|Download JSON)$/ })),
  ).toHaveText(['Reveal', 'Similar code', /Download CSV/, /Download JSON/]);
  await expect(page.getByRole('link', { name: 'Download CSV' })).toHaveAttribute('href', /^\/api\/admin\/contests\/4\/export\.csv\?tz=Asia%2F(Kolkata|Calcutta)$/);
  await expect(page.getByRole('link', { name: 'Download JSON' })).toHaveAttribute('href', '/api/admin/contests/4/export.json');
  await expect(page.getByText(/^Students see the board without the results from \d{2}:\d{2}\. Reveal shows them the full board\.$/)).toBeVisible();
  // The animated walk and the editorial are not offered (V3-16): Reveal is one term and one button.
  await expect(page.getByRole('button', { name: 'Start the reveal' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Editorial' })).toHaveCount(0);
  await expect(page.getByText(/release/i)).toHaveCount(0);

  // The download says which file it is (pilot P3), in the browser's zone.
  const waiting = page.waitForEvent('download');
  await page.getByRole('link', { name: 'Download CSV' }).click();
  const download = await waiting;
  expect(download.suggestedFilename()).toBe('week-6-results.csv');
  // Chromium names India's zone by its old alias; Go's zone data knows both.
  expect(new URL(download.url()).searchParams.get('tz')).toMatch(/^Asia\/(Kolkata|Calcutta)$/);
  await expect(page.getByRole('status').filter({ hasText: "Downloading week-6-results.csv to your browser's downloads." })).toBeVisible();

  // It asks once, then the line follows the state (pilot P2).
  await page.getByRole('button', { name: 'Reveal', exact: true }).click();
  const confirmReveal = page.getByRole('dialog', { name: 'Reveal the full board?' });
  await expect(confirmReveal).toContainText('This cannot be undone.');
  await expect(confirmReveal.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmReveal.getByRole('button', { name: 'Reveal', exact: true }).click();
  await expect.poll(() => api.reveals).toBe(1);
  await expect(page.getByText(/^Revealed \d+ \w{3}, \d{2}:\d{2}\. Students see the full board\.$/)).toBeVisible();
  await expect(page.getByText('Reveal shows them the full board.', { exact: false })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Reveal', exact: true })).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByLabel('Full', { exact: true })).toBeChecked();
  await expect(page.getByText('Final standings.')).toBeVisible();

  // Similar code opens the integrity page, titled and trailed so.
  await page.getByRole('link', { name: 'Similar code' }).click();
  await expect(page).toHaveURL('/admin/contests/4/integrity');
  await expect(page).toHaveTitle(/^Similar code · Week 6 contest/);
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).locator('[aria-current="page"]')).toHaveText('Similar code');
});

test('the reveal is not offered before every student\'s time is up, and nothing says release (pilot P2)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during', holdReveal: true });
  await page.goto('/admin/contests/4/results');
  await expect(
    page.getByText(/^Results from \d{2}:\d{2} stay hidden from students until you reveal them\. The reveal opens at \d{2}:\d{2}, when every student's time is up\.$/),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reveal', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /release|show the full board/i })).toHaveCount(0);
  await expect(page.getByText(/release/i)).toHaveCount(0);
  expect(api.reveals).toBe(0);
});

test('similar code waits for the end, and no contest has an Integrity tab (V3-16)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByRole('heading', { name: 'Standings' })).toBeVisible();
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link')).toHaveText([/^Setup/, /^Live/, /^Results/, 'Student view']);
  await expect(page.getByRole('link', { name: 'Similar code' })).toHaveCount(0);
  // Without a held board there is nothing to reveal.
  await expect(page.getByRole('button', { name: 'Reveal', exact: true })).toHaveCount(0);
});

test('after the end, a held board gives the place only and says when it updates', async ({ page }) => {
  await mockApi(page, { phase: 'after', studentFrozen: true });
  await page.goto('/contests/week-6');
  await expect(
    page.getByText(/^You are \d+(st|nd|rd|th) of 28 so far\. Your final place shows after the reveal\.$/),
  ).toBeVisible();
  await expect(page.getByText(/points\.$/)).toHaveCount(0);
  await page.getByRole('link', { name: 'See the standings' }).click();
  await expect(page.getByText(/^Results from \d{2}:\d{2} are hidden until the organisers reveal them\./)).toBeVisible();
  await expect(
    page.getByText('The bar under a score is the share of points earned; a dash means not tried. Time is when the total was reached, from the start.', { exact: true }),
  ).toBeVisible();
  await page.getByText('How to read the board').click();
  await expect(page.getByText('counts submissions made while the newest results are hidden', { exact: false })).toBeVisible();
  // S6: the own place is the page's one display figure, labelled "so far" while results are hidden.
  await expect(page.getByText('Your place so far', { exact: true })).toBeVisible();
  const figure = page.getByText(/^\s*\d+ of 28$/);
  await expect(figure).toBeVisible();
  expect(await figure.getByText(/^\d+$/).evaluate((el) => getComputedStyle(el).fontSize)).toBe('44px');
  await expect(page.locator('tr.me th.name')).toContainText('Asha Rao (you)');
});

test('below 1024 px the clock sticks to the top, and time up shows on screen', async ({ page }) => {
  await mockApi(page, { phase: 'during', endsInMs: 4000 });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/contests/week-6');
  // P5 F7.2: the clock is in the workspace's sticky strip.
  const bar = page.locator('[data-sticky-bar]');
  await expect(bar.getByRole('timer')).toBeVisible();
  expect(await bar.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');
  await page.mouse.wheel(0, 600);
  await expect(bar.getByRole('timer')).toBeInViewport();
  await page.screenshot({ path: `${shots()}contest-clock-sticky-375-light.png` });
  // The window ends: the page moves to the results and keeps saying why.
  await expect(page.getByRole('status').filter({ hasText: 'Time is up. Your submissions are saved.' })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
});

test('a missing contest says so', async ({ page }) => {
  await mockApi(page);
  await page.goto('/contests/nope');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Contest not found');
});

test('keyboard only: a student starts, solves, submits, reads the verdict, asks and checks the standings', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Week 6 contest' })).toBeVisible();

  // The first Tab shows the skip link, and it moves focus into the page.
  await page.keyboard.press('Tab');
  const skip = page.getByRole('link', { name: 'Skip to content' });
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press('Enter');
  await expect(page.locator('main')).toBeFocused();

  // Contest list, then the rules and the start button.
  await activate(page, page.getByRole('link', { name: 'Week 6 contest' }));
  await expect(page).toHaveURL('/contests/week-6');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await activate(page, page.getByRole('button', { name: 'I agree, start the contest' }));
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');

  // "]" and "[" move between problems when nothing is being typed.
  await page.locator('main h1').focus();
  await page.keyboard.press(']');
  await expect(page).toHaveURL('/contests/week-6/problems/B');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await page.keyboard.press('[');
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await page.keyboard.press('[');
  await expect(page).toHaveURL('/contests/week-6/problems/A');

  // "?" opens Help with the shortcuts; Escape closes it and focus comes back.
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Help' });
  await expect(keys).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Solving a problem' })).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Reveal' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  // Read the statement: the sample copy buttons are reachable.
  await tabTo(page, page.getByRole('button', { name: /Copy example 1 input/i }));

  // Write code: Tab reaches the editor, typing "]" stays in the code, and
  // Escape then Tab leaves it.
  const code = codeEditor(page);
  await tabTo(page, code);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// a[i]?');
  await expect(code).toContainText('// a[i]?');
  await expect(page.getByRole('dialog', { name: 'Help' })).toBeHidden();
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  expect(await hasFocus(code)).toBe(false);

  // Run and submit from the keyboard.
  await code.focus();
  await page.keyboard.press("Control+'");
  await expect(page.getByRole('heading', { name: 'Run on the examples' })).toBeVisible();
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  const id = Number(api.submissions.at(-1)!.id);
  const judged = submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.scores.A = 40;
  api.stream.push({ type: 'submission', data: judged });
  const result = page.getByRole('region', { name: 'Result' });
  // Submit selects the Submissions tab (B2).
  await expect(result.getByRole('tabpanel', { name: /^Submissions/ }).getByText('Partial, 40 of 100', { exact: true }).first()).toBeVisible({ timeout: 10_000 });

  // Read the verdict: the failing subtask is open, the passing one opens with Enter.
  const passing = result.locator('details.group summary').filter({ hasText: 'Subtask 1' }).first();
  // Focus is still in the editor, where Tab indents: Escape first, then Tab on.
  await page.keyboard.press('Escape');
  await tabTo(page, passing);
  await page.keyboard.press('Enter');
  await expect(result.locator('details.group').filter({ hasText: 'Subtask 1' }).first()).toHaveAttribute('open', '');

  // Messages: open, ask, close; focus returns to the button.
  const messages = page.getByRole('button', { name: /Messages/ });
  // The rail comes before the heading, so Shift+Tab reaches it (forward,
  // Tab would enter the editor, where it indents until Escape).
  await page.locator('main h1').focus();
  await tabTo(page, messages, { back: true });
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Messages' });
  await expect(dialog).toBeVisible();
  await tabTo(page, dialog.getByLabel('Question', { exact: true }));
  await page.keyboard.type('Can a and b be negative?');
  await activate(page, dialog.getByRole('button', { name: 'Send question' }));
  await expect.poll(() => api.asked.length).toBe(1);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(messages).toBeFocused();

  // Standings: the button before Messages in the rail (forward, Tab would
  // have to cross the editor) opens the board over the editor (B3).
  const standingsButton = page.getByRole('button', { name: 'Standings' });
  await tabTo(page, standingsButton, { back: true });
  await page.keyboard.press('Enter');
  const board = page.getByRole('dialog', { name: 'Standings' });
  await expect(board.locator('tr.me')).toBeVisible();
  await tabTo(page, board.getByRole('region', { name: /^Standings for/ }));
  // Escape closes it: the same problem, focus back on the button.
  await page.keyboard.press('Escape');
  await expect(board).toBeHidden();
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await expect(standingsButton).toBeFocused();
});

test('closing the shortcuts list gives focus to the heading when the page loaded behind it', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');

  // Problem B answers only when the test says so, so "?" lands mid-load.
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/contests/week-6/problems/B', async (route) => {
    await held;
    await route.fallback();
  });
  await page.locator('main h1').focus();
  await page.keyboard.press(']');
  await expect(page).toHaveURL('/contests/week-6/problems/B');
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Help' });
  await expect(keys).toBeVisible();
  release();
  await expect(page.locator('main h1')).toHaveText('B. Interval covering');
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
});

test('keyboard only: an organiser edits a contest, runs the console and reveals the board', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'after', holdReveal: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  // An ended contest opens on Results (O1); its Setup is one tab away.
  await activate(page, page.getByRole('link', { name: 'Week 6 contest', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/results');
  await activate(page, page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: 'Setup' }));
  await expect(page).toHaveURL('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');

  // Add a problem, move it with the arrow buttons; focus never drops to the page.
  await tabTo(page, page.getByLabel('Add a validated problem'));
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await activate(page, page.getByRole('button', { name: 'Add problem' }));
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  await page.getByRole('button', { name: 'Move Interval covering up' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  // At the top the up arrow is off, so focus moved to the down arrow of the same problem.
  await expect(page.getByRole('button', { name: 'Move Interval covering down' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Move Interval covering up' })).toBeFocused();
  // Removing a problem hands focus to the row that took its place.
  // The problem is saved already and the contest is over, so removing it asks first.
  await page.getByRole('button', { name: 'Remove Interval covering' }).focus();
  await page.keyboard.press('Enter');
  const removeDialog = page.getByRole('dialog', { name: /^Remove problem [AB]\?$/ });
  await activate(page, removeDialog.getByRole('button', { name: 'Remove problem' }));
  await expect(page.getByLabel('Letter for Interval covering')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove A plus B' })).toBeFocused();

  // A setting under Advanced changed from the keyboard saves by itself; Advanced opens from its summary.
  await activate(page, page.locator('#advanced summary'));
  await tabTo(page, page.getByLabel('Wait between submissions, seconds'));
  await page.keyboard.press('Control+a');
  await page.keyboard.type('20');
  await expect.poll(() => api.settingsPuts.at(-1)?.min_gap_s).toBe(20);
  await expect(saveStatus(page)).toHaveText('Saved');

  // Live: the history of changes opens from the keyboard, and Escape hands focus back.
  await activate(page, page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: /^Live/ }));
  await expect(page).toHaveURL('/admin/contests/4/console');
  const history = page.getByRole('button', { name: 'History of changes' });
  await activate(page, history);
  const changes = page.getByRole('dialog', { name: 'History of changes' });
  await expect(changes).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(changes).toBeHidden();
  await expect(history).toBeFocused();

  // Results: Reveal and its confirmation from the keyboard, then Similar code.
  await page.locator('main h1').focus();
  await activate(page, page.getByRole('link', { name: 'Results', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/results');
  await activate(page, page.getByRole('button', { name: 'Reveal', exact: true }));
  const confirmReveal = page.getByRole('dialog', { name: 'Reveal the full board?' });
  await expect(confirmReveal.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expect.poll(() => hasFocus(confirmReveal)).toBe(true);
  await activate(page, confirmReveal.getByRole('button', { name: 'Reveal', exact: true }));
  await expect.poll(() => api.reveals).toBe(1);
  await expect(page.getByText(/^Revealed .+\. Students see the full board\.$/)).toBeVisible();
  await activate(page, page.getByRole('link', { name: 'Similar code' }));
  await expect(page).toHaveURL('/admin/contests/4/integrity');
});

// F7: stacked rows on a phone keep their table semantics, and the title
// link stretches over its row so the whole row opens the contest.
test('stacked rows stay table rows and the whole row is the link', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/');
  const table = page.getByRole('table');
  const rows = table.getByRole('row');
  await expect(rows.first()).toBeVisible();
  expect(await rows.count()).toBeGreaterThan(1);
  await expect(table.getByRole('rowheader').first()).toBeVisible();
  const row = rows.nth(1);
  const link = row.getByRole('link').first();
  const [rowBox, linkBox] = [await row.boundingBox(), await link.boundingBox()];
  expect(rowBox && rowBox.height >= 44).toBe(true);
  // A press near the row's bottom right corner, away from the title, follows the title link.
  const href = await link.getAttribute('href');
  expect(linkBox).not.toBeNull();
  await page.mouse.click((rowBox?.x ?? 0) + (rowBox?.width ?? 0) - 8, (rowBox?.y ?? 0) + (rowBox?.height ?? 0) - 8);
  await expect(page).toHaveURL(new RegExp(`${href}$`));
});

test('publishing a draft asks first, listing the checks, and a refusal names the reason (P3 O1)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.goto('/admin/contests/4');
  await expect(page.getByText(/^Not published\. 1 problem, starts/)).toBeVisible();
  await page.getByRole('button', { name: 'Publish contest' }).click();
  const publish = page.getByRole('dialog', { name: 'Publish Week 6 contest?' });
  await expect(publish).toContainText('Students from the allowed domains see it in their contest list at once.');
  await expect(publish).toContainText('1 problem, all validated.');
  await publish.getByRole('button', { name: 'Publish contest' }).click();
  await expect(page.locator('#publish-error')).toHaveText('Problem B changed or lost its validation since it was added. Validate it and add it again.');
});

test('an unpublished contest past its start is a draft, not live (C2-015)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during', published: false });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByText('Draft', { exact: true })).toBeVisible();
  await expect(page.getByText('This contest is live.', { exact: false })).toHaveCount(0);
  await expect(page.getByRole('note').filter({ hasText: 'The start time has passed.' })).toHaveText(
    'The start time has passed. Students cannot see this contest until you publish it, so move the start or publish now.',
  );
  await expect(page.getByRole('region', { name: 'Before you publish' })).toContainText('The start time has passed. Move it ahead, or students start at once.');
  // Nobody can see it, so a time change saves without the live warning.
  await page.getByLabel('Hidden minutes at the end').fill('20');
  await expect.poll(() => api.settingsPuts.length).toBe(1);
  await expect(page.getByRole('dialog', { name: 'Change a live contest?' })).toHaveCount(0);
  await expect.poll(() => api.settingsPuts.at(-1)?.freeze_minutes).toBe(20);
});

test('an unpublished contest reads Draft, and keeping results hidden needs hidden minutes (P3 O1, O8)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.goto('/admin/contests/4');
  await expect(page.getByText('Draft', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Before you publish' })).toContainText('1 problem, all validated.');
  await page.locator('#advanced summary').click();
  const hold = page.getByLabel('Keep the hidden results hidden after the end until you reveal them');
  await expect(hold).toBeEnabled();
  await page.getByLabel('Hidden minutes at the end').fill('0');
  await expect(hold).toBeDisabled();
  await expect(page.getByText('Needs hidden minutes at the end; with none there is nothing to reveal.')).toBeVisible();
});

test('a save that fails says so, keeps the page on a link or Back, and Try again saves it (C3, F8)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.goto('/admin/contests');
  await page.getByRole('link', { name: 'Week 6 contest', exact: true }).click();
  await expect(page).toHaveURL('/admin/contests/4');
  await page.route('**/api/admin/contests/4', (route) =>
    route.request().method() === 'PUT' ? route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'The server is busy. Try again in a moment.' } } }) : route.fallback(),
  );
  await page.getByLabel('Title', { exact: true }).fill('Week 6 final');
  await expect(saveStatus(page)).toHaveText('Not saved.');
  await expect(page.getByRole('alert').filter({ hasText: 'The server is busy.' })).toBeVisible();

  // A link asks first.
  await page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: /^Live/ }).click();
  const leave = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(leave).toContainText('Problems are saved already.');
  await leave.getByRole('button', { name: 'Cancel' }).click();
  await expect(page).toHaveURL('/admin/contests/4');

  // So does Back.
  await page.goBack();
  await expect(leave).toBeVisible();
  await expect(page).toHaveURL('/admin/contests/4');
  await leave.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 final');

  await page.unroute('**/api/admin/contests/4');
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(saveStatus(page)).toHaveText('Saved');
  expect(api.settingsPuts.at(-1)).toEqual({ title: 'Week 6 final' });
});

test('setup saves as you type: a title changed survives a reload, with no Save button (C3)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Title', { exact: true }).fill('Week 6 practice round');
  await expect(page.getByText('Saved', { exact: true })).toHaveCount(1);
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ title: 'Week 6 practice round' });
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 practice round');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Week 6 practice round');
  await expect(page.getByRole('button', { name: /^Save/ })).toHaveCount(0);
  // The length is what is typed; the end follows it.
  await page.getByLabel('Length in minutes').fill('90');
  await expect.poll(() => {
    const b = api.settingsPuts.at(-1) ?? {};
    return Number(b.ends_at) - Number(b.starts_at);
  }).toBe(90 * 60_000);
});

test('setup at 1440 px shows six inputs and folds the rest, under 1,200 px tall (C3)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await page.evaluate(() => document.fonts.ready);
  for (const name of ['Title', 'Add a validated problem', 'Starts', 'Length in minutes', 'Hidden minutes at the end']) {
    await expect(page.getByRole('form', { name: 'Contest setup' }).getByLabel(name, { exact: true })).toBeVisible();
  }
  await expect(page.getByRole('group', { name: 'Languages' })).toBeVisible();
  const advanced = page.locator('#advanced');
  await expect(advanced).not.toHaveAttribute('open');
  for (const name of ['Wait between submissions, seconds', 'Submissions per problem', 'Description (Markdown)', 'Honour code (Markdown)']) {
    await expect(page.getByLabel(name, { exact: true })).toBeHidden();
  }
  // Duplicate and Archive sit in one menu beside Publish.
  await expect(page.getByRole('button', { name: 'Publish contest' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Duplicate' })).toBeHidden();
  await page.getByRole('button', { name: 'More' }).click();
  await expect(page.getByRole('button', { name: 'Duplicate' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Archive' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'More' })).toBeFocused();
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  test.info().annotations.push({ type: 'setup height at 1440 px', description: String(height) });
  expect(height).toBeLessThan(1200);
});

test('Duplicate asks for the new start and opens the copy (C3)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  const clones: unknown[] = [];
  await page.route('**/api/admin/contests/4/clone', async (route) => {
    clones.push(route.request().postDataJSON());
    await route.fulfill({ status: 201, json: { id: 9, slug: 'week-6-contest-2', title: 'Week 6 contest', state: 'upcoming', published: false, problems: [] } });
  });
  await page.goto('/admin/contests/4');
  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('button', { name: 'Duplicate' }).click();
  const dialog = page.getByRole('dialog', { name: 'Duplicate Week 6 contest' });
  await expect(dialog).toContainText('A new draft with the same setup, problems and length, titled Week 6 contest (copy).');
  await dialog.getByLabel('Starts').fill('2026-12-01T18:00');
  await dialog.getByRole('button', { name: 'Duplicate contest' }).click();
  await expect(page).toHaveURL('/admin/contests/9');
  const body = clones[0] as { starts_at: number; ends_at: number };
  expect(body.ends_at - body.starts_at).toBe(2 * 3_600_000);
  await expect(toasts(page).getByText('Duplicated Week 6 contest as a draft', { exact: true })).toBeVisible();
});

test('the contests home puts live and next first, and Needs you waits until a contest has run (V3-20)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during', staleDraft: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  const live = page.getByRole('region', { name: 'Week 6 contest' });
  await expect(live).toContainText('Ends in');
  await expect(live.getByRole('link', { name: 'Open live view' })).toHaveAttribute('href', '/admin/contests/4/console');
  const needs = page.getByRole('region', { name: 'Needs you' });
  await expect(needs.getByRole('link', { name: '1 unanswered question in Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4/console#clar-title');
  await expect(needs.getByRole('link', { name: /^Problem A in Week 7 contest changed/ })).toHaveAttribute('href', '/admin/contests/5');
  // Drafts and the rest share one table; a draft opens on Setup.
  await expect(page.getByRole('row', { name: /Week 7 contest/ }).getByRole('link')).toHaveAttribute('href', '/admin/contests/5');
  // Live and next come before the table.
  const order = await page.evaluate(() => {
    const block = document.querySelector('.next-list');
    const table = document.querySelector('.list');
    return !!block && !!table && !!(block.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  expect(order).toBe(true);
});

test('before any contest has run there is no Needs you, and every title opens the tab that fits (O1)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming', published: false, staleDraft: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await expect(page.getByRole('row', { name: /Week 6 contest/ })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Needs you' })).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Week 6 contest/ }).getByRole('link')).toHaveAttribute('href', '/admin/contests/4');

  await mockApi(page, { who: 'organiser', phase: 'after' });
  await page.goto('/admin/contests');
  await expect(page.getByRole('row', { name: /Week 6 contest/ }).getByRole('link')).toHaveAttribute('href', '/admin/contests/4/results');
  // The trail's contest step follows the same rule.
  await page.goto('/admin/contests/4/console');
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Week 6 contest' })).toHaveAttribute('href', '/admin/contests/4/results');
});

test('the contest list stacks below 1024 px and points to the bank when empty (P3 R1, O6)', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 900, height: 900 });
  await page.goto('/admin/contests');
  const row = page.getByRole('row', { name: /Week 7 contest/ });
  await expect(row).toBeVisible();
  await expect(page.locator('.list thead')).toHaveCSS('position', 'absolute');
  await expect(row).toHaveCSS('display', 'grid');
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);

  await page.route('**/api/admin/contests', (route) => route.fulfill({ json: { contests: [], server_now: Date.now() } }));
  await page.reload();
  await expect(page.getByText('No contests yet. Contests use validated problems from the problem bank.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Go to the problem bank' })).toHaveAttribute('href', '/admin/problems');
});

test('Student view shows the problems and the board as students see them, with no start page (F3.3, C2-056, P10)', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const link = page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: 'Student view' });
  await expect(link).toHaveAttribute('href', '/contests/week-6');
  await link.click();
  await expect(page).toHaveURL('/contests/week-6');
  await expect(page.getByText('Student view: the problems and the standings as students see them.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'I agree, start the contest' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Honour code' })).toHaveCount(0);
  // The board students get: during the hidden minutes, the hidden one.
  await page.getByRole('main').getByRole('link', { name: 'Standings' }).click();
  await expect(page.getByText('Student view: the board as students see it now.')).toBeVisible();
  await expect(page.getByText(/^Results from \d{2}:\d{2} are hidden until the end\./)).toBeVisible();
  expect(api.boardQueries).toContain('?as=student');
  // A problem opens in the workspace, saying nothing submitted here counts.
  await page.goto('/contests/week-6');
  await page.getByRole('link', { name: 'A. A plus B' }).click();
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await expect(page.getByText('Student view. Run and Submit work here, and nothing you submit counts.')).toBeVisible();
});
