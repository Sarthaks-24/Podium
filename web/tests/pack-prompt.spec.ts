// Stage F: "Prompt for another AI" in the problem bank's Import pack area.
// The problems API is the in-test mock; the prompt route is added on top of
// it here. The panel is reached from the keyboard, shows the prompt and the
// four reminders, and Copy puts the prompt on the clipboard with a status.
import { expect, test } from '@playwright/test';
import { activate } from './keyboard';
import { mockApi as problemsApi } from './mocks/problems';
import { mockApi as aiApi } from './mocks/ai';

const PROMPT = 'You are helping an organiser write a problem.\nproblem.yaml\n=== PROBLEM DESCRIPTION ===\n';

test.use({ permissions: ['clipboard-read', 'clipboard-write'] });

test('the prompt panel opens from the keyboard, shows the reminders and copies the prompt', async ({ page }) => {
  await problemsApi(page);
  await page.route('**/api/admin/problems/pack-prompt', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ prompt: PROMPT }) }));
  await page.goto('/admin/problems');
  const opener = page.getByRole('button', { name: 'Prompt for another AI' });
  await expect(opener).toHaveAttribute('aria-expanded', 'false');
  await activate(page, opener);
  await expect(opener).toHaveAttribute('aria-expanded', 'true');

  const box = page.getByRole('textbox', { name: 'Prompt for another AI' });
  await expect(box).toHaveValue(PROMPT);
  await expect(box).toHaveAttribute('readonly', '');
  const panel = page.locator('#pack-prompt-panel');
  await expect(panel.getByRole('listitem')).toHaveText([
    'Write the problem in your own words.',
    'Do not paste unreleased contest problems or student material into an outside AI.',
    'If the import fails, paste its error list back into the chat.',
    'An imported problem still has to pass Validate before a contest can use it.',
  ]);

  await activate(page, panel.getByRole('button', { name: 'Copy prompt' }));
  await expect(panel.getByRole('status')).toHaveText('Copied the prompt.');
  // Windows clipboards hand back CRLF.
  expect((await page.evaluate(() => navigator.clipboard.readText())).replaceAll('\r\n', '\n')).toBe(PROMPT);

  await opener.click();
  await expect(panel).toHaveCount(0);
});

test('Download as file saves the prompt as podium-pack-prompt.txt and says so', async ({ page }) => {
  await problemsApi(page);
  await page.route('**/api/admin/problems/pack-prompt', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ prompt: PROMPT }) }));
  await page.goto('/admin/problems');
  await activate(page, page.getByRole('button', { name: 'Prompt for another AI' }));
  const panel = page.locator('#pack-prompt-panel');
  const saved = page.waitForEvent('download');
  await activate(page, panel.getByRole('button', { name: 'Download as file' }));
  const file = await saved;
  expect(file.suggestedFilename()).toBe('podium-pack-prompt.txt');
  const path = await file.path();
  expect((await import('node:fs')).readFileSync(path, 'utf8')).toBe(PROMPT);
  await expect(panel.getByRole('status')).toHaveText('Downloaded podium-pack-prompt.txt.');
});

test('the wizard tells an admin, not an organiser, when the site uses a custom prompt', async ({ page }) => {
  await aiApi(page);
  await page.goto('/admin/ai/new');
  await expect(page.getByRole('heading', { name: 'Add a key to start' })).toBeVisible();
  const note = page.getByText('This site uses a custom prompt.');
  await expect(note).toHaveCount(0);

  const real = await page.evaluate(async () => (await fetch('/api/admin/ai/key')).json());
  const me = await page.evaluate(async () => (await fetch('/api/me')).json());
  await page.route('**/api/admin/ai/key', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...real, custom_prompt: true }) });
  });
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Add a key to start' })).toBeVisible();
  await expect(note, 'an organiser does not see it').toHaveCount(0);

  await page.route('**/api/me', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...me, role: 'admin' }) }));
  await page.reload();
  await expect(note).toBeVisible();
});
