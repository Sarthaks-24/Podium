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
// during and after a contest, exam mode, the frozen and the closed board,
// the editor, the console and the reveal.
import { expect, test } from '@playwright/test';
import { activate, hasFocus, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, submission, hidden, mockApi, settle, SCREENS } from './mocks/contests';

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

test('the contest list puts the live contest first with a countdown', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/');
  // S10: the live contest is pinned on top with its countdown and one button.
  const pinned = page.getByRole('region', { name: 'Week 6 contest' });
  await expect(pinned).toContainText('Live');
  await expect(pinned.locator('.pinned-count')).toHaveText(/^Ends in\s*\d{2}:\d{2}:\d{2}$/);
  await expect(pinned.getByRole('link', { name: 'Continue' })).toHaveAttribute('href', '/contests/week-6');
  await expect(pinned.getByRole('link', { name: 'Continue' })).toHaveClass(/btn-primary/);
  await expect(page.locator('.btn-primary')).toHaveCount(1);
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('Upcoming');
  await expect(rows.nth(1)).toContainText('240 of 300');
  await expect(rows.nth(1)).toContainText('Ended');
  await expect(rows.nth(0).locator('.countdown')).toHaveText(/^Starts in \d d( \d+ h)?$/);
  // Phones keep the score, on the line under the start.
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(rows.nth(1).getByText('240 of 300')).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  // Students get Contests and Your progress (P4).
  await expect(page.getByRole('link', { name: 'Contests', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link')).toHaveText(['Contests', 'Your progress']);
  await expect(page.getByRole('link', { name: 'Problem bank' })).toHaveCount(0);
});

test('messages posted while the student was away still show, until read (S1)', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during' });
  const before = Date.now() - 5 * 60_000;
  api.announcements.push({ id: 5, body_md: 'Sample 2 of B is fixed.', created_at: before });
  api.clarifications.push({ id: 9, problem_label: 'A', question: 'Can a be negative?', answer: 'No, a is at least 0.', public: false, created_at: before - 60_000, answered_at: before + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(page.locator('.banner-slot .strip')).toContainText('No, a is at least 0.');
  await expect(page.locator('.banner-slot .strip')).toContainText('1 more message waiting.');
  await expect(rail.getByRole('button', { name: /Messages/ })).toContainText('2');
  await rail.getByRole('button', { name: /Messages/ }).click();
  await expect(page.getByRole('dialog', { name: 'Messages' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.reload();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  await expect(page.locator('.banner-slot .strip')).toHaveCount(0);
  await expect(rail.getByRole('button', { name: /Messages/ })).not.toContainText('2');
});

test('a student agrees to the honour code, solves, reads an announcement and asks a question', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByRole('link', { name: 'Week 6 contest' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Week 6 contest');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await expect(page.getByRole('heading', { name: 'Rules' })).toBeVisible();
  await expect(page.getByText('You can submit up to 15 times per problem, at least 30 s apart.')).toBeVisible();
  await expect(page.getByText('It is deleted 180 days after the contest.', { exact: false })).toBeVisible();
  await expect(page.getByText(/^The contest started at \d{2}:\d{2} and your time is running\. It ends in/)).toBeVisible();
  // S3: the workspace is taught before the clock starts, with the real verdict pieces.
  const how = page.getByRole('region', { name: 'How the workspace works' });
  await expect(how.locator('details.how')).toHaveAttribute('open', '');
  await expect(how.getByText('Partial, 40 of 100')).toBeVisible();
  await expect(how.locator('.strip')).toHaveAttribute('aria-label', 'Subtask 1, 40 points, passed; Subtask 2, 60 points, failed');

  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await expect.poll(() => api.starts).toBe(1);
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(rail.locator('.problems').getByRole('link')).toHaveCount(3);
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, not tried' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');
  await expect(page.getByRole('timer')).toHaveText(/^\d{2}:\d{2}:\d{2}$/);
  await expect(page.getByText(/^Standings freeze in/)).toBeVisible();
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
  await expect(page.getByRole('region', { name: 'Result' }).locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, 40 of 100 points' })).toBeVisible({ timeout: 10_000 });

  // An announcement arrives as a quiet banner, not an alert.
  api.announcements.push({ id: 1, body_md: 'Problem B: the statement has been corrected.', created_at: Date.now() + 1000 });
  api.stream.push({ type: 'announcement', data: { contest: 4, id: 1, body_md: 'Problem B: the statement has been corrected.' } });
  const banner = page.locator('.banner-slot .strip').filter({ hasText: 'Problem B: the statement has been corrected.' });
  await expect(banner).toBeVisible({ timeout: 10_000 });
  // X1: spoken once through the polite announcer.
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Announcement: Problem B: the statement has been corrected.');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(rail.getByRole('button', { name: /Messages/ })).toContainText('1');
  await page.screenshot({ path: `${shots()}contest-banner-1440-light.png` });

  // Open the messages and ask about problem B.
  await page.getByRole('button', { name: 'Show all messages' }).click();
  const dialog = page.getByRole('dialog', { name: 'Messages' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Problem B: the statement has been corrected.')).toBeVisible();
  await expect(dialog.getByLabel('About')).toHaveValue('A');
  await dialog.getByLabel('About').selectOption('B');
  await dialog.getByRole('button', { name: 'Send question' }).click();
  await expect(dialog.getByText('Write your question first.')).toBeVisible();
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
  expect(order[1]).toBeLessThan(order[2]);
});

test('a window-mode contest starts the clock on agreement', async ({ page }) => {
  const api = await mockApi(page, { phase: 'ready', startMode: 'window' });
  await page.goto('/contests/week-6');
  await expect(page.getByText('Your clock starts when you agree and start, and runs 1 h', { exact: false })).toBeVisible();
  await expect(page.getByText('The standings open when the contest closes.')).toBeVisible();
  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('timer')).toHaveText(/^00:(39|40):\d{2}$/);
  // Window mode has no board to freeze, so no freeze line.
  await expect(page.getByText(/Standings freeze/)).toHaveCount(0);
});

test('exam mode shows only "Submitted"', async ({ page }) => {
  const api = await mockApi(page, { phase: 'during', feedback: 'exam' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('.cm-content')).toBeVisible();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.getByText('Submitted', { exact: true }).first()).toBeVisible();
  await expect(result.getByText('Scores appear after the contest.')).toBeVisible();
  const id = Number(api.submissions.at(-1)!.id);
  // Even if a full result slipped through, exam mode shows nothing more.
  api.stream.push({ type: 'submission', data: hidden(id, 11) });
  api.stream.push({ type: 'submission', data: submission(id, 11, 'done', 40) });
  // Both events were delivered (the mock sends one per connection) before checking that nothing more shows.
  await expect.poll(() => api.stream.length, { timeout: 15_000 }).toBe(0);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
  await expect(result.getByText(/Partial|Wrong answer|Accepted|40 of 100/)).toHaveCount(0);
  await expect(result.locator('.strip')).toHaveCount(0);
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Submitted.');
  const rail = page.getByRole('navigation', { name: 'Problems' });
  await expect(rail.getByRole('link', { name: 'Problem A, A plus B, submitted once, score after the contest' })).toBeVisible();
  await expect(rail.getByRole('link', { name: /Problem A/ }).locator('.word')).toHaveText('submitted');
  await expect(rail.getByRole('link', { name: 'Problem B, Interval covering, not submitted' })).toBeVisible();
  await page.screenshot({ path: `${shots()}contest-exam-1440-light.png` });
});

test('after the contest: result, submissions, editorial and practice', async ({ page }) => {
  const api = await mockApi(page, { phase: 'practice' });
  api.clarifications.push({ id: 9, problem_label: 'B', question: 'Can intervals touch?', answer: 'Yes, touching counts as covered.', public: false, created_at: Date.now() - 3 * 3_600_000, answered_at: Date.now() - 3 * 3_600_000 + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  const questions = page.getByRole('region', { name: 'Your questions' });
  await expect(questions).toContainText('Can intervals touch?');
  await expect(questions).toContainText('Yes, touching counts as covered.');
  await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
  // S9: one lead, the place as the display figure, no repeated sentence.
  await expect(page.locator('.result-lead')).toHaveText(/^Ended \d+ [A-Z][a-z]{2}, \d{2}:\d{2}\. You scored 140 of 300\.$/);
  await expect(page.locator('.result .place-figure')).toHaveText(/^\s*\d+ of 28$/);
  await expect(page.getByText(/You are \d+/)).toHaveCount(0);
  await expect(page.getByText('Practice is open: new submissions do not count.')).toBeVisible();
  await expect(page.getByText('Use 64-bit integers.')).toBeVisible();
  // The problem with the newest submission opens by itself.
  await expect(page.locator('details[open] summary')).toHaveText(['Problem B: Interval covering']);
  await expect(page.locator('details[open]').getByText('40 of 100').first()).toBeVisible();

  await page.getByRole('link', { name: 'Practise problem B' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('B. Interval covering');
  await expect(page.getByText('Practice is open. Submissions you make now do not count towards the standings.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeEnabled();
});

test('after the contest the page keeps answers for everyone, in the dialog copy (C1-010)', async ({ page }) => {
  const api = await mockApi(page, { phase: 'after' });
  api.clarifications.push({ id: 9, problem_label: 'B', question: 'Is n at most 10^5?', answer: null, public: false, created_at: Date.now() - 3 * 3_600_000 });
  api.publicClarifications.push({ id: 10, problem_label: 'A', question: 'May the sum overflow?', answer: 'Use 64-bit integers for the sum.', public: true, created_at: Date.now() - 2 * 3_600_000, answered_at: Date.now() - 2 * 3_600_000 + 60_000 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
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
  await expect(page.getByText(/^Frozen at \d{2}:\d{2}\. Updates when the contest ends\. 2 submissions are still being judged\.$/)).toBeVisible();
  const mine = page.locator('tr.me');
  await expect(mine).toContainText('Asha Rao');
  await expect(mine).toContainText('(you)');
  await expect(mine.locator('.pending')).toHaveText('+2');
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
  await expect(page.locator('.tools')).toHaveClass(/away/);
  await expect(page.locator('tr.me .name-text')).toHaveText(/Asha Rao\s\(you\)/);

  // The header stays put while rows scroll.
  const head = page.locator('.board-wrap thead th').first();
  expect(await head.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');

  // On a phone the rank and name stay pinned while the problems scroll.
  await page.setViewportSize({ width: 375, height: 812 });
  const wrap = page.locator('.board-wrap');
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
  const wrap = page.locator('.board-wrap');
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

test('a window-mode board stays closed until the window closes', async ({ page }) => {
  await mockApi(page, { phase: 'during', startMode: 'window', boardClosed: true });
  await page.goto('/contests/week-6/standings');
  await expect(page.getByText('The standings open when the contest closes.')).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);
});

test('a second tab with the same contest shows a notice, as does a second session', async ({ page, context, browser }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('.cm-content')).toBeVisible();
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
  await mockApi(page, { who: 'admin', phase: 'during' });
  await page.goto('/');
  await expect(page).toHaveURL('/admin/contests');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Problem bank', 'Students', 'Inbox']);
  await expect(nav.getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByText('To create a contest or follow one live')).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Week 7 contest/ })).toContainText('Draft');
  // A published live contest reads by its state; there is no separate visibility column (P3 O1).
  await expect(page.getByRole('row', { name: /Week 6 contest/ }).locator('.state-word')).toHaveText(/^(Live|Frozen)$/);

  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title').fill('Week 8 contest');
  await expect(page.getByLabel('Slug')).toHaveValue('week-8-contest');
  await page.getByRole('button', { name: 'Create contest' }).click();
  await expect(page).toHaveURL('/admin/contests/9');
});

test('a new contest marks its required fields, and a field error from the server takes focus (X7)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'upcoming' });
  await page.goto('/admin/contests');
  await page.getByRole('button', { name: 'New contest' }).click();
  const form = page.locator('#create-contest');
  await expect(form.getByText('All fields are required.')).toBeVisible();
  for (const name of ['Title', 'Slug', 'Starts', 'Ends']) await expect(form.getByLabel(name, { exact: true })).toHaveAttribute('aria-required', 'true');
  await form.getByLabel('Title', { exact: true }).fill('Week 6');
  await form.getByLabel('Starts', { exact: true }).fill('2026-11-01T18:00');
  await form.getByLabel('Ends', { exact: true }).fill('2026-11-01T20:00');
  await page.getByRole('button', { name: 'Create contest' }).click();
  const slug = form.getByLabel('Slug', { exact: true });
  await expect(slug).toBeFocused();
  await expect(slug).toHaveAttribute('aria-invalid', 'true');
  await expect(page.locator('#new-slug-error')).toHaveText('Another contest uses this slug. Choose another.');
});

test('students cannot open the contest admin', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/admin/contests');
  await expect(page.getByText('Only organisers can manage contests.')).toBeVisible();
  // F3: a closed screen names where to ask, as a mailto link.
  await expect(page.getByRole('main').getByRole('link', { name: 'priya@college.edu' })).toHaveAttribute('href', 'mailto:priya@college.edu');
});

test('the Console link counts unanswered questions on Settings (O4)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'during' });
  const asked: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/clarifications')) asked.push(new URL(r.url()).pathname);
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  const nav = page.getByRole('navigation', { name: 'Contest' });
  await expect(nav.getByRole('link', { name: /Console/ })).toContainText('1');
  await expect(nav.getByRole('link', { name: /Console/ })).toContainText('unanswered question');
  expect(asked).toContain('/api/admin/contests/4/clarifications');
  expect(asked.filter((p) => !p.startsWith('/api/'))).toEqual([]);
});

test('hidden tests shown in practice are named, and grading off after the start asks first (C1-015)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', published: false, shownInPractice: [11, 12], publishOk: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByLabel('Add a validated problem').locator('option', { hasText: 'Interval covering' })).toHaveText(
    'Interval covering (100 points), hidden tests shown in practice',
  );
  await page.getByRole('button', { name: 'Publish contest' }).click();
  const publish = page.getByRole('dialog', { name: 'Publish Week 6 contest?' });
  await expect(publish).toContainText('in an ungraded contest or in practice, so students may know the hidden tests.');
  await publish.getByRole('button', { name: 'Cancel' }).click();

  await page.getByText('Rules and scoring').click();
  await page.getByRole('checkbox', { name: /^Graded:/ }).uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  const ask = page.getByRole('dialog', { name: 'Turn grading off in a started contest?' });
  await expect(ask).toContainText('sees the first hidden test each later submission fails');
  await ask.getByRole('button', { name: 'Turn grading off' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)).toMatchObject({ graded: false, confirm_ungraded: true });

  // The bank marks the same problems.
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /Interval covering/ });
  await expect(row.getByText('Hidden tests shown in practice')).toBeVisible();
  await expect(page.getByRole('row', { name: /Unfinished draft/ }).getByText('Hidden tests shown in practice')).toHaveCount(0);
});

test('grading back on after an ungraded start asks first (C2-014)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', ungraded: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.getByText('Rules and scoring').click();
  await page.getByRole('checkbox', { name: /^Graded:/ }).check();
  await page.getByRole('button', { name: 'Save changes' }).click();
  const ask = page.getByRole('dialog', { name: 'Turn grading on in a started contest?' });
  await expect(ask).toContainText('Students have already seen hidden tests in this contest');
  await ask.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await ask.getByRole('button', { name: 'Turn grading on' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)).toMatchObject({ graded: true, confirm_graded: true });
});

test('the editor shows field errors, picks problems and previews a roster', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByRole('note').filter({ hasText: 'This contest is live.' })).toBeVisible();
  await expect(page.getByRole('note').getByRole('link', { name: 'console' })).toHaveAttribute('href', '/admin/contests/4/console');

  // A refused slug shows under the field and takes focus.
  await page.getByLabel('Slug', { exact: true }).fill('Week 6');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const slug = page.getByLabel('Slug', { exact: true });
  await expect(page.locator('#f-slug-error')).toHaveText('Use lowercase letters, digits and single hyphens, like week-6.');
  await expect(slug).toHaveAttribute('aria-invalid', 'true');
  await expect(slug).toBeFocused();

  await slug.fill('week-6');
  await page.getByText('Rules and scoring').click();
  await page.getByLabel('Python 3').uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved changes', { exact: true })).toBeVisible();
  expect(api.settingsPuts.at(-1)).toMatchObject({ slug: 'week-6', languages: ['cpp17'], published: true, freeze_minutes: 30 });

  // Changing the freeze of a live contest asks first.
  const puts = api.settingsPuts.length;
  await page.getByLabel('Freeze the board, minutes before the end').fill('20');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const live = page.getByRole('dialog', { name: 'Change a live contest?' });
  await expect(live.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await live.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts.length).toBe(puts);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await live.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)?.freeze_minutes).toBe(20);

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
  await expect(page.locator('.toasts').getByText('Added Interval covering as B', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save problems' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 12, label: 'A' },
    { problem_id: 11, label: 'B' },
  ]);

  // The roster preview names every refused line before anything is sent.
  await page.getByLabel('Or paste the CSV').fill('email,name,roll number,section\nravi@college.edu,Ravi Kumar,21CS002,B\nnot-an-email,Nobody\nravi@college.edu,Again\n');
  await expect(page.getByText('1 line ready, 2 refused.', { exact: false })).toBeVisible();
  await expect(page.getByText('The file replaces the whole roster: it adds 1 and removes 1 student.', { exact: false })).toBeVisible();
  const preview = page.getByRole('table', { name: 'Roster preview' });
  await expect(preview.getByRole('row')).toHaveCount(4);
  await expect(preview.getByRole('row', { name: /not-an-email/ })).toContainText('Not an email address');
  await expect(preview.getByRole('row', { name: /Again/ })).toContainText('Listed twice');
  await page.screenshot({ path: `${shots()}contest-editor-roster-1440-light.png`, fullPage: true });
  await page.getByRole('button', { name: 'Replace roster' }).click();
  const replace = page.getByRole('dialog', { name: 'Replace the roster?' });
  await expect(replace).toContainText('1 student now, 1 after. The 1 not in the file lose access and are taken out of the contest at once.');
  await expect(replace.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await replace.getByRole('button', { name: 'Replace roster' }).click();
  await expect.poll(() => api.rosterPuts.length).toBe(1);
  expect(api.rosterPuts[0]).toBe('email,name,roll_number,section\nravi@college.edu,Ravi Kumar,21CS002,B\n');
  await page.getByText('Show the current roster').click();
  await expect(page.getByText('asha@college.edu')).toBeVisible();

  await page.getByRole('button', { name: 'Remove Meera Iyer' }).click();
  const remove = page.getByRole('dialog', { name: 'Remove Meera Iyer?' });
  await remove.getByRole('button', { name: 'Remove Meera Iyer' }).click();
  await expect.poll(() => api.adminDeletes).toEqual([{ email: 'ta@college.edu' }]);
});

test('the console pauses, extends, judges again, announces and answers', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const strip = page.getByRole('region', { name: 'Needs attention' });
  await expect(strip).toContainText('Judging on');

  // Pausing asks first, then leaves an announcement ready to post (C3); it is never posted by itself.
  await strip.getByRole('button', { name: 'Pause judging' }).click();
  const dialog = page.getByRole('dialog', { name: 'Pause judging?' });
  await expect(dialog).toContainText('their submissions wait in the queue and nothing is lost');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Pause judging' }).click();
  await expect(strip).toContainText('Judging paused');
  await expect.poll(() => api.pauses).toBe(1);
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  expect(api.posted).toHaveLength(0);
  await strip.getByRole('button', { name: 'Resume judging' }).click();
  await expect(strip).toContainText('Judging on');

  await expect(strip.getByRole('link', { name: '1 unanswered question', exact: true })).toHaveAttribute('href', '#clar-title');
  await page.getByLabel('Minutes for everyone').fill('0');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  await expect(page.locator('#extend-error')).toHaveText('Enter the minutes to add, 1 or more.');
  await expect(page.getByLabel('Minutes for everyone')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Minutes for everyone').fill('15');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  const extend = page.getByRole('dialog', { name: 'Extend the contest by 15 minutes?' });
  await expect(extend).toContainText("Everyone's contest then ends at");
  await extend.getByRole('button', { name: 'Extend contest' }).click();
  await expect.poll(() => api.extends).toEqual([{ minutes: 15 }]);
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue('The contest has been extended by 15 minutes.');

  await page.locator('#ext-user').selectOption({ label: 'Asha Rao' });
  await page.getByLabel('Extra minutes').fill('10');
  await page.getByRole('textbox', { name: 'Reason' }).fill('Laptop crashed');
  await page.getByRole('button', { name: 'Give extra time' }).click();
  await expect.poll(() => api.extensions).toEqual([{ user: '7', body: { minutes: 10, reason: 'Laptop crashed' } }]);
  await expect(page.locator('.extensions')).toContainText('Asha Rao: 10 min, Laptop crashed');

  // Filters go to the feed's query.
  await page.locator('#f-problem').selectOption('A');
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?problem=A&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: /^Judge again: Arjun Mehta, problem A/ }).click();
  await expect.poll(() => api.rejudges).toEqual([{ submission_id: 300 }]);
  await page.getByLabel('Judge every submission to a problem again').selectOption('A');
  await page.getByRole('button', { name: 'Judge problem again' }).click();
  await page.getByRole('dialog', { name: 'Judge problem A again?' }).getByRole('button', { name: 'Judge again' }).click();
  await expect.poll(() => api.rejudges.at(-1)).toEqual({ problem_label: 'A' });
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(/Every submission to problem A is being judged again/);

  // Posting shows a preview and asks first (C3).
  await page.getByLabel('Start from a template').selectOption('paused');
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  await expect(page.getByRole('group', { name: 'Preview' })).toContainText('Judging is paused for a few minutes');
  await page.getByRole('button', { name: 'Post announcement' }).click();
  const post = page.getByRole('dialog', { name: 'Post to every student?' });
  await post.getByRole('button', { name: 'Post announcement' }).click();
  await expect.poll(() => api.posted.length).toBe(1);
  // Closing hands focus back to the opener; wait so it does not land later.
  await expect(post).toBeHidden();

  // A standard answer from the one list, answered for everyone.
  await page.getByLabel('Insert a standard answer').selectOption({ label: 'Yes.' });
  await expect(page.getByLabel('Answer', { exact: true })).toHaveValue('Yes.');
  await expect(page.getByLabel('Answer', { exact: true })).toBeFocused();
  await page.getByLabel('Answer for everyone').check();
  await page.getByRole('button', { name: 'Send to everyone' }).click();
  await expect.poll(() => api.answers).toEqual([{ id: '51', body: { answer: 'Yes.', public: true } }]);

  // The audit log follows every action, newest first, with who did it.
  const audit = page.getByRole('region', { name: 'Changes' });
  await expect(audit.getByRole('listitem').first()).toContainText('Judged problem A again (1 submission)');
  // v1.4.0 rows carry reason_given, never the reason; the reason stays in Time.
  await expect(audit).toContainText('Gave Asha Rao 10 extra minutes, with a reason (see Time)');
  await expect(audit).toContainText('Extended the contest by 15 minutes');
  await expect(audit).toContainText('Paused judging');
  await expect(audit).toContainText('priya@college.edu');
  await page.screenshot({ path: `${shots()}contest-console-1440-light.png`, fullPage: true });
});

test('the console says partial scores as students read them and keeps rows when a refresh fails (P3 O3, C2)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.clock.install();
  await page.goto('/admin/contests/4/console');
  const feed = page.locator('.feed tbody');
  await expect(feed.getByRole('row', { name: /Asha Rao/ })).toContainText('Partial, 40 of 100');
  await expect(feed.getByRole('row', { name: /Kiran Das/ })).toContainText('Wrong answer');
  await expect(feed.getByRole('row', { name: /Arjun Mehta/ })).toContainText('Accepted');
  // Minutes from the start, with the clock time on hover.
  await expect(feed.getByRole('row', { name: /Asha Rao/ }).locator('.when')).toHaveText(/^\+\d+ min$/);
  await expect(feed.getByRole('row', { name: /Asha Rao/ }).locator('.when')).toHaveAttribute('title', /^\d\d:\d\d$/);

  await page.route('**/api/admin/contests/4/submissions*', (route) => route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'Down.' } } }));
  await page.clock.runFor(10_500);
  const strip = page.getByRole('region', { name: 'Needs attention' });
  await expect(strip).toContainText(/Could not refresh at \d\d:\d\d:\d\d\. Showing \d\d:\d\d:\d\d\. Retrying\./);
  await expect(feed.getByRole('row')).toHaveCount(5);
});

