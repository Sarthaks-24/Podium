// The v2.0.1 stage E theme picker in a real browser: a preset, a custom theme
// built from a background and an accent, the editor colour scheme, and the
// four things the plan names: the first frame after a reload is already right
// (checked with the app's own script blocked, so only public/theme-init.js
// has run), a dark custom theme gets the dark verdict colours (E4b), signing
// out returns the machine to System, and the editor scheme reaches the
// workspace's editor. Every /api/ call is mocked. Screenshots of two presets
// and one custom theme at the four widths go to cache/screenshots-v201-e/.
import { expect, test, type Page, type Route } from '@playwright/test';
import { derive } from '../src/lib/theme';
import { PRESETS } from '../src/lib/theme-presets';
import { ADMIN } from './mocks/contests';
import { mockApi as mockWorkspace, openWorkspace } from './mocks/workspace';

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-v201-e/`;
}

const OWNER = { id: 1, email: 'priya@college.edu', name: 'Priya Raman', avatar_url: '', role: 'owner', theme: '', verdicts: 'standard', editor_colours: 'night', theme_custom: null };

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

async function mockApi(page: Page, me: Record<string, unknown> = OWNER) {
  const state = { signedIn: true, saved: [] as Record<string, unknown>[], me: { ...me } };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (path === '/api/auth/config') return json(route, 200, { contact_email: 'priya@college.edu' });
      if (path === '/api/me' && req.method() === 'GET') {
        return state.signedIn ? json(route, 200, state.me) : json(route, 401, { error: { code: 'signed_out', message: 'Sign in to continue.' } });
      }
      if (path === '/api/me/preferences' && req.method() === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.saved.push(body);
        state.me = { ...state.me, ...body };
        return json(route, 200, body);
      }
      if (path === '/api/auth/logout') {
        state.signedIn = false;
        return json(route, 204);
      }
      if (path === '/api/admin/people') return json(route, 200, { people: [] });
      return json(route, 404, { error: { code: 'not_found', message: 'No mock.' } });
    },
  );
  return state;
}

async function openDialog(page: Page) {
  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Theme and colours' }).click();
  const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
  await expect(dialog).toBeVisible();
  return dialog;
}

const root = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim().toUpperCase(), name);

test('a preset applies, saves and survives a reload with the first frame right', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const html = page.locator('html');
  const dialog = await openDialog(page);

  await dialog.getByRole('radio', { name: /^Nordic slate/ }).check();
  const d = derive(PRESETS.find((p) => p.id === 'nordic-slate')!.input);
  await expect(html).toHaveAttribute('data-theme', 'nordic-slate');
  await expect(html).toHaveAttribute('data-family', 'dark');
  expect(await root(page, '--paper')).toBe(d.vars['--paper'].toUpperCase());
  expect(await root(page, '--act')).toBe(d.vars['--act'].toUpperCase());
  // The verdict colours come from the dark family, not from the preset id.
  expect(await root(page, '--ok')).toBe('#5CC489');
  await expect.poll(() => api.saved.at(-1)).toMatchObject({ theme: 'nordic-slate', verdicts: 'standard', editor_colours: 'night', theme_custom: null });
  await expect(dialog).toContainText('Current theme: Nordic slate');
  await page.keyboard.press('Escape');

  // First frame: with the app's own script blocked, only theme-init.js has run (the stylesheet
  // is loaded by that script in development, so the attributes and the inline variables are what show).
  await page.route(/\/src\/main\.ts/, (r) => r.abort());
  await page.reload();
  await expect(html).toHaveAttribute('data-theme', 'nordic-slate');
  await expect(html).toHaveAttribute('data-family', 'dark');
  expect(await root(page, '--paper')).toBe(d.vars['--paper'].toUpperCase());
});

test('a custom theme is built, saved, and a dark one gets the dark verdicts', async ({ page }) => {
  const api = await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const html = page.locator('html');
  const dialog = await openDialog(page);

  await dialog.getByRole('radio', { name: /^Custom/ }).check();
  await expect(html).toHaveAttribute('data-theme', 'custom');
  await dialog.getByLabel('Background', { exact: true }).fill('#102030');
  await dialog.getByLabel('Accent', { exact: true }).fill('#5cc489');
  const input = { background: '#102030', accent: '#5cc489', contrast: 'standard' as const };
  const d = derive(input);
  await expect.poll(() => api.saved.at(-1)).toMatchObject({ theme: 'custom', theme_custom: input });

  await expect(html).toHaveAttribute('data-family', 'dark');
  expect(await root(page, '--paper')).toBe(d.vars['--paper'].toUpperCase());
  expect(await root(page, '--act')).toBe(d.vars['--act'].toUpperCase());
  // E4b: dark verdicts for a dark custom theme, light ones for a light one.
  expect(await root(page, '--ok')).toBe('#5CC489');
  expect(await root(page, '--bad')).toBe('#F0776B');
  expect(await root(page, 'color-scheme')).toBe('DARK');
  // The accent was the green used for Accepted, so the engine moved it and said so.
  await expect(dialog.getByText('Adjusted so text stays readable')).toBeVisible();
  await expect(dialog.getByText(/Accent moved away from the green used for Accepted/)).toBeVisible();

  await dialog.getByLabel('Background', { exact: true }).fill('#f7f3ea');
  await expect(html).toHaveAttribute('data-family', 'light');
  expect(await root(page, '--ok')).toBe('#137A48');
  expect(await root(page, 'color-scheme')).toBe('LIGHT');

  // High contrast switches the verdicts to the 7:1 set.
  await dialog.getByRole('radio', { name: 'High', exact: true }).check();
  await expect(html).toHaveAttribute('data-contrast', 'high');
  expect(await root(page, '--ok')).toBe('#0A5A2E');

  // A bad hex says what is wrong and changes nothing.
  await dialog.getByLabel('Accent', { exact: true }).fill('#12');
  await expect(dialog.getByText('Use six hex digits after the #, for example 1A2B3C.')).toBeVisible();

  // Back to dark for the reload check.
  await dialog.getByLabel('Accent', { exact: true }).fill('#88b4c8');
  await dialog.getByLabel('Background', { exact: true }).fill('#102030');
  await dialog.getByRole('radio', { name: 'Standard', exact: true }).check();
  const dark = derive({ background: '#102030', accent: '#88b4c8', contrast: 'standard' });
  await expect.poll(() => api.saved.at(-1)).toMatchObject({ theme_custom: { background: '#102030', accent: '#88b4c8', contrast: 'standard' } });
  await page.keyboard.press('Escape');

  await page.route(/\/src\/main\.ts/, (r) => r.abort());
  await page.reload();
  await expect(html).toHaveAttribute('data-theme', 'custom');
  await expect(html).toHaveAttribute('data-family', 'dark');
  expect(await root(page, '--paper')).toBe(dark.vars['--paper'].toUpperCase());
  expect(await root(page, '--act')).toBe(dark.vars['--act'].toUpperCase());
});

test('signing out returns the machine to System and clears every cached key', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const html = page.locator('html');
  const dialog = await openDialog(page);
  await dialog.getByRole('radio', { name: /^Soft lavender/ }).check();
  await dialog.getByRole('radio', { name: /^Paper/ }).check();
  await expect(html).toHaveAttribute('data-editor', 'paper');
  expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-theme') || k === 'podium-editor-colours').sort())).toEqual([
    'podium-editor-colours',
    'podium-theme',
    'podium-theme-vars',
  ]);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/signin/);
  for (const a of ['data-theme', 'data-family', 'data-contrast', 'data-editor', 'data-verdicts']) await expect(html).not.toHaveAttribute(a, /.*/);
  expect(await page.evaluate(() => document.documentElement.getAttribute('style') ?? '')).not.toContain('--paper');
  expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('podium-theme') || k === 'podium-editor-colours'))).toEqual([]);
});

test('reset to default returns to System, Night and the standard verdicts', async ({ page }) => {
  const api = await mockApi(page);
  await page.goto('/');
  const dialog = await openDialog(page);
  await dialog.getByRole('radio', { name: /^Cosmic violet/ }).check();
  await dialog.getByRole('radio', { name: /^Dusk/ }).check();
  await dialog.getByRole('checkbox', { name: /Colour-blind safe verdict colours/ }).check();
  await dialog.getByRole('button', { name: 'Reset to default' }).click();
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', /.*/);
  await expect(page.locator('html')).not.toHaveAttribute('data-editor', /.*/);
  await expect.poll(() => api.saved.at(-1)).toEqual({ theme: '', verdicts: 'standard', editor_colours: 'night', theme_custom: null });
});

test('the editor colour scheme reaches the workspace editor', async ({ page }) => {
  await mockWorkspace(page);
  // The workspace mock signs in an organiser; give that account a saved scheme and answer the save.
  await page.route(
    (url) => url.pathname === '/api/me' || url.pathname === '/api/me/preferences',
    (route) => {
      if (route.request().method() === 'PUT') return json(route, 200, route.request().postDataJSON());
      return json(route, 200, { ...ADMIN, editor_colours: 'paper', theme_custom: null });
    },
  );
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  const html = page.locator('html');
  await expect(html).toHaveAttribute('data-editor', 'paper');
  const bg = () => page.locator('.cm-editor').evaluate((el) => getComputedStyle(el).backgroundColor);
  await expect.poll(bg).toBe('rgb(250, 250, 248)');
  // The text colour inside the editor is the scheme's ink, not the dark scheme's.
  expect(await page.locator('.cm-content').evaluate((el) => getComputedStyle(el).color)).toBe('rgb(27, 31, 35)');

  // Choosing another scheme in the account dialog repaints the open editor.
  await page.getByRole('button', { name: ADMIN.name }).click();
  await page.getByRole('button', { name: 'Theme and colours' }).click();
  await page.getByRole('dialog', { name: 'Theme and colours' }).getByRole('radio', { name: /^Midnight/ }).check();
  await expect(html).toHaveAttribute('data-editor', 'midnight');
  await expect.poll(bg).toBe('rgb(0, 0, 0)');
});

// Screenshots for the owner: two presets (one light, one dark) and a custom
// theme, with the picker open, at the four widths.
const SHOT_WIDTHS = [375, 768, 1024, 1440] as const;
for (const [name, pick] of [
  ['preset-soft-lavender', /^Soft lavender/],
  ['preset-nordic-slate', /^Nordic slate/],
  ['custom', /^Custom/],
] as const) {
  test(`screenshots: ${name}`, async ({ page }) => {
    await mockApi(page);
    for (const width of SHOT_WIDTHS) {
      await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
      await page.goto('/');
      if (width < 768) {
        await page.getByRole('button', { name: 'Menu' }).click();
        await page.locator('#site-menu').getByRole('button', { name: 'Theme and colours' }).click();
      } else {
        await page.getByRole('button', { name: /Priya Raman|Account/ }).click();
        await page.getByRole('button', { name: 'Theme and colours' }).click();
      }
      const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
      await dialog.getByRole('radio', { name: pick }).check();
      if (name === 'custom') {
        await dialog.getByLabel('Background', { exact: true }).fill('#1c2430');
        await dialog.getByLabel('Accent', { exact: true }).fill('#e07a5f');
      }
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: `${shots()}${name}-dialog-${width}.png` });
      await page.keyboard.press('Escape');
      await page.goto('/admin/people');
      await expect(page.locator('h1')).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow, `overflow at ${width}px`).toBeLessThanOrEqual(0);
      await page.screenshot({ path: `${shots()}${name}-page-${width}.png`, fullPage: true });
    }
  });
}
