// Full screen in the workspace (v4 E8). The Fullscreen API is replaced by a
// fake that records the calls, so the test does not depend on the browser
// allowing full screen in a headless run. An exam that requires full
// screen makes its own request on Start, so its workspace has no button.
import { expect, test, type Page } from '@playwright/test';
import * as contests from './mocks/contests';
import { codeEditor } from './locators';

async function fakeFullscreen(page: Page) {
  await page.addInitScript(() => {
    const w = window as unknown as { __fs: string[] };
    w.__fs = [];
    let element: Element | null = null;
    Object.defineProperty(document, 'fullscreenEnabled', { get: () => true });
    Object.defineProperty(document, 'fullscreenElement', { get: () => element });
    Element.prototype.requestFullscreen = function () {
      w.__fs.push('request');
      element = document.documentElement;
      document.dispatchEvent(new Event('fullscreenchange'));
      return Promise.resolve();
    };
    document.exitFullscreen = () => {
      w.__fs.push('exit');
      element = null;
      document.dispatchEvent(new Event('fullscreenchange'));
      return Promise.resolve();
    };
  });
}

const calls = (page: Page) => page.evaluate(() => (window as unknown as { __fs: string[] }).__fs);

test('Full screen in a contest workspace enters and leaves full screen, and names F11', async ({ page }) => {
  await fakeFullscreen(page);
  await contests.mockApi(page, { phase: 'during' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  const button = page.getByRole('button', { name: 'Full screen', exact: true });
  await expect(button).toHaveAttribute('title', 'Full screen (F11 works too)');
  await button.click();
  await expect(page.getByRole('button', { name: 'Exit full screen' })).toBeVisible();
  await page.getByRole('button', { name: 'Exit full screen' }).click();
  await expect(button).toBeVisible();
  expect(await calls(page)).toEqual(['request', 'exit']);
});

test('an exam that requires full screen shows no Full screen button in the workspace', async ({ page }) => {
  await fakeFullscreen(page);
  await contests.mockApi(page, { phase: 'during', exam: { full_screen: true } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(codeEditor(page)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Full screen', exact: true })).toHaveCount(0);
});
