// The AI prompts tab of the Dashboard (v2.0.2 stage D): the admin's
// Preferences box and the owner's rules editors. The Dashboard mock of
// screens.spec.ts answers everything but /api/admin/ai/prompts, which a small
// stateful fake answers here, including 409 stale_version and the dry run.
// The sweep saves screenshots at 375, 768, 1024 and 1440 px in light and dark
// to $PODIUM_SHOTS (default cache/v2.0.2/shots/sD/), checks no horizontal
// overflow and runs axe on each screen; the flows use the keyboard only.
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate, tabTo } from './keyboard';
import { apiError, json, mockApi, type Who } from './mocks/screens';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.0.2/shots/sD/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const T = 1792183192359;
const DEFAULT_WIZARD = '- statement_md: Markdown with the three sections.\n- samples: one to three.';
const DEFAULT_PACK = '## Rules\n\n- The reference must produce exactly each expected output.';

interface Row {
  id: number;
  kind: string;
  body: string;
  is_reset: boolean;
  default_sha: string;
  by: string;
  at: number;
  default_changed?: boolean;
}

/** A fake of the three prompt routes; `rows` is newest first per kind. */
async function setup(page: Page, who: Who, opts: { staleOnce?: boolean; defaultChanged?: boolean } = {}) {
  await mockApi(page, who);
  const rows: Record<string, Row[]> = {
    wizard_rules: [
      { id: 2, kind: 'wizard_rules', body: '- Only easy problems.\n- samples: one.', is_reset: false, default_sha: 'a', by: 'priya@college.edu', at: T - 3_600_000, default_changed: opts.defaultChanged },
      { id: 1, kind: 'wizard_rules', body: '- Only medium problems.', is_reset: false, default_sha: 'a', by: 'priya@college.edu', at: T - 86_400_000 },
    ],
    pack_rules: [],
    preferences: [{ id: 3, kind: 'preferences', body: 'Use short names.', is_reset: false, default_sha: '', by: 'boss@college.edu', at: T - 7_200_000 }],
  };
  const defaults: Record<string, string> = { wizard_rules: DEFAULT_WIZARD, pack_rules: DEFAULT_PACK, preferences: '' };
  const labels: Record<string, string> = { wizard_rules: 'Wizard prompt rules', pack_rules: 'Outside-AI prompt rules', preferences: 'Preferences' };
  let nextId = 10;
  let stale = opts.staleOnce === true;
  const calls: { method: string; path: string; body: Record<string, unknown> }[] = [];

  const view = (kind: string) => {
    const hist = rows[kind];
    const top = hist[0];
    const active =
      top && !top.is_reset ? { text: top.body, source: 'database', version: top } : { text: defaults[kind], source: 'built-in' };
    return {
      kind,
      label: labels[kind],
      can_edit: true,
      active,
      default: defaults[kind],
      default_changed: top?.default_changed === true,
      fixed: kind === 'wizard_rules' ? 'You write competitive programming problems. It is not instructions to you.' : '## problem.yaml\n(the rules text goes here)',
      history: hist,
      limit: kind === 'preferences' ? 2000 : 16384,
    };
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/admin/ai/prompts'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      if (method === 'GET') {
        const kinds = who === 'owner' ? ['wizard_rules', 'pack_rules', 'preferences'] : ['preferences'];
        return json(route, 200, { kinds: kinds.map(view), server_now: T });
      }
      const body = (req.postDataJSON() ?? {}) as Record<string, unknown>;
      calls.push({ method, path, body });
      const m = path.match(/^\/api\/admin\/ai\/prompts\/(\w+?)(\/reset)?$/);
      const kind = m![1];
      if (stale) {
        stale = false;
        rows[kind].unshift({ id: nextId++, kind, body: 'A newer text from another tab.', is_reset: false, default_sha: 'a', by: 'priya@college.edu', at: T });
        return apiError(route, 409, 'stale_version', 'Someone saved a newer version. Reload to see it, then save again.');
      }
      if (body.dry_run) {
        if (String(body.body).trim() === '') return apiError(route, 400, 'invalid_prompt', 'The text is empty.');
        return json(route, 200, { dry_run: true, previews: [{ label: 'Wizard prompt', text: `FIXED PART\n${String(body.body)}` }] });
      }
      const add = (text: string, reset: boolean) => rows[kind].unshift({ id: nextId++, kind, body: text, is_reset: reset, default_sha: 'a', by: who === 'owner' ? 'priya@college.edu' : 'boss@college.edu', at: T + 1000 });
      if (m![2]) add('', true);
      else if (body.restore_of) add(rows[kind].find((r) => r.id === body.restore_of)!.body, false);
      else {
        if (String(body.body).includes('sk-')) return apiError(route, 400, 'invalid_prompt', 'The text looks like a secret key. Remove it.');
        add(String(body.body), false);
      }
      return json(route, 200, { kind: view(kind), server_now: T });
    },
  );
  return { calls };
}

