// The key form with the other providers (v2.1.0 stage K): OpenRouter and Groq
// under their own group in the provider list, the privacy line under the key
// field for them and for no built-in provider, a typed model id such as
// "vendor/model:free", a host with no model list (the id is required), the
// key never in the page afterwards, axe, keyboard and a sweep at 375, 768,
// 1024 and 1440 px in light and dark. Every /api/ call is answered by
// tests/mocks/ai.ts (its `other` option). Screenshots go to $PODIUM_SHOTS
// (default cache/v2.1.0/shots/K/); the keys typed are fakes.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate, tabTo } from './keyboard';
import { expectKeyGone, GROQ_KEY, OPENROUTER_KEY, mockApi, THEMES, WIDTHS } from './mocks/ai';

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.1.0/shots/K/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const PRIVACY = (name: string) => `Your problem text is sent to ${name}. Free plans may keep what you send.`;

test('the other providers are grouped, and only they carry the privacy line', async ({ page }) => {
  await mockApi(page, { other: true });
  await page.goto('/admin/ai/new');
  const provider = page.getByLabel('Provider');
  await expect(provider.locator('option')).toHaveText(['Anthropic', 'OpenAI', 'Google Gemini', 'OpenRouter', 'Groq']);
  await expect(provider.locator('optgroup')).toHaveCount(2);
  await expect(provider.locator('optgroup').nth(0)).toHaveAttribute('label', 'Providers');
  await expect(provider.locator('optgroup').nth(1)).toHaveAttribute('label', 'Other providers');
  await expect(page.getByText('Your problem text is sent to')).toHaveCount(0);

  await provider.selectOption('openrouter');
  await expect(page.getByText(PRIVACY('OpenRouter'))).toBeVisible();
  await expect(page.getByLabel('API key')).toHaveAccessibleDescription(new RegExp(PRIVACY('OpenRouter')));
  await provider.selectOption('groq');
  await expect(page.getByText(PRIVACY('Groq'))).toBeVisible();
  await provider.selectOption('gemini');
  await expect(page.getByText('Your problem text is sent to')).toHaveCount(0);
});

test('an old server that sends no group keeps the plain list and no privacy line', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/ai/new');
  await expect(page.getByLabel('Provider').locator('optgroup')).toHaveCount(0);
  await expect(page.getByText('Your problem text is sent to')).toHaveCount(0);
});

test('a key for OpenRouter lists models, takes a typed id with a slash and is gone from the page', async ({ page }) => {
  const state = await mockApi(page, { other: true });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Provider').selectOption('openrouter');
  const field = page.getByLabel('API key');

  await field.fill('wrong-key-typed-by-mistake');
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('OpenRouter refused the key. Check it and paste it again.')).toBeVisible();
  await expect(field).toHaveValue('');

  await field.fill(OPENROUTER_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  const model = page.getByLabel('Model', { exact: true });
  await expect(model.locator('option')).toHaveText(['Choose a model', 'Chat New (free)', 'Chat Old', 'Type a model id']);
  await expect(field).toHaveValue('');
  await expectKeyGone(page, state);

  await model.selectOption({ label: 'Type a model id' });
  await page.getByLabel('Model id').fill('vendor/typed-model:free');
  await page.getByRole('button', { name: 'Save model' }).click();
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeEnabled();
  expect(state.keyBodies.map((b) => ({ provider: b.provider, model: b.model }))).toEqual([
    { provider: 'openrouter', model: '' },
    { provider: 'openrouter', model: '' },
    { provider: 'openrouter', model: 'vendor/typed-model:free' },
  ]);
  await expectKeyGone(page, state);
});

test('a host with no model list asks for the model id and says it is required', async ({ page }) => {
  const state = await mockApi(page, { other: 'no-list' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Provider').selectOption('groq');
  await expect(page.getByLabel('Model', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Model id')).toBeVisible();
  await expect(page.getByText('Exactly as Groq writes it. Required.')).toBeVisible();
  await expect(page.getByText('Optional. Leave it empty')).toHaveCount(0);

  await page.getByLabel('API key').fill(GROQ_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Enter the model id.')).toBeVisible();
  await expect(page.getByLabel('Model id')).toBeFocused();
  await expect(page.getByLabel('Model id')).toHaveAttribute('aria-invalid', 'true');
  expect(state.keyBodies).toHaveLength(0);

  await page.getByLabel('Model id').fill('llama-x');
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeEnabled();
  expect(state.keyBodies).toHaveLength(1);
  await expectKeyGone(page, state);
});

test('the provider list and the key field work by keyboard alone', async ({ page }) => {
  const state = await mockApi(page, { other: true });
  await page.goto('/admin/ai/new');
  const provider = page.getByLabel('Provider');
  await tabTo(page, provider);
  await page.keyboard.press('End');
  await expect(provider).toHaveValue('groq');
  await tabTo(page, page.getByLabel('API key'));
  await page.keyboard.type(GROQ_KEY);
  await activate(page, page.getByRole('button', { name: 'Save key' }), 'Enter');
  await expect(page.getByLabel('Model', { exact: true })).toBeVisible();
  await expectKeyGone(page, state);
});

for (const theme of THEMES) {
  test(`the key form for OpenRouter at four widths, ${theme}: no overflow, axe clean`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await mockApi(page, { other: true });
    await page.goto('/admin/ai/new');
    await page.getByLabel('Provider').selectOption('openrouter');
    await page.getByLabel('API key').fill('fake-key-for-the-screenshot');
    await expect(page.getByText(PRIVACY('OpenRouter'))).toBeVisible();
    const dir = shotDir();
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
      await page.evaluate(() => document.fonts.ready);
      const overflow = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
      expect(overflow.scroll, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(overflow.client);
      await page.screenshot({ path: `${dir}openrouter-key-${width}-${theme}.png`, fullPage: true });
    }
    const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
    expect(results.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
  });
}