test('the attention strip shows queue health, a silent judge and toolchain drift (P3 F6.1, C10)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', queueHealth: 'trouble' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const strip = page.getByRole('region', { name: 'Needs attention' });
  await expect(strip.getByRole('link', { name: '14 waiting, 2 judging, oldest 1 min' })).toBeVisible();
  await expect(strip.locator('.tone-bad')).toContainText(['1 judge error', 'No judge for 3 min', '1 unanswered question']);
  await expect(strip.locator('.tone-part')).toContainText(['14 waiting', 'Problem A was validated on another judge toolchain']);
  // The judge error link filters the feed to judge errors.
  await strip.getByRole('link', { name: '1 judge error' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=error&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await page.screenshot({ path: `${shots()}contest-console-trouble-1440-light.png`, fullPage: true });
});

test('End the contest now says what happens and sets the end to now (P3 O5)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await page.getByRole('button', { name: 'End the contest now' }).click();
  const end = page.getByRole('dialog', { name: 'End the contest now?' });
  await expect(end).toContainText("Everyone's time ends now");
  await expect(end).toContainText('Queued submissions are still judged.');
  await expect(end).toContainText('Students see Ended, and practice opens.');
  await expect(end.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await end.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  const before = Date.now();
  await page.getByRole('button', { name: 'End the contest now' }).click();
  await end.getByRole('button', { name: 'End the contest' }).click();
  await expect.poll(() => api.settingsPuts.length).toBe(1);
  const body = api.settingsPuts[0];
  // Every other setting is sent as it was; only the end moves to now.
  expect(body).toMatchObject({ slug: 'week-6', freeze_minutes: 30, languages: ['cpp17', 'python3'], published: true });
  expect(Math.abs(Number(body.ends_at) - before)).toBeLessThan(15_000);
  await expect(page.locator('.toasts').getByText('Ended the contest', { exact: true })).toBeVisible();
});

test('unpublishing a live contest asks first', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', published: true });
  await page.goto('/admin/contests/4');
  await expect(page.locator('.publish-line')).toContainText('Published and live.');
  await page.getByRole('button', { name: 'Unpublish contest' }).click();
  const unpublish = page.getByRole('dialog', { name: 'Unpublish Week 6 contest?' });
  await expect(unpublish).toContainText('The contest disappears from students at once, in the middle of it');
  await expect(unpublish.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await unpublish.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  await page.getByRole('button', { name: 'Unpublish contest' }).click();
  await unpublish.getByRole('button', { name: 'Unpublish contest' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)?.published).toBe(false);
  await expect(page.locator('.toasts').getByText('Unpublished', { exact: true })).toBeVisible();
});

test('the console feed filters waiting or judging and judge errors', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.goto('/admin/contests/4/console');
  const verdict = page.getByLabel('Verdict', { exact: true });
  await verdict.selectOption({ label: 'Waiting or judging' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=pending&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await expect(page.locator('.feed tbody')).toContainText('Judging');
  await verdict.selectOption({ label: 'Judge error' });
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?verdict=error&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await expect(page.locator('.feed tbody')).toContainText('Meera Iyer');
});

test('a graded contest asks for a reason before it takes an AI problem nobody solved', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'upcoming', aiGate: true });
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
  await expect(page.locator('.toasts').getByText('Saved problems. Problem B is graded as the AI wrote it.', { exact: true })).toBeVisible();
  await expect(reason).toHaveCount(0);
});

