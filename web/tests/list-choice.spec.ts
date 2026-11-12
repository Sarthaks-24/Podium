// The list choice on a Java function problem (v2.1.0 stage F13): a starting
// code with two forms, split by `// choice: <name>` lines, lets a student
// start from the array form or the ArrayList form. The choice shows only
// while the editor holds a starting form unchanged, swaps without asking,
// and is read from the code after a reload or on another device. After
// typing, Reset offers both forms. Locked regions stay locked, Run and
// Submit send only the student's code, and a problem without markers keeps
// today's page. Screens go to $PODIUM_SHOTS (default cache/v2.1.0/shots/F/).
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { tabTo } from './keyboard';
import { codeEditor, toasts } from './locators';
import * as problems from './mocks/problems';
import { THEMES, WIDTHS, editorText, mockApi, openWorkspace, submission, type Json } from './mocks/workspace';

const TOP = 'import java.util.*;\n\nclass Missing extends RuntimeException {}\n\nclass Base {\n    long arraySum(int[] a) { return 0; }\n}\n';
const BOTTOM = 'public class Main {\n    public static void main(String[] args) {\n        System.out.println(new Solution().arraySum(new int[0]));\n    }\n}\n';
const form = (type: string) => `class Solution extends Base {\n    @Override\n    long arraySum(${type} a) {\n        return 0;\n    }\n}\n`;
const TWO = `// choice: int[]\n${form('int[]')}// choice: ArrayList<Integer>\n${form('ArrayList<Integer>')}`;
const LONGS = `// choice: long[]\n${form('long[]')}// choice: ArrayList<Long>\n${form('ArrayList<Long>')}`;
const CPP = 'long long add(long long a, long long b) {\n}\n';

/** The workspace answer of a Java function problem; C++ is offered too, with one form. */
function fn(starters: Record<string, string> = { java21: TWO, cpp17: CPP }): Json {
  return {
    mode: 'function',
    languages: [
      { id: 'java21', name: 'Java 21', time_factor: 2 },
      { id: 'cpp17', name: 'C++17', time_factor: 1 },
    ],
    locked_top: { java21: TOP, cpp17: '#include <bits/stdc++.h>\n' },
    locked_bottom: { java21: BOTTOM, cpp17: 'int main() {}\n' },
    starters,
  };
}

const whole = (code: string) => `${TOP}${code.trimEnd()}\n${BOTTOM.trimEnd()}`;
const choice = (page: Page) => page.getByRole('group', { name: 'Lists in your code' });

async function open(page: Page, width = 1440, workspace: Json = fn(), extra: { submissions?: Json[]; draft?: { language: string; source: string } } = {}) {
  const api = await mockApi(page, { workspace, ...extra });
  await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
  await openWorkspace(page);
  if (width >= 1024 && !extra.draft) await page.getByLabel('Language').selectOption('java21');
  return api;
}

async function typeInCode(page: Page, text: string) {
  await codeEditor(page).click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(text);
}

