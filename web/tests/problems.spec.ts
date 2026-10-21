// Screen sweep and flows for the M2 problem screens (/admin/problems and
// /admin/problems/{id}). Every /api/ call is answered by an in-test mock of
// the admin problems API in cache/v1/m2-plan.md section 4, one per page, so
// each test starts from the same fixtures: an admin with four problems
// (validated, not validated with an unassigned test, archived, and one whose
// last validation failed).
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m2/<screen>-<width>-<theme>.png. Further tests cover the
// keyboard path through the editor, stacking at 375 px, saving with a field
// error, validation from queued to passed and to failed, import errors and
// success, creating, archiving, and the unsaved-changes guard.
import { expect, test, type Page } from '@playwright/test';
import { activate, hasFocus, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, T0, watchFileChoosers, zipFile, mockApi, SCREENS } from './mocks/problems';

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

/** Describes the focused element and whether its focus indicator is visible. */
function focusInfo(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    const style = getComputedStyle(el);
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

for (const width of [1440, 375]) {
  test(`keyboard reaches every control on the editor at ${width}px`, async ({ page }) => {
    await mockApi(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/problems/1');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
    if (width >= 768) await expect(page.locator('.statement .katex').first()).toBeVisible();

    const reached: string[] = [];
    for (let i = 0; i < 120; i++) {
      await page.keyboard.press('Tab');
      const info = await focusInfo(page);
      if (info === null) break;
      expect(info.visible, `no visible focus on ${info.tag} "${info.name || info.id}"`).toBe(true);
      reached.push(info.name || info.id);
      if (info.name === 'Archive problem') break;
    }

    const always = [
      'Problem bank', 'Try as a student', 'Validated 16 Oct 02:09', 'Save changes', 'Validate', 'title', 'slug',
      'statement_md', 'time_limit_ms', 'memory_limit_mb', 'checker', 'tags', 'difficulty', 'samples', 'subtask-0-points',
      'Remove subtask 1', 'Remove subtask 2', 'Add subtask', 'Replace tests', 'Regenerate all outputs', 'Show all 5 tests',
      'reference-language', 'reference-source', 'Remove brute force', 'Add validator', 'Download pack', 'Archive problem',
    ];
    // The jump links carry a short state (P3 A3) and stay on phones as one scrolling row.
    const wide = ['Statement', 'Solutions with brute force', 'Tests 5', 'Judging no validator', 'Validation'];
    const phone = ['Write'];
    for (const name of [...always, ...wide, ...(width >= 768 ? [] : phone)]) {
      expect(reached, `Tab did not reach "${name}"`).toContain(name);
    }
  });
}

test('subtasks, saving and validating work from the keyboard', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');

  const add = page.getByRole('button', { name: 'Add subtask' });
  await add.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#subtask-2-points')).toBeFocused();
  await page.keyboard.type('10');
  await page.keyboard.press('Tab');
  await page.keyboard.type('Extra');
  await page.keyboard.press('Tab');
  await page.keyboard.type('6');
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Remove subtask 3' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('group', { name: 'Subtask 3' })).toHaveCount(0);
  await expect(add).toBeFocused();

  await page.locator('#subtask-1-points').fill('50');
  await page.getByRole('button', { name: 'Save changes' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('.toasts').getByText('Saved', { exact: true })).toBeVisible();
  // The polite announcer says the same words, apart from the visible toast (C1-019).
  await expect(page.locator('[data-announcer="polite"]')).toHaveText('Saved');
  expect(state.puts.at(-1)?.subtasks).toEqual([
    { points: 40, title: 'Small numbers', tests: '3-4' },
    { points: 50, title: 'Full range', tests: '5' },
  ]);
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);
  await expect(page.locator('.status')).toContainText('Not validated');
  await expect(page.getByText('These results are for an earlier version. Validate again.')).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText('Waiting for the judge (2nd in queue)')).toBeVisible();
  await expect(page.locator('.headline')).toHaveText('Running', { timeout: 10_000 });
  await expect(page.locator('.headline')).toHaveText('Validation passed', { timeout: 10_000 });
  await expect(page.getByText('Reference solution passed 5 of 5 tests')).toBeVisible();
  await expect(page.getByText('Brute force agreed on 5 of 5 tests')).toBeVisible();
  await expect(page.getByText('Slowest reference run 212 ms of 1000 ms')).toBeVisible();
  await expect(page.locator('.status')).toContainText('Validated');
  await expect(page.locator('[aria-live="polite"]').filter({ hasText: 'Validation passed' })).toHaveCount(1);
  await page.screenshot({ path: `${shots()}problem-editor-validated-1440-light.png`, fullPage: true });
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

  // Tests stack: input over output.
  await page.getByText('Show all 5 tests').click();
  const row = page.getByRole('row', { name: /^5 Subtask 2/ });
  const input = await row.locator('.file-cell').nth(0).boundingBox();
  const output = await row.locator('.file-cell').nth(1).boundingBox();
  expect(output!.y).toBeGreaterThan(input!.y + input!.height - 1);

  const boxes = [];
  // Sections in dependency order (P3 A3): the statement, the solutions the outputs come from, then the tests.
  for (const name of ['Statement and limits', 'Solutions', 'Tests']) {
    const box = await page.getByRole('region', { name, exact: true }).boundingBox();
    expect(box, name).not.toBeNull();
    boxes.push(box!);
  }
  for (let i = 1; i < boxes.length; i++) {
    expect(boxes[i].y, 'sections follow each other').toBeGreaterThanOrEqual(boxes[i - 1].y + boxes[i - 1].height - 1);
    expect(Math.abs(boxes[i].x - boxes[0].x)).toBeLessThan(2);
  }
});

