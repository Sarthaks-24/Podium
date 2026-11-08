// Function problems (v2.0.2 stage C): the workspace shows the locked top and
// bottom around the student's code, refuses edits that touch them, cuts a
// pasted whole file back to the code, folds the blocks under 768 px, skips
// them when re-indenting and inserting a line, and sends only the student's
// code on Run, Submit and draft saves. A compile error that points into
// locked code gets the hint. Screens at four widths, light and dark, to
// $PODIUM_SHOTS (default cache/v2.0.2/shots/sC/), checked with axe and with
// the keyboard alone.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { codeEditor, toasts } from './locators';
import { THEMES, WIDTHS, editorText, mockApi, openWorkspace, submission, type Json } from './mocks/workspace';

const TOP = '#include <bits/stdc++.h>\nusing namespace std;\n';
const BOTTOM = 'int main() {\n    long long a, b;\n    cin >> a >> b;\n    cout << add(a, b) << endl;\n}\n';
const STUB = 'long long add(long long a, long long b) {\n}\n';

const LANGS = {
  cpp17: { top: TOP, bottom: BOTTOM, stub: STUB },
  java21: {
    top: 'import java.util.*;\n',
    bottom: 'public class Main {\n    public static void main(String[] args) {\n        System.out.println(new Solution().add(1, 2));\n    }\n}\n',
    stub: 'class Solution {\n    long add(long a, long b) {\n    }\n}\n',
  },
  python3: { top: 'import sys\n', bottom: 'a, b = map(int, sys.stdin.read().split())\nprint(add(a, b))\n', stub: 'def add(a, b):\n    pass\n' },
  node22: { top: "'use strict';\n", bottom: 'const [a, b] = require("fs").readFileSync(0, "utf8").split(" ").map(Number);\nconsole.log(add(a, b));\n', stub: 'function add(a, b) {\n}\n' },
} as const;

/** The workspace answer of a function problem in all four languages. */
function fn(only: keyof typeof LANGS | null = null): Json {
  const ids = (only ? [only] : Object.keys(LANGS)) as (keyof typeof LANGS)[];
  const names = { cpp17: 'C++17', java21: 'Java 21', python3: 'Python 3', node22: 'JavaScript (Node 22)' };
  return {
    mode: 'function',
    languages: ids.map((id) => ({ id, name: names[id], time_factor: 1 })),
    locked_top: Object.fromEntries(ids.map((id) => [id, LANGS[id].top])),
    locked_bottom: Object.fromEntries(ids.map((id) => [id, LANGS[id].bottom])),
    starters: Object.fromEntries(ids.map((id) => [id, LANGS[id].stub])),
  };
}

async function open(page: Page, width = 1440, workspace: Json = fn(), extra: { submissions?: Json[] } = {}) {
  const api = await mockApi(page, { workspace, ...extra });
  await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
  await openWorkspace(page);
  return api;
}

/** Puts the cursor at the end of the student's first line (the stub's opening line). */
async function clickCode(page: Page) {
  await codeEditor(page).click();
  await page.keyboard.press('Control+End');
}

async function paste(page: Page, text: string) {
  await page.locator('.cm-content').evaluate((el, t) => {
    const dt = new DataTransfer();
    dt.setData('text/plain', t);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
  }, text);
}

test('the editor shows the locked top and bottom around the stub', async ({ page }) => {
  await open(page);
  const text = await editorText(page);
  expect(text).toBe(`${TOP}${STUB.trimEnd()}\n${BOTTOM.trimEnd()}`);
  await expect(page.locator('.cm-lockedLine')).toHaveCount(TOP.split('\n').length - 1 + BOTTOM.split('\n').length - 1);
  await expect(page.getByText('Common imports are already added. Add more if you need them.')).toBeVisible();
  // Locked lines have no number; the code counts from 1, as compiler messages do.
  const numbers = await page.locator('.cm-lineNumbers .cm-gutterElement').allTextContents();
  expect(numbers.filter((n) => n !== '')).toEqual(['1', '2']);
});

test('typing at the edges works, and deleting into a locked range is refused with a toast', async ({ page }) => {
  await open(page);
  await clickCode(page);
  // The cursor sits at the end of the stub's last line, right above the locked bottom.
  await page.keyboard.press('Delete');
  await expect(toasts(page)).toContainText('That part of the file is locked');
  expect(await editorText(page)).toContain('int main() {');
  await page.keyboard.press('Control+Home');
  await page.keyboard.press('Backspace');
  expect(await editorText(page)).toBe(`${TOP}${STUB.trimEnd()}\n${BOTTOM.trimEnd()}`);
  await page.keyboard.type('// first');
  await expect(page.locator('.cm-content')).toContainText('// first');
  expect((await editorText(page)).startsWith(`${TOP}// first`)).toBe(true);
});

