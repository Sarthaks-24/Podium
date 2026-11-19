// The problem list of the contest editor (v2.1.0 stage V): the save queue,
// the problem bank picker and the letter field. Answered by the mocks of
// mocks/contests.ts.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { apiError, json, mockApi, shots, THEMES, WIDTHS } from './mocks/contests';

test('a quick Add then Move sends the second save only after the first has been answered', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  let firstReleased = false;
  const secondSawRelease: boolean[] = [];
  const puts: { problem_id: number; label: string }[][] = [];
  const loaded: number[][] = [];
  await page.route('**/api/admin/contests/4/problems', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    const body = route.request().postDataJSON() as { problems: { problem_id: number; label: string }[]; loaded_problem_ids: number[] };
    puts.push(body.problems);
    loaded.push(body.loaded_problem_ids);
    if (puts.length === 1) await held;
    else secondSawRelease.push(firstReleased);
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await page.getByRole('button', { name: 'Move Interval covering up' }).click();

  // The first answer is held; the Move's request must arrive only after it is let go.
  firstReleased = true;
  release();
  await expect.poll(() => puts.length).toBe(2);
  expect(secondSawRelease).toEqual([true]);
  expect(puts[0].map((p) => p.label)).toEqual(['A', 'B']);
  // The editor always names the problems it last loaded or saved: the first save the loaded list, the next the one just saved.
  expect(loaded[0]).toEqual([11]);
  expect(loaded[1]).toEqual(puts[0].map((p) => p.problem_id));
  expect(puts[1].map((p) => `${p.label}:${p.problem_id}`)).toEqual([`A:${puts[0][1].problem_id}`, `B:${puts[0][0].problem_id}`]);
});

test('a failed load of the problem bank says so and Retry loads it', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let failing = true;
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/admin/problems', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    if (failing) return apiError(route, 500, 'internal', 'Something went wrong on our side.');
    await held;
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  const alert = page.getByRole('alert').filter({ hasText: 'The problem bank did not load' });
  await expect(alert).toBeVisible();
  failing = false;
  await alert.getByRole('button', { name: 'Retry' }).click();
  // While the second request is held the bank is unknown: no claim about it.
  await expect(alert).toHaveCount(0);
  await expect(page.getByText('The problem bank is empty.')).toHaveCount(0);
  await expect(page.getByText('Every validated problem is in this contest.')).toHaveCount(0);
  release();
  await expect(page.getByLabel('Add a validated problem').getByRole('option', { name: /Interval covering/ })).toHaveCount(1);
});

test('before the bank has answered the page claims nothing about it', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/admin/problems', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    await held;
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
  await expect(page.getByText('The problem bank is empty.')).toHaveCount(0);
  await expect(page.getByText('Every validated problem is in this contest.')).toHaveCount(0);
  release();
  await expect(page.getByLabel('Add a validated problem').getByRole('option', { name: /Interval covering/ })).toHaveCount(1);
});

test('an empty problem bank gets a hint, not a bare picker', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.route('**/api/admin/problems', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return json(route, 200, { problems: [] });
  });
  await page.goto('/admin/contests/4');
  await expect(page.getByText('The problem bank is empty.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Go to the problem bank' })).toBeVisible();
});

/** A bank of three validated problems, so a contest can hold three. */
async function bankWithThree(page: Page) {
  await page.route('**/api/admin/problems', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const problems = [
      [11, 'a-plus-b', 'A plus B'],
      [12, 'interval-covering', 'Interval covering'],
      [13, 'shortest-detour', 'Shortest detour'],
    ].map(([id, slug, title]) => ({ id, slug, title, difficulty: 'easy', tags: [], origin: 'import', archived: false, test_count: 6, points: 100, validated: true, validated_at: 1, updated_at: 1, shown_in_practice: false }));
    return json(route, 200, { problems });
  });
}

async function addTwo(page: Page) {
  for (const title of ['Interval covering', 'Shortest detour']) {
    await page.getByLabel('Add a validated problem').selectOption({ label: `${title} (100 points)` });
    await page.getByRole('button', { name: 'Add problem' }).click();
    await expect(page.getByLabel(`Letter for ${title}`)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add problem' })).toBeDisabled();
  }
}

test('only the input with the bad letter is marked, the others are not', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await bankWithThree(page);
  await page.goto('/admin/contests/4');
  await addTwo(page);
  const [a, b, c] = ['A plus B', 'Interval covering', 'Shortest detour'].map((t) => page.getByLabel(`Letter for ${t}`));
  await b.fill('1');
  await b.press('Tab');
  await expect(b).toHaveAttribute('aria-invalid', 'true');
  await expect(b).toHaveAttribute('aria-describedby', 'problems-error');
  for (const ok of [a, c]) {
    await expect(ok).not.toHaveAttribute('aria-invalid', /.*/);
    await expect(ok).not.toHaveAttribute('aria-describedby', /.*/);
  }
});