test('results switch boards, link the exports, release a held board and save the editorial', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'after', holdReveal: true });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByText('Final standings.')).toBeVisible();
  await page.getByLabel('As students saw it frozen').check();
  await expect(page.getByText(/^Frozen at/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Download CSV' })).toHaveAttribute('href', '/api/admin/contests/4/export.csv');
  await expect(page.getByRole('link', { name: 'Download JSON' })).toHaveAttribute('href', '/api/admin/contests/4/export.json');
  await expect(page.getByText('8 students scored 40')).toBeAttached();
  await expect(page.getByText('Students still see the frozen board.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Release the full board' }).click();
  const confirmRelease = page.getByRole('dialog', { name: 'Release the full board?' });
  await expect(confirmRelease).toContainText('This cannot be undone.');
  await expect(confirmRelease.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirmRelease.getByRole('button', { name: 'Release the full board' }).click();
  await expect(page.getByText('Students still see the frozen board.', { exact: false })).toBeHidden();
  await expect.poll(() => api.releases).toBe(1);
  await page.getByRole('button', { name: 'Publish editorial' }).click();
  await expect.poll(() => api.editorials).toEqual([{ editorial_md: '## A\n\nUse long long.', published: true }]);
  await expect(page.getByText('Published: students see it after the contest.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Unpublish editorial' })).toBeVisible();
});

