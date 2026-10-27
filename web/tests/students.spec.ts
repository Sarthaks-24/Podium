// The organisers' Students table (v3): name, contests taken part in and the
// last submission, with a search the address keeps. v3 removed progress
// pages, notices, standing, cases and records (V3-2), and their tests with
// them. Every /api/ call is mocked here or by tests/mocks/organiser.ts.
import { expect, test, type Page } from '@playwright/test';
import * as org from './mocks/organiser';

const ROWS = [
  { id: 7, name: 'Asha Rao', email: 'asha@college.edu', contests: 3, last_submission_at: Date.now() - 3_600_000 },
  { id: 8, name: 'Bilal Khan', email: 'bilal@college.edu', contests: 1, last_submission_at: null },
  { id: 9, name: 'Zoë Fernandes', email: 'zoe@college.edu', contests: 0, last_submission_at: null },
];

async function students(page: Page, rows = ROWS) {
  const asked: string[] = [];
  await page.route(
    (url) => url.pathname === '/api/admin/students',
    (route) => {
      const q = new URL(route.request().url()).searchParams.get('q') ?? '';
      asked.push(q);
      const shown = rows.filter((r) => (r.name + r.email).toLowerCase().includes(q.toLowerCase()));
      return route.fulfill({ json: { students: shown, server_now: Date.now() } });
    },
  );
  return asked;
}

test('lists every student by name, and a search narrows it and stays in the address', async ({ page }) => {
  await org.mockApi(page, {});
  const asked = await students(page);
  await page.goto('/admin/students');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Students');
  const table = page.getByRole('table');
  await expect(table.getByRole('row')).toHaveCount(4);
  await expect(table.getByRole('row', { name: /Asha Rao/ })).toContainText('3');

  await page.getByLabel('Name or email').fill('asha');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(table.getByRole('row')).toHaveCount(2);
  await expect(page).toHaveURL('/admin/students?q=asha');
  expect(asked.at(-1)).toBe('asha');

  await page.getByLabel('Name or email').fill('nobody');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByText(/No student matches/)).toBeVisible();
});

test('with no students yet, the list says how students arrive (C2-024)', async ({ page }) => {
  await org.mockApi(page, {});
  await students(page, []);
  await page.goto('/admin/students');
  await expect(page.getByText(/No students yet/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'People' }).last()).toHaveAttribute('href', '/admin/people');
});

test('a student cannot see the list', async ({ page }) => {
  await org.mockApi(page, { who: 'student' });
  await students(page);
  await page.goto('/admin/students');
  await expect(page.getByText('Only organisers can see the students.')).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);
});