test('two problems sharing a letter are both marked, the third is not', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await bankWithThree(page);
  await page.goto('/admin/contests/4');
  await addTwo(page);
  const [a, b, c] = ['A plus B', 'Interval covering', 'Shortest detour'].map((t) => page.getByLabel(`Letter for ${t}`));
  await c.fill('b');
  await c.press('Tab');
  await expect(page.getByText('Each problem needs its own letter.')).toBeVisible();
  await expect(b).toHaveAttribute('aria-invalid', 'true');
  await expect(c).toHaveAttribute('aria-invalid', 'true');
  await expect(a).not.toHaveAttribute('aria-invalid', /.*/);
});

test('a bad problem letter is tied to its input and the input is marked invalid', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  await page.goto('/admin/contests/4');
  const letter = page.getByLabel('Letter for A plus B');
  await letter.fill('1');
  await letter.press('Tab');
  const message = page.getByText('Each letter must be one to three capital letters.');
  await expect(message).toBeVisible();
  await expect(letter).toHaveAttribute('aria-invalid', 'true');
  await expect(letter).toHaveAccessibleDescription('Each letter must be one to three capital letters.');
});

const VIEWS = {
  'contest-bank-error': async (page: Page) => {
    await page.route('**/api/admin/problems', (route) => (route.request().method() === 'GET' ? apiError(route, 500, 'internal', 'Something went wrong on our side.') : route.fallback()));
  },
  'contest-bank-empty': async (page: Page) => {
    await page.route('**/api/admin/problems', (route) => (route.request().method() === 'GET' ? json(route, 200, { problems: [] }) : route.fallback()));
  },
  'contest-letter-error': async () => {},
} as const;

for (const theme of THEMES) {
  for (const [name, prepare] of Object.entries(VIEWS)) {
    test(`${name} holds at every width, ${theme} theme, axe clean`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await mockApi(page, { who: 'organiser', phase: 'upcoming' });
      await prepare(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/contests/4');
      await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');
      if (name === 'contest-letter-error') {
        await page.getByLabel('Letter for A plus B').fill('1');
        await page.getByLabel('Letter for A plus B').press('Tab');
        await expect(page.getByLabel('Letter for A plus B')).toHaveAttribute('aria-invalid', 'true');
      } else {
        await expect(page.locator('#add-problem')).toBeVisible();
        await expect(name === 'contest-bank-error' ? page.getByText('The problem bank did not load') : page.getByText('The problem bank is empty.')).toBeVisible();
      }
      const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`)).toEqual([]);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
        await page.locator('#add-problem').scrollIntoViewIfNeeded();
        const dir = process.env.PODIUM_SHOTS ?? shots();
        mkdirSync(dir, { recursive: true });
        await page.screenshot({ path: `${dir}${name}-${width}-${theme}.png`, fullPage: true });
      }
    });
  }
}

test('a save refused because problems were added keeps the list on screen and Reload fetches theirs', async ({ page }) => {
  await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  const message = 'Problems were added to this contest after you opened it. Reload to see them, then make your change again.';
  let refuse = true;
  const bodies: { problems: { problem_id: number; label: string }[]; loaded_problem_ids: number[] }[] = [];
  await page.route('**/api/admin/contests/4/problems', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    bodies.push(route.request().postDataJSON());
    if (refuse) return apiError(route, 409, 'problems_changed', message);
    return route.fallback();
  });
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  const alert = page.getByRole('alert').filter({ hasText: message });
  await expect(alert).toBeVisible();
  // What the organiser arranged stays, and nothing was saved.
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  expect(bodies[0].loaded_problem_ids).toEqual([11]);
  refuse = false;
  await alert.getByRole('button', { name: 'Reload' }).click();
  await expect(alert).toHaveCount(0);
  // The reload shows the saved list; the same change now goes through with the new loaded list.
  await expect(page.getByLabel('Letter for Interval covering')).toHaveCount(0);
  await page.getByLabel('Add a validated problem').selectOption({ label: 'Interval covering (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await expect(page.getByLabel('Letter for Interval covering')).toHaveValue('B');
  expect(bodies).toHaveLength(2);
  expect(bodies[1].loaded_problem_ids).toEqual([11]);
});

test('an empty contest still sends loaded_problem_ids as an empty list', async ({ page }) => {
  const api = await mockApi(page, { who: 'organiser', phase: 'upcoming' });
  api.adminProblems.length = 0;
  await page.goto('/admin/contests/4');
  await page.getByLabel('Add a validated problem').selectOption({ label: 'A plus B (100 points)' });
  await page.getByRole('button', { name: 'Add problem' }).click();
  await expect.poll(() => api.problemBodies.length).toBe(1);
  expect((api.problemBodies[0] as { loaded_problem_ids: number[] }).loaded_problem_ids).toEqual([]);
});
