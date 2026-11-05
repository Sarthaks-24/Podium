// The screens the audit visits (V201-16). To add one, add an entry:
//
//   name   the file stem of its screenshots and its heading in REPORT.md
//   path   the route to open
//   mock   installs the API mock (tests/mocks/) for the page
//   ready  optional: waits until the page has loaded (defaults to main or h1)
//   steps  optional: reaches the state to audit, for example opens a dialog
//
// A state that leaves a <dialog> open also gets the focus-after-Escape check.
import { expect, type Page, type Route } from '@playwright/test';
import * as ai from '../mocks/ai';
import * as contests from '../mocks/contests';
import * as problems from '../mocks/problems';
import * as screens from '../mocks/screens';
import * as workspace from '../mocks/workspace';
import { codeEditor } from '../locators';

export interface AuditScreen {
  name: string;
  path: string;
  mock: (page: Page) => Promise<unknown>;
  ready?: (page: Page) => Promise<void>;
  steps?: (page: Page) => Promise<void>;
}

type Json = Record<string, unknown>;

const bank = async (page: Page) => problems.mockApi(page);
const bankRestoreTaken = async (page: Page) => {
  const state = await problems.mockApi(page);
  state.restoreTaken = true;
};
const contestsAs = (options: contests.Options) => (page: Page) => contests.mockApi(page, options);
const organiser = contestsAs({ who: 'organiser', phase: 'during' });

const json = (route: Route, status: number, body: unknown) => route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: JSON.stringify(body) });

const bankLoaded = (page: Page) => expect(page.getByRole('link', { name: 'Interval covering', exact: true })).toBeVisible();
const contestsLoaded = (page: Page) => expect(page.getByRole('link', { name: 'Week 7 contest', exact: true })).toBeVisible();
const editorLoaded = (page: Page) => expect(page.getByLabel('Title', { exact: true })).toHaveValue('Week 6 contest');

/** Opens "Theme and colours" from the account menu (the Menu panel under 768 px). */
async function themeDialog(page: Page) {
  await page.route('**/api/me/preferences', (route) => json(route, 200, {}));
  if ((page.viewportSize()?.width ?? 1440) < 768) {
    await page.getByRole('button', { name: 'Menu' }).click();
    await page.locator('#site-menu').getByRole('button', { name: 'Theme and colours' }).click();
  } else {
    await page.getByRole('button', { name: /Priya Raman|Account/ }).click();
    await page.getByRole('button', { name: 'Theme and colours' }).click();
  }
  const dialog = page.getByRole('dialog', { name: 'Theme and colours' });
  await expect(dialog).toBeVisible();
  return dialog;
}

const owner = (page: Page) => screens.mockApi(page, 'owner');
const ownerReady = (page: Page) => expect(page.locator('h1')).toBeVisible();

const conflictDialog = (page: Page) => page.getByRole('dialog', { name: 'A problem with the slug a-plus-b already exists' });

/** Picks a pack whose slug (a-plus-b) is taken, which opens the import conflict dialog. */
async function importConflict(page: Page) {
  await page.locator('input[type="file"]').setInputFiles(problems.zipFile('dupe.zip', 'dupe pack'));
  await expect(conflictDialog(page)).toBeVisible();
}

/** Opens a problem's menu, picks an item, and waits for the named dialog. */
async function fromMenu(page: Page, item: string, dialog: string) {
  await page.getByRole('button', { name: 'More' }).click();
  await page.locator('#problem-menu').getByRole('button', { name: item }).click();
  await expect(page.getByRole('dialog', { name: dialog })).toBeVisible();
}

/** The Restore confirm on the bank's archived row, then the slug step (the mock says the slug is taken). */
async function bankRestore(page: Page, toSlugStep: boolean) {
  await page.getByRole('button', { name: 'Restore Old sorting' }).click();
  const confirm = page.getByRole('dialog', { name: 'Restore Old sorting?' });
  await expect(confirm).toBeVisible();
  if (!toSlugStep) return;
  await confirm.getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('dialog', { name: 'Restore Old sorting' }).getByLabel('New address')).toBeVisible();
}

