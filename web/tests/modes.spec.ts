// The sign-in page in each run mode (decision D48): testing mode shows a
// form and a banner warning that sign-in is off; production shows only the
// Google link. API calls are mocked.
import { expect, test, type Page, type Route } from '@playwright/test';

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

async function mock(page: Page, mode: string) {
  const state = { signedIn: false, posted: [] as unknown[] };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (path === '/api/auth/config') return json(route, 200, { allowed_domains: ['college.edu'], contact_email: 'owner@podium.test', mode });
      if (path === '/api/auth/test-signin') {
        const body = req.postDataJSON() as { email: string; role: string };
        state.posted.push(body);
        if (!body.email.includes('@')) {
          return json(route, 400, { error: { code: 'bad_request', message: 'Enter an email address, for example asha@college.edu.' } });
        }
        state.signedIn = true;
        return json(route, 200, { next: '/' });
      }
      if (path === '/api/me') {
        return state.signedIn
          ? json(route, 200, { id: 3, email: 'ta@college.edu', name: 'TA', avatar_url: '', role: 'organiser', theme: '', verdicts: 'standard' })
          : json(route, 401, { error: { code: 'signed_out', message: 'Sign in to continue.' } });
      }
      if (path === '/api/admin/problems') return json(route, 200, { problems: [] });
      return json(route, 404, { error: { code: 'not_found', message: 'No mock.' } });
    },
  );
  return state;
}

test('testing mode signs in from a form', async ({ page }) => {
  const state = await mock(page, 'testing');
  await page.goto('/signin');
  await expect(page.getByRole('note')).toContainText('Testing mode');
  await expect(page.getByRole('link', { name: 'Continue with Google' })).toHaveCount(0);

  await page.getByLabel('Email').fill('nope');
  await page.getByRole('button', { name: 'Sign in for testing' }).click();
  await expect(page.getByRole('alert')).toContainText('Enter an email address');

  await page.getByLabel('Email').fill('ta@college.edu');
  await page.getByLabel('Role').selectOption('organiser');
  await page.getByRole('button', { name: 'Sign in for testing' }).click();
  await expect(page).toHaveURL(/\/admin\/contests$/);
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toBeVisible();
  expect(state.posted.at(-1)).toMatchObject({ email: 'ta@college.edu', role: 'organiser' });
  await expect(page.getByRole('note')).toContainText('Testing mode');
});

test('production mode shows only Google sign-in', async ({ page }) => {
  await mock(page, 'production');
  await page.goto('/signin');
  await expect(page.getByRole('link', { name: 'Continue with Google' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign in for testing' })).toHaveCount(0);
  await expect(page.getByRole('note')).toHaveCount(0);
});
