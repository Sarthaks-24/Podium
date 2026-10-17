/**
 * Display preferences: the colour theme and the verdict colour set.
 *
 * A theme choice is "system" (no data-theme on <html>, so tokens.css follows
 * prefers-color-scheme) or one of the theme ids below, applied as
 * data-theme. The verdict set is "standard" or "signal", the colour-blind
 * safe set, applied as data-verdicts="signal" (decision D40).
 *
 * The account holds the real preferences (GET /api/me, PUT
 * /api/me/preferences; decision D42). localStorage keeps a copy under
 * THEME_KEY and VERDICTS_KEY only so public/theme-init.js can paint the
 * first frame right; a test keeps that script's id list in step with
 * THEMES. Storage access can throw (private windows, blocked site data), so
 * every read and write is guarded and the app works without it.
 */

/** A theme the picker offers, in the order it lists them. */
export interface ThemeInfo {
  id: string;
  label: string;
  group: ThemeGroup;
  /** Light or dark family: decides verdict colours and color-scheme. */
  family: 'light' | 'dark';
}

export type ThemeGroup = 'Light' | 'Dark' | 'Accessible';

/** Group order in the picker. */
export const GROUPS: readonly ThemeGroup[] = ['Light', 'Dark', 'Accessible'];

/** Every theme in tokens.css (decision D39). */
export const THEMES: readonly ThemeInfo[] = [
  { id: 'light', label: 'Light', group: 'Light', family: 'light' },
  { id: 'nordic-frost', label: 'Nordic frost', group: 'Light', family: 'light' },
  { id: 'soft-lavender', label: 'Soft lavender', group: 'Light', family: 'light' },
  { id: 'dark', label: 'Dark', group: 'Dark', family: 'dark' },
  { id: 'overcast', label: 'Overcast', group: 'Dark', family: 'dark' },
  { id: 'oled-black', label: 'OLED black', group: 'Dark', family: 'dark' },
  { id: 'nordic-slate', label: 'Nordic slate', group: 'Dark', family: 'dark' },
  { id: 'cosmic-violet', label: 'Cosmic violet', group: 'Dark', family: 'dark' },
  { id: 'hc-light', label: 'High contrast light', group: 'Accessible', family: 'light' },
  { id: 'hc-dark', label: 'High contrast dark', group: 'Accessible', family: 'dark' },
];

/** "system", or a theme id from THEMES. */
export type ThemeChoice = string;

export type VerdictSet = 'standard' | 'signal';

export interface Preferences {
  theme: ThemeChoice;
  verdicts: VerdictSet;
}

export const DEFAULT_PREFERENCES: Preferences = { theme: 'system', verdicts: 'standard' };

/** localStorage keys, also read by public/theme-init.js. */
export const THEME_KEY = 'podium-theme';
export const VERDICTS_KEY = 'podium-verdicts';

const BY_ID = new Map(THEMES.map((t) => [t.id, t]));

/** Turns any value into a valid theme choice, defaulting to "system". */
export function parseChoice(value: unknown): ThemeChoice {
  return typeof value === 'string' && BY_ID.has(value) ? value : 'system';
}

/** Turns any value into a verdict set, defaulting to "standard". */
export function parseVerdicts(value: unknown): VerdictSet {
  return value === 'signal' ? 'signal' : 'standard';
}

/** Sentence-case name of a choice for the interface. */
export function choiceLabel(choice: ThemeChoice): string {
  return BY_ID.get(choice)?.label ?? 'System';
}

/** The themes in one picker group. */
export function themesIn(group: ThemeGroup): ThemeInfo[] {
  return THEMES.filter((t) => t.group === group);
}

/**
 * Converts the account's stored values (theme "" means system) into
 * preferences. Unknown theme ids fall back to the system theme.
 */
export function fromAccount(theme: string | undefined, verdicts: string | undefined): Preferences {
  return { theme: parseChoice(theme), verdicts: parseVerdicts(verdicts) };
}

/** The body for PUT /api/me/preferences. */
export function toAccount(p: Preferences): { theme: string; verdicts: VerdictSet } {
  return { theme: p.theme === 'system' ? '' : p.theme, verdicts: p.verdicts };
}

/** The subset of Storage this module uses, so tests can pass a fake. */
export type ThemeStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** Returns window.localStorage, or null when it is missing or blocked. */
export function getStorage(): ThemeStorage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** Reads the cached preferences; any failure means the defaults. */
export function readPreferences(storage: ThemeStorage | null): Preferences {
  if (!storage) return { ...DEFAULT_PREFERENCES };
  try {
    return { theme: parseChoice(storage.getItem(THEME_KEY)), verdicts: parseVerdicts(storage.getItem(VERDICTS_KEY)) };
  } catch {
    return { ...DEFAULT_PREFERENCES };
  }
}

/**
 * Caches the preferences. Defaults clear their key, so a fresh visit
 * follows the system. Returns false when storage is unavailable.
 */
export function savePreferences(storage: ThemeStorage | null, p: Preferences): boolean {
  if (!storage) return false;
  try {
    if (p.theme === 'system') storage.removeItem(THEME_KEY);
    else storage.setItem(THEME_KEY, p.theme);
    if (p.verdicts === 'standard') storage.removeItem(VERDICTS_KEY);
    else storage.setItem(VERDICTS_KEY, p.verdicts);
    return true;
  } catch {
    return false;
  }
}

/** Element operations applyPreferences needs, so tests can pass a fake. */
export type ThemeRoot = Pick<Element, 'setAttribute' | 'removeAttribute'>;

/** Applies preferences to an element, normally document.documentElement. */
export function applyPreferences(root: ThemeRoot, p: Preferences): void {
  if (p.theme === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', p.theme);
  if (p.verdicts === 'signal') root.setAttribute('data-verdicts', 'signal');
  else root.removeAttribute('data-verdicts');
}

/**
 * Points the browser chrome colour at the theme's paper. With the system
 * theme the two media-specific metas in index.html are right already, so
 * their own values are restored.
 */
export function syncThemeColor(doc: Document, p: Preferences): void {
  const paper = getComputedStyle(doc.documentElement).getPropertyValue('--paper').trim();
  for (const meta of doc.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    meta.dataset.initial ??= meta.content;
    meta.content = p.theme === 'system' || paper === '' ? meta.dataset.initial : paper;
  }
}
