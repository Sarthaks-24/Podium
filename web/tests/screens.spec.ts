// Screen sweep for the M1 identity screens. Every /api/ call is answered by
// an in-test mock, one per page, so each test starts from the same fixtures:
// signed out, a student, or an owner with three organisers (one locked) and
// a student on the sign-in list.
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m1/<screen>-<width>-<theme>.png. Further tests cover the
// keyboard path through the Dashboard tabs at /admin/dashboard, the confirm and
// add flows, each tab's height, the one first-run checklist, the sign-in
// redirects and the small-screen menu.
import { expect, test, type Page } from '@playwright/test';
import { activate } from './keyboard';
import { shots, WIDTHS, THEMES, mockApi, SCREENS } from './mocks/screens';
import { toasts } from './locators';

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

/** Presses Tab until `last` has focus (or 40 times), checking each focus shows; answers the names reached. */
async function tabThrough(page: Page, last: string): Promise<string[]> {
  const reached: string[] = [];
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab');
    const info = await focusInfo(page);
    if (info === null) break;
    expect(info.visible, `no visible focus on ${info.tag} "${info.name}"`).toBe(true);
    reached.push(info.name);
    if (info.name === last) break;
  }
  return reached;
}

for (const width of [1440, 375]) {
  test(`keyboard reaches every control on each Dashboard tab at ${width}px`, async ({ page }) => {
    await mockApi(page, 'owner');
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/dashboard');
    await expect(page.getByRole('table').first()).toBeVisible();

    const people = await tabThrough(page, 'Check the addresses');
    for (const name of ['People', 'Remove Arjun Mehta', 'Remove ta@college.edu', 'Remove Asha Rao', 'Check the addresses']) {
      expect(people, `Tab did not reach "${name}"`).toContain(name);
    }
    expect(people).not.toContain('Remove Priya Raman');
    // One tab stop for the row of tabs; the arrows move between them and show each one.
    expect(people).not.toContain('Sign-in');

    await page.getByRole('tab', { name: 'People' }).focus();
    await page.keyboard.press('ArrowRight');
    await expect(page.getByRole('tab', { name: 'Live' })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(page.getByRole('tab', { name: 'Sign-in' })).toBeFocused();
    await expect(page.getByRole('tab', { name: 'Sign-in' })).toHaveAttribute('aria-selected', 'true');
    await expect(page).toHaveURL('/admin/dashboard?tab=sign-in');
    const signIn = await tabThrough(page, 'Reset Google link');
    for (const name of ['Reset Google link']) {
      expect(signIn, `Tab did not reach "${name}"`).toContain(name);
    }

    await page.getByRole('tab', { name: 'Sign-in' }).focus();
    await page.keyboard.press('End');
    await expect(page.getByRole('tab', { name: 'Organisation' })).toBeFocused();
    const organisation = await tabThrough(page, 'Save organisation details');
    for (const name of ['Save organisation details']) {
      expect(organisation, `Tab did not reach "${name}"`).toContain(name);
    }
  });
}

test('removing someone from the list asks first: a person who signed in is signed out, one who never came is deleted', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/admin/dashboard');
  await page.getByRole('button', { name: 'Remove Asha Rao' }).click();
  const dialog = page.getByRole('dialog', { name: 'Remove Asha Rao from the list?' });
  await expect(dialog).toContainText('They are signed out on every device and cannot sign in again. Their submissions and results stay.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.screenshot({ path: `${shots()}settings-people-confirm-1280-light.png` });
  await dialog.getByRole('button', { name: 'Remove and sign out' }).click();
  await expect(toasts(page).getByText('Removed Asha Rao from the list', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /asha@college\.edu/ })).toContainText('disabled');

  await page.getByRole('button', { name: 'Remove ravi@college.edu' }).click();
  const never = page.getByRole('dialog', { name: 'Remove ravi@college.edu from the list?' });
  await expect(never).toContainText('They have not signed in yet, so their entry is deleted.');
  await never.getByRole('button', { name: 'Remove', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'ravi@college.edu', exact: true })).toHaveCount(0);
  await expect(page.getByText('Set in server configuration')).toBeVisible();
});

