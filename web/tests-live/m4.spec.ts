// Against a live testing-mode server with at least two validated problems
// (the M2 end-to-end run imports A plus B and the other example packs): an
// organiser creates a contest that has already started with two problems
// and publishes it; a student, in a second browser context, opens it from
// "Your contests", agrees to the honour code, solves A in Python and finds
// their own row on the standings. The contest is ended at the end, so no
// later edit of A plus B needs a reason.
import { expect, test, type Browser, type Page } from '@playwright/test';

const AC = [
  'import sys',
  'd=sys.stdin.read().split()',
  't=int(d[0])',
  "print('\\n'.join(str(int(d[1+2*i])+int(d[2+2*i])) for i in range(t)))",
].join('\n');

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
  // Students land on "Your contests", organisers on "Contests".
  await expect(page.getByRole('heading', { level: 1, name: role === 'admin' ? 'Contests' : 'Your contests' })).toBeVisible();
  return page;
}

async function typeCode(page: Page, source: string) {
  const editor = page.locator('.cm-content');
  await editor.click();
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.press('Delete');
  await page.keyboard.insertText(source);
}

test('an organiser runs a contest that a student takes', async ({ browser }) => {
  test.setTimeout(240_000);
  const stamp = Date.now().toString(36);
  const title = `Live M4 ${stamp}`;

  // The organiser creates, fills and publishes the contest.
  const admin = await signIn(browser, 'live-setter@college.edu', 'admin');
  await admin.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contests', exact: true }).click();
  await admin.getByRole('button', { name: 'New contest' }).click();
  await admin.getByLabel('Title').fill(title);
  await admin.getByLabel('Starts').fill(localInput(Date.now() - 2 * 60_000));
  await admin.getByLabel('Ends').fill(localInput(Date.now() + 2 * 3_600_000));
  await admin.getByRole('button', { name: 'Create contest' }).click();
  await expect(admin.getByRole('heading', { level: 1, name: title })).toBeVisible();
  const contestPath = new URL(admin.url()).pathname;

  // Adding a problem saves it at once; publishing asks first and lists the checks.
  const add = admin.getByLabel('Add a validated problem');
  const aPlusB = add.locator('option', { hasText: 'A plus B' });
  await add.selectOption({ label: (await aPlusB.first().textContent())!.trim() });
  await admin.getByRole('button', { name: 'Add problem' }).click();
  await add.selectOption({ index: 1 });
  await admin.getByRole('button', { name: 'Add problem' }).click();
  await expect(admin.getByLabel(/^Letter for /)).toHaveCount(2);
  await admin.getByRole('button', { name: 'Publish contest' }).click();
  await admin.getByRole('dialog').getByRole('button', { name: 'Publish contest' }).click();
  await expect(admin.getByRole('button', { name: 'Unpublish contest' })).toBeVisible();
  await admin.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m4/live-editor-1440.png`, fullPage: true });

  try {
    // The student takes it.
    const student = await signIn(browser, `live-student-${stamp}@college.edu`, 'student');
    await student.getByRole('link', { name: title }).click();
    await expect(student.getByRole('heading', { name: 'Honour code' })).toBeVisible();
    await student.getByRole('button', { name: 'I agree, start the contest' }).click();
    const rail = student.getByRole('navigation', { name: 'Problems' });
    await expect(rail.locator('.problems').getByRole('link')).toHaveCount(2);
    await expect(student.getByRole('timer')).toBeVisible();

    await student.getByLabel('Language').selectOption('python3');
    await typeCode(student, AC);
    await student.getByRole('button', { name: 'Submit', exact: true }).click();
    const result = student.getByRole('region', { name: 'Result' });
    await expect(result.locator('p.headline')).toHaveText('Accepted', { timeout: 120_000 });
    await expect(rail.getByRole('link', { name: /Problem A, A plus B, 100 of 100 points/ })).toBeVisible({ timeout: 30_000 });
    await student.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m4/live-workspace-1440.png` });

    await rail.getByRole('link', { name: 'Standings' }).click();
    await expect(student.getByRole('heading', { level: 1, name: 'Standings' })).toBeVisible();
    const mine = student.locator('tr.me');
    await expect(mine).toContainText('(you)', { timeout: 30_000 });
    await expect(mine.locator('td.total')).toContainText('100');
    await student.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m4/live-standings-1440.png` });

    // The organiser sees the submission in the console feed.
    await admin.getByRole('link', { name: 'Console', exact: true }).click();
    await expect(admin.locator('.feed')).toContainText('Accepted', { timeout: 30_000 });
  } finally {
    // Leave nothing live with A plus B, pass or fail.
    await admin.goto(`${contestPath}/console`);
    await admin.getByRole('button', { name: 'End the contest now' }).click();
    await admin.getByRole('dialog', { name: 'End the contest now?' }).getByRole('button', { name: 'End the contest' }).click();
    await expect(admin.getByText('Ended the contest', { exact: true })).toBeVisible();
  }
});
