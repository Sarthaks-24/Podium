// The link crawl and "every button does something" (web-quality #20 and
// #29, P3 FP.5). On the contest mock (tests/mocks/contests.ts), for the
// student and the organiser screens: every link on every screen is
// collected, and each internal one is opened and must not land on "Page
// not found"; mailto: and tel: links must carry an address. Links under
// /api/ (exports, Google sign-in) are full page loads by design and are
// left to the server's own tests. Then, on the student screens and the
// organiser screens (contests, problems, People, students), every visible,
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
import * as organiser from './mocks/organiser';
import * as problems from './mocks/problems';
import * as people from './mocks/screens';

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

/** A screen to press every button on: how to load it, ready to use. */
type Pressed = { name: string; load: (page: Page) => Promise<void> };

const fromContests = (name: string): Pressed => ({ name, load: async (page) => void (await open(page, name)) });
const fromProblems = (name: string): Pressed => ({
  name,
  load: async (page) => {
    const s = problems.SCREENS.find((x) => x.name === name)!;
    await problems.mockApi(page, 'admin');
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(s.path);
    await s.ready(page);
    await settle(page);
  },
});
const fromOrganiser = (name: string): Pressed => ({
  name,
  load: async (page) => {
    const s = organiser.SCREENS.find((x) => x.name === name)!;
    await organiser.mockApi(page, s.options);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(s.path);
    await s.ready(page);
    await settle(page);
  },
});
const fromPeople = (name: string): Pressed => ({
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
  fromPeople('people-owner'),
  ...['students', 'record'].map(fromOrganiser),
];

const BUTTONS = 'main button, footer button';

/**
 * Loads a screen on a fixed clock and marks its pressable buttons with
 * data-press (their order on the page); answers each one's name.
 */
async function load(page: Page, s: Pressed): Promise<string[]> {
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.clock.setFixedTime(new Date());
  await s.load(page);
  return page.evaluate((sel) => {
    const out: string[] = [];
    for (const b of document.querySelectorAll<HTMLButtonElement>(sel)) {
      // Disabled, inert (a "Jump to you" with the own row already in view) or not drawn: nothing to press.
      if (b.disabled || b.getAttribute('aria-disabled') === 'true' || b.closest('[inert]') !== null) continue;
      if (!b.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
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
      expect(again[index], `${s.name} loads the same buttons each time`).toBe(label);
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
