// The v2 baseline (cache/v2-plan/phases/P0-gate.md F0.2 and F0.3), on the
// contest mock of tests/mocks/contests.ts.
//
// Sweep (F0.2): the student workspace inside a contest, tab by tab
// (Problem, Code, Result) at 375, 768, 1024 and 1440 px, plus with the
// on-screen keyboard open at 375 and 768; the contest before and after and
// the workspace at the short laptop size 1093x500 and at 1280x720; and the
// same student screens with touch emulation (isMobile, hasTouch) at 375
// and 768. Each shot checks for no horizontal overflow and goes to
// cache/v2-planning-files/screenshots-v2-baseline/.
//
// Pixel diffs (F0.3): toHaveScreenshot on the student screens (contest
// list, contest before, during, after, standings) at 375 and 1440 px in
// light and dark, on a frozen clock so countdowns and times never move.
// The reference PNGs are not in git: playwright.config.ts keeps them under
// cache/v2-planning-files/v2-frontend/baseline/. A missing reference is
// written on the first run; `npx playwright test baseline --update-snapshots`
// accepts an expected change.
import { expect, test, type Page } from '@playwright/test';
import { freezeClock, mockApi, settle, submission, SCREENS, THEMES, WIDTHS, MIN, type Options } from './mocks/contests';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/v2-planning-files/screenshots-v2-baseline/`;
}

function screen(name: string) {
  const s = SCREENS.find((x) => x.name === name);
  if (!s) throw new Error(`no screen ${name}`);
  return s;
}

async function noOverflow(page: Page, where: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `horizontal overflow ${where}`).toBeLessThanOrEqual(0);
}

/** The contest workspace on problem A with one judged submission, so Result has something to show. */
async function openWorkspace(page: Page, options: Options = { phase: 'during' }) {
  const api = await mockApi(page, options);
  api.submissions.push(submission(30, 11, 'done', 40, Date.now() - 5 * MIN));
  await page.goto('/contests/week-6/problems/A');
  // Under 1024 px the editor waits behind the Code tab.
  await expect(page.locator('.cm-content')).toBeAttached();
  await expect(page.locator('.statement .katex').first()).toBeVisible();
  await settle(page);
}

/** The on-screen keyboard takes this much of the height, roughly a phone's and a tablet's. */
const KEYBOARD: Record<number, number> = { 375: 336, 768: 400 };

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('contest workspace, tab by tab and with the keyboard open', async ({ page }) => {
      await page.setViewportSize({ width: 1440, height: 900 });
      await openWorkspace(page);
      for (const width of WIDTHS) {
        const height = width < 768 ? 812 : width < 1024 ? 1024 : 900;
        await page.setViewportSize({ width, height });
        await settle(page);
        await noOverflow(page, `at ${width}px`);
        if (width >= 1024) {
          await page.screenshot({ path: `${shots()}workspace-contest-${width}-${theme}.png` });
          continue;
        }
        const tabs = width < 768 ? ['Problem', 'Code', 'Result'] : ['Problem', 'Code'];
        for (const tab of tabs) {
          await page.getByRole('tab', { name: tab }).click();
          await expect(page.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true');
          await settle(page);
          await noOverflow(page, `on ${tab} at ${width}px`);
          await page.screenshot({ path: `${shots()}workspace-contest-${tab.toLowerCase()}-${width}-${theme}.png`, fullPage: true });
        }
        // Keyboard open: emulated as a shorter viewport with the editor focused.
        await page.getByRole('tab', { name: 'Code' }).click();
        await page.locator('.cm-content').click();
        await page.setViewportSize({ width, height: height - KEYBOARD[width] });
        await settle(page);
        await expect(page.locator('.cm-content')).toBeFocused();
        await noOverflow(page, `with the keyboard open at ${width}px`);
        await page.screenshot({ path: `${shots()}workspace-contest-keyboard-${width}-${theme}.png` });
        await page.keyboard.press('Escape');
        await page.getByRole('tab', { name: 'Problem' }).click();
      }
    });

    for (const size of [
      { width: 1093, height: 500 },
      { width: 1280, height: 720 },
    ]) {
      test(`laptop ${size.width}x${size.height}: contest before, workspace, after`, async ({ page }) => {
        await page.setViewportSize(size);
        const tag = `${size.width}x${size.height}`;
        for (const name of ['contest-before', 'contest-after']) {
          const s = screen(name);
          await mockApi(page, s.options);
          await page.goto(s.path);
          await s.ready(page);
          await settle(page);
          await noOverflow(page, `${name} at ${tag}`);
          await page.screenshot({ path: `${shots()}${name}-${tag}-${theme}.png` });
          // Leave the page first, so its event stream stops before the mock goes.
          await page.goto('about:blank');
          await page.unrouteAll({ behavior: 'ignoreErrors' });
        }
        await openWorkspace(page);
        await noOverflow(page, `workspace at ${tag}`);
        await page.screenshot({ path: `${shots()}workspace-contest-${tag}-${theme}.png` });
      });
    }
  });
}

test.describe('touch', () => {
  test.use({ hasTouch: true, isMobile: true });

  for (const size of [
    { width: 375, height: 812 },
    { width: 768, height: 1024 },
  ]) {
    test(`touch at ${size.width} px: contest before, workspace, standings`, async ({ page }) => {
      await page.setViewportSize(size);
      for (const name of ['contest-before', 'standings-frozen']) {
        const s = screen(name);
        await mockApi(page, s.options);
        await page.goto(s.path);
        await s.ready(page);
        await settle(page);
        await noOverflow(page, `${name} on touch at ${size.width}px`);
        await page.screenshot({ path: `${shots()}touch-${name}-${size.width}.png`, fullPage: true });
        await page.goto('about:blank');
        await page.unrouteAll({ behavior: 'ignoreErrors' });
      }
      await openWorkspace(page);
      await page.getByRole('tab', { name: 'Code' }).tap();
      await expect(page.getByRole('tab', { name: 'Code' })).toHaveAttribute('aria-selected', 'true');
      await noOverflow(page, `workspace on touch at ${size.width}px`);
      await page.screenshot({ path: `${shots()}touch-workspace-code-${size.width}.png`, fullPage: true });
    });
  }
});

// 2026-10-20 10:00 in Asia/Kolkata, the config's time zone.
const FROZEN = Date.UTC(2026, 9, 20, 4, 30);

test.describe('pixel baselines', () => {
  test.beforeEach(async ({ page }) => {
    freezeClock(FROZEN);
    await page.clock.setFixedTime(FROZEN);
  });
  test.afterEach(() => freezeClock(null));

  for (const theme of THEMES) {
    for (const name of ['home-student', 'contest-before', 'contest-during', 'contest-after', 'standings-frozen']) {
      test(`${name} ${theme}`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: theme });
        const s = screen(name);
        await mockApi(page, s.options);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(s.path);
        await s.ready(page);
        for (const width of [1440, 375]) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          await expect(page).toHaveScreenshot(`${name}-${width}-${theme}.png`, { fullPage: true });
        }
      });
    }
  }
});
