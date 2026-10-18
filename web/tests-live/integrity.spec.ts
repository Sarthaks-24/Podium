// Against a live testing-mode server with A plus B validated (the M2
// end-to-end run imports it): an organiser creates a short contest that
// ends four minutes after it starts; two students, in their own browser
// contexts, solve A with the same program under different names (with a
// multibyte comment, so the spans cross UTF-8 characters); after the end
// the organiser runs the integrity check, finds the similar-code signal,
// compares the code, opens a case and shares it; the first student sees
// the notice, their own evidence and saves an explanation; the organiser
// sees it and clears the case. Takes about six minutes, most of it waiting
// for the contest to end.
import { expect, test, type Browser, type Page } from '@playwright/test';

const PROGRAM_A = [
  '# Somme des paires, écrite à la main: lire, découper, additionner.',
  'import sys',
  'def read_all():',
  '    data = sys.stdin.read().split()',
  '    return [int(x) for x in data]',
  'def solve(values):',
  '    count = values[0]',
  '    out = []',
  '    for i in range(count):',
  '        left = values[1 + 2 * i]',
  '        right = values[2 + 2 * i]',
  '        out.append(str(left + right))',
  '    return out',
  "print('\\n'.join(solve(read_all())))",
].join('\n');

// The same program with every name changed and another comment.
const PROGRAM_B = PROGRAM_A.replace('# Somme des paires, écrite à la main: lire, découper, additionner.', '# sum pairs 🙂')
  .replaceAll('read_all', 'grab')
  .replaceAll('values', 'nums')
  .replaceAll('data', 'raw')
  .replaceAll('solve', 'work')
  .replaceAll('left', 'p')
  .replaceAll('right', 'q')
  .replaceAll('out', 'res');

function localInput(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

async function signIn(browser: Browser, email: string, role: 'admin' | 'student'): Promise<Page> {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/signin');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Role').selectOption(role);
  await page.getByRole('button', { name: 'Sign in for testing' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Your contests' })).toBeVisible();
  return page;
}

async function solve(page: Page, title: string, source: string) {
  await page.getByRole('link', { name: title }).click();
  await page.getByRole('button', { name: 'I agree, start the contest' }).click();
  await page.getByLabel('Language').selectOption('python3');
  const editor = page.locator('.cm-content');
  await editor.click();
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.press('Delete');
  await page.keyboard.insertText(source);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Result' }).locator('p.headline')).toHaveText('Accepted', { timeout: 120_000 });
}

test('an organiser checks a finished contest, opens a case and a student explains', async ({ browser }) => {
  test.setTimeout(600_000);
  const stamp = Date.now().toString(36);
  const title = `Live integrity ${stamp}`;
  const shots = `${test.info().project.testDir}/../../cache/screenshots-integrity/`;

  const admin = await signIn(browser, 'live-reviewer@college.edu', 'admin');
  await admin.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contest admin' }).click();
  await admin.getByRole('button', { name: 'New contest' }).click();
  await admin.getByLabel('Title').fill(title);
  const endsAt = Date.now() + 4 * 60_000;
  await admin.getByLabel('Starts').fill(localInput(Date.now() - 60_000));
  await admin.getByLabel('Ends').fill(localInput(endsAt));
  await admin.getByRole('button', { name: 'Create contest' }).click();
  await expect(admin.getByRole('heading', { level: 1, name: title })).toBeVisible();
  const add = admin.getByLabel('Add a validated problem');
  const aPlusB = add.locator('option', { hasText: 'A plus B' });
  await add.selectOption({ label: (await aPlusB.first().textContent())!.trim() });
  await admin.getByRole('button', { name: 'Add problem' }).click();
  await admin.getByRole('button', { name: 'Save problems' }).click();
  await expect(admin.getByText('Saved problems')).toBeVisible();
  await admin.getByLabel('Published: students can see the contest').check();
  await admin.getByRole('button', { name: 'Save changes' }).click();
  await expect(admin.getByText('Published', { exact: true })).toBeVisible();

  const first = await signIn(browser, `live-int-a-${stamp}@college.edu`, 'student');
  const second = await signIn(browser, `live-int-b-${stamp}@college.edu`, 'student');
  await solve(first, title, PROGRAM_A);
  await solve(second, title, PROGRAM_B);

  // While the contest runs the check stays closed.
  await admin.getByRole('link', { name: 'Integrity', exact: true }).click();
  await expect(admin.getByRole('button', { name: 'Check integrity' })).toBeDisabled();
  await expect(admin.getByText('Check integrity after the contest ends for everyone.')).toBeVisible();

  // Wait for the end (the minute picker rounds down, so up to a minute early).
  await expect
    .poll(
      async () => {
        await admin.reload();
        return admin.getByRole('button', { name: 'Check integrity' }).isEnabled();
      },
      { timeout: endsAt - Date.now() + 120_000, intervals: [15_000] },
    )
    .toBe(true);
  await admin.getByRole('button', { name: 'Check integrity' }).click();
  const table = admin.getByRole('table', { name: /^Signals on A\./ });
  await expect(table).toBeVisible({ timeout: 90_000 });
  const row = table.getByRole('row').filter({ hasText: 'Similar code' }).first();
  await expect(row).toBeVisible();
  await admin.screenshot({ path: `${shots}live-integrity-1440.png`, fullPage: true });

  await row.getByRole('link', { name: /^Compare code/ }).click();
  await expect(admin.locator('mark').first()).toBeVisible();
  await expect(admin.getByText('Shared passage starts.').first()).toBeAttached();
  await admin.screenshot({ path: `${shots}live-pair-1440.png`, fullPage: true });

  await admin.getByRole('button', { name: 'Open a case' }).click();
  const dialog = admin.getByRole('dialog', { name: 'Open a case' });
  await dialog.getByLabel('Reason').fill('Same program with every name changed, live run.');
  await dialog.getByRole('button', { name: 'Open the case' }).click();
  await expect(admin).toHaveURL(/\/admin\/integrity\/cases\/\d+$/);
  await admin.getByLabel('Note for the log').fill('Asking both students to explain, live run.');
  await admin.getByRole('button', { name: 'Share with the students' }).click();
  await admin.getByRole('dialog', { name: 'Share with the students?' }).getByRole('button', { name: 'Share with the students' }).click();
  await expect(admin.getByRole('button', { name: 'Take action' })).toBeEnabled();

  // The student sees the notice and only their own work.
  await first.goto('/');
  await first.getByRole('link', { name: 'Write your explanation' }).click();
  await expect(first.getByText('Waiting for your explanation.', { exact: false })).toBeVisible();
  await expect(first.locator('mark').first()).toBeVisible();
  await expect(first.locator('main')).not.toContainText('live-int-b');
  await first.getByLabel('Explain how you wrote this, in your own words').fill('I wrote it myself during the contest, live run.');
  await first.getByRole('button', { name: 'Save explanation' }).click();
  await expect(first.getByText('Saved. You can change it until a decision is made.')).toBeVisible();
  await first.screenshot({ path: `${shots}live-student-1440.png`, fullPage: true });

  await admin.reload();
  await expect(admin.getByText('I wrote it myself during the contest, live run.')).toBeVisible();
  await admin.getByLabel('Note for the log').fill('Explanation accepted after a short talk, live run.');
  await admin.getByRole('button', { name: 'Clear' }).click();
  await expect(admin.getByRole('button', { name: 'Reopen the case' })).toBeVisible();
});
