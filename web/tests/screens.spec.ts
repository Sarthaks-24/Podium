// Screen sweep for the M1 identity screens. Every /api/ call is answered by
// an in-test mock, one per page, so each test starts from the same fixtures:
// signed out, a student, or an owner with three organisers (one locked) and
// two allowed domains.
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m1/<screen>-<width>-<theme>.png. Further tests cover the
// keyboard path through /admin/people, the confirm and add flows, the
// sign-in redirects and the small-screen menu.
import { expect, test, type Page, type Route } from '@playwright/test';
import { activate } from './keyboard';

/** cache/screenshots-m1/ at the repository root (git-ignored), with a trailing slash. */
function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m1/`;
}
const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

type Who = 'signed-out' | 'student' | 'owner';

const OWNER = { id: 1, email: 'priya@college.edu', name: 'Priya Raman', avatar_url: '', role: 'owner' };
const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student' };

const PEOPLE = [
  { id: 1, email: 'priya@college.edu', name: 'Priya Raman', role: 'owner', last_login_at: 1792183192359, locked: true },
  { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', role: 'admin', last_login_at: 1791923992359, locked: false },
  { id: 3, email: 'ta@college.edu', name: '', role: 'admin', last_login_at: null, locked: false },
];

const DOMAINS = [
  { domain: 'college.edu', created_at: 1791000000000 },
  { domain: 'cs.college.edu', created_at: 1791500000000 },
];

const CONFIG = { allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' };

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: body === undefined ? '' : JSON.stringify(body),
  });
}

function apiError(route: Route, status: number, code: string, message: string) {
  return json(route, status, { error: { code, message } });
}

/** Installs the API mock for one page and returns its mutable state. */
async function mockApi(page: Page, who: Who) {
  const state = {
    who,
    people: structuredClone(PEOPLE),
    domains: structuredClone(DOMAINS),
    nextId: 10,
    resets: [] as string[],
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      const me = state.who === 'owner' ? OWNER : state.who === 'student' ? STUDENT : null;

      if (path === '/api/auth/config' && method === 'GET') return json(route, 200, CONFIG);
      if (path === '/api/auth/logout' && method === 'POST') {
        state.who = 'signed-out';
        return json(route, 204);
      }
      if (path === '/api/me' && method === 'GET') {
        return me ? json(route, 200, me) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      }
      if (path === '/api/contests' && method === 'GET') {
        return me ? json(route, 200, { live: [], upcoming: [], past: [] }) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      }
      if (path.startsWith('/api/admin/')) {
        if (!me) return apiError(route, 401, 'signed_out', 'Sign in to continue.');
        if (me.role !== 'owner') return apiError(route, 403, 'forbidden', 'Only owners can do that.');
      }
      if (path === '/api/admin/people' && method === 'GET') {
        return json(route, 200, { people: state.people, domains: state.domains });
      }
      if (path === '/api/admin/people' && method === 'POST') {
        const body = req.postDataJSON() as { email: string; role: string };
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(body.email)) {
          return apiError(route, 400, 'bad_request', 'Enter a full email address, for example ta@college.edu.');
        }
        const person = { id: state.nextId++, email: body.email, name: '', role: body.role, last_login_at: null, locked: false };
        state.people.push(person);
        return json(route, 200, person);
      }
      const personMatch = path.match(/^\/api\/admin\/people\/(\d+)$/);
      if (personMatch && method === 'DELETE') {
        const id = Number(personMatch[1]);
        const person = state.people.find((p) => p.id === id);
        if (!person) return apiError(route, 404, 'not_found', 'That person is not an organiser.');
        if (person.locked) return apiError(route, 409, 'conflict', 'The configured owner cannot be changed here.');
        state.people = state.people.filter((p) => p.id !== id);
        return json(route, 204);
      }
      if (path === '/api/admin/domains' && method === 'POST') {
        const raw = (req.postDataJSON() as { domain: string }).domain.toLowerCase().replace(/^@/, '');
        if (!/^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(raw)) {
          return apiError(route, 400, 'bad_request', 'Enter a domain like college.edu.');
        }
        const existing = state.domains.find((d) => d.domain === raw);
        if (existing) return json(route, 200, existing);
        const added = { domain: raw, created_at: Date.now() };
        state.domains.push(added);
        return json(route, 201, added);
      }
      if (path === '/api/admin/accounts/reset-google' && method === 'POST') {
        const type = (await req.headerValue('content-type')) ?? '';
        if (!type.startsWith('application/json')) {
          return apiError(route, 415, 'unsupported_media_type', 'Send the request as JSON.');
        }
        const target = (req.postDataJSON() as { email: string }).email;
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(target)) {
          return apiError(route, 400, 'bad_request', 'Enter a full email address, for example ta@college.edu.');
        }
        const known = [...state.people.map((p) => p.email), STUDENT.email];
        if (!known.includes(target)) return apiError(route, 404, 'not_found', 'Nobody with that email has an account.');
        state.resets.push(target);
        return json(route, 204);
      }
      const domainMatch = path.match(/^\/api\/admin\/domains\/(.+)$/);
      if (domainMatch && method === 'DELETE') {
        const domain = decodeURIComponent(domainMatch[1]);
        if (!state.domains.some((d) => d.domain === domain)) {
          return apiError(route, 404, 'not_found', 'That domain is not on the list.');
        }
        state.domains = state.domains.filter((d) => d.domain !== domain);
        return json(route, 204);
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

interface Screen {
  name: string;
  path: string;
  who: Who;
  title: string;
  /** Something that is only on the page once it has finished loading. */
  ready: (page: Page) => Promise<void>;
}

const SCREENS: Screen[] = [
  {
    name: 'signin-error-account',
    path: '/signin?error=account',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByRole('alert')).toContainText('This email is linked to a different Google account.'),
  },
  {
    name: 'signin',
    path: '/signin',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByText('Sign in with your @college.edu Google account.')).toBeVisible(),
  },
  {
    name: 'signin-error-domain',
    path: '/signin?error=domain',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByRole('alert')).toContainText('This account is not on the allowed list.'),
  },
  {
    name: 'home-owner',
    path: '/',
    who: 'owner',
    title: 'Your contests · podium',
    ready: (page) => expect(page.getByText('No contests yet.')).toBeVisible(),
  },
  {
    name: 'people-owner',
    path: '/admin/people',
    who: 'owner',
    title: 'People · podium',
    ready: (page) => expect(page.getByRole('table')).toBeVisible(),
  },
  {
    name: 'people-student',
    path: '/admin/people',
    who: 'student',
    title: 'People · podium',
    ready: (page) => expect(page.getByText('Only owners can manage people.')).toBeVisible(),
  },
  {
    name: 'not-found',
    path: '/no-such-page',
    who: 'owner',
    title: 'Page not found · podium',
    ready: (page) => expect(page.getByRole('link', { name: 'Go to your contests.' })).toBeVisible(),
  },
];

test('each route has its own title', () => {
  const byPath = new Map<string, string>();
  for (const s of SCREENS) byPath.set(s.path.split('?')[0], s.title);
  expect(new Set(byPath.values()).size).toBe(byPath.size);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(`${screen.name}`, async ({ page }) => {
        await mockApi(page, screen.who);
        await page.setViewportSize({ width: WIDTHS[WIDTHS.length - 1], height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        await page.evaluate(() => document.fonts.ready);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));

          const overflow = await page.evaluate(() => ({
            scroll: document.documentElement.scrollWidth,
            client: document.documentElement.clientWidth,
          }));
          expect(overflow.scroll, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(overflow.client);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page).toHaveTitle(screen.title);

          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

/** Describes the focused element and whether its focus indicator is visible. */
function focusInfo(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    const style = getComputedStyle(el);
    const outlineVisible =
      style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0 && !/rgba\(.*,\s*0\)$/.test(style.outlineColor);
    return {
      name: (el.getAttribute('aria-label') ?? el.textContent ?? '').trim(),
      tag: el.tagName.toLowerCase(),
      visible: style.boxShadow !== 'none' || outlineVisible,
    };
  });
}

for (const width of [1440, 375]) {
  test(`keyboard reaches every control on /admin/people at ${width}px`, async ({ page }) => {
    await mockApi(page, 'owner');
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/people');
    await expect(page.getByRole('table')).toBeVisible();

    const reached: string[] = [];
    for (let i = 0; i < 40; i++) {
      await page.keyboard.press('Tab');
      const info = await focusInfo(page);
      if (info === null) break;
      expect(info.visible, `no visible focus on ${info.tag} "${info.name}"`).toBe(true);
      reached.push(info.name);
      if (info.name === 'Reset link') break;
    }

    for (const name of ['Add organiser', 'Remove Arjun Mehta', 'Remove ta@college.edu', 'Remove @college.edu', 'Remove @cs.college.edu', 'Add domain', 'Reset link']) {
      expect(reached, `Tab did not reach "${name}"`).toContain(name);
    }
    expect(reached).not.toContain('Remove Priya Raman');
  });
}

test('removing an organiser asks first and confirms with a toast', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/admin/people');
  await page.getByRole('button', { name: 'Remove Arjun Mehta' }).click();

  const dialog = page.getByRole('dialog', { name: 'Remove Arjun Mehta as admin?' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('They keep their account and become a student.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.screenshot({ path: `${shots()}people-owner-confirm-1280-light.png` });

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  await page.getByRole('button', { name: 'Remove Arjun Mehta' }).click();
  await dialog.getByRole('button', { name: 'Remove' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Removed Arjun Mehta', { exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'arjun.mehta@college.edu', exact: true })).toHaveCount(0);
  await expect(page.getByText('Set in server configuration')).toBeVisible();
});

test('adding an organiser shows server errors inline and success as a toast', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/admin/people');
  const email = page.getByRole('region', { name: 'Organisers' }).getByLabel('Email', { exact: true });

  await page.getByRole('button', { name: 'Add organiser' }).click();
  await expect(page.locator('#person-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-describedby', 'person-error');
  await expect(email).toHaveAttribute('aria-invalid', 'true');

  await email.fill('not-an-email');
  await page.getByRole('button', { name: 'Add organiser' }).click();
  await expect(page.locator('#person-error')).toHaveText('Enter a full email address, for example ta@college.edu.');

  await email.fill('new.ta@college.edu');
  await page.getByLabel('Role', { exact: true }).selectOption('owner');
  await page.getByRole('button', { name: 'Add organiser' }).click();
  await expect(page.getByText('Added new.ta@college.edu as owner')).toBeVisible();
  await expect(page.locator('#person-error')).toHaveCount(0);
  await expect(page.getByRole('cell', { name: 'new.ta@college.edu', exact: true })).toBeVisible();
  await expect(email).toHaveValue('');
});

test('domains can be added and removed', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/admin/people');

  await page.getByLabel('Domain', { exact: true }).fill('@Alumni.College.edu');
  await page.getByRole('button', { name: 'Add domain' }).click();
  await expect(page.getByText('Added @alumni.college.edu')).toBeVisible();

  await page.getByRole('button', { name: 'Remove @cs.college.edu' }).click();
  const dialog = page.getByRole('dialog', { name: 'Remove @cs.college.edu from the allowed list?' });
  await expect(dialog).toContainText(
    'People with @cs.college.edu addresses will no longer be able to sign in. Existing organisers are not affected.',
  );
  await dialog.getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText('Removed @cs.college.edu')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Remove @cs.college.edu' })).toHaveCount(0);
});

test('resetting a Google account link asks first and reports errors inline', async ({ page }) => {
  const state = await mockApi(page, 'owner');
  await page.goto('/admin/people');
  const section = page.getByRole('region', { name: 'Google account links' });
  await expect(section).toContainText(
    "Each account is tied to the Google account that first signed in with it. If someone's address moved to a new Google account, reset the link so their next sign-in is accepted.",
  );
  const email = section.getByLabel('Email', { exact: true });
  const submit = section.getByRole('button', { name: 'Reset link' });

  await submit.click();
  await expect(page.locator('#reset-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-describedby', 'reset-error');

  await email.fill('nobody@college.edu');
  await submit.click();
  const dialog = page.getByRole('dialog', { name: 'Reset the Google account link for nobody@college.edu?' });
  await expect(dialog).toContainText(
    'Their current sessions end. The next Google account that signs in with this address is linked to it.',
  );
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  expect(state.resets).toEqual([]);

  await submit.click();
  await dialog.getByRole('button', { name: 'Reset link' }).click();
  await expect(page.locator('#reset-error')).toHaveText('Nobody with that email has an account.');
  await expect(email).toHaveAttribute('aria-invalid', 'true');

  await email.fill('asha@college.edu');
  await submit.click();
  await page.getByRole('dialog', { name: 'Reset the Google account link for asha@college.edu?' })
    .getByRole('button', { name: 'Reset link' })
    .click();
  await expect(page.getByText('Reset the Google account link for asha@college.edu', { exact: true })).toBeVisible();
  await expect(page.locator('#reset-error')).toHaveCount(0);
  await expect(email).toHaveValue('');
  expect(state.resets).toEqual(['asha@college.edu']);
});

test('signed-out visitors are sent to sign in and back', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/admin/people');
  await expect(page).toHaveURL('/signin?next=%2Fadmin%2Fpeople');
  await expect(page.getByRole('link', { name: 'Continue with Google' })).toHaveAttribute(
    'href',
    '/api/auth/google/start?next=%2Fadmin%2Fpeople',
  );
  await expect(page.getByRole('navigation')).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'priya@college.edu' })).toHaveAttribute('href', 'mailto:priya@college.edu');
});

test('signed-in visitors to /signin go home, and sign out returns them', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/signin');
  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Your contests');
  await expect(page.getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');

  await page.getByRole('link', { name: 'People' }).click();
  await expect(page).toHaveURL('/admin/people');
  await expect(page.getByRole('link', { name: 'People' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL('/signin');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests');
});

test('students do not see the People link', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Contests' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'People' })).toHaveCount(0);
  await expect(page.getByText('Asha Rao')).toBeVisible();
});

test('the small-screen menu opens, and Escape closes it', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/');
  const menu = page.getByRole('button', { name: 'Menu' });
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('link', { name: 'People' })).toBeHidden();

  await menu.click();
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('link', { name: 'People' })).toBeVisible();
  await page.screenshot({ path: `${shots()}home-owner-menu-open-375-light.png`, fullPage: true });

  await page.keyboard.press('Escape');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(menu).toBeFocused();

  // With the theme panel open inside the menu, one Escape closes one thing.
  await menu.click();
  const theme = page.getByRole('button', { name: 'Theme' });
  await theme.click();
  await expect(theme).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('Escape');
  await expect(theme).toHaveAttribute('aria-expanded', 'false');
  await expect(theme).toBeFocused();
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('Escape');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(menu).toBeFocused();

  await menu.click();
  await page.getByRole('link', { name: 'People' }).click();
  await expect(page).toHaveURL('/admin/people');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
});

test('keyboard only: sign in, the theme menu and the shortcut list are reachable', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/signin');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
  await activate(page, page.getByRole('link', { name: 'Continue with Google' }), 'Tab');
  await expect(page.getByRole('link', { name: 'Continue with Google' })).not.toBeFocused();

  // The footer button and "?" open the same list; Escape closes it and focus returns.
  const footer = page.getByRole('button', { name: 'Keyboard shortcuts' });
  await activate(page, footer);
  const keys = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(keys).toBeVisible();
  await expect(keys.getByRole('heading', { name: 'Problem editor' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
  await expect(footer).toBeFocused();
  await page.keyboard.press('?');
  await expect(keys).toBeVisible();
  for (const width of WIDTHS) {
    for (const theme of THEMES) {
      await page.emulateMedia({ colorScheme: theme });
      await page.setViewportSize({ width, height: 900 });
      await page.screenshot({ path: `${shots()}shortcuts-${width}-${theme}.png` });
    }
  }
  await activate(page, keys.getByRole('button', { name: 'Close keyboard shortcuts' }));
  await expect(keys).toBeHidden();
});

test('a network failure on start-up is reported', async ({ page }) => {
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    (route) => route.abort('failed'),
  );
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText('Could not reach the server.');
  await page.getByRole('button', { name: 'Dismiss' }).click();
  await expect(page.getByRole('button', { name: 'Dismiss' })).toHaveCount(0);
});
