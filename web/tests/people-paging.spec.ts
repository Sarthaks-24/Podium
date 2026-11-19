// People tab paging (stage L, V211-21 and V211-65). 701 mocked people: 300 on
// the staff part of the list (3 from the fixtures and 297 organisers) and 401
// students who never signed in. The list shows 50 rows a page, 120 refused
// sign-ins page separately, and every step works by keyboard.
import { expect, test, type Page } from '@playwright/test';
import { mockApi } from './mocks/screens';
import { tabTo } from './keyboard';
import { toasts } from './locators';

async function setup(page: Page) {
  const api = await mockApi(page, 'owner');
  for (let i = 0; i < 297; i++) {
    api.people.push({ id: 100 + i, email: `org${i}@college.edu`, name: `Organiser ${i}`, role: 'organiser', last_login_at: 1792100000000, locked: false });
  }
  for (let i = 0; i < 399; i++) {
    api.students.push({ id: 1000 + i, email: `s${String(i).padStart(4, '0')}@college.edu`, name: '', role: 'student', last_login_at: null, locked: false });
  }
  for (let i = 0; i < 120; i++) {
    api.refused.push({ email: `guest${String(i).padStart(3, '0')}@gmail.com`, count: 1, first_at: 1792000000000, last_at: 1792090000000 });
  }
  await page.goto('/admin/dashboard');
  return api;
}

const rows = (page: Page) => page.locator('table.people tbody tr, .people tbody tr');
const showing = (page: Page, text: string) => page.getByText(text, { exact: true });

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [375, 1440]) {
    test.describe(`${scheme} at ${width}px`, () => {
      test.use({ colorScheme: scheme, viewport: { width, height: width < 768 ? 812 : 900 } });

      test('shows 50 rows, pages by keyboard and moves focus when a button turns off', async ({ page }) => {
        await setup(page);
        await expect(rows(page)).toHaveCount(50);
        await expect(showing(page, 'Showing 1 to 50 of 701').first()).toBeVisible();
        const previous = page.locator('#people-prev');
        const next = page.locator('#people-next');
        await expect(previous).toBeDisabled();

        await tabTo(page, next, { max: 400 });
        await page.keyboard.press('Enter');
        await expect(showing(page, 'Showing 51 to 100 of 701').first()).toBeVisible();
        await expect(rows(page)).toHaveCount(50);
        await expect(next).toBeFocused();
        for (let i = 0; i < 13; i++) await page.keyboard.press('Enter');
        await expect(showing(page, 'Showing 701 to 701 of 701').first()).toBeVisible();
        await expect(rows(page)).toHaveCount(1);
        // Next turned off, so focus moved to Previous.
        await expect(next).toBeDisabled();
        await expect(previous).toBeFocused();
        await page.keyboard.press('Enter');
        await expect(showing(page, 'Showing 651 to 700 of 701').first()).toBeVisible();
        await expect(next).toBeEnabled();
      });

      test('the Add people form is within one page of rows by keyboard', async ({ page }) => {
        await setup(page);
        const presses = await tabTo(page, page.locator('#people-paste'), { max: 400 });
        // 50 rows with up to three controls each (Submissions, Role, Remove), plus the controls around them.
        expect(presses).toBeLessThanOrEqual(50 * 3 + 15);
        await expect(page.getByRole('heading', { name: 'Add people' })).toBeVisible();
      });
    });
  }
}

test.describe('list behaviour', () => {
  test.use({ viewport: { width: 1024, height: 900 } });

  test('search finds a person on any page and returns to page 1', async ({ page }) => {
    await setup(page);
    await page.locator('#people-next').click();
    await page.locator('#people-next').click();
    await expect(showing(page, 'Showing 101 to 150 of 701').first()).toBeVisible();
    await page.getByLabel('Find a person').fill('s0300@');
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).first()).toContainText('s0300@college.edu');
    // One page is enough, so the controls are hidden.
    await expect(page.locator('#people-next')).toHaveCount(0);
    await page.getByLabel('Find a person').fill('');
    await expect(showing(page, 'Showing 1 to 50 of 701').first()).toBeVisible();
  });

  test('the Role filter defaults to All roles, counts the filtered list and returns to page 1', async ({ page }) => {
    await setup(page);
    const role = page.getByRole('combobox', { name: 'Role', exact: true });
    await expect(role).toHaveValue('all');
    await expect(role.getByRole('option')).toHaveText(['All roles', 'Students', 'Organisers', 'Admins', 'Owners']);
    await page.locator('#people-next').click();
    await page.locator('#people-next').click();
    await role.selectOption('student');
    await expect(showing(page, 'Showing 1 to 50 of 401').first()).toBeVisible();
    await role.selectOption('owner');
    await expect(rows(page)).toHaveCount(1);
    await expect(page.locator('#people-next')).toHaveCount(0);
  });

  test('with no matches the empty text says what to do', async ({ page }) => {
    await setup(page);
    await page.getByLabel('Find a person').fill('nobody-called-this');
    await expect(page.getByText('Nobody matches. Clear the search or choose All roles.')).toBeVisible();
    await page.getByLabel('Find a person').fill('');
    await page.getByRole('combobox', { name: 'Role', exact: true }).selectOption('admin');
    await expect(page.getByText('Nobody matches. Clear the search or choose All roles.')).toBeVisible();
    await expect(page.getByText('Clear the search to see everyone')).toHaveCount(0);
  });

  test('changing a role keeps the page', async ({ page }) => {
    const api = await setup(page);
    await page.locator('#people-next').click();
    await expect(showing(page, 'Showing 51 to 100 of 701').first()).toBeVisible();
    const select = page.getByRole('combobox', { name: 'Role for Organiser 49' });
    await select.selectOption('admin');
    await page.getByRole('dialog').getByRole('button', { name: 'Change role' }).click();
    await expect(toasts(page).getByText('Organiser 49 is now an admin', { exact: true })).toBeVisible();
    expect(api.people.find((p) => p.email === 'org49@college.edu')?.role).toBe('admin');
    await expect(showing(page, 'Showing 51 to 100 of 701').first()).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Role for Organiser 49' })).toHaveValue('admin');
  });

  test('removing the last row on the last page moves to the new last page', async ({ page }) => {
    await setup(page);
    for (let i = 0; i < 14; i++) await page.locator('#people-next').click();
    await expect(showing(page, 'Showing 701 to 701 of 701').first()).toBeVisible();
    await page.getByRole('button', { name: 'Remove s0398@college.edu' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Remove', exact: true }).click();
    await expect(showing(page, 'Showing 651 to 700 of 700').first()).toBeVisible();
    await expect(rows(page)).toHaveCount(50);
  });

  test('refused sign-ins page separately from the list', async ({ page }) => {
    await setup(page);
    const refused = page.locator('ul.refused li');
    // 121 refused: the fixture's one plus the 120 added.
    await expect(refused).toHaveCount(50);
    await expect(showing(page, 'Showing 1 to 50 of 121')).toBeVisible();
    await page.locator('#refused-next').click();
    await expect(showing(page, 'Showing 51 to 100 of 121')).toBeVisible();
    await expect(showing(page, 'Showing 1 to 50 of 701').first()).toBeVisible();
    await page.locator('#people-next').click();
    await expect(showing(page, 'Showing 51 to 100 of 121')).toBeVisible();
    await page.locator('#refused-next').click();
    await expect(refused).toHaveCount(21);
    await expect(page.locator('#refused-next')).toBeDisabled();
    await expect(page.locator('#refused-prev')).toBeFocused();
  });
});
