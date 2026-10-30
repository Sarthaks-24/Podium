// The editor's shortcut table is the contract (cache/v3/design/editor.md
// section 3, lib/shortcuts.ts). Every row of the editor groups is pressed
// here and checked against the document, the cursor ("Ln, Col" under the
// editor) or what appears; a row this file has no check for fails the
// first test, so the table and the behaviour cannot drift apart.
// Windows and Linux keys (Ctrl); the Mac names are covered by Vitest.
import { expect, test, type Page } from '@playwright/test';
import { NOT_AVAILABLE, shortcutGroups } from '../src/lib/shortcuts';
import { codeEditor } from './locators';
import { editorText, mockApi, openWorkspace } from './mocks/workspace';

const EDITOR_GROUPS = new Set(['Editing', 'Selection and cursors', 'Moving around', 'Find and replace']);

/** Replaces the code with `text` by pasting it, then puts the cursor at line and column (from 1). */
async function setCode(page: Page, text: string, line = 1, col = 1) {
  await codeEditor(page).click();
  await page.evaluate((t) => navigator.clipboard.writeText(t), text);
  await page.keyboard.press('Control+A');
  if (text === '') await page.keyboard.press('Delete');
  else await page.keyboard.press('Control+V');
  const lines = text.split('\n');
  if (lines.length < 30) {
    await expect.poll(() => editorText(page)).toBe(text);
  } else {
    // Long code is drawn only where it is visible; check its length by the cursor instead.
    await page.keyboard.press('Control+End');
    await expect.poll(() => cursor(page)).toEqual({ line: lines.length, col: lines[lines.length - 1].length + 1 });
  }
  await page.keyboard.press('Control+Home');
  for (let i = 1; i < line; i++) await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Home');
  // Home on an indented line goes to the first character; go to column 1 first.
  if ((await cursor(page)).col !== 1) await page.keyboard.press('Home');
  for (let i = 1; i < col; i++) await page.keyboard.press('ArrowRight');
  await expect.poll(() => cursor(page)).toEqual({ line, col });
}

async function cursor(page: Page): Promise<{ line: number; col: number }> {
  const text = (await page.getByText(/^Ln \d+, Col \d+$/).textContent()) ?? '';
  const m = /Ln (\d+), Col (\d+)/.exec(text);
  return { line: Number(m?.[1]), col: Number(m?.[2]) };
}

const code = (page: Page) => expect.poll(() => editorText(page));
const at = (page: Page) => expect.poll(() => cursor(page));
const selected = (page: Page) => page.evaluate(() => document.getSelection()?.toString() ?? '');

type Check = (page: Page) => Promise<void>;

