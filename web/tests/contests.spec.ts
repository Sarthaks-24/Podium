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
  // Students get only the Contests link.
  await expect(page.getByRole('link', { name: 'Contests', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link')).toHaveText(['Contests']);
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
  await page.waitForTimeout(1500);
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

  await page.getByRole('button', { name: 'Jump to you' }).click();
  await expect(mine).toBeFocused();
  await expect(mine).toBeInViewport();

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
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Problem bank']);
  await expect(nav.getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByText('To create a contest or follow one live')).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Week 7 contest/ })).toContainText('Draft');
  await expect(page.getByRole('row', { name: /Week 6 contest/ })).toContainText('Published');

  await page.getByRole('button', { name: 'New contest' }).click();
  await page.getByLabel('Title').fill('Week 8 contest');
  await expect(page.getByLabel('Slug')).toHaveValue('week-8-contest');
  await page.getByRole('button', { name: 'Create contest' }).click();
  await expect(page).toHaveURL('/admin/contests/9');
});

test('students cannot open the contest admin', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/admin/contests');
  await expect(page.getByText('Only admins can manage contests.')).toBeVisible();
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

  // Publishing a contest whose problem changed is refused with the reason.
  await slug.fill('week-6');
  await page.getByLabel('Published: students can see the contest').check();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await page.getByRole('dialog', { name: 'Change a live contest?' }).getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#f-published-error')).toHaveText('Problem B changed since it was added; validate it and add it again.');
  await page.getByLabel('Published: students can see the contest').uncheck();
  await page.getByLabel('Python 3').uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved changes')).toBeVisible();
  expect(api.settingsPuts.at(-1)).toMatchObject({ slug: 'week-6', languages: ['cpp17'], published: false, freeze_minutes: 30 });

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
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('A');
  await page.getByRole('button', { name: 'Save problems' }).click();
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

test('the console pauses, extends, rejudges, announces and answers', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/console');
  await expect(page.getByText('Judging is running.')).toBeVisible();

  await page.getByRole('button', { name: 'Pause judging' }).click();
  const dialog = page.getByRole('dialog', { name: 'Pause judging?' });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Pause judging' }).click();
  await expect(page.getByText('Judging is paused. Submissions are queuing.')).toBeVisible();
  await expect.poll(() => api.pauses).toBe(1);
  await page.getByRole('button', { name: 'Resume judging' }).click();
  await expect(page.getByText('Judging is running.')).toBeVisible();

  await expect(page.getByRole('link', { name: '1 unanswered question', exact: true })).toHaveAttribute('href', '#clar-title');
  await page.getByLabel('Minutes for everyone').fill('0');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  await expect(page.locator('#extend-error')).toHaveText('Enter the minutes to add, 1 or more.');
  await expect(page.getByLabel('Minutes for everyone')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Minutes for everyone').fill('15');
  await page.getByRole('button', { name: 'Extend contest' }).click();
  await page.getByRole('dialog', { name: 'Extend the contest by 15 minutes?' }).getByRole('button', { name: 'Extend contest' }).click();
  await expect.poll(() => api.extends).toEqual([{ minutes: 15 }]);
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue('The contest has been extended by 15 minutes.');

  await page.getByLabel('Student', { exact: true }).first().selectOption({ label: 'Asha Rao' });
  await page.getByLabel('Extra minutes').fill('10');
  await page.getByLabel('Reason').fill('Laptop crashed');
  await page.getByRole('button', { name: 'Give extra time' }).click();
  await expect.poll(() => api.extensions).toEqual([{ user: '7', body: { minutes: 10, reason: 'Laptop crashed' } }]);
  await expect(page.locator('.extensions')).toContainText('Asha Rao: 10 min, Laptop crashed');

  // Filters go to the feed's query.
  await page.locator('#f-problem').selectOption('A');
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect.poll(() => api.feedQueries.at(-1)).toBe('?problem=A&page=1');
  await expect(page.locator('.feed tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: /Rejudge submission by Arjun Mehta to A/ }).click();
  await expect.poll(() => api.rejudges).toEqual([{ submission_id: 300 }]);
  await page.getByLabel('Rejudge every submission to').selectOption('A');
  await page.getByRole('button', { name: 'Rejudge problem' }).click();
  await page.getByRole('dialog', { name: 'Rejudge problem A?' }).getByRole('button', { name: 'Rejudge problem' }).click();
  await expect.poll(() => api.rejudges.at(-1)).toEqual({ problem_label: 'A' });

  await page.getByLabel('Start from a template').selectOption('paused');
  await expect(page.getByLabel('Announcement (Markdown)')).toHaveValue(/Judging is paused for a few minutes/);
  await page.getByRole('button', { name: 'Post announcement' }).click();
  await expect.poll(() => api.posted.length).toBe(1);

  // A canned reply, answered for everyone.
  await page.getByRole('group', { name: 'Canned replies' }).getByRole('button', { name: 'Yes.' }).click();
  await expect(page.getByLabel('Answer', { exact: true })).toHaveValue('Yes.');
  await page.getByLabel('Answer for everyone').check();
  await page.getByRole('button', { name: 'Send answer' }).click();
  await expect.poll(() => api.answers).toEqual([{ id: '51', body: { answer: 'Yes.', public: true } }]);

  // The audit log follows every action, newest first, with who did it.
  const audit = page.getByRole('region', { name: 'Changes' });
  await expect(audit.getByRole('listitem').first()).toContainText('Rejudged problem A (1 submission)');
  await expect(audit).toContainText('Gave asha@college.edu 10 extra minutes (Laptop crashed)');
  await expect(audit).toContainText('Extended the contest by 15 minutes');
  await expect(audit).toContainText('Paused judging');
  await expect(audit).toContainText('priya@college.edu');
  await page.screenshot({ path: `${shots()}contest-console-1440-light.png`, fullPage: true });
});

test('unpublishing a live contest asks first', async ({ page }) => {
  const api = await mockApi(page, { who: 'admin', phase: 'during', published: true });
  await page.goto('/admin/contests/4');
  const published = page.getByLabel('Published: students can see the contest');
  await expect(published).toBeChecked();
  await published.uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  const live = page.getByRole('dialog', { name: 'Change a live contest?' });
  await expect(live).toContainText('unpublishing hides the contest from them');
  await live.getByRole('button', { name: 'Cancel' }).click();
  expect(api.settingsPuts).toHaveLength(0);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await live.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.settingsPuts.at(-1)?.published).toBe(false);
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
  await page.getByRole('button', { name: 'Save problems' }).click();

  const reason = page.getByLabel('Reason to grade problem B as the AI wrote it');
  await expect(reason).toBeFocused();
  await expect(page.getByText('Problem B came from the AI wizard', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Grade as written' }).click();
  await expect(page.locator('#ai-reason-error')).toHaveText('Write a reason of at least 10 characters.');
  await expect(reason).toHaveAttribute('aria-invalid', 'true');

  await reason.fill('Checked the reference by hand against the statement.');
  await page.getByRole('button', { name: 'Grade as written' }).click();
  await expect.poll(() => api.problemPuts.at(-1)).toEqual([
    { problem_id: 11, label: 'A' },
    { problem_id: 12, label: 'B', ai_reason: 'Checked the reference by hand against the statement.' },
  ]);
  await expect(page.getByText('Saved problems with your reason', { exact: true })).toBeVisible();
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
  await reveal.getByRole('button', { name: 'Exit' }).click();
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
    await page.waitForTimeout(400);
    await expect(reveal.locator('.row').filter({ hasText: 'Asha Rao' }).locator('.rank')).toHaveText('2');
    await page.screenshot({ path: `${shots()}reveal-step-1440-${motion}.png` });

    // The left arrow undoes the step, the right arrow does it again.
    await page.keyboard.press('ArrowLeft');
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
    await page.keyboard.press('ArrowRight');
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);
    await page.waitForTimeout(400);

    // Leaving keeps the place; opening again resumes.
    await page.keyboard.press('Escape');
    await expect(reveal).toBeHidden();
    const resume = page.getByRole('button', { name: 'Resume the reveal' });
    await expect(resume).toBeFocused();
    await resume.click();
    await expect.poll(names).toEqual(['Priya Raman', 'Asha Rao', 'Arjun Mehta', 'Ravi Kumar']);

    await page.keyboard.press('ArrowRight');
    await expect(reveal.getByText('All rows resolved.', { exact: false })).toBeVisible();
    await expect(reveal.getByRole('button', { name: 'Next' })).toBeDisabled();
    await page.screenshot({ path: `${shots()}reveal-done-1440-${motion}.png` });

    // Space on Exit exits instead of stepping.
    await reveal.getByRole('button', { name: 'Exit' }).focus();
    await page.keyboard.press('Space');
    await expect(reveal).toBeHidden();
    await resume.click();
    await reveal.getByRole('button', { name: 'Start over' }).click();
    await expect.poll(names).toEqual(['Priya Raman', 'Arjun Mehta', 'Asha Rao', 'Ravi Kumar']);
  });
}

test('after the end, a held board gives the place only and says when it updates', async ({ page }) => {
  await mockApi(page, { phase: 'after', studentFrozen: true });
  await page.goto('/contests/week-6');
  await expect(
    page.getByText(/^In the frozen standings you are \d+(st|nd|rd|th) of 28\. Your final place shows when the organisers release the results\.$/),
  ).toBeVisible();
  await expect(page.getByText(/points\.$/)).toHaveCount(0);
  await page.getByRole('link', { name: 'See the standings' }).click();
  await expect(page.getByText(/^Frozen at \d{2}:\d{2}\. The final standings appear when the organisers release them\./)).toBeVisible();
  await expect(page.locator('.legend')).toContainText('The shaded part of a cell is the share of points earned.');
  await expect(page.locator('.legend')).toContainText('A dash means not tried.');
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
  const bar = page.locator('.clock-bar');
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
  await activate(page, page.getByRole('button', { name: 'Jump to you' }));
  await expect(page.locator('tr.me')).toBeFocused();
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

  // A setting changed and saved from the keyboard.
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
  await page.getByRole('button', { name: 'Remove Interval covering' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Letter for Interval covering')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove A plus B' })).toBeFocused();

  // Console: the section links, and an error that takes focus.
  await activate(page, page.getByRole('navigation', { name: 'Contest' }).getByRole('link', { name: /^Console/ }));
  await expect(page).toHaveURL('/admin/contests/4/console');
  await activate(page, page.getByRole('button', { name: 'Give extra time' }));
  await expect(page.locator('#ext-error')).toHaveText('Choose a student.');
  await expect(page.getByLabel('Student', { exact: true }).first()).toBeFocused();

  // Results and the reveal, driven by keys from start to end and back.
  await page.locator('main h1').focus();
  await activate(page, page.getByRole('link', { name: 'Results', exact: true }));
  await expect(page).toHaveURL('/admin/contests/4/results');
  await activate(page, page.getByRole('button', { name: 'Start the reveal' }));
  const reveal = page.getByRole('dialog', { name: 'Week 6 contest' });
  await expect(reveal).toBeVisible();
  await page.keyboard.press('Space');
  await page.waitForTimeout(400);
  await page.keyboard.press('Space');
  await expect(reveal.getByText('All rows resolved.', { exact: false })).toBeVisible();
  // Next is now off; focus stayed in the dialog, so the keys still work.
  await expect.poll(() => hasFocus(reveal)).toBe(true);
  await page.waitForTimeout(400);
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
