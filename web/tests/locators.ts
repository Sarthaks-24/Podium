// Role and name locators shared by the browser tests, so a redesign that
// moves markup does not break them (stage A6).
import type { Page } from '@playwright/test';

/**
 * The code editor: CodeMirror's textbox, named "Code for <problem>, <language>".
 * Hidden ones count too: under 1024 px the editor waits on the Code tab.
 */
export const codeEditor = (page: Page) => page.getByRole('textbox', { name: /^Code for /, includeHidden: true });

/** The stack of toasts in the corner. */
export const toasts = (page: Page) => page.getByRole('group', { name: 'Notifications' });
