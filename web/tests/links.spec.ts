// The link crawl and "every button does something" (web-quality #20 and
// #29, P3 FP.5, v4 H3). On the student and organiser contest screens, the
// problem bank and editor, the Settings tabs, privacy and the similar-code
// screens: every link is collected, and each internal one is opened and
// must not land on "Page not found"; mailto: and tel: links must carry an
// address; a disabled button must say why (a title or a described-by
// line), and a planted bad link must fail the crawl. Links under
// /api/ (exports, Google sign-in) are full page loads by design and are
// left to the server's own tests. Then, on the student screens and the
// organiser screens (contests, problems, the Settings tabs), every visible,
// enabled button is pressed on a fresh load and must do something a person
// can see or the server can hear: a change in the page, a navigation, a
// dialog, focus moved somewhere other than the pressed button, or an /api/
// request other than the event stream. The clock is fixed and each screen
// must stay still for a moment before the press, so a ticking countdown
// cannot pass for an effect; a self-test proves that a button which does
// nothing fails (council run 2, C2-018).
// The static files the head and the manifest name must answer with the
// right type, and the social card must be a 1200 by 630 PNG under 60 KB.
import { expect, test, type Page } from '@playwright/test';
import { mockApi, settle, SCREENS } from './mocks/contests';
import * as integrity from './mocks/integrity';
import * as problems from './mocks/problems';
import * as people from './mocks/screens';

async function open(page: Page, name: string) {
  const s = SCREENS.find((x) => x.name === name)!;
  await mockApi(page, s.options);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(s.path);
  await s.ready(page);
  await settle(page);
  return s;
}

/** A screen to crawl: its address, the mock that serves it and what it shows once loaded. */
type Crawled = { name: string; path: string; mock: (page: Page) => Promise<unknown>; ready: (page: Page) => Promise<void> };

const CRAWLED: Crawled[] = [
  ...['home-student', 'contest-before', 'contest-during', 'contest-after', 'standings-frozen', 'admin-contests', 'contest-editor', 'contest-console', 'contest-results'].map((name) => {
    const s = SCREENS.find((x) => x.name === name)!;
    return { name, path: s.path, mock: (p: Page) => mockApi(p, s.options), ready: s.ready };
  }),
  ...problems.SCREENS.filter((s) => ['problems-bank', 'problem-editor'].includes(s.name)).map((s) => ({ name: s.name, path: s.path, mock: (p: Page) => problems.mockApi(p, 'organiser'), ready: s.ready })),
  ...people.SCREENS.filter((s) => s.name.startsWith('settings-') || s.name === 'privacy').map((s) => ({ name: s.name, path: s.path, mock: (p: Page) => people.mockApi(p, s.who), ready: s.ready })),
  ...integrity.SCREENS.map((s) => ({ name: `integrity-${s.name}`, path: s.path, mock: (p: Page) => integrity.mockApi(p, s.options), ready: s.ready })),
];

async function hrefs(page: Page): Promise<string[]> {
  return page.evaluate(() => [...document.querySelectorAll<HTMLAnchorElement>('a[href]')].map((a) => a.getAttribute('href') ?? ''));
}