test('server field errors appear next to their fields', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const slug = page.getByLabel('Slug', { exact: true });
  await slug.fill('taken');
  await page.getByRole('button', { name: 'Save changes' }).click();

  await expect(page.locator('#slug-error')).toHaveText('Another problem already uses the slug taken.');
  await expect(slug).toHaveAttribute('aria-invalid', 'true');
  await expect(slug).toHaveAttribute('aria-describedby', 'slug-help slug-error');
  await expect(slug).toBeFocused();
  await expect(page.locator('#subtasks-1-points-error')).toHaveText('Points run from 1 to 1000.');
  await expect(page.getByRole('alert').filter({ hasText: 'Some fields need fixing.' })).toBeVisible();

  await slug.fill('a-plus-b');
  await page.locator('#samples').fill('1, x');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#samples-error')).toContainText('"x" is not a test number or range.');
  await expect(page.locator('#slug-error')).toHaveCount(0);
  await page.screenshot({ path: `${shots()}problem-editor-field-errors-1440-light.png`, fullPage: true });
});

test('a failed validation names the test, program and verdict', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await expect(page.locator('.headline')).toHaveText('Validation failed');
  await expect(page.locator('.status')).toHaveText('Validation failed, 1 problem');
  await expect(page.locator('.report .lead')).toHaveText('Fix the brute force: it did not compile.');

  // Grouped by program, each with a link to its source.
  const reference = page.getByRole('region', { name: 'Reference solution' });
  await expect(reference.getByText('Wrong answer on test 7')).toBeVisible();
  await expect(reference.getByText('Line 1: expected 2.500000, found 2.400000')).toBeVisible();
  await expect(reference.getByText('Reference solution passed 14 of 15 tests')).toBeVisible();
  await expect(reference.locator('.guidance')).toContainText('expected output');
  const brute = page.getByRole('region', { name: 'Brute force' });
  await expect(brute.getByText('Brute force did not compile', { exact: true })).toBeVisible();
  await expect(brute.locator('pre').filter({ hasText: "SyntaxError: '(' was never closed" })).toBeVisible();
  await brute.getByRole('link', { name: 'Go to brute force' }).click();
  await expect(page.locator('#brute-source')).toBeFocused();
  // Only failing lines are red.
  const passColour = await reference.locator('.line.tone-ok').first().evaluate((el) => getComputedStyle(el).color);
  const inkColour = await page.locator('h1').evaluate((el) => getComputedStyle(el).color);
  expect(passColour).toBe(inkColour);

  await page.getByText('All test results').click();
  await expect(page.getByRole('cell', { name: 'Wrong answer' })).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).click();
  await expect(page.getByText('Waiting for the judge (2nd in queue)')).toBeVisible();
  await expect(page.locator('.headline')).toHaveText('Validation failed', { timeout: 10_000 });
  await expect(page.getByText('Wrong answer on test 7')).toBeVisible();
});

