// The profile page (v4 M1): /me shows the person's own picture (or their
// initials), name, email, role and dates, then the theme and the editor's
// preferences, the same controls as the menus. Four widths, both themes,
// no sideways scroll; the picture is fetched with no referrer.
import { expect, test, type Page } from '@playwright/test';
import * as contests from './mocks/contests';
import * as screens from './mocks/screens';
import { codeEditor } from './locators';

// A one-pixel image, served in place of Google's picture so no test reaches the network.
const PICTURE = '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1" fill="#888"/></svg>';

async function withPicture(page: Page, ok: boolean) {
  await page.route('https://lh3.googleusercontent.com/**', (route) =>
    ok ? route.fulfill({ status: 200, contentType: 'image/svg+xml', body: PICTURE }) : route.fulfill({ status: 404 }),
  );
  await page.route('**/api/me', (route) => route.fulfill({ json: { ...screens.STUDENT, theme: '', verdicts: 'standard', avatar_url: 'https://lh3.googleusercontent.com/a/asha' } }));
}

test('the account menu leads to Your profile, which shows who you are and your dates', async ({ page }) => {
  await screens.mockApi(page, 'student');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByRole('button', { name: /Account|Asha/ }).first().click();
  await page.getByRole('link', { name: 'Your profile' }).click();
  await expect(page).toHaveURL(/\/me$/);
  await expect(page).toHaveTitle('Your profile · podium');
  await expect(page.getByRole('heading', { level: 2, name: 'Asha Rao' })).toBeVisible();
  const facts = page.getByRole('region', { name: 'Asha Rao' });
  await expect(facts).toContainText('asha@college.edu');
  await expect(facts).toContainText('Student');
  await expect(facts.getByText('Member since')).toBeVisible();
  await expect(facts.getByText('Last sign-in')).toBeVisible();
  // No picture: the initials stand in.
  await expect(page.locator('.avatar')).toHaveText('AR');
});

test('Your contests lists each contest entered, with score and place, and says when a place is not out', async ({ page }) => {
  await screens.mockApi(page, 'student');
  await page.goto('/me');
  const list = page.getByRole('region', { name: 'Your contests' });
  const rows = list.getByRole('listitem');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('Week 6 contest');
  await expect(rows.nth(0)).toContainText('3rd of 41');
  await expect(rows.nth(0).getByRole('link', { name: 'Week 6 contest' })).toHaveAttribute('href', '/contests/week-6');
  await expect(rows.nth(1)).toContainText('Place not shown yet');
});

test('with no contests entered, the list says what will show there', async ({ page }) => {
  await screens.mockApi(page, 'student');
  await page.route('**/api/me/contests', (route) => route.fulfill({ json: { contests: [], server_now: Date.now() } }));
  await page.goto('/me');
  await expect(page.getByRole('region', { name: 'Your contests' })).toContainText('No contests yet.');
});

test('the picture loads with no referrer, and a broken one leaves the initials', async ({ page }) => {
  await screens.mockApi(page, 'student');
  await withPicture(page, true);
  await page.goto('/me');
  const img = page.locator('.avatar img');
  await expect(img).toHaveAttribute('referrerpolicy', 'no-referrer');
  await expect(img).toBeVisible();
  await expect(page.locator('.avatar .initials')).toHaveCount(0);

  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await screens.mockApi(page, 'student');
  await withPicture(page, false);
  await page.reload();
  await expect(page.locator('.avatar')).toHaveText('AR');
  await expect(page.locator('.avatar img')).toHaveCount(0);
});

test('an editor preference set on the profile is kept for the editor', async ({ page }) => {
  await screens.mockApi(page, 'student');
  await page.goto('/me');
  const editor = page.getByRole('region', { name: 'Code editor' });
  await editor.getByLabel('Font size').selectOption('18');
  await editor.getByLabel('Word wrap').check();
  await page.reload();
  await expect(editor.getByLabel('Font size')).toHaveValue('18');
  await expect(editor.getByLabel('Word wrap')).toBeChecked();
  // The theme controls are the account menu's own.
  await expect(page.getByRole('region', { name: 'Theme and colours' }).getByRole('radio').first()).toBeVisible();
});

for (const theme of ['light', 'dark'] as const) {
  for (const width of [375, 768, 1024, 1440]) {
    test(`/me at ${width} px, ${theme}: no sideways scroll`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await screens.mockApi(page, 'student');
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/me');
      await expect(page.getByText('Last sign-in')).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
      await page.screenshot({ path: `${test.info().project.testDir}/../../cache/v4/shots/M1/me-${width}-${theme}.png`, fullPage: true });
    });
  }
}

test('a font size set on the profile is the one the workspace editor opens with', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/me');
  await page.getByRole('region', { name: 'Code editor' }).getByLabel('Font size').selectOption('18');
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await page.getByRole('button', { name: 'Settings' }).click();
  await expect(page.locator('#ws-font')).toHaveValue('18');
});
