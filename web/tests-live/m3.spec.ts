// Against a live testing-mode server with A plus B imported and validated
// (the M2 and M3 end-to-end runs do this): an organiser opens "Try as a
// student", runs a Python solution on the samples, submits a wrong one and
// reads the partial score, then submits a correct one and reads "Accepted".
import { expect, test, type Page } from '@playwright/test';

const AC = [
  'import sys',
  'd=sys.stdin.read().split()',
  't=int(d[0])',
  "print('\\n'.join(str(int(d[1+2*i])+int(d[2+2*i])) for i in range(t)))",
].join('\n');
// Correct on small numbers, wrong past 10^12: passes the samples and subtask 1.
const WA = [
  'import sys',
  'd=sys.stdin.read().split()',
  't=int(d[0])',
  's=[int(d[1+2*i])+int(d[2+2*i]) for i in range(t)]',
  "print('\\n'.join(str(x if abs(x)<10**12 else x//10) for x in s))",
].join('\n');

async function typeCode(page: Page, source: string) {
  const editor = page.locator('.cm-content');
  await editor.click();
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.press('Delete');
  // insertText skips CodeMirror's auto-indent and bracket closing.
  await page.keyboard.insertText(source);
}

test('an organiser tries a problem as a student', async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/signin');
  await page.getByLabel('Email').fill('live-setter@college.edu');
  await page.getByLabel('Role').selectOption('admin');
  await page.getByRole('button', { name: 'Sign in for testing' }).click();

  await page.getByRole('link', { name: 'Problems' }).click();
  await page.getByRole('row', { name: /A plus B/ }).getByRole('link', { name: 'A plus B', exact: true }).click();
  await page.getByRole('link', { name: 'Try as a student' }).click();

  await page.getByLabel('Language').selectOption('python3');
  await typeCode(page, AC);
  await page.getByRole('button', { name: 'Run on samples' }).first().click();
  const result = page.getByRole('region', { name: 'Result' });
  await expect(result).toContainText('3\n7', { timeout: 60_000 });

  await typeCode(page, WA);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(result.locator('p.headline')).toHaveText('Partial, 40 of 100', { timeout: 90_000 });

  // The organiser gap between submits is 5 seconds.
  await page.waitForTimeout(5_500);
  await typeCode(page, AC);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(result.locator('p.headline')).toHaveText('Accepted', { timeout: 90_000 });
  await page.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m3/live-workspace-1440.png` });
});
