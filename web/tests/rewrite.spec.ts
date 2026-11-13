// Rewrite with AI (v2.1.0 stage W): the "Rewrite with AI" menu item, the
// wizard opened for one problem, the banner on the rewrite draft with
// Replace and Keep both, the confirm dialog and each refusal of Replace
// with the server's sentence. Every /api/ call is answered by the mocks of
// problems.spec.ts with extra routes for the key status, the wizard and
// the replace route. Problem 2 (Interval covering) is the problem being
// rewritten; problem 1 is a rewrite that has passed Validate and problem 4
// one that has not. The sweep saves screenshots at 375, 768, 1024 and 1440
// px in light and dark to $PODIUM_SHOTS (default cache/v2.1.0/shots/W/),
// checks no horizontal overflow, and runs axe on each screen.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate } from './keyboard';
import { toasts } from './locators';
import * as bank from './mocks/problems';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.1.0/shots/W/`;
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

const OLD_ID = 2;
const OLD_TITLE = 'Interval covering';
const LINK = { problem_id: OLD_ID, base: 'base-of-2', title: OLD_TITLE };
const VALIDATED_DRAFT = 1;
const UNVALIDATED_DRAFT = 4;

interface Answer {
  status: number;
  body: Record<string, unknown>;
}

const REPLACED: Answer = { status: 200, body: { problem_id: OLD_ID, copy_id: 11, copy_title: `${OLD_TITLE} (before rewrite, 15 Nov 2026)`, validation_queued: true, not_ready: [] } };

const REFUSALS: { name: string; answer: Answer; text: string }[] = [
  {
    name: 'a running contest',
    answer: { status: 409, body: { code: 'contest_running', message: `A contest that is running uses ${OLD_TITLE}, so it cannot be replaced now. Try again after the contest ends, or keep this rewrite as its own problem.` } },
    text: 'A contest that is running uses Interval covering, so it cannot be replaced now. Try again after the contest ends, or keep this rewrite as its own problem.',
  },
  {
    name: 'students who used the problem',
    answer: {
      status: 409,
      body: { code: 'in_use', message: `Students have submitted or saved code for ${OLD_TITLE}, so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests.` },
    },
    text: 'Students have submitted or saved code for Interval covering, so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests.',
  },
  {
    name: 'an archived problem',
    answer: { status: 409, body: { code: 'archived', message: `${OLD_TITLE} is archived, so it cannot be replaced. Restore it first, or keep this rewrite as its own problem.` } },
    text: 'Interval covering is archived, so it cannot be replaced. Restore it first, or keep this rewrite as its own problem.',
  },
  {
    name: 'a deleted problem',
    answer: { status: 409, body: { code: 'deleted', message: `${OLD_TITLE} was deleted, so it cannot be replaced. Keep this rewrite as its own problem.` } },
    text: 'Interval covering was deleted, so it cannot be replaced. Keep this rewrite as its own problem.',
  },
  {
    name: 'a problem that changed',
    answer: { status: 409, body: { code: 'changed', message: `${OLD_TITLE} changed after this rewrite began. Rewrite it again, or keep this rewrite as its own problem.` } },
    text: 'Interval covering changed after this rewrite began. Rewrite it again, or keep this rewrite as its own problem.',
  },
];

interface Calls {
  started: Record<string, unknown>[];
  replaced: { path: string; body: Record<string, unknown> }[];
}

interface Options {
  replace?: Answer;
  /** Problem 2 is a function problem. */
  fn?: boolean;
}

async function setup(page: Page, opts: Options = {}) {
  await bank.mockApi(page);
  const calls: Calls = { started: [], replaced: [] };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      if (path === '/api/admin/ai/key' && req.method() === 'GET') return bank.json(route, 200, KEY);
      if (opts.fn && path === `/api/admin/problems/${OLD_ID}` && req.method() === 'GET') {
        return bank.json(route, 200, { ...bank.problemFixtures()[OLD_ID], mode: 'function', locked_top: {}, locked_bottom: {} });
      }
      if (path === '/api/admin/ai/wizard' && req.method() === 'POST') {
        calls.started.push(req.postDataJSON());
        return bank.json(route, 202, { wizard_id: 'w1', server_now: bank.T0 });
      }
      if (path === '/api/admin/ai/wizard/w1' && req.method() === 'GET') {
        return bank.json(route, 200, {
          id: 'w1',
          kind: 'generate',
          state: 'draft_created',
          problem_id: VALIDATED_DRAFT,
          rewrite_of: LINK,
          provider: 'anthropic',
          model: 'claude-opus-5-5',
          started_at: bank.T0,
          server_now: bank.T0,
        });
      }
      const m = path.match(/^\/api\/admin\/problems\/(\d+)\/replace-with-rewrite$/);
      if (m && req.method() === 'POST') {
        calls.replaced.push({ path, body: req.postDataJSON() });
        const a = opts.replace ?? REPLACED;
        return a.status === 200 ? bank.json(route, 200, a.body) : bank.apiError(route, a.status, a.body.code as string, a.body.message as string);
      }
      return route.fallback();
    },
  );
  return calls;
}

/** Puts the link of a rewrite in the browser before the first page loads, once. */
async function remember(page: Page, draft: number) {
  await page.addInitScript(
    ([key, value]) => {
      if (!sessionStorage.getItem('seeded')) {
        localStorage.setItem(key, value);
        sessionStorage.setItem('seeded', '1');
      }
    },
    [`podium-rewrite:${draft}`, JSON.stringify(LINK)],
  );
}

const stored = (page: Page, draft: number) => page.evaluate((k) => localStorage.getItem(k), `podium-rewrite:${draft}`);

async function openDraft(page: Page, draft = VALIDATED_DRAFT) {
  await remember(page, draft);
  await page.goto(`/admin/problems/${draft}`);
  await expect(page.getByRole('heading', { level: 1 })).not.toHaveText('Problem');
}

const replaceButton = (page: Page) => page.getByRole('button', { name: `Replace ${OLD_TITLE} with this` });

test('the menu item comes after Edit with AI and opens the wizard for that problem', async ({ page }) => {
  await setup(page);
  await page.goto(`/admin/problems/${OLD_ID}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(OLD_TITLE);
  await page.getByRole('button', { name: 'More' }).click();
  const items = await page.locator('#problem-menu').getByRole('button').allTextContents();
  const at = items.indexOf('Edit with AI');
  expect(at).toBeGreaterThan(-1);
  expect(items[at + 1]).toBe('Rewrite with AI');
  await page.locator('#problem-menu').getByRole('button', { name: 'Rewrite with AI' }).click();
  await expect(page).toHaveURL(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByText(`Rewriting: ${OLD_TITLE}`)).toBeVisible();
});

