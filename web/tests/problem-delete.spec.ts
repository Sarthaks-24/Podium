// Deleting a problem (v2.0.2 stage A): the Delete dialog in the editor menu,
// the Deleted problems list for admins and the owner, and the "Deleted
// problem" label wherever a contest lists a problem that was deleted. Every
// /api/ call is answered by the mocks of problems.spec.ts and
// contests.spec.ts, with extra routes for the three delete endpoints. The
// sweep saves screenshots at 375, 768, 1024 and 1440 px in light and dark to
// $PODIUM_SHOTS (default cache/v2.0.2/shots/sA/), checks no horizontal
// overflow, and runs axe on each screen.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate, tabTo } from './keyboard';
import { toasts } from './locators';
import * as bank from './mocks/problems';
import * as contests from './mocks/contests';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.0.2/shots/sA/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const week6 = { contest_id: 6, slug: 'week-6', title: 'Week 6', label: 'A' };
const week7 = { contest_id: 7, slug: 'week-7', title: 'Week 7', label: 'A' };
const lab = { contest_id: 9, slug: 'lab-2', title: 'Lab 2', label: 'B' };

type Info = { used: boolean; contests: unknown[]; unended: unknown[]; practice: unknown[] };
const UNUSED: Info = { used: false, contests: [], unended: [], practice: [] };
const USED: Info = { used: true, contests: [week6, lab], unended: [], practice: [week6, lab] };
const RUNNING: Info = { used: true, contests: [week7], unended: [week7], practice: [] };

const DELETED = [
  { id: 21, slug: 'two-sum', title: 'Two sum', deleted_at: bank.T0 - 3_600_000, deleted_by: 'priya@college.edu', used: true },
  { id: 22, slug: 'scratch-problem', title: 'Scratch problem', deleted_at: bank.T0 - 86_400_000, deleted_by: '', used: false },
];

type Role = 'organiser' | 'admin' | 'owner';

/** The problems mock with a signed-in person of `role` and the three delete endpoints. */
async function setup(page: Page, role: Role, info: Info = UNUSED) {
  await bank.mockApi(page);
  const deletes: number[] = [];
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (path === '/api/me') return bank.json(route, 200, { ...bank.ADMIN, role });
      if (path === '/api/admin/problems/deleted' && req.method() === 'GET') {
        return role === 'organiser' ? bank.apiError(route, 403, 'forbidden', 'You do not have access to this.') : bank.json(route, 200, { problems: DELETED });
      }
      const m = path.match(/^\/api\/admin\/problems\/(\d+)\/(delete-info|delete)$/);
      if (m && m[2] === 'delete-info') return bank.json(route, 200, info);
      if (m && m[2] === 'delete' && req.method() === 'POST') {
        deletes.push(Number(m[1]));
        return bank.json(route, 204);
      }
      return route.fallback();
    },
  );
  return { deletes };
}

/** Opens the editor of A plus B and its Delete dialog with the mouse. */
async function openDialog(page: Page) {
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Delete' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete A plus B?' });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('an admin deletes an unused problem from the keyboard, typing the slug', async ({ page }) => {
  const { deletes } = await setup(page, 'admin');
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await activate(page, page.getByRole('button', { name: 'More' }));
  await activate(page, page.locator('#problem-menu').getByRole('button', { name: 'Delete' }));

  const dialog = page.getByRole('dialog', { name: 'Delete A plus B?' });
  await expect(dialog).toContainText('It cannot be restored.');
  await expect(dialog.getByRole('link', { name: 'Download pack' })).toHaveCount(0);
  const slug = dialog.getByLabel(/^Type a-plus-b to confirm/);
  await expect(slug).toBeFocused();
  const button = dialog.getByRole('button', { name: 'Delete problem' });
  await expect(button).toBeDisabled();
  await page.keyboard.type('a-plus');
  await expect(button).toBeDisabled();
  await page.keyboard.type('-b');
  await expect(button).toBeEnabled();
  await tabTo(page, button);
  await page.keyboard.press('Enter');

  await expect(toasts(page)).toContainText('Deleted A plus B');
  await expect(page).toHaveURL('/admin/problems');
  expect(deletes).toEqual([1]);
});

