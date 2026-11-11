// Keep me signed in (v2.1.0, PF-14): the sign-in page's checkbox, the account
// menu's switch for organisers and above, the one-time notice, and the owner's
// "Staying signed in" section of the Dashboard's Sign-in tab. The API is mocked
// (tests/mocks/screens.ts); the server's rules are tested in Go.
//
// Each screen is checked at 375, 768, 1024 and 1440 px in light and dark: no
// sideways scroll, axe (WCAG 2.2 A and AA) clean, a screenshot saved to
// PODIUM_SHOTS (default cache/screenshots-v210-b/). The keyboard paths use
// Tab, Space, the arrow keys and Enter only.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { tabTo } from './keyboard';
import * as screens from './mocks/screens';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shot(name: string): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/screenshots-v210-b/`;
  return `${dir}${name}.png`;
}

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
}

/** At every width and theme: no sideways scroll, no axe violation, a screenshot. */
async function sweep(page: Page, name: string, theme: string, prepare?: () => Promise<void>) {
  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await prepare?.();
    await settle(page);
    const overflow = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
    expect(overflow.scroll, `sideways scroll at ${width}px`).toBeLessThanOrEqual(overflow.client);
    const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
    expect(results.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`), `axe at ${width}px`).toEqual([]);
    await page.screenshot({ path: shot(`${name}-${width}-${theme}`), fullPage: true });
  }
}

