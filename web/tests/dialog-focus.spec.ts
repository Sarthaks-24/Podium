// Where focus lands after Escape closes a dialog (components/Dialog). The
// opener when it is still there; the title link of the row it sat in when a
// re-render replaced it; else the page heading. Covers the dialogs that the
// problem screens' mock (tests/mocks/problems.ts) can open: the bank's
// Restore (both steps), the import conflict, the problem editor's Archive
// and Add to contest, and the shortcut list.
import { expect, test, type Page } from '@playwright/test';
import { mockApi, zipFile } from './mocks/problems';

const dialogs = (page: Page) => page.getByRole('dialog');

test('Escape on the bank Restore confirm returns focus to that row Restore button', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems?archived=1');
  const restore = page.getByRole('button', { name: 'Restore Old sorting' });
  await restore.click();
  await expect(page.getByRole('dialog', { name: 'Restore Old sorting?' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialogs(page)).toHaveCount(0);
  await expect(restore).toBeFocused();
});

test('Escape on the Restore slug step returns focus to the row Restore button', async ({ page }) => {
  const state = await mockApi(page);
  state.restoreTaken = true;
  await page.goto('/admin/problems?archived=1');
  const restore = page.getByRole('button', { name: 'Restore Old sorting' });
  await restore.click();
  await page.getByRole('dialog', { name: 'Restore Old sorting?' }).getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('dialog', { name: 'Restore Old sorting' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialogs(page)).toHaveCount(0);
  await expect(restore).toBeFocused();
});

test('after a restore the row title link takes focus, the Restore button being gone', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems?archived=1');
  await page.getByRole('button', { name: 'Restore Old sorting' }).click();
  await page.getByRole('dialog', { name: 'Restore Old sorting?' }).getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('button', { name: 'Restore Old sorting' })).toHaveCount(0);
  await expect(page.getByRole('row', { name: /Old sorting/ }).getByRole('link', { name: 'Old sorting', exact: true })).toBeFocused();
});

test('Escape on the import conflict returns focus to the Import pack button', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems');
  await page.locator('input[type="file"]').setInputFiles(zipFile('dupe.zip', 'dupe pack'));
  await expect(page.getByRole('dialog', { name: 'A problem with the slug a-plus-b already exists' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialogs(page)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Import pack' })).toBeFocused();
});

test('Escape on the problem editor Archive dialog returns focus to More', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const more = page.getByRole('button', { name: 'More' });
  await more.click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Archive' }).click();
  await expect(page.getByRole('dialog', { name: 'Archive A plus B?' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialogs(page)).toHaveCount(0);
  await expect(more).toBeFocused();
});

test('Escape on the shortcut list returns focus to the heading it opened from', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const h1 = page.locator('main h1');
  await h1.focus();
  await page.keyboard.press('?');
  await expect(page.getByRole('dialog', { name: 'Help' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialogs(page)).toHaveCount(0);
  await expect(h1).toBeFocused();
});
