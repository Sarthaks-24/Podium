// Against a live testing-mode server with a judge (make dev-test, or
// tools/dev-stack.sh inside WSL2): an admin turns on the fake provider,
// which needs no key, drafts one problem from a brief and one from pasted
// text, and follows each draft into the editor until the generator has made
// the inputs, the reference the outputs, and validation has passed
// (cache/v1/m5-plan.md section 5). Then the admin asks a model to solve the
// first draft and reads the zero-shot score. Nothing is mocked; no real key
// is used anywhere.
import { expect, test, type Page } from '@playwright/test';

async function signIn(page: Page) {
  await page.goto('/signin');
  await expect(page.getByRole('note')).toContainText('Testing mode');
  await page.getByLabel('Email').fill('live-ai-setter@college.edu');
  await page.getByLabel('Role').selectOption('admin');
  await page.getByRole('button', { name: 'Sign in for testing' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Your contests' })).toBeVisible();
}

async function useFakeProvider(page: Page) {
  await page.goto('/admin/ai');
  await expect(page.getByRole('heading', { level: 1, name: 'AI assistant' })).toBeVisible();
  await page.getByLabel('Provider').selectOption('fake');
  await expect(page.getByLabel('API key')).toHaveCount(0);
  await page.getByRole('button', { name: 'Use fake provider' }).click();
  await expect(page.getByText('Fake provider active, no key needed')).toBeVisible();
}

/** Waits for the wizard's draft, opens it, and waits until it is validated. */
async function followDraftToValidated(page: Page, shot: string) {
  await expect(page.getByText('Writing the problem').first()).toBeVisible();
  const open = page.getByRole('link', { name: 'Open the draft in the editor' });
  await expect(open).toBeVisible({ timeout: 200_000 });
  await open.click();
  await expect(page.getByRole('region', { name: 'Generator', exact: true })).toBeVisible();
  await expect(page.locator('.action-bar').getByText('AI draft')).toBeVisible();
  await expect(page.locator('.action-bar .status')).toContainText('Validated', { timeout: 240_000 });
  await page.screenshot({ path: `${test.info().project.testDir}/../../cache/screenshots-m5/${shot}`, fullPage: true });
}

test('the fake provider drafts problems that reach Validated', async ({ page }) => {
  test.setTimeout(900_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  await signIn(page);
  await useFakeProvider(page);

  // From a brief.
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Sum of two integers');
  await page.getByLabel('Difficulty').selectOption('easy');
  await page.getByRole('button', { name: 'Generate' }).click();
  await followDraftToValidated(page, 'live-brief-validated-1440.png');
  const briefDraft = page.url();

  // From pasted text.
  await page.goto('/admin/ai/new');
  await page.getByLabel('From pasted text').check();
  await page
    .getByLabel('Problem text')
    .fill('Given n integers, print the largest sum of a non-empty contiguous subarray. 1 <= n <= 200000, |a_i| <= 10^9.');
  await page.getByRole('button', { name: 'Generate' }).click();
  await followDraftToValidated(page, 'live-paste-validated-1440.png');

  // Can a model solve the first draft?
  await page.goto(briefDraft);
  await page.getByRole('button', { name: 'Ask a model to solve it' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Ask a model' }).click();
  await expect(page.locator('.solve-line')).toContainText('A model solved this on its first try:', { timeout: 240_000 });
  await expect(page.getByRole('link', { name: "See the model's submission" })).toBeVisible();
});

test('forgetting the key turns the wizard off', async ({ page }) => {
  await signIn(page);
  await useFakeProvider(page);
  await page.getByRole('button', { name: 'Forget key' }).click();
  await expect(page.getByText('No key yet. Add one below to draft problems with AI.')).toBeVisible();
  await page.goto('/admin/ai/new');
  await expect(page.getByRole('button', { name: 'Generate' })).toBeDisabled();
});