test('a pasted list is checked first, then "Add N people" adds only the new addresses (O4)', async ({ page }) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/dashboard');
  const paste = page.getByLabel('Email addresses');
  await page.getByRole('button', { name: 'Check the addresses' }).click();
  await expect(page.locator('#people-add-error')).toHaveText('Enter an email address, or paste a list of them.');
  await expect(paste).toHaveAttribute('aria-invalid', 'true');

  await paste.fill('new1@college.edu, new2@college.edu\nasha@college.edu not-an-email\nnew3@college.edu');
  await page.getByRole('button', { name: 'Check the addresses' }).click();
  const preview = page.getByRole('status').filter({ hasText: 'new addresses' });
  await expect(preview).toContainText('3 new addresses, 1 already on the list, 1 is not an address.');
  await expect(preview).toContainText('Not an address: not-an-email');
  expect(api.imports.at(-1)?.dry_run).toBe(true);
  expect(api.students.map((p) => p.email)).not.toContain('new1@college.edu');
  const origin = new URL(page.url()).origin;
  await expect(preview).toContainText(`Then send them this address: ${origin}.`);
  await preview.getByRole('button', { name: 'Copy address' }).click();
  await expect(toasts(page).getByText('Copied the address', { exact: true })).toBeVisible();

  await preview.getByRole('button', { name: 'Add 3 people' }).click();
  await expect(toasts(page).getByText('Added 3 people to the list', { exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'new2@college.edu', exact: true })).toBeVisible();
  await expect(paste).toHaveValue('');
  await expect(page.getByText(/8 people on the list, 3 signed in, 5 never came\./)).toBeVisible();
});

test('a role changes only after the owner confirms it', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/dashboard');
  const select = page.getByRole('combobox', { name: 'Role for Arjun Mehta' });
  await expect(select.getByRole('option')).toHaveText(['Student', 'Organiser', 'Admin', 'Owner']);
  await select.selectOption('owner');
  const dialog = page.getByRole('dialog', { name: 'Make Arjun Mehta an owner?' });
  await expect(dialog).toContainText('They can do everything, including the organisation details and other admins.');
  // Until confirmed, the row still says what is true.
  await expect(select).toHaveValue('organiser');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(api.people.find((p) => p.id === 2)?.role).toBe('organiser');

  await select.selectOption('owner');
  await dialog.getByRole('button', { name: 'Change role' }).click();
  await expect(toasts(page).getByText('Arjun Mehta is now an owner', { exact: true })).toBeVisible();
  await expect(select).toHaveValue('owner');
  expect(api.people.find((p) => p.id === 2)?.role).toBe('owner');
});

