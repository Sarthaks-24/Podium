// Screen sweep and flows for the problem screens (/admin/problems and
// /admin/problems/{id}). Every /api/ call is answered by an in-test mock of
// the admin problems API (tests/mocks/problems.ts), one per page, so each
// test starts from the same fixtures: an admin with five problems
// (validated, not validated with a test in no subtask, archived, one whose
// last validation failed, and one with missing outputs) and three contests
// for Add to contest.
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m2/<screen>-<width>-<theme>.png. Further tests cover the
// editor's three tabs from the keyboard, roles chosen per test, saving with
// field errors on another tab, Validate making missing outputs, Add to
// contest, the menu, the bank, and the height of each tab at 1440 px.
import { expect, test, type Page } from '@playwright/test';
import { activate, hasFocus, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, T0, watchFileChoosers, zipFile, mockApi, SCREENS } from './mocks/problems';
import { toasts } from './locators';

test('each problem route has its own title', () => {
  const titles = SCREENS.map((s) => s.title);
  expect(new Set(titles).size).toBe(titles.length);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(`${screen.name}`, async ({ page }) => {
        await mockApi(page);
        await page.setViewportSize({ width: WIDTHS[WIDTHS.length - 1], height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        await page.evaluate(() => document.fonts.ready);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));

          const overflow = await page.evaluate(() => ({
            scroll: document.documentElement.scrollWidth,
            client: document.documentElement.clientWidth,
          }));
          expect(overflow.scroll, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(overflow.client);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page).toHaveTitle(screen.title);

          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

/** Opens one of the editor's tabs by its name. */
/** The editor's status line in the title row, a link to the validation report. */
function statusLine(page: Page) {
  return page.getByRole('link', { name: /^(Not validated|Validat|Making outputs|Archived)/ });
}

/** The status badge in a row of the problem bank. */
function rowStatus(row: ReturnType<Page['getByRole']>) {
  return row.getByRole('cell', { name: /^(Not validated|Validating|Validated|Archived)$/ });
}

function tabOf(page: Page, name: 'Statement' | 'Tests' | 'Checking') {
  return page.getByRole('tab', { name: new RegExp(`^${name}`) });
}

/** Describes the focused element and whether its focus indicator is visible. */
function focusInfo(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    // The code editor draws its ring on the well around it (CodeEditor.svelte).
    const ringed = el.closest('.host') as HTMLElement | null;
    const style = getComputedStyle(ringed ?? el);
    const outlineVisible =
      style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0 && !/rgba\(.*,\s*0\)$/.test(style.outlineColor);
    return {
      name: (el.getAttribute('aria-label') ?? (['INPUT', 'SELECT', 'TEXTAREA'].includes(el.tagName) ? el.id : el.textContent) ?? '').trim(),
      id: el.id,
      tag: el.tagName.toLowerCase(),
      visible: style.boxShadow !== 'none' || outlineVisible,
    };
  });
}

/** Tabs forward until `last` is reached, checking each stop shows focus; returns what was reached. */
async function tabThrough(page: Page, last: string, max = 80): Promise<string[]> {
  const reached: string[] = [];
  for (let i = 0; i < max; i++) {
    await page.keyboard.press('Tab');
    const info = await focusInfo(page);
    if (info === null) break;
    expect(info.visible, `no visible focus on ${info.tag} "${info.name || info.id}"`).toBe(true);
    reached.push(info.name || info.id);
    if (info.name === last || info.id === last) break;
  }
  return reached;
}

for (const width of [1440, 375]) {
  test(`keyboard reaches every control on each tab of the editor at ${width}px`, async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/problems/1');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');

    const statement = await tabThrough(page, 'tags');
    const head = ['Problems', 'Validated, 5 tests, slowest 212 ms', 'Try as a student', 'More', 'Statement', 'Save changes', 'Add to contest'];
    for (const name of [...head, 'title', 'Statement', 'time_limit_ms', 'memory_limit_mb', 'difficulty', 'tags', ...(width >= 768 ? [] : ['Write'])]) {
      expect(statement, `Tab did not reach "${name}" on Statement`).toContain(name);
    }

    // Arrows move between the tabs, as on every tablist.
    await tabOf(page, 'Statement').focus();
    await page.keyboard.press('ArrowRight');
    await expect(tabOf(page, 'Tests')).toHaveAttribute('aria-selected', 'true');
    const tests = await tabThrough(page, 'Add subtask');
    for (const name of ['Replace tests', 'Role of test 1', 'Role of test 5', 'Download input of test 5', 'subtask-0-points', 'subtask-0-title', 'Remove subtask 2', 'Add subtask']) {
      expect(tests, `Tab did not reach "${name}" on Tests`).toContain(name);
    }

    await tabOf(page, 'Tests').focus();
    await page.keyboard.press('ArrowRight');
    await expect(tabOf(page, 'Checking')).toHaveAttribute('aria-selected', 'true');
    const checking = await tabThrough(page, 'Optional extra checks');
    for (const name of ['reference-language', 'reference-source', 'checker', 'Optional extra checks']) {
      expect(checking, `Tab did not reach "${name}" on Checking`).toContain(name);
    }
  });
}