test('an archived problem has no Rewrite with AI', async ({ page }) => {
  await setup(page);
  await page.goto('/admin/problems/3');
  await page.getByRole('button', { name: 'More' }).click();
  await expect(page.locator('#problem-menu').getByRole('button', { name: 'Rewrite with AI' })).toHaveCount(0);
});

test('the wizard for a rewrite is in paste mode, filled in, with only what should change to write', async ({ page }) => {
  await setup(page);
  await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByText(`Rewriting: ${OLD_TITLE}`)).toBeVisible();
  await expect(
    page.getByText(
      `The AI writes this problem again from what you say should change. It sees the statement, settings, reference solution and sample tests, never the hidden tests. Nothing changes on ${OLD_TITLE} until you press Replace.`,
    ),
  ).toBeVisible();
  await expect(page.getByLabel('What should change')).toHaveValue('');
  await expect(page.getByText('For example: the limits are wrong, the statement is unclear, or the tests miss a case.')).toBeVisible();
  await expect(page.getByRole('radio', { name: /A whole program/ })).toBeChecked();
  await expect(page.getByText('Start from', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Topic')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Rewrite the problem' })).toBeVisible();
  await expect(page.getByText('Starting from a problem you already have?')).toHaveCount(0);
});

test('a function problem opens with One function chosen', async ({ page }) => {
  await setup(page, { fn: true });
  await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByRole('radio', { name: /One function, between locked code/ })).toBeChecked();
  await expect(page.getByText('In Java, students may use')).toBeVisible();
});

test('the new problem page points to Edit with AI and Rewrite with AI', async ({ page }) => {
  await setup(page);
  await page.goto('/admin/ai/new');
  await expect(page.getByText('Starting from a problem you already have? Use Edit with AI or Rewrite with AI on its page.')).toBeVisible();
});

test('an empty box is refused on the page without a request', async ({ page }) => {
  const calls = await setup(page);
  await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByText(`Rewriting: ${OLD_TITLE}`)).toBeVisible();
  await page.getByRole('button', { name: 'Rewrite the problem' }).click();
  await expect(page.getByText('Say what should change.')).toBeVisible();
  await expect(page.getByLabel('What should change')).toBeFocused();
  expect(calls.started).toEqual([]);
});

test('a problem that cannot be read says so and shows no form', async ({ page }) => {
  await setup(page);
  await page.goto('/admin/ai/new?rewrite=99');
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByLabel('What should change')).toHaveCount(0);
});

