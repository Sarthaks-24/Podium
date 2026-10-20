// Accessibility baseline (cache/v2-plan/phases/P0-gate.md F0.4) over every
// route, using the screens and mocks of the other specs (tests/mocks/).
//
// For each screen, in light and dark: no Vite error overlay, then axe-core
// with the WCAG 2.2 A and AA rules at 1440 and at 320 px. Every result is
// saved to cache/v2-plan/audits/axe/<screen>-<theme>[-320].json, and the test fails on a
// serious or critical violation that KNOWN does not list with the plan
// item that fixes it (cache/v2-plan/audits/2026-10-20-axe-baseline.md). At
// 320 px (WCAG 1.4.10 reflow) the page must not scroll sideways, and in
// light theme a forced-colours screenshot is saved for review; both go to
// cache/v2-planning-files/screenshots-v2-baseline/.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import * as ai from './mocks/ai';
import * as contests from './mocks/contests';
import * as integrity from './mocks/integrity';
import * as problems from './mocks/problems';
import * as screens from './mocks/screens';
import * as workspace from './mocks/workspace';

interface Target {
  name: string;
  path: string;
  mock: (page: Page) => Promise<unknown>;
  ready: (page: Page) => Promise<void>;
}

const TARGETS: Target[] = [
  ...screens.SCREENS.map((s) => ({ ...s, mock: (p: Page) => screens.mockApi(p, s.who) })),
  ...problems.SCREENS.map((s) => ({ ...s, mock: (p: Page) => problems.mockApi(p) })),
  ...ai.SCREENS.map((s) => ({ ...s, mock: (p: Page) => ai.mockApi(p, s.opts) })),
  ...contests.SCREENS.map((s) => ({ ...s, mock: (p: Page) => contests.mockApi(p, s.options) })),
  ...integrity.SCREENS.map((s) => ({ ...s, name: s.name.startsWith('integrity') ? s.name : `integrity-${s.name}`, mock: (p: Page) => integrity.mockApi(p, s.options) })),
  {
    name: 'problem-try',
    path: '/admin/problems/1/try',
    mock: (p: Page) => workspace.mockApi(p, { submissions: workspace.HISTORY }),
    ready: async (p: Page) => {
      await expect(p.getByRole('heading', { level: 1 })).toHaveText('A plus B');
      await expect(p.locator('.cm-content')).toHaveCount(1);
    },
  },
];

/**
 * Serious and critical findings already known, by axe rule id (any screen)
 * or "rule@screen", with the plan item that fixes them. A finding not
 * listed here fails the test.
 */
const KNOWN: Record<string, string> = {
  // The problem statistics table scrolls sideways at 320 px and its wrapper cannot take focus.
  'scrollable-region-focusable@contest-results': 'F7 (P1, F-V2) table utilities, applied to Results by R1 (P3, F-V6)',
};

/** Screens allowed to scroll sideways at 320 px, with the plan item that fixes them. */
const SUBNAV = 'N3 (P2, F-V3): the contest sub-nav is 14 px wider than 320 px; it must wrap or scroll when N3 adds "Student view"';
const KNOWN_REFLOW: Record<string, string> = {
  'contest-editor': SUBNAV,
  'contest-console': SUBNAV,
  'contest-results': SUBNAV,
  integrity: SUBNAV,
  'integrity-live': SUBNAV,
};

function cache(sub: string): string {
  return `${test.info().project.testDir}/../../cache/${sub}`;
}

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
}

/** Runs axe, saves the result as <file>.json and fails on a serious or critical finding KNOWN does not list. */
async function audit(page: Page, t: Target, file: string) {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  const dir = cache('v2-plan/audits/axe');
  mkdirSync(dir, { recursive: true });
  const report = results.violations.map((v) => ({
    rule: v.id,
    impact: v.impact,
    help: v.help,
    nodes: v.nodes.map((n) => ({ target: n.target.join(' '), summary: n.failureSummary })),
  }));
  writeFileSync(`${dir}/${file}.json`, JSON.stringify({ path: t.path, viewport: page.viewportSize(), violations: report }, null, 2));
  const unknown = report
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .filter((v) => !KNOWN[v.rule] && !KNOWN[`${v.rule}@${t.name}`])
    .map((v) => `${v.impact} ${v.rule}: ${v.help} (${v.nodes.length}: ${v.nodes[0].target})`);
  expect(unknown, `serious or critical axe findings not in KNOWN (${file})`).toEqual([]);
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    // Two axe runs and three screenshots per screen.
    test.describe.configure({ timeout: 60_000 });

    for (const t of TARGETS) {
      test(`axe, 320 px and forced colours: ${t.name}`, async ({ page }) => {
        await t.mock(page);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(t.path);
        await t.ready(page);
        await settle(page);
        await expect(page.locator('vite-error-overlay')).toHaveCount(0);

        await audit(page, t, `${t.name}-${theme}`);

        const shots = cache('v2-planning-files/screenshots-v2-baseline');
        await page.setViewportSize({ width: 320, height: 640 });
        await settle(page);
        await audit(page, t, `${t.name}-${theme}-320`);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        await page.screenshot({ path: `${shots}/a11y-320/${t.name}-${theme}.png`, fullPage: true });
        if (!KNOWN_REFLOW[t.name]) expect(overflow, 'horizontal scroll at 320 px').toBeLessThanOrEqual(0);

        if (theme === 'light') {
          await page.emulateMedia({ forcedColors: 'active' });
          await page.setViewportSize({ width: 1024, height: 768 });
          await settle(page);
          await page.screenshot({ path: `${shots}/forced-colours/${t.name}.png`, fullPage: true });
        }
      });
    }
  });
}