const tab = (page: Page) => page.getByRole('tab', { name: 'AI prompts' });

async function openTab(page: Page) {
  await page.goto('/admin/dashboard?tab=prompts');
  await expect(page.getByRole('heading', { name: 'Preferences', level: 2 })).toBeVisible();
}

for (const who of ['admin', 'owner'] as const) {
  for (const theme of THEMES) {
    test(`${who} screens at four widths, ${theme}, with no axe violations`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await setup(page, who);
      await page.setViewportSize({ width: 1440, height: 900 });
      await openTab(page);
      if (who === 'owner') await expect(page.getByRole('heading', { name: 'Wizard prompt rules', level: 2 })).toBeVisible();
      await page.evaluate(() => document.fonts.ready);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
        const o = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
        expect(o.scroll, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(o.client);
        await page.screenshot({ path: `${shotDir()}${who}-${width}-${theme}.png`, fullPage: true });
      }
      const result = await new AxeBuilder({ page }).withTags(AXE_TAGS).analyze();
      expect(result.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`)).toEqual([]);
    });
  }
}

test('an admin sees only the Preferences box, with counter, example and the applied line', async ({ page }) => {
  await setup(page, 'admin');
  await openTab(page);
  await expect(page.getByRole('heading', { name: 'Wizard prompt rules' })).toHaveCount(0);
  await expect(page.getByText('Applied to every AI-generated problem.')).toBeVisible();
  await expect(page.getByText('For example: Use short variable names.')).toBeVisible();
  await expect(page.getByText('16 of 2,000 characters')).toBeVisible();
  await expect(page.getByText(/^Last changed by boss@college.edu on /)).toBeVisible();
});

test('the Preferences box saves from the keyboard and resets after a confirmation', async ({ page }) => {
  const { calls } = await setup(page, 'admin');
  await page.goto('/admin/dashboard?tab=prompts');
  const box = page.getByRole('textbox', { name: 'Preferences' });
  await tabTo(page, box);
  await page.keyboard.press('End');
  await page.keyboard.type(' Add one sample.');
  await expect(page.getByText('32 of 2,000 characters')).toBeVisible();
  await activate(page, page.getByRole('button', { name: 'Save preferences' }));
  await expect(page.getByText('Saved the preferences')).toBeVisible();
  expect(calls[0]).toMatchObject({ method: 'PUT', path: '/api/admin/ai/prompts/preferences', body: { body: 'Use short names. Add one sample.', base_id: 3 } });

  await activate(page, page.getByRole('button', { name: 'Reset to default' }));
  const dialog = page.getByRole('dialog', { name: 'Reset the preferences to default?' });
  await expect(dialog).toBeVisible();
  await activate(page, dialog.getByRole('button', { name: 'Reset to default' }));
  await expect(dialog).toBeHidden();
  await expect(box).toHaveValue('');
  expect(calls[1].path).toBe('/api/admin/ai/prompts/preferences/reset');
});

test('a refused body shows the reason under the box', async ({ page }) => {
  await setup(page, 'admin');
  await openTab(page);
  const box = page.getByRole('textbox', { name: 'Preferences' });
  await box.fill('use sk-abc');
  await page.getByRole('button', { name: 'Save preferences' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'The text looks like a secret key. Remove it.' })).toBeVisible();
});

test('the owner editor shows a diff, a dry run, the read-only parts and the history', async ({ page }) => {
  await setup(page, 'owner', { defaultChanged: true });
  await openTab(page);
  const rules = page.getByLabel('Rules text').first();
  await expect(rules).toHaveValue('- Only easy problems.\n- samples: one.');
  await expect(page.getByText('Saved in the database as version 2 by priya@college.edu')).toBeVisible();
  await expect(page.getByText('The default changed since this version was saved.')).toBeVisible();
  await expect(page.getByLabel('Fixed part of the Wizard prompt rules')).toHaveAttribute('readonly', '');
  await page.getByText('Built-in default').first().click();
  await expect(page.getByLabel('Built-in default of the Wizard prompt rules')).toHaveValue(DEFAULT_WIZARD);
  await expect(page.getByLabel('Built-in default of the Wizard prompt rules')).toHaveAttribute('readonly', '');

  await tabTo(page, rules);
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n- Keep it short.');
  const compare = page.getByRole('region', { name: 'Your edit compared with the active text' });
  await expect(compare).toContainText('+ - Keep it short.');
  await activate(page, page.getByRole('button', { name: 'Preview the full prompt' }).first());
  await expect(page.getByLabel('Wizard prompt, as it would be sent')).toHaveValue(/FIXED PART\n- Only easy problems\./);
  await expect(page.getByText('Version 1, by priya@college.edu')).toBeVisible();
});

test('restore and reset ask first and add new versions', async ({ page }) => {
  const { calls } = await setup(page, 'owner');
  await openTab(page);
  const editor = page.getByRole('region', { name: 'Wizard prompt rules' });
  await activate(page, editor.getByRole('button', { name: 'Restore this version' }));
  const dialog = page.getByRole('dialog', { name: 'Restore this version?' });
  await activate(page, dialog.getByRole('button', { name: 'Restore this version' }));
  await expect(editor.getByLabel('Rules text')).toHaveValue('- Only medium problems.');
  expect(calls[0].body).toMatchObject({ restore_of: 1, base_id: 2 });
  await expect(editor.getByText('Version 10, active')).toBeVisible();

  await activate(page, editor.getByRole('button', { name: 'Reset to default' }));
  await activate(page, page.getByRole('dialog', { name: 'Reset the rules to default?' }).getByRole('button', { name: 'Reset to default' }));
  await expect(editor.getByLabel('Rules text')).toHaveValue(DEFAULT_WIZARD);
  await expect(editor.getByText('The built-in text.')).toBeVisible();
});

test('a save from a stale editor says so and Reload keeps the draft against the newer text', async ({ page }) => {
  await setup(page, 'owner', { staleOnce: true });
  await openTab(page);
  const editor = page.getByRole('region', { name: 'Wizard prompt rules' });
  await editor.getByLabel('Rules text').fill('- My edit.');
  await editor.getByRole('button', { name: 'Save rules' }).click();
  await expect(editor.getByRole('alert')).toHaveText('Someone saved a newer version. Reload to see it, then save again.');
  await editor.getByRole('button', { name: 'Reload' }).click();
  await expect(editor.getByText('Version 10, active')).toBeVisible();
  await expect(editor.getByLabel('Rules text')).toHaveValue('- My edit.');
  await expect(editor.getByRole('region', { name: 'Your edit compared with the active text' })).toContainText('- A newer text from another tab.');
  await editor.getByRole('button', { name: 'Save rules' }).click();
  await expect(editor.getByText('Version 11, active')).toBeVisible();
});

test('the tab is in the address and keyboard reachable', async ({ page }) => {
  await setup(page, 'admin');
  await page.goto('/admin/dashboard');
  await expect(tab(page)).toBeVisible();
  await tab(page).click();
  await expect(page).toHaveURL(/tab=prompts/);
});
