// AI edit of a problem (v2.0.2 stage E): the "Edit with AI" panel in the
// problem editor, and in v2.1.0 stage A the kept note, the refusal with its
// two disclosures, the mode refusal and Close. Every /api/ call is answered by
// the mocks of problems.spec.ts with extra routes for the key status and the
// three ai-edit routes. The sweep saves screenshots at 375, 768, 1024 and 1440
// px in light and dark to $PODIUM_SHOTS (default cache/v2.0.2/shots/sE/),
// checks no horizontal overflow, and runs axe on the panel with a draft or a
// refusal showing.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate, tabTo } from './keyboard';
import { toasts } from './locators';
import * as bank from './mocks/problems';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.0.2/shots/sE/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const KEY = {
  active: true,
  provider: 'anthropic',
  model: 'claude-opus-5-5',
  active_until: bank.T0 + 3_600_000,
  server_now: bank.T0,
  providers: [
    { id: 'anthropic', name: 'Anthropic', has_key: true, model: 'claude-opus-5-5', active_until: bank.T0 + 3_600_000, models: [{ id: 'claude-opus-5-5', name: 'Claude Opus 5.5' }] },
    { id: 'openai', name: 'OpenAI', has_key: false, model: '', active_until: null, models: [] },
    { id: 'gemini', name: 'Google Gemini', has_key: false, model: '', active_until: null, models: [] },
  ],
};

const DRAFT = {
  draft_id: 'd1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1',
  summary: 'Added a line about negative sums to the statement.',
  kept: [] as string[],
  provider: 'anthropic',
  model: 'claude-opus-5-5',
  files: [
    {
      path: 'statement.md',
      status: 'changed',
      before: 'Add two integers.\n\n## Input\n\nTwo integers.\n',
      after: 'Add two integers. Either may be negative.\n\n## Input\n\nTwo integers.\n',
    },
  ],
};

interface Calls {
  asked: Record<string, unknown>[];
  accepted: string[];
  rejected: string[];
}

interface Answer {
  status: number;
  body?: unknown;
}

const RUNNING: Answer = { status: 409, body: { code: 'contest_running', message: 'A contest that holds this problem is running. Accept after it ends.' } };

function refusal(route: Parameters<typeof bank.json>[0], a: Answer) {
  const { code, message, ...extra } = a.body as { code: string; message: string };
  return bank.apiError(route, a.status, code, message, extra);
}

const REFUSED_TEXT = "The AI's edit would break this problem, so nothing was saved. Press Ask for a draft to try again, describe a smaller change, or rewrite the whole problem.";
const MODE_TEXT = 'Edit with AI cannot change whether students write a whole program or one function. Use Rewrite with AI and choose there.';

const REFUSED: Answer = {
  status: 422,
  body: {
    code: 'invalid_answer',
    message: REFUSED_TEXT,
    problems: ['problem.yaml tests.yaml: Planned tests need a generator. Add one, or remove the plan.'],
    sent_back: {
      answers: [
        { summary: 'Second try.', files: [{ path: 'problem.yaml', content: 'format: 1\ntitle: Largest gap\n' }], cut: false },
        { summary: 'First try.', files: [{ path: 'problem.yaml', content: 'format: 1\ntitle: Largest gap, first\n' }], cut: true },
      ],
    },
  },
};

const MODE_REFUSED: Answer = { status: 422, body: { code: 'mode_change', message: MODE_TEXT } };

async function setup(page: Page, ask: Answer = { status: 200, body: DRAFT }, accept: Answer = { status: 200, body: { problem_id: 1, validation_queued: true, not_ready: [] } }) {
  await bank.mockApi(page);
  const calls: Calls = { asked: [], accepted: [], rejected: [] };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (path === '/api/admin/ai/key' && req.method() === 'GET') return bank.json(route, 200, KEY);
      const m = path.match(/^\/api\/admin\/problems\/1\/ai-edit(?:\/([^/]+?)(\/accept)?)?$/);
      if (!m) return route.fallback();
      if (!m[1] && req.method() === 'POST') {
        calls.asked.push(req.postDataJSON());
        return ask.status === 200 ? bank.json(route, 200, ask.body) : refusal(route, ask);
      }
      if (m[2] && req.method() === 'POST') {
        calls.accepted.push(m[1]);
        return accept.status === 200 ? bank.json(route, 200, accept.body) : refusal(route, accept);
      }
      if (req.method() === 'DELETE') {
        calls.rejected.push(m[1]);
        return bank.json(route, 204);
      }
      return route.fallback();
    },
  );
  return calls;
}

