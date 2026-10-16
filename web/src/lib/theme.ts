/**
 * Theme choice: follow the system, or force light or dark.
 *
 * The choice is stored in localStorage under STORAGE_KEY and applied as a
 * data-theme attribute on <html>. "system" removes the attribute so the
 * prefers-color-scheme rules in tokens.css take over. public/theme-init.js
 * reads the same key before first paint; a test keeps the two in step.
 *
 * Storage access can throw (private windows, blocked site data), so every
 * read and write is guarded and the app works without it.
 */

export type ThemeChoice = 'system' | 'light' | 'dark';

/** localStorage key, also read by public/theme-init.js. */
export const STORAGE_KEY = 'podium-theme';

const ORDER: readonly ThemeChoice[] = ['system', 'light', 'dark'];

const LABELS: Record<ThemeChoice, string> = {
  system: 'System',
  light: 'Light',
  dark: 'Dark',
};

/** The subset of Storage this module uses, so tests can pass a fake. */
export type ThemeStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** Turns any stored value into a valid choice, defaulting to "system". */
export function parseChoice(value: unknown): ThemeChoice {
  return value === 'light' || value === 'dark' ? value : 'system';
}

/** The choice the theme button moves to next: System, Light, Dark, then back. */
export function nextChoice(current: ThemeChoice): ThemeChoice {
  return ORDER[(ORDER.indexOf(current) + 1) % ORDER.length];
}

/** Sentence-case name of a choice for the interface. */
export function choiceLabel(choice: ThemeChoice): string {
  return LABELS[choice];
}

/** Returns window.localStorage, or null when it is missing or blocked. */
export function getStorage(): ThemeStorage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** Reads the saved choice; any failure means "system". */
export function readChoice(storage: ThemeStorage | null): ThemeChoice {
  if (!storage) return 'system';
  try {
    return parseChoice(storage.getItem(STORAGE_KEY));
  } catch {
    return 'system';
  }
}

/**
 * Saves the choice. "system" clears the key so a fresh visit follows the
 * system again. Returns false when storage is unavailable.
 */
export function saveChoice(storage: ThemeStorage | null, choice: ThemeChoice): boolean {
  if (!storage) return false;
  try {
    if (choice === 'system') {
      storage.removeItem(STORAGE_KEY);
    } else {
      storage.setItem(STORAGE_KEY, choice);
    }
    return true;
  } catch {
    return false;
  }
}

/** Applies the choice to an element, normally document.documentElement. */
export function applyChoice(root: Pick<Element, 'setAttribute' | 'removeAttribute'>, choice: ThemeChoice): void {
  if (choice === 'system') {
    root.removeAttribute('data-theme');
  } else {
    root.setAttribute('data-theme', choice);
  }
}