test('no markers means no choice and the same Reset confirm', async ({ page }) => {
  await open(page, 1440, fn({ java21: form('int[]'), cpp17: CPP }));
  await expect(page.getByLabel('Language')).toHaveValue('java21');
  expect(await editorText(page)).toBe(whole(form('int[]')));
  await expect(page.locator('fieldset', { hasText: 'Lists in your code' })).toHaveCount(0);
  await expect(page.getByText('Lists in your code')).toHaveCount(0);
  await typeInCode(page, '\n// mine');
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  const ask = page.getByRole('dialog', { name: 'Replace your code with the starter?' });
  await expect(ask).toContainText('Your Java 21 code for this problem is replaced with the starter, and saved.');
  await expect(ask.getByRole('button', { name: 'Reset', exact: true })).toBeVisible();
  await expect(ask.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expect(ask.getByRole('button', { name: /^Start again/ })).toHaveCount(0);
  await expect(ask.getByRole('button', { name: 'Keep my code' })).toHaveCount(0);
});

test('another language of a two-form problem has no choice and the same Reset confirm', async ({ page }) => {
  await open(page);
  await page.getByLabel('Language').selectOption('cpp17');
  await expect(choice(page)).toHaveCount(0);
  await typeInCode(page, '\n// mine');
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  const ask = page.getByRole('dialog', { name: 'Replace your code with the starter?' });
  await expect(ask.getByRole('button', { name: 'Reset', exact: true })).toBeVisible();
  await expect(ask.getByRole('button', { name: /^Start again/ })).toHaveCount(0);
});

test('the choice shows above the editor with the array form first and selected', async ({ page }) => {
  await open(page);
  await expect(choice(page)).toBeVisible();
  const radios = choice(page).getByRole('radio');
  await expect(radios).toHaveCount(2);
  await expect(radios.nth(0)).toHaveAccessibleName('int[]');
  await expect(radios.nth(1)).toHaveAccessibleName('ArrayList<Integer>');
  await expect(radios.nth(0)).toBeChecked();
  expect(await editorText(page)).toBe(whole(form('int[]')));
  expect(await editorText(page)).not.toContain('choice:');
});

test('picking the other form swaps the starting code at once, without asking, and says so', async ({ page }) => {
  const api = await open(page);
  await choice(page).getByLabel('ArrayList<Integer>').check();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await editorText(page)).toBe(whole(form('ArrayList<Integer>')));
  await expect(choice(page).getByLabel('ArrayList<Integer>')).toBeChecked();
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Starting code changed to ArrayList<Integer>.');
  await expect.poll(() => api.draftPuts.at(-1)?.source).toBe(form('ArrayList<Integer>').trimEnd());
  await choice(page).getByLabel('int[]').check();
  expect(await editorText(page)).toBe(whole(form('int[]')));
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Starting code changed to int[].');
});

test('after typing the choice is gone and Reset offers both forms', async ({ page }) => {
  await open(page);
  await typeInCode(page, '\n// mine');
  await expect(choice(page)).toHaveCount(0);
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  const ask = page.getByRole('dialog', { name: 'Start again?' });
  await expect(ask).toContainText(
    'Your Java code for this problem is replaced with the starting code and saved. Code you have not submitted cannot be brought back. Your submissions keep theirs.',
  );
  await expect(ask.getByRole('button', { name: 'Start again with int[]' })).toBeVisible();
  await expect(ask.getByRole('button', { name: 'Start again with ArrayList<Integer>' })).toBeVisible();
  await expect(ask.getByRole('button', { name: 'Keep my code' })).toBeFocused();
  await expect(ask.getByRole('button', { name: 'Cancel' })).toHaveCount(0);
  await ask.getByRole('button', { name: 'Start again with ArrayList<Integer>' }).click();
  await expect.poll(() => editorText(page)).toBe(whole(form('ArrayList<Integer>')));
  await expect(choice(page).getByLabel('ArrayList<Integer>')).toBeChecked();
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Reset. The starter is back.');
});

test('Start again with int[] puts the array form back', async ({ page }) => {
  const api = await open(page);
  await choice(page).getByLabel('ArrayList<Integer>').check();
  await typeInCode(page, '\n// mine');
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  await page.getByRole('button', { name: 'Start again with int[]' }).click();
  await expect.poll(() => editorText(page)).toBe(whole(form('int[]')));
  await expect(choice(page).getByLabel('int[]')).toBeChecked();
  await expect.poll(() => api.draftPuts.at(-1)?.source).toBe(form('int[]').trimEnd());
});