test('an archived problem is not rewritten', async ({ page }) => {
  await setup(page);
  await page.goto('/admin/ai/new?rewrite=3');
  await expect(page.getByRole('alert')).toContainText('is archived. Restore it first, then rewrite it.');
  await expect(page.getByLabel('What should change')).toHaveCount(0);
});

test('a rewrite sends the problem and the change, follows the run and remembers what it rewrites', async ({ page }) => {
  const calls = await setup(page);
  await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByText(`Rewriting: ${OLD_TITLE}`)).toBeVisible();
  await page.getByLabel('What should change').fill('The limits are wrong.');
  await page.getByRole('button', { name: 'Rewrite the problem' }).click();
  expect(calls.started).toEqual([{ mode: 'paste', text: 'The limits are wrong.', rewrite_of: OLD_ID }]);
  await expect(page.getByRole('link', { name: /Open the draft/ })).toHaveAttribute('href', `/admin/problems/${VALIDATED_DRAFT}`, { timeout: 15_000 });
  await expect(page.getByText(`Rewriting: ${OLD_TITLE}`)).toBeVisible();
  expect(JSON.parse((await stored(page, VALIDATED_DRAFT)) ?? 'null')).toEqual(LINK);
});

test('a function rewrite sends the function choice', async ({ page }) => {
  const calls = await setup(page, { fn: true });
  await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
  await expect(page.getByRole('radio', { name: /One function/ })).toBeChecked();
  await page.getByLabel('What should change').fill('Add a case.');
  await page.getByRole('button', { name: 'Rewrite the problem' }).click();
  expect(calls.started).toEqual([{ mode: 'paste', text: 'Add a case.', rewrite_of: OLD_ID, students_write: 'function', java_lists: 'array' }]);
});

test('the draft has the banner, and Replace waits for Validate', async ({ page }) => {
  await setup(page);
  await openDraft(page, UNVALIDATED_DRAFT);
  await expect(page.getByText(`This is a rewrite of ${OLD_TITLE}.`)).toBeVisible();
  await expect(replaceButton(page)).toBeDisabled();
  await expect(replaceButton(page)).toHaveAccessibleDescription('Validate this rewrite first.');
  await expect(page.getByRole('button', { name: 'Keep both' })).toBeEnabled();
});

test('a validated draft can be replaced only with the saved content', async ({ page }) => {
  await setup(page);
  await openDraft(page);
  await expect(replaceButton(page)).toBeEnabled();
  await page.getByLabel('Title', { exact: true }).fill('A plus B, edited');
  await expect(replaceButton(page)).toBeDisabled();
  await expect(replaceButton(page)).toHaveAccessibleDescription('Save your changes first.');
});