test('proof: a problem imported, a test given a new role from the select, saved, validated and added to a contest', async ({ page }) => {
  const state = await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems');
  await page.getByRole('button', { name: 'Import pack' }).click();
  await (await choosers.next()).setFiles(zipFile('pair.zip', 'good pack'));
  const row = page.getByRole('row', { name: /Pair with sum/ });
  await expect(rowStatus(row)).toHaveText('Validated', { timeout: 15_000 });
  await row.getByRole('link', { name: 'Pair with sum', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pair with sum');

  await tabOf(page, 'Tests').click();
  const role = page.getByLabel('Role of test 4');
  await expect(role).toHaveValue('1');
  await role.selectOption({ label: 'Subtask 2' });
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Subtask 1' }).getByText(/^\d+ tests?$/)).toHaveText('1 test');
  await expect(page.getByRole('group', { name: 'Subtask 2' }).getByText(/^\d+ tests?$/)).toHaveText('2 tests');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(toasts(page).getByText('Saved', { exact: true })).toBeVisible();
  // The server gets the same lists as before: examples and a range per subtask.
  expect(state.puts.at(-1)?.samples).toEqual([1, 2]);
  expect(state.puts.at(-1)?.subtasks).toEqual([
    { points: 40, title: 'Small numbers', tests: '3' },
    { points: 60, title: 'Full range', tests: '4-5' },
  ]);
  await expect(statusLine(page)).toHaveText('Not validated, 5 tests');

  const validate = page.getByRole('button', { name: 'Validate', exact: true });
  await expect(validate).toHaveClass(/btn-primary/);
  await validate.click();
  await expect(statusLine(page)).toHaveText('Validating, 2nd in queue');
  await expect(statusLine(page)).toHaveText('Validated, 5 tests, slowest 212 ms', { timeout: 15_000 });
  // Once it passes, Add to contest takes Validate's place.
  await expect(validate).toHaveCount(0);
  await page.getByRole('button', { name: 'Add to contest' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add Pair with sum to a contest' });
  await expect(dialog.getByLabel('Contest').locator('option')).toHaveText(['Week 7, draft', /^Week 8, starts /]);
  await dialog.getByRole('button', { name: 'Add to contest' }).click();
  await expect(toasts(page).getByText('Added to Week 7 as problem B', { exact: true })).toBeVisible();
  expect(state.contestPuts).toEqual([{ id: 7, body: [{ problem_id: 2, label: 'A' }, { problem_id: 10, label: 'B' }] }]);
  await page.screenshot({ path: `${shots()}problem-editor-proof-1440-light.png`, fullPage: true });
});

test('each tab of the editor is under 1,400 px tall at 1440 px (the walk measured 3,987 px)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const id of [1, 4]) {
    await page.goto(`/admin/problems/${id}`);
    await expect(statusLine(page)).toBeVisible();
    for (const name of ['Statement', 'Tests', 'Checking'] as const) {
      await tabOf(page, name).click();
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
      const height = await page.evaluate(() => document.documentElement.scrollHeight);
      test.info().annotations.push({ type: 'height', description: `problem ${id}, ${name}: ${height} px` });
      await page.screenshot({ path: `${shots()}problem-editor-${id}-${name.toLowerCase()}-1440-light.png`, fullPage: true });
      // Problem 1 is the walk's problem; problem 4 (15 tests, a failed report) is recorded for comparison.
      if (id === 1) expect.soft(height, `${name} tab`).toBeLessThan(1400);
    }
  }
});

test('subtasks and roles work from the keyboard, and removing a subtask leaves its tests in none', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await tabOf(page, 'Tests').click();

  const add = page.getByRole('button', { name: 'Add subtask' });
  await add.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#subtask-2-points')).toBeFocused();
  await page.keyboard.type('10');
  await page.keyboard.press('Tab');
  await page.keyboard.type('Extra');
  await page.getByLabel('Role of test 5').selectOption('3');
  await expect(page.getByLabel('Role of test 5').locator('option')).toHaveText(['Example', 'Subtask 1', 'Subtask 2', 'Subtask 3', 'Not in a subtask']);

  await page.getByRole('button', { name: 'Remove subtask 3' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('group', { name: 'Subtask 3' })).toHaveCount(0);
  await expect(add).toBeFocused();
  await expect(page.getByLabel('Role of test 5')).toHaveValue('none');
  await expect(page.getByText('Test 5 is in no subtask. Choose its role.')).toBeVisible();
  await expect(page.getByRole('group', { name: 'Subtask 2' }).getByText(/^\d+ tests?$/)).toHaveText('0 tests');

  // An empty subtask is caught before saving and its link goes to the table.
  await page.getByRole('button', { name: 'Save changes' }).click();
  const list = page.locator('#field-errors');
  await expect(list).toContainText("Subtask 2 tests: Give subtask 2 at least one test: choose Subtask 2 as a test's role.");
  expect(state.puts).toHaveLength(0);

  await page.getByLabel('Role of test 5').focus();
  await page.keyboard.press('ArrowUp');
  await expect(page.getByLabel('Role of test 5')).toHaveValue('2');
  await page.locator('#subtask-1-points').fill('50');
  await page.getByRole('button', { name: 'Save changes' }).focus();
  await page.keyboard.press('Enter');
  await expect(toasts(page).getByText('Saved', { exact: true })).toBeVisible();
  // The polite announcer says the same words, apart from the visible toast (C1-019).
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Saved');
  expect(state.puts.at(-1)?.subtasks).toEqual([
    { points: 40, title: 'Small numbers', tests: '3-4' },
    { points: 50, title: 'Full range', tests: '5' },
  ]);
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);
  await expect(statusLine(page)).toHaveText('Not validated, 5 tests');

  await page.getByRole('button', { name: 'Validate', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(statusLine(page)).toHaveText('Validated, 5 tests, slowest 212 ms', { timeout: 15_000 });
  await tabOf(page, 'Checking').click();
  await expect(page.getByTestId('validation-headline')).toHaveText('Validation passed');
  await expect(page.getByText('Reference solution passed 5 of 5 tests')).toBeVisible();
  await expect(page.getByText('Slowest reference run 212 ms of 1000 ms')).toBeVisible();
  await expect(page.locator('[aria-live="polite"]').filter({ hasText: 'Validation passed' })).toHaveCount(1);
});

test('the editor stacks at 375 px with Write and Preview tabs for the statement', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.getByRole('region', { name: 'Preview', exact: true })).toHaveCount(0);

  const write = page.getByRole('tab', { name: 'Write' });
  await expect(write).toHaveAttribute('aria-selected', 'true');
  // Every aria-controls names a panel that is in the page (C1-037).
  const dangling = () =>
    page.evaluate(() => [...document.querySelectorAll('[role="tab"][aria-controls]')].filter((t) => !document.getElementById(t.getAttribute('aria-controls') ?? '')).length);
  expect(await dangling()).toBe(0);
  await write.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tabpanel', { name: 'Preview' }).locator('.katex').first()).toBeVisible();
  await expect(page.locator('#statement_md')).toBeHidden();
  expect(await dangling()).toBe(0);
  await page.screenshot({ path: `${shots()}problem-editor-preview-tab-375-light.png`, fullPage: true });
  await page.getByRole('tab', { name: 'Write' }).click();
  await expect(page.locator('#statement_md')).toBeVisible();

  // Tests stack: number and role, then input over output.
  await tabOf(page, 'Tests').click();
  const row = page.getByRole('row').filter({ has: page.getByLabel('Role of test 5') });
  const input = await row.getByRole('cell').nth(0).boundingBox();
  const output = await row.getByRole('cell').nth(1).boundingBox();
  const num = await row.getByRole('rowheader').boundingBox();
  const role = await row.getByRole('cell').filter({ has: page.getByLabel('Role of test 5') }).boundingBox();
  expect(output!.y).toBeGreaterThan(input!.y + input!.height - 1);
  expect(Math.abs(role!.y - num!.y)).toBeLessThan(12);
  await page.screenshot({ path: `${shots()}problem-editor-tests-375-light.png`, fullPage: true });
});

test('server field errors appear next to their fields, and a link opens the tab that holds one', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const time = page.getByLabel('Time limit (ms)');
  await time.fill('99999');
  await page.getByRole('button', { name: 'Save changes' }).click();

  await expect(page.locator('#time_limit_ms-error')).toHaveText('Set a time limit from 100 to 10000 ms.');
  await expect(time).toHaveAttribute('aria-invalid', 'true');
  await expect(time).toHaveAttribute('aria-describedby', 'time_limit_ms-error');
  await expect(time).toBeFocused();
  await expect(page.getByRole('link', { name: '2 fields need attention' })).toBeVisible();
  const list = page.locator('#field-errors');
  await expect(list).toContainText('Some fields need fixing.');
  await list.getByRole('link', { name: 'Subtask 2 points: Points run from 1 to 1000.' }).click();
  await expect(tabOf(page, 'Tests')).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#subtask-1-points')).toBeFocused();
  await expect(page.locator('#subtasks-1-points-error')).toHaveText('Points run from 1 to 1000.');
  await page.screenshot({ path: `${shots()}problem-editor-field-errors-1440-light.png`, fullPage: true });
});

