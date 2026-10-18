/**
 * Drafts: the code in the editor, kept in two places so a crash, a closed
 * lid or a dropped connection loses nothing.
 *
 * The server copy is saved every 10 s and when the editor loses focus. The
 * local copy is a mirror in localStorage, written on every pause in typing.
 * On load, pickDraft() takes whichever is newer; both carry server time
 * (the browser's clock corrected by the offset from lib/clock.ts), so a
 * laptop with a wrong clock still compares fairly. Storage can throw
 * (private windows, blocked site data), so every access is guarded and the
 * editor works without it.
 *
 * The editor's own settings (font size, Tab behaviour, last language) live
 * here too, under their own keys.
 */
import type { Draft, Language } from './workspace';

/** The part of Storage the mirror uses. */
export type DraftStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** localStorage, or null when the browser refuses it. */
export function draftStorage(): DraftStorage | null {
  try {
    if (typeof localStorage === 'undefined') return null;
    return localStorage;
  } catch {
    return null;
  }
}

const LANGS: readonly string[] = ['cpp17', 'java21', 'python3', 'node22'];

function isDraft(v: unknown): v is Draft {
  if (typeof v !== 'object' || v === null) return false;
  const d = v as Record<string, unknown>;
  return typeof d.language === 'string' && LANGS.includes(d.language) && typeof d.source === 'string' && typeof d.updated_at === 'number';
}

/** The storage key for a workspace's mirror, from its API base. */
export function mirrorKey(base: string): string {
  return `podium-draft:${base}`;
}

/** Reads the local mirror, or null when there is none or it is unreadable. */
export function readMirror(storage: DraftStorage | null, key: string): Draft | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(key);
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    return isDraft(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/** Writes the local mirror. Returns false when storage refused it. */
export function writeMirror(storage: DraftStorage | null, key: string, draft: Draft): boolean {
  if (!storage) return false;
  try {
    storage.setItem(key, JSON.stringify(draft));
    return true;
  } catch {
    return false;
  }
}

/** Where the draft on screen came from. */
export type DraftOrigin = 'server' | 'local' | 'none';

/**
 * Chooses between the server's draft and the local mirror: the newer one
 * wins, and the server wins a tie or when the two hold the same code.
 */
export function pickDraft(server: Draft | null, local: Draft | null): { draft: Draft | null; origin: DraftOrigin } {
  if (!server && !local) return { draft: null, origin: 'none' };
  if (!local) return { draft: server, origin: 'server' };
  if (!server) return { draft: local, origin: 'local' };
  if (local.source === server.source && local.language === server.language) return { draft: server, origin: 'server' };
  return local.updated_at > server.updated_at ? { draft: local, origin: 'local' } : { draft: server, origin: 'server' };
}

/** Font sizes the editor offers, in px. */
export const FONT_SIZES = [12, 13, 14, 15, 16, 18, 20, 22, 24] as const;

/** The editor's default font size (design.md section 4). */
export const DEFAULT_FONT_SIZE = 14;

const FONT_KEY = 'podium-editor-font';
const TAB_KEY = 'podium-editor-tab-focus';
const LANG_KEY = 'podium-editor-language';

function read(storage: DraftStorage | null, key: string): string | null {
  if (!storage) return null;
  try {
    return storage.getItem(key);
  } catch {
    return null;
  }
}

function write(storage: DraftStorage | null, key: string, value: string): void {
  if (!storage) return;
  try {
    storage.setItem(key, value);
  } catch {
    // The setting lasts for this page only.
  }
}

/** The editor settings remembered on this device. */
export interface EditorPrefs {
  fontSize: number;
  tabMovesFocus: boolean;
  language: Language;
}

/** Reads the remembered editor settings, falling back to the defaults. */
export function readEditorPrefs(storage: DraftStorage | null): EditorPrefs {
  const size = Number(read(storage, FONT_KEY));
  const lang = read(storage, LANG_KEY);
  return {
    fontSize: (FONT_SIZES as readonly number[]).includes(size) ? size : DEFAULT_FONT_SIZE,
    tabMovesFocus: read(storage, TAB_KEY) === '1',
    language: lang !== null && LANGS.includes(lang) ? (lang as Language) : 'cpp17',
  };
}

/** Remembers the editor settings on this device. */
export function writeEditorPrefs(storage: DraftStorage | null, prefs: EditorPrefs): void {
  write(storage, FONT_KEY, String(prefs.fontSize));
  write(storage, TAB_KEY, prefs.tabMovesFocus ? '1' : '0');
  write(storage, LANG_KEY, prefs.language);
}
