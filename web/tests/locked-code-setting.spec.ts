// The contest setting "Locked code" (v2.1.1 stage P): the contest editor's
// Locked code group (Shown, Folded, Hidden) saves on its own and carries the
// help text; a Folded workspace opens with both locked blocks folded and
// unfolds by keyboard; a Hidden workspace holds only the student's code with
// one read-only note above and one below, and sends only that code on Submit.
// Each screen at four widths, light and dark, with axe and no sideways scroll.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { codeEditor } from './locators';
import { mockApi as mockContest } from './mocks/contests';
import { THEMES, WIDTHS, editorText, mockApi, openWorkspace, type Json } from './mocks/workspace';

const TOP = '#include <bits/stdc++.h>\nusing namespace std;\n';
const BOTTOM = 'int main() {\n    long long a, b;\n    cin >> a >> b;\n    cout << add(a, b) << endl;\n}\n';
const STUB = 'long long add(long long a, long long b) {\n}\n';

const BEFORE = 'Locked code runs before your function.';
const AFTER = 'Locked code runs after your function.';
const HELP =
  "For function problems. Folded: students can open it. Hidden: students see only their function; staff always see everything. Hidden is not a secret: a student's program can still read the file it runs in.";

/** The workspace answer of a C++ function problem under one setting, as the server sends it. */
function fn(mode: 'shown' | 'folded' | 'hidden'): Json {
  const hidden = mode === 'hidden';
  return {
    mode: 'function',
    locked_code: mode,
    languages: [{ id: 'cpp17', name: 'C++17', time_factor: 1 }],
    locked_top: hidden ? {} : { cpp17: TOP },
    locked_bottom: hidden ? {} : { cpp17: BOTTOM },
    starters: { cpp17: STUB },
  };
}

async function open(page: Page, mode: 'shown' | 'folded' | 'hidden', width = 1440) {
  const api = await mockApi(page, { workspace: fn(mode) });
  await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
  await openWorkspace(page);
  if (width < 1024) await page.getByRole('tab', { name: 'Code' }).click();
  return api;
}

const foldButtons = (page: Page) => page.getByRole('button', { name: /^Locked code, \d+ lines?, show$/ });

test('the contest editor offers Shown, Folded and Hidden, with the help text, and saves each choice alone', async ({ page }) => {
  const api = await mockContest(page, { who: 'organiser', phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  await page.locator('#advanced summary').click();
  const group = page.getByRole('group', { name: 'Locked code' });
  await expect(group.getByRole('radio')).toHaveCount(3);
  await expect(group.getByRole('radio', { name: 'Shown' })).toBeChecked();
  await expect(group).toContainText(HELP);
  await expect(group).toHaveAccessibleDescription(HELP);

  await group.getByRole('radio', { name: 'Hidden' }).check();
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ locked_code: 'hidden' });
  await expect(group.getByRole('radio', { name: 'Hidden' })).toBeChecked();
  // The arrow keys move through the group, and each stop saves.
  await group.getByRole('radio', { name: 'Hidden' }).focus();
  await page.keyboard.press('ArrowUp');
  await expect(group.getByRole('radio', { name: 'Folded' })).toBeChecked();
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ locked_code: 'folded' });
  await page.reload();
  await page.locator('#advanced summary').click();
  await expect(page.getByRole('group', { name: 'Locked code' }).getByRole('radio', { name: 'Folded' })).toBeChecked();
});

test('a shown workspace is as before: no note, no fold', async ({ page }) => {
  await open(page, 'shown');
  await expect(page.locator('.cm-lockedNote')).toHaveCount(0);
  await expect(foldButtons(page)).toHaveCount(0);
  await expect(page.locator('.cm-lockedLine').first()).toBeVisible();
  expect(await editorText(page)).toBe(`${TOP}${STUB.trimEnd()}\n${BOTTOM.trimEnd()}`);
});

test('a folded workspace opens with both blocks folded and unfolds by keyboard', async ({ page }) => {
  await open(page, 'folded');
  await expect(foldButtons(page)).toHaveCount(2);
  await expect(foldButtons(page).first()).toHaveText('Locked code, 2 lines');
  expect(await editorText(page)).not.toContain('#include');
  await foldButtons(page).first().focus();
  await page.keyboard.press('Enter');
  await expect(foldButtons(page)).toHaveCount(1);
  expect(await editorText(page)).toContain('#include <bits/stdc++.h>');
  await foldButtons(page).first().focus();
  await page.keyboard.press('Enter');
  await expect(foldButtons(page)).toHaveCount(0);
});

test('a hidden workspace shows only the function, with a note above and below, and submits only that code', async ({ page }) => {
  const api = await open(page, 'hidden');
  await expect(page.locator('.cm-lockedNote')).toHaveText([BEFORE, AFTER]);
  await expect(page.locator('.cm-lockedLine')).toHaveCount(0);
  await expect(foldButtons(page)).toHaveCount(0);
  expect(await editorText(page)).toBe(STUB.trimEnd() + '\n');
  expect(await editorText(page)).not.toContain('#include');
  // The notes are text of the page, not of the file: typing and Submit never include them.
  await codeEditor(page).focus();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// kb');
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => api.submits.length).toBe(1);
  expect(String(api.submits[0].source)).toContain('// kb');
  expect(String(api.submits[0].source)).not.toContain('Locked code runs');
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const width of WIDTHS) {
      test(`the contest editor group passes axe and fits at ${width} px`, async ({ page }) => {
        await mockContest(page, { who: 'organiser', phase: 'during' });
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.goto('/admin/contests/4');
        await page.locator('#advanced summary').click();
        const group = page.getByRole('group', { name: 'Locked code' });
        await group.scrollIntoViewIfNeeded();
        await expect(group).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
        const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
        expect(results.violations.map((v) => `${v.id}: ${v.nodes[0]?.target}`)).toEqual([]);
      });

      for (const mode of ['folded', 'hidden'] as const) {
        test(`the ${mode} workspace passes axe and fits at ${width} px`, async ({ page }) => {
          await open(page, mode, width);
          if (mode === 'hidden') await expect(page.locator('.cm-lockedNote').first()).toBeVisible();
          else await expect(foldButtons(page).first()).toBeVisible();
          await page.evaluate(() => document.fonts.ready);
          expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
          const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
          // The CodeMirror scroller holds the focusable editor, which axe does not
          // count; the same known false positive as in locked-code.spec.ts.
          const violations = results.violations
            .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
            .filter((v) => !v.startsWith('scrollable-region-focusable'));
          expect(violations).toEqual([]);
        });
      }
    }
  });
}