test('the reveal enters full screen on open and leaves it on exit', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await page.getByRole('button', { name: 'Start the reveal' }).click();
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement?.tagName ?? null)).toBe('HTML');
  await reveal.getByRole('button', { name: 'Close the reveal' }).click();
  await expect(reveal).toBeHidden();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement)).toBeNull();
});

for (const motion of ['no-preference', 'reduce'] as const) {
  test(`the reveal walks from the bottom one row per key press (${motion})`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion: motion });
    await mockApi(page, { who: 'admin', phase: 'after' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/contests/4/results');
    await page.getByRole('button', { name: 'Start the reveal' }).click();
    const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
    await expect(reveal).toBeVisible();
    const names = () => reveal.locator('.rows .row .name').allTextContents();
    expect(await names()).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
    await expect(reveal.getByText('2 rows left.', { exact: false })).toBeVisible();
    await expect(reveal.locator('.row.next-row .name')).toHaveText('Asha Rao');
    // The next row is marked in muted ink; the action colour is kept for the row that just moved (C6).
    await expect(reveal.locator('.row.just-moved')).toHaveCount(0);
    await page.screenshot({ path: `${shots()}reveal-start-1440-${motion}.png` });

    // Record each row that is marked as moving, with its stacking order, as it happens.
    await reveal.evaluate((el) => {
      const seen: string[] = [];
      (window as unknown as { movingSeen: string[] }).movingSeen = seen;
      new MutationObserver(() => {
        for (const row of el.querySelectorAll<HTMLElement>('.row.moving')) seen.push(`${row.querySelector('.name')?.textContent} ${getComputedStyle(row).zIndex}`);
      }).observe(el, { subtree: true, attributes: true, attributeFilter: ['class'] });
    });
    await page.keyboard.press('Space');
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);
    await expect(reveal.locator('[aria-live="polite"]')).toHaveText('Asha Rao moves from 3rd to 2nd with 200 points.');
    // The climbing row is drawn above the row it passes while it slides (no move to draw with reduced motion).
    const seen = () => page.evaluate(() => [...new Set((window as unknown as { movingSeen: string[] }).movingSeen)]);
    if (motion === 'no-preference') await expect.poll(seen).toEqual(['Asha Rao 1']);
    else expect(await seen()).toEqual([]);
    await expect(reveal.locator('.row.moving')).toHaveCount(0);
    if (motion === 'reduce') await expect(reveal.locator('.row.flash .name')).toHaveText('Asha Rao');
    await expect(reveal.locator('.board[data-stepping]')).toHaveCount(0);
    await expect(reveal.locator('.row').filter({ hasText: 'Asha Rao' }).locator('.rank')).toHaveText('2');
    await expect(reveal.locator('.row.just-moved .name')).toHaveText('Asha Rao');
    await page.screenshot({ path: `${shots()}reveal-step-1440-${motion}.png` });

    // The left arrow undoes the step, the right arrow does it again.
    await page.keyboard.press('ArrowLeft');
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
    await page.keyboard.press('ArrowRight');
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);
    await expect(reveal.locator('.board[data-stepping]')).toHaveCount(0);

    // Leaving keeps the place; opening again resumes.
    await page.keyboard.press('Escape');
    await expect(reveal).toBeHidden();
    const resume = page.getByRole('button', { name: 'Resume the reveal' });
    await expect(resume).toBeFocused();
    await resume.click();
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);

    await page.keyboard.press('ArrowRight');
    await expect(reveal.getByText('Every result is shown.', { exact: false })).toBeVisible();
    await expect(reveal.getByRole('button', { name: 'Show next' })).toHaveCount(0);
    await page.screenshot({ path: `${shots()}reveal-done-1440-${motion}.png` });

    // Space on Close the reveal closes instead of stepping.
    await reveal.getByRole('button', { name: 'Close the reveal' }).focus();
    await page.keyboard.press('Space');
    await expect(reveal).toBeHidden();
    await resume.click();
    await reveal.getByRole('button', { name: 'Start over' }).click();
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
  });
}

