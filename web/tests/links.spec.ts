// The link crawl and "every button does something" (web-quality #20 and
// #29, P3 FP.5). On the contest mock (tests/mocks/contests.ts), for the
// student and the organiser screens: every link on every screen is
// collected, and each internal one is opened and must not land on "Page
// not found"; mailto: and tel: links must carry an address. Links under
// /api/ (exports, Google sign-in) are full page loads by design and are
// left to the server's own tests. Then, on the
// student screens, every visible, enabled button is pressed on a fresh
// load and must do something a person can see or the server can hear: a
// change in the page, a navigation, a dialog, a moved focus or a request.
// The static files the head and the manifest name must answer with the
// right type, and the social card must be a 1200 by 630 PNG under 60 KB.
import { expect, test, type Page } from '@playwright/test';
import { mockApi, settle, SCREENS } from './mocks/contests';

const CRAWLED = ['home-student', 'contest-before', 'contest-during', 'contest-after', 'standings-frozen', 'admin-contests', 'contest-editor', 'contest-console', 'contest-results'];

async function open(page: Page, name: string) {
  const s = SCREENS.find((x) => x.name === name)!;
  await mockApi(page, s.options);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(s.path);
  await s.ready(page);
  await settle(page);
  return s;
}

async function hrefs(page: Page): Promise<string[]> {
  return page.evaluate(() => [...document.querySelectorAll<HTMLAnchorElement>('a[href]')].map((a) => a.getAttribute('href') ?? ''));
}

for (const name of CRAWLED) {
  test(`every link on ${name} goes somewhere`, async ({ page }) => {
    test.setTimeout(180_000);
    const s = await open(page, name);
    const all = await hrefs(page);
    expect(all.length, 'links found').toBeGreaterThan(0);
    for (const h of all) {
      if (h.startsWith('mailto:')) expect(h, h).toMatch(/^mailto:[^@\s]+@[^@\s]+\.[a-z]+(\?.*)?$/i);
      else if (h.startsWith('tel:')) expect(h, h).toMatch(/^tel:\+?\d{6,}$/);
      else if (/^https?:/.test(h)) expect(h, `no external link expected: ${h}`).toBe('');
      else expect(h, `an empty or placeholder href: "${h}"`).toMatch(/^(\/|#.)/);
    }
    const internal = [...new Set(all.filter((h) => h.startsWith('/') && !h.startsWith('/api/')).map((h) => h.split('#')[0]))];
    for (const path of internal) {
      await page.unrouteAll({ behavior: 'ignoreErrors' });
      await mockApi(page, s.options);
      await page.goto(path);
      await expect(page.locator('h1'), `${path} from ${name}`).toHaveCount(1);
      await expect(page.locator('h1'), `${path} from ${name}`).not.toHaveText('Page not found');
    }
  });
}

/** Watches the page for anything a press can cause. */
async function watch(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __changed: boolean; __obs?: MutationObserver };
    w.__changed = false;
    w.__obs?.disconnect();
    w.__obs = new MutationObserver(() => (w.__changed = true));
    w.__obs.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
  });
}

for (const name of ['home-student', 'contest-before', 'contest-during', 'contest-after', 'standings-frozen']) {
  test(`every button on ${name} does something`, async ({ page }) => {
    test.setTimeout(180_000);
    await open(page, name);
    const labels = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLButtonElement>('main button, footer button')]
        .filter((b) => !b.disabled && b.getAttribute('aria-disabled') !== 'true' && b.offsetParent !== null)
        .map((b) => b.getAttribute('aria-label') || b.textContent?.trim() || ''),
    );
    expect(labels.length, 'buttons found').toBeGreaterThan(0);
    for (const label of [...new Set(labels)]) {
      await page.unrouteAll({ behavior: 'ignoreErrors' });
      await open(page, name);
      const button = page.locator('main button, footer button').filter({ hasText: label }).first();
      if (!(await button.isVisible())) continue;
      const before = page.url();
      let requests = 0;
      page.on('request', () => requests++);
      await watch(page);
      const focusBefore = await page.evaluate(() => document.activeElement?.outerHTML ?? '');
      await button.click();
      await page.waitForTimeout(300);
      const changed = await page.evaluate(() => (window as unknown as { __changed: boolean }).__changed);
      const focusAfter = await page.evaluate(() => document.activeElement?.outerHTML ?? '');
      const dialog = await page.locator('dialog[open]').count();
      const did = changed || dialog > 0 || page.url() !== before || requests > 0 || focusAfter !== focusBefore;
      expect(did, `"${label}" on ${name} did nothing`).toBe(true);
    }
  });
}

test('the files the head and the manifest name are there (FP.4)', async ({ request }) => {
  const types: Record<string, RegExp> = {
    '/favicon.svg': /svg/,
    '/favicon.ico': /icon|octet-stream/,
    '/apple-touch-icon.png': /png/,
    '/icon-192.png': /png/,
    '/icon-512.png': /png/,
    '/og.png': /png/,
    '/site.webmanifest': /manifest|json|octet-stream/,
  };
  for (const [path, type] of Object.entries(types)) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(200);
    expect(res.headers()['content-type'] ?? '', path).toMatch(type);
  }
  const og = await (await request.get('/og.png')).body();
  expect(og.length, 'og.png bytes').toBeLessThan(60 * 1024);
  // PNG IHDR: width and height as big-endian 32-bit integers at bytes 16 and 20.
  expect([og.readUInt32BE(16), og.readUInt32BE(20)]).toEqual([1200, 630]);
  const manifest = JSON.parse(await (await request.get('/site.webmanifest')).text());
  for (const icon of manifest.icons) expect((await request.get(icon.src)).status(), icon.src).toBe(200);
});

test('the page head carries one robots rule, the icons and no social tags of its own (FP.4)', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/');
  expect(await page.locator('meta[name="robots"]').count()).toBe(1);
  expect(await page.locator('meta[property^="og:"], meta[name^="twitter:"]').count()).toBe(0);
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/site.webmanifest');
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/apple-touch-icon.png');
  expect(await page.locator('meta[name="theme-color"]').count()).toBe(2);
});
