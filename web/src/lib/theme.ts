/**
 * Display preferences: the interface theme, the verdict colour set and the
 * editor colour scheme.
 *
 * A theme choice is "system" (no data-theme on <html>, so tokens.css follows
 * prefers-color-scheme), a built-in theme id below, a preset id
 * (theme-presets.ts) or "custom" (the person's own background, accent and
 * contrast). Built-in themes are token blocks in tokens.css. A preset or a
 * custom theme is derived by theme-engine.ts and applied as inline custom
 * properties on <html> plus data-theme, data-family (light or dark, which
 * picks the verdict colours and color-scheme) and, at the high contrast
 * level, data-contrast. The verdict set is "standard" or "signal", the
 * colour-blind safe set, applied as data-verdicts="signal". The editor
 * scheme (editor-colours.ts) is data-editor, absent for Night.
 *
 * The account holds the real preferences (GET /api/me, PUT
 * /api/me/preferences), so they follow the person to another machine.
 * localStorage keeps a copy only so public/theme-init.js can paint the first
 * frame right: the ids, the custom theme's inputs and, for a preset or a
 * custom theme, the derived variables (VARS_KEY). A test keeps that script's
 * lists in step with this file. Storage access can throw (private windows,
 * blocked site data), so every read and write is guarded and the app works
 * without it.
 */
import { DEFAULT_EDITOR_COLOURS, EDITOR_COLOURS_KEY, parseEditorColours } from './editor-colours';
import { ENGINE_TOKENS, deriveTheme, isHex, type DerivedTheme, type Family, type ThemeInput } from './theme-engine';
import { PRESETS } from './theme-presets';

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

/** The four hand-tuned themes in tokens.css: light, dark and high contrast in both. */
export const THEMES: readonly ThemeInfo[] = [
  { id: 'light', label: 'Light', group: 'Light', family: 'light' },
  { id: 'dark', label: 'Dark', group: 'Dark', family: 'dark' },
  { id: 'hc-light', label: 'High contrast light', group: 'Accessible', family: 'light' },
  { id: 'hc-dark', label: 'High contrast dark', group: 'Accessible', family: 'dark' },
];

/** "system", a theme id from THEMES, a preset id, or "custom". */
export type ThemeChoice = string;

export type VerdictSet = 'standard' | 'signal';

export interface Preferences {
  theme: ThemeChoice;
  verdicts: VerdictSet;
  /** An editor colour scheme id (editor-colours.ts). */
  editor: string;
  /** The custom theme's inputs, kept even while another theme is chosen. */
  custom: ThemeInput | null;
}

export const DEFAULT_PREFERENCES: Preferences = { theme: 'system', verdicts: 'standard', editor: DEFAULT_EDITOR_COLOURS, custom: null };

/** The theme id of the custom theme. */
export const CUSTOM_ID = 'custom';

/** localStorage keys, also read by public/theme-init.js. */
export const THEME_KEY = 'podium-theme';
export const VERDICTS_KEY = 'podium-verdicts';
/** The custom theme's three inputs as JSON. */
export const CUSTOM_KEY = 'podium-theme-custom';
/** The derived variables of a preset or custom theme, for the first frame. */
export const VARS_KEY = 'podium-theme-vars';

const BY_ID = new Map(THEMES.map((t) => [t.id, t]));
const PRESET_BY_ID = new Map(PRESETS.map((p) => [p.id, p]));

/** Turns any value into a valid theme choice, defaulting to "system". */
export function parseChoice(value: unknown): ThemeChoice {
  if (typeof value !== 'string') return 'system';
  return BY_ID.has(value) || PRESET_BY_ID.has(value) || value === CUSTOM_ID ? value : 'system';
}

/** Turns any value into a verdict set, defaulting to "standard". */
export function parseVerdicts(value: unknown): VerdictSet {
  return value === 'signal' ? 'signal' : 'standard';
}

/** Turns any value into custom-theme inputs (lowercase colours), or null when it is not three valid fields. */
export function parseCustom(value: unknown): ThemeInput | null {
  if (!value || typeof value !== 'object') return null;
  const v = value as Record<string, unknown>;
  if (!isHex(v.background) || !isHex(v.accent) || (v.contrast !== 'standard' && v.contrast !== 'high')) return null;
  return { background: v.background.toLowerCase(), accent: v.accent.toLowerCase(), contrast: v.contrast };
}

/** Sentence-case name of a choice for the interface. */
export function choiceLabel(choice: ThemeChoice): string {
  if (choice === CUSTOM_ID) return 'Custom';
  return BY_ID.get(choice)?.label ?? PRESET_BY_ID.get(choice)?.label ?? 'System';
}

/** The themes in one picker group. */
export function themesIn(group: ThemeGroup): ThemeInfo[] {
  return THEMES.filter((t) => t.group === group);
}

/** How the interface looks for a choice: its family and, for a preset or custom theme, the derived tokens. */
export interface Resolved {
  family: Family;
  contrast: 'standard' | 'high';
  /** null for a built-in theme, whose tokens are in tokens.css. */
  derived: DerivedTheme | null;
}

const derivedCache = new Map<string, DerivedTheme>();

