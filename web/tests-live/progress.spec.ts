// Against a live testing-mode server (make dev-test, or podium-server alone
// with PODIUM_MODE=testing; no judge needed): the P4 student routes return
// the shapes the web app reads (contracts C8 and C9, docs/API.md
// "Students"), and the pages built on them load. A fresh student has no
// final contest, so progress is empty but every field is there; notices,
// standing and the unread count answer; the organisers' progress route
// refuses a student and serves an admin. Nothing is mocked.
import { expect, test, type Page } from '@playwright/test';

async function signIn(page: Page, email: string, role: 'student' | 'admin') {
  await page.goto('/signin');
  await expect(page.getByRole('note')).toContainText('Testing mode');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Role').selectOption(role);
  await page.getByRole('button', { name: 'Sign in for testing' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
}

test('the student routes of P4 answer in the agreed shapes', async ({ page, browser }) => {
  const stamp = Date.now();
  await signIn(page, `live-progress-${stamp}@college.edu`, 'student');

  const me = await (await page.request.get('/api/me')).json();
  expect(typeof me.notifications_unread).toBe('number');
  expect(typeof me.hide_progress).toBe('boolean');

  const res = await page.request.get('/api/me/progress');
  expect(res.status()).toBe(200);
  const p = await res.json();
  expect(p.user.id).toBe(me.id);
  expect(p.hide_progress).toBe(false);
  for (const key of ['contests_taken', 'points', 'max_points', 'problems_full', 'problems_partial', 'problems_zero']) {
    expect(typeof p.totals[key], `totals.${key}`).toBe('number');
  }
  for (const key of ['contests', 'by_tag', 'by_difficulty', 'losses', 'retry']) {
    expect(Array.isArray(p[key]), key).toBe(true);
  }
  expect(typeof p.server_now).toBe('number');

  const notices = await (await page.request.get('/api/me/notifications')).json();
  expect(Array.isArray(notices.notifications)).toBe(true);
  expect(typeof notices.unread).toBe('number');
  expect(typeof notices.held).toBe('boolean');
  expect(typeof notices.has_more).toBe('boolean');

  const standing = await (await page.request.get('/api/me/standing')).json();
  expect(standing.band).toBe('good');
  expect(standing.band_name).toBe('Good standing');
  expect(Array.isArray(standing.lines)).toBe(true);
  // The number never reaches a student.
  expect(JSON.stringify(standing)).not.toMatch(/value_tenths|rating/);

  expect((await page.request.get(`/api/admin/users/${me.id}/progress`)).status()).toBe(403);

  // The pages on top of them.
  await page.goto('/progress');
  await expect(page.getByText('Your progress appears here after your first contest ends.')).toBeVisible();
  await page.goto('/notices');
  await expect(page.getByText(/^No notices yet\./)).toBeVisible();
  await page.goto('/integrity');
  await expect(page.getByText('Good standing')).toBeVisible();

  // An admin reads the same shape for that account.
  const context = await browser.newContext();
  const admin = await context.newPage();
  await signIn(admin, `live-progress-admin-${stamp}@college.edu`, 'admin');
  const org = await admin.request.get(`/api/admin/users/${me.id}/progress`);
  expect(org.status()).toBe(200);
  const op = await org.json();
  expect(op.user.id).toBe(me.id);
  expect(op.hide_progress).toBeUndefined();
  await admin.goto(`/admin/users/${me.id}/progress`);
  await expect(admin.getByRole('heading', { level: 1 })).toContainText('progress');
  await context.close();
});
