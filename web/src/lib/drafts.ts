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
 * The mirror belongs to one account: its key and its value both carry the
 * user id, an entry written for someone else is ignored, and every mirror
 * on the device is removed at sign-out and whenever a different account
 * signs in (forgetDrafts(), noteSignedInUser()). On a shared lab machine
 * the next student never sees, or saves as their own, the last student's
 * code.
 *
 * The editor's own settings (font size, tab size, word wrap, Tab
 * behaviour) and the size of the workspace's result panel (P5 F7.5) live
 * here too, under their own keys: they belong to this device. The last
 * language belongs to the account, so its key carries the user id.
 */
import type { Draft, Language } from './workspace';

/** The part of Storage the mirror uses; key() and length let forgetDrafts() list it. */
export type DraftStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> & Partial<Pick<Storage, 'key' | 'length'>>;

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

/** Every mirror key starts with this. */
export const MIRROR_PREFIX = 'podium-draft:';

/** Remembers which account last signed in on this device. */
const LAST_USER_KEY = 'podium-last-user';

/** The storage key for one user's mirror of a workspace, from its API base. */
export function mirrorKey(owner: number, base: string): string {
  return `${MIRROR_PREFIX}${owner}:${base}`;
}

/**
 * Reads one user's local mirror, or null when there is none, it is
 * unreadable, or it was written for another account.
 */
export function readMirror(storage: DraftStorage | null, key: string, owner: number): Draft | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(key);
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!isDraft(parsed) || (parsed as { owner?: unknown }).owner !== owner) return null;
    return { language: parsed.language, source: parsed.source, updated_at: parsed.updated_at };
  } catch {
    return null;
  }
}

/** Writes one user's local mirror. Returns false when storage refused it. */
export function writeMirror(storage: DraftStorage | null, key: string, draft: Draft, owner: number): boolean {
  if (!storage) return false;
  try {
    storage.setItem(key, JSON.stringify({ ...draft, owner }));
    return true;
  } catch {
    return false;
  }
}

// The account whose mirrors may be written, kept outside Svelte state on
// purpose: a component's teardown sees the state from before a change, so
// a workspace closing at sign-out would otherwise write its mirror back.
let activeOwner: number | null = null;
let signedOut = false;

/**
 * True when a mirror may be written for `owner` now: not after sign-out,
 * and only for the account that signed in last.
 */
export function mayMirror(owner: number): boolean {
  return !signedOut && (activeOwner === null || activeOwner === owner);
}

/** Removes every draft mirror on this device, whoever wrote it. */
export function forgetDrafts(storage: DraftStorage | null = draftStorage()): void {
  if (!storage || typeof storage.key !== 'function') return;
  try {
    const keys: string[] = [];
    for (let i = 0; i < (storage.length ?? 0); i++) {
      const k = storage.key(i);
      if (k !== null && k.startsWith(MIRROR_PREFIX)) keys.push(k);
    }
    for (const k of keys) storage.removeItem(k);
  } catch {
    // Nothing more can be done; mirrors of another owner are ignored on read anyway.
  }
}

/**
 * Records the account that just signed in; when it differs from the last
 * one seen on this device, every mirror is removed first.
 */
export function noteSignedInUser(id: number, storage: DraftStorage | null = draftStorage()): void {
  activeOwner = id;
  signedOut = false;
  if (!storage) return;
  try {
    if (storage.getItem(LAST_USER_KEY) !== String(id)) {
      forgetDrafts(storage);
      storage.setItem(LAST_USER_KEY, String(id));
    }
  } catch {
    // Storage refused; reads still check the owner.
  }
}