test('validation is refused while a test is in no subtask', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  // The readiness panel names what blocks validation first, then the advice (P3 A1).
  const blockers = page.getByRole('group', { name: 'Fix this before you validate' });
  await expect(blockers).toContainText('Test 7 is in no subtask. Add it to a subtask, or mark it as a sample or an open test.');
  await expect(page.getByRole('note', { name: 'Before you validate' })).toContainText('No brute force');
  // While something blocks validation, Validate is not the primary action.
  await expect(page.getByRole('button', { name: 'Validate' })).toHaveClass(/btn-secondary/);
  // Named outside the table, which stays closed until asked for.
  await expect(page.getByText('7 tests: 1 sample, 5 in 2 subtasks, 1 in no subtask.')).toBeVisible();
  await expect(page.getByText('Test 7 is in no subtask. Add it to a subtask range, to the samples or to the open tests.')).toBeVisible();
  await page.getByText('Show all 7 tests').click();
  await expect(page.getByRole('row', { name: /^7 Not in a subtask/ })).toBeVisible();

  // Groups follow the subtasks as typed, before saving.
  await page.locator('#subtask-1-tests').fill('5-7');
  await expect(page.getByRole('row', { name: /^7 Subtask 2/ })).toBeVisible();
  await expect(page.getByText('Test 7 is in no subtask. Add it to a subtask range, to the samples or to the open tests.')).toHaveCount(0);
  await page.locator('#subtask-1-tests').fill('5-6');
  // An open test belongs to no subtask and is not an error (D115).
  await page.locator('#open').fill('7');
  await expect(page.getByRole('row', { name: /^7 Open test/ })).toBeVisible();
  await expect(page.locator('#tests-heading ~ .field-error')).toHaveCount(0);
  await expect(page.getByText('7 tests: 1 sample, 1 open test, 5 in 2 subtasks.')).toBeVisible();
  await page.locator('#open').fill('');
  await expect(page.getByLabel('Allowed error')).toBeVisible();

  await page.getByRole('button', { name: 'Validate' }).click();
  const alert = page.getByRole('alert').filter({ hasText: 'This problem cannot be validated yet.' });
  await expect(alert).toContainText('Test 7 is in no subtask.');
  await expect(page.getByText('Not validated yet.')).toBeVisible();
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
  await expect(page.locator('.toasts').getByText('Imported Pair with sum. Validating.', { exact: true })).toBeVisible();
  await expect(page).toHaveURL('/admin/problems');
  const row = page.getByRole('row', { name: /Pair with sum/ });
  await expect(row.locator('.badge')).toHaveText('Validating');
  await expect(row.locator('.badge')).toHaveText('Validated', { timeout: 15_000 });
  expect(state.imports).toEqual(['bad pack', 'good pack']);
  await row.getByRole('link', { name: 'Pair with sum', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pair with sum');
});

test('several packs import in one pick, and a row offers Validate when it is not validated (P3 O6)', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('row', { name: /Interval covering/ }).locator('.badge')).toHaveText('Not validated');
  await page.locator('input[type="file"]').setInputFiles([zipFile('one.zip', 'good pack 1'), zipFile('two.zip', 'good pack 2')]);
  await expect.poll(() => state.imports).toEqual(['good pack 1', 'good pack 2']);
  await expect(page.locator('.toasts').getByText('Imported Pair with sum 1. Validating. Imported Pair with sum 2. Validating.', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /Pair with sum 1/ })).toHaveCount(1);
  await expect(page.getByRole('row', { name: /Pair with sum 2/ })).toHaveCount(1);
  // Interval covering has a test in no subtask, so validating it is refused with the reason.
  await page.getByRole('button', { name: 'Validate Interval covering' }).click();
  await expect(page.getByText('Interval covering cannot be validated yet: Test 7 is in no subtask.', { exact: false }).first()).toBeVisible();
});

test('the bank shows status words, archived problems on request, and creates problems', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems');
  await expect(page.getByRole('row', { name: /A plus B/ })).toContainText('Validated');
  await expect(page.getByRole('row', { name: /Interval covering/ })).toContainText('Not validated');

  await page.getByLabel('Show archived').check();
  await expect(page).toHaveURL('/admin/problems?archived=1');
  await expect(page.getByRole('row', { name: /Old sorting/ })).toContainText('Archived');

  await page.getByRole('button', { name: 'New problem' }).click();
  const title = page.getByLabel('Title', { exact: true });
  await expect(title).toBeFocused();
  await title.fill('Interval Covering');
  await expect(page.getByLabel('Slug', { exact: true })).toHaveValue('interval-covering');
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page.locator('#new-slug-error')).toHaveText('Another problem already uses the slug interval-covering.');

  await page.getByLabel('Slug', { exact: true }).fill('interval-covering-2');
  await title.fill('Interval Covering II');
  await expect(page.getByLabel('Slug', { exact: true })).toHaveValue('interval-covering-2');
  await page.getByRole('button', { name: 'Create problem' }).click();
  await expect(page).toHaveURL('/admin/problems/10');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Interval Covering II');
  await expect(page.getByText('No tests yet.')).toBeVisible();
});