/** POST .../restore of a contest answers 409 with a free slug, as the server does when the address was taken. */
async function takenSlug(page: Page, id: number) {
  await page.route(`**/api/admin/contests/${id}/restore`, (route) => {
    const body = route.request().postDataJSON() as Json;
    if (body.slug !== '') return json(route, 200, {});
    const message = 'Another contest now uses week-4. Choose a new slug.';
    return json(route, 409, { error: { code: 'conflict', message, fields: { slug: message }, suggested_slug: 'week-4-2' } });
  });
}

/**
 * The Contests list with "Show archived" on and one archived contest (Week 4),
 * built from the mock's own list.
 */
async function archivedContests(page: Page) {
  const real = await page.evaluate(async () => (await fetch('/api/admin/contests')).json());
  const first = real.contests[0];
  const gap = 14 * 86_400_000;
  const week4 = { ...first, id: 9, slug: 'week-4', title: 'Week 4 contest', archived: true, published: true, state: 'ended', problems: [], starts_at: first.starts_at - gap, ends_at: first.ends_at - gap, close_at: first.ends_at - gap };
  await page.route(
    (url) => url.pathname === '/api/admin/contests' && url.searchParams.get('archived') === '1',
    (route) => json(route, 200, { ...real, contests: [...real.contests, week4] }),
  );
  await takenSlug(page, 9);
  await page.getByLabel('Show archived').check();
  await expect(page.getByRole('button', { name: 'Restore Week 4 contest' })).toBeVisible();
}

async function contestsRestore(page: Page, toSlugStep: boolean) {
  await archivedContests(page);
  const confirm = page.getByRole('dialog', { name: 'Restore Week 4 contest?' });
  // From the keyboard: at 768 px Playwright's click hangs scrolling this button into view.
  await page.getByRole('button', { name: 'Restore Week 4 contest' }).focus();
  await page.keyboard.press('Enter');
  await expect(confirm).toBeVisible();
  if (!toSlugStep) return;
  await confirm.getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('dialog', { name: 'Restore Week 4 contest' }).getByLabel('New address')).toBeVisible();
}

/** The contest editor for an archived contest: More, Restore, and optionally on to the slug step. */
async function editorRestore(page: Page, toSlugStep: boolean) {
  const real = await page.evaluate(async () => (await fetch('/api/admin/contests/4')).json());
  await page.route('**/api/admin/contests/4', (route) => (route.request().method() === 'GET' ? json(route, 200, { ...real, archived: true, archived_slug: 'week-6' }) : route.fallback()));
  await takenSlug(page, 4);
  await page.reload();
  await editorLoaded(page);
  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('button', { name: 'Restore' }).first().click();
  const confirm = page.getByRole('dialog', { name: 'Restore Week 6 contest?' });
  await expect(confirm).toBeVisible();
  if (!toSlugStep) return;
  await confirm.getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('dialog', { name: 'Restore Week 6 contest' }).getByLabel('New address')).toBeVisible();
}

/** An admin sees the wizard's custom-prompt note: the key route says so and /api/me says admin. */
async function customPromptNote(page: Page) {
  const real = await page.evaluate(async () => (await fetch('/api/admin/ai/key')).json());
  const me = await page.evaluate(async () => (await fetch('/api/me')).json());
  await page.route('**/api/admin/ai/key', (route) => (route.request().method() === 'GET' ? json(route, 200, { ...real, custom_prompt: true }) : route.fallback()));
  await page.route('**/api/me', (route) => json(route, 200, { ...me, role: 'admin' }));
  await page.reload();
  await expect(page.getByText('This site uses a custom prompt.')).toBeVisible();
}

const PROMPT = 'You are helping an organiser write a problem.\nproblem.yaml\n=== PROBLEM DESCRIPTION ===\n';

const TABBED = new Set(['contest-during', 'contest-console']);

