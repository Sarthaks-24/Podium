// Starter code per problem (v4 E5): an organiser writes one in the problem
// editor and it is saved with the problem; a student with no draft opens the
// problem's own starter, and a language without one gets Podium's.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import * as problems from './mocks/problems';
import { codeEditor } from './locators';

test('an organiser writes a starter for one language and it is saved with the problem', async ({ page }) => {
  const api = await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await page.locator('summary', { hasText: 'Starter code' }).click();
  const group = page.getByRole('group', { name: 'Starter code' });
  await expect(group).toBeVisible();
  await group.getByLabel('Language').selectOption('java21');
  await group.getByLabel('Starter in Java 21').fill('public class Main {\n    // read n, print the answer\n}\n');
  await expect(page.locator('summary', { hasText: 'Starter code' })).toHaveText('Starter code, set for Java 21');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.puts.length).toBe(1);
  expect(api.puts[0].starters).toEqual({ java21: 'public class Main {\n    // read n, print the answer\n}\n' });
});

test("a student with no draft opens the problem's own starter, and Podium's where it has none", async ({ page }) => {
  await contests.mockApi(page, { phase: 'during', starters: { cpp17: '// Read t, then t lines.\nint main() {}\n' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await expect(page.locator('.cm-content')).toContainText('// Read t, then t lines.');
  await page.getByLabel('Language').selectOption('python3');
  await expect(page.locator('.cm-content')).toContainText('input = sys.stdin.readline');
});