test('archiving asks first and returns to the bank', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'Archive problem' }).click();
  const dialog = page.getByRole('dialog', { name: 'Archive A plus B?' });
  await expect(dialog).toContainText('It leaves the problem bank. Contests that used it keep it.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  expect(state.archived).toEqual([]);

  await page.getByRole('button', { name: 'Archive problem' }).click();
  await dialog.getByRole('button', { name: 'Archive problem' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByText('Archived', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toHaveCount(0);
  expect(state.archived).toEqual([1]);
});

test('leaving with unsaved changes asks first', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  await page.locator('#title').fill('A plus B, edited');
  await expect(page.locator('.statement').getByText('Given two integers')).toBeVisible();
  await expect(page.locator('.preview-title')).toHaveText('A plus B, edited');

  await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Problem bank' }).click();
  const dialog = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page).toHaveURL('/admin/problems/1');
  await expect(page.locator('#title')).toHaveValue('A plus B, edited');

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Problem bank', exact: true }).click();
  await dialog.getByRole('button', { name: 'Leave page' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Problem bank');
});

test('replacing tests uploads a zip and lists the new tests', async ({ page }) => {
  await mockApi(page);
  const choosers = watchFileChoosers(page);
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'Replace tests' }).click();
  // Replacing every test asks first; the file picker opens from the confirm.
  const confirm = page.getByRole('dialog', { name: /^Replace all \d+ tests\?$/ });
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirm.getByRole('button', { name: 'Replace tests' }).click();
  await (await choosers.next()).setFiles(zipFile('tests.zip', 'zip'));
  await expect(page.getByText('Replaced tests. 2 tests stored.')).toBeVisible();
  await page.getByText('Show all 2 tests').click();
  // Groups come from the sample and subtask lists as they stand (samples 1, 2).
  await expect(page.getByRole('row', { name: /^2 Sample/ })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Download output of test 2' })).toHaveAttribute(
    'href',
    '/api/admin/problems/1/tests/2/output',
  );
});

test('statements are sanitised and maths is rendered', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const statement = page.getByLabel('Statement', { exact: true });
  await statement.fill(
    '# Heading\n\nHi <img src=x onerror="window.__pwned=1"> [link](javascript:window.__pwned=2) and $x^2$.\n\n![figure](/fig.png)',
  );
  const preview = page.locator('.statement');
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
  await expect(page.getByRole('link', { name: 'Problem bank', exact: true })).toHaveCount(0);
});

