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
import { expect, test, type Page } from '@playwright/test';
import { activate } from './keyboard';
import { shots, WIDTHS, THEMES, mockApi, SCREENS } from './mocks/screens';

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
      if (info.name === 'Reset Google link') break;
    }

    for (const name of ['Add organiser', 'Remove Arjun Mehta', 'Remove ta@college.edu', 'Remove @college.edu', 'Remove @cs.college.edu', 'Add domain', 'Reset Google link']) {
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
  const section = page.getByRole('region', { name: 'Account help' });
  await expect(section).toContainText("Reset Google link when someone's address moved to a new Google account, so their next sign-in is accepted.");
  const email = section.getByLabel('Email', { exact: true });
  const submit = section.getByRole('button', { name: 'Reset Google link' });

  await submit.click();
  await expect(page.locator('#account-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-describedby', 'account-error');

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
  await dialog.getByRole('button', { name: 'Reset Google link' }).click();
  await expect(page.locator('#account-error')).toHaveText('Nobody with that email has an account.');
  await expect(email).toHaveAttribute('aria-invalid', 'true');

  await email.fill('asha@college.edu');
  await submit.click();
  await page.getByRole('dialog', { name: 'Reset the Google account link for asha@college.edu?' })
    .getByRole('button', { name: 'Reset Google link' })
    .click();
  await expect(page.getByText('Reset the Google account link for asha@college.edu', { exact: true })).toBeVisible();
  await expect(page.locator('#account-error')).toHaveCount(0);
  await expect(email).toHaveValue('');
  expect(state.resets).toEqual(['asha@college.edu']);
});

test('an owner ends sessions, disables and enables an account, each asked first', async ({ page }) => {
  const state = await mockApi(page, 'owner');
  await page.goto('/admin/people');
  const section = page.getByRole('region', { name: 'Account help' });
  const email = section.getByLabel('Email', { exact: true });
  await expect(section).toContainText('No account is disabled.');

  await section.getByRole('button', { name: 'End sessions' }).click();
  await expect(page.locator('#account-error')).toHaveText('Enter an email address.');
  await expect(email).toHaveAttribute('aria-invalid', 'true');

  await email.fill('asha@college.edu');
  await section.getByRole('button', { name: 'End sessions' }).click();
  const end = page.getByRole('dialog', { name: 'End every session of asha@college.edu?' });
  await expect(end).toContainText('They are signed out on every device at once');
  await expect(end.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await end.getByRole('button', { name: 'End sessions' }).click();
  await expect(page.getByText('Ended 2 sessions of asha@college.edu', { exact: true })).toBeVisible();

  await email.fill('priya@college.edu');
  await section.getByRole('button', { name: 'Disable account' }).click();
  await page.getByRole('dialog', { name: 'Disable priya@college.edu?' }).getByRole('button', { name: 'Disable account' }).click();
  await expect(page.locator('#account-error')).toHaveText('This owner is set in the server configuration and cannot be disabled.');

  await email.fill('asha@college.edu');
  await section.getByRole('button', { name: 'Disable account' }).click();
  const disable = page.getByRole('dialog', { name: 'Disable asha@college.edu?' });
  await expect(disable).toContainText('cannot sign in until an owner enables the account again');
  await disable.getByRole('button', { name: 'Disable account' }).click();
  await expect(page.getByText('Disabled asha@college.edu', { exact: true })).toBeVisible();
  await expect(section.getByRole('listitem')).toContainText(['asha@college.edu']);
  await page.screenshot({ path: `${shots()}people-access-1280-light.png`, fullPage: true });

  await section.getByRole('button', { name: 'Enable asha@college.edu' }).click();
  await page.getByRole('dialog', { name: 'Enable asha@college.edu?' }).getByRole('button', { name: 'Enable account' }).click();
  await expect(page.getByText('Enabled asha@college.edu', { exact: true })).toBeVisible();
  await expect(section).toContainText('No account is disabled.');
  expect(state.accountCalls).toEqual(['end-sessions asha@college.edu', 'disable asha@college.edu', 'enable asha@college.edu']);
});

test('a role changes only after the owner confirms it', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/people');
  const select = page.getByRole('combobox', { name: 'Role for Arjun Mehta' });
  await select.selectOption('owner');
  const dialog = page.getByRole('dialog', { name: 'Make Arjun Mehta an owner?' });
  await expect(dialog).toContainText('Owners can also manage people, domains and accounts on this page.');
  // Until confirmed, the row still says what is true.
  await expect(select).toHaveValue('admin');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(api.people.find((p) => p.id === 2)?.role).toBe('admin');

  await select.selectOption('owner');
  await dialog.getByRole('button', { name: 'Change role' }).click();
  await expect(page.getByText('Arjun Mehta is now an owner', { exact: true })).toBeVisible();
  await expect(select).toHaveValue('owner');
  expect(api.people.find((p) => p.id === 2)?.role).toBe('owner');
});

