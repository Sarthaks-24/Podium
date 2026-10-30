// Skeletons (v4 U1): a slow screen shows static blocks shaped like it after
// 300 ms, with the loading sentence still read out, then its content. A fast
// one shows nothing. The blocks never move, with or without reduced motion.
import { expect, test, type Page } from '@playwright/test';
import * as contests from './mocks/contests';

/** Holds the answer to `url` until the returned function is called. */
async function hold(page: Page, url: string | RegExp): Promise<() => void> {
  let release = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  await page.route(url, async (route) => {
    await gate;
    await route.fallback();
  });
  return release;
}

for (const reducedMotion of ['no-preference', 'reduce'] as const) {
  test(`a slow contest list shows still rows, then the contests (${reducedMotion})`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion });
    await contests.mockApi(page, { phase: 'during' });
    const release = await hold(page, '**/api/contests');
    await page.goto('/');
    const skeleton = page.getByTestId('skeleton');
    await expect(skeleton).toBeVisible();
    await expect(skeleton).toHaveAttribute('aria-hidden', 'true');
    await expect(page.getByRole('status').filter({ hasText: 'Loading contests' })).toHaveCount(1);
    // Reduced motion sets every duration to 0.01 ms (base.css), so under 1 ms counts as still.
    const moving = await skeleton.evaluate((el) =>
      [el, ...el.querySelectorAll('*')].some((n) => {
        const s = getComputedStyle(n);
        return s.animationName !== 'none' || s.transitionDuration.split(',').some((d) => parseFloat(d) > 0.001) || s.backgroundImage !== 'none';
      }),
    );
    expect(moving).toBe(false);
    release();
    await expect(skeleton).toHaveCount(0);
    await expect(page.getByRole('link', { name: /Week 6/ }).first()).toBeVisible();
  });
}

test('a slow problem shows a still statement, and a fast screen shows no blocks', async ({ page }) => {
  await contests.mockApi(page, { phase: 'during' });
  const release = await hold(page, '**/api/contests/week-6/problems/A');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6/problems/A');
  await expect(page.locator('[data-testid="skeleton"].statement')).toBeVisible();
  release();
  await expect(page.getByTestId('skeleton')).toHaveCount(0);

  // Back to the list: answered at once, so no blocks ever show.
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Week 6/ }).first()).toBeVisible();
  await expect(page.getByTestId('skeleton')).toHaveCount(0);
});
