// PF-10 (v2.0.2 stage 0): "Update students now" on an ended contest whose
// points would change shows the problems and the number of students in a
// second dialog, and only a second confirmation sends confirm_ended. Run
// with SHOTS_DIR set to save a screenshot of the second dialog at four
// widths in each theme.
import { expect, test } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import * as contests from './mocks/contests';
import { activate } from './keyboard';

const WIDTHS = [375, 768, 1024, 1440];

async function openSecondDialog(page: import('@playwright/test').Page, width: number) {
  const api = await contests.mockApi(page, { who: 'organiser', phase: 'after', updateAvailable: true, endedScoresChange: true });
  await page.setViewportSize({ width, height: 900 });
  await page.goto('/admin/contests/4/console#versions-title');
  await page.getByRole('button', { name: 'Update students now for problem A' }).first().click();
  const first = page.getByRole('dialog', { name: 'Update students now to the edited problem A?' });
  await first.getByRole('button', { name: 'Update students now' }).click();
  return api;
}

test('an ended contest asks a second time and shows what changes', async ({ page }) => {
  const api = await openSecondDialog(page, 1440);
  const second = page.getByRole('dialog', { name: 'Change the final scores of problem A?' });
  await expect(second).toBeVisible();
  await expect(second).toContainText('This contest has ended.');
  await expect(second).toContainText('Problem A: points per part 40, 60 become 30, 70 (total 100 to 100).');
  await expect(second).toContainText('final scores change for 12 students');
  // Nothing was sent with the confirmation yet, and focus starts on Cancel.
  expect(api.pinPuts).toHaveLength(0);
  expect(api.pinRefusals).toHaveLength(1);
  await expect(second.getByRole('button', { name: 'Cancel' })).toBeFocused();

  await second.getByRole('button', { name: 'Update students now' }).click();
  await expect.poll(() => api.pinPuts.length).toBe(1);
  expect(api.pinPuts[0].body.confirm_ended).toBe(true);
});

test('the second dialog says raised points need Judge again', async ({ page }) => {
  await openSecondDialog(page, 1440);
  const second = page.getByRole('dialog', { name: 'Change the final scores of problem A?' });
  await expect(second).toContainText('Points you raise show in scores only after you press "Judge again" for that problem. Points you lower apply at once.');
});

test('Cancel on the second dialog changes nothing', async ({ page }) => {
  const api = await openSecondDialog(page, 1440);
  const second = page.getByRole('dialog', { name: 'Change the final scores of problem A?' });
  await second.getByRole('button', { name: 'Cancel' }).click();
  await expect(second).toBeHidden();
  expect(api.pinPuts).toHaveLength(0);
});

test('the second confirmation works from the keyboard alone', async ({ page }) => {
  const api = await contests.mockApi(page, { who: 'organiser', phase: 'after', updateAvailable: true, endedScoresChange: true });
  await page.setViewportSize({ width: 1024, height: 800 });
  await page.goto('/admin/contests/4/console#versions-title');
  await activate(page, page.getByRole('button', { name: 'Update students now for problem A' }).first());
  const first = page.getByRole('dialog', { name: 'Update students now to the edited problem A?' });
  await expect(first).toBeVisible();
  await activate(page, first.getByRole('button', { name: 'Update students now' }));
  const second = page.getByRole('dialog', { name: 'Change the final scores of problem A?' });
  await expect(second).toBeVisible();
  // A stray Enter cancels, it never confirms: focus starts on Cancel.
  await expect(second.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(second.getByRole('button', { name: 'Update students now' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect.poll(() => api.pinPuts.length).toBe(1);
});

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const width of WIDTHS) {
      test(`second dialog at ${width} px`, async ({ page }) => {
        await openSecondDialog(page, width);
        const second = page.getByRole('dialog', { name: 'Change the final scores of problem A?' });
        await expect(second).toBeVisible();
        const box = await second.boundingBox();
        expect(box && box.x >= 0 && box.x + box.width <= width, 'the dialog fits the viewport').toBe(true);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow, 'horizontal scroll').toBeLessThanOrEqual(0);
        const dir = process.env.SHOTS_DIR;
        if (dir) {
          mkdirSync(dir, { recursive: true });
          await page.screenshot({ path: `${dir}/ended-repin-${width}-${theme}.png` });
        }
      });
    }
  });
}
