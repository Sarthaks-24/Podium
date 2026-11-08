// The contest description on the student page of an ended contest and of a
// practice set (v2.0.2 release fix). It reads after the problem list, so the
// list stays in the first screen on a phone. Mocked API; screenshots at four
// widths in both themes go to cache/v2.0.2/shots/fix2/; axe finds nothing.
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { mockApi, settle, THEMES, WIDTHS } from './mocks/contests';

const OUT = '../../../../../cache/v2.0.2/shots/fix2/';

for (const phase of ['after', 'practice'] as const) {
  for (const theme of THEMES) {
    test.describe(`${phase}, ${theme} theme`, () => {
      test.use({ colorScheme: theme });

      test('the description shows after the problem list', async ({ page }) => {
        await mockApi(page, { phase });
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto('/contests/week-6');
        const description = page.locator('.description');
        await expect(description).toContainText('Three problems on sums and intervals. Good luck.');
        const list = page.getByRole('list', { name: 'Your problems' });
        const listBox = await list.boundingBox();
        const descBox = await description.boundingBox();
        expect(descBox!.y).toBeGreaterThan(listBox!.y + listBox!.height - 1);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          if (width === 375) {
            // The problem list starts inside the first screen.
            await expect(list.locator(':scope > li').first()).toBeInViewport();
          }
          await page.screenshot({ path: `${test.info().project.testDir}/${OUT}${phase}-${width}-${theme}.png`, fullPage: true });
        }
        const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
        expect(results.violations.map((v) => `${v.id}: ${v.nodes.length}`)).toEqual([]);
      });
    });
  }
}