test('a failed validation names the test, program and verdict, and its links unfold the extra checks', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await expect(statusLine(page)).toHaveText('Validation failed on test 7');
  await statusLine(page).click();
  await expect(tabOf(page, 'Checking')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeFocused();
  await expect(page.getByTestId('validation-headline')).toHaveText('Validation failed');
  await expect(page.getByText('Fix the brute force: it did not compile.', { exact: true })).toBeVisible();

  // Grouped by program, each with a link to its source.
  const reference = page.getByRole('region', { name: 'Reference solution' });
  await expect(reference.getByText('Wrong answer on test 7')).toBeVisible();
  await expect(reference.getByText('Line 1: expected 2.500000, found 2.400000')).toBeVisible();
  await expect(reference.getByText(/expected output/)).toBeVisible();
  const brute = page.getByRole('region', { name: 'Brute force' });
  await expect(brute.getByText('Brute force did not compile', { exact: true })).toBeVisible();
  await expect(page.locator('#brute-source')).toBeHidden();
  await brute.getByRole('link', { name: 'Go to brute force' }).click();
  await expect(page.locator('#brute-source')).toBeFocused();

  await page.getByText('All test results').click();
  await expect(page.getByRole('cell', { name: 'Wrong answer' })).toBeVisible();

  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(page.getByText('Waiting for the judge (2nd in queue)')).toBeVisible();
  await expect(page.getByTestId('validation-headline')).toHaveText('Validation failed', { timeout: 10_000 });
});

