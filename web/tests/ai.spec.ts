// Screen sweep and flows for the M5 AI wizard screens: the AI assistant
// settings at /admin/ai, "New problem with AI" at /admin/ai/new, and the
// editor's Generator section and "Can a model solve this?". Every /api/
// call is answered by an in-test mock of cache/v1/m5-plan.md section 7, one
// per page, so each test starts from the same fixtures: an admin with no
// key, a validated problem, and an AI draft with a generator and a plan.
//
// For each screen, in light and dark, at 375, 768, 1024 and 1440 px, the
// sweep checks there is no horizontal page overflow, exactly one h1 and the
// expected title, then saves a full-page screenshot to
// cache/screenshots-m5/<screen>-<width>-<theme>.png. Further tests cover
// the key flow (the key never reaches the page's state, storage or address
// after saving), the fake provider, the wizard from a brief over the live
// stream and from pasted text over polling, a wizard failure with reasons,
// the editor's generator section and input generation, and the solve check.
import { expect, test } from '@playwright/test';
import { activate, tabTo } from './keyboard';
import { shots, WIDTHS, THEMES, T0, WIZARD_ID, FAKE_KEY, OPENAI_KEY, GEMINI_KEY, type Json, json, mockApi, expectKeyGone, SCREENS } from './mocks/ai';

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    for (const screen of SCREENS) {
      test(`${screen.name}`, async ({ page }) => {
        await mockApi(page, screen.opts);
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

test('the bank links to the wizard and the settings, and marks AI drafts', async ({ page }) => {
  await mockApi(page);
  await page.route('**/api/admin/problems', (route) =>
    json(route, 200, {
      problems: [
        { id: 20, slug: 'maximum-subarray', title: 'Maximum subarray', difficulty: 'medium', tags: ['arrays', 'ai draft'], origin: 'ai', archived: false, test_count: 1, points: 100, validated: false, validated_at: null, updated_at: T0 },
      ],
    }),
  );
  await page.goto('/admin/problems');
  const row = page.getByRole('row', { name: /Maximum subarray/ });
  await expect(row.getByText('AI draft')).toBeVisible();
  await expect(row.locator('.tags')).toHaveText('arrays');
  await page.getByRole('link', { name: 'New problem with AI' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'New problem with AI' })).toBeFocused();
  await expect(page.getByText('No key yet.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeDisabled();
  // The key form is on the wizard itself, above the brief (P3 O7).
  const inline = page.getByRole('form', { name: 'Add a key to start' });
  await inline.getByLabel('API key').fill(FAKE_KEY);
  await inline.getByRole('button', { name: 'Save key' }).click();
  const model = page.getByRole('form', { name: 'Choose a model to start' });
  await model.getByLabel('Model', { exact: true }).selectOption({ label: 'Claude Opus 5.5' });
  await model.getByRole('button', { name: 'Save model' }).click();
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeEnabled();
  await expect(page.getByRole('form', { name: /to start$/ })).toHaveCount(0);
  await expect(page.getByLabel('Topic', { exact: true })).toBeFocused();
  await page.getByRole('link', { name: 'Change key or model' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'AI assistant' })).toBeFocused();
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Problem bank' })).toHaveAttribute('aria-current', 'page');
});

test('saving a key checks it, lists the models, clears the field and never keeps the key', async ({ page }) => {
  const state = await mockApi(page);
  await page.goto('/admin/ai');
  await expect(page.getByText("A key stays in the server's memory for 2 hours", { exact: false })).toBeVisible();
  await expect(page.getByLabel('Provider')).toHaveValue('anthropic');
  await expect(page.getByLabel('Provider').locator('option')).toHaveText(['Anthropic', 'OpenAI', 'Google Gemini']);
  // No default model (C11): before a key is saved there is no list, only an optional id field.
  await expect(page.getByLabel('Model', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Model id')).toHaveValue('');
  const field = page.getByLabel('API key');
  await expect(field).toHaveAttribute('type', 'password');
  await expect(field).toHaveAttribute('autocomplete', 'off');

  // An empty key is caught before anything is sent.
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(field).toBeFocused();
  await expect(page.getByText('Paste your API key.')).toBeVisible();
  expect(state.keyBodies).toHaveLength(0);

  // A refused key: the reason sits under the field, tied to it, and the field is empty again.
  await field.fill('sk-' + 'ant-wrong-and-typed-by-mistake');
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Anthropic refused the key. Check it and paste it again.')).toBeVisible();
  await expect(field).toHaveValue('');
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(field).toHaveAttribute('aria-describedby', /api-key-error/);
  await expect(field).toBeFocused();

  // A good key with no model: the server checks it by listing the models, which fill the picker.
  await field.fill(FAKE_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Key saved. Choose a model to start drafting.')).toBeVisible();
  expect(state.keyBodies.at(-1)).toEqual({ provider: 'anthropic', model: '', key: FAKE_KEY });
  const picker = page.getByLabel('Model', { exact: true });
  await expect(picker).toBeFocused();
  await expect(picker.locator('option:not([disabled])')).toHaveText(['Claude Opus 5.5', 'Claude Sonnet 5.5', 'Type a model id']);
  await expect(page.getByText('Anthropic: Key active until 14:32')).toBeVisible();
  await expect(page.getByText('No model chosen yet', { exact: false })).toBeVisible();
  await expect(field).toHaveValue('');

  // With the key held, the model changes alone: the key field may stay empty.
  await picker.selectOption('claude-sonnet-5-5');
  await expect(page.getByText('A key for Anthropic is active. Leave this empty to change only the model.')).toBeVisible();
  await page.getByRole('button', { name: 'Save model' }).click();
  await expect(page.getByText('Model saved', { exact: true })).toBeVisible();
  expect(state.keyBodies.at(-1)).toEqual({ provider: 'anthropic', model: 'claude-sonnet-5-5', key: '' });
  await expect(page.getByText('Claude Sonnet 5.5. Used for new drafts.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'New problem with AI' })).toBeVisible();
  await expectKeyGone(page, state);

  // Reloading shows the state from the server, still without the key.
  await page.reload();
  await expect(page.getByText('Anthropic: Key active until 14:32')).toBeVisible();
  await expect(page.getByLabel('Model', { exact: true })).toHaveValue('claude-sonnet-5-5');
  await expectKeyGone(page, state);

  await page.getByRole('button', { name: 'Forget key for Anthropic' }).click();
  await expect(page.getByText('No key yet. Add one below to draft problems with AI.')).toBeVisible();
  await expect(page.getByLabel('Provider')).toBeFocused();
  await expectKeyGone(page, state);
});

test('each provider keeps its own key, a typed model id is accepted, and the list refreshes', async ({ page }) => {
  const state = await mockApi(page, { key: 'active' });
  await page.goto('/admin/ai');
  await expect(page.getByText('Anthropic: Key active until 14:32')).toBeVisible();

  // OpenAI has no key yet: no list, so its model id is typed. The provider's lookup knows this one.
  await page.getByLabel('Provider').selectOption('openai');
  await expect(page.getByLabel('Model', { exact: true })).toHaveCount(0);
  await page.getByLabel('Model id').fill('gpt-x-preview');
  await page.getByLabel('API key').fill(OPENAI_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Key saved', { exact: true })).toBeVisible();
  expect(state.keyBodies.at(-1)).toEqual({ provider: 'openai', model: 'gpt-x-preview', key: OPENAI_KEY });
  await expect(page.getByText('OpenAI: Key active until 14:32')).toBeVisible();
  await expect(page.getByText('gpt-x-preview. Used for new drafts.')).toBeVisible();
  // The Anthropic key is still held, no longer the one used for drafts.
  await expect(page.getByText('Anthropic: Key active until 14:32')).toBeVisible();
  await expect(page.getByText('Claude Opus 5.5', { exact: true })).toBeVisible();
  // The typed model is not in the list, so the picker shows it typed.
  await expect(page.getByLabel('Model', { exact: true })).toHaveValue('');
  await expect(page.getByLabel('Model id')).toHaveValue('gpt-x-preview');

  // Refresh asks the provider again; the new model is offered.
  await page.getByRole('button', { name: 'Refresh the list' }).click();
  await expect(page.getByText('Model list updated')).toBeVisible();
  expect(state.modelGets.at(-1)).toBe('?provider=openai&refresh=1');
  await expect(page.getByLabel('Model', { exact: true }).locator('option', { hasText: 'gpt-x-2' })).toHaveCount(1);

  // A model the provider does not offer is refused in the provider's words.
  await page.getByLabel('Model id').fill('gpt-nope');
  await page.getByRole('button', { name: 'Save model' }).click();
  await expect(page.getByText('OpenAI does not offer the model gpt-nope to this key. Choose another model.')).toBeVisible();

  // A malformed id is caught before anything is sent.
  const sent = state.keyBodies.length;
  await page.getByLabel('Model id').fill('gpt x');
  await page.getByRole('button', { name: 'Save model' }).click();
  await expect(page.getByLabel('Model id')).toBeFocused();
  await expect(page.getByText('A model id has only letters, digits', { exact: false })).toBeVisible();
  expect(state.keyBodies).toHaveLength(sent);

  // Gemini: a pasted "models/" prefix is dropped.
  await page.getByLabel('Provider').selectOption('gemini');
  await page.getByLabel('Model id').fill('models/gemini-x-pro');
  await page.getByLabel('API key').fill(GEMINI_KEY);
  await page.getByRole('button', { name: 'Save key' }).click();
  await expect(page.getByText('Google Gemini: Key active until 14:32')).toBeVisible();
  expect(state.keyBodies.at(-1)).toEqual({ provider: 'gemini', model: 'gemini-x-pro', key: GEMINI_KEY });
  await expect(page.getByLabel('Model', { exact: true })).toHaveValue('gemini-x-pro');
  await expectKeyGone(page, state);

  // Forgetting one provider's key leaves the others.
  await page.getByRole('button', { name: 'Forget key for OpenAI' }).click();
  await expect(page.getByText('OpenAI: Key active until 14:32')).toHaveCount(0);
  await expect(page.getByText('Google Gemini: Key active until 14:32')).toBeVisible();
  await expect(page.getByText('Anthropic: Key active until 14:32')).toBeVisible();
  await page.screenshot({ path: `${shots()}ai-settings-three-keys-1440-light.png`, fullPage: true });
});

test('the wizard waits for a model when a key was saved without one', async ({ page }) => {
  await mockApi(page, { key: 'no-model' });
  await page.goto('/admin/ai/new');
  await expect(page.getByText('Choose a model for OpenAI in AI assistant first.').first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Generate' })).toBeDisabled();
  await expect(page.getByRole('link', { name: 'Go to AI assistant' })).toHaveAttribute('href', '/admin/ai');
});

test('in testing mode the fake provider needs no key', async ({ page }) => {
  const state = await mockApi(page, { testing: true });
  await page.goto('/admin/ai');
  await expect(page.getByText('In testing mode, choose the fake provider to try drafting without a key.')).toBeVisible();
  await page.getByLabel('Provider').selectOption('fake');
  await expect(page.getByLabel('API key')).toHaveCount(0);
  await expect(page.getByText('The fake provider needs no key.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Use fake provider' }).click();
  await expect(page.getByText('Fake provider active, no key needed')).toBeVisible();
  expect(state.keyBodies).toEqual([{ provider: 'fake', model: 'canned', key: '' }]);
});

test('a wizard from a brief follows the live event to the draft', async ({ page }) => {
  const state = await mockApi(page, { key: 'active', events: 'stream', wizardOnlyByEvent: true });
  await page.goto('/admin/ai/new');
  await expect(page.getByLabel('From a brief')).toBeChecked();

  await page.getByRole('button', { name: 'Generate' }).click();
  await expect(page.getByLabel('Topic', { exact: true })).toBeFocused();
  await expect(page.getByText('Say what the problem is about', { exact: false })).toBeVisible();

  await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
  await page.getByLabel('Difficulty').selectOption('hard');
  await page.getByLabel('Constraints (optional)').fill('n up to 200000, values up to 10^9 in size');
  await page.getByLabel('Subtask ideas (optional)').fill('30 points for n up to 1000');
  await page.getByRole('button', { name: 'Generate' }).click();

  await expect(page.getByRole('heading', { name: 'Progress' })).toBeFocused();
  await expect(page.locator('.step-current')).toHaveText(/Writing the problem/);
  await expect(page).toHaveURL(new RegExp(`/admin/ai/new\\?wizard=${WIZARD_ID}$`));
  expect(state.wizardBodies).toEqual([
    {
      mode: 'brief',
      brief: { topic: 'Maximum subarray sum', difficulty: 'hard', constraints: 'n up to 200000, values up to 10^9 in size', subtasks: '30 points for n up to 1000' },
    },
  ]);
  await page.screenshot({ path: `${shots()}ai-wizard-writing-1440-light.png`, fullPage: true });

  // Only the live event can finish this wizard: GETs keep saying "thinking".
  state.events.push(
    `event: wizard\ndata: ${JSON.stringify({ id: WIZARD_ID, kind: 'generate', state: 'draft_created', problem_id: 20, provider: 'anthropic', model: 'claude-opus-5-5' })}\n\n`,
  );
  await expect(page.locator('.step-current')).toHaveText(/Generating the tests/);
  // C11: the run names the provider and model that made the draft.
  await expect(page.getByText('Drafted by Anthropic with Claude Opus 5.5.', { exact: false })).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Generating the tests' })).toHaveCount(1);
  await page.getByRole('link', { name: 'Open the draft in the editor' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
});

test('a wizard from pasted text polls when live updates are down', async ({ page }) => {
  const state = await mockApi(page, { key: 'active', events: 'down' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('From pasted text').check();
  const text = page.getByLabel('Problem text');
  await expect(text).toHaveAttribute('aria-describedby', /paste-note/);
  const pasted = 'Given an array of integers and a target, return the positions of the two numbers that add up to the target.';
  await text.fill(`  ${pasted}\n`);
  await expect(page.getByText(`${pasted.length} of 20,000 characters`)).toBeVisible();
  await page.getByRole('button', { name: 'Generate' }).click();
  await expect(page.locator('.step-current')).toHaveText(/Writing the problem/);
  await expect(page.locator('.step-current')).toHaveText(/Generating the tests/, { timeout: 10_000 });
  expect(state.wizardBodies).toEqual([{ mode: 'paste', text: pasted }]);
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toHaveAttribute('href', '/admin/problems/20');

  // A reload picks the request up from the address.
  await page.reload();
  await expect(page.getByRole('link', { name: 'Open the draft in the editor' })).toBeVisible();
});

test('a wizard failure shows the reasons and keeps the request', async ({ page }) => {
  await mockApi(page, { key: 'active', wizardEnd: 'error' });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Interval scheduling');
  await page.getByRole('button', { name: 'Generate' }).click();
  const alert = page.getByRole('alert').filter({ hasText: "The model's answer could not be used." });
  await expect(alert).toContainText("The model's answer could not be used.", { timeout: 10_000 });
  await expect(alert.getByRole('listitem')).toHaveText(['subtasks: points add up to 90, not 100.', 'reference: the C++ source is empty.']);
  await expect(page.locator('.step-failed')).toHaveText(/Writing the problem/);
  await expect(page.getByLabel('Topic', { exact: true })).toHaveValue('Interval scheduling');
  await page.screenshot({ path: `${shots()}ai-wizard-error-1440-light.png`, fullPage: true });
});

test('the editor edits the generator and plan, and generates inputs', async ({ page }) => {
  const state = await mockApi(page, { key: 'active' });
  await page.goto('/admin/problems/20');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
  await expect(page.locator('.action-bar').getByText('AI draft')).toBeVisible();
  await expect(page.getByText('The AI assistant wrote this draft.', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Tags')).toHaveValue('arrays');

  const section = page.getByRole('region', { name: 'Generator', exact: true });
  await expect(section.getByRole('group', { name: 'Generator program' })).toBeVisible();
  await expect(section.getByLabel('Subtask 1, n up to 1000')).toHaveValue('n=1000');
  const table = section.getByRole('table', { name: 'Planned tests' });
  await expect(table.getByRole('row')).toHaveCount(5);
  await expect(page.getByLabel('Row 4, arguments')).toHaveValue('max=1');

  // Add a row: the next free test number, the last row's subtask and the next seed.
  await section.getByRole('button', { name: 'Add planned test' }).click();
  await expect(page.getByLabel('Row 5, seed')).toBeFocused();
  await expect(page.getByLabel('Row 5, test number')).toHaveValue('6');
  await expect(page.getByLabel('Row 5, subtask')).toHaveValue('2');
  await expect(page.getByLabel('Row 5, seed')).toHaveValue('5');
  await page.getByLabel('Row 5, arguments').fill('n=200000 {bad}');
  await page.getByRole('button', { name: 'Remove planned test 2' }).click();
  await expect(section.getByRole('button', { name: 'Add planned test' })).toBeFocused();

  // A bad argument is caught before saving and tied to its field.
  await page.getByRole('button', { name: 'Save changes' }).click();
  const args = page.getByLabel('Row 4, arguments');
  await expect(args).toBeFocused();
  await expect(args).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByText('"{bad}" is not a valid argument.', { exact: false }).first()).toBeVisible();
  await page.getByRole('link', { name: /Planned test 4 arguments/ }).click();
  await expect(args).toBeFocused();
  await args.fill('n=200000 edge=1');
  await page.getByLabel('Subtask 2, Full range').fill('n=200000 neg=0');

  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  const put = state.puts.at(-1)!;
  expect(put.tags).toEqual(['arrays']);
  expect(put.plan).toEqual([
    { idx: 3, subtask: 1, seed: 2, args: ['neg=1'] },
    { idx: 4, subtask: 2, seed: 3, args: [] },
    { idx: 5, subtask: 2, seed: 4, args: ['max=1'] },
    { idx: 6, subtask: 2, seed: 5, args: ['n=200000', 'edge=1'] },
  ]);
  expect((put.subtasks as Json[]).map((s) => s.generator_args)).toEqual([['n=1000'], ['n=200000', 'neg=0']]);
  expect((put.generator as Json).language).toBe('python3');
  // The tag went with the person's save.
  await expect(page.locator('.action-bar').getByText('AI draft')).toHaveCount(0);

  // The first run names the failing test; the second succeeds and outputs follow.
  await section.getByRole('button', { name: 'Generate inputs' }).click();
  await expect(section.getByText('Generating inputs', { exact: false }).first()).toBeVisible();
  await expect(section.getByText('The generator failed on test 5 (subtask 2, seed 4): Runtime error')).toBeVisible({ timeout: 10_000 });
  await expect(section.getByText('Some inputs could not be generated, so none were applied.')).toBeVisible();
  await page.screenshot({ path: `${shots()}ai-editor-inputs-failed-1440-light.png`, fullPage: true });
  await section.getByRole('button', { name: 'Generate inputs' }).click();
  await expect(section.getByText('Generated 4 inputs. Outputs are being generated from the reference solution, then the problem is validated.')).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Generated 4 outputs.', { exact: false })).toBeVisible({ timeout: 10_000 });
});

test('the generator section is reachable from the keyboard on a phone', async ({ page }) => {
  await mockApi(page, { key: 'active' });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/admin/problems/20');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
  await page.getByRole('button', { name: 'Remove generator' }).focus();
  const reached: string[] = [];
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab');
    const name = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      return el ? (el.getAttribute('aria-label') || el.id || el.textContent || '').trim() : '';
    });
    reached.push(name);
    if (name === 'add-plan-row') break;
  }
  for (const name of ['generator-language', 'generator-source', 'subtask-0-args', 'subtask-1-args', 'Row 1, test number', 'Row 1, subtask', 'Row 1, seed', 'Row 1, arguments', 'Remove planned test 2', 'add-plan-row']) {
    expect(reached, `Tab did not reach "${name}"`).toContain(name);
  }
  // Rows stack: seed sits under the test number.
  const idx = await page.getByLabel('Row 1, test number').boundingBox();
  const seed = await page.getByLabel('Row 1, seed').boundingBox();
  expect(seed!.y).toBeGreaterThan(idx!.y + idx!.height - 1);
});

test('asking a model to solve a problem confirms first and shows the score', async ({ page }) => {
  await mockApi(page, { key: 'active' });
  await page.goto('/admin/problems/21');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Two sum');
  const ask = page.getByRole('button', { name: 'Ask a model to solve it' });
  await ask.click();
  const dialog = page.getByRole('dialog', { name: 'Ask a model to solve this problem?' });
  await expect(dialog).toContainText('The statement goes to Claude Opus 5.5 with your key');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(ask).toBeFocused();

  await ask.click();
  await dialog.getByRole('button', { name: 'Ask a model' }).click();
  await expect(page.getByText('Waiting for the model to answer').first()).toBeVisible();
  await expect(page.getByText("Judging the model's solution").first()).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('A model solved this on its first try: 100 of 100').first()).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Anyone who pastes the statement into a model gets full marks.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: "See the model's submission" })).toHaveAttribute('href', '/admin/problems/21/try');
  await expect(page.getByRole('button', { name: 'Ask a model again' })).toBeEnabled();
});

test('without a key the solve check says where to add one', async ({ page }) => {
  await mockApi(page);
  await page.goto('/admin/problems/21');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Two sum');
  await expect(page.getByRole('button', { name: 'Ask a model to solve it' })).toBeDisabled();
  await expect(page.getByRole('link', { name: 'Add a key in AI assistant' })).toHaveAttribute('href', '/admin/ai');
});

test('keyboard only: a brief goes from the form to the draft', async ({ page }) => {
  const state = await mockApi(page, { key: 'active', events: 'stream', wizardOnlyByEvent: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/ai/new');
  await expect(page.getByLabel('From a brief')).toBeChecked();

  await tabTo(page, page.getByLabel('Topic', { exact: true }));
  await page.keyboard.type('Maximum subarray sum');
  await activate(page, page.getByRole('button', { name: 'Generate' }));
  await expect(page.getByRole('heading', { name: 'Progress' })).toBeFocused();
  expect(state.wizardBodies).toHaveLength(1);

  state.events.push(`event: wizard\ndata: ${JSON.stringify({ id: WIZARD_ID, kind: 'generate', state: 'draft_created', problem_id: 20 })}\n\n`);
  await activate(page, page.getByRole('link', { name: 'Open the draft in the editor' }));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Maximum subarray');
});

test('the wizard follows the draft until it is validated (P3 O7)', async ({ page }) => {
  await mockApi(page, { key: 'active', chain: true });
  await page.goto('/admin/ai/new');
  await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  const progress = page.getByRole('region', { name: 'Progress' });
  await expect(progress.getByRole('listitem')).toHaveText([/Writing the problem/, /Draft created/, /Generating the tests/, /Validating the draft/]);
  await expect(progress.getByRole('listitem').nth(2)).toContainText('in progress', { timeout: 10_000 });
  await expect(progress).toContainText('Validation passed. Read the draft, then add it to a contest.', { timeout: 40_000 });
  await expect(progress.getByRole('link', { name: 'Open the draft' })).toHaveAttribute('href', '/admin/problems/20');
  await expect(progress.getByRole('listitem').nth(3)).toContainText('done');
});

test('wizard copy keeps its spaces and full stops (P3 O7)', async ({ page }) => {
  await mockApi(page, { key: 'active' });
  await page.goto('/admin/ai/new');
  await expect(page.locator('.key-state')).not.toContainText('needed Change');
  await page.getByLabel('Topic', { exact: true }).fill('Maximum subarray sum');
  await page.getByRole('button', { name: 'Generate draft' }).click();
  await expect(page.locator('.created')).toHaveText(/^Drafted by .+\. The draft is in the problem bank\./, { timeout: 10_000 });
});
