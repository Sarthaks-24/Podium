// Against a live testing-mode server with the example packs imported (the
// M2 end-to-end run imports them): sign in from the form, open the problem
// bank, open a validated problem, and read its validation report.
import { expect, test } from '@playwright/test';

test('a setter signs in and reads a validation report', async ({ page }) => {
  await page.goto('/signin');
  await expect(page.getByRole('note')).toContainText('Testing mode');
  await page.getByLabel('Email').fill('live-setter@college.edu');
  await page.getByLabel('Role').selectOption('admin');
  await page.getByRole('button', { name: 'Sign in for testing' }).click();

  await page.getByRole('link', { name: 'Problems' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Problems' })).toBeVisible();
  const row = page.getByRole('row', { name: /A plus B/ });
  await expect(row).toContainText('Validated');

  await row.getByRole('link', { name: 'A plus B' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toContainText('A plus B');
  await expect(page.getByText(/Reference solution passed 6 of 6 tests/)).toBeVisible();
  await expect(page.getByText(/Validator accepted 6 of 6 inputs/)).toBeVisible();
  // The statement preview renders KaTeX from the real statement.
  await expect(page.locator('.katex').first()).toBeVisible();
  await page.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m2/live-editor-1280.png`, fullPage: true });
});
