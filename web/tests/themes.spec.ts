// Themes in a real browser (decisions D39 to D43):
// - the browser's cascade gives every theme, with and without the
//   colour-blind safe verdicts, exactly the values lib/colour.ts resolves
//   (which the unit tests check for contrast), and the system theme
//   follows prefers-color-scheme;
// - a signed-in person's account preferences are applied on load, a choice
//   in the menu is saved to the account, the menu works from the keyboard,
//   and signing out clears the theme for the next person at the machine.
// Screenshots of the home and people screens in every theme, and of the
// open menu, go to cache/screenshots-themes/.
import { expect, test, type Page, type Route } from '@playwright/test';
import { COLOUR_TOKENS, parseTokenRules, resolveTheme, type TokenRule } from '../src/lib/colour';
import { THEMES } from '../src/lib/theme';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-themes/`;
}

/** Reads tokens.css as the dev server has it, through Vite's ?raw import. */
async function loadRules(page: Page): Promise<TokenRule[]> {
  const css = await page.evaluate(async () => {
    const url = '/src/styles/tokens.css?raw';
    const mod = await import(/* @vite-ignore */ url);
    return mod.default as string;
  });
  return parseTokenRules(css);
}

const OWNER = {
  id: 1,
  email: 'priya@college.edu',
  name: 'Priya Raman',
  avatar_url: '',
  role: 'owner',
  theme: 'overcast',
  verdicts: 'standard',
};
const PEOPLE = [
  { id: 1, email: 'priya@college.edu', name: 'Priya Raman', role: 'owner', last_login_at: 1792183192359, locked: true },
  { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', role: 'admin', last_login_at: 1791923992359, locked: false },
];

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: body === undefined ? '' : JSON.stringify(body),
  });
}

async function mockApi(page: Page, signedIn: boolean) {
  const state = { signedIn, saved: [] as unknown[], me: { ...OWNER } };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      if (path === '/api/auth/config') return json(route, 200, { allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' });
      if (path === '/api/me' && method === 'GET') {
        return state.signedIn ? json(route, 200, state.me) : json(route, 401, { error: { code: 'signed_out', message: 'Sign in to continue.' } });
      }
      if (path === '/api/me/preferences' && method === 'PUT') {
        const body = req.postDataJSON() as { theme: string; verdicts: string };
        state.saved.push(body);
        state.me = { ...state.me, ...body };
        return json(route, 200, body);
      }
      if (path === '/api/auth/logout') {
        state.signedIn = false;
        return json(route, 204);
      }
      if (path === '/api/admin/people') return json(route, 200, { people: PEOPLE, domains: [{ domain: 'college.edu', created_at: 1791000000000 }] });
      return json(route, 404, { error: { code: 'not_found', message: 'No mock.' } });
    },
  );
  return state;
}

/** Reads the colour tokens the browser computed for <html>. */
function computedTokens(page: Page) {
  return page.evaluate((names) => {
    const style = getComputedStyle(document.documentElement);
    return Object.fromEntries(names.map((n) => [n, style.getPropertyValue(n).trim().toUpperCase()]));
  }, [...COLOUR_TOKENS]);
}

function expected(rules: TokenRule[], id: string, signal: boolean) {
  const t = resolveTheme(rules, id, signal);
  return Object.fromEntries(COLOUR_TOKENS.map((k) => [k, t[k].toUpperCase()]));
}

test.describe('the cascade in tokens.css', () => {
  for (const scheme of ['light', 'dark'] as const) {
    test(`matches the resolved values (system ${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await mockApi(page, false);
      await page.goto('/signin');
      await expect(page.locator('h1')).toBeVisible();
      const rules = await loadRules(page);

      for (const { id } of THEMES) {
        for (const signal of [false, true]) {
          await page.evaluate(
            ([theme, on]) => {
              document.documentElement.setAttribute('data-theme', theme as string);
              if (on) document.documentElement.setAttribute('data-verdicts', 'signal');
              else document.documentElement.removeAttribute('data-verdicts');
            },
            [id, signal] as const,
          );
          expect(await computedTokens(page), `${id} signal=${signal} under system ${scheme}`).toEqual(expected(rules, id, signal));
        }
      }

      // No theme chosen: the system decides between Light and Dark.
      for (const signal of [false, true]) {
        await page.evaluate((on) => {
          document.documentElement.removeAttribute('data-theme');
          if (on) document.documentElement.setAttribute('data-verdicts', 'signal');
          else document.documentElement.removeAttribute('data-verdicts');
        }, signal);
        expect(await computedTokens(page), `system signal=${signal}`).toEqual(expected(rules, scheme, signal));
      }
    });
  }
});