test('once validated, the advice is headed Worth adding, not Before you validate (walk 2026-10-21)', async ({ page }) => {
  const api = await mockApi(page);
  (api.problems[1] as { warnings: string[] }).warnings = ['There is no validator.'];
  await page.goto('/admin/problems/1');
  await tabOf(page, 'Checking').click();
  await expect(page.getByRole('note', { name: 'Worth adding' })).toContainText('There is no validator.');
  await expect(page.getByRole('note', { name: 'Before you validate' })).toHaveCount(0);
});

test('validation is refused while a test is in no subtask, and choosing its role fixes the table', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  // What blocks validation sits under the bar on every tab.
  const blockers = page.getByRole('group', { name: 'Fix this before you validate' });
  await expect(blockers).toContainText('Test 7 is in no subtask. Add it to a subtask, or mark it as a sample.');
  // While something blocks validation, Validate is not the primary action.
  await expect(page.getByRole('button', { name: 'Validate', exact: true })).toHaveClass(/btn-secondary/);

  await tabOf(page, 'Tests').click();
  await expect(page.getByText('7 tests: 1 example that students see, the rest hidden.')).toBeVisible();
  await expect(page.getByText('Test 7 is in no subtask. Choose its role.')).toBeVisible();
  await expect(page.getByLabel('Role of test 7')).toHaveValue('none');
  await page.getByLabel('Role of test 7').selectOption({ label: 'Subtask 2' });
  await expect(page.getByText('Test 7 is in no subtask. Choose its role.')).toHaveCount(0);
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();
  // Moved back, the lists are as they were: nothing to save.
  await page.getByLabel('Role of test 7').selectOption('none');
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);

  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  const alert = page.getByRole('alert').filter({ hasText: 'This problem cannot be validated yet.' });
  await expect(alert).toContainText('Test 7 is in no subtask.');
});

test('importing a pack shows its problems, then validates the new problem in place (P3 O6)', async ({ page }) => {
  const state = await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Old sorting', exact: true })).toHaveCount(0);

  const importButton = page.getByRole('button', { name: 'Import pack' });
  await importButton.focus();
  await page.keyboard.press('Enter');
  await (await choosers.next()).setFiles(zipFile('broken.zip', 'bad pack'));

  const alert = page.getByRole('alert');
  await expect(alert).toContainText('The pack has 2 problems. Fix them and import it again.');
  await expect(alert.getByRole('listitem')).toHaveText([
    'problem.yaml: unknown key "timelimit". Did you mean time_limit_ms?',
    'tests/03.out is missing.',
  ]);
  await page.screenshot({ path: `${shots()}problems-bank-import-error-1440-light.png`, fullPage: true });

  // A good pack stays on the list and starts validating at once: no visit to the editor needed.
  await page.locator('input[type="file"]').setInputFiles(zipFile('pair.zip', 'good pack'));
  await expect(toasts(page).getByText('Imported Pair with sum. Validating.', { exact: true })).toBeVisible();
  await expect(page).toHaveURL('/admin/problems');
  const row = page.getByRole('row', { name: /Pair with sum/ });
  await expect(rowStatus(row)).toHaveText('Validating');
  await expect(rowStatus(row)).toHaveText('Validated', { timeout: 15_000 });
  expect(state.imports).toEqual(['bad pack', 'good pack']);
});

