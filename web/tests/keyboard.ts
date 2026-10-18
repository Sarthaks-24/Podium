// Keyboard-only helpers for the M6 run-throughs. tabTo presses Tab (or
// Shift+Tab) until the target holds focus and fails after a fixed number of
// presses, so a control that cannot be reached, or a trap that keeps focus
// in one place, fails the test instead of passing by mouse.
import { expect, type Locator, type Page } from '@playwright/test';

/** True when the target or something inside it has focus. */
export function hasFocus(target: Locator): Promise<boolean> {
  return target.evaluate((el) => el === document.activeElement || el.contains(document.activeElement));
}

/** Presses Tab until `target` has focus; returns the number of presses. */
export async function tabTo(page: Page, target: Locator, options: { max?: number; back?: boolean } = {}): Promise<number> {
  const max = options.max ?? 60;
  await expect(target).toBeAttached();
  for (let i = 1; i <= max; i++) {
    await page.keyboard.press(options.back ? 'Shift+Tab' : 'Tab');
    if (await hasFocus(target)) return i;
  }
  const where = await page.evaluate(() => {
    const el = document.activeElement;
    return el ? `${el.tagName.toLowerCase()} ${el.getAttribute('aria-label') ?? el.textContent?.trim().slice(0, 40) ?? ''}` : 'nothing';
  });
  throw new Error(`Not reached in ${max} presses of Tab; focus is on ${where}`);
}

/** Tabs to a control and presses Enter on it. */
export async function activate(page: Page, target: Locator, key = 'Enter'): Promise<void> {
  await tabTo(page, target);
  await page.keyboard.press(key);
}
