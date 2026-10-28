// C2-055, against a live testing-mode server with A plus B imported and
// validated (the M2 end-to-end run imports it): an organiser runs a contest
// with A plus B; a student opens problem A; the organiser edits the pinned
// problem's statement while the contest is live, which needs a reason, and
// moves the contest to the new revision from the console; the student's
// open workspace then shows "Statement updated" and the line diff. The
// contest is ended at the end, so no later edit of A plus B needs a reason.
import { expect, test, type Browser, type Page } from '@playwright/test';

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
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  return page;
}

test('a statement edited mid-contest reaches the student with its diff', async ({ browser }) => {
  test.setTimeout(240_000);
  const stamp = Date.now().toString(36);
  const title = `Live history ${stamp}`;
  const line = `Revision check ${stamp}: the answer fits in a 64-bit integer.`;

  // The organiser creates a running contest with A plus B and publishes it.
  const admin = await signIn(browser, 'live-history@college.edu', 'admin');
  await admin.goto('/admin/contests');
  await admin.getByRole('button', { name: 'New contest' }).click();
  await admin.getByLabel('Title').fill(title);
  await admin.getByLabel('Starts').fill(localInput(Date.now() - 2 * 60_000));
  await admin.getByLabel('Ends').fill(localInput(Date.now() + 60 * 60_000));
  await admin.getByRole('button', { name: 'Create contest' }).click();
  await expect(admin.getByRole('heading', { level: 1, name: title })).toBeVisible();
  const contestPath = new URL(admin.url()).pathname;
  const add = admin.getByLabel('Add a validated problem');
  const aPlusB = add.locator('option', { hasText: 'A plus B' });
  await add.selectOption({ label: (await aPlusB.first().textContent())!.trim() });
  await admin.getByRole('button', { name: 'Add problem' }).click();
  await expect(admin.getByLabel(/^Letter for /)).toHaveCount(1);
  await admin.getByRole('button', { name: 'Publish contest' }).click();
  await admin.getByRole('dialog').getByRole('button', { name: 'Publish contest' }).click();
  await expect(admin.getByRole('button', { name: 'Unpublish contest' })).toBeVisible();

  try {
    // The student starts and sits on problem A.
    const student = await signIn(browser, `live-history-${stamp}@college.edu`, 'student');
    await student.getByRole('link', { name: title }).click();
    await student.getByRole('button', { name: 'I agree, start the contest' }).click();
    await expect(student.getByRole('heading', { level: 1 })).toHaveText('A. A plus B');

    // The organiser edits the pinned problem: a live contest makes the reason required.
    await admin.goto('/admin/problems');
    await admin.getByRole('row', { name: /A plus B/ }).getByRole('link', { name: 'A plus B', exact: true }).click();
    await expect(admin.locator('.live-note')).toContainText(title);
    await expect(admin.locator('.live-note')).toContainText('live with this problem.');
    const statement = admin.getByLabel('Statement', { exact: true });
    const before = (await statement.inputValue()).replace(/\n*Revision check [^\n]*$/, '');
    await statement.fill(`${before}\n\n${line}`);
    await admin.getByRole('button', { name: 'Save changes' }).click();
    await expect(admin.locator('#reason-error')).toBeVisible();
    await expect(admin.getByLabel('Reason for this change')).toBeFocused();
    await admin.getByLabel('Reason for this change').fill(`Live history check ${stamp}`);
    await admin.getByRole('button', { name: 'Save changes' }).click();
    const history = admin.getByRole('region', { name: 'History' });
    const newest = history.getByRole('listitem').first();
    await expect(newest).toContainText('Statement changed.');
    await expect(newest).toContainText(`Reason: Live history check ${stamp}`);
    const rev = Number((await newest.locator('.rev').innerText()).replace(/\D/g, ''));

    // The console, one link away, moves the contest to the new revision.
    await admin.locator('.live-note').getByRole('link', { name: `Open the console of ${title}` }).click();
    await expect(admin).toHaveURL(new RegExp(`${contestPath}/console#versions-title$`));
    await expect(admin.getByRole('region', { name: 'Needs attention' })).toContainText('Problem A has a newer revision for students');
    const versions = admin.getByRole('region', { name: 'Problem revisions' });
    await versions.getByRole('button', { name: 'Update students now for problem A' }).click();
    const confirm = admin.getByRole('dialog', { name: 'Update students now to the new version of problem A?' });
    await expect(confirm).toContainText('Only the statement changed, so no submission is judged again.');
    await confirm.getByRole('button', { name: 'Update students now' }).click();
    await expect(admin.getByText('Students now get the new version of problem A', { exact: true })).toBeVisible();
    await expect(versions).toContainText(`A plus B, revision ${rev}`);

    // The student's open workspace shows the update and the diff, without a reload.
    const update = student.getByText(/^Statement updated \d\d:\d\d\./);
    await expect(update).toBeVisible({ timeout: 30_000 });
    await expect(student.locator('main')).toContainText(line);
    await student.getByRole('button', { name: 'See what changed' }).click();
    const diff = student.getByRole('dialog', { name: 'What changed in problem A' });
    await expect(diff.locator('.line.add').filter({ hasText: line })).toHaveCount(1);
    await student.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-history/live-diff-1440.png` });
    await diff.getByRole('button', { name: /Close/ }).click();
  } finally {
    // Leave nothing live with A plus B, pass or fail.
    await admin.goto(`${contestPath}/console`);
    await admin.getByRole('button', { name: 'End the contest now' }).click();
    await admin.getByRole('dialog', { name: 'End the contest now?' }).getByRole('button', { name: 'End the contest' }).click();
    await expect(admin.getByText('Ended the contest', { exact: true })).toBeVisible();
  }
});