export const AUDIT_SCREENS: AuditScreen[] = [
  // Problem bank, archive and restore.
  { name: 'bank', path: '/admin/problems', mock: bank, ready: bankLoaded },
  { name: 'bank-archived', path: '/admin/problems?archived=1', mock: bank, ready: (p) => expect(p.getByRole('button', { name: 'Restore Old sorting' })).toBeVisible() },
  {
    name: 'bank-archive-dialog',
    path: '/admin/problems/1',
    mock: async (page) => {
      const state = await problems.mockApi(page);
      state.problems[1].open_contests = [{ title: 'Week 6 practice' }, { title: 'Week 7 contest' }];
    },
    steps: (p) => fromMenu(p, 'Archive', 'Archive A plus B?'),
  },
  { name: 'bank-restore-confirm', path: '/admin/problems?archived=1', mock: bankRestoreTaken, steps: (p) => bankRestore(p, false) },
  { name: 'bank-restore-slug', path: '/admin/problems?archived=1', mock: bankRestoreTaken, steps: (p) => bankRestore(p, true) },
  { name: 'problem-restore-confirm', path: '/admin/problems/3', mock: bank, steps: (p) => fromMenu(p, 'Restore', 'Restore Old sorting?') },

  // Import conflict.
  { name: 'import-conflict', path: '/admin/problems', mock: bank, ready: bankLoaded, steps: importConflict },
  {
    name: 'import-conflict-archive',
    path: '/admin/problems',
    mock: bank,
    ready: bankLoaded,
    steps: async (p) => {
      await importConflict(p);
      await conflictDialog(p).getByRole('radio', { name: /Archive the existing problem/ }).check();
      await expect(conflictDialog(p).getByText('Week 6 practice', { exact: true })).toBeVisible();
    },
  },
  {
    name: 'import-conflict-replace-disabled',
    path: '/admin/problems',
    mock: async (page) => {
      const state = await problems.mockApi(page);
      state.importLive = true;
    },
    ready: bankLoaded,
    steps: async (p) => {
      await importConflict(p);
      await expect(conflictDialog(p).getByRole('radio', { name: /Replace the existing problem/ })).toBeDisabled();
    },
  },
  {
    name: 'import-conflict-slug-error',
    path: '/admin/problems',
    mock: bank,
    ready: bankLoaded,
    steps: async (p) => {
      await importConflict(p);
      await conflictDialog(p).getByLabel('New slug').fill('');
      await conflictDialog(p).getByRole('button', { name: 'Import as new' }).click();
      await expect(conflictDialog(p).getByText('Enter a slug.')).toBeVisible();
    },
  },
  {
    name: 'new-problem-same-title',
    path: '/admin/problems',
    mock: bank,
    ready: bankLoaded,
    steps: async (p) => {
      await p.getByRole('button', { name: 'New problem' }).click();
      await p.getByLabel('Title', { exact: true }).fill('Old sorting');
      await expect(p.locator('#create-problem').getByRole('status')).toContainText('A problem called Old sorting already exists.');
    },
  },

  // Prompt for another AI, and the wizard note.
  {
    name: 'prompt-panel',
    path: '/admin/problems',
    mock: async (page) => {
      await problems.mockApi(page);
      await page.route('**/api/admin/problems/pack-prompt', (route) => json(route, 200, { prompt: PROMPT }));
    },
    ready: bankLoaded,
    steps: async (p) => {
      await p.getByRole('button', { name: 'Prompt for another AI' }).click();
      await expect(p.getByRole('textbox', { name: 'Prompt for another AI' })).toHaveValue(PROMPT);
    },
  },
  {
    name: 'wizard-custom-prompt',
    path: '/admin/ai/new',
    mock: (p) => ai.mockApi(p),
    ready: (p) => expect(p.getByRole('heading', { name: 'Add a key to start' })).toBeVisible(),
    steps: customPromptNote,
  },

  // Contests.
  { name: 'contests-list', path: '/admin/contests', mock: organiser, ready: contestsLoaded },
  { name: 'contests-archived', path: '/admin/contests', mock: organiser, ready: contestsLoaded, steps: archivedContests },
  { name: 'contests-restore-confirm', path: '/admin/contests', mock: organiser, ready: contestsLoaded, steps: (p) => contestsRestore(p, false) },
  { name: 'contests-restore-slug', path: '/admin/contests', mock: organiser, ready: contestsLoaded, steps: (p) => contestsRestore(p, true) },
  { name: 'contest-editor-restore-confirm', path: '/admin/contests/4', mock: organiser, ready: editorLoaded, steps: (p) => editorRestore(p, false) },
  { name: 'contest-editor-restore-slug', path: '/admin/contests/4', mock: organiser, ready: editorLoaded, steps: (p) => editorRestore(p, true) },
  {
    name: 'contest-editor-contact',
    path: '/admin/contests/4',
    mock: organiser,
    ready: editorLoaded,
    steps: async (p) => {
      await p.locator('#advanced summary').click();
      await expect(p.getByLabel('Contact email for this contest')).toBeVisible();
    },
  },
  {
    name: 'contest-page-contact',
    path: '/contests/week-6',
    mock: contestsAs({ phase: 'ready', contestContact: 'week6@college.edu' }),
    ready: (p) => expect(p.getByText('Questions about this contest?')).toBeVisible(),
  },
  {
    name: 'contest-page-contact-long',
    path: '/contests/week-6',
    mock: contestsAs({ phase: 'ready', contestContact: `${'contest-organisers-'.repeat(4)}ab@college.edu`.slice(0, 100) }),
    ready: (p) => expect(p.getByText('Questions about this contest?')).toBeVisible(),
  },

  // Theme and colours (v2.0.1 stage E): the open picker, a preset, the custom builder, an editor scheme.
  {
    name: 'theme-picker',
    path: '/',
    mock: owner,
    ready: ownerReady,
    steps: async (p) => {
      await themeDialog(p);
    },
  },
  {
    name: 'theme-picker-preset',
    path: '/',
    mock: owner,
    ready: ownerReady,
    steps: async (p) => {
      const d = await themeDialog(p);
      await d.getByRole('radio', { name: /^Nordic slate/ }).check();
      await expect(d).toContainText('Current theme: Nordic slate');
    },
  },
  {
    name: 'theme-picker-custom',
    path: '/',
    mock: owner,
    ready: ownerReady,
    steps: async (p) => {
      const d = await themeDialog(p);
      await d.getByRole('radio', { name: /^Custom/ }).check();
      await d.getByLabel('Background', { exact: true }).fill('#1c2430');
      await d.getByLabel('Accent', { exact: true }).fill('#5cc489');
      await expect(d.getByText('Adjusted so text stays readable')).toBeVisible();
      await d.getByLabel('Accent', { exact: true }).scrollIntoViewIfNeeded();
    },
  },
  {
    name: 'theme-picker-editor-paper',
    path: '/',
    mock: owner,
    ready: ownerReady,
    steps: async (p) => {
      const d = await themeDialog(p);
      await d.getByRole('radio', { name: /^Paper/ }).check();
      await d.getByRole('radio', { name: /^Paper/ }).scrollIntoViewIfNeeded();
    },
  },

  // Footer, sign-in and the main pages.
  { name: 'footer', path: '/', mock: (p) => screens.mockApi(p, 'student'), ready: (p) => expect(p.locator('footer')).toContainText('Problems with the site?') },
  {
    name: 'signin',
    path: '/signin',
    mock: (p) => screens.mockApi(p, 'signed-out'),
    ready: (p) => expect(p.getByText('Sign in with the Google account of the email the organisers added.')).toBeVisible(),
  },
  // Their own ready checks wait for desktop-only content (the editor, the feed and the statement sit behind tabs under 1024 px), so they use the default.
  ...contests.SCREENS.map((s) => ({ name: s.name, path: s.path, mock: contestsAs(s.options), ready: TABBED.has(s.name) ? undefined : s.ready })),
  ...problems.SCREENS.filter((s) => s.name === 'problem-editor').map((s) => ({ name: s.name, path: s.path, mock: bank })),
  {
    name: 'workspace-try',
    path: '/admin/problems/1/try',
    mock: (p) => workspace.mockApi(p, { submissions: workspace.HISTORY }),
    ready: async (p) => {
      await expect(p.getByRole('heading', { level: 1 })).toHaveText('A plus B');
      await expect(codeEditor(p)).toHaveCount(1);
    },
  },
];