test('admins see the Problem bank link and it marks the current page', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/1');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await nav.getByRole('link', { name: 'Problem bank', exact: true }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(nav.getByRole('link', { name: 'Problem bank', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await page.getByRole('link', { name: 'A plus B', exact: true }).click();
  await expect(nav.getByRole('link', { name: 'Problem bank', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('link', { name: 'People' })).toHaveCount(0);
});

test('missing outputs are named and generated from the reference', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/5');
  await expect(page.getByText('2 tests have no expected output. Generate outputs to fill them from the reference solution.')).toBeVisible();
  await page.getByText('Show all 5 tests').click();
  await expect(page.getByRole('row', { name: /^4 Subtask 1/ })).toContainText('Output missing');
  await expect(page.getByRole('link', { name: 'Download output of test 4' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('Generating outputs');
  expect(state.generates).toEqual([{ all: false }]);
  await expect(page.locator('.gen-line')).toHaveText('Generated 2 outputs. Slowest run 220 ms.', { timeout: 10_000 });
  await expect(page.getByText('Output missing')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Generate outputs' })).toHaveCount(0);
  await page.screenshot({ path: `${shots()}problem-editor-generated-1440-light.png`, fullPage: true });

  // Regenerating everything asks first and uses the same word.
  await page.getByRole('button', { name: 'Regenerate all outputs' }).click();
  const dialog = page.getByRole('dialog', { name: 'Regenerate all outputs?' });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await dialog.getByRole('button', { name: 'Regenerate all outputs' }).click();
  await expect.poll(() => state.generates.at(-1)).toEqual({ all: true });
  await expect(page.locator('.gen-line')).toHaveText('Generated 5 outputs. Slowest run 220 ms.', { timeout: 10_000 });
});

test('a refused generation shows the reason in place', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/4');
  await page.getByRole('button', { name: 'Regenerate all outputs' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Regenerate all outputs' }).click();
  // Shown in the polite region under the buttons, read once (not also an alert).
  const reason = page.locator('.generation[aria-live="polite"] .banner-error').filter({ hasText: 'The problem is not ready. Fix these first.' });
  await expect(reason).toContainText('Add a reference solution first.');
});

test('a reference that does not compile, or a problem changed mid-run, is said plainly', async ({ page }) => {
  const state = await mockApi(page);
  state.genOutcome = 'compile';
  await page.goto('/admin/problems/5');
  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('The reference solution did not compile, so no outputs were generated.', {
    timeout: 10_000,
  });
  await expect(page.locator('.gen-failures')).toContainText('expected ; before return');

  state.genOutcome = 'stale';
  await page.getByRole('button', { name: 'Generate outputs' }).click();
  await expect(page.locator('.gen-line')).toHaveText('Some outputs were not applied.', { timeout: 10_000 });
  await expect(page.locator('.gen-failures')).toContainText('The problem changed while outputs were generated.');
});

test('the sticky bar keeps status, field errors and the main actions in reach', async ({ page }) => {
  const state = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('link', { name: 'Try as a student' })).toHaveAttribute('href', '/admin/problems/1/try');
  const validate = page.getByRole('button', { name: 'Validate', exact: true });
  await expect(validate).toHaveClass(/btn-secondary/);

  await page.locator('#title').fill('A plus B, edited');
  await expect(page.getByRole('button', { name: 'Save and validate' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes' })).toHaveClass(/btn-primary/);

  // The bar stays at the top while the page scrolls.
  await page.getByRole('link', { name: /^Solutions/ }).click();
  await page.mouse.wheel(0, 2000);
  const bar = await page.locator('.action-bar').boundingBox();
  expect(bar!.y).toBeLessThanOrEqual(1);
  await page.screenshot({ path: `${shots()}problem-editor-sticky-1440-light.png` });

  // Ctrl+S saves.
  await page.keyboard.press('Control+s');
  await expect(page.locator('.toasts').getByText('Saved', { exact: true })).toBeVisible();
  expect(state.puts).toHaveLength(1);

  // Field errors: a count in the bar and a list of links that focus each field.
  await page.locator('#slug').fill('taken');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('.error-count')).toHaveText('2 fields need attention');
  const list = page.locator('#field-errors');
  await list.getByRole('link', { name: 'Subtask 2 points: Points run from 1 to 1000.' }).click();
  await expect(page.locator('#subtask-1-points')).toBeFocused();
  await list.getByRole('link', { name: 'Slug: Another problem already uses the slug taken.' }).click();
  await expect(page.locator('#slug')).toBeFocused();
});

test('a problem with no tests offers Upload tests', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/3');
  await expect(page.getByRole('button', { name: 'Upload tests' })).toBeVisible();
  await expect(page.getByText('No tests yet. Upload a .zip of numbered input and output files to add them.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Regenerate all outputs' })).toHaveCount(0);
  await expect(page.getByText('No validator. Add one to check every test input against the constraints.')).toBeVisible();
});

test('reference and brute force sit side by side at 1440 px', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const ref = await page.getByRole('group', { name: 'Reference solution' }).boundingBox();
  const brute = await page.getByRole('group', { name: 'Brute force' }).boundingBox();
  expect(Math.abs(ref!.y - brute!.y)).toBeLessThan(2);
  expect(brute!.x).toBeGreaterThan(ref!.x + ref!.width - 1);
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
  const title = await row.locator('.title').boundingBox();
  const status = await row.locator('.state').boundingBox();
  const tests = await row.locator('.tests').boundingBox();
  expect(Math.abs(title!.y - status!.y)).toBeLessThan(6);
  expect(tests!.y).toBeGreaterThan(title!.y + title!.height - 1);
  await expect(row.locator('.tags')).toBeHidden();
});

test('keyboard only: an organiser opens a problem, edits it, saves with Ctrl+S and validates', async ({ page }) => {
  const state = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems');
  await expect(page.getByRole('row', { name: /A plus B/ })).toBeVisible();

  await activate(page, page.getByRole('link', { name: 'A plus B', exact: true }));
  await expect(page).toHaveURL('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  // The statement is a plain text area: Tab moves on from it, never traps.
  const statement = page.getByLabel('Statement', { exact: true });
  await tabTo(page, statement);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n\nAll numbers fit in 64 bits.');
  await page.keyboard.press('Tab');
  expect(await hasFocus(statement)).toBe(false);
  await expect(page.getByText('Unsaved changes', { exact: true })).toBeVisible();

  await statement.focus();
  await page.keyboard.press('Control+s');
  await expect(page.locator('.toasts').getByText('Saved', { exact: true })).toBeVisible();
  expect(String(state.puts.at(-1)?.statement_md)).toContain('All numbers fit in 64 bits.');

  await activate(page, page.getByRole('button', { name: 'Validate' }));
  await expect(page.locator('.headline')).toHaveText('Validation passed', { timeout: 15_000 });

  // The shortcut list names the editor keys for organisers.
  await page.locator('main h1').focus();
  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(keys.getByRole('heading', { name: 'Problem editor' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
});

test('a finished validation says its result in the bar and the report link takes focus (P3 O2)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const readiness = page.getByRole('region', { name: /^Readiness/ });
  await expect(readiness).toContainText('Validated on this version.');
  await expect(readiness.getByRole('link', { name: 'Choose a contest' })).toHaveAttribute('href', '/admin/contests');

  // An unsaved judged change says it will need a new validation; a title change does not.
  await page.locator('#title').fill('A plus B again');
  await expect(readiness).toContainText('Validated on this version. Your unsaved changes do not affect it.');
  await page.locator('#time_limit_ms').fill('2000');
  await expect(readiness).toContainText('Saving needs a new validation: you changed the limits.');
  await page.locator('#time_limit_ms').fill('1000');
  await page.locator('#title').fill('A plus B');

  await page.getByRole('button', { name: 'Validate' }).click();
  const bar = page.locator('.bar-result');
  await expect(bar).toHaveText(/Validation passed: 5 of 5 tests, slowest 212 ms\.\s*See the report/, { timeout: 15_000 });
  await bar.getByRole('link', { name: 'See the report' }).click();
  await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeFocused();
});

test('removing the brute force or validator can be undone until the next save (P3 A6)', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/2');
  await page.getByRole('button', { name: 'Remove validator' }).click();
  await expect(page.getByText('Validator removed. It goes for good when you save.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Undo remove' })).toBeFocused();
  await page.getByRole('button', { name: 'Undo remove' }).click();
  await expect(page.locator('#validator-source')).toBeFocused();
  await expect(page.getByText('Unsaved changes', { exact: true })).toHaveCount(0);
});

test('the preview shows the samples as students see them (P3 A5)', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/problems/1');
  const preview = page.getByRole('region', { name: 'Preview' });
  await expect(preview.getByRole('heading', { name: 'Sample 1' })).toBeVisible();
  await expect(preview.getByRole('heading', { name: 'Sample 2' })).toBeVisible();
  await expect(preview.getByRole('button', { name: 'Copy sample 1 input' })).toBeVisible();
  // The samples follow the field as typed.
  await page.locator('#samples').fill('1');
  await expect(preview.getByRole('heading', { name: 'Sample 2' })).toHaveCount(0);
});

test('open tests are their own kind: no subtask error, editable, saved (D115, C2-001)', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/problems/4');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Average speed');
  const open = page.getByLabel('Open tests (optional)');
  await expect(open).toHaveValue('7');
  await expect(page.getByText('15 tests: 1 sample, 1 open test, 13 in 1 subtask.')).toBeVisible();
  await expect(page.getByText('in no subtask', { exact: false })).toHaveCount(0);
  await page.getByText('Show all 15 tests').click();
  await expect(page.getByRole('row', { name: /^7 Open test/ })).toBeVisible();

  // Changing them is a judged change, like the samples.
  await open.fill('7, 8');
  await page.locator('#subtask-0-tests').fill('2-6, 9-15');
  await expect(page.getByRole('region', { name: 'Readiness' })).toContainText('open tests and subtasks');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  expect(state.puts.at(-1)?.open).toEqual([7, 8]);

  // The server's refusal lands on the field.
  await open.fill('1, 7');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#open-error')).toHaveText('Test 1 is a sample and an open test. Put each test in one place.');
  await expect(open).toHaveAttribute('aria-invalid', 'true');
});