async function openPanel(page: Page) {
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: 'Edit with AI' }).click();
  await expect(page.getByRole('heading', { name: 'Edit with AI' })).toBeFocused();
}

test('the whole flow from the keyboard: ask, read the diff, accept', async ({ page }) => {
  const calls = await setup(page);
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await activate(page, page.getByRole('button', { name: 'More' }));
  await activate(page, page.locator('#problem-menu').getByRole('button', { name: 'Edit with AI' }));
  await expect(page.getByText('Using Anthropic, Claude Opus 5.5.')).toBeVisible();

  await tabTo(page, page.getByLabel('What should change'));
  await page.keyboard.type('Say that sums can be negative.');
  await activate(page, page.getByRole('button', { name: 'Ask for a draft' }));

  await expect(page.getByRole('heading', { name: 'Draft' })).toBeFocused();
  await expect(page.getByText(DRAFT.summary)).toBeVisible();
  const diff = page.getByRole('region', { name: /statement\.md, the draft compared/ });
  await expect(diff).toContainText('- Add two integers.');
  await expect(diff).toContainText('+ Add two integers. Either may be negative.');
  expect(calls.asked).toEqual([{ instruction: 'Say that sums can be negative.', provider: 'anthropic' }]);

  await activate(page, page.getByRole('button', { name: 'Accept the edit' }));
  await expect(toasts(page)).toContainText('Edit accepted. Validation has started.');
  await expect(page.getByRole('heading', { name: 'Edit with AI' })).toHaveCount(0);
  expect(calls.accepted).toEqual([DRAFT.draft_id]);
});

test('Reject forgets the draft and changes nothing', async ({ page }) => {
  const calls = await setup(page);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it harder.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await page.getByRole('button', { name: 'Reject the edit' }).click();
  await expect(page.getByRole('heading', { name: 'Draft' })).toHaveCount(0);
  await expect(page.getByLabel('What should change')).toBeFocused();
  expect(calls.rejected).toEqual([DRAFT.draft_id]);
  expect(calls.accepted).toEqual([]);
});

test('Accept is disabled while the editor has unsaved changes, with the reason beside it', async ({ page }) => {
  const calls = await setup(page);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it harder.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  const accept = page.getByRole('button', { name: 'Accept the edit' });
  await expect(accept).toBeEnabled();
  await page.getByLabel('Title', { exact: true }).fill('A plus B, edited');
  await expect(accept).toBeDisabled();
  await expect(page.getByText('You have unsaved changes. Save or discard them first')).toBeVisible();
  await expect(accept).toHaveAccessibleDescription(/Save or discard them first/);
  expect(calls.accepted).toEqual([]);
});

test('the key form inside the panel is a level 3 heading under the panel level 2', async ({ page }) => {
  await setup(page);
  await openPanel(page);
  await page.getByRole('button', { name: 'Change key or model' }).click();
  await expect(page.getByRole('heading', { level: 3, name: /key or model/ })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: /key or model/ })).toHaveCount(0);
});