test('after adding an organiser, the page says what to send them', async ({ page }) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await mockApi(page, 'owner');
  await page.goto('/admin/people');
  await page.getByRole('region', { name: 'Organisers' }).getByLabel('Email', { exact: true }).fill('new.ta@college.edu');
  await page.getByRole('button', { name: 'Add organiser' }).click();
  const note = page.locator('.invite');
  await expect(note).toContainText('Send new.ta@college.edu this address: http://127.0.0.1:5179. They sign in with Google using that email.');
  await expect(page.getByRole('row', { name: /new\.ta@college\.edu/ })).toContainText('Invited');
  await note.getByRole('button', { name: 'Copy address' }).click();
  await expect(page.getByText('Copied the address', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('http://127.0.0.1:5179');
  await note.getByRole('button', { name: 'Dismiss' }).click();
  await expect(note).toHaveCount(0);
});

test('a fresh install starts with a short checklist', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  api.domains = [];
  api.people = api.people.filter((p) => p.locked);
  await page.goto('/admin/people');
  const start = page.getByRole('region', { name: 'Getting started' });
  await expect(start.getByRole('listitem')).toHaveCount(3);
  await expect(start.getByRole('listitem').nth(0)).toContainText('To do');
  await expect(start).toContainText('Share the address with your students: http://127.0.0.1:5179');
  await page.screenshot({ path: `${shots()}people-fresh-1280-light.png`, fullPage: true });

  await page.getByLabel('Domain', { exact: true }).fill('college.edu');
  await page.getByRole('button', { name: 'Add domain' }).click();
  await expect(start.getByRole('listitem').nth(0)).toContainText('Done');
});