test('applies, saves and clears preferences', async ({ page }) => {
  const api = await mockApi(page, true);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const html = page.locator('html');
  await expect(html).toHaveAttribute('data-theme', 'overcast');

  // Signed in, the picker is the account menu's "Theme and colours" dialog (N2).
  const account = page.getByRole('button', { name: 'Priya Raman' });
  await account.focus();
  await page.keyboard.press('Enter');
  const item = page.getByRole('button', { name: 'Theme and colours' });
  await item.focus();
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
  await expect(dialog).toBeVisible();
  const overcast = page.getByRole('radio', { name: 'Overcast' });
  await expect(overcast).toBeChecked();

  // Arrow keys move within the radio group and choose as they go.
  await overcast.focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('radio', { name: 'OLED black' })).toBeChecked();
  await expect(html).toHaveAttribute('data-theme', 'oled-black');
  await expect.poll(() => api.saved.at(-1)).toEqual({ theme: 'oled-black', verdicts: 'standard' });

  await page.getByRole('checkbox', { name: /Colour-blind safe verdict colours/ }).check();
  await expect(html).toHaveAttribute('data-verdicts', 'signal');
  await expect.poll(() => api.saved.at(-1)).toEqual({ theme: 'oled-black', verdicts: 'signal' });
  expect(await page.evaluate(() => [localStorage.getItem('podium-theme'), localStorage.getItem('podium-verdicts')])).toEqual([
    'oled-black',
    'signal',
  ]);

  // The dialog says which theme is on; Escape closes it and focus returns to the item.
  await expect(dialog).toContainText('Current theme: OLED black');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(item).toBeFocused();

  // Reload: the pre-paint cache and the account agree.
  await page.reload();
  await expect(html).toHaveAttribute('data-theme', 'oled-black');
  await expect(html).toHaveAttribute('data-verdicts', 'signal');

  // System clears the attribute and is saved as an empty theme.
  await account.click();
  await item.click();
  await page.getByRole('radio', { name: /^System/ }).check();
  await expect(html).not.toHaveAttribute('data-theme', /.*/);
  await expect.poll(() => api.saved.at(-1)).toEqual({ theme: '', verdicts: 'signal' });

  // Sign out: the next person at this machine starts from the defaults.
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/signin/);
  await expect(html).not.toHaveAttribute('data-verdicts', /.*/);
  expect(await page.evaluate(() => [localStorage.getItem('podium-theme'), localStorage.getItem('podium-verdicts')])).toEqual([
    null,
    null,
  ]);
});

test('a click outside closes the menu', async ({ page }) => {
  await mockApi(page, false);
  await page.goto('/signin');
  const trigger = page.getByRole('button', { name: 'Theme', exact: true });
  await trigger.click();
  await expect(page.getByRole('radio', { name: 'Nordic frost' })).toBeVisible();
  await page.locator('h1').click();
  await expect(page.getByRole('radio', { name: 'Nordic frost' })).toBeHidden();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
});

test('tabbing out of the menu closes it', async ({ page }) => {
  await mockApi(page, false);
  await page.goto('/signin');
  await page.getByRole('button', { name: 'Theme', exact: true }).click();
  await page.getByRole('checkbox', { name: /Colour-blind safe verdict colours/ }).focus();
  await page.keyboard.press('Tab');
  await expect(page.locator('#theme-panel')).toBeHidden();
});

test('signed out, a choice applies on this device without saving', async ({ page }) => {
  const api = await mockApi(page, false);
  await page.goto('/signin');
  await page.getByRole('button', { name: 'Theme', exact: true }).click();
  await page.getByRole('radio', { name: 'Soft lavender' }).check();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'soft-lavender');
  expect(api.saved).toEqual([]);
});

test.describe('screens in every theme', () => {
  for (const { id } of THEMES) {
    test(id, async ({ page }) => {
      const api = await mockApi(page, true);
      api.me = { ...api.me, theme: id };
      for (const [route, name] of [['/admin/people', 'people']] as const) {
        for (const width of [375, 1024]) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.goto(route);
          await expect(page.locator('html')).toHaveAttribute('data-theme', id);
          await expect(page.locator('h1')).toBeVisible();
          await page.evaluate(() => document.fonts.ready);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `overflow at ${width}px`).toBeLessThanOrEqual(0);
          await page.screenshot({ path: `${shots()}${name}-${width}-${id}.png`, fullPage: true });
        }
      }
    });
  }
});

for (const width of [375, 1440]) {
  test(`the open menu fits at ${width}px`, async ({ page }) => {
    await mockApi(page, true);
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.goto('/');
    if (width < 768) {
      await page.getByRole('button', { name: 'Menu' }).click();
      await page.locator('#site-menu').getByRole('button', { name: 'Theme and colours' }).click();
    } else {
      await page.getByRole('button', { name: 'Priya Raman' }).click();
      await page.getByRole('button', { name: 'Theme and colours' }).click();
    }
    const panel = page.getByRole('dialog', { name: 'Theme and colours' });
    await expect(panel).toBeVisible();
    const box = await panel.boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= width, 'panel inside the viewport').toBe(true);
    await page.screenshot({ path: `${shots()}menu-open-${width}.png` });
  });
}
