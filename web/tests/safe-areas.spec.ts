// Safe areas (v4 U9): with viewport-fit=cover the page reaches under a notch
// and a home bar, so the header, the footer, the toasts and the sheets keep
// clear of them. Chromium emulates the insets over CDP; the screenshots go to
// cache/v4/shots/U9/ for a person to look at.
import { expect, test, type Page } from '@playwright/test';
import * as contests from './mocks/contests';

const INSETS = { top: 47, right: 0, bottom: 34, left: 0 };

async function notch(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setSafeAreaInsetsOverride' as never, { insets: INSETS } as never);
}

const shot = (name: string) => `${test.info().project.testDir}/../../cache/v4/shots/U9/${name}-375-light.png`;

test('at 375 px with a notch and a home bar, the header, footer and toasts stay clear of them', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await notch(page);
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Week 6/ }).first()).toBeVisible();
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute('content', /viewport-fit=cover/);

  const header = await page.getByRole('banner').boundingBox();
  expect(header!.y).toBeGreaterThanOrEqual(INSETS.top);
  await page.screenshot({ path: shot('home') });

  // Scrolled to the end, the footer stops above the home bar (boxes are viewport relative).
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  const footer = await page.getByRole('contentinfo').boundingBox();
  expect(footer!.y + footer!.height).toBeLessThanOrEqual(812 - INSETS.bottom + 1);
});

test('a sheet keeps its heading below the notch', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 375, height: 812 });
  await notch(page);
  await page.goto('/contests/week-6/problems/A');
  await page.keyboard.press('Shift+Slash');
  const help = page.getByRole('dialog');
  await expect(help).toBeVisible();
  const heading = await help.getByRole('heading').first().boundingBox();
  expect(heading!.y).toBeGreaterThanOrEqual(INSETS.top);
  await page.screenshot({ path: shot('help') });
});
