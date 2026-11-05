// The screen audit (V201-16). For every screen in screens.ts, in light and
// dark at 375, 768, 1024 and 1440 px: open the state, save a full-page
// screenshot, check the page does not scroll sideways, run axe, and for a
// state with a dialog press Escape and check focus lands on an element in the
// page, not on body. A failed check is recorded, never thrown, so every cell
// runs; the test fails at the end if any cell did, and report.ts writes
// REPORT.md from the recorded cells.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { AXE_TAGS } from '../axe';
import { auditDir, rawDir, THEMES, WIDTHS, type Cell } from './paths';
import { AUDIT_SCREENS, type AuditScreen } from './screens';

/** Playwright colours its messages for the terminal; the report is plain text. */
const plain = (text: string) => text.split(String.fromCharCode(27)).map((part, i) => (i === 0 ? part : part.replace(/^[[0-9;]*m/, ''))).join('');

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
}

/** Where focus is, as a short description, or '' when it is on body or nowhere. */
function focusOwner(page: Page): Promise<string> {
  return page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body || el === document.documentElement || !document.body.contains(el)) return '';
    const name = el.getAttribute('aria-label') || (el.textContent ?? '').trim().slice(0, 30);
    return `${el.tagName.toLowerCase()}${name ? ` "${name}"` : ''}`;
  });
}

async function checkCell(page: Page, s: AuditScreen, width: number, theme: string, dir: string): Promise<Cell> {
  const cell: Cell = { screen: s.name, width, theme, failures: [], axe: [] };
  try {
    await page.setViewportSize({ width, height: 900 });
    await s.mock(page);
    await page.goto(s.path);
    await (s.ready ?? ((p) => expect(p.locator('main, h1').first()).toBeVisible()))(page);
    await s.steps?.(page);
    await settle(page);

    if ((await page.locator('vite-error-overlay').count()) > 0) cell.failures.push('load: Vite error overlay');

    await page.screenshot({ path: `${dir}/${s.name}-${width}-${theme}.png`, fullPage: true });

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    if (overflow > 0) cell.failures.push(`overflow: page is ${overflow} px wider than the window`);

    const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
    for (const v of results.violations) {
      cell.axe.push({ rule: v.id, impact: v.impact ?? '', help: v.help, targets: v.nodes.map((n) => n.target.join(' ')) });
    }
    if (results.violations.length > 0) cell.failures.push(`axe: ${results.violations.map((v) => `${v.id} (${v.nodes.length})`).join(', ')}`);

    // A dialog open here must give focus back when Escape closes it.
    if ((await page.locator('dialog[open]').count()) > 0) {
      await page.keyboard.press('Escape');
      let owner = '';
      try {
        await expect.poll(async () => (owner = await focusOwner(page)), { timeout: 2000 }).not.toBe('');
      } catch {
        // Falls through to the message below.
      }
      if ((await page.locator('dialog[open]').count()) > 0) cell.failures.push('focus: Escape did not close the dialog');
      else if (owner === '') cell.failures.push('focus: after Escape, focus is on body');
    }
  } catch (e) {
    const lines = plain(String(e instanceof Error ? e.message : e)).split('\n').filter((l) => l.trim() !== '');
    cell.failures.push(`load: ${lines.slice(0, 8).join(' ')}`);
    // What the page showed when the step gave up, for whoever reads the report.
    await page.screenshot({ path: `${dir}/${s.name}-${width}-${theme}-FAILED.png`, fullPage: true }).catch(() => undefined);
  }
  return cell;
}

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    // Four page loads, each with a screenshot and an axe run.
    test.describe.configure({ timeout: 120_000 });

    for (const s of AUDIT_SCREENS) {
      test(s.name, async ({ page }) => {
        const dir = auditDir();
        mkdirSync(rawDir(), { recursive: true });
        const cells: Cell[] = [];
        for (const width of WIDTHS) {
          // A fresh page per width, so one state never leaks into the next.
          const fresh = await page.context().newPage();
          cells.push(await checkCell(fresh, s, width, theme, dir));
          await fresh.close();
        }
        writeFileSync(`${rawDir()}/${s.name}-${theme}.json`, JSON.stringify(cells, null, 2));
        const failed = cells.filter((c) => c.failures.length > 0).map((c) => `${c.width}: ${c.failures.join('; ')}`);
        expect(failed, `${s.name}, ${theme}`).toEqual([]);
      });
    }
  });
}
