// Role and name locators shared by the browser tests, so a redesign that
// moves markup does not break them (stage A6).
import type { Page } from '@playwright/test';

/**
 * The code editor: CodeMirror's textbox, named "Code for <problem>, <language>".
 * Hidden ones count too: under 1024 px the editor waits on the Code tab.
 */
export const codeEditor = (page: Page) => page.getByRole('textbox', { name: /^Code for /, includeHidden: true });

/** The newest contest message, shown above the problem. */
export const contestBanner = (page: Page) => page.getByTestId('contest-banner');

/** The contest editor's one save line. */
export const saveStatus = (page: Page) => page.getByRole('status').filter({ hasText: /^(Saving|Saved|Not saved\..*)$/ });

/** The rows of the console's submissions feed, without the header row. */
export const feedRows = (page: Page) => page.getByRole('region', { name: 'Submissions' }).getByRole('row').filter({ has: page.getByRole('rowheader') });

/** The stack of toasts in the corner. */
export const toasts = (page: Page) => page.getByRole('group', { name: 'Notifications' });