test('an admin gives and takes the organiser role, and never sees admin, owner or their own row as changeable (V4-16)', async ({ page }) => {
  await mockApi(page, 'admin');
  await page.goto('/admin/dashboard');
  await expect(page.getByRole('tab')).toHaveText(['People', 'Live', 'Sign-in', 'Audit']);
  const asha = page.getByRole('combobox', { name: 'Role for Asha Rao' });
  await expect(asha.getByRole('option')).toHaveText(['Student', 'Organiser']);
  await expect(page.getByRole('combobox', { name: 'Role for Bilal Khan' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove Bilal Khan' })).toHaveCount(0);
  await expect(page.getByRole('row', { name: /priya@college\.edu/ }).getByRole('combobox')).toHaveCount(0);

  await asha.selectOption('organiser');
  await page.getByRole('dialog', { name: 'Make Asha Rao an organiser?' }).getByRole('button', { name: 'Change role' }).click();
  await expect(toasts(page).getByText('Asha Rao is now an organiser', { exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Role for Asha Rao' }).selectOption('student');
  const demote = page.getByRole('dialog', { name: 'Make Asha Rao a student?' });
  await expect(demote).toContainText('They are signed out everywhere');
  await demote.getByRole('button', { name: 'Change role' }).click();
  await expect(toasts(page).getByText('Asha Rao is now a student', { exact: true })).toBeVisible();

  await page.getByRole('tab', { name: 'Sign-in' }).click();
  await expect(page.getByRole('button', { name: 'Reset Google link' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'End sessions' })).toBeVisible();
});

test('a refused sign-in is let in with one click', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/admin/dashboard');
  const refused = page.getByRole('region', { name: 'Refused sign-ins' });
  await expect(refused).toContainText('stranger@gmail.com');
  await refused.getByRole('button', { name: 'Let stranger@gmail.com in' }).click();
  await expect(toasts(page).getByText('Let stranger@gmail.com in. They can sign in now.', { exact: true })).toBeVisible();
  await expect(refused).toContainText('Nobody has been refused.');
  await expect(page.getByRole('cell', { name: 'stranger@gmail.com', exact: true })).toBeVisible();
});

test('someone who signed in without a name reads "No name", someone who never came reads "Not signed in yet"', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  api.people.push({ id: 4, email: 'lab@college.edu', name: '', role: 'organiser', last_login_at: 1792183192359, locked: false });
  await page.goto('/admin/dashboard');
  const row = page.getByRole('row', { name: /lab@college\.edu/ });
  await expect(row).toContainText('No name');
  await expect(row).not.toContainText('Not signed in yet');
  await expect(page.getByRole('row', { name: /^Not signed in yet ravi@college/ })).toHaveCount(1);
});

test('resetting a Google account link asks first and reports errors inline', async ({ page }) => {
  const state = await mockApi(page, 'owner');
  await page.goto('/admin/people?tab=sign-in');
  const section = page.getByRole('region', { name: 'Account help' });
  await expect(section).toContainText("Reset Google link when someone's address moved to a new Google account, so their next sign-in is accepted.");
  const email = section.getByLabel('Email', { exact: true });
  const submit = section.getByRole('button', { name: 'Reset Google link' });

  await submit.click();
  await expect(page.locator('#account-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-describedby', 'account-who account-error');

  // The address is looked up first (O10): with no account the actions stay off.
  await email.fill('nobody@college.edu');
  await expect(section.locator('#account-who')).toHaveText('Nobody with that email has an account. Check the address.');
  await expect(submit).toBeDisabled();
  await expect(section.getByRole('button', { name: 'Disable account' })).toBeDisabled();
  expect(state.lookups).toContain('nobody@college.edu');

  // A student is found by the lookup too, and the confirmation names them.
  await email.fill('asha@college.edu');
  await expect(section.locator('#account-who')).toContainText('Asha Rao, student, last signed in');
  await submit.click();
  const dialog = page.getByRole('dialog', { name: 'Reset the Google account link for Asha Rao (asha@college.edu)?' });
  await expect(dialog).toContainText(
    'Their current sessions end. The next Google account that signs in with this address is linked to it.',
  );
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  expect(state.resets).toEqual([]);
  await submit.click();
  await dialog.getByRole('button', { name: 'Reset Google link' }).click();
  await expect(page.getByText('Reset the Google account link for asha@college.edu', { exact: true })).toBeVisible();
  await expect(page.locator('#account-error')).toHaveCount(0);
  await expect(email).toHaveValue('');
  expect(state.resets).toEqual(['asha@college.edu']);
});

test('the account actions wait until the lookup names the account (C2-031)', async ({ page }) => {
  await mockApi(page, 'owner');
  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(
    (url) => url.pathname === '/api/admin/accounts',
    async (route) => {
      await held;
      await route.fallback();
    },
  );
  await page.goto('/admin/people?tab=sign-in');
  const section = page.getByRole('region', { name: 'Account help' });
  await section.getByLabel('Email', { exact: true }).fill('asha@college.edu');
  await expect(section.locator('#account-who')).toHaveText('Looking up the account.');
  for (const name of ['Reset Google link', 'End sessions', 'Disable account']) await expect(section.getByRole('button', { name })).toBeDisabled();
  release();
  await expect(section.locator('#account-who')).toContainText('Asha Rao, student');
  for (const name of ['Reset Google link', 'End sessions', 'Disable account']) await expect(section.getByRole('button', { name })).toBeEnabled();
});

test('an owner ends sessions, disables and enables an account, each asked first', async ({ page }) => {
  const state = await mockApi(page, 'owner');
  await page.goto('/admin/people?tab=sign-in');
  const section = page.getByRole('region', { name: 'Account help' });
  const email = section.getByLabel('Email', { exact: true });
  await expect(section).toContainText('No account is disabled.');

  await section.getByRole('button', { name: 'End sessions' }).click();
  await expect(page.locator('#account-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-invalid', 'true');

  await email.fill('asha@college.edu');
  await expect(section.locator('#account-who')).toContainText('Asha Rao, student');
  await section.getByRole('button', { name: 'End sessions' }).click();
  const end = page.getByRole('dialog', { name: 'End every session of Asha Rao (asha@college.edu)?' });
  await expect(end).toContainText('They are signed out on every device at once');
  await expect(end.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await end.getByRole('button', { name: 'End sessions' }).click();
  await expect(page.getByText('Ended 2 sessions of asha@college.edu', { exact: true })).toBeVisible();

  // The field says whose account it is when the page knows it, and the confirmation names them (P3 O10).
  await email.fill('priya@college.edu');
  await expect(section.locator('#account-who')).toContainText('owner');
  await section.getByRole('button', { name: 'Disable account' }).click();
  await page.getByRole('dialog', { name: /^Disable .+ \(priya@college\.edu\)\?$/ }).getByRole('button', { name: 'Disable account' }).click();
  await expect(page.locator('#account-error')).toHaveText('This owner is set in the server configuration and cannot be disabled.');

  await email.fill('asha@college.edu');
  await expect(section.locator('#account-who')).toContainText('Asha Rao, student');
  await section.getByRole('button', { name: 'Disable account' }).click();
  const disable = page.getByRole('dialog', { name: 'Disable Asha Rao (asha@college.edu)?' });
  await expect(disable).toContainText('cannot sign in until an owner enables the account again');
  await disable.getByRole('button', { name: 'Disable account' }).click();
  await expect(toasts(page).getByText('Disabled asha@college.edu', { exact: true })).toBeVisible();
  await expect(section.getByRole('listitem')).toContainText(['asha@college.edu']);
  await page.screenshot({ path: `${shots()}settings-sign-in-access-1280-light.png`, fullPage: true });

  await section.getByRole('button', { name: 'Enable asha@college.edu' }).click();
  await page.getByRole('dialog', { name: 'Enable asha@college.edu?' }).getByRole('button', { name: 'Enable account' }).click();
  await expect(toasts(page).getByText('Enabled asha@college.edu', { exact: true })).toBeVisible();
  await expect(section).toContainText('No account is disabled.');
  expect(state.accountCalls).toEqual(['end-sessions asha@college.edu', 'disable asha@college.edu', 'enable asha@college.edu']);
});

test('the Audit tab lists and filters the site changes, for owners and admins (O7)', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/dashboard');
  await expect(page.getByRole('table').first()).toBeVisible();
  expect(api.auditQueries).toEqual([]);
  await page.getByRole('tab', { name: 'Audit' }).click();
  await expect(page).toHaveURL('/admin/dashboard?tab=audit');
  const history = page.getByRole('tabpanel', { name: 'Audit' });
  await expect(history.getByRole('listitem')).toHaveCount(3);
  await expect(history.getByRole('listitem').first()).toContainText('Made ta@college.edu an organiser');
  await expect(history.getByRole('listitem').first()).toContainText('priya@college.edu (Owner)');
  await expect(history.getByRole('listitem').last()).toContainText('The server');

  await history.getByLabel('Kind of change').selectOption('list_imported');
  await expect(history.getByRole('listitem')).toHaveCount(1);
  await expect(history).toContainText('Added 12 people to the list');
  await history.getByLabel('Kind of change').selectOption('');
  await history.getByLabel('Email contains').fill('nobody');
  await history.getByRole('button', { name: 'Search' }).click();
  await expect(history).toContainText('No change matches. Clear the filter to see everything.');
  expect(api.auditQueries).toContain('page=1&action=list_imported');
  expect(api.auditQueries).toContain('page=1&q=nobody');

  // An admin reads the same history.
  api.who = 'admin';
  await page.goto('/admin/dashboard?tab=audit');
  await expect(page.getByRole('tabpanel', { name: 'Audit' }).getByRole('listitem')).toHaveCount(3);
});

test('the Sign-in and Organisation tabs at 1440 px are under 1,000 px tall, and one tab shows at a time', async ({ page }) => {
  await mockApi(page, 'owner');
  // A short window, so the page's height is its content's, not the window's (the page fills at least the window).
  await page.setViewportSize({ width: 1440, height: 400 });
  await page.goto('/admin/dashboard');
  await expect(page.getByRole('table').first()).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  for (const [name, path] of [['People', '/admin/dashboard'], ['Sign-in', '/admin/dashboard?tab=sign-in'], ['Organisation', '/admin/dashboard?tab=organisation']] as const) {
    await page.getByRole('tab', { name }).click();
    await expect(page).toHaveURL(path);
    await expect(page.getByRole('tabpanel')).toHaveCount(1);
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    test.info().annotations.push({ type: 'height', description: `${name} tab at 1440 px: ${height} px` });
    // People holds the whole sign-in list, so it grows with the class (V4-15); the other tabs fit a screen.
    if (name !== 'People') expect(height, `${name} tab height`).toBeLessThan(1000);
    await page.screenshot({ path: `${shots()}settings-${name.toLowerCase()}-1440-light.png`, fullPage: true });
  }
  // A reload keeps the tab.
  await page.reload();
  await expect(page.getByRole('tab', { name: 'Organisation' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard');
});

test('the first-contest checklist appears once: on the contests home, never in the Dashboard', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  api.students = [];
  api.people = api.people.filter((p) => p.locked);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const checklist = page.getByRole('region', { name: 'Your first contest' });
  await expect(checklist).toHaveCount(1);
  await expect(checklist.getByRole('listitem').nth(0)).toContainText('To do');
  await expect(checklist.getByRole('listitem').nth(1)).toContainText('Add another organiser in the Dashboard, so someone else can run a contest.');
  await expect(checklist.getByRole('listitem').nth(1)).toContainText('To do');

  await checklist.getByRole('listitem').nth(0).getByRole('link', { name: 'Dashboard' }).click();
  await expect(page).toHaveURL('/admin/dashboard');
  await expect(page.getByRole('tab', { name: 'People' })).toHaveAttribute('aria-selected', 'true');
  for (const tab of ['People', 'Sign-in', 'Organisation']) {
    await page.getByRole('tab', { name: tab }).click();
    await expect(page.getByRole('tabpanel')).toBeVisible();
    await expect(page.getByText('Your first contest')).toHaveCount(0);
    await expect(page.getByText('Getting started')).toHaveCount(0);
    await expect(page.getByText('To do', { exact: true })).toHaveCount(0);
  }
  // A student goes on the list from People; the step is then done.
  await page.getByRole('tab', { name: 'People' }).click();
  await page.getByLabel('Email addresses').fill('asha@college.edu');
  await page.getByRole('button', { name: 'Check the addresses' }).click();
  await page.getByRole('button', { name: 'Add 1 person' }).click();
  await expect(toasts(page).getByText('Added asha@college.edu to the list')).toBeVisible();
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contests' }).click();
  await expect(page.getByRole('region', { name: 'Your first contest' }).getByRole('listitem').nth(0)).toContainText('Done');
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

test('signed-in organisers at /signin land on their contests, and sign out returns them', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/signin');
  await expect(page).toHaveURL('/admin/contests');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Contests');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link', { name: 'Contests' })).toHaveAttribute('aria-current', 'page');
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Problems', 'Dashboard']);

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Dashboard' }).click();
  await expect(page).toHaveURL('/admin/dashboard');
  await expect(nav.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL('/signin');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests');
});

test('students on a phone keep their links and the account button in the row', async ({ page }) => {
  await mockApi(page, 'student');
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Menu' })).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Contests' })).toBeVisible();
  const account = page.getByRole('button', { name: 'Account', exact: true });
  await account.click();
  await expect(page.getByText('Signed in as asha@college.edu')).toBeVisible();
  await expect(page.locator('#account-panel')).toContainText('Student');
  await page.screenshot({ path: `${shots()}home-student-account-375-light.png` });
});

test('the small-screen menu opens, and Escape closes it', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/people');
  const menu = page.getByRole('button', { name: 'Menu' });
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeHidden();

  await menu.click();
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible();
  // The account items sit in the panel itself, not in a second popover.
  await expect(page.locator('#site-menu').getByText('Signed in as priya@college.edu')).toBeVisible();
  await page.screenshot({ path: `${shots()}home-owner-menu-open-375-light.png`, fullPage: true });

  await page.keyboard.press('Escape');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(menu).toBeFocused();

  // The theme dialog opened from the menu takes the first Escape; the menu the second.
  await menu.click();
  await page.locator('#site-menu').getByRole('button', { name: 'Theme and colours' }).click();
  const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('#site-menu').getByRole('button', { name: 'Theme and colours' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
  await expect(menu).toBeFocused();

  await menu.click();
  await page.getByRole('link', { name: 'Problems', exact: true }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
});

test('keyboard only: the account menu opens, its items work, and Escape returns focus', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/people');
  await expect(page.getByRole('table').first()).toBeVisible();
  const account = page.getByRole('button', { name: 'Priya Raman' });
  await account.focus();
  await page.keyboard.press('Enter');
  await expect(account).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('#account-panel')).toContainText('Signed in as priya@college.edu');
  await expect(page.locator('#account-panel')).toContainText('Owner');

  // Tab walks the items in order; each shows its focus.
  const items: string[] = [];
  for (let i = 0; i < 4; i++) {
    await page.keyboard.press('Tab');
    const info = await focusInfo(page);
    expect(info?.visible, `no visible focus on ${info?.name}`).toBe(true);
    items.push(info?.name ?? '');
  }
  // Organisers get "Forget key" for the AI assistant (V3-15).
  expect(items).toEqual(['Theme and colours', 'Help', 'Forget key', 'Sign out']);

  // Theme and colours opens the dialog; closing it lands back on the item.
  for (let i = 0; i < 3; i++) await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading', { name: 'Theme and colours' })).toBeFocused();
  for (const width of [375, 1440]) {
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    const box = await dialog.boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= width, 'dialog inside the viewport').toBe(true);
    await page.screenshot({ path: `${shots()}theme-dialog-${width}-light.png` });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  // P8: the menu closed with the dialog, so focus returns to the account button.
  await expect(account).toHaveAttribute('aria-expanded', 'false');
  await expect(account).toBeFocused();

  // Tabbing out of the open menu closes it.
  await page.keyboard.press('Enter');
  for (let i = 0; i < 5; i++) await page.keyboard.press('Tab');
  await expect(page.locator('#account-panel')).toBeHidden();
});

test('from 768 px the organiser header fits one row', async ({ page }) => {
  await mockApi(page, 'owner');
  for (const width of [768, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/people');
    await expect(page.getByRole('table').first()).toBeVisible();
    const top = async (sel: string) => {
      const box = await page.locator(sel).first().boundingBox();
      return box ? box.y + box.height / 2 : -1;
    };
    const mark = await top('.wordmark');
    expect(Math.abs((await top('.nav a')) - mark), `nav on the word-mark's row at ${width}`).toBeLessThan(8);
    expect(Math.abs((await top('.account-popover .trigger')) - mark), `account on the word-mark's row at ${width}`).toBeLessThan(8);
    await expect(page.getByRole('button', { name: width < 1024 ? 'Account' : 'Priya Raman', exact: true })).toBeVisible();
    await page.screenshot({ path: `${shots()}settings-header-${width}-light.png` });
  }
});

test('the footer names the organisers, and privacy waits for the owner (D85)', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/');
  const foot = page.locator('footer');
  await expect(foot).toContainText('Contact the organisers: priya@college.edu');
  await expect(foot.getByRole('link', { name: 'priya@college.edu' })).toHaveAttribute('href', 'mailto:priya@college.edu');
  await expect(foot.getByRole('link', { name: 'Privacy' })).toHaveCount(0);
  await page.goto('/privacy');
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.locator('h1')).toHaveText('Privacy');
  await expect(page).toHaveTitle('Privacy · podium');
});

test('the privacy page opens for signed-out visitors too', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/privacy');
  await expect(page).toHaveURL('/privacy');
  await expect(page.locator('h1')).toHaveText('Privacy');
  await expect(page.getByRole('main').getByRole('link', { name: 'priya@college.edu' })).toHaveAttribute('href', 'mailto:priya@college.edu');
});

test('the 404 page echoes the address and links by role', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/no/such/place');
  await expect(page.getByText('Nothing lives at /no/such/place.')).toBeVisible();
  await expect(page.getByRole('main').getByRole('link')).toHaveText(['Your contests']);
});

test('the 404 page offers organisers their contests and the problem bank, and sign in to visitors', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.goto('/nope');
  await expect(page.getByRole('main').getByRole('link')).toHaveText(['Contests', 'Problems']);
  await page.unrouteAll();
  await mockApi(page, 'signed-out');
  await page.goto('/nope');
  await expect(page.getByRole('main').getByRole('link')).toHaveText(['Sign in']);
});

test('keyboard only: sign in, the theme menu and the shortcut list are reachable', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/signin');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
  await activate(page, page.getByRole('link', { name: 'Continue with Google' }), 'Tab');
  await expect(page.getByRole('link', { name: 'Continue with Google' })).not.toBeFocused();

  // The footer button and "?" open the same list; Escape closes it and focus returns.
  const footer = page.getByRole('button', { name: 'Help', exact: true });
  await activate(page, footer);
  const keys = page.getByRole('dialog', { name: 'Help' });
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
  // F6: on a phone the dialog sits at the top, and its head stays in view while the body scrolls.
  await page.setViewportSize({ width: 375, height: 640 });
  const box = await keys.boundingBox();
  expect(box?.y ?? 0).toBeLessThanOrEqual(24);
  await keys.evaluate((d) => d.scrollTo(0, d.scrollHeight));
  const close = keys.getByRole('button', { name: 'Close help' });
  await expect(close).toBeInViewport();
  const closeBox = await close.boundingBox();
  expect(closeBox && box ? closeBox.y - box.y : 999).toBeLessThan(40);
  await page.screenshot({ path: `${shots()}shortcuts-375-scrolled.png` });
  await activate(page, keys.getByRole('button', { name: 'Close help' }));
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

test('the owner saves the organisation details, and a bad field says what to fix (C1-011)', async ({ page }) => {
  const state = await mockApi(page, 'owner');
  await page.goto('/admin/people?tab=organisation');
  const section = page.getByRole('region', { name: 'Organisation details' });
  await section.getByLabel('Organisation name').fill('Example College of Engineering');
  await section.getByLabel('Contact email').fill('not-an-address');
  await section.getByRole('button', { name: 'Save organisation details' }).click();
  await expect(section.getByText('Enter an email address, for example podium@college.edu.')).toBeVisible();
  await expect(section.getByLabel('Contact email')).toBeFocused();
  await section.getByLabel('Contact email').fill('podium@college.edu');
  await section.getByRole('button', { name: 'Save organisation details' }).click();
  await expect(toasts(page).getByText('Saved the organisation details')).toBeVisible();
  expect(state.settingsPuts.at(-1)).toEqual({ name: 'Example College of Engineering', contact_email: 'podium@college.edu', phone: '', privacy_contact: '' });
  // The footer reads the new contact address without a reload.
  await expect(page.locator('footer')).toContainText('podium@college.edu');
});

test('a first-run owner sees the steps to a first contest, with what is done (C2-024)', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const steps = page.getByRole('region', { name: 'Your first contest' }).getByRole('listitem');
  await expect(steps).toHaveCount(5);
  // Students on the list and other organisers exist in the mock, validated problems do not.
  await expect(steps.nth(0)).toContainText('Add your students to the list in the Dashboard, so they can sign in.');
  await expect(steps.nth(0)).toContainText('Done');
  await expect(steps.nth(0).getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/admin/dashboard');
  await expect(steps.nth(1)).toContainText('Done');
  await expect(steps.nth(1).getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/admin/dashboard');
  await expect(steps.nth(2)).toContainText('To do');
  await expect(steps.nth(2).getByRole('link', { name: 'problem bank' })).toHaveAttribute('href', '/admin/problems');
  await expect(steps.nth(4)).toContainText('Share the address with your students: http://127.0.0.1:5179');
});

test('a student who opens the Dashboard sees who may, under the page title', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/admin/people');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard');
  await expect(page.getByRole('main')).toContainText('Only admins and owners can open the Dashboard.');
  await expect(page.getByRole('tab')).toHaveCount(0);
  await expect(page).toHaveTitle('Dashboard · podium');
});

test('the Live tab shows the five lines and reads them again only while it is open (O5)', async ({ page }) => {
  await mockApi(page, 'owner');
  let reads = 0;
  await page.route('**/api/admin/live', (route) => {
    reads++;
    return route.fallback();
  });
  await page.clock.install();
  await page.goto('/admin/dashboard');
  await page.getByRole('tab', { name: 'Live' }).click();
  const live = page.getByRole('tabpanel', { name: 'Live' });
  for (const line of ['42 people with a page open', 'Last asked for work 3 s ago', '2 waiting, median wait 2.4 s over the last 10 minutes', '18,432 MB free, database 54 MB', 'No backup yet.']) {
    await expect(live).toContainText(line);
  }
  await expect.poll(() => reads).toBe(1);
  await page.clock.runFor(5000);
  await expect.poll(() => reads).toBe(2);
  await page.getByRole('tab', { name: 'People' }).click();
  await page.clock.runFor(15000);
  expect(reads).toBe(2);
});

test('closing sign-in asks first, the sign-in page then says so, and opening is one press (O6)', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/dashboard?tab=sign-in');
  await expect(page.getByText('Sign-in is open: everyone on the list can sign in.')).toBeVisible();
  await page.getByRole('button', { name: 'Close sign-in' }).click();
  const dialog = page.getByRole('dialog', { name: 'Close sign-in?' });
  await expect(dialog).toContainText('People signed in now stay signed in.');
  await dialog.getByRole('button', { name: 'Close sign-in' }).click();
  await expect(toasts(page).getByText('Closed sign-in', { exact: true })).toBeVisible();
  await expect(page.getByText('Sign-in is closed.')).toBeVisible();
  expect(api.signinOpen).toBe(false);

  api.who = 'signed-out';
  await page.goto('/signin');
  await expect(page.getByRole('status').filter({ hasText: 'Sign-in is closed right now. Organisers can still sign in.' })).toBeVisible();
  await page.goto('/signin?error=closed');
  await expect(page.getByRole('alert')).toContainText('Sign-in is closed right now. Ask priya@college.edu when it opens again.');

  api.who = 'owner';
  await page.goto('/admin/dashboard?tab=sign-in');
  await page.getByRole('button', { name: 'Open sign-in' }).click();
  await expect(toasts(page).getByText('Opened sign-in', { exact: true })).toBeVisible();
  expect(api.signinOpen).toBe(true);
});
