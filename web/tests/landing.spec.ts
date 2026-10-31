// The landing page (v4 P1): a signed-out visitor at / reads what Podium is
// for and how a contest works, and signs in from it; nothing moves and
// nothing scrolls sideways at four widths in either theme. Signed in, /
// stays the student's contests.
import { expect, test } from '@playwright/test';
import * as screens from './mocks/screens';

test('signed out, / is the landing page with the way in; signed in, it is Your contests', async ({ page }) => {
  await screens.mockApi(page, 'signed-out');
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Programming contests at this college');
  await expect(page.getByRole('heading', { level: 2 })).toHaveText(['For students', 'For organisers', 'How a contest works']);
  const signIn = page.getByRole('link', { name: 'Sign in with your college Google account' });
  await signIn.click();
  await expect(page).toHaveURL(/\/signin/);

  await screens.mockApi(page, 'student');
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Your contests');
});

test('a protected page still asks to sign in when signed out', async ({ page }) => {
  await screens.mockApi(page, 'signed-out');
  await page.goto('/me');
  await expect(page).toHaveURL(/\/signin\?next=%2Fme$/);
});

for (const theme of ['light', 'dark'] as const) {
  for (const width of [375, 768, 1024, 1440]) {
    test(`the landing page at ${width} px, ${theme}: no sideways scroll`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await screens.mockApi(page, 'signed-out');
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
      await page.screenshot({ path: `${test.info().project.testDir}/../../cache/v4/shots/P1/landing-${width}-${theme}.png`, fullPage: true });
    });
  }
}