test('arrow keys and Select all stay inside the code', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.press('Control+a');
  await page.keyboard.press('Backspace');
  // Only the student's code went; both locked blocks stay.
  expect(await editorText(page)).toBe(`${TOP}\n${BOTTOM.trimEnd()}`);
  await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowUp');
  await page.keyboard.type('x');
  expect((await editorText(page)).startsWith(`${TOP}x`)).toBe(true);
});

test('a pasted whole file loses its locked top and bottom, and anything else lands in the code', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.press('Control+a');
  await paste(page, `${TOP}long long add(long long a, long long b) {\n    return a + b;\n}\n${BOTTOM}`);
  expect(await editorText(page)).toBe(`${TOP}long long add(long long a, long long b) {\n    return a + b;\n}\n${BOTTOM.trimEnd()}`);
  await expect(toasts(page)).not.toContainText('locked');
  await page.keyboard.press('Control+a');
  await paste(page, 'return 0;');
  expect(await editorText(page)).toBe(`${TOP}return 0;\n${BOTTOM.trimEnd()}`);
});

test('undo takes back typing in the code and never reaches the locked text', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.type('// note');
  await page.keyboard.press('Control+z');
  await page.keyboard.press('Control+z');
  await page.keyboard.press('Control+z');
  expect(await editorText(page)).toBe(`${TOP}${STUB.trimEnd()}\n${BOTTOM.trimEnd()}`);
  await page.keyboard.press('Control+y');
  expect(await editorText(page)).toContain('// note');
});

test('dragging text onto a locked line is refused', async ({ page }) => {
  await open(page);
  await clickCode(page);
  const refused = await page.locator('.cm-content').evaluate((el) => {
    const first = el.querySelector('.cm-line') as HTMLElement;
    const r = first.getBoundingClientRect();
    const dt = new DataTransfer();
    dt.setData('text', 'evil();');
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, clientX: r.left + 20, clientY: r.top + r.height / 2, bubbles: true, cancelable: true }));
    return Array.from(el.querySelectorAll('.cm-line'), (l) => l.textContent).join('\n').includes('evil');
  });
  // The drop lands at the edge of the code at most, never inside the top.
  const text = await editorText(page);
  expect(text.startsWith(TOP)).toBe(true);
  expect(refused === text.includes('evil();')).toBe(true);
  expect(text.indexOf('evil();') === -1 || text.indexOf('evil();') >= TOP.length).toBe(true);
});

test('re-indent and insert line skip the locked lines', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.press('Control+a');
  await paste(page, 'long long add(long long a, long long b) {\nreturn a + b;\n}');
  await page.keyboard.press('Shift+Alt+f');
  expect(await editorText(page)).toBe(`${TOP}long long add(long long a, long long b) {\n    return a + b;\n}\n${BOTTOM.trimEnd()}`);
  await page.keyboard.press('Control+Home');
  await page.keyboard.press('Control+Shift+Enter');
  const text = await editorText(page);
  expect(text.startsWith(`${TOP}\nlong long add`)).toBe(true);
  expect(text.endsWith(BOTTOM.trimEnd())).toBe(true);
});

test('Run, Submit and draft saves send only the code', async ({ page }) => {
  const api = await open(page);
  await clickCode(page);
  await page.keyboard.type('\n// mine');
  const code = `${STUB.trimEnd()}\n// mine`;
  await page.getByRole('button', { name: 'Run' }).click();
  await expect.poll(() => api.runs.length).toBe(1);
  expect(api.runs[0].source).toBe(code);
  await page.getByRole('button', { name: /^Submit/ }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0].source).toBe(code);
  await page.keyboard.press('Control+s');
  await expect.poll(() => api.draftPuts.length).toBeGreaterThan(0);
  expect(api.draftPuts.at(-1)?.source).toBe(code);
});

test('switching language swaps the locked code and keeps the code of each language', async ({ page }) => {
  await open(page);
  await page.getByLabel('Language').selectOption('python3');
  await expect(page.locator('.cm-content')).toContainText('import sys');
  expect(await editorText(page)).toBe(`${LANGS.python3.top}${LANGS.python3.stub.trimEnd()}\n${LANGS.python3.bottom.trimEnd()}`);
  await page.getByLabel('Language').selectOption('cpp17');
  expect(await editorText(page)).toBe(`${TOP}${STUB.trimEnd()}\n${BOTTOM.trimEnd()}`);
});

