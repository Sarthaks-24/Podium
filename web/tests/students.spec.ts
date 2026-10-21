// v2 P4 student screens (cache/v2-plan/phases/P4-students.md F4b.1 to
// F4b.3, F9.1, F9.2) on the mocks of tests/mocks/students.ts (contracts C8,
// C9 and C12): "Your progress" at four widths in both themes, its table
// view, the hidden and empty states and the organiser view; notices with
// the unread count and the held state; Standing on /integrity; the
// student case page with its opened call, explanation and appeal; the Home
// line; and two guards: no student page prints the rating value, and no
// progress request is made for a contest whose board is still frozen.
// Screenshots go to cache/screenshots-p4-student/.
import { expect, test, type Page } from '@playwright/test';
import { activate, tabTo } from './keyboard';
import * as contests from './mocks/contests';
import * as org from './mocks/organiser';
import { LEAKED_RATING, mockApi, settle, shots, SCREENS, THEMES, WIDTHS } from './mocks/students';

function screen(name: string) {
  const s = SCREENS.find((x) => x.name === name);
  if (!s) throw new Error(`no screen ${name}`);
  return s;
}

async function noOverflow(page: Page, where: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `horizontal overflow ${where}`).toBeLessThanOrEqual(0);
}

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const s of SCREENS) {
      test(`${s.name} at four widths`, async ({ page }) => {
        await mockApi(page, s.options);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(s.path);
        await s.ready(page);
        await expect(page).toHaveTitle(s.title);
        await expect(page.locator('h1')).toHaveCount(1);
        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          await noOverflow(page, `${s.name} at ${width}px`);
          await page.screenshot({ path: `${shots()}${s.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

test.describe('your progress', () => {
  test('shows figures, the line, the grid, topics weakest first and what to practise', async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/progress');
    await screen('progress').ready(page);

    const figures = page.locator('.figures');
    await expect(figures.getByText('Contests taken')).toBeVisible();
    await expect(figures.locator('dd').first()).toHaveText('5');
    // Points across all five, the exam included: 140 + 240 + 180 + 140 + 130.
    await expect(figures.locator('dd').nth(1)).toHaveText('830 of 1400');

    // One sentence names the series; the place is a small fact, never a headline.
    await expect(page.getByRole('img', { name: 'Scores over 5 contests, from 47 to 80 percent. Latest 47 percent.' })).toBeVisible();
    await expect(page.locator('.grid tbody tr').first().locator('.place')).toHaveText('Place 31 of 140');
    await expect(page.locator('.place-figure')).toHaveCount(0);

    // Cells carry a sentence; the exam reads totals only; a zero by decision says so.
    await expect(page.getByText('Problem B, 40 of 100, 4 attempts, best result time limit exceeded.')).toBeAttached();
    await expect(page.getByText('Totals only: this contest hid per-problem results.')).toBeVisible();
    await expect(page.getByText('Week 3 contest, problem B: zero on this problem by decision.')).toBeVisible();
    await expect(page.locator('.zeroed').getByRole('link', { name: 'See your case' })).toHaveAttribute('href', '/integrity');

    // Topics weakest first; one-problem topics are muted with a note.
    const topics = page.getByRole('heading', { name: 'By topic' }).locator('xpath=..').locator('dt');
    await expect(topics.first()).toContainText('binary search');
    await expect(topics.first()).toContainText('(1 problem)');
    const difficulty = page.getByRole('heading', { name: 'By difficulty' }).locator('xpath=..').locator('dt');
    await expect(difficulty).toHaveText(['Easy', 'Medium', 'Hard']);

    // Losses: words, not verdict colours, and one line of guidance under the largest.
    const losses = page.getByRole('region', { name: 'Where points were lost' });
    await expect(losses.locator('dt').first()).toHaveText('Wrong answer');
    await expect(losses.getByText('Check edge cases')).toBeVisible();

    // Practise next: a link button when practice is open, a muted line when not.
    const practise = page.getByRole('region', { name: 'Practise next' });
    await expect(practise.getByRole('link', { name: 'Practise' }).first()).toHaveAttribute('href', '/contests/week-6/problems/B');
    // One treatment for the row action, as on the contest page (C1-039).
    await expect(practise.getByRole('link', { name: 'Practise' }).first()).toHaveClass(/btn-link/);
    await expect(practise.getByText('Practice is not open for this contest').first()).toBeVisible();

    // No comparison with anyone (D94).
    await expect(page.getByText(/median|percentile|average of the class/i)).toHaveCount(0);
  });

  test('"Show as table" is the keyboard path for the line', async ({ page }) => {
    await mockApi(page);
    await page.goto('/progress');
    await screen('progress').ready(page);
    await page.getByRole('button', { name: 'Show as table' }).focus();
    await page.keyboard.press('Enter');
    const table = page.getByRole('table', { name: 'Score in each contest, oldest first' });
    await expect(table).toBeVisible();
    await expect(table.locator('tbody tr').first().locator('th')).toHaveText('Week 2 contest');
    await expect(table.locator('tbody tr').last()).toContainText('140 of 300');
    await page.getByRole('button', { name: 'Show as chart' }).click();
    await expect(page.getByRole('img', { name: /Scores over 5 contests/ })).toBeVisible();
  });

  test('a pointer over the line shows the nearest contest', async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/progress');
    await screen('progress').ready(page);
    const svg = page.locator('.score-line svg');
    const box = (await svg.boundingBox())!;
    await page.mouse.move(box.x + box.width - 50, box.y + box.height / 2);
    await expect(page.locator('.score-line .tip')).toContainText('Week 6 contest: 140 of 300 (47 percent)');
  });

  test('one contest hides the line; hidden progress offers "Show my progress"', async ({ page }) => {
    const api = await mockApi(page, { progress: 'hidden' });
    await page.goto('/progress');
    await expect(page.getByRole('heading', { name: 'Your progress is hidden' })).toBeVisible();
    await page.getByRole('button', { name: 'Show my progress' }).click();
    await expect(page.getByRole('heading', { name: 'Contests and problems' })).toBeVisible();
    expect(api.preferencePuts).toEqual([{ theme: '', verdicts: 'standard', hide_progress: false }]);
  });

  test('the account menu hides and shows progress', async ({ page }) => {
    const api = await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/progress');
    await screen('progress').ready(page);
    await page.getByRole('button', { name: 'Asha Rao' }).click();
    const box = page.getByRole('checkbox', { name: 'Hide my progress' });
    await expect(box).not.toBeChecked();
    await box.check();
    await expect(page.getByText('Your progress page is hidden. Your results are kept.')).toBeVisible();
    expect(api.preferencePuts.at(-1)).toMatchObject({ hide_progress: true });
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Your progress is hidden' })).toBeVisible();
  });

  test('an organiser sees the same page under the student name; a student is refused', async ({ page }) => {
    await mockApi(page, { who: 'admin' });
    await page.goto('/admin/users/7/progress');
    await expect(page.getByRole('heading', { level: 1, name: "Asha Rao's progress" })).toBeVisible();
    await expect(page.getByText('The page the student sees')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Week 6 contest' })).toHaveAttribute('href', '/admin/contests/6/results');
    await expect(page.getByRole('region', { name: 'Practise next' }).getByRole('link', { name: 'Practise' })).toHaveCount(0);
    // The trail of the sibling Student and Record pages (C2-058).
    const trail = page.getByRole('navigation', { name: 'Breadcrumb' });
    await expect(trail.getByRole('link', { name: 'Students' })).toHaveAttribute('href', '/admin/students');
    await expect(trail.getByRole('link', { name: 'Asha Rao' })).toHaveAttribute('href', '/admin/students/7');
    await page.goto('/admin/users/99/progress');
    await expect(page.getByText('There is no account with that number.')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Go to the students.' })).toHaveAttribute('href', '/admin/students');
  });

  test('a student is forbidden from the organiser route', async ({ page }) => {
    await mockApi(page, { who: 'student' });
    await page.goto('/admin/users/7/progress');
    await expect(page.getByText("Only owners and admins can see a student's progress.")).toBeVisible();
    await expect(page.getByRole('link', { name: 'priya@college.edu' }).first()).toBeVisible();
  });

  test('"Your progress" is in the student nav and Home sums up the final results', async ({ page }) => {
    await mockApi(page, { progress: 'one' });
    await page.goto('/');
    await expect(page.getByText(/Across 1 contest: 240 of 300 points\./)).toBeVisible();
    await page.getByRole('link', { name: 'See your progress' }).click();
    await expect(page).toHaveURL(/\/progress$/);
    await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Your progress' })).toHaveAttribute('aria-current', 'page');
    // One contest: no line.
    await expect(page.getByRole('heading', { name: 'Score over time' })).toHaveCount(0);
  });

  test('Home has no line while nothing is final or the student hid it', async ({ page }) => {
    await mockApi(page, { progress: 'hidden' });
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Your contests' })).toBeVisible();
    await expect(page.getByText(/^Across /)).toHaveCount(0);
  });
});

test.describe('the result after a contest', () => {
  test('no progress request is made while the board is frozen or held', async ({ page }) => {
    const requests: string[] = [];
    page.on('request', (r) => {
      if (new URL(r.url()).pathname === '/api/me/progress') requests.push(r.url());
    });
    await contests.mockApi(page, { phase: 'after', studentFrozen: true });
    await page.goto('/contests/week-6');
    await expect(page.getByRole('heading', { name: 'Your result' })).toBeVisible();
    await expect(page.getByText('In the frozen standings you are')).toBeVisible();
    await page.waitForTimeout(500);
    expect(requests).toEqual([]);
    await expect(page.getByRole('link', { name: 'See your progress' })).toHaveCount(0);
  });

  test('a final board links to progress and names the problem to practise', async ({ page }) => {
    await contests.mockApi(page, { phase: 'after' });
    await page.goto('/contests/week-6');
    await expect(page.getByRole('link', { name: 'See your progress' })).toBeVisible();
    await expect(page.getByText(/To practise next: problem C, Shortest detour, where you scored 0 of 100./)).toBeVisible();
    await expect(page.getByRole('link', { name: 'Practise problem C' })).toHaveAttribute('href', '/contests/week-6/problems/C');
  });

  test("a problem set to zero by decision says so on the student's own result", async ({ page }) => {
    await contests.mockApi(page, { phase: 'practice', zeroB: true });
    await page.goto('/contests/week-6');
    const row = page.locator('.results tbody tr', { hasText: 'Interval covering' });
    await expect(row).toContainText('Zero on this problem by decision.');
    await expect(row.getByRole('link', { name: 'See your case' })).toHaveAttribute('href', '/integrity/cases/12');
  });
});

test.describe('notices', () => {
  test('show what happened, what to do and by when, and mark themselves read', async ({ page }) => {
    const api = await mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/');
    const link = page.getByRole('link', { name: 'Notices, 2 unread' });
    await expect(link).toBeVisible();
    await link.click();
    await expect(page.getByRole('heading', { level: 1, name: 'Notices' })).toBeVisible();
    const first = page.locator('.notice').first();
    await expect(first).toContainText('An organiser asked you to explain your submission to problem B in Week 6 contest.');
    await expect(first).toContainText('Read the question and reply on the case page.');
    await expect(first).toContainText(/Reply by \d+ \w{3}, \d\d:\d\d/);
    await expect(first).toContainText('Nothing affects your marks until a person decides.');
    await expect(first.getByText('New')).toBeVisible();
    await expect(first.getByRole('link', { name: 'Reply on the case page' })).toHaveAttribute('href', '/integrity/cases/12');
    await expect(page.locator('.notice').nth(1).getByRole('link', { name: 'See your result' })).toHaveAttribute('href', '/contests/week-6');
    // The read notice has no "New".
    await expect(page.locator('.notice').nth(2).getByText('New')).toHaveCount(0);
    await expect.poll(() => api.reads).toEqual([{ ids: [14, 13] }]);
    await expect(page.getByRole('link', { name: 'Notices', exact: true })).toBeVisible();
    // The account menu says the same.
    await page.getByRole('button', { name: 'Asha Rao' }).click();
    await expect(page.locator('#account-panel').getByRole('link', { name: 'Notices' })).not.toContainText('unread');
  });

  test('are held during a live contest', async ({ page }) => {
    await mockApi(page, { held: true });
    await page.goto('/notices');
    await expect(page.getByText('You are in a contest now. Notices wait until it ends')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Notices', exact: true })).toBeVisible();
  });

  test('say what arrives here when there are none', async ({ page }) => {
    await mockApi(page, { noNotices: true });
    await page.goto('/notices');
    await expect(page.getByText(/^No notices yet\./)).toBeVisible();
  });

  test('keep the header to one row at 375 px with the nav on its own row', async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/notices');
    await expect(page.getByRole('heading', { level: 1, name: 'Notices' })).toBeVisible();
    const mark = (await page.locator('.wordmark').boundingBox())!;
    const bell = (await page.locator('.notices-link').boundingBox())!;
    const nav = (await page.getByRole('navigation', { name: 'Main' }).boundingBox())!;
    expect(Math.abs(mark.y + mark.height / 2 - (bell.y + bell.height / 2))).toBeLessThan(4);
    expect(nav.y).toBeGreaterThan(mark.y + mark.height - 1);
    await noOverflow(page, 'header at 375 px');
  });
});

test.describe('standing and cases', () => {
  test('/integrity shows the band and dated lines with reasons, and appeals an adjustment', async ({ page }) => {
    const api = await mockApi(page);
    await page.goto('/integrity');
    await expect(page.getByRole('heading', { level: 1, name: 'Questions and standing' })).toBeVisible();
    await expect(page.locator('.band')).toHaveText('Outcomes on record');
    const lines = page.locator('.line');
    await expect(lines.first()).toContainText('Standing adjusted by an organiser.');
    await expect(lines.first()).toContainText('You shared your solution');
    await expect(lines.nth(1)).toContainText('Under appeal');
    await expect(lines.nth(1).getByRole('link', { name: 'See the case' })).toHaveAttribute('href', '/integrity/cases/8');

    await lines.first().getByRole('button', { name: 'Appeal this decision' }).click();
    const box = page.getByLabel('Why do you think the decision is wrong?');
    await expect(box).toBeFocused();
    await box.fill('Too short');
    await page.getByRole('button', { name: 'Send appeal' }).click();
    await expect(page.getByText('Write at least 20 characters, so the organiser can understand your appeal.')).toBeVisible();
    expect(api.appeals).toEqual([]);
    await box.fill('The group message was sent after my window had closed, as the timestamps show.');
    await page.getByRole('button', { name: 'Send appeal' }).click();
    await expect(lines.first()).toContainText('Under appeal');
    expect(api.appeals).toEqual([{ adjustment_id: 40, text: 'The group message was sent after my window had closed, as the timestamps show.' }]);
  });

  test('an old notice address goes to the case page', async ({ page }) => {
    await mockApi(page);
    await page.goto('/integrity#case-12');
    await expect(page).toHaveURL(/\/integrity\/cases\/12$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeVisible();
  });

  test('the case page tells the server it was opened, highlights the lines and saves the explanation', async ({ page }) => {
    const api = await mockApi(page);
    await page.goto('/integrity/cases/12');
    await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeVisible();
    await expect.poll(() => api.opened).toEqual([12]);
    await expect(page.getByText(/Reply by \d+ \w{3}, \d\d:\d\d\./)).toBeVisible();
    await expect(page.getByText('Nothing affects your marks until a person decides.')).toBeVisible();
    await expect(page.locator('.well .line.hit')).toHaveCount(1);
    await expect(page.locator('.well .line.hit')).toContainText('if (a == 3 && b == 4)');
    await page.getByLabel('Explain how you wrote this, in your own words').fill('The special case was a test I forgot to remove.');
    await page.getByRole('button', { name: 'Save explanation' }).click();
    await expect(page.getByText('Saved. You can change it until a decision is made.')).toBeVisible();
    expect(api.explanations).toEqual([{ id: 12, body: { explanation: 'The special case was a test I forgot to remove.' } }]);
  });

  test('a decided case shows the decision, its reason and the appeal form', async ({ page }) => {
    const api = await mockApi(page);
    await page.goto('/integrity/cases/8');
    await expect(page.getByText('Zero for problem B in Week 3 contest.')).toBeVisible();
    await expect(page.getByText('Your program for problem B printed fixed answers')).toBeVisible();
    await expect(page.getByText('An organiser has decided, so you can no longer change your explanation.')).toBeVisible();
    await page.getByRole('button', { name: 'Appeal this decision' }).click();
    await page.getByLabel('Why do you think the decision is wrong?').fill('The fixed answer was a leftover debug line; the real solution runs below it.');
    await page.getByRole('button', { name: 'Send appeal' }).click();
    await expect(page.getByRole('region', { name: 'Decision' }).getByText('Under appeal')).toBeVisible();
    expect(api.appeals[0]).toMatchObject({ case_id: 8 });
  });

  test('an unknown case reads as missing', async ({ page }) => {
    await mockApi(page);
    await page.goto('/integrity/cases/999');
    await expect(page.getByText('There is no question with that number for you.')).toBeVisible();
  });
});

test('no student page prints the rating value', async ({ page }) => {
  await mockApi(page);
  for (const path of ['/', '/progress', '/notices', '/integrity', '/integrity/cases/12', '/integrity/cases/8']) {
    await page.goto(path);
    await expect(page.locator('h1')).toHaveCount(1);
    await settle(page);
    await page.waitForLoadState('networkidle');
    const text = await page.locator('body').innerText();
    expect(text, path).not.toMatch(/\brating\b/i);
    expect(text, path).not.toContain(String(LEAKED_RATING));
    expect(text, path).not.toContain((LEAKED_RATING / 10).toFixed(1));
  }
});

test('the privacy draft lists the P4 data and who sees it (F9.6)', async ({ page }) => {
  await mockApi(page);
  await page.goto('/privacy?draft');
  const main = page.locator('main');
  for (const text of [
    'Your result in each contest.',
    'Notices.',
    "Organisers' private notes.",
    'Questions about your work, and decisions.',
    'Your standing.',
    'A rating, for the organisers only.',
    "Who read a student's record.",
    'whether you chose to hide your progress page',
    'sees the record of who read a student',
  ]) {
    await expect(main.getByText(text, { exact: false }).first(), text).toBeVisible();
  }
});

test('keyboard only: notices to the case, an explanation, and an appeal on a decided case', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Your contests' })).toBeVisible();
  await activate(page, page.getByRole('link', { name: 'Notices, 2 unread' }));
  await expect(page.getByRole('heading', { level: 1, name: 'Notices' })).toBeFocused();
  await activate(page, page.getByRole('link', { name: 'Reply on the case page' }).first());
  await expect(page.getByRole('heading', { level: 1, name: 'Question about Week 6 contest' })).toBeFocused();
  const field = page.getByLabel('Explain how you wrote this, in your own words');
  await activate(page, page.getByRole('button', { name: 'Save explanation' }));
  await expect(field).toBeFocused();
  await page.keyboard.type('I wrote the special case to test the sample and forgot it.');
  await activate(page, page.getByRole('button', { name: 'Save explanation' }));
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  expect(api.explanations).toHaveLength(1);

  await page.goto('/integrity/cases/8');
  await expect(page.getByText('Zero for problem B in Week 3 contest.')).toBeVisible();
  await activate(page, page.getByRole('button', { name: 'Appeal this decision' }));
  await expect(page.getByLabel('Why do you think the decision is wrong?')).toBeFocused();
  await page.keyboard.type('The fixed answer was a leftover debug line; the real solution follows it.');
  await tabTo(page, page.getByRole('button', { name: 'Send appeal' }));
  await page.keyboard.press('Enter');
  await expect(page.getByRole('region', { name: 'Decision' }).getByText('Under appeal')).toBeVisible();
  expect(api.appeals).toHaveLength(1);
});

// v2 P4 organiser screens (F9.2b, F9.4) on tests/mocks/organiser.ts: the
// students table, a student's page with the trend beside the class
// median, and the record page with the rating, its history, the edit
// form and the two-person rule. Screenshots go to
// cache/screenshots-p4-organiser/.
for (const theme of org.THEMES) {
  test.describe(`organiser, ${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const s of org.SCREENS) {
      test(`${s.name} at four widths`, async ({ page }) => {
        await org.mockApi(page, s.options);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(s.path);
        await s.ready(page);
        await expect(page).toHaveTitle(s.title);
        await expect(page.locator('h1')).toHaveCount(1);
        for (const width of org.WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await org.settle(page);
          await noOverflow(page, `${s.name} at ${width}px`);
          await page.screenshot({ path: `${org.shots()}${s.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

test.describe('the students table', () => {
  test('is sorted by name, sorts by rating on request, and filters say which contests they used', async ({ page }) => {
    const api = await org.mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/students');
    const nav = page.getByRole('navigation', { name: 'Main' });
    await expect(nav.getByRole('link', { name: 'Students' })).toHaveAttribute('aria-current', 'page');
    const table = page.getByRole('table', { name: /^Students, sorted by name, ascending/ });
    const names = page.locator('tbody th a');
    // Case and accents do not change the order.
    await expect(names).toHaveText(['Asha Rao', 'bilal Khan', 'Kabir Shah', 'Meera Iyer', 'Zoë Fernandes']);
    await expect(table.getByRole('columnheader', { name: /^Name/ })).toHaveAttribute('aria-sort', 'ascending');
    await expect(table.locator('tbody tr').first()).toContainText('7.5');
    await expect(table.locator('tbody tr').first()).toContainText('Outcomes on record');

    await table.getByRole('button', { name: /^Rating/ }).click();
    const sorted = page.getByRole('table', { name: /^Students, sorted by rating/ });
    await expect(sorted.getByRole('columnheader', { name: /^Rating/ })).toHaveAttribute('aria-sort', 'ascending');
    await expect(names.first()).toHaveText('Zoë Fernandes');
    await sorted.getByRole('button', { name: /^Rating/ }).click();
    await expect(sorted.getByRole('columnheader', { name: /^Rating/ })).toHaveAttribute('aria-sort', 'descending');
    await expect(names.first()).toHaveText('bilal Khan');

    await page.getByLabel('Show').selectOption('missed_last_two');
    await expect(page.getByRole('heading', { level: 2, name: 'Missed the last 2 graded contests' })).toBeVisible();
    await expect(names).toHaveText(['Meera Iyer']);
    await expect(page.locator('#filter-help')).toHaveText('Students who could enter Week 6 contest and Week 5 contest and are on neither board. Practice does not count.');
    await page.getByLabel('Show').selectOption('no_accepted');
    await expect(names).toHaveText(['Zoë Fernandes']);
    await page.getByLabel('Show').selectOption('');
    await expect(names).toHaveCount(5);
    await page.getByLabel('Name or email').fill('nobody');
    await page.getByRole('button', { name: 'Search' }).click();
    await expect(page.getByText('No student matches "nobody". Check the spelling, or clear the search.')).toBeVisible();
    expect(api.studentQueries).toEqual(['', '?filter=missed_last_two', '?filter=no_accepted', '', '?q=nobody']);
    // The address keeps the search, so a reload shows the same rows.
    await expect(page).toHaveURL(/\/admin\/students\?q=nobody$/);
    await page.goto('/admin/students?filter=no_accepted');
    await expect(page.getByLabel('Show')).toHaveValue('no_accepted');
    await expect(names).toHaveText(['Zoë Fernandes']);
  });

  test('stacks on a phone and opens a student', async ({ page }) => {
    await org.mockApi(page);
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/admin/students');
    const row = page.locator('tbody tr').first();
    await expect(row).toHaveCSS('display', 'grid');
    await expect(row).toContainText('Graded contests');
    await row.getByRole('link', { name: 'Asha Rao' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Asha Rao' })).toBeFocused();
  });
});

test.describe('a student page', () => {
  test('shows the trend beside the class median, as a table on request, and links to the record', async ({ page }) => {
    await org.mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/students/7');
    const chart = page.getByRole('img', { name: /^Score as a percentage over 4 graded contests/ });
    // The ungraded warm-up is left out; the latest is compared with the median.
    await expect(chart).toHaveAccessibleName('Score as a percentage over 4 graded contests. Latest, Week 6 contest: 80 percent, above the class median of 62.5 percent.');
    await expect(page.getByRole('list', { name: 'Legend' }).getByRole('listitem')).toHaveText(['This student', 'Class median']);
    await page.getByRole('button', { name: 'Show as table' }).click();
    const table = page.getByRole('table', { name: 'Score in each graded contest beside the class median, oldest first' });
    await expect(table.locator('tbody tr')).toHaveCount(4);
    await expect(table.locator('tbody tr').last()).toContainText('62.5%');
    const contests = page.getByRole('table', { name: 'Contests Asha Rao took part in, newest first' });
    await expect(contests.locator('tbody tr').first()).toContainText('240 of 300');
    await expect(contests.locator('tbody tr').filter({ hasText: 'Warm-up practice' })).toContainText('not graded');
    await expect(page.getByRole('link', { name: 'See their progress page' })).toHaveAttribute('href', '/admin/users/7/progress');
    await page.getByRole('link', { name: 'Open the record' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Asha Rao: record' })).toBeFocused();
  });

  test('an unknown student reads as missing', async ({ page }) => {
    await org.mockApi(page);
    await page.goto('/admin/students/99');
    await expect(page.getByText('There is no student with that number, or their account was erased.')).toBeVisible();
  });
});

test.describe('the record page', () => {
  test('shows the rating, marks the line that crossed a band, and says the visit is logged', async ({ page }) => {
    await org.mockApi(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/students/7/record');
    await expect(page.getByText('Each visit to this page is logged for the owner.', { exact: false })).toBeVisible();
    const rating = page.getByRole('region', { name: 'Rating', exact: true });
    await expect(rating.locator('.figure')).toHaveText('7.5 of 10');
    await expect(rating).toContainText('Outcomes on record');
    const lines = page.locator('section[aria-labelledby="history-title"] li');
    await expect(lines).toHaveCount(4);
    // Only the zero that took Asha out of Good standing carries the crossing.
    await expect(page.getByText('Dropped from Good standing to Outcomes on record')).toHaveCount(1);
    await expect(lines.nth(2)).toContainText('Dropped from Good standing to Outcomes on record');
    await expect(lines.nth(2)).toContainText('By Ravi Kumar, confirmed by Arjun Mehta');
    await expect(lines.nth(1)).toContainText('semester 2026 monsoon');
    await expect(page.getByRole('link', { name: 'Week 3 contest, case 12' })).toHaveAttribute('href', '/admin/integrity/cases/12');
    await expect(page.getByText('Deleted on', { exact: false })).toBeVisible();
  });

  test('the edit form refuses an empty or short reason before sending anything', async ({ page }) => {
    const api = await org.mockApi(page);
    await page.goto('/admin/students/7/record');
    await page.getByLabel('New rating, 0 to 10').fill('7');
    await page.getByRole('button', { name: 'Change rating' }).click();
    await expect(page.getByText('Write the reason first. The student reads it on their standing.')).toBeVisible();
    await expect(page.getByLabel('Reason, for the student')).toBeFocused();
    await expect(page.getByLabel('Reason, for the student')).toHaveAttribute('aria-invalid', 'true');
    await page.getByLabel('Reason, for the student').fill('Too short');
    await page.getByRole('button', { name: 'Change rating' }).click();
    await expect(page.getByText('Write at least 20 characters, so the student can understand the change.')).toBeVisible();
    await page.getByLabel('New rating, 0 to 10').fill('7.5');
    await page.getByRole('button', { name: 'Change rating' }).click();
    await expect(page.getByText('The rating is already 7.5. Enter a different value.')).toBeVisible();
    await expect(page.getByLabel('New rating, 0 to 10')).toBeFocused();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(api.ratingPuts).toEqual([]);
  });

  test('an edit inside the band applies at once; one that drops the band waits for a second organiser', async ({ page }) => {
    const api = await org.mockApi(page);
    await page.goto('/admin/students/7/record');
    const value = page.getByLabel('New rating, 0 to 10');
    const reason = page.getByLabel('Reason, for the student');
    await value.fill('7');
    await reason.fill('A late extension request was granted after the fact, so the warning is lighter.');
    await page.getByRole('button', { name: 'Change rating' }).click();
    const same = page.getByRole('dialog', { name: "Change Asha Rao's rating from 7.5 to 7?" });
    await expect(same).toContainText('The band stays Outcomes on record.');
    await expect(same).toContainText('The change applies at once.');
    await same.getByRole('button', { name: 'Change the rating' }).click();
    await expect(page.locator('.toasts').getByText('Changed the rating to 7')).toBeVisible();
    await expect(page.locator('.figure')).toHaveText('7 of 10');
    // The edit wrote a standing line too, so the record is read again (C2-032).
    await expect.poll(() => api.recordGets).toBe(2);

    await value.fill('5.5');
    await reason.fill('A second outcome this semester was confirmed in Week 6, so the standing drops a band.');
    await page.getByRole('button', { name: 'Change rating' }).click();
    const down = page.getByRole('dialog', { name: "Change Asha Rao's rating from 7 to 5.5?" });
    await expect(down).toContainText('The band goes from Outcomes on record to Repeated outcomes.');
    await expect(down).toContainText('waits until a second owner or admin confirms it');
    await down.getByRole('button', { name: 'Propose the change' }).click();
    await expect(page.getByRole('heading', { level: 3, name: 'Waiting for a second organiser' })).toBeFocused();
    await expect(page.locator('.figure')).toHaveText('7 of 10');
    await expect(page.getByText('A band that drops needs a second owner or admin, who did not propose it, to confirm.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Confirm the change' })).toHaveCount(0);
    expect(api.ratingPuts.map((b) => b.value_tenths)).toEqual([70, 55]);
  });

  test('a second organiser confirms a waiting change', async ({ page }) => {
    const api = await org.mockApi(page, { pending: 'theirs' });
    await page.goto('/admin/students/7/record');
    const box = page.getByRole('group', { name: 'Waiting for a second organiser' });
    await expect(box).toContainText('Ravi Kumar proposed 7.5 to 5.5');
    await expect(box).toContainText('Outcomes on record to Repeated outcomes');
    await box.getByRole('button', { name: 'Confirm the change' }).click();
    await expect(page.locator('.toasts').getByText('Confirmed: the rating is now 5.5')).toBeVisible();
    await expect(page.locator('.figure')).toHaveText('5.5 of 10');
    await expect(page.getByText('Dropped from Outcomes on record to Repeated outcomes')).toBeVisible();
    expect(api.confirms).toEqual([{ pid: 5, kind: 'confirm', body: {} }]);
  });

  test('an appeal against an adjustment is ruled on here, and an outcome appeal points to its case', async ({ page }) => {
    const api = await org.mockApi(page);
    await page.goto('/admin/students/7/record');
    const appeal = page.getByRole('article', { name: 'Appeal by Asha Rao against a manual adjustment of the standing' });
    await appeal.getByRole('button', { name: 'Rule on the appeal' }).click();
    await expect(appeal.getByText('Choose whether the appeal is upheld.')).toBeVisible();
    await appeal.getByLabel(/Uphold the appeal/).check();
    await appeal.getByLabel('Reason for the ruling, for the student').fill('The group messages are timestamped after the contest closed.');
    await appeal.getByRole('button', { name: 'Rule on the appeal' }).click();
    const sure = page.getByRole('dialog', { name: 'Uphold the appeal?' });
    await sure.getByRole('button', { name: 'Uphold the appeal' }).click();
    await expect(page.locator('.toasts').getByText('Upheld the appeal')).toBeVisible();
    expect(api.rulings).toEqual([{ id: 70, upheld: true, reason: 'The group messages are timestamped after the contest closed.' }]);
    await expect(page.getByRole('link', { name: 'Rule on it on the case page' })).toHaveAttribute('href', '/admin/integrity/cases/12#appeals-title');
    await expect(page.getByText('Overdue').first()).toBeVisible();
  });

  test('nobody opens their own record', async ({ page }) => {
    await org.mockApi(page, { own: true });
    await page.goto('/admin/students/7/record');
    await expect(page.getByText('Nobody opens their own record. Another owner or admin can.')).toBeVisible();
  });
});

test('with no students yet, the list says how students arrive (C2-024)', async ({ page }) => {
  await org.mockApi(page, {});
  await page.route(
    (url) => url.pathname === '/api/admin/students',
    (route) => route.fulfill({ json: { students: [], filter: '', last_graded: [], server_now: Date.now() } }),
  );
  await page.goto('/admin/students');
  const empty = page.locator('p.empty');
  await expect(empty).toContainText('No students yet. A student appears here after signing in with an allowed email domain');
  await expect(empty.getByRole('link', { name: 'People' })).toHaveAttribute('href', '/admin/people');
  await expect(empty.getByRole('link', { name: 'Contests' })).toHaveAttribute('href', '/admin/contests');
});

test('a slow older answer never shows under a newer filter (C2-032)', async ({ page }) => {
  await org.mockApi(page, {});
  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(
    (url) => url.pathname === '/api/admin/students' && url.searchParams.get('filter') === 'missed_last_two',
    async (route) => {
      await held;
      await route.fallback();
    },
  );
  await page.goto('/admin/students');
  const names = page.locator('tbody th a');
  await expect(names).toHaveCount(5);
  await page.getByLabel('Show').selectOption('missed_last_two');
  await page.getByLabel('Show').selectOption('no_accepted');
  await expect(page.getByRole('heading', { level: 2, name: '10 or more submissions, nothing accepted' })).toBeVisible();
  release();
  await page.waitForResponse((r) => r.url().includes('filter=missed_last_two'));
  await expect(page.getByRole('heading', { level: 2, name: '10 or more submissions, nothing accepted' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Missed the last 2 graded contests' })).toHaveCount(0);
  await expect(names).toHaveText(['Zoë Fernandes']);
});