/** Removes every mirror and the last-user note, at sign-out. */
export function forgetDraftsAtSignOut(storage: DraftStorage | null = draftStorage()): void {
  signedOut = true;
  activeOwner = null;
  forgetDrafts(storage);
  try {
    storage?.removeItem(LAST_USER_KEY);
  } catch {
    // Ignored.
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
const TAB_SIZE_KEY = 'podium-editor-tab-size';
const WRAP_KEY = 'podium-editor-wrap';
const RESULT_KEY = 'podium-workspace-result';
/** The statement's share of the width beside the code in a contest, and whether it is hidden (B6). */
export const STATEMENT_KEY = 'podium-workspace-statement';

/** The result panel's share of the code column, in percent, while open (F7.5). */
export const RESULT_SHARE = { min: 20, max: 80, default: 40 } as const;

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

/** The editor settings remembered on this device; the language per account. */
export interface EditorPrefs {
  fontSize: number;
  tabMovesFocus: boolean;
  tabSize: 2 | 4;
  wrap: boolean;
  /** The last language this account chose, or null before the first choice. */
  language: Language | null;
}

/** The result panel under the editor from 1024 px: its share when open, and whether it is collapsed. */
export interface ResultPanelPrefs {
  share: number;
  collapsed: boolean;
}

/** Keeps a share inside the allowed range, rounded to a whole percent. */
export function clampShare(share: number): number {
  if (!Number.isFinite(share)) return RESULT_SHARE.default;
  return Math.round(Math.min(RESULT_SHARE.max, Math.max(RESULT_SHARE.min, share)));
}

/** Reads the result panel's size, or `fallback` open when nothing usable is stored. */
export function readResultPanel(storage: DraftStorage | null, fallback: number = RESULT_SHARE.default, key = RESULT_KEY): ResultPanelPrefs {
  const none = { share: clampShare(fallback), collapsed: false };
  const raw = read(storage, key);
  if (raw === null) return none;
  try {
    const v = JSON.parse(raw) as { share?: unknown; collapsed?: unknown };
    if (typeof v.share !== 'number') return none;
    return { share: clampShare(v.share), collapsed: v.collapsed === true };
  } catch {
    return none;
  }
}

/** Remembers the result panel's size on this device. */
export function writeResultPanel(storage: DraftStorage | null, prefs: ResultPanelPrefs, key = RESULT_KEY): void {
  write(storage, key, JSON.stringify({ share: clampShare(prefs.share), collapsed: prefs.collapsed }));
}

/**
 * Reads the remembered editor settings, falling back to the defaults. Word
 * wrap starts on when `narrow` (under 768 px) until it is set; the
 * language is the one `owner` chose last, on any problem or contest (P5).
 */
export function readEditorPrefs(storage: DraftStorage | null, owner: number, narrow = false): EditorPrefs {
  const size = Number(read(storage, FONT_KEY));
  const lang = read(storage, `${LANG_KEY}:${owner}`);
  const wrap = read(storage, WRAP_KEY);
  return {
    fontSize: (FONT_SIZES as readonly number[]).includes(size) ? size : DEFAULT_FONT_SIZE,
    tabMovesFocus: read(storage, TAB_KEY) === '1',
    tabSize: read(storage, TAB_SIZE_KEY) === '2' ? 2 : 4,
    wrap: wrap === null ? narrow : wrap === '1',
    language: lang !== null && LANGS.includes(lang) ? (lang as Language) : null,
  };
}

/** Remembers the editor settings on this device, and the language for `owner`. */
export function writeEditorPrefs(storage: DraftStorage | null, prefs: EditorPrefs, owner: number): void {
  write(storage, FONT_KEY, String(prefs.fontSize));
  write(storage, TAB_KEY, prefs.tabMovesFocus ? '1' : '0');
  write(storage, TAB_SIZE_KEY, String(prefs.tabSize));
  write(storage, WRAP_KEY, prefs.wrap ? '1' : '0');
  if (prefs.language) write(storage, `${LANG_KEY}:${owner}`, prefs.language);
}

/**
 * The key of the code kept for one language of a workspace, so switching
 * languages and back brings the code back. It shares the mirror prefix, so
 * sign-out removes it with the rest.
 */
export function languageMirrorKey(owner: number, base: string, language: Language): string {
  return `${mirrorKey(owner, base)}#${language}`;
}
