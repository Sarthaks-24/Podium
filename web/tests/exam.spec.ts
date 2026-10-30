// Exam options (V3-25, stage C slice C7): four switches under Advanced in
// contest setup, all off by default. A student reads one line naming the
// measures before Start; a blocked paste or copy does nothing and says so;
// with full screen on, leaving it is counted and the console's feed shows
// the count. With no option on, nothing changes.
import { expect, test, type Page } from '@playwright/test';
import { codeEditor, toasts } from './locators';
import * as contests from './mocks/contests';
import * as integrity from './mocks/integrity';

/** Playwright cannot enter full screen: the request and the exit are simulated, with the event the browser fires. */
async function fakeFullscreen(page: Page) {
  await page.addInitScript(() => {
    let el: Element | null = null;
    const w = window as unknown as { fullscreenRequests: number; leaveFullscreen: () => void };
    w.fullscreenRequests = 0;
    Object.defineProperty(Document.prototype, 'fullscreenElement', { configurable: true, get: () => el });
    Element.prototype.requestFullscreen = () => {
      w.fullscreenRequests++;
      el = document.documentElement;
      document.dispatchEvent(new Event('fullscreenchange'));
      return Promise.resolve();
    };
    w.leaveFullscreen = () => {
      el = null;
      document.dispatchEvent(new Event('fullscreenchange'));
    };
  });
}

/** The line above Start (Help's rules list says it too). */
const examLine = (page: Page, text: string | RegExp) => page.getByRole('paragraph').filter({ hasText: text });

async function paste(page: Page, text: string) {
  await codeEditor(page).evaluate((el, t) => {
    const data = new DataTransfer();
    data.setData('text/plain', t);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, text);
}

test('the four switches sit under Advanced, off, and each saves alone', async ({ page }) => {
  const api = await contests.mockApi(page, { who: 'organiser', phase: 'upcoming', published: false });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4');
  const options = page.getByRole('group', { name: 'Exam options' });
  await expect(options).toBeHidden();
  await page.getByRole('heading', { name: 'Advanced' }).click();
  for (const name of ['Full screen on start', 'Count tab switches and focus loss', 'Block paste into the editor', 'Block copy out of the statement']) {
    await expect(options.getByRole('checkbox', { name })).not.toBeChecked();
  }
  await options.getByRole('checkbox', { name: 'Block paste into the editor' }).check();
  await expect.poll(() => api.settingsPuts.at(-1)).toEqual({ block_paste: true });
  await expect(page.getByText(/exam options: block paste into the editor/)).toBeVisible();
});

test('with paste and copy blocked, the student is told before Start, and a paste inserts nothing', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'ready', exam: { block_paste: true, block_copy: true } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  await expect(examLine(page, 'This contest blocks pasting into the editor and blocks copying from the statement.')).toBeVisible();
  await page.getByRole('button', { name: /start the contest/i }).click();
  await expect.poll(() => api.starts).toBe(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/A plus B/);

  const editor = codeEditor(page);
  await editor.click();
  const before = await editor.textContent();
  await paste(page, 'copied answer');
  await expect(toasts(page).getByText('Pasting is blocked in this contest. Nothing was pasted.')).toBeVisible();
  expect(await editor.textContent()).toBe(before);

  // A copy from the statement is refused too, with its own line; the examples have no Copy buttons.
  const refused = await page.getByText('One line with', { exact: false }).first().evaluate((el) => !el.dispatchEvent(new ClipboardEvent('copy', { bubbles: true, cancelable: true })));
  expect(refused).toBe(true);
  await expect(toasts(page).getByText('Copying from the statement is blocked in this contest.')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Copy example/ })).toHaveCount(0);

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0]).toMatchObject({ pasted_chars: 0 });
});

test('with full screen on, Start asks for it and leaving it is a count the console shows', async ({ page }) => {
  await fakeFullscreen(page);
  const api = await contests.mockApi(page, { phase: 'ready', exam: { full_screen: true } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  await expect(examLine(page, 'This contest opens in full screen, counting each time you leave it.')).toBeVisible();
  await page.getByRole('button', { name: /start the contest/i }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/A plus B/);
  expect(await page.evaluate(() => (window as unknown as { fullscreenRequests: number }).fullscreenRequests)).toBe(1);
  await expect(page.getByRole('button', { name: 'Return to full screen' })).toHaveCount(0);

  await page.evaluate(() => (window as unknown as { leaveFullscreen: () => void }).leaveFullscreen());
  const back = page.getByRole('button', { name: 'Return to full screen' });
  await expect(back).toBeVisible();
  await back.click();
  await expect(back).toHaveCount(0);

  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect.poll(() => api.submits.length).toBe(1);
  expect(api.submits[0]).toMatchObject({ fullscreen_exits: 1 });

  // The organiser's live view: the feed carries the count beside the pasted characters.
  api.who = 'organiser';
  await page.goto('/admin/contests/4/console');
  const feed = page.getByRole('region', { name: 'Submissions' });
  await expect(feed.getByRole('columnheader', { name: 'Recorded' })).toBeVisible();
  const row = feed.getByRole('row').filter({ has: page.getByRole('rowheader', { name: 'Asha Rao' }) }).first();
  await expect(row).toContainText('Pasted 0 characters, left full screen 1 time');
});

test('with no exam option on, nothing changes', async ({ page }) => {
  const api = await contests.mockApi(page, { phase: 'ready' });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/contests/week-6');
  await expect(page.getByRole('button', { name: /start the contest/i })).toBeVisible();
  await expect(page.getByText(/This contest (blocks|opens|counts each)/)).toHaveCount(0);
  await page.getByRole('button', { name: /start the contest/i }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/A plus B/);
  await codeEditor(page).click();
  await page.keyboard.press('Control+End');
  await paste(page, 'pasted!');
  await expect(codeEditor(page)).toContainText('pasted!');
  await expect(page.getByRole('button', { name: /^Copy example/ }).first()).toBeVisible();

  api.who = 'organiser';
  await page.goto('/admin/contests/4/console');
  const feed = page.getByRole('region', { name: 'Submissions' });
  await expect(feed.getByRole('columnheader', { name: 'Outcome' })).toBeVisible();
  await expect(feed.getByRole('columnheader', { name: 'Recorded' })).toHaveCount(0);
});

test('the similar-code page lists what was recorded when a measure was on', async ({ page }) => {
  await integrity.mockApi(page, { exam: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests/4/integrity');
  const section = page.getByRole('region', { name: 'Recorded during the contest' });
  await expect(section.getByRole('columnheader', { name: 'Left full screen' })).toBeVisible();
  await expect(section.getByRole('columnheader', { name: 'Left the page' })).toBeVisible();
  const asha = section.getByRole('row').filter({ has: page.getByRole('rowheader', { name: 'Asha Rao' }) });
  await expect(asha).toContainText('431 characters in 2 pastes');
  await expect(asha).toContainText('2 times');
});