/** Opens the account menu: the small-screen menu for organisers on a phone, the popover elsewhere. */
async function openAccount(page: Page) {
  const menu = page.getByRole('button', { name: 'Menu' });
  if (await menu.isVisible()) {
    if ((await menu.getAttribute('aria-expanded')) !== 'true') await menu.click();
    return;
  }
  const account = page.getByRole('button', { name: /^(Account|Bilal Khan|Asha Rao)$/ }).first();
  if ((await account.getAttribute('aria-expanded')) !== 'true') await account.click();
}

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('sign-in page: the checkbox shows only when students choose, with its help', async ({ page }) => {
      const state = await screens.mockApi(page, 'signed-out');
      await page.goto('/signin');
      await expect(page.getByRole('link', { name: 'Continue with Google' })).toBeVisible();
      await expect(page.getByRole('checkbox', { name: 'Keep me signed in' })).toHaveCount(0);
      await sweep(page, 'signin-without-tick', theme);

      state.keep.policy = { ...state.keep.policy, students: 'choose' };
      await page.goto('/signin?next=%2Fcontests%2Fweek-6');
      const tick = page.getByRole('checkbox', { name: 'Keep me signed in' });
      await expect(tick).toBeVisible();
      await expect(tick).not.toBeChecked();
      await expect(page.locator('#keep-help')).toHaveText(
        'Stay signed in on this device for 30 days after you last use Podium. Leave this off on a lab or shared computer.',
      );
      await expect(tick).toHaveAttribute('aria-describedby', 'keep-help');
      const link = page.getByRole('link', { name: 'Continue with Google' });
      await expect(link).toHaveAttribute('href', '/api/auth/google/start?next=%2Fcontests%2Fweek-6');
      await tick.check();
      await expect(link).toHaveAttribute('href', '/api/auth/google/start?next=%2Fcontests%2Fweek-6&remember=1');
      await tick.uncheck();
      await expect(link).toHaveAttribute('href', '/api/auth/google/start?next=%2Fcontests%2Fweek-6');
      await sweep(page, 'signin-with-tick', theme);
    });

    test('sign-in page: the checkbox comes before the button in tab order and keyboard ticks it', async ({ page }) => {
      const state = await screens.mockApi(page, 'signed-out');
      state.keep.policy = { ...state.keep.policy, students: 'choose', until_sign_out: true };
      await page.goto('/signin');
      const tick = page.getByRole('checkbox', { name: 'Keep me signed in' });
      await expect(page.locator('#keep-help')).toHaveText('Stay signed in on this device until you sign out. Leave this off on a lab or shared computer.');
      await tabTo(page, tick);
      await page.keyboard.press('Space');
      await expect(tick).toBeChecked();
      await page.keyboard.press('Tab');
      const link = page.getByRole('link', { name: 'Continue with Google' });
      await expect(link).toBeFocused();
      await expect(link).toHaveAttribute('href', /&remember=1$/);
    });

    test('sign-in page in testing mode: the same checkbox, sent with the form', async ({ page }) => {
      const state = await screens.mockApi(page, 'signed-out');
      state.keep.policy = { ...state.keep.policy, students: 'choose' };
      await page.route('**/api/auth/config', (route) =>
        route.fulfill({ json: { contact_email: '', mode: 'testing', signin_open: true, keep_signed_in: { offer_tick: true, days: 30, until_sign_out: false } } }),
      );
      const posts: Record<string, unknown>[] = [];
      await page.route('**/api/auth/test-signin', (route) => {
        posts.push(route.request().postDataJSON() as Record<string, unknown>);
        return route.fulfill({ json: { next: '/' } });
      });
      await page.goto('/signin');
      const tick = page.getByRole('checkbox', { name: 'Keep me signed in' });
      await expect(tick).toBeVisible();
      await page.getByLabel('Email').fill('asha@college.edu');
      await page.getByRole('button', { name: 'Sign in for testing' }).click();
      await expect.poll(() => posts.length).toBe(1);
      expect(posts[0].remember).toBe(false);
      await page.goto('/signin');
      await page.getByLabel('Email').fill('asha@college.edu');
      await page.getByRole('checkbox', { name: 'Keep me signed in' }).check();
      await page.getByRole('button', { name: 'Sign in for testing' }).click();
      await expect.poll(() => posts.length).toBe(2);
      expect(posts[1].remember).toBe(true);
    });

    test('a tick that was not used is told once, on the first page', async ({ page, context }) => {
      await screens.mockApi(page, 'student');
      await context.addCookies([{ name: 'podium_notice', value: 'keep_ignored', url: 'http://127.0.0.1:5179/' }]);
      await page.goto('/');
      await expect(page.getByText('Podium does not keep your account signed in, so the tick was not used.')).toBeVisible();
      await expect(page.getByText('You are signed out when the browser closes or after 12 hours.', { exact: false })).toBeVisible();
      expect((await context.cookies()).some((c) => c.name === 'podium_notice')).toBe(false);
      await page.reload();
      await expect(page.getByRole('link', { name: 'Contests' }).first()).toBeVisible();
      await expect(page.getByText('so the tick was not used')).toHaveCount(0);
    });

    test('account menu: the switch shows for an organiser whose role chooses, and turns the session on and off', async ({ page }) => {
      const state = await screens.mockApi(page, 'admin');
      state.keep.meMode = 'choose';
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/contests');
      await openAccount(page);
      const sw = page.getByRole('checkbox', { name: 'Keep me signed in on this device' });
      await expect(sw).toBeVisible();
      await expect(sw).not.toBeChecked();
      await expect(page.locator('#account-keep-help-panel')).toHaveText(
        'Stay signed in on this device for 30 days after you last use Podium. Leave this off on a lab or shared computer.',
      );
      await expect(sw).toHaveAttribute('aria-describedby', 'account-keep-help-panel');
      await sweep(page, 'account-menu-off', theme, () => openAccount(page));

      await sw.check();
      await expect(sw).toBeChecked();
      await expect(sw).toBeFocused();
      expect(state.keep.meKeepPuts).toEqual([true]);
      await expect(page.getByText('Saved')).toBeVisible();
      await sweep(page, 'account-menu-on', theme, () => openAccount(page));
      await sw.uncheck();
      await expect(sw).not.toBeChecked();
      await expect(sw).toBeFocused();
      expect(state.keep.meKeepPuts).toEqual([true, false]);
    });

    test('account menu: the switch is reached and set by keyboard', async ({ page }) => {
      const state = await screens.mockApi(page, 'admin');
      state.keep.meMode = 'choose';
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/contests');
      await tabTo(page, page.getByRole('button', { name: 'Bilal Khan', exact: true }));
      await page.keyboard.press('Enter');
      const sw = page.getByRole('checkbox', { name: 'Keep me signed in on this device' });
      await tabTo(page, sw);
      await page.keyboard.press('Space');
      await expect(sw).toBeChecked();
      expect(state.keep.meKeepPuts).toEqual([true]);
      await page.keyboard.press('Escape');
      await expect(page.locator('#account-panel')).toBeHidden();
    });

    test('account menu: no switch when the role is not set to choose, or for a student', async ({ page }) => {
      const state = await screens.mockApi(page, 'admin');
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/contests');
      await openAccount(page);
      await expect(page.locator('#account-panel').getByText('Signed in as boss@college.edu')).toBeVisible();
      await expect(page.getByRole('checkbox', { name: 'Keep me signed in on this device' })).toHaveCount(0);
      state.keep.meMode = 'everyone';
      await page.reload();
      await openAccount(page);
      await expect(page.locator('#account-panel').getByText('Signed in as boss@college.edu')).toBeVisible();
      await expect(page.getByRole('checkbox', { name: 'Keep me signed in on this device' })).toHaveCount(0);

      const student = await screens.mockApi(page, 'student');
      student.keep.meMode = 'choose';
      await page.goto('/');
      await openAccount(page);
      await expect(page.locator('#account-panel').getByText('Signed in as asha@college.edu')).toBeVisible();
      await expect(page.getByRole('checkbox', { name: 'Keep me signed in on this device' })).toHaveCount(0);
    });

    test('Dashboard: the owner sees every role on Nobody, and the days are not used', async ({ page }) => {
      await screens.mockApi(page, 'owner');
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      await expect(section).toBeVisible();
      for (const role of ['Students', 'Organisers', 'Admins', 'Owner']) {
        const group = section.getByRole('group', { name: role, exact: true });
        await expect(group.getByRole('radio', { name: 'Nobody' })).toBeChecked();
        await expect(group.getByRole('radio')).toHaveCount(3);
      }
      await expect(section.getByRole('radio', { name: 'Sign out after a number of days without use' })).toBeDisabled();
      await expect(section.getByRole('radio', { name: 'Only when they sign out' })).toBeDisabled();
      await expect(section.getByText('Not used while nobody is kept signed in.')).toBeVisible();
      await expect(section.getByText('Nobody: signed out when the browser closes, or after 12 hours.')).toBeVisible();
      await expect(section.getByText('Each person chooses: a checkbox on the sign-in page.')).toBeVisible();
      await expect(section.getByText('Everyone: every sign-in is remembered.')).toBeVisible();
      await sweep(page, 'dashboard-keep-default', theme);
    });

    test('Dashboard: choosing roles shows the warnings and saves the whole setting', async ({ page }) => {
      const state = await screens.mockApi(page, 'owner');
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      await section.getByRole('group', { name: 'Students', exact: true }).getByRole('radio', { name: 'Each person chooses' }).check();
      await expect(section.getByRole('radio', { name: 'Only when they sign out' })).toBeEnabled();
      await expect(section.getByText('On a shared or lab computer')).toHaveCount(0);
      await section.getByRole('group', { name: 'Admins', exact: true }).getByRole('radio', { name: 'Everyone' }).check();
      await expect(section.getByText('A signed-in admin can change contests and paste AI keys. Use this only on your own computer.')).toBeVisible();
      await expect(section.getByText('On a shared or lab computer the next person will be signed in as the last one unless they sign out.')).toBeVisible();
      await section.getByLabel('days without use', { exact: true }).fill('45');
      await sweep(page, 'dashboard-keep-chosen', theme);

      await section.getByRole('button', { name: 'Save' }).click();
      await expect(page.getByText('Saved', { exact: true })).toBeVisible();
      expect(state.keep.puts).toEqual([{ students: 'choose', organisers: 'nobody', admins: 'everyone', owner: 'nobody', days: 45, until_sign_out: false }]);
      await section.getByRole('radio', { name: 'Only when they sign out' }).check();
      await expect(section.getByLabel('days without use', { exact: true })).toBeDisabled();
      await section.getByRole('button', { name: 'Save' }).click();
      await expect.poll(() => state.keep.puts.length).toBe(2);
      expect(state.keep.puts[1]).toMatchObject({ days: 45, until_sign_out: true });
    });

    test('Dashboard: a wrong number of days says what to fix and saves nothing', async ({ page }) => {
      const state = await screens.mockApi(page, 'owner');
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      await section.getByRole('group', { name: 'Students', exact: true }).getByRole('radio', { name: 'Everyone' }).check();
      const days = section.getByLabel('days without use', { exact: true });
      for (const bad of ['0', '366', '', 'ten', '1.5']) {
        await days.fill(bad);
        await section.getByRole('button', { name: 'Save' }).click();
        await expect(section.getByText('Enter a whole number of days from 1 to 365.')).toBeVisible();
        await expect(days).toBeFocused();
        await expect(days).toHaveAttribute('aria-invalid', 'true');
      }
      expect(state.keep.puts).toEqual([]);
      await sweep(page, 'dashboard-keep-error', theme);
    });

    test('Dashboard: switching a role to Nobody asks first and says how many people', async ({ page }) => {
      const state = await screens.mockApi(page, 'owner');
      state.keep.policy = { ...state.keep.policy, students: 'everyone', owner: 'everyone' };
      state.keep.remembered = { students: 40, organisers: 0, admins: 0, owner: 2 };
      state.keep.youRemembered = true;
      state.keep.signedOutByPut = 42;
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      await section.getByRole('group', { name: 'Students', exact: true }).getByRole('radio', { name: 'Nobody' }).check();
      await section.getByRole('group', { name: 'Owner', exact: true }).getByRole('radio', { name: 'Nobody' }).check();
      await section.getByRole('button', { name: 'Save' }).click();
      const dialog = page.getByRole('dialog', { name: 'Sign out 42 people?' });
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText('42 people who chose to stay signed in will be signed out now, you included. They can sign in again.');
      await sweep(page, 'dashboard-keep-confirm', theme);

      await dialog.getByRole('button', { name: 'Cancel' }).click();
      await expect(dialog).toBeHidden();
      expect(state.keep.puts).toEqual([]);
      await section.getByRole('button', { name: 'Save' }).click();
      await page.getByRole('dialog', { name: 'Sign out 42 people?' }).getByRole('button', { name: 'Sign out 42 people' }).click();
      await expect(page.getByText('Saved. 42 people were signed out.')).toBeVisible();
      await expect(section.getByRole('button', { name: 'Save' })).toBeFocused();
      expect(state.keep.puts).toHaveLength(1);
      expect(state.keep.puts[0]).toMatchObject({ students: 'nobody', owner: 'nobody' });
    });

    test('Dashboard: the setting is set by keyboard alone', async ({ page }) => {
      const state = await screens.mockApi(page, 'owner');
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      const students = section.getByRole('group', { name: 'Students', exact: true });
      await tabTo(page, students.getByRole('radio', { name: 'Nobody' }));
      await page.keyboard.press('ArrowDown');
      await expect(students.getByRole('radio', { name: 'Each person chooses' })).toBeChecked();
      await expect(students.getByRole('radio', { name: 'Each person chooses' })).toBeFocused();
      await tabTo(page, section.getByLabel('days without use', { exact: true }));
      await page.keyboard.press('Control+a');
      await page.keyboard.type('14');
      await tabTo(page, section.getByRole('button', { name: 'Save' }));
      await page.keyboard.press('Enter');
      await expect.poll(() => state.keep.puts.length).toBe(1);
      expect(state.keep.puts[0]).toMatchObject({ students: 'choose', days: 14 });
    });

    test('Dashboard: an admin does not see the section, and a failed load offers a retry', async ({ page }) => {
      await screens.mockApi(page, 'admin');
      await page.goto('/admin/dashboard?tab=sign-in');
      await expect(page.getByRole('region', { name: 'Account help' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Staying signed in' })).toHaveCount(0);

      await screens.mockApi(page, 'owner');
      let fail = true;
      await page.route('**/api/admin/keep-signed-in', (route) =>
        fail ? route.fulfill({ status: 500, json: { error: { code: 'server', message: 'Something went wrong. Try again in a minute.' } } }) : route.fallback(),
      );
      await page.goto('/admin/dashboard?tab=sign-in');
      const section = page.getByRole('region', { name: 'Staying signed in' });
      await expect(section.getByRole('alert')).toContainText('Something went wrong');
      fail = false;
      await section.getByRole('button', { name: 'Try again' }).click();
      await expect(section.getByRole('group', { name: 'Students', exact: true })).toBeVisible();
    });
  });
}
