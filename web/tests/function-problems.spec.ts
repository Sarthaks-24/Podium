// Function problems (v2.0.2 stage B): in the problem editor an organiser
// chooses "One function, between locked code" and writes a locked top and
// bottom per language beside the stub; the save sends the mode and the
// locked code. The fold is keyboard reachable and passes axe at phone and
// desktop widths, light and dark.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { tabTo } from './keyboard';
import * as problems from './mocks/problems';

const TOP = 'import java.util.*;\n';
const BOTTOM = 'public class Main {\n    public static void main(String[] args) {\n        Scanner in = new Scanner(System.in);\n    }\n}\n';
const STUB = 'class Solution {\n    long arraySum(int[] a) {\n        return 0;\n    }\n}\n';

test('an organiser makes a function problem in Java and it is saved with its locked code', async ({ page }) => {
  const api = await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const fold = page.locator('summary', { hasText: 'What students write' });
  await expect(fold).toHaveText('What students write, the whole program');
  await fold.click();
  await page.getByLabel('One function, between locked code').check();
  const group = page.getByRole('group', { name: 'What students write' });
  await group.getByLabel('Language').selectOption('java21');
  await group.getByLabel('Locked top in Java 21').fill(TOP);
  await group.getByLabel('Locked bottom in Java 21').fill(BOTTOM);
  await expect(fold).toHaveText('What students write, a function in Java 21');
  await page.locator('summary', { hasText: 'Starter code' }).click();
  const starters = page.getByRole('group', { name: 'Starter code' });
  await starters.getByLabel('Language').selectOption('java21');
  await starters.getByLabel('Starter in Java 21').fill(STUB);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => api.puts.length).toBe(1);
  expect(api.puts[0].mode).toBe('function');
  expect(api.puts[0].locked_top).toEqual({ java21: TOP });
  expect(api.puts[0].locked_bottom).toEqual({ java21: BOTTOM });
  expect(api.puts[0].starters).toEqual({ java21: STUB });
});

test('the mode is chosen with the keyboard', async ({ page }) => {
  await problems.mockApi(page);
  await page.setViewportSize({ width: 1024, height: 800 });
  await page.goto('/admin/problems/1');
  const fold = page.locator('summary', { hasText: 'What students write' });
  await fold.focus();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('The whole program')).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByLabel('One function, between locked code')).toBeChecked();
  await expect(page.getByLabel('Locked top in C++17')).toBeVisible();
});

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.use({ colorScheme: theme });
    for (const width of [375, 768, 1024, 1440]) {
      test(`the open fold passes axe and fits at ${width} px`, async ({ page }) => {
        await problems.mockApi(page);
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/admin/problems/1');
        // Under 768 px the statement has Write and Preview tabs; the fold is on Write.
        await page.locator('summary', { hasText: 'What students write' }).click();
        await page.getByLabel('One function, between locked code').check();
        await expect(page.getByLabel('Locked bottom in C++17')).toBeVisible();
        const results = await new AxeBuilder({ page }).include('.fold').withTags(AXE_TAGS).analyze();
        expect(results.violations.map((v) => v.id)).toEqual([]);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow).toBeLessThanOrEqual(0);
      });
    }
  });
}

test('Add standard imports fills the top and the preview joins it with the starter and the bottom', async ({ page }) => {
  await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await page.locator('summary', { hasText: 'What students write' }).click();
  await page.getByLabel('One function, between locked code').check();
  await page.getByRole('button', { name: 'Add standard imports' }).click();
  await expect(page.getByLabel('Locked top in C++17')).toHaveValue('#include <bits/stdc++.h>\nusing namespace std;\n');
  await page.getByLabel('Locked bottom in C++17').fill('int main() {}\n');
  const preview = page.getByLabel(/^What students see in C\+\+17/);
  await expect(preview).toContainText('using namespace std;');
  await expect(preview).toContainText('int main() {}');
});

async function openFunctionFold(page: Page) {
  await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await page.locator('summary', { hasText: 'What students write' }).click();
  await page.getByLabel('One function, between locked code').check();
}

test('Add standard imports keeps the lines already there and adds only what is missing', async ({ page }) => {
  await openFunctionFold(page);
  const top = page.getByLabel('Locked top in C++17');
  const add = page.getByRole('button', { name: 'Add standard imports' });
  await top.fill('#include <bits/stdc++.h>\n#include <cassert>\n');
  await add.click();
  await expect(top).toHaveValue('#include <bits/stdc++.h>\nusing namespace std;\n#include <cassert>\n');
  await add.click();
  await expect(top).toHaveValue('#include <bits/stdc++.h>\nusing namespace std;\n#include <cassert>\n');
  await top.fill('using namespace std;\n');
  await add.click();
  await expect(top).toHaveValue('#include <bits/stdc++.h>\nusing namespace std;\n');
});

test('an error about one language switches to it and names it', async ({ page }) => {
  await openFunctionFold(page);
  await page.route('**/api/admin/problems/1', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    const message = 'Add a locked top for java21. Common imports go there.';
    return problems.apiError(route, 400, 'bad_request', 'Some fields need fixing.', { fields: { locked_top: message } });
  });
  await page.getByLabel('Locked top in C++17').fill('#include <bits/stdc++.h>\n');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#locked-language')).toHaveValue('java21');
  await expect(page.getByLabel('Locked top in Java 21')).toBeVisible();
  await expect(page.locator('#locked-top-error')).toHaveText('Add a locked top for Java 21. Common imports go there.');
});

test('the preview of what students see is a labelled region the keyboard can reach', async ({ page }) => {
  await openFunctionFold(page);
  const preview = page.getByRole('region', { name: /^What students see in C\+\+17/ });
  await tabTo(page, preview);
  await expect(preview).toBeFocused();
});

for (const theme of ['light', 'dark'] as const) {
  for (const width of [375, 768, 1024, 1440]) {
    test(`a language error and the focusable preview hold at ${width} px, ${theme}, axe clean`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await problems.mockApi(page);
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/admin/problems/1');
      await page.locator('summary', { hasText: 'What students write' }).click();
      await page.getByLabel('One function, between locked code').check();
      await page.route('**/api/admin/problems/1', async (route) => {
        if (route.request().method() !== 'PUT') return route.fallback();
        return problems.apiError(route, 400, 'bad_request', 'Some fields need fixing.', { fields: { locked_top: 'Add a locked top for java21. Common imports go there.' } });
      });
      await page.getByLabel('Locked top in C++17').fill('#include <bits/stdc++.h>\n');
      await page.getByRole('button', { name: 'Save changes' }).click();
      await expect(page.locator('#locked-top-error')).toBeVisible();
      const results = await new AxeBuilder({ page }).include('.fold').withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => v.id)).toEqual([]);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
      const dir = process.env.PODIUM_SHOTS ?? problems.shots();
      mkdirSync(dir, { recursive: true });
      await page.locator('details.fold', { hasText: 'What students write' }).screenshot({ path: `${dir}locked-code-error-${width}-${theme}.png` });
    });
  }
}
