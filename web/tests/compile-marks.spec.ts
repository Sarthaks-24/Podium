// Compiler marks and the fold gutter (v4 E4). A run that ends in a compile
// error underlines the line the compiler names and marks it in the gutter;
// editing that line takes its mark away, and the next run clears the rest.
import { expect, test } from '@playwright/test';
import { codeEditor } from './locators';
import { mockApi, openWorkspace } from './mocks/workspace';

const CE = (id: number, compile_output: string) => ({
  type: 'run',
  data: { id, state: 'done', compile_output, results: [{ label: 'Example 1', verdict: '', time_ms: 0, memory_kb: 0, output: '', expected: '3\n', message: '' }] },
});

test('a C++ compile error marks line 3, F8 goes there, and editing the line or running again clears marks', async ({ page }) => {
  const api = await mockApi(page, { draft: { language: 'cpp17', source: 'int main() {\n  int a = 1;\n  return 0\n  int b = 2;\n}' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);

  // The fold gutter is there for code.
  await expect(page.locator('.cm-foldGutter')).toBeVisible();

  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  api.stream.push(CE(51, "main.cpp:3:11: error: expected ';' before 'int'\nmain.cpp:4:7: warning: unused variable 'b'\n"));
  const errors = page.locator('.cm-lintRange-error');
  await expect(errors).toHaveCount(1, { timeout: 10_000 });
  await expect(errors).toHaveText('return 0');
  await expect(page.locator('.cm-lintRange-warning')).toHaveText('b = 2;');
  await expect(page.locator('.cm-lint-marker-error')).toHaveCount(1);

  await codeEditor(page).click();
  await page.keyboard.press('Control+Home');
  await page.keyboard.press('F8');
  await expect(page.getByText('Ln 3, Col 11', { exact: true })).toBeVisible();

  // Typing on line 3 takes its mark away; line 4's stays.
  await page.keyboard.type(';');
  await expect(errors).toHaveCount(0);
  await expect(page.locator('.cm-lintRange-warning')).toHaveCount(1);

  // The next run clears the rest as soon as it is queued.
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect.poll(() => api.runs.length).toBe(2);
  await expect(page.locator('.cm-lintRange-warning')).toHaveCount(0);
});
