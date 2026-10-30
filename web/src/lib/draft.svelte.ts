/**
 * The code in the workspace editor and where it is kept: the
 * language and source, the copy on the server and the mirror on this
 * device (lib/drafts.ts), the editor settings, and the language memory.
 *One per Workspace; the component holds the timers and calls
 * save() every 10 s, on blur and when the page hides.
 *
 * - On load the newer of the server's draft and the device mirror wins;
 *   an empty draft starts from the language's starter (lib/starters.ts).
 * - A language switch keeps the code typed so far under the old language
 *   on this device and brings back the new language's own code, or its
 *   starter while the code is still a starter; over typed code it waits
 *   in `pendingLanguage` for the confirm, and Cancel keeps the code.
 * - Reset puts the starter back and saves at once, so the next timed save
 *   or a reload cannot bring the old code back.
 */
import { ApiError, isSignedOut, sendJSON } from './api';
import { serverClock } from './clock';
import {
  DEFAULT_FONT_SIZE,
  draftStorage,
  languageMirrorKey,
  mayMirror,
  mirrorKey,
  pickDraft,
  readEditorPrefs,
  readMirror,
  writeEditorPrefs,
  writeMirror,
} from './drafts';
import { isStarterOrEmpty, starter } from './starters';
import type { Language, Workspace } from './workspace';

export type SaveState = 'saved' | 'saving' | 'local' | 'failed';

/** The editor settings Settings changes. */
export interface EditorSettingsValue {
  fontSize: number;
  tabSize: 2 | 4;
  wrap: boolean;
  tabMovesFocus: boolean;
}

/** What the draft needs of the editor component. */
export interface EditorLike {
  setDoc(doc: string): void;
  focus(): void;
}

export class Draft {
  language = $state<Language>('cpp17');
  source = $state('');
  /** The code the editor starts with. */
  initialDoc = $state('');
  saveState = $state<SaveState>('saved');
  fontSize = $state(DEFAULT_FONT_SIZE);
  tabMovesFocus = $state(false);
  tabSize = $state<2 | 4>(4);
  wrap = $state(false);
  cursor = $state({ line: 1, col: 1 });
  /** A language chosen over typed code, waiting for the confirm. */
  pendingLanguage = $state<Language | null>(null);
  editor: EditorLike | undefined;

  private loaded = false;
  /** The problem's own starters, by language (v4 E5). */
  private starters: Partial<Record<string, string>> = {};
  private savedLanguage = '';
  private savedSource = '';
  private mirrorTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly storage = draftStorage();

  /** `base` is the workspace's API base; the mirror belongs to `owner` (lib/drafts.ts). */
  constructor(
    private readonly base: string,
    private readonly owner: number,
    narrow: boolean,
  ) {
    const prefs = readEditorPrefs(this.storage, owner, narrow);
    this.language = prefs.language ?? 'cpp17';
    this.fontSize = prefs.fontSize;
    this.tabMovesFocus = prefs.tabMovesFocus;
    this.tabSize = prefs.tabSize;
    this.wrap = prefs.wrap;
  }

  /** True when the code on screen differs from the server's copy. */
  get unsaved(): boolean {
    return this.source !== this.savedSource || this.language !== this.savedLanguage;
  }

  /** Takes the loaded workspace's draft, or the device mirror when it is newer, or the starter. */
  restore(data: Workspace): void {
    this.starters = data.starters ?? {};
    const allowed = (data.languages ?? []).map((l) => l.id);
    if (allowed.length > 0 && !allowed.includes(this.language)) this.language = allowed[0];
    const picked = pickDraft(data.draft, readMirror(this.storage, mirrorKey(this.owner, this.base), this.owner));
    if (picked.draft && (allowed.length === 0 || allowed.includes(picked.draft.language))) {
      this.language = picked.draft.language;
      this.source = picked.draft.source;
    } else if (picked.draft) {
      this.source = picked.draft.source;
    } else {
      this.source = starter(this.language, this.starters);
    }
    this.initialDoc = this.source;
    this.loaded = true;
    if (picked.origin === 'server' && data.draft) {
      this.savedLanguage = data.draft.language;
      this.savedSource = data.draft.source;
      this.saveState = 'saved';
    } else {
      // The local copy is newer, or there is nothing on the server yet.
      this.savedLanguage = '';
      this.savedSource = '';
      this.saveState = picked.origin === 'local' ? 'local' : 'saved';
      if (picked.origin === 'local') void this.save();
    }
  }

  /** The editor's text changed. */
  change(next: string): void {
    this.source = next;
    this.queueMirror();
  }