test('several packs import in one pick, and a row offers Validate when it is not validated (P3 O6)', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems');
  await expect(rowStatus(page.getByRole('row', { name: /Interval covering/ }))).toHaveText('Not validated');
  await page.locator('input[type="file"]').setInputFiles([zipFile('one.zip', 'good pack 1'), zipFile('two.zip', 'good pack 2')]);
  await expect.poll(() => state.imports).toEqual(['good pack 1', 'good pack 2']);
  await expect(toasts(page).getByText('Imported Pair with sum 1. Validating. Imported Pair with sum 2. Validating.', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Pair with sum 1/ })).toHaveCount(1);
  await expect(page.getByRole('row', { name: /Pair with sum 2/ })).toHaveCount(1);
  // Interval covering has a test in no subtask, so validating it is refused with the reason.
  await page.getByRole('button', { name: 'Validate Interval covering' }).click();
  await expect(page.getByText('Interval covering cannot be validated yet: Test 7 is in no subtask.', { exact: false }).first()).toBeVisible();
});

test('the bank shows status words, archived problems on request, and creates a problem from a title alone', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problems');
  await expect(page.getByRole('row', { name: /A plus B/ })).toContainText('Validated');
  await expect(page.getByRole('row', { name: /Interval covering/ })).toContainText('Not validated');

  await page.getByLabel('Show archived').check();
  await expect(page).toHaveURL('/admin/problems?archived=1');
  await expect(page.getByRole('row', { name: /Old sorting/ })).toContainText('Archived');

  await page.getByRole('button', { name: 'New problem' }).click();
  const title = page.getByLabel('Title', { exact: true });
  await expect(title).toBeFocused();
  // The form asks for a title only; the server makes the rest.
  await expect(page.locator('#create-problem input')).toHaveCount(1);
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page.locator('#new-title-error')).toHaveText('Enter a title.');
  await title.fill('Interval Covering');
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page).toHaveURL('/admin/problems/10');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Interval Covering');
  expect(state.problems[10].slug).toBe('interval-covering-2');
  await tabOf(page, 'Tests').click();
  await expect(page.getByText('No tests yet.')).toBeVisible();
});

test('archiving is in the menu, asks first and returns to the bank', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  const menu = page.getByRole('button', { name: 'More' });
  await menu.click();
  await expect(page.locator('#problem-menu').getByRole('button')).toHaveText(['Download pack', 'Regenerate all outputs', 'Archive']);
  await page.locator('#problem-menu').getByRole('button', { name: 'Archive' }).click();
  const dialog = page.getByRole('dialog', { name: 'Archive A plus B?' });
  await expect(dialog).toContainText('It leaves the list of problems. Contests that used it keep it.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  expect(state.archived).toEqual([]);

  await menu.click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Archive' }).click();
  await dialog.getByRole('button', { name: 'Archive problem' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByText('Archived', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toHaveCount(0);
  expect(state.archived).toEqual([1]);
});

test('Download pack in the menu downloads the pack', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'More' }).click();
  const download = page.waitForEvent('download');
  await page.locator('#problem-menu').getByRole('button', { name: 'Download pack' }).click();
  // The browser's download is not routed through the mock, so the address is what is checked.
  expect(new URL((await download).url()).pathname).toBe('/api/admin/problems/1/export');
});

test('leaving with unsaved changes asks first', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.locator('#title').fill('A plus B, edited');
  const preview = page.getByRole('region', { name: 'Preview', exact: true });
  await expect(preview.getByText('Given two integers')).toBeVisible();
  await expect(preview.getByText('A plus B, edited', { exact: true })).toBeVisible();

  await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Problems' }).click();
  const dialog = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page).toHaveURL('/admin/problems/1');
  await expect(page.locator('#title')).toHaveValue('A plus B, edited');

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Problems', exact: true }).click();
  await dialog.getByRole('button', { name: 'Leave page' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problems');
});