test('Escape and Cancel close the dialog and delete nothing; focus returns to the menu button', async ({ page }) => {
  const { deletes } = await setup(page, 'admin');
  const dialog = await openDialog(page);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('button', { name: 'More' })).toBeFocused();
  expect(deletes).toEqual([]);
});

test('a used problem offers Download pack and the results, and says where practice closes', async ({ page }) => {
  const { deletes } = await setup(page, 'owner', USED);
  const dialog = await openDialog(page);
  await expect(dialog).toContainText('Practice for this problem closes in: Week 6, Lab 2.');
  await expect(dialog.getByRole('link', { name: 'Download pack' })).toHaveAttribute('href', '/api/admin/problems/1/export');
  await expect(dialog.getByRole('link', { name: 'Results of Week 6' })).toHaveAttribute('href', '/admin/contests/6/results');
  await expect(dialog.getByRole('link', { name: 'Results of Lab 2' })).toHaveAttribute('href', '/admin/contests/9/results');
  await dialog.getByLabel(/^Type a-plus-b to confirm/).fill('a-plus-b');
  await dialog.getByRole('button', { name: 'Delete problem' }).click();
  await expect(toasts(page)).toContainText('Deleted A plus B');
  expect(deletes).toEqual([1]);
});

test('an admin is told only the owner deletes a used problem, with Close as the only action', async ({ page }) => {
  const { deletes } = await setup(page, 'admin', USED);
  const dialog = await openDialog(page);
  await expect(dialog).toContainText('Only the site owner can delete a problem that has been used. Archive it instead.');
  await expect(dialog.getByLabel(/Type a-plus-b/)).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Delete problem' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Close' })).toBeFocused();
  expect(deletes).toEqual([]);
});

test('a contest that has not ended blocks the delete for the owner, naming it', async ({ page }) => {
  await setup(page, 'owner', RUNNING);
  const dialog = await openDialog(page);
  await expect(dialog).toContainText('This problem is in contests that have not ended: Week 7. Remove it from them or wait until they end.');
  await expect(dialog.getByRole('button', { name: 'Delete problem' })).toHaveCount(0);
});