  /** Saves to the server now when the code changed since the last save. */
  async save(): Promise<void> {
    if (!this.loaded || !this.unsaved) return;
    const body = { language: this.language, source: this.source };
    this.mirror({ ...body, updated_at: serverClock.serverNow() });
    this.saveState = 'saving';
    try {
      await sendJSON<void>('PUT', `${this.base}/draft`, body);
      this.savedLanguage = body.language;
      this.savedSource = body.source;
      this.saveState = this.unsaved ? 'local' : 'saved';
    } catch (e) {
      if (isSignedOut(e)) return;
      // Saved too soon after the last save: the copy here is kept and the next save sends it.
      this.saveState = e instanceof ApiError && e.status === 429 ? 'local' : 'failed';
    }
  }

  /** The last chance to save when the page goes away; not awaited. */
  saveOnLeave(): void {
    if (!this.unsaved || !mayMirror(this.owner)) return;
    const body = { language: this.language, source: this.source };
    this.mirror({ ...body, updated_at: serverClock.serverNow() });
    try {
      void fetch(`${this.base}/draft`, {
        method: 'PUT',
        keepalive: true,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(body),
      }).catch(() => undefined);
    } catch {
      // The local mirror still has it.
    }
  }

  /** A language picked in the select. */
  choose(next: Language): void {
    if (next === this.language) return;
    this.keep(this.language, this.source);
    const kept = readMirror(this.storage, languageMirrorKey(this.owner, this.base, next), this.owner);
    if (kept && !isStarterOrEmpty(kept.source, this.starters)) this.switchTo(next, kept.source);
    else if (isStarterOrEmpty(this.source, this.starters)) this.switchTo(next, starter(next, this.starters));
    else this.pendingLanguage = next;
  }

  /** Ends the confirm: `useStarter` loads the new language's starter, otherwise the code stays. */
  confirmLanguage(useStarter: boolean): void {
    const next = this.pendingLanguage;
    if (next) this.switchTo(next, useStarter ? starter(next, this.starters) : null);
  }

  /** Reset: the starter replaces the code and is saved at once. */
  async reset(): Promise<void> {
    const code = starter(this.language, this.starters);
    this.source = code;
    this.editor?.setDoc(code);
    await this.save();
  }

  /** Use this code: a past submission's code (and its language) replaces the editor's and is saved at once. */
  async useCode(language: Language, code: string): Promise<void> {
    this.keep(this.language, this.source);
    this.switchTo(language, code);
    await this.save();
  }

  settings(next: Partial<EditorSettingsValue>): void {
    if (next.fontSize !== undefined) this.fontSize = next.fontSize || DEFAULT_FONT_SIZE;
    if (next.tabSize !== undefined) this.tabSize = next.tabSize;
    if (next.wrap !== undefined) this.wrap = next.wrap;
    if (next.tabMovesFocus !== undefined) this.tabMovesFocus = next.tabMovesFocus;
    this.savePrefs();
  }

  dispose(): void {
    clearTimeout(this.mirrorTimer);
  }

  private switchTo(next: Language, code: string | null): void {
    this.pendingLanguage = null;
    this.language = next;
    this.savePrefs();
    if (code !== null) {
      this.source = code;
      this.editor?.setDoc(code);
    }
    this.queueMirror();
  }

  private savePrefs(): void {
    const { fontSize, tabMovesFocus, tabSize, wrap, language } = this;
    writeEditorPrefs(this.storage, { fontSize, tabMovesFocus, tabSize, wrap, language }, this.owner);
  }

  private queueMirror(): void {
    if (this.saveState === 'saved' && this.unsaved) this.saveState = 'local';
    clearTimeout(this.mirrorTimer);
    this.mirrorTimer = setTimeout(() => this.mirror({ language: this.language, source: this.source, updated_at: serverClock.serverNow() }), 400);
  }

  private keep(language: Language, source: string): void {
    if (mayMirror(this.owner)) writeMirror(this.storage, languageMirrorKey(this.owner, this.base, language), { language, source, updated_at: serverClock.serverNow() }, this.owner);
  }

  /**
   * Mirrors the draft on this device, only while its owner is still the
   * signed-in user: at sign-out the shell unmounts the workspace after the
   * mirrors were removed, and nothing may be written back for the next person.
   */
  private mirror(draft: { language: Language; source: string; updated_at: number }): void {
    if (!mayMirror(this.owner)) return;
    writeMirror(this.storage, mirrorKey(this.owner, this.base), draft, this.owner);
    writeMirror(this.storage, languageMirrorKey(this.owner, this.base, draft.language), draft, this.owner);
  }
}