test('Keep my code and Escape keep the code', async ({ page }) => {
  await open(page);
  await typeInCode(page, '\n// mine');
  const reset = page.getByRole('button', { name: 'Reset', exact: true });
  await reset.click();
  await page.getByRole('button', { name: 'Keep my code' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(reset).toBeFocused();
  expect(await editorText(page)).toContain('// mine');
  await reset.click();
  await expect(page.getByRole('dialog', { name: 'Start again?' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await editorText(page)).toContain('// mine');
  await expect(choice(page)).toHaveCount(0);
});

test('a reload and a second browser context show the choice from the saved code', async ({ page, browser }) => {
  const api = await open(page);
  await choice(page).getByLabel('ArrayList<Integer>').check();
  await expect.poll(() => api.draftPuts.at(-1)?.source).toBe(form('ArrayList<Integer>').trimEnd());
  await page.reload();
  await expect(codeEditor(page)).toHaveAttribute('data-language', /java/);
  await expect(choice(page).getByLabel('ArrayList<Integer>')).toBeChecked();
  expect(await editorText(page)).toBe(whole(form('ArrayList<Integer>')));

  // Another device: no local copy, only what the server holds.
  const other = await browser.newContext({ viewport: { width: 1440, height: 900 }, timezoneId: 'Asia/Kolkata', locale: 'en-GB' });
  try {
    const second = await other.newPage();
    await mockApi(second, { workspace: fn(), draft: { language: 'java21', source: String(api.draftPuts.at(-1)?.source) } });
    await openWorkspace(second);
    await expect(choice(second).getByLabel('ArrayList<Integer>')).toBeChecked();
    expect(await editorText(second)).toBe(whole(form('ArrayList<Integer>')));
  } finally {
    await other.close();
  }
});

test('a reload after typing shows no choice', async ({ page }) => {
  const api = await open(page);
  await typeInCode(page, '\n// mine');
  await page.keyboard.press('Control+s');
  await expect.poll(() => String(api.draftPuts.at(-1)?.source)).toContain('// mine');
  await page.reload();
  await expect(codeEditor(page)).toHaveAttribute('data-language', /java/);
  expect(await editorText(page)).toContain('// mine');
  await expect(choice(page)).toHaveCount(0);
});

test('a saved draft of a plain Java starter shows the first form selected', async ({ page }) => {
  await open(page, 1440, fn(), { draft: { language: 'java21', source: form('int[]') } });
  await expect(choice(page).getByLabel('int[]')).toBeChecked();
});

test('the choice is reached and changed with the keyboard, and the swap is announced', async ({ page }) => {
  await open(page);
  await tabTo(page, choice(page).getByLabel('int[]'));
  await page.keyboard.press('ArrowRight');
  await expect(choice(page).getByLabel('ArrayList<Integer>')).toBeChecked();
  await expect(choice(page).getByLabel('ArrayList<Integer>')).toBeFocused();
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Starting code changed to ArrayList<Integer>.');
  expect(await editorText(page)).toBe(whole(form('ArrayList<Integer>')));
  await page.keyboard.press('ArrowLeft');
  await expect(choice(page).getByLabel('int[]')).toBeChecked();
  expect(await editorText(page)).toBe(whole(form('int[]')));
});

test('the Reset confirm works with the keyboard alone', async ({ page }) => {
  await open(page);
  await typeInCode(page, '\n// mine');
  const reset = page.getByRole('button', { name: 'Reset', exact: true });
  await reset.focus();
  await page.keyboard.press('Enter');
  const ask = page.getByRole('dialog', { name: 'Start again?' });
  await expect(ask.getByRole('button', { name: 'Keep my code' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(ask).toHaveCount(0);
  expect(await editorText(page)).toContain('// mine');
  await reset.focus();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Shift+Tab');
  await expect(ask.getByRole('button', { name: 'Start again with ArrayList<Integer>' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect.poll(() => editorText(page)).toBe(whole(form('ArrayList<Integer>')));
});

test('the locked regions stay locked before and after a swap', async ({ page }) => {
  await open(page);
  for (const type of ['int[]', 'ArrayList<Integer>', 'int[]']) {
    await choice(page).getByLabel(type).check();
    const text = whole(form(type));
    expect(await editorText(page)).toBe(text);
    await expect(page.locator('.cm-lockedLine')).toHaveCount(TOP.split('\n').length - 1 + BOTTOM.split('\n').length - 1);
    // The cursor stops at the edge of the code; deleting into the locked top or bottom is refused.
    await codeEditor(page).click();
    await page.keyboard.press('Control+Home');
    await page.keyboard.press('Backspace');
    await expect(toasts(page)).toContainText('That part of the file is locked');
    expect(await editorText(page)).toBe(text);
    await page.keyboard.press('Control+End');
    await page.keyboard.press('Delete');
    expect(await editorText(page)).toBe(text);
  }
});

test('Run and Submit send only the student code, with no marker text, after a swap', async ({ page }) => {
  const api = await open(page);
  await choice(page).getByLabel('ArrayList<Integer>').check();
  await page.getByRole('button', { name: 'Run' }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  expect(api.runs[0].source).toBe(form('ArrayList<Integer>').trimEnd());
  await page.getByRole('button', { name: /^Submit/ }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0].source).toBe(form('ArrayList<Integer>').trimEnd());
  for (const body of [api.runs[0], api.submits[0]]) {
    expect(String(body.source)).not.toContain('choice:');
    expect(String(body.source)).not.toContain('class Base');
    expect(String(body.source)).not.toContain('public class Main');
  }
});

test('the long pair works the same way', async ({ page }) => {
  const api = await open(page, 1440, fn({ java21: LONGS, cpp17: CPP }));
  const radios = choice(page).getByRole('radio');
  await expect(radios.nth(0)).toHaveAccessibleName('long[]');
  await expect(radios.nth(1)).toHaveAccessibleName('ArrayList<Long>');
  await choice(page).getByLabel('ArrayList<Long>').check();
  expect(await editorText(page)).toBe(whole(form('ArrayList<Long>')));
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Starting code changed to ArrayList<Long>.');
  await typeInCode(page, '\n// mine');
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Start again with long[]' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Start again with ArrayList<Long>' })).toBeVisible();
  await page.getByRole('button', { name: 'Keep my code' }).click();
  await page.getByRole('button', { name: 'Run' }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  expect(String(api.runs[0].source)).not.toContain('choice:');
});

test('switching to another language and back keeps the choice rule', async ({ page }) => {
  await open(page);
  await choice(page).getByLabel('ArrayList<Integer>').check();
  await page.getByLabel('Language').selectOption('cpp17');
  await expect(choice(page)).toHaveCount(0);
  await page.getByLabel('Language').selectOption('java21');
  await expect(choice(page)).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a wrong override gets the hint with the pair of types', async ({ page }) => {
  const output = 'Main.java:6: error: method does not override or implement a method from a supertype\n    @Override\n    ^\n';
  await open(page, 1440, fn(), { submissions: [submission(9, 'ce', { language: 'java21', compile_output: output })] });
  await expect(page.getByText('The function must keep its name and take int[] or ArrayList<Integer>.')).toBeVisible();
  await expect(page.getByText('Your code no longer matches the function the locked code calls')).toHaveCount(0);
});

test('the override hint stays away from a problem without a choice', async ({ page }) => {
  const output = 'Main.java:6: error: method does not override or implement a method from a supertype\n    @Override\n    ^\n';
  await open(page, 1440, fn({ java21: form('int[]'), cpp17: CPP }), { submissions: [submission(9, 'ce', { language: 'java21', compile_output: output })] });
  await expect(page.getByText('Compiler output')).toBeVisible();
  await expect(page.getByText('The function must keep its name')).toHaveCount(0);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const width of WIDTHS) {
      test(`the workspace passes axe and fits at ${width} px`, async ({ page }) => {
        const dir = process.env.PODIUM_SHOTS ?? 'E:/Podium/cache/v2.1.0/shots/F/';
        mkdirSync(dir, { recursive: true });
        await open(page, width, fn(), { draft: width < 1024 ? { language: 'java21', source: form('int[]') } : undefined });
        if (width < 1024) {
          await page.getByRole('tab', { name: 'Code' }).click();
          // Under 1024 px code is read, not written: there is nothing to choose.
          await expect(choice(page)).toHaveCount(0);
        } else {
          await expect(choice(page)).toBeVisible();
        }
        await page.evaluate(() => document.fonts.ready);
        await page.screenshot({ path: `${dir}choice-${width}-${theme}.png`, fullPage: width < 1024 });
        const check = async () => {
          const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
          // The same known false positive as locked-code.spec.ts: the editor's scroller.
          const violations = results.violations.map((v) => `${v.id}: ${v.nodes[0]?.target}`).filter((v) => v !== 'scrollable-region-focusable: .cm-scroller');
          expect(violations).toEqual([]);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow).toBeLessThanOrEqual(0);
        };
        await check();
        if (width >= 1024) {
          await typeInCode(page, '\n// mine');
          await page.getByRole('button', { name: 'Reset', exact: true }).click();
          await expect(page.getByRole('dialog', { name: 'Start again?' })).toBeVisible();
          await page.screenshot({ path: `${dir}reset-${width}-${theme}.png` });
          await check();
        }
      });
    }
  });
}

test('the organiser sees which forms students choose between, both previewed read-only', async ({ page }) => {
  await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await page.locator('summary', { hasText: 'What students write' }).click();
  await page.getByLabel('One function, between locked code').check();
  const group = page.getByRole('group', { name: 'What students write' });
  await group.getByLabel('Language').selectOption('java21');
  await group.getByLabel('Locked top in Java 21').fill(TOP);
  await group.getByLabel('Locked bottom in Java 21').fill(BOTTOM);
  await expect(page.getByText('Students choose between')).toHaveCount(0);
  await page.locator('summary', { hasText: 'Starter code' }).click();
  await page.getByRole('group', { name: 'Starter code' }).getByLabel('Language').selectOption('java21');
  await page.getByLabel('Starter in Java 21').fill(TWO);
  await expect(page.getByText('Students choose between: int[], ArrayList<Integer>.')).toBeVisible();
  const first = page.getByRole('region', { name: /^What students see in Java 21.*int\[\]/ });
  const second = page.getByRole('region', { name: /^What students see in Java 21.*ArrayList<Integer>/ });
  await expect(first).toContainText('long arraySum(int[] a)');
  await expect(first).not.toContainText('choice:');
  await expect(second).toContainText('long arraySum(ArrayList<Integer> a)');
  await expect(second).toContainText('public class Main');
  // Shown as text: no element is made from the code.
  await page.getByLabel('Starter in Java 21').fill(`// choice: int[]\n<img src=x onerror=alert(1)>\n// choice: ArrayList<Integer>\n<b>bold</b>\n`);
  await expect(first).toContainText('<img src=x onerror=alert(1)>');
  await expect(page.locator('.preview img, .preview b')).toHaveCount(0);
  // A starter with one form says nothing.
  await page.getByLabel('Starter in Java 21').fill(form('int[]'));
  await expect(page.getByText('Students choose between')).toHaveCount(0);
});

test('a near-miss choice line shows the server error on the starter field', async ({ page }) => {
  await problems.mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const message = 'This looks like a choice line. Write it exactly as // choice: Name.';
  await page.route('**/api/admin/problems/1', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    return problems.apiError(route, 400, 'bad_request', 'Some fields need fixing.', { fields: { starters: message } });
  });
  await page.locator('summary', { hasText: 'Starter code' }).click();
  const starters = page.getByRole('group', { name: 'Starter code' });
  await starters.getByLabel('Language').selectOption('java21');
  await starters.getByLabel('Starter in Java 21').fill(`//choice: int[]\n${form('int[]')}`);
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#starters-error')).toHaveText(message);
  await expect(starters.getByLabel('Starter in Java 21')).toHaveAttribute('aria-invalid', 'true');
});