test('the reveal skips to the top 10 and releases a held board from its end (P3 F6.4)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'after', revealBoard: 'long', holdReveal: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await page.getByRole('button', { name: 'Start the reveal' }).click();
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal.getByText('14 rows left.', { exact: false })).toBeVisible();
  await reveal.getByRole('button', { name: 'Skip to the top 10' }).click();
  await expect(reveal.getByText('10 rows left.', { exact: false })).toBeVisible();
  await expect(reveal.getByRole('button', { name: 'Skip to the top 10' })).toHaveCount(0);
  await expect(reveal.locator('.row.next-row .rank')).toHaveText('10');
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press('Space');
    const n = 9 - i;
    await expect(reveal.getByText(n === 0 ? 'Every result is shown.' : n === 1 ? '1 row left.' : `${n} rows left.`, { exact: false })).toBeVisible();
    await expect(reveal.locator('.board[data-stepping]')).toHaveCount(0);
  }
  await expect(reveal.getByText('Every result is shown.', { exact: false })).toBeVisible();
  await reveal.getByRole('button', { name: 'Release the full board' }).click();
  await page.getByRole('dialog', { name: 'Release the full board?' }).getByRole('button', { name: 'Release the full board' }).click();
  await expect.poll(() => api.releases).toBe(1);
  await expect(reveal.getByRole('button', { name: 'Release the full board' })).toHaveCount(0);
});

