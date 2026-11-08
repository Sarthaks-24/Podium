// Problems to finish (v2.0.2 stage U): under each ended contest on /me, a
// plain list of the problems not finished, with a practice link, and a link
// to the editorial when it is published. Every /api/ call is answered by the
// mocks of screens.ts, with /api/me/contests replaced here. The sweep saves
// screenshots at 375, 768, 1024 and 1440 px in light and dark to
// $PODIUM_SHOTS (default cache/v2.0.2/shots/sU/), checks no horizontal
// overflow, and runs axe on each screen.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { tabTo } from './keyboard';
import * as screens from './mocks/screens';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.0.2/shots/sU/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const CONTESTS = [
  {
    slug: 'week-6',
    title: 'Week 6 contest',
    starts_at: 1792000000000,
    state: 'practice',
    score: 140,
    max_score: 300,
    place: 3,
    of: 41,
    editorial: true,
    to_finish: [
      { label: 'A', title: 'Pair with a given sum', score: 40, max: 100, can_practice: true },
      { label: 'C', title: 'Longest increasing run over a very long statement title that wraps on a phone', score: 0, max: 100, can_practice: true },
      { label: 'D', title: 'Average speed', score: 60, max: 100, can_practice: false },
    ],
  },
  {
    slug: 'week-5',
    title: 'Week 5 contest',
    starts_at: 1791400000000,
    state: 'ended',
    score: 100,
    max_score: 300,
    editorial: false,
    to_finish: [{ label: 'B', title: 'Sorted merge', score: 0, max: 100, can_practice: false }],
  },
  { slug: 'week-4', title: 'Week 4 contest', starts_at: 1790800000000, state: 'ended', score: 300, max_score: 300, place: 1, of: 40, editorial: false, to_finish: [] },
  { slug: 'week-3', title: 'Week 3 contest', starts_at: 1790200000000, state: 'live', score: 20, max_score: 300, editorial: false, to_finish: [] },
];

async function open(page: Page, contests: unknown[] = CONTESTS) {
  await screens.mockApi(page, 'student');
  await page.route('**/api/me/contests', (route) => route.fulfill({ json: { contests, server_now: Date.now() } }));
  await page.goto('/me');
}

test('each ended contest lists its unfinished problems, with practice links where practice is open', async ({ page }) => {
  await open(page);
  const week6 = page.getByRole('list', { name: 'Problems to finish in Week 6 contest' });
  await expect(week6.getByRole('listitem')).toHaveCount(3);
  await expect(week6.getByRole('link', { name: /^Practice A\. Pair with a given sum/ })).toHaveAttribute('href', '/contests/week-6/problems/A');
  await expect(week6.getByRole('listitem').nth(0)).toContainText('40 of 100');
  await expect(week6.getByRole('listitem').nth(1)).toContainText('0 of 100');
  // Practice closed for a changed problem: no link, and the reason.
  const closed = week6.getByRole('listitem').nth(2);
  await expect(closed.getByRole('link')).toHaveCount(0);
  await expect(closed).toContainText('Practice closed: this problem changed after the contest');
  // Practice off for the contest: the link only reads the statement.
  const week5 = page.getByRole('list', { name: 'Problems to finish in Week 5 contest' });
  await expect(week5.getByRole('link', { name: /^Read B\. Sorted merge/ })).toHaveAttribute('href', '/contests/week-5/problems/B');
  // A finished or still-running contest carries no list.
  await expect(page.getByRole('list', { name: /Week 4 contest/ })).toHaveCount(0);
  await expect(page.getByRole('list', { name: /Week 3 contest/ })).toHaveCount(0);
});

test('the editorial link shows only when the editorial is open', async ({ page }) => {
  await open(page);
  await expect(page.getByRole('link', { name: 'Read the editorial' })).toHaveCount(1);
  await expect(page.getByRole('link', { name: 'Read the editorial' })).toHaveAttribute('href', '/contests/week-6');
});

test('an older server without the new fields shows the rows as before', async ({ page }) => {
  await open(page, [{ slug: 'week-6', title: 'Week 6 contest', starts_at: 1792000000000, state: 'ended', score: 240, max_score: 300, place: 3, of: 41 }]);
  const rows = page.getByRole('region', { name: 'Your contests' }).getByRole('listitem');
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText('3rd of 41');
  await expect(page.getByText('Problems to finish')).toHaveCount(0);
});

test('keyboard only: Tab reaches a practice link and Enter opens the problem', async ({ page }) => {
  await open(page);
  const link = page.getByRole('link', { name: /^Practice A\. Pair with a given sum/ });
  await tabTo(page, link);
  await expect(link).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/contests\/week-6\/problems\/A$/);
});

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`/me with problems to finish at ${width} px, ${theme}: no sideways scroll, axe clean`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.setViewportSize({ width, height: 900 });
      await open(page);
      await expect(page.getByText('Problems to finish').first()).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
      await page.getByRole('region', { name: 'Your contests' }).scrollIntoViewIfNeeded();
      await page.screenshot({ path: `${shotDir()}/me-finish-${width}-${theme}.png`, fullPage: true });
      const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(results.violations, results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`).join('\n')).toEqual([]);
    });
  }
}