test('a running contest refuses Accept with the server sentence and keeps the draft', async ({ page }) => {
  const calls = await setup(page, undefined, RUNNING);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it harder.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await page.getByRole('button', { name: 'Accept the edit' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'A contest that holds this problem is running. Accept after it ends.' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reject the edit' })).toBeEnabled();
  expect(calls.accepted).toHaveLength(1);
});

test('what the pack cannot carry is shown as the server wrote it', async ({ page }) => {
  await setup(page, { status: 422, body: { code: 'not_in_pack', message: 'A pack cannot carry that change: contest settings are not in a pack.' } });
  await openPanel(page);
  await page.getByLabel('What should change').fill('Move the contest start.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await expect(page.getByRole('alert')).toContainText('A pack cannot carry that change');
  await expect(page.getByRole('heading', { name: 'Draft' })).toHaveCount(0);
});

test('the draft names what the AI left out and the server kept', async ({ page }) => {
  await setup(page, { status: 200, body: { ...DRAFT, kept: ['the test generator', 'the generator settings of each subtask'] } });
  await openPanel(page);
  await page.getByLabel('What should change').fill('Rename it.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await expect(page.getByText('The AI left these out, so your current ones were kept: the test generator, the generator settings of each subtask.')).toBeVisible();
});

test('a draft with nothing kept has no kept note', async ({ page }) => {
  await setup(page);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Rename it.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await expect(page.getByRole('button', { name: 'Accept the edit' })).toBeVisible();
  await expect(page.getByText('The AI left these out')).toHaveCount(0);
});

test('a refusal leads with one sentence, keeps focus on Ask and holds two closed disclosures', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await setup(page, REFUSED);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Rename it.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();

  const banner = page.getByRole('alert').filter({ hasText: REFUSED_TEXT });
  await expect(banner).toBeVisible();
  await expect(page.getByRole('button', { name: 'Ask for a draft' })).toBeFocused();
  const details = banner.locator('details').first();
  const sent = banner.locator('details').nth(1);
  await expect(details).not.toHaveAttribute('open', '');
  await expect(sent).not.toHaveAttribute('open', '');
  await expect(banner.getByText('Planned tests need a generator')).toBeHidden();

  // From the keyboard: Details, What the AI sent back, then the Copy buttons.
  await tabTo(page, details.locator('summary'));
  await page.keyboard.press('Enter');
  await expect(banner.getByText('Planned tests need a generator')).toBeVisible();
  await tabTo(page, sent.locator('summary'));
  await page.keyboard.press('Enter');
  await expect(sent.getByText('Send this to whoever runs Podium if it keeps happening.')).toBeVisible();
  await expect(sent.getByText('Second answer')).toBeVisible();
  await expect(sent.getByText('First answer')).toBeVisible();
  await expect(sent.getByText('Cut at 200 KB. The rest was not kept.')).toBeVisible();
  await expect(sent.getByRole('region', { name: 'problem.yaml, as the AI sent it' })).toHaveCount(2);

  const copy = sent.locator('.sent-file button').first();
  await tabTo(page, copy);
  await page.keyboard.press('Enter');
  await expect(copy).toContainText('Copied');
  await expect(toasts(page)).toContainText('Copied problem.yaml');
  const clip = await page.evaluate(() => navigator.clipboard.readText());
  expect(clip.replaceAll('\r\n', '\n')).toBe('format: 1\ntitle: Largest gap\n');
  await expect(copy).not.toContainText('Copied', { timeout: 4000 });
});

test('a copy that the browser refuses says so', async ({ page }) => {
  await setup(page, REFUSED);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.reject(new Error('no')) }, configurable: true });
  });
  await openPanel(page);
  await page.getByLabel('What should change').fill('Rename it.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  const sent = page.locator('details').filter({ hasText: 'What the AI sent back' });
  await sent.locator('summary').click();
  await sent.getByRole('button', { name: /^Copy problem\.yaml$/ }).first().click();
  await expect(page.getByText('Could not copy. Select the text and copy it by hand.')).toBeVisible();
});

