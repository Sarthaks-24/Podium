// Shortcut hints (v4 U6): Run and Submit name their keys in their title and
// aria-keyshortcuts, read from the shortcut table (lib/shortcuts.ts), the
// one source the Help dialog shows too.
import { expect, test } from '@playwright/test';
import { shortcutGroups } from '../src/lib/shortcuts';
import { mockApi, openWorkspace } from './mocks/workspace';

const row = (start: string) =>
  shortcutGroups({ mac: false, organiser: false })
    .flatMap((g) => g.items)
    .find((s) => s.action.startsWith(start))!;

test('Run and Submit name the keys the shortcut table gives them', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openWorkspace(page);
  const run = page.getByRole('button', { name: 'Run', exact: true });
  const submit = page.getByRole('button', { name: 'Submit', exact: true });
  await expect(run).toHaveAttribute('title', `Run on the examples (${row('Run on the examples').keys[0]})`);
  await expect(submit).toHaveAttribute('title', `Submit (${row('Submit').keys[0]})`);
  await expect(submit).toHaveAttribute('aria-keyshortcuts', 'Control+Enter');

  // The same keys are in Help.
  await page.keyboard.press('Shift+Slash');
  const help = page.getByRole('dialog');
  await expect(help.getByText(row('Submit').action, { exact: true })).toBeVisible();
});