test('replacing tests uploads a zip and lists the new tests with their roles', async ({ page }) => {
  await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.goto('/admin/problems/1');
  await tabOf(page, 'Tests').click();
  await page.getByRole('button', { name: 'Replace tests' }).click();
  // Replacing every test asks first; the file picker opens from the confirm.
  const confirm = page.getByRole('dialog', { name: /^Replace all \d+ tests\?$/ });
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirm.getByRole('button', { name: 'Replace tests' }).click();
  await (await choosers.next()).setFiles(zipFile('tests.zip', 'zip'));
  await expect(page.getByText('Replaced tests. 2 tests stored.')).toBeVisible();
  // Roles come from the example and subtask lists as they stand (examples 1, 2).
  await expect(page.getByLabel('Role of test 2')).toHaveValue('example');
  await expect(tabOf(page, 'Tests')).toHaveText(/Tests\s*2/);
  await expect(page.getByRole('link', { name: 'Download output of test 2' })).toHaveAttribute('href', '/api/admin/problems/1/tests/2/output');
});

test('statements are sanitised and maths is rendered', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const statement = page.locator('#statement_md');
  await expect(statement).toHaveAttribute('contenteditable', 'true');
  await statement.fill('# Heading\n\nHi <img src=x onerror="window.__pwned=1"> [link](javascript:window.__pwned=2) and $x^2$.\n\n![figure](/fig.png)');
  const preview = page.getByRole('region', { name: 'Preview', exact: true });
  await expect(preview.locator('.katex')).toHaveCount(1);
  await expect(preview.locator('h4')).toHaveText('Heading');
  await expect(preview.locator('img[onerror]')).toHaveCount(0);
  await expect(preview).toContainText('<img src=x onerror="window.__pwned=1">');
  expect(await preview.locator('a').getAttribute('href')).toBeNull();
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined();
  await expect(page.locator('h1')).toHaveCount(1);
});

test('students see an explanation instead of the bank and no Problems link', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/admin/problems');
  await expect(page.getByText('Only organisers can manage problems.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Problems', exact: true })).toHaveCount(0);
});