test('a board that was never frozen says there is nothing to reveal (P3 O8)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after', revealBoard: 'nothing' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await page.getByRole('button', { name: 'Start the reveal' }).click();
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal.getByText('Nothing to reveal: the board was never frozen, so these are the final standings.')).toBeVisible();
  await expect(reveal.getByRole('heading', { name: 'Week 6 contest' })).toBeFocused();
  // The board is as tall as its rows, not the screen.
  const board = await reveal.locator('.board').boundingBox();
  expect(board!.height).toBeLessThan(400);
});

test('after the end, a held board gives the place only and says when it updates', async ({ page }) => {
  await mockApi(page, { phase: 'after', studentFrozen: true });
  await page.goto('/contests/week-6');
  await expect(
    page.getByText(/^In the frozen standings you are \d+(st|nd|rd|th) of 28\. Your final place shows when the organisers release the results\.$/),
  ).toBeVisible();
  await expect(page.getByText(/points\.$/)).toHaveCount(0);
  await page.getByRole('link', { name: 'See the standings' }).click();
  await expect(page.getByText(/^Frozen at \d{2}:\d{2}\. The final standings appear when the organisers release them\./)).toBeVisible();
  await expect(page.locator('.legend')).toHaveText('The bar under a score is the share of points earned. A dash means not tried.');
  await page.getByText('How to read the board').click();
  await expect(page.getByText('counts submissions made after the freeze', { exact: false })).toBeVisible();
  // S6: the own place is the page's one display figure, labelled for the frozen board.
  await expect(page.locator('.place')).toContainText('Your place on the frozen board');
  await expect(page.locator(".place-figure")).toHaveText(/^\s*\d+ of 28$/);
  expect(await page.locator('.place-rank').evaluate((el) => getComputedStyle(el).fontSize)).toBe('44px');
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

  // "?" lists the shortcuts; Escape closes the list and focus comes back.
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(keys).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Solving a problem' })).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Reveal' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  // Read the statement: the sample copy buttons are reachable.
  await tabTo(page, page.getByRole('button', { name: /Copy sample 1 input/i }));

  // Write code: Tab reaches the editor, typing "]" stays in the code, and
  // Escape then Tab leaves it.
  const code = page.locator('.cm-content');
  await tabTo(page, code);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// a[i]?');
  await expect(code).toContainText('// a[i]?');
  await expect(page.getByRole('dialog', { name: 'Keyboard shortcuts' })).toBeHidden();
  await expect(page).toHaveURL('/contests/week-6/problems/A');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  expect(await hasFocus(code)).toBe(false);

  // Run and submit from the keyboard.
  await code.focus();
  await page.keyboard.press("Control+'");
  await expect(page.getByRole('heading', { name: 'Run on samples' })).toBeVisible();
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  const id = Number(api.submissions.at(-1)!.id);
  const judged = submission(id, 11, 'done', 40);
  api.submissions[api.submissions.length - 1] = judged;
  api.scores.A = 40;
  api.stream.push({ type: 'submission', data: judged });
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result.locator('.headline').first()).toHaveText('Partial, 40 of 100', { timeout: 10_000 });

  // Read the verdict: the failing subtask is open, the passing one opens with Enter.
  const passing = result.locator('details.group summary').filter({ hasText: 'Subtask 1' }).first();
  // Focus is still in the editor, where Tab indents: Escape first, then Tab on.
  await page.keyboard.press('Escape');
  await tabTo(page, passing);
  await page.keyboard.press('Enter');
  await expect(result.locator('details.group').filter({ hasText: 'Subtask 1' }).first()).toHaveAttribute('open', '');

  // Messages: open, ask, close; focus returns to the button.
  const messages = page.getByRole('navigation', { name: 'Problems' }).getByRole('button', { name: /Messages/ });
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

  // Standings: the link just before Messages in the rail (forward, Tab would
  // have to cross the editor), then Jump to you.
  await tabTo(page, page.getByRole('navigation', { name: 'Problems' }).getByRole('link', { name: 'Standings' }), { back: true });
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL('/contests/week-6/standings');
  // The own row is already on screen, so "Jump to you" steps aside (S8) and the board box takes focus.
  await expect(page.locator('tr.me')).toBeInViewport();
  await expect(page.locator('.tools')).toHaveClass(/away/);
  await tabTo(page, page.getByRole('region', { name: /^Standings for/ }));
});