test('an answer that was not JSON is shown as the text the AI sent', async ({ page }) => {
  const body = REFUSED.body as { code: string; message: string; problems: string[] };
  await setup(page, { status: 422, body: { ...body, sent_back: { answers: [{ summary: '', files: [], raw: 'I could not do that, sorry.', cut: false }] } } });
  await openPanel(page);
  await page.getByLabel('What should change').fill('Rename it.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  const sent = page.locator('details').filter({ hasText: 'What the AI sent back' });
  await sent.locator('summary').click();
  await expect(sent.getByRole('region', { name: 'The answer, as the AI sent it' })).toContainText('I could not do that, sorry.');
});

test('a change of mode is refused with its own sentence and nothing to expand', async ({ page }) => {
  await setup(page, MODE_REFUSED);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it a function problem.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await expect(page.getByRole('alert').filter({ hasText: MODE_TEXT })).toBeVisible();
  await expect(page.getByRole('alert').locator('details')).toHaveCount(0);
});

test('Close hides the panel and returns focus to the menu; Reject says so', async ({ page }) => {
  await setup(page);
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it harder.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await page.getByRole('button', { name: 'Reject the edit' }).click();
  await expect(toasts(page)).toContainText('Draft rejected');
  await page.getByRole('button', { name: 'Close' }).click();
  await expect(page.getByRole('heading', { name: 'Edit with AI' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'More' })).toBeFocused();
});

test('while the AI works the button and a status line say so and take focus', async ({ page }) => {
  await setup(page);
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/admin/problems/1/ai-edit', async (route) => {
    if (route.request().method() === 'POST') await gate;
    await route.fallback();
  });
  await openPanel(page);
  await page.getByLabel('What should change').fill('Make it harder.');
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  const status = page.getByText('This takes a minute or two, and the AI may try twice. Stay on this page.');
  await expect(status).toBeFocused();
  await expect(page.getByRole('button', { name: 'Asking the AI' })).toBeDisabled();
  release();
  await expect(page.getByRole('heading', { name: 'Draft' })).toBeFocused();
});

test('an empty instruction is refused on the page without a request', async ({ page }) => {
  const calls = await setup(page);
  await openPanel(page);
  await page.getByRole('button', { name: 'Ask for a draft' }).click();
  await expect(page.getByText('Say what to change.')).toBeVisible();
  await expect(page.getByLabel('What should change')).toBeFocused();
  expect(calls.asked).toEqual([]);
});

const SCREENS = [
  {
    name: 'ai-edit-draft',
    run: async (page: Page) => {
      await setup(page);
      await openPanel(page);
      await page.getByLabel('What should change').fill('Say that sums can be negative.');
      await page.getByRole('button', { name: 'Ask for a draft' }).click();
      await expect(page.getByRole('button', { name: 'Accept the edit' })).toBeVisible();
    },
  },
  {
    name: 'ai-edit-kept',
    run: async (page: Page) => {
      await setup(page, { status: 200, body: { ...DRAFT, kept: ['the test generator', 'the generator settings of each subtask'] } });
      await openPanel(page);
      await page.getByLabel('What should change').fill('Rename it.');
      await page.getByRole('button', { name: 'Ask for a draft' }).click();
      await expect(page.getByText('The AI left these out')).toBeVisible();
    },
  },
  {
    name: 'ai-edit-refused-open',
    run: async (page: Page) => {
      await setup(page, REFUSED);
      await openPanel(page);
      await page.getByLabel('What should change').fill('Rename it.');
      await page.getByRole('button', { name: 'Ask for a draft' }).click();
      const banner = page.getByRole('alert').filter({ hasText: REFUSED_TEXT });
      await banner.locator('summary').first().click();
      await banner.locator('summary').last().click();
      await expect(banner.getByText('Second answer')).toBeVisible();
    },
  },
  {
    name: 'ai-edit-mode',
    run: async (page: Page) => {
      await setup(page, MODE_REFUSED);
      await openPanel(page);
      await page.getByLabel('What should change').fill('Make it a function problem.');
      await page.getByRole('button', { name: 'Ask for a draft' }).click();
      await expect(page.getByRole('alert').filter({ hasText: MODE_TEXT })).toBeVisible();
    },
  },
  {
    name: 'ai-edit-refused',
    run: async (page: Page) => {
      await setup(page, undefined, RUNNING);
      await openPanel(page);
      await page.getByLabel('What should change').fill('Make it harder.');
      await page.getByRole('button', { name: 'Ask for a draft' }).click();
      await page.getByRole('button', { name: 'Accept the edit' }).click();
      await expect(page.getByRole('alert').filter({ hasText: 'Accept after it ends.' })).toBeVisible();
    },
  },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    test.describe.configure({ timeout: 60_000 });

    for (const screen of SCREENS) {
      test(screen.name, async ({ page }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await screen.run(page);
        await page.evaluate(() => document.fonts.ready);

        const results = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
        const bad = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.help} (${v.nodes[0].target.join(' ')})`);
        expect(bad, `axe findings on ${screen.name}`).toEqual([]);

        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          await page.screenshot({ path: `${shotDir()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}