test('admins see the Problems link and it marks the current page', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await nav.getByRole('link', { name: 'Problems', exact: true }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(nav.getByRole('link', { name: 'Problems', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await page.getByRole('link', { name: 'A plus B', exact: true }).click();
  await expect(nav.getByRole('link', { name: 'Problems', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'Settings' })).toHaveCount(0);
});

test('Validate makes missing outputs from the reference, then validates; there is no Generate button', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/5');
  await expect(statusLine(page)).toHaveText('Not validated, 5 tests');
  // The missing outputs are not something to fix: Validate makes them.
  await expect(page.getByRole('group', { name: /before you validate/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Validate', exact: true })).toHaveClass(/btn-primary/);
  await tabOf(page, 'Tests').click();
  await expect(page.getByText('2 tests have no expected output yet. Validate makes them from the reference solution.')).toBeVisible();
  await expect(page.getByRole('row').filter({ has: page.getByLabel('Role of test 4') })).toContainText('Made on Validate');
  await expect(page.getByRole('link', { name: 'Download output of test 4' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Generate/ })).toHaveCount(0);

  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(statusLine(page)).toHaveText('Making outputs, then validating');
  expect(state.generates).toEqual([{ then_validate: true }]);
  await expect(statusLine(page)).toHaveText('Validated, 5 tests, slowest 212 ms', { timeout: 15_000 });
  await expect(page.getByText('Made on Validate')).toHaveCount(0);
  await page.screenshot({ path: `${shots()}problem-editor-generated-1440-light.png`, fullPage: true });
});

test('Regenerate all outputs is in the menu and asks first', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Regenerate all outputs' }).click();
  const dialog = page.getByRole('dialog', { name: 'Regenerate all outputs?' });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Regenerate all outputs' }).click();
  await expect.poll(() => state.generates.at(-1)).toEqual({ all: true });
  await expect(page.getByText('Generated 5 outputs. Slowest run 220 ms.', { exact: true })).toBeVisible({ timeout: 10_000 });
});

test('a refused generation shows the reason in place', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Regenerate all outputs' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Regenerate all outputs' }).click();
  // Shown in the polite region under the bar, read once (not also an alert).
  const reason = page.locator('[aria-live="polite"]').filter({ hasText: 'The problem is not ready. Fix these first.' });
  await expect(reason).toContainText('Add a reference solution first.');
});

test('a reference that does not compile, or a problem changed mid-run, is said plainly', async ({ page }) => {
  const state = await mockApi(page);
  state.genOutcome = 'compile';
  await page.goto('/admin/problems/5');
  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(page.getByText('The reference solution did not compile, so no outputs were generated.', { exact: true })).toBeVisible({
    timeout: 10_000,
  });
  await expect(page.getByRole('listitem').filter({ hasText: 'expected ; before return' })).toBeVisible();
  await expect(statusLine(page)).toHaveText('Not validated, 5 tests');

  state.genOutcome = 'stale';
  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(page.getByText('Some outputs were not applied.', { exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole('listitem').filter({ hasText: 'The problem changed while outputs were generated.' })).toBeVisible();
});

test('the sticky bar keeps the tabs, field errors and the main actions in reach', async ({ page }) => {
  const state = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('link', { name: 'Try as a student' })).toHaveAttribute('href', '/admin/problems/1/try');
  await expect(page.getByRole('button', { name: 'Add to contest' })).toHaveClass(/btn-primary/);

  await page.locator('#title').fill('A plus B, edited');
  await expect(page.getByRole('button', { name: 'Save and validate' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes' })).toHaveClass(/btn-primary/);

  // The bar stays at the top while the page scrolls.
  await tabOf(page, 'Checking').click();
  await page.mouse.wheel(0, 2000);
  // The scroll can settle a frame after the wheel under load.
  await expect.poll(async () => (await page.locator('.action-bar').boundingBox())!.y).toBeLessThanOrEqual(1);
  await page.screenshot({ path: `${shots()}problem-editor-sticky-1440-light.png` });

  // Ctrl+S saves.
  await page.keyboard.press('Control+s');
  await expect(toasts(page).getByText('Saved', { exact: true })).toBeVisible();
  expect(state.puts).toHaveLength(1);
});

test('a problem with no tests offers Upload tests, and no extra checks are in use', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/3');
  await expect(statusLine(page)).toHaveText('Archived');
  await tabOf(page, 'Tests').click();
  await expect(page.getByRole('button', { name: 'Upload tests' })).toBeVisible();
  await expect(page.getByText('No tests yet. Upload a .zip of numbered input and output files to add them.')).toBeVisible();
  await page.getByRole('button', { name: 'More' }).click();
  await expect(page.locator('#problem-menu').getByRole('button')).toHaveText(['Download pack']);
  await page.keyboard.press('Escape');
  await tabOf(page, 'Checking').click();
  await expect(page.getByRole('list', { name: 'Optional extra checks in use' })).toHaveText('None in use.');
  await page.getByText('Optional extra checks').click();
  await expect(page.getByText('None. Add one to check every test input against the constraints.')).toBeVisible();
});

test('the optional extra checks fold away with one line for each in use', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  await tabOf(page, 'Checking').click();
  await expect(page.getByLabel('Allowed error')).toBeVisible();
  const used = page.getByRole('list', { name: 'Optional extra checks in use' });
  await expect(used.getByRole('listitem')).toHaveText(['Validator in Python 3']);
  await expect(page.getByRole('group', { name: 'Validator' })).toBeHidden();

  // Choosing the custom checker unfolds the section at its source.
  await page.getByLabel('How answers are compared').selectOption('custom');
  await expect(page.locator('#checker_source')).toBeFocused();
  await expect(used).toHaveCount(0);
  await page.getByText('Optional extra checks').click();
  await expect(used.getByRole('listitem')).toHaveText(['Validator in Python 3', 'Custom checker in C++17 with testlib']);
});

test('the bank sorts by last change, filters past ten problems and links to Try', async ({ page }) => {
  const state = await mockApi(page);
  for (let i = 0; i < 8; i++) {
    const id = 20 + i;
    state.problems[id] = { ...state.problems[1], id, slug: `extra-${i}`, title: `Extra ${i}`, tags: i === 3 ? ['graphs'] : [], updated_at: T0 - (40 + i) * 86400000 };
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems');
  const titles = page.locator('tbody th.title');
  await expect(titles.first()).toHaveText('Pair sums');
  await expect(titles.nth(1)).toHaveText('Interval covering');
  await expect(page.getByRole('row', { name: /A plus B/ }).first()).toContainText('Easy');
  await expect(page.getByRole('row', { name: /A plus B/ }).first()).toContainText('math, intro');
  await expect(page.getByRole('link', { name: 'Try A plus B as a student' }).first()).toHaveAttribute('href', '/admin/problems/1/try');

  const filter = page.getByLabel('Filter by title or tag');
  await filter.fill('graphs');
  await expect(titles).toHaveText(['Extra 3']);
  await filter.fill('nothing like this');
  await expect(page.getByText('No problem matches "nothing like this". Try another word.')).toBeVisible();
});

test('bank rows read as title and status, then tests and date, on a phone', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /A plus B/ });
  const title = await row.getByRole('rowheader').boundingBox();
  const status = await row.locator('.state').boundingBox();
  const tests = await row.getByRole('cell').filter({ hasText: /^Tests\s*\d+$/ }).boundingBox();
  expect(Math.abs(title!.y - status!.y)).toBeLessThan(6);
  expect(tests!.y).toBeGreaterThan(title!.y + title!.height - 1);
  await expect(row.locator('.tags')).toBeHidden();
});

test('keyboard only: an organiser opens a problem, edits the statement, saves with Ctrl+S and adds it to a contest', async ({ page }) => {
  const state = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems');
  await expect(page.getByRole('row', { name: /A plus B/ })).toBeVisible();

  await activate(page, page.getByRole('link', { name: 'A plus B', exact: true }));
  await expect(page).toHaveURL('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  // The statement is the workspace's editor as plain text: Tab moves on from it, never traps.
  const statement = page.locator('#statement_md');
  await tabTo(page, statement);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n\nAll numbers fit in 64 bits.');
  await page.keyboard.press('Tab');
  expect(await hasFocus(statement)).toBe(false);
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();

  await statement.focus();
  await page.keyboard.press('Control+s');
  await expect(toasts(page).getByText('Saved', { exact: true })).toBeVisible();
  expect(String(state.puts.at(-1)?.statement_md)).toContain('All numbers fit in 64 bits.');

  // The statement is not judged, so the problem stays validated and Add to contest is the next step.
  await page.locator('main h1').focus();
  await activate(page, page.getByRole('button', { name: 'Add to contest' }));
  const dialog = page.getByRole('dialog', { name: 'Add A plus B to a contest' });
  await expect(dialog).toBeVisible();
  await tabTo(page, dialog.getByLabel('Contest'));
  await page.keyboard.press('ArrowDown');
  await expect(dialog.getByLabel('Contest')).toHaveValue('8');
  await activate(page, dialog.getByRole('button', { name: 'Add to contest' }));
  await expect(toasts(page).getByText('Added to Week 8 as problem A', { exact: true })).toBeVisible();
  expect(state.contestPuts.at(-1)).toEqual({ id: 8, body: [{ problem_id: 1, label: 'A' }] });

  // The shortcut list names the editor keys for organisers.
  await page.locator('main h1').focus();
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Help' });
  await expect(keys.getByRole('heading', { name: 'Problem editor' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
});

test('a finished validation says its result under the bar and the report link opens Checking (P3 O2)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');

  // An unsaved judged change says it will need a new validation; a title change does not.
  await page.locator('#title').fill('A plus B again');
  await expect(page.getByText('Saving needs a new validation', { exact: false })).toHaveCount(0);
  await page.locator('#time_limit_ms').fill('2000');
  await expect(page.getByText('Saving needs a new validation: you changed the limits.')).toBeVisible();

  await page.getByRole('button', { name: 'Save and validate' }).click();
  await expect(page.getByText('Validation passed: 5 of 5 tests, slowest 212 ms.', { exact: true })).toBeVisible({ timeout: 15_000 });
  await page.getByRole('link', { name: 'See the report' }).click();
  await expect(tabOf(page, 'Checking')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeFocused();
});

test('removing the brute force or validator can be undone until the next save (P3 A6)', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  await tabOf(page, 'Checking').click();
  await page.getByText('Optional extra checks').click();
  await page.getByRole('button', { name: 'Remove validator' }).click();
  await expect(page.getByText('Validator removed. It goes for good when you save.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Undo remove' })).toBeFocused();
  await page.getByRole('button', { name: 'Undo remove' }).click();
  await expect(page.locator('#validator-source')).toBeFocused();
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);
});

test('the preview shows the examples as students see them (P3 A5)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const preview = page.getByRole('region', { name: 'Preview' });
  await expect(preview.getByRole('heading', { name: 'Example 1' })).toBeVisible();
  await expect(preview.getByRole('heading', { name: 'Example 2' })).toBeVisible();
  await expect(preview.getByRole('button', { name: 'Copy example 1 input' })).toBeVisible();
  // The examples follow the roles as chosen.
  await tabOf(page, 'Tests').click();
  await page.getByLabel('Role of test 2').selectOption({ label: 'Subtask 1' });
  await tabOf(page, 'Statement').click();
  await expect(preview.getByRole('heading', { name: 'Example 2' })).toHaveCount(0);
});

test('an organiser screen that fails to load says so and offers a reload (C2-069)', async ({ page }) => {
  await mockApi(page);
  let fail = true;
  await page.route(
    (url) => url.pathname.includes('/routes/ProblemBank.svelte'),
    (route) => (fail ? route.abort() : route.fallback()),
  );
  await page.goto('/admin/problems');
  await expect(page.getByRole('alert')).toHaveText('This page could not load. Reload to try again.');
  fail = false;
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('link', { name: 'Interval covering', exact: true })).toBeVisible();
});