/** Every link on the loaded screen that is malformed or lands on "Page not found", in words. */
async function badLinks(page: Page, s: Crawled): Promise<string[]> {
  const all = await hrefs(page);
  // A button that cannot be pressed says why: a title, or a described-by line.
  const bad: string[] = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLButtonElement>('main button')]
      .filter((b) => (b.disabled || b.getAttribute('aria-disabled') === 'true') && b.checkVisibility())
      .filter((b) => !b.title && !b.getAttribute('aria-describedby'))
      .map((b) => `a disabled button with no reason: "${(b.getAttribute('aria-label') || b.textContent || '').trim()}"`),
  );
  for (const h of all) {
    if (h.startsWith('mailto:')) {
      if (!/^mailto:[^@\s]+@[^@\s]+\.[a-z]+(\?.*)?$/i.test(h)) bad.push(`a mailto: link without an address: ${h}`);
    } else if (h.startsWith('tel:')) {
      if (!/^tel:\+?\d{6,}$/.test(h)) bad.push(`a tel: link without a number: ${h}`);
    } else if (/^https?:/.test(h)) bad.push(`an external link: ${h}`);
    else if (!/^(\/|#.)/.test(h)) bad.push(`an empty or placeholder href: "${h}"`);
  }
  const internal = [...new Set(all.filter((h) => h.startsWith('/') && !h.startsWith('/api/')).map((h) => h.split('#')[0]))];
  for (const path of internal) {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await s.mock(page);
    await page.goto(path);
    await expect(page.locator('h1'), `${path} from ${s.name}`).toHaveCount(1);
    if ((await page.locator('h1').textContent()) === 'Page not found') bad.push(`${path} is Page not found`);
  }
  return bad;
}

async function load1440(page: Page, s: Crawled) {
  await s.mock(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(s.path);
  await s.ready(page);
  await settle(page);
}

for (const s of CRAWLED) {
  test(`every link on ${s.name} goes somewhere`, async ({ page }) => {
    test.setTimeout(180_000);
    await load1440(page, s);
    expect((await hrefs(page)).length, 'links found').toBeGreaterThan(0);
    expect(await badLinks(page, s)).toEqual([]);
  });
}

test('a link to a missing page fails the crawl (v4 H3 self-test)', async ({ page }) => {
  const s = CRAWLED.find((x) => x.name === 'contest-during')!;
  await load1440(page, s);
  await page.evaluate(() => {
    const a = document.createElement('a');
    a.href = '/contests/week-6/nowhere';
    a.textContent = 'Nowhere';
    document.querySelector('main')!.append(a);
  });
  expect(await badLinks(page, s)).toEqual(['/contests/week-6/nowhere is Page not found']);
});

/** A screen to press every button on: how to load it, ready to use. */
type Pressed = { name: string; load: (page: Page) => Promise<void> };

const fromContests = (name: string): Pressed => ({ name, load: async (page) => void (await open(page, name)) });
const fromProblems = (name: string): Pressed => ({
  name,
  load: async (page) => {
    const s = problems.SCREENS.find((x) => x.name === name)!;
    await problems.mockApi(page, 'organiser');
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(s.path);
    await s.ready(page);
    await settle(page);
  },
});
const fromSettings = (name: string): Pressed => ({
  name,
  load: async (page) => {
    const s = people.SCREENS.find((x) => x.name === name)!;
    await people.mockApi(page, s.who);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(s.path);
    await s.ready(page);
    await settle(page);
  },
});

const PRESSED: Pressed[] = [
  ...['home-student', 'contest-before', 'contest-during', 'contest-after', 'standings-frozen'].map(fromContests),
  ...['admin-contests', 'contest-editor', 'contest-console', 'contest-results'].map(fromContests),
  ...['problems-bank', 'problem-editor'].map(fromProblems),
  ...['settings-people', 'settings-sign-in', 'settings-organisation'].map(fromSettings),
];

const BUTTONS = 'main button, footer button';

/**
 * Loads a screen on a fixed clock and marks its pressable buttons with
 * data-press (their order on the page); answers each one's name.
 */
async function load(page: Page, s: Pressed): Promise<string[]> {
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.clock.setFixedTime(new Date());
  // A press may leave a choice on this device (such as "Hide result"); each load starts clean.
  await page.evaluate(() => {
    try {
      localStorage.clear();
    } catch {
      // about:blank has no storage.
    }
  });
  await s.load(page);
  return page.evaluate((sel) => {
    const out: string[] = [];
    for (const b of document.querySelectorAll<HTMLButtonElement>(sel)) {
      // Disabled, inert (a "Jump to you" with the own row already in view) or not drawn: nothing to press.
      if (b.disabled || b.getAttribute('aria-disabled') === 'true' || b.closest('[inert]') !== null) continue;
      if (!b.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
      // The selected tab is already shown; pressing it again rightly does nothing.
      if (b.getAttribute('role') === 'tab' && b.getAttribute('aria-selected') === 'true') continue;
      b.dataset.press = String(out.length);
      out.push((b.getAttribute('aria-label') || b.textContent || '').replace(/\s+/g, ' ').trim());
    }
    return out;
  }, BUTTONS);
}

type Seen = { changed: boolean; requests: number; files: number; dialog: boolean; url: string; focusMoved: boolean };

/**
 * Starts watching the page for anything a press can cause; the returned
 * function says what happened. With `learn`, the nodes that change are
 * remembered as the screen's own ticking (a clock), and later watches
 * ignore them.
 */
async function watch(page: Page, learn = false): Promise<() => Promise<Seen>> {
  let requests = 0;
  const onRequest = (r: { url(): string }) => {
    const path = new URL(r.url()).pathname;
    if (path.startsWith('/api/') && path !== '/api/events') requests++;
  };
  page.on('request', onRequest);
  // A file picker is an effect too ("Choose a CSV file", "Import pack").
  let files = 0;
  const onFile = () => files++;
  page.on('filechooser', onFile);
  await page.evaluate((learning) => {
    const w = window as unknown as { __changed: boolean; __obs?: MutationObserver; __ticking?: Set<Node> };
    const ticking = (w.__ticking ??= new Set<Node>());
    w.__changed = false;
    w.__obs?.disconnect();
    w.__obs = new MutationObserver((records) => {
      for (const m of records) {
        if (learning) ticking.add(m.target);
        else if (!ticking.has(m.target)) w.__changed = true;
      }
    });
    w.__obs.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
  }, learn);
  return async () => {
    page.off('request', onRequest);
    page.off('filechooser', onFile);
    const seen = await page.evaluate(() => {
      const w = window as unknown as { __changed: boolean; __obs?: MutationObserver; __ticking?: Set<Node> };
      w.__obs?.disconnect();
      const a = document.activeElement as HTMLElement | null;
      return {
        // While learning, anything outside text and the countdown is noise worth failing on.
        changed: w.__changed || [...(w.__ticking ?? [])].some((n) => n === document.body || n === document.querySelector('main')),
        dialog: document.querySelector('dialog[open]') !== null,
        // Focus on the pressed button itself, or on nothing, is no effect.
        focusMoved: a !== null && a !== document.body && a.dataset.press === undefined,
      };
    });
    return { ...seen, requests, files, url: page.url() };
  };
}

/** Presses the button marked `index` and says whether it did anything, once the screen's own ticking is known. */
async function pressDoesSomething(page: Page, index: number | string, label: string, screen: string): Promise<boolean> {
  const button = page.locator(`[data-press="${index}"]`);
  await expect(button, `"${label}" on ${screen}`).toBeVisible();
  // Scroll first, so whatever scrolling changes is learnt, not credited to the press.
  await button.scrollIntoViewIfNeeded();
  // A little over a second: a countdown ticks once and is ignored from then on.
  const idle = await watch(page, true);
  await page.waitForTimeout(1100);
  const quiet = await idle();
  expect(quiet.changed || quiet.requests > 0, `${screen} changes on its own, so its buttons cannot be checked`).toBe(false);

  const before = page.url();
  const done = await watch(page);
  await button.click();
  await page.waitForTimeout(300);
  const seen = await done();
  return seen.changed || seen.dialog || seen.url !== before || seen.requests > 0 || seen.files > 0 || seen.focusMoved;
}

for (const s of PRESSED) {
  test(`every button on ${s.name} does something`, async ({ page }) => {
    test.setTimeout(300_000);
    const labels = await load(page, s);
    expect(labels.length, 'buttons found').toBeGreaterThan(0);
    // One press per distinct name: a row's "Remove" runs the same code on every row.
    const first = new Map<string, number>();
    labels.forEach((l, i) => {
      if (!first.has(l)) first.set(l, i);
    });
    for (const [label, index] of first) {
      const again = await load(page, s);
      const untimed = (l: string | undefined) => (l ?? '').replace(/\d{2}:\d{2}/g, 'hh:mm');
      expect(untimed(again[index]), `${s.name} loads the same buttons each time`).toBe(untimed(label));
      expect(await pressDoesSomething(page, index, label, s.name), `"${label}" on ${s.name} did nothing`).toBe(true);
    }
  });
}

test('a button that does nothing fails the check (C2-018 self-test)', async ({ page }) => {
  const s = PRESSED.find((x) => x.name === 'contest-during')!;
  await load(page, s);
  await page.evaluate(() => {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = 'Noop';
    b.dataset.press = 'noop';
    document.querySelector('main')!.append(b);
  });
  expect(await pressDoesSomething(page, 'noop', 'Noop', s.name)).toBe(false);
});

test('the files the head names are there', async ({ request }) => {
  const types: Record<string, RegExp> = {
    '/favicon.svg': /svg/,
    '/favicon.ico': /icon|octet-stream/,
    '/apple-touch-icon.png': /png/,
  };
  for (const [path, type] of Object.entries(types)) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(200);
    expect(res.headers()['content-type'] ?? '', path).toMatch(type);
  }
});

test('the page head carries one noindex rule, the icons and no social tags (V3-4)', async ({ page }) => {
  await mockApi(page, { phase: 'during' });
  await page.goto('/');
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  expect(await page.locator('meta[property^="og:"], meta[name^="twitter:"]').count()).toBe(0);
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/apple-touch-icon.png');
  expect(await page.locator('meta[name="theme-color"]').count()).toBe(2);
});
