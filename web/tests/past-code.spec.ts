// Past code (v4 E2): an open submission row shows the code that was sent,
// compares it with the editor's code now, and puts it back in the editor
// after the same kind of confirm as Reset.
import { expect, test } from '@playwright/test';
import * as contests from './mocks/contests';
import { codeEditor } from './locators';

const first = 'int main() {\n    return 0;\n}\n';

test('a past submission shows its code, compares with mine, and Use this code puts it back and saves', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'during', starters: { cpp17: first } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('.cm-content')).toContainText('return 0;');

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);

  await codeEditor(page).click();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type('int main() {}');
  await expect(page.locator('.cm-content')).not.toContainText('return 0;');

  const result = page.getByRole('region', { name: 'Result' });
  await page.getByRole('tab', { name: /^Submissions/ }).click();
  await result.locator('button.row').first().click();

  await result.getByRole('button', { name: 'Code', exact: true }).click();
  await expect(result.getByRole('region', { name: /^Code submitted .*, 3 lines$/ })).toContainText('return 0;');

  await result.getByRole('button', { name: 'Compare with mine' }).click();
  const compare = result.getByRole('region', { name: 'This submission compared with your code' });
  await expect(compare).toContainText('- int main() {');
  await expect(compare).toContainText('+ int main() {}');

  await result.getByRole('button', { name: 'Use this code' }).click();
  const dialog = page.getByRole('alertdialog').or(page.getByRole('dialog'));
  await expect(dialog).toContainText("Replace your code with this submission's?");
  await dialog.getByRole('button', { name: 'Use this code' }).click();
  await expect(page.locator('.cm-content')).toContainText('return 0;');
  await expect.poll(() => api.draftPuts.at(-1)?.source).toBe(first);
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
});
