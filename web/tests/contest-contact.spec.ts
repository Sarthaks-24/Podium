// V201-16 G3: the contest's own contact (organiser-typed, never an account
// email) on the student contest header, the ended page and the closed-problem
// state, and nothing at all when it is empty. The not-invited state is left
// out on purpose: the server answers it with the same 404 as a contest that
// does not exist, so it has no contest to name.
import { expect, test } from '@playwright/test';
import { mockApi } from './mocks/contests';

const CONTACT = 'week6@college.edu';
const LINE = 'Questions about this contest?';
for (const width of [375, 1440]) {
  test(`the contest header carries the contact at ${width} px`, async ({ page }) => {
    await mockApi(page, { phase: 'during', contestContact: CONTACT });
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.goto('/contests/week-6/problems/A');
    if (width < 1024) {
      await page.getByRole('button', { name: 'Menu' }).click();
      const link = page.getByRole('link', { name: `Email organisers, ${CONTACT}` });
      await expect(link).toHaveAttribute('href', `mailto:${CONTACT}`);
    } else {
      const link = page.getByRole('link', { name: `Email organisers, ${CONTACT}` });
      await expect(link).toHaveText('Email organisers');
      await expect(link).toHaveAttribute('href', `mailto:${CONTACT}`);
    }
  });

  test(`the ended page carries the contact at ${width} px`, async ({ page }) => {
    await mockApi(page, { phase: 'after', contestContact: CONTACT });
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.goto('/contests/week-6');
    await expect(page.getByText(LINE)).toHaveText(`${LINE} Email ${CONTACT}`);
    await expect(page.getByRole('link', { name: CONTACT })).toHaveAttribute('href', `mailto:${CONTACT}`);
  });

  test(`a closed problem names the contact at ${width} px`, async ({ page }) => {
    await mockApi(page, { phase: 'during', problemRefusal: 'not_started', contestContact: CONTACT });
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.goto('/contests/week-6/problems/B');
    await expect(page.getByText('Accept the rules on the contest page to start.')).toBeVisible();
    await expect(page.getByText(LINE)).toHaveText(`${LINE} Email ${CONTACT}`);
  });

  test(`the not-started page keeps its contact at ${width} px`, async ({ page }) => {
    await mockApi(page, { phase: 'upcoming', contestContact: CONTACT });
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.goto('/contests/week-6');
    await expect(page.getByText(LINE)).toHaveText(`${LINE} Email ${CONTACT}`);
  });
}

test('nothing shows when the contest has no contact', async ({ page }) => {
  for (const [phase, path] of [['during', '/contests/week-6/problems/A'], ['after', '/contests/week-6'], ['upcoming', '/contests/week-6']] as const) {
    await mockApi(page, { phase });
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(path);
    await expect(page.getByRole('heading', { level: 1 }).or(page.getByRole('navigation', { name: 'Problems' })).first()).toBeVisible();
    await expect(page.getByText(LINE)).toHaveCount(0);
    await expect(page.getByRole('link', { name: /^Email the organisers/ })).toHaveCount(0);
  }
  await mockApi(page, { phase: 'during', problemRefusal: 'not_started' });
  await page.goto('/contests/week-6/problems/B');
  await expect(page.getByText('Accept the rules on the contest page to start.')).toBeVisible();
  await expect(page.getByText(LINE)).toHaveCount(0);
});