test('History lists the site changes and filters them', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.goto('/admin/people');
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem')).toHaveCount(3);
  await expect(history.getByRole('listitem').first()).toContainText('Made ta@college.edu an admin');
  await expect(history.getByRole('listitem').first()).toContainText('priya@college.edu (Owner)');
  await expect(history.getByRole('listitem').last()).toContainText('The server');

  await history.getByLabel('Kind of change').selectOption('domain_added');
  await expect(history.getByRole('listitem')).toHaveCount(1);
  await expect(history).toContainText('Allowed @cs.college.edu to sign in');
  await history.getByLabel('Kind of change').selectOption('');
  await history.getByLabel('Email contains').fill('nobody');
  await history.getByRole('button', { name: 'Search' }).click();
  await expect(history).toContainText('No change matches. Clear the filter to see everything.');
  expect(api.auditQueries).toContain('page=1&action=domain_added');
  expect(api.auditQueries).toContain('page=1&q=nobody');
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
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Problem bank', 'People', 'Inbox']);

  await page.getByRole('link', { name: 'People' }).click();
  await expect(page).toHaveURL('/admin/people');
  await expect(page.getByRole('link', { name: 'People' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  await page.getByRole('button', { name: 'Priya Raman' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL('/signin');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests');
});

test('students see their own nav, with Questions once a case is shared', async ({ page }) => {
  const api = await mockApi(page, 'student');
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link')).toHaveText(['Contests']);
  await expect(page.getByRole('link', { name: 'People' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Asha Rao' })).toBeVisible();

  api.cases = [{ id: 2, decided: true }];
  await page.reload();
  await expect(nav.getByRole('link')).toHaveText(['Contests', 'Questions']);
  await expect(page.getByText('An organiser decided on a question about your submission.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Read the decision' })).toHaveAttribute('href', '/integrity');
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
  await expect(page.getByRole('link', { name: 'People' })).toBeHidden();

  await menu.click();
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('link', { name: 'People' })).toBeVisible();
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
  await page.getByRole('link', { name: 'Problem bank' }).click();
  await expect(page).toHaveURL('/admin/problems');
  await expect(menu).toHaveAttribute('aria-expanded', 'false');
});

test('keyboard only: the account menu opens, its items work, and Escape returns focus', async ({ page }) => {
  await mockApi(page, 'owner');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/people');
  await expect(page.getByRole('table')).toBeVisible();
  const account = page.getByRole('button', { name: 'Priya Raman' });
  await account.focus();
  await page.keyboard.press('Enter');
  await expect(account).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('#account-panel')).toContainText('Signed in as priya@college.edu');
  await expect(page.locator('#account-panel')).toContainText('Owner');

  // Tab walks the items in order; each shows its focus.
  const items: string[] = [];
  for (let i = 0; i < 3; i++) {
    await page.keyboard.press('Tab');
    const info = await focusInfo(page);
    expect(info?.visible, `no visible focus on ${info?.name}`).toBe(true);
    items.push(info?.name ?? '');
  }
  expect(items).toEqual(['Theme and colours', 'Keyboard shortcuts', 'Sign out']);

  // Theme and colours opens the dialog; closing it lands back on the item.
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Shift+Tab');
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
  await expect(page.getByRole('button', { name: 'Theme and colours' })).toBeFocused();

  // Escape closes the menu and returns focus to the account button.
  await page.keyboard.press('Escape');
  await expect(account).toHaveAttribute('aria-expanded', 'false');
  await expect(account).toBeFocused();

  // Tabbing out of the open menu closes it.
  await page.keyboard.press('Enter');
  for (let i = 0; i < 4; i++) await page.keyboard.press('Tab');
  await expect(page.locator('#account-panel')).toBeHidden();
});

test('from 768 px the organiser header fits one row', async ({ page }) => {
  await mockApi(page, 'owner');
  for (const width of [768, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/people');
    await expect(page.getByRole('table')).toBeVisible();
    const top = async (sel: string) => {
      const box = await page.locator(sel).first().boundingBox();
      return box ? box.y + box.height / 2 : -1;
    };
    const mark = await top('.wordmark');
    expect(Math.abs((await top('.nav a')) - mark), `nav on the word-mark's row at ${width}`).toBeLessThan(8);
    expect(Math.abs((await top('.account-popover .trigger')) - mark), `account on the word-mark's row at ${width}`).toBeLessThan(8);
    await expect(page.getByRole('button', { name: width < 1024 ? 'Account' : 'Priya Raman', exact: true })).toBeVisible();
    await page.screenshot({ path: `${shots()}people-owner-header-${width}-light.png` });
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
  await expect(page.getByRole('main').getByRole('link')).toHaveText(['Contests', 'Problem bank']);
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
  // F6: on a phone the dialog sits at the top, and its head stays in view while the body scrolls.
  await page.setViewportSize({ width: 375, height: 640 });
  const box = await keys.boundingBox();
  expect(box?.y ?? 0).toBeLessThanOrEqual(24);
  await keys.evaluate((d) => d.scrollTo(0, d.scrollHeight));
  const close = keys.getByRole('button', { name: 'Close keyboard shortcuts' });
  await expect(close).toBeInViewport();
  const closeBox = await close.boundingBox();
  expect(closeBox && box ? closeBox.y - box.y : 999).toBeLessThan(40);
  await page.screenshot({ path: `${shots()}shortcuts-375-scrolled.png` });
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