/** One check per row, keyed by the row's action text. */
const CHECKS: Record<string, Check> = {
  // Editing
  Undo: async (page) => {
    await setCode(page, 'a', 1, 2);
    await page.keyboard.type('b');
    await code(page).toBe('ab');
    await page.keyboard.press('Control+Z');
    await code(page).toBe('a');
  },
  Redo: async (page) => {
    await setCode(page, 'a', 1, 2);
    await page.keyboard.type('b');
    await page.keyboard.press('Control+Z');
    await page.keyboard.press('Control+Y');
    await code(page).toBe('ab');
    await page.keyboard.press('Control+Z');
    await page.keyboard.press('Control+Shift+Z');
    await code(page).toBe('ab');
  },
  'Cut the selection, or the whole line when nothing is selected': async (page) => {
    await setCode(page, 'one\ntwo');
    await page.keyboard.press('Control+X');
    await code(page).toBe('two');
  },
  'Comment or uncomment the line or selection': async (page) => {
    await setCode(page, 'int a;');
    await page.keyboard.press('Control+/');
    await code(page).toBe('// int a;');
    await page.keyboard.press('Control+/');
    await code(page).toBe('int a;');
  },
  'Block comment': async (page) => {
    await setCode(page, 'int a;');
    await page.keyboard.press('Control+A');
    await page.keyboard.press('Shift+Alt+A');
    await code(page).toBe('/* int a; */');
  },
  'Indent or outdent the line or selection': async (page) => {
    await setCode(page, 'x');
    await page.keyboard.press('Tab');
    await code(page).toBe('    x');
    await page.keyboard.press('Shift+Tab');
    await code(page).toBe('x');
  },
  'Indent or outdent': async (page) => {
    await setCode(page, 'x');
    await page.keyboard.press('Control+]');
    await code(page).toBe('    x');
    await page.keyboard.press('Control+[');
    await code(page).toBe('x');
  },
  'Move the line up or down': async (page) => {
    await setCode(page, 'a\nb', 2);
    await page.keyboard.press('Alt+ArrowUp');
    await code(page).toBe('b\na');
    await page.keyboard.press('Alt+ArrowDown');
    await code(page).toBe('a\nb');
  },
  'Copy the line up or down': async (page) => {
    await setCode(page, 'a');
    await page.keyboard.press('Shift+Alt+ArrowDown');
    await code(page).toBe('a\na');
    await page.keyboard.press('Shift+Alt+ArrowUp');
    await code(page).toBe('a\na\na');
  },
  'Delete the line': async (page) => {
    await setCode(page, 'a\nb');
    await page.keyboard.press('Control+Shift+K');
    await code(page).toBe('b');
  },
  'Insert a line above': async (page) => {
    await setCode(page, '  a', 1, 3);
    await page.keyboard.press('Control+Shift+Enter');
    await code(page).toBe('  \n  a');
    await at(page).toEqual({ line: 1, col: 3 });
  },
  'Delete the word before the cursor': async (page) => {
    await setCode(page, 'foo bar', 1, 8);
    await page.keyboard.press('Control+Backspace');
    await code(page).toBe('foo ');
  },
  'Jump to the matching bracket': async (page) => {
    await setCode(page, '(abc)');
    await page.keyboard.press('Control+Shift+Backslash');
    // CodeMirror lands just past the matching bracket.
    await at(page).toEqual({ line: 1, col: 6 });
  },
  'Open completion': async (page) => {
    await setCode(page, 'apple\nap', 2, 3);
    await page.keyboard.press('Control+Space');
    await expect(page.getByRole('listbox', { name: 'Completions' })).toBeVisible();
  },
  'Accept the completion': async (page) => {
    await setCode(page, 'apple\nap', 2, 3);
    await page.keyboard.press('Control+Space');
    await expect(page.getByRole('listbox', { name: 'Completions' })).toBeVisible();
    // CodeMirror ignores an accept in the first 75 ms, so a stray Enter never picks a word.
    await page.waitForTimeout(150);
    await page.keyboard.press('Enter');
    await code(page).toBe('apple\napple');
  },
  'Fold or unfold the block': async (page) => {
    await setCode(page, 'int main() {\n  int a;\n}', 1, 13);
    await page.keyboard.press('Control+Shift+BracketLeft');
    await expect(page.getByLabel('folded code')).toHaveCount(1);
    await page.keyboard.press('Control+Shift+BracketRight');
    await expect(page.getByLabel('folded code')).toHaveCount(0);
  },
  'Fold all': async (page) => {
    await setCode(page, 'int main() {\n  int a;\n}');
    await page.keyboard.press('Control+K');
    await page.keyboard.press('Control+0');
    await expect(page.getByLabel('folded code')).toHaveCount(1);
  },
  'Unfold all': async (page) => {
    await setCode(page, 'int main() {\n  int a;\n}');
    await page.keyboard.press('Control+K');
    await page.keyboard.press('Control+0');
    await expect(page.getByLabel('folded code')).toHaveCount(1);
    await page.keyboard.press('Control+K');
    await page.keyboard.press('Control+J');
    await expect(page.getByLabel('folded code')).toHaveCount(0);
  },
  'Type the closing bracket or quote too': async (page) => {
    await setCode(page, '');
    await page.keyboard.type('(');
    await code(page).toBe('()');
    await page.keyboard.type('[');
    await code(page).toBe('([])');
  },

  // Selection and cursors
  'Select all': async (page) => {
    await setCode(page, 'a\nb');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('z');
    await code(page).toBe('z');
  },
  'Select the line': async (page) => {
    await setCode(page, 'a\nb');
    await page.keyboard.press('Control+L');
    await page.keyboard.type('z');
    await code(page).toBe('zb');
  },
  'Select the word, then the next match on each press': async (page) => {
    await setCode(page, 'foo foo', 1, 2);
    await page.keyboard.press('Control+D');
    await page.keyboard.press('Control+D');
    await page.keyboard.type('x');
    await code(page).toBe('x x');
  },
  'Select every match of the selection': async (page) => {
    await setCode(page, 'foo foo foo', 1, 2);
    await page.keyboard.press('Control+D');
    await page.keyboard.press('Control+Shift+L');
    await page.keyboard.type('x');
    await code(page).toBe('x x x');
  },
  'Undo the last cursor move': async (page) => {
    await setCode(page, 'foo foo', 1, 2);
    await page.keyboard.press('Control+D');
    await page.keyboard.press('Control+D');
    await page.keyboard.press('Control+U');
    await page.keyboard.type('x');
    await code(page).toBe('x foo');
  },
  'Add a cursor': async (page) => {
    await setCode(page, 'aa\nbb', 1, 3);
    const line2 = page.locator('.cm-line').nth(1);
    const box = await line2.boundingBox();
    await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
    await page.keyboard.down('Alt');
    await page.mouse.down();
    await page.mouse.up();
    await page.keyboard.up('Alt');
    await page.keyboard.type('x');
    await code(page).toBe('aax\nbbx');
  },
  'Add a cursor above or below': async (page) => {
    await setCode(page, 'aa\nbb');
    await page.keyboard.press('Control+Alt+ArrowDown');
    await page.keyboard.type('x');
    await code(page).toBe('xaa\nxbb');
  },
  'Select a box of text': async (page) => {
    await setCode(page, 'abc\ndef');
    // The left edge of each line's first character, and one character's width.
    const box = await page.locator('.cm-line').evaluateAll((els) =>
      els.slice(0, 2).map((el) => {
        const text = document.createTreeWalker(el, NodeFilter.SHOW_TEXT).nextNode()!;
        const r = document.createRange();
        r.setStart(text, 0);
        r.setEnd(text, 1);
        const rect = r.getBoundingClientRect();
        return { x: rect.left, w: rect.width, y: rect.top + rect.height / 2 };
      }),
    );
    await page.keyboard.down('Shift');
    await page.keyboard.down('Alt');
    await page.mouse.move(box[0].x + 1, box[0].y);
    await page.mouse.down();
    await page.mouse.move(box[1].x + 2 * box[1].w + 1, box[1].y, { steps: 5 });
    await page.mouse.up();
    await page.keyboard.up('Alt');
    await page.keyboard.up('Shift');
    await page.keyboard.type('x');
    await code(page).toBe('xc\nxf');
  },
  'Grow the selection to the enclosing block': async (page) => {
    await setCode(page, 'f(a + b);', 1, 3);
    await page.keyboard.press('Shift+Alt+ArrowRight');
    await expect.poll(() => selected(page)).not.toBe('');
  },
  'Shrink the selection back': async (page) => {
    await setCode(page, 'f(a + b);', 1, 3);
    await page.keyboard.press('Shift+Alt+ArrowRight');
    const first = await selected(page);
    await page.keyboard.press('Shift+Alt+ArrowRight');
    await expect.poll(() => selected(page).then((s) => s.length)).toBeGreaterThan(first.length);
    await page.keyboard.press('Shift+Alt+ArrowLeft');
    await expect.poll(() => selected(page)).toBe(first);
  },
  'Select to the start or the end of the file': async (page) => {
    await setCode(page, 'a\nb', 2, 2);
    await page.keyboard.press('Control+Shift+Home');
    await page.keyboard.type('x');
    await code(page).toBe('x');
    await setCode(page, 'a\nb');
    await page.keyboard.press('Control+Shift+End');
    await page.keyboard.type('y');
    await code(page).toBe('y');
  },

  // Moving around
  'Go to a line': async (page) => {
    await setCode(page, 'a\nb\nc');
    await page.keyboard.press('Control+G');
    const field = page.getByRole('textbox', { name: /^Go to line/ });
    await expect(field).toBeFocused();
    await field.fill('3');
    await page.keyboard.press('Enter');
    await at(page).toEqual({ line: 3, col: 1 });
  },
  'Go to the start or the end of the file': async (page) => {
    await setCode(page, 'a\nbb');
    await page.keyboard.press('Control+End');
    await at(page).toEqual({ line: 2, col: 3 });
    await page.keyboard.press('Control+Home');
    await at(page).toEqual({ line: 1, col: 1 });
  },
  'Go to the first character of the line, then column 1': async (page) => {
    await setCode(page, '    x', 1, 6);
    await page.keyboard.press('Home');
    await at(page).toEqual({ line: 1, col: 5 });
    await page.keyboard.press('Home');
    await at(page).toEqual({ line: 1, col: 1 });
  },
  'Go to the next word': async (page) => {
    await setCode(page, 'foo bar');
    await page.keyboard.press('Control+ArrowRight');
    await at(page).toEqual({ line: 1, col: 4 });
  },
  'Scroll a line without moving the cursor': async (page) => {
    await setCode(page, Array.from({ length: 200 }, (_, i) => `line ${i}`).join('\n'), 1);
    const scroller = page.locator('.cm-scroller');
    await page.keyboard.press('Control+ArrowDown');
    await expect.poll(() => scroller.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    await at(page).toEqual({ line: 1, col: 1 });
  },
  'Scroll a page without moving the cursor': async (page) => {
    await setCode(page, Array.from({ length: 200 }, (_, i) => `line ${i}`).join('\n'), 1);
    const scroller = page.locator('.cm-scroller');
    // A page is the scroller's height, so wait for the layout to give it one.
    await expect.poll(() => scroller.evaluate((el) => el.clientHeight)).toBeGreaterThan(150);
    await page.keyboard.press('Alt+PageDown');
    await expect.poll(() => scroller.evaluate((el) => el.scrollTop)).toBeGreaterThan(100);
    await at(page).toEqual({ line: 1, col: 1 });
  },

  // Find and replace
  Find: async (page) => {
    await setCode(page, 'x y x');
    await page.keyboard.press('Control+F');
    await expect(page.getByRole('textbox', { name: 'Find' })).toBeFocused();
  },
  Replace: async (page) => {
    await setCode(page, 'x y x');
    await page.keyboard.press('Control+H');
    await expect(page.getByRole('textbox', { name: 'Replace' })).toBeVisible();
  },
  'Next or previous match': async (page) => {
    await setCode(page, 'x y x');
    await page.keyboard.press('Control+F');
    await page.keyboard.type('x');
    await codeEditor(page).click();
    await page.keyboard.press('Control+Home');
    await page.keyboard.press('F3');
    await at(page).toEqual({ line: 1, col: 2 });
    await page.keyboard.press('F3');
    await at(page).toEqual({ line: 1, col: 6 });
    await page.keyboard.press('Shift+F3');
    await at(page).toEqual({ line: 1, col: 2 });
  },
  'Next or previous match, in the find box': async (page) => {
    await setCode(page, 'x y x');
    await page.keyboard.press('Control+F');
    await page.keyboard.type('x');
    await page.keyboard.press('Enter');
    await page.keyboard.press('Enter');
    await at(page).toEqual({ line: 1, col: 6 });
    await page.keyboard.press('Shift+Enter');
    await at(page).toEqual({ line: 1, col: 2 });
  },
  'Replace all, in the replace box': async (page) => {
    await setCode(page, 'a a');
    await page.keyboard.press('Control+H');
    await page.keyboard.type('a');
    // The panel reads its fields on key presses, so type rather than fill.
    await page.getByRole('textbox', { name: 'Replace' }).focus();
    await page.keyboard.type('b');
    await page.keyboard.press('Control+Alt+Enter');
    await code(page).toBe('b b');
  },
  'Match case, whole word or regular expression, in the find box': async (page) => {
    await setCode(page, 'x');
    await page.keyboard.press('Control+F');
    await page.keyboard.press('Alt+C');
    await expect(page.getByRole('checkbox', { name: 'Match case' })).toBeChecked();
    await page.keyboard.press('Alt+W');
    await expect(page.getByRole('checkbox', { name: 'Whole word' })).toBeChecked();
    await page.keyboard.press('Alt+R');
    await expect(page.getByRole('checkbox', { name: 'Regular expression' })).toBeChecked();
  },
  'Close find': async (page) => {
    await setCode(page, 'x');
    await page.keyboard.press('Control+F');
    await expect(page.getByRole('textbox', { name: 'Find' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('textbox', { name: 'Find' })).toHaveCount(0);
  },
};

const rows = shortcutGroups({ mac: false, organiser: false })
  .filter((g) => EDITOR_GROUPS.has(g.title))
  .flatMap((g) => g.items.map((item) => ({ group: g.title, ...item })));

test.use({ permissions: ['clipboard-read', 'clipboard-write'] });

test('every editor row of the shortcut table has a check here', () => {
  const missing = rows.filter((r) => !CHECKS[r.action]).map((r) => r.action);
  expect(missing).toEqual([]);
  expect(shortcutGroups({ mac: false, organiser: false }).some((g) => g.title === NOT_AVAILABLE)).toBe(true);
});

test.describe('the editor keys', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page, { draft: { language: 'cpp17', source: 'int a;' } });
    await page.setViewportSize({ width: 1440, height: 900 });
    await openWorkspace(page);
  });

  for (const row of rows) {
    test(`${row.group}: ${row.keys.join(row.joiner === 'then' ? ' then ' : ' or ')}, ${row.action}`, async ({ page }) => {
      await CHECKS[row.action](page);
    });
  }
});

test('an editor key pressed outside the editor says where it works (P6)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await page.getByRole('heading', { level: 1 }).click();
  await page.keyboard.press('Control+/');
  await expect(page.getByText('Click in the code editor first. Editor shortcuts work only there.')).toBeVisible();
});

test('Ctrl+Enter in the find panel never submits', async ({ page }) => {
  const api = await mockApi(page, { draft: { language: 'cpp17', source: 'int a;' } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  await codeEditor(page).click();
  await page.keyboard.press('Control+H');
  await page.getByRole('textbox', { name: 'Replace' }).focus();
  await page.keyboard.press('Control+Enter');
  await page.waitForTimeout(300);
  expect(api.submits).toHaveLength(0);
});