test('keyboard only: an organiser edits a contest, runs the console and walks the reveal', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  // Contest admin list to the editor.
  await activate(page, page.getByRole('link', { name: 'Week 6 contest', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');

  // A setting changed and saved from the keyboard; the rules open from their summary.
  await activate(page, page.locator('#rules summary'));
  await tabTo(page, page.getByLabel('Seconds between submissions'));
  await page.keyboard.press('Control+a');
  await page.keyboard.type('20');
  await activate(page, page.getByRole('button', { name: 'Save changes' }));
  await expect.poll(() => api.settingsPuts.at(-1)?.min_gap_s).toBe(20);

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

  // Console: the section links, and an error that takes focus.
  await activate(page, page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: /^Console/ }));
  await expect(page).toHaveURL('/admin/contests/4/console');
  await activate(page, page.getByRole('button', { name: 'Give extra time' }));
  await expect(page.locator('#ext-error')).toHaveText('Choose a student.');
  await expect(page.locator('#ext-user')).toBeFocused();

  // Results and the reveal, driven by keys from start to end and back.
  await page.locator('main h1').focus();
  await activate(page, page.getByRole('link', { name: 'Results', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/results');
  await activate(page, page.getByRole('button', { name: 'Start the reveal' }));
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal).toBeVisible();
  await page.keyboard.press('Space');
  await expect(reveal.locator('.board[data-stepping]')).toHaveCount(0);
  await page.keyboard.press('Space');
  await expect(reveal.getByText('Every result is shown.', { exact: false })).toBeVisible();
  // Next is now off; focus stayed in the dialog, so the keys still work.
  await expect.poll(() => hasFocus(reveal)).toBe(true);
  await expect(reveal.locator('.board[data-stepping]')).toHaveCount(0);
  await page.keyboard.press('ArrowLeft');
  await expect(reveal.getByText('1 row left.', { exact: false })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(reveal).toBeHidden();
  await expect(page.getByRole('button', { name: 'Resume the reveal' })).toBeFocused();
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
  await mockApi(page, { who: 'admin', phase: 'upcoming', published: false });
  await page.goto('/admin/contests/4');
  await expect(page.locator('.publish-line')).toContainText('Not published. 1 problem, starts');
  await page.getByRole('button', { name: 'Publish contest' }).click();
  const publish = page.getByRole('dialog', { name: 'Publish Week 6 contest?' });
  await expect(publish).toContainText('Students on the roster see it in their contest list at once.');
  await expect(publish).toContainText('1 problem, all validated.');
  await publish.getByRole('button', { name: 'Publish contest' }).click();
  await expect(page.locator('#publish-error')).toHaveText('Problem B changed or lost its validation since it was added. Validate it and add it again.');
});

test('an unpublished contest past its start is a draft, not live (C2-015)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', published: false });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await expect(page.locator('.title-row .state-word')).toHaveText('Draft');
  await expect(page.getByText('This contest is live.', { exact: false })).toHaveCount(0);
  await expect(page.getByRole('note').filter({ hasText: 'The start time has passed.' })).toHaveText(
    'The start time has passed. Students cannot see this contest until you publish it, so move the start or publish now.',
  );
  await expect(page.getByRole('region', { name: 'Before you publish' })).toContainText('The start time has passed. Move it ahead, or students start at once.');
  // Nobody can see it, so a time change saves without the live warning.
  await page.locator('#rules summary').click();
  await page.getByLabel('Freeze the board, minutes before the end').fill('20');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('dialog', { name: 'Change a live contest?' })).toHaveCount(0);
  await expect.poll(() => api.settingsPuts.at(-1)?.freeze_minutes).toBe(20);
});

test('an unpublished contest reads Draft, and holding the board needs a freeze (P3 O1, O8)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'upcoming', published: false });
  await page.goto('/admin/contests/4');
  await expect(page.locator('.title-row .state-word')).toHaveText('Draft');
  await expect(page.getByRole('region', { name: 'Before you publish' })).toContainText('1 problem, all validated.');
  await page.locator('#rules summary').click();
  const hold = page.getByLabel('Hold the full board after the end until you reveal it');
  await expect(hold).toBeEnabled();
  await page.getByLabel('Freeze the board, minutes before the end').fill('0');
  await expect(hold).toBeDisabled();
  await expect(page.getByText('Needs a freeze; with no freeze there is nothing to reveal.')).toBeVisible();
  // Settings left unsaved ask before leaving.
  await page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: /^Console/ }).click();
  const leave = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(leave).toContainText('Problems are saved already.');
  await leave.getByRole('button', { name: 'Cancel' }).click();
  await expect(page).toHaveURL('/admin/contests/4');
});