test('a problem with no link has no banner', async ({ page }) => {
  await setup(page);
  await page.goto(`/admin/problems/${VALIDATED_DRAFT}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.getByText('This is a rewrite of')).toHaveCount(0);
});

test('Replace asks first, with the title, the date and where the old version goes, and Cancel changes nothing', async ({ page }) => {
  const calls = await setup(page);
  await openDraft(page);
  await replaceButton(page).click();
  const dialog = page.getByRole('dialog', { name: `Replace ${OLD_TITLE}?` });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  const body = await dialog.locator('.body').innerText();
  expect(body).toMatch(
    new RegExp(
      `^${OLD_TITLE} gets this rewrite's statement, tests and solutions\\. Your own reference solution and brute force are replaced by the AI's, so a contest will ask before taking the problem until you review it\\. The current version is kept as an archived problem, ${OLD_TITLE} \\(before rewrite, \\d{1,2} [A-Z][a-z]{2} \\d{4}\\)\\. A contest that already holds ${OLD_TITLE} keeps the old version until you press Update students now\\.$`,
    ),
  );
  await expect(dialog.getByRole('button', { name: `Replace ${OLD_TITLE}`, exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(replaceButton(page)).toBeFocused();
  expect(calls.replaced).toEqual([]);
});

test('Replace sends the rewrite and what it was made from, thanks, and opens the old problem', async ({ page }) => {
  const calls = await setup(page);
  await openDraft(page);
  await replaceButton(page).click();
  await page.getByRole('dialog').getByRole('button', { name: `Replace ${OLD_TITLE}`, exact: true }).click();
  await expect(toasts(page)).toContainText(`Replaced ${OLD_TITLE}. Validation has started.`);
  await expect(page).toHaveURL(`/admin/problems/${OLD_ID}`);
  expect(calls.replaced).toEqual([{ path: `/api/admin/problems/${OLD_ID}/replace-with-rewrite`, body: { draft_id: VALIDATED_DRAFT, base: LINK.base } }]);
  expect(await stored(page, VALIDATED_DRAFT)).toBeNull();
});

test('the whole flow from the keyboard: Replace, read the dialog, confirm', async ({ page }) => {
  const calls = await setup(page);
  await openDraft(page);
  await activate(page, replaceButton(page));
  const dialog = page.getByRole('dialog', { name: `Replace ${OLD_TITLE}?` });
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await activate(page, dialog.getByRole('button', { name: `Replace ${OLD_TITLE}`, exact: true }));
  await expect(page).toHaveURL(`/admin/problems/${OLD_ID}`);
  expect(calls.replaced).toHaveLength(1);
});

for (const refusal of REFUSALS) {
  test(`Replace refused for ${refusal.name} says why and keeps the rewrite`, async ({ page }) => {
    const calls = await setup(page, { replace: refusal.answer });
    await openDraft(page);
    await replaceButton(page).click();
    await page.getByRole('dialog').getByRole('button', { name: `Replace ${OLD_TITLE}`, exact: true }).click();
    await expect(page.getByRole('alert').filter({ hasText: refusal.text })).toBeVisible();
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(page).toHaveURL(`/admin/problems/${VALIDATED_DRAFT}`);
    await expect(page.getByRole('button', { name: 'Keep both' })).toBeVisible();
    expect(calls.replaced).toHaveLength(1);
    expect(await stored(page, VALIDATED_DRAFT)).not.toBeNull();
  });
}

test('Keep both removes the banner and the link, with its toast', async ({ page }) => {
  const calls = await setup(page);
  await openDraft(page);
  await page.getByRole('button', { name: 'Keep both' }).click();
  await expect(toasts(page)).toContainText('Kept as its own problem.');
  await expect(page.getByText('This is a rewrite of')).toHaveCount(0);
  expect(await stored(page, VALIDATED_DRAFT)).toBeNull();
  await page.reload();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await expect(page.getByText('This is a rewrite of')).toHaveCount(0);
  expect(calls.replaced).toEqual([]);
});

const SCREENS = [
  {
    name: 'rewrite-wizard',
    run: async (page: Page) => {
      await setup(page);
      await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
      await expect(page.getByLabel('What should change')).toBeVisible();
    },
  },
  {
    name: 'rewrite-wizard-function',
    run: async (page: Page) => {
      await setup(page, { fn: true });
      await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
      await expect(page.getByText('In Java, students may use')).toBeVisible();
    },
  },
  {
    name: 'rewrite-wizard-empty',
    run: async (page: Page) => {
      await setup(page);
      await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
      await page.getByRole('button', { name: 'Rewrite the problem' }).click();
      await expect(page.getByText('Say what should change.')).toBeVisible();
    },
  },
  {
    name: 'rewrite-wizard-result',
    run: async (page: Page) => {
      await setup(page);
      await page.goto(`/admin/ai/new?rewrite=${OLD_ID}`);
      await page.getByLabel('What should change').fill('The limits are wrong.');
      await page.getByRole('button', { name: 'Rewrite the problem' }).click();
      await expect(page.getByRole('link', { name: /Open the draft/ })).toBeVisible({ timeout: 15_000 });
    },
  },
  {
    name: 'rewrite-banner-waiting',
    run: async (page: Page) => {
      await setup(page);
      await openDraft(page, UNVALIDATED_DRAFT);
      await expect(page.getByText('Validate this rewrite first.')).toBeVisible();
    },
  },
  {
    name: 'rewrite-banner-ready',
    run: async (page: Page) => {
      await setup(page);
      await openDraft(page);
      await expect(replaceButton(page)).toBeEnabled();
    },
  },
  {
    name: 'rewrite-confirm',
    run: async (page: Page) => {
      await setup(page);
      await openDraft(page);
      await replaceButton(page).click();
      await expect(page.getByRole('dialog', { name: `Replace ${OLD_TITLE}?` })).toBeVisible();
    },
  },
  ...REFUSALS.map((refusal) => ({
    name: `rewrite-refused-${refusal.answer.body.code as string}`,
    run: async (page: Page) => {
      await setup(page, { replace: refusal.answer });
      await openDraft(page);
      await replaceButton(page).click();
      await page.getByRole('dialog').getByRole('button', { name: `Replace ${OLD_TITLE}`, exact: true }).click();
      await expect(page.getByRole('alert').filter({ hasText: refusal.text })).toBeVisible();
    },
  })),
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