test('the locked blocks fold with the keyboard and a click', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.press('Control+k');
  await page.keyboard.press('Control+0');
  const labels = page.getByRole('button', { name: /^Locked code, \d+ lines?, show$/ });
  await expect(labels).toHaveCount(2);
  await expect(labels.first()).toHaveText('Locked code, 2 lines');
  await labels.first().click();
  await expect(labels).toHaveCount(1);
  await codeEditor(page).focus();
  await page.keyboard.press('Control+k');
  await page.keyboard.press('Control+j');
  await expect(labels).toHaveCount(0);
});

test('under 768 px the locked blocks start folded', async ({ page }) => {
  await open(page, 375);
  await page.getByRole('tab', { name: 'Code' }).click();
  await expect(page.getByRole('button', { name: 'Locked code, 2 lines, show' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Locked code, 5 lines, show' })).toBeVisible();
  expect(await editorText(page)).not.toContain('#include');
});

for (const language of Object.keys(LANGS) as (keyof typeof LANGS)[]) {
  test(`a compile error inside locked code shows the hint in ${language}`, async ({ page }) => {
    const shape = {
      cpp17: "locked code, line 4: error: 'add' was not declared in this scope\n",
      java21: 'locked code, line 3: error: cannot find symbol\n',
      python3: 'Sorry: IndentationError: unexpected indent (locked code, line 2)\n',
      node22: 'locked code, line 2\nconsole.log(add(a, b));\n^\n',
    }[language];
    const ce = submission(9, 'ce', { language, compile_output: shape });
    await open(page, 1440, fn(), { submissions: [ce] });
    await expect(page.getByText('Your code no longer matches the function the locked code calls. Keep the name and parameters from the starter.')).toBeVisible();
  });
}

test('a compile error in the student code has no hint', async ({ page }) => {
  await open(page, 1440, fn(), { submissions: [submission(9, 'ce')] });
  await expect(page.getByText('Compiler output')).toBeVisible();
  await expect(page.getByText('Your code no longer matches')).toHaveCount(0);
});

test('a whole-program problem has no locked code and no hint', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await expect(page.locator('.cm-lockedLine')).toHaveCount(0);
  await expect(page.getByText('Common imports are already added')).toHaveCount(0);
});

test('the whole flow works with the keyboard alone', async ({ page }) => {
  const api = await open(page);
  await codeEditor(page).focus();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n// kb');
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0].source).toBe(`${STUB.trimEnd()}\n// kb`);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const width of WIDTHS) {
      test(`the workspace passes axe and fits at ${width} px`, async ({ page }) => {
        const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.0.2/shots/sC/`;
        await open(page, width);
        if (width < 1024) await page.getByRole('tab', { name: 'Code' }).click();
        await expect(page.locator('.cm-lockedLine').first()).toBeVisible();
        await page.evaluate(() => document.fonts.ready);
        await page.screenshot({ path: `${dir}locked-${width}-${theme}.png`, fullPage: width < 1024 });
        const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
        // The CodeMirror scroller holds the focusable editor, which axe does not
        // count; the same known false positive as in a11y.spec.ts (v2.0.1 stage D).
        // It shows only where the fonts make the editor scroll (Linux CI).
        const violations = results.violations
          .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
          .filter((v) => v !== 'scrollable-region-focusable: .cm-scroller');
        expect(violations).toEqual([]);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow).toBeLessThanOrEqual(0);
        if (width >= 1024) {
          await codeEditor(page).click();
          await page.keyboard.press('Control+k');
          await page.keyboard.press('Control+0');
          await page.screenshot({ path: `${dir}locked-folded-${width}-${theme}.png` });
          const folded = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
          expect(folded.violations.map((v) => `${v.id}: ${v.nodes[0]?.target}`)).toEqual([]);
        }
      });
    }
  });
}

test('the fold chords list in the shortcut help, and Enter on a focused fold button unfolds it', async ({ page }) => {
  await open(page);
  await clickCode(page);
  await page.keyboard.press('Control+k');
  await page.keyboard.press('Control+0');
  const labels = page.getByRole('button', { name: /^Locked code, \d+ lines?, show$/ });
  await expect(labels).toHaveCount(2);
  await labels.first().focus();
  await page.keyboard.press('Enter');
  await expect(labels).toHaveCount(1);
  await page.keyboard.press('Escape');
  await page.locator('main h1').focus();
  await page.keyboard.press('Shift+?');
  const help = page.getByRole('dialog');
  await expect(help).toContainText('Fold all');
  await expect(help).toContainText('Unfold all');
});
