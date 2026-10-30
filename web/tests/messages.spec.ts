// Every action says what happened (v4 H5). The audit of every action that
// changes something on the server is in cache/v4/audits/H5-messages.md; each
// test here covers one gap it found, by failing one request on top of the
// usual screen mock (a route added later is matched first).
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';
import * as screens from './mocks/screens';
import { toasts } from './locators';

test('a theme the account could not save says so and how to retry', async ({ page }) => {
  await screens.mockApi(page, 'owner');
  await page.route('**/api/me/preferences', (route) => route.abort());
  await page.goto('/admin/people');
  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Theme and colours' }).click();
  await page.getByRole('radio', { name: 'Dark', exact: true }).check();
  await expect(toasts(page).getByText(/^Changed on this device only\. It was not saved to your account: .+ Pick it again to retry\.$/)).toBeVisible();
});

test('a sign-out the server did not confirm says what to do', async ({ page }) => {
  await screens.mockApi(page, 'owner');
  await page.route('**/api/auth/logout', (route) => route.abort());
  await page.goto('/admin/people');
  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL('/signin');
  await expect(toasts(page).getByText('Signed out on this device, but the server did not confirm it. To end the session everywhere, sign in and sign out again once you are online.')).toBeVisible();
});

test('a validation that could not start for a network fault does not say to open the problem', async ({ page }) => {
  await problems.mockApi(page);
  await page.route(/\/api\/admin\/problems\/\d+\/validate$/, (route) => route.abort());
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Validate Interval covering' }).click();
  const toast = toasts(page).getByText(/^Could not start validating Interval covering\. /);
  await expect(toast).toBeVisible();
  await expect(toast).not.toContainText('Open it to fix that');
});

test('a run refused for pacing says so on screen while Submit counts down', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.locator('button.submit')).toHaveText(/^Submit in \d+ s$/);
  const pace = 'You have run code 20 times in the last minute. Wait a moment and run again.';
  await page.route('**/problems/A/run', (route) =>
    route.fulfill({ status: 429, contentType: 'application/json', body: JSON.stringify({ error: { code: 'rate_limited', message: pace, retry_after_s: 20 } }) }),
  );
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.locator('#ws-submit-status')).toContainText(pace);
});

test('a reveal that worked says so even when the board could not reload', async ({ page }) => {
  const api = await contests.mockApi(page, { who: 'organiser', phase: 'after', holdReveal: true });
  await page.route('**/api/admin/contests/4', (route) => (api.reveals > 0 && route.request().method() === 'GET' ? route.abort() : route.fallback()));
  await page.goto('/admin/contests/4/results');
  await page.getByRole('button', { name: 'Reveal', exact: true }).click();
  await page.getByRole('dialog', { name: 'Reveal the full board?' }).getByRole('button', { name: 'Reveal', exact: true }).click();
  await expect(toasts(page).getByText('Revealed the full board', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert').filter({ hasText: 'Revealed the full board, but this page could not load it. Reload the page to see it.' })).toBeVisible();
});