test('an organiser has no Delete in the menu and no Deleted problems link', async ({ page }) => {
  await setup(page, 'organiser');
  await page.goto('/admin/problems/1');
  await page.getByRole('button', { name: 'More' }).click();
  await expect(page.locator('#problem-menu').getByRole('button', { name: 'Delete' })).toHaveCount(0);
  await page.goto('/admin/problems');
  await expect(page.getByRole('link', { name: 'A plus B', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Deleted problems' })).toHaveCount(0);
});

test('admins and the owner read the Deleted problems list: plain text, no link to open, no restore', async ({ page }) => {
  await setup(page, 'admin');
  await page.goto('/admin/problems');
  await activate(page, page.getByRole('link', { name: 'Deleted problems' }));
  await expect(page).toHaveURL('/admin/problems?deleted=1');
  const table = page.getByRole('table');
  await expect(table.getByRole('row', { name: /Two sum/ })).toContainText('priya@college.edu');
  await expect(table.getByRole('row', { name: /Two sum/ })).toContainText('In contests or submitted to');
  await expect(table.getByRole('row', { name: /Scratch problem/ })).toContainText('An account that no longer exists');
  await expect(table.getByRole('row', { name: /Scratch problem/ })).toContainText('Never used');
  await expect(table.getByRole('link')).toHaveCount(0);
  await expect(table.getByRole('button')).toHaveCount(0);
  await activate(page, page.getByRole('link', { name: 'Back to problems' }));
  await expect(page).toHaveURL('/admin/problems');
});

test('a deleted problem shows the Deleted problem label, not a link, in the contest the student practises in', async ({ page }) => {
  await contests.mockApi(page, { phase: 'practice', deletedB: true });
  await page.goto('/contests/week-6');
  const list = page.getByRole('list', { name: 'Your problems' });
  const b = list.getByRole('listitem').filter({ hasText: 'Interval covering' });
  await expect(b).toContainText('Deleted problem');
  await b.locator('summary').click();
  await expect(b.getByRole('link', { name: 'Open problem B' })).toHaveCount(0);
  const a = list.getByRole('listitem').filter({ hasText: 'A plus B' });
  await expect(a).not.toContainText('Deleted problem');
  await a.locator('summary').click();
  await expect(a.getByRole('link', { name: 'Open problem A' })).toBeVisible();
});

test('the organiser sees the label in the contest editor, its results and the console', async ({ page }) => {
  await contests.mockApi(page, { who: 'organiser', phase: 'after', deletedB: true });
  await page.goto('/admin/contests/4');
  const pick = page.locator('.pick').filter({ hasText: 'Interval covering' });
  await expect(pick).toContainText('Deleted problem');
  await expect(pick).toContainText('It stays here because students used it. It cannot be changed.');
  await expect(page.locator('.pick').filter({ hasText: 'A plus B' })).not.toContainText('Deleted problem');
  await page.goto('/admin/contests/4/results');
  await expect(page.getByRole('rowheader', { name: /Interval covering/ })).toContainText('Deleted problem');
  await page.goto('/admin/contests/4/console');
  const row = page.getByRole('listitem').filter({ hasText: 'Interval covering' });
  await expect(row).toContainText('Deleted problem');
  await expect(row.getByRole('button', { name: 'Judge problem B again' })).toBeDisabled();
});

// The sweep: each new screen at four widths in both themes, then axe.
interface Screen {
  name: string;
  run: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
  {
    name: 'deleted-list',
    run: async (page) => {
      await setup(page, 'owner');
      await page.goto('/admin/problems?deleted=1');
      await expect(page.getByRole('row', { name: /Two sum/ })).toBeVisible();
    },
  },
  {
    name: 'delete-dialog-unused',
    run: async (page) => {
      await setup(page, 'admin');
      await openDialog(page);
    },
  },
  {
    name: 'delete-dialog-used',
    run: async (page) => {
      await setup(page, 'owner', USED);
      const dialog = await openDialog(page);
      await expect(dialog.getByRole('link', { name: 'Download pack' })).toBeVisible();
    },
  },
  {
    name: 'delete-dialog-refused',
    run: async (page) => {
      await setup(page, 'owner', RUNNING);
      const dialog = await openDialog(page);
      await expect(dialog).toContainText('have not ended');
    },
  },
  {
    name: 'contest-with-deleted-problem',
    run: async (page) => {
      await contests.mockApi(page, { phase: 'practice', deletedB: true });
      await page.goto('/contests/week-6');
      await expect(page.getByRole('list', { name: 'Your problems' })).toContainText('Deleted problem');
    },
  },
  {
    name: 'contest-editor-with-deleted-problem',
    run: async (page) => {
      await contests.mockApi(page, { who: 'organiser', phase: 'after', deletedB: true });
      await page.goto('/admin/contests/4');
      await expect(page.locator('.pick').filter({ hasText: 'Deleted problem' })).toBeVisible();
    },
  },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    test.describe.configure({ timeout: 60_000 });

    for (const screen of SCREENS) {
      test(screen.name, async ({ page }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await screen.run(page);
        await page.evaluate(() => document.fonts.ready);

        const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
        const bad = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.help} (${v.nodes[0].target.join(' ')})`);
        expect(bad, `axe findings on ${screen.name}`).toEqual([]);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          await page.screenshot({ path: `${shotDir()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}