test('a draft as the next contest says publish it, with Settings first (C2-024)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  const featured = page.getByRole('region', { name: 'Week 7 contest' });
  await expect(featured).toContainText('Not published. Add problems and publish it in Settings.');
  await expect(featured).not.toContainText('Starts in');
  await expect(featured.getByRole('link', { name: 'Settings' })).toHaveClass(/btn-primary/);
  await expect(featured.getByRole('link', { name: 'Console' })).toHaveClass(/btn-quiet/);
});

test('the contest list stacks below 1024 px and points to the bank when empty (P3 R1, O6)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'during' });
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

test('the console feed links names to progress, keeps private notes and opens a case from a submission (F9.3)', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const feed = page.locator('.feed tbody');
  const asha = feed.getByRole('row', { name: /Asha Rao/ });
  await expect(asha.getByRole('link', { name: 'Asha Rao' })).toHaveAttribute('href', '/admin/users/7/progress');
  await asha.getByRole('button', { name: /^Notes on Asha Rao's submission to problem B/ }).click();
  const dialog = page.getByRole('dialog', { name: "Notes on Asha Rao's submission to problem B" });
  await expect(dialog).toContainText('The student is never told about a note');
  await expect(dialog.getByRole('list', { name: 'Notes, oldest first' })).toContainText('Ravi Kumar, lines 8 to 10');
  await expect(dialog).toContainText('unless a case is opened');
  // The program is shown as text, and the typed lines are highlighted in it.
  const program = dialog.getByRole('region', { name: /^Asha Rao's program for problem B/ });
  await expect(program).toContainText('// <b>sum</b>');
  await expect(program.locator('b')).toHaveCount(0);
  await dialog.getByLabel('Lines in question (optional)').fill('3-4');
  await expect(program.locator('.line.hit')).toHaveCount(2);
  await dialog.getByLabel('Lines in question (optional)').fill('');

  await dialog.getByRole('button', { name: 'Save note' }).click();
  await expect(dialog.getByText('Write the note first.')).toBeVisible();
  await dialog.getByLabel('New note').fill('Same variable names as the editorial draft.');
  await dialog.getByLabel('Lines in question (optional)').fill('10-8');
  await dialog.getByRole('button', { name: 'Save note' }).click();
  await expect(dialog.getByText('In 10-8, the first line comes first.')).toBeVisible();
  await dialog.getByLabel('Lines in question (optional)').fill('3-5, 12');
  await dialog.getByRole('button', { name: 'Save note' }).click();
  await expect(dialog.getByRole('list', { name: 'Notes, oldest first' }).getByRole('listitem')).toHaveCount(2);
  expect(api.notePosts).toEqual([{ text: 'Same variable names as the editorial draft.', lines: [{ from: 3, to: 5 }, { from: 12, to: 12 }] }]);

  await dialog.getByRole('button', { name: 'Open a case' }).click();
  const open = page.getByRole('dialog', { name: 'Open a case' });
  await expect(open.getByLabel('Title')).toHaveValue('Submission to problem B');
  await expect(open).toContainText("From Asha Rao's submission to problem B. The submission and its private notes are copied into the case");
  await open.getByLabel('Reason').fill('The notes point at three passages that match the draft.');
  await open.getByRole('button', { name: 'Open the case' }).click();
  await expect(page.locator('.toasts').getByText('Opened the case. Share it with the student when you are ready.')).toBeVisible();
  await expect(asha.getByRole('link', { name: 'Case 14' })).toHaveAttribute('href', '/admin/integrity/cases/14');
  expect(api.casePosts).toEqual([{ title: 'Submission to problem B', note: 'The notes point at three passages that match the draft.' }]);
  await expect(asha.getByRole('button', { name: /^Notes on Asha Rao/ })).toHaveText('2 notes');
  await page.screenshot({ path: `${shots()}console-notes-1440-light.png`, fullPage: true });
});

test('results link names to progress, say a decision changed the board, mark no cell, and show the class view (F9.2b, F9.4)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after', zeroB: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByText(/^Board updated on .+ after a review decision\.$/)).toBeVisible();
  const board = page.getByRole('region', { name: /^Standings for Week 6 contest/ });
  await expect(board.getByRole('link', { name: 'Asha Rao' })).toHaveAttribute('href', '/admin/users/7/progress');
  // The board itself carries no adjustment mark: no word, no class, no data attribute.
  const html = await board.innerHTML();
  expect(html).not.toMatch(/decision|adjust|zero_by|zeroed/i);
  await expect(page.getByText('Both files have a "zero by decision" column', { exact: false })).toBeVisible();
  const klass = page.getByRole('table', { name: 'How the class did on each problem' });
  await expect(klass.getByRole('row', { name: /Interval covering/ })).toContainText('19 of 30');
  await expect(klass.getByRole('row', { name: /A plus B/ })).toContainText('Most often the first failure: test 3 of subtask 2, for 5 students.');
  await expect(klass.getByRole('row', { name: /Interval covering/ })).toContainText('13.3%');
  await expect(klass).not.toContainText('Asha');
  await page.screenshot({ path: `${shots()}results-p4-1440-light.png`, fullPage: true });
});

test('the class view waits for final results', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'after', notFinal: true });
  await page.goto('/admin/contests/4/results');
  await expect(page.getByText('The class view opens when the results are final.')).toBeVisible();
});

test('the shared standings say a decision changed the board, and no cell carries a mark (D77, C1-040)', async ({ page }) => {
  await mockApi(page, { phase: 'after', zeroB: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/standings');
  await expect(page.getByText(/^Board updated on .+ after a review decision\.$/)).toBeVisible();
  const board = page.getByRole('region', { name: /^Standings for Week 6 contest/ });
  await expect(board.getByRole('row').nth(1)).toBeVisible();
  // The board snapshot: no word, class or attribute marks an adjusted cell, and no name links anywhere.
  const html = await board.innerHTML();
  expect(html).not.toMatch(/decision|adjust|zero_by|zeroed|review/i);
  await expect(board.getByRole('link')).toHaveCount(0);
  await page.goto('/contests/week-6');
  await expect(page.getByText(/^Board updated on .+ after a review decision\.$/)).toBeVisible();
});

test('the contest nav opens the student view of the contest (F3.3, C2-056)', async ({ page }) => {
  await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  const link = page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: 'Student view' });
  await expect(link).toHaveAttribute('href', '/contests/week-6');
  await link.click();
  await expect(page).toHaveURL('/contests/week-6');
  await expect(page.locator('.cm-content')).toBeVisible();
});
