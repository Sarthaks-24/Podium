// Bring back an earlier version (v2.1.0 stage H): "Version history" in the
// editor's menu, the list with the button or the line that says why a
// version cannot be brought back, the confirm (focus on Cancel), the toast,
// and the server's refusals in the dialog. Every /api/ call is answered by
// the mocks of problems.spec.ts, with the two version routes added. The sweep
// saves screenshots at 375, 768, 1024 and 1440 px in light and dark to
// $PODIUM_SHOTS (default cache/v2.1.0/shots/H/), checks no horizontal
// overflow, and runs axe on each screen.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate, tabTo } from './keyboard';
import { toasts } from './locators';
import * as bank from './mocks/problems';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.1.0/shots/H/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const NOT_KEPT = "This version's files were not kept, so it cannot be brought back.";
// 3 Nov 2026, 14:20 in the test zone (Asia/Kolkata): 08:50 UTC.
const NOV3 = Date.UTC(2026, 10, 3, 8, 50);

const HISTORY = {
  total: 3,
  revisions: [
    { id: 30, rev: 3, created_at: bank.T0, created_by: 2, reason: 'Fewer tests', current: true, can_bring_back: true },
    { id: 20, rev: 2, created_at: NOV3, created_by: 2, reason: '', current: false, can_bring_back: true },
    { id: 10, rev: 1, created_at: NOV3 - 86_400_000, created_by: 0, reason: '', current: false, can_bring_back: false },
  ],
};

/** The problems mock plus the two version routes; `refusal` makes bring back answer a 409. */
async function setup(page: Page, refusal?: { code: string; message: string }) {
  await bank.mockApi(page);
  const brought: string[] = [];
  await page.route(
    (url) => /^\/api\/admin\/problems\/\d+\/revisions/.test(url.pathname),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (req.method() === 'GET') return bank.json(route, 200, HISTORY);
      brought.push(path);
      if (refusal) return bank.apiError(route, 409, refusal.code, refusal.message);
      return bank.json(route, 200, { problem_id: 1, revision_id: 31, rev: 4, validation_queued: true, not_ready: [] });
    },
  );
  return { brought };
}

async function openHistory(page: Page) {
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Version history' }).click();
  const panel = page.getByRole('region', { name: 'Version history' });
  await expect(panel).toBeVisible();
  return panel;
}

test('the list marks the current version and says why an old one cannot be brought back', async ({ page }) => {
  await setup(page);
  const panel = await openHistory(page);
  const items = panel.getByRole('listitem');
  await expect(items).toHaveCount(3);
  await expect(items.nth(0)).toContainText('Current version');
  await expect(items.nth(0)).toContainText('Fewer tests');
  await expect(items.nth(0).getByRole('button')).toHaveCount(0);
  await expect(items.nth(1)).toContainText('3 Nov 2026, 14:20');
  await expect(items.nth(1).getByRole('button', { name: 'Bring back this version' })).toBeVisible();
  await expect(items.nth(2)).toContainText(NOT_KEPT);
  await expect(items.nth(2).getByRole('button')).toHaveCount(0);
});

test('bringing back a version from the keyboard asks first, with focus on Cancel', async ({ page }) => {
  const { brought } = await setup(page);
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await activate(page, page.getByRole('button', { name: 'More' }));
  await activate(page, page.locator('#problem-menu').getByRole('button', { name: 'Version history' }));
  const panel = page.getByRole('region', { name: 'Version history' });
  await expect(page.locator('#history-heading')).toBeFocused();
  await tabTo(page, panel.getByRole('button', { name: 'Bring back this version' }));
  await page.keyboard.press('Enter');

  const dialog = page.getByRole('dialog', { name: 'Bring back the version from 3 Nov 2026, 14:20?' });
  await expect(dialog).toContainText('Tags, difficulty and the test plan stay as they are now.');
  await expect(dialog).toContainText('The problem must pass Validate again.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  expect(brought).toEqual([]);
  await tabTo(page, dialog.getByRole('button', { name: 'Bring back this version' }), { back: true });
  await page.keyboard.press('Enter');

  await expect(toasts(page)).toContainText('Brought back the version from 3 Nov 2026. Validation has started.');
  expect(brought).toEqual(['/api/admin/problems/1/revisions/20/bring-back']);
  await expect(panel).toBeHidden();
});

test('Escape and Cancel bring nothing back', async ({ page }) => {
  const { brought } = await setup(page);
  const panel = await openHistory(page);
  await panel.getByRole('button', { name: 'Bring back this version' }).click();
  const dialog = page.getByRole('dialog', { name: /^Bring back the version from/ });
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await panel.getByRole('button', { name: 'Bring back this version' }).click();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  expect(brought).toEqual([]);
});

test('a refusal is shown in the dialog in the server words and nothing changes', async ({ page }) => {
  const message = 'Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead.';
  await setup(page, { code: 'in_use', message });
  const panel = await openHistory(page);
  await panel.getByRole('button', { name: 'Bring back this version' }).click();
  const dialog = page.getByRole('dialog', { name: /^Bring back the version from/ });
  await dialog.getByRole('button', { name: 'Bring back this version' }).click();
  await expect(dialog.getByRole('alert')).toHaveText(message);
  await expect(toasts(page)).not.toContainText('Brought back');
  await expect(panel).toBeVisible();
});

test('Close returns focus to the menu button', async ({ page }) => {
  await setup(page);
  const panel = await openHistory(page);
  await panel.getByRole('button', { name: 'Close' }).click();
  await expect(panel).toBeHidden();
  await expect(page.getByRole('button', { name: 'More' })).toBeFocused();
});

interface Screen {
  name: string;
  run: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
  {
    name: 'history',
    run: async (page) => {
      await setup(page);
      await openHistory(page);
    },
  },
  {
    name: 'bring-back-confirm',
    run: async (page) => {
      await setup(page);
      const panel = await openHistory(page);
      await panel.getByRole('button', { name: 'Bring back this version' }).click();
      await expect(page.getByRole('dialog', { name: /^Bring back the version from/ })).toBeVisible();
    },
  },
  {
    name: 'bring-back-refused',
    run: async (page) => {
      await setup(page, { code: 'contest_running', message: 'A contest that is running uses this problem. Try again after it ends.' });
      const panel = await openHistory(page);
      await panel.getByRole('button', { name: 'Bring back this version' }).click();
      const dialog = page.getByRole('dialog', { name: /^Bring back the version from/ });
      await dialog.getByRole('button', { name: 'Bring back this version' }).click();
      await expect(dialog.getByRole('alert')).toBeVisible();
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
