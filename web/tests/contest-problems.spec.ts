// The problem list of the contest editor (v2.1.0 stage V): the save queue,
// the problem bank picker and the letter field. Answered by the mocks of
// mocks/contests.ts.
import { expect, test } from '@playwright/test';
import { mockApi } from './mocks/contests';

test('a quick Add then Move sends the second save only after the first has been answered', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  const puts: { problem_id: number; label: string }[][] = [];
  await page.route('**/api/admin/contests/4/problems', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    puts.push(route.request().postDataJSON());
    if (puts.length === 1) await held;
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();

  // The first answer is held; the Move must still be waiting behind it.
  await page.waitForTimeout(500);
  expect(puts).toHaveLength(1);
  release();
  await expect.poll(() => puts.length).toBe(2);
  expect(puts[0].map((p) => p.label)).toEqual(['A', 'B']);
  expect(puts[1].map((p) => `${p.label}:${p.problem_id}`)).toEqual([`A:${puts[0][1].problem_id}`, `B:${puts[0][0].problem_id}`]);
});