/** Derives a theme from inputs, remembering the last few results. */
export function derive(input: ThemeInput): DerivedTheme {
  const key = `${input.background}|${input.accent}|${input.contrast}`;
  let d = derivedCache.get(key);
  if (!d) {
    d = deriveTheme(input);
    if (derivedCache.size > 64) derivedCache.clear();
    derivedCache.set(key, d);
  }
  return d;
}

/** The interface for preferences, or null for "system" (the browser's setting decides). */
export function resolveInterface(p: Pick<Preferences, 'theme' | 'custom'>): Resolved | null {
  if (p.theme === 'system') return null;
  const builtin = BY_ID.get(p.theme);
  if (builtin) return { family: builtin.family, contrast: 'standard', derived: null };
  const input = p.theme === CUSTOM_ID ? p.custom : PRESET_BY_ID.get(p.theme)?.input;
  if (!input) return null;
  const derived = derive(input);
  return { family: derived.family, contrast: derived.contrast, derived };
}

/** The values arriving from GET /api/me. */
export interface AccountPreferences {
  theme?: string;
  verdicts?: string;
  editor_colours?: string;
  theme_custom?: unknown;
}

/**
 * Converts the account's stored values (theme "" means system) into
 * preferences. Unknown theme ids fall back to the system theme, and so does
 * "custom" with no valid inputs saved.
 */
export function fromAccount(a: AccountPreferences): Preferences {
  const custom = parseCustom(a.theme_custom);
  const theme = parseChoice(a.theme);
  return {
    theme: theme === CUSTOM_ID && !custom ? 'system' : theme,
    verdicts: parseVerdicts(a.verdicts),
    editor: parseEditorColours(a.editor_colours),
    custom,
  };
}

/** The body for PUT /api/me/preferences. */
export function toAccount(p: Preferences): { theme: string; verdicts: VerdictSet; editor_colours: string; theme_custom: ThemeInput | null } {
  return { theme: p.theme === 'system' ? '' : p.theme, verdicts: p.verdicts, editor_colours: p.editor, theme_custom: p.custom };
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
    let custom: ThemeInput | null = null;
    try {
      custom = parseCustom(JSON.parse(storage.getItem(CUSTOM_KEY) ?? 'null'));
    } catch {
      custom = null;
    }
    const theme = parseChoice(storage.getItem(THEME_KEY));
    return {
      theme: theme === CUSTOM_ID && !custom ? 'system' : theme,
      verdicts: parseVerdicts(storage.getItem(VERDICTS_KEY)),
      editor: parseEditorColours(storage.getItem(EDITOR_COLOURS_KEY)),
      custom,
    };
  } catch {
    return { ...DEFAULT_PREFERENCES };
  }
}

/** The JSON public/theme-init.js reads: the derived variables of a preset or custom theme. */
export function varsBlob(p: Pick<Preferences, 'theme' | 'custom'>): string | null {
  const r = resolveInterface(p);
  if (!r?.derived) return null;
  return JSON.stringify({ theme: p.theme, family: r.family, contrast: r.contrast, vars: r.derived.vars });
}

/**
 * Caches the preferences. Defaults clear their key, so a fresh visit
 * follows the system. Returns false when storage is unavailable.
 */
export function savePreferences(storage: ThemeStorage | null, p: Preferences): boolean {
  if (!storage) return false;
  const put = (key: string, value: string | null) => (value === null ? storage.removeItem(key) : storage.setItem(key, value));
  try {
    put(THEME_KEY, p.theme === 'system' ? null : p.theme);
    put(VERDICTS_KEY, p.verdicts === 'standard' ? null : p.verdicts);
    put(EDITOR_COLOURS_KEY, p.editor === DEFAULT_EDITOR_COLOURS ? null : p.editor);
    put(CUSTOM_KEY, p.custom ? JSON.stringify(p.custom) : null);
    put(VARS_KEY, varsBlob(p));
    return true;
  } catch {
    return false;
  }
}

/** Element operations applyPreferences needs, so tests can pass a fake. */
export interface ThemeRoot {
  setAttribute(name: string, value: string): void;
  removeAttribute(name: string): void;
  style: Pick<CSSStyleDeclaration, 'setProperty' | 'removeProperty'>;
}

/** Applies preferences to an element, normally document.documentElement. */
export function applyPreferences(root: ThemeRoot, p: Preferences): void {
  const r = resolveInterface(p);
  if (!r) {
    for (const a of ['data-theme', 'data-family', 'data-contrast']) root.removeAttribute(a);
  } else {
    root.setAttribute('data-theme', p.theme);
    root.setAttribute('data-family', r.family);
    if (r.contrast === 'high' && r.derived) root.setAttribute('data-contrast', 'high');
    else root.removeAttribute('data-contrast');
  }
  for (const name of ENGINE_TOKENS) {
    const value = r?.derived?.vars[name];
    if (value) root.style.setProperty(name, value);
    else root.style.removeProperty(name);
  }
  if (p.verdicts === 'signal') root.setAttribute('data-verdicts', 'signal');
  else root.removeAttribute('data-verdicts');
  if (p.editor === DEFAULT_EDITOR_COLOURS) root.removeAttribute('data-editor');
  else root.setAttribute('data-editor', p.editor);
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
