/**
 * The code editor: CodeMirror 6 set up for the workspace.
 *
 * Loaded with a dynamic import by components/CodeEditor.svelte only, so
 * CodeMirror stays out of the main bundle. Each language mode is its own
 * chunk, fetched when that language is first chosen (P5 F7.8): the editor
 * opens at once with plain text and the highlighting arrives with the
 * mode. A later switch that lands first wins; a mode that fails to load
 * leaves the code as plain text, which still edits and submits.
 *
 * - Colours come from CSS custom properties (the well and the --code-*
 *   tokens), so the editor follows every theme without being rebuilt.
 * - Font size is the --editor-font-size-used property on the host element
 *   (CodeEditor.svelte keeps it at 16 px or more on touch screens).
 * - Tab indents unless "Tab moves focus" is on. Either way, Escape then Tab
 *   leaves the editor (CodeMirror's tab-focus escape), so a keyboard user is
 *   never trapped.
 * - Mod-Enter and Mod-' call the caller's submit and run; they are bound
 *   ahead of the VS Code keymap, which would otherwise insert a line.
 * - Keys follow VS Code (`@replit/codemirror-vscode-keymap` plus the few it
 *   leaves out, below); the full table is lib/shortcuts.ts. Find and
 *   replace, auto-closed brackets, completion (words from the file for C++
 *   and Java), multiple cursors, folding, word wrap and tab size 2 or 4.
 * - Typed and pasted characters are reported from each transaction's
 *   userEvent ("input.type", "input.paste"), never with timing.
 * - `describedBy` names the element holding the editor's keyboard hint, so
 *   a screen reader hears how to leave the editor (X2).
 */
import { autocompletion, closeBrackets, closeBracketsKeymap, closeCompletion, completeAnyWord, currentCompletions } from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap, indentMore, indentWithTab, selectParentSyntax } from '@codemirror/commands';
import { bracketMatching, HighlightStyle, indentOnInput, indentUnit, syntaxHighlighting } from '@codemirror/language';
import {
  findNext,
  findPrevious,
  getSearchQuery,
  highlightSelectionMatches,
  openSearchPanel,
  search,
  SearchQuery,
  setSearchQuery,
} from '@codemirror/search';
import { Compartment, EditorSelection, EditorState, type Extension } from '@codemirror/state';
import {
  crosshairCursor,
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
  type Command,
  type KeyBinding,
} from '@codemirror/view';
import { tags as t } from '@lezer/highlight';
import { vscodeKeymap } from '@replit/codemirror-vscode-keymap';
import type { Language } from './workspace';

const MODES: Record<Language, () => Promise<Extension>> = {
  cpp17: () => import('@codemirror/lang-cpp').then((m) => m.cpp()),
  java21: () => import('@codemirror/lang-java').then((m) => m.java()),
  python3: () => import('@codemirror/lang-python').then((m) => m.python()),
  node22: () => import('@codemirror/lang-javascript').then((m) => m.javascript()),
};

const loaded = new Map<Language, Promise<Extension>>();

/** The language mode, fetched once per page; a failed fetch is tried again next time. */
export function loadMode(language: Language): Promise<Extension> {
  let p = loaded.get(language);
  if (!p) {
    p = MODES[language]();
    loaded.set(language, p);
    p.catch(() => loaded.delete(language));
  }
  return p;
}

const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword, t.definitionKeyword, t.modifier], color: 'var(--code-keyword)' },
  { tag: [t.string, t.special(t.string), t.character, t.regexp], color: 'var(--code-string)' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--code-number)' },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: 'var(--code-comment)', fontStyle: 'italic' },
  { tag: [t.typeName, t.className, t.namespace, t.standard(t.typeName)], color: 'var(--code-type)' },
  { tag: [t.function(t.variableName), t.function(t.propertyName), t.macroName], color: 'var(--code-function)' },
  { tag: [t.processingInstruction, t.meta, t.annotation], color: 'var(--code-meta)' },
  { tag: t.invalid, color: 'var(--code-invalid)' },
]);

const theme = EditorView.theme(
  {
    '&': {
      height: '100%',
      color: 'var(--well-ink)',
      backgroundColor: 'var(--well)',
      fontSize: 'var(--editor-font-size-used, var(--editor-font-size, var(--fs-0)))',
    },
    '&.cm-focused': { outline: 'none' },
    '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.55', overflow: 'auto' },
    '.cm-content': { caretColor: 'var(--well-ink)', padding: 'var(--space-2) 0' },
    '.cm-line': { padding: '0 var(--space-3) 0 var(--space-2)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--well-ink)', borderLeftWidth: 'calc(2 * var(--rule))' },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection':
      { backgroundColor: 'var(--code-selection)' },
    '.cm-gutters': { backgroundColor: 'var(--well)', color: 'var(--code-gutter)', border: 'none' },
    '.cm-lineNumbers .cm-gutterElement': { padding: '0 var(--space-2) 0 var(--space-3)', minWidth: '3ch' },
    '.cm-activeLine': { backgroundColor: 'var(--code-active-line)' },
    '.cm-activeLineGutter': { backgroundColor: 'var(--code-active-line)', color: 'var(--well-ink)' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--code-selection)', outline: 'var(--rule) solid var(--code-gutter)' },
    '.cm-specialChar': { color: 'var(--code-invalid)' },
    '.cm-searchMatch': { backgroundColor: 'var(--code-match)', outline: 'var(--rule) solid var(--code-match-edge)' },
    '.cm-searchMatch-selected, .cm-selectionMatch': { backgroundColor: 'var(--code-selection)' },
    '.cm-panels': { backgroundColor: 'var(--well)', color: 'var(--well-ink)' },
    '.cm-panels-top': { borderBottom: 'var(--rule) solid var(--well-edge)' },
    '.cm-panel.cm-search': { padding: 'var(--space-2) var(--space-3)', fontFamily: 'var(--font-sans)', fontSize: 'var(--fs-0)' },
    '.cm-panel.cm-search input, .cm-panel.cm-search button': { font: 'inherit', color: 'var(--well-ink)' },
    '.cm-textfield': { backgroundColor: 'var(--well)', border: 'var(--rule) solid var(--code-gutter)', borderRadius: 'var(--radius)' },
    '.cm-button': { backgroundImage: 'none', backgroundColor: 'var(--code-active-line)', border: 'var(--rule) solid var(--code-gutter)', borderRadius: 'var(--radius)' },
    '.cm-tooltip': { backgroundColor: 'var(--well)', color: 'var(--well-ink)', border: 'var(--rule) solid var(--code-gutter)' },
    '.cm-tooltip-autocomplete > ul': { fontFamily: 'var(--font-mono)' },
    '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'var(--code-selection)', color: 'var(--well-ink)' },
  },
  { dark: true },
);

/** What the workspace hears from the editor. */
export interface EditorCallbacks {
  onChange: (source: string) => void;
  onTyped: (chars: number) => void;
  onPaste: (chars: number) => void;
  onBlur: () => void;
  onSubmit: () => void;
  onRun: () => void;
  /** The main cursor moved: line and column, both from 1. */
  onCursor?: (line: number, col: number) => void;
}

export interface EditorOptions extends EditorCallbacks {
  parent: HTMLElement;
  doc: string;
  language: Language;
  label: string;
  /** The id of the element that describes the editor (its keyboard hint). */
  describedBy?: string;
  tabMovesFocus: boolean;
  tabSize?: 2 | 4;
  wrap?: boolean;
}

/** The handle the component keeps. */
export interface EditorHandle {
  setLanguage(language: Language, label: string): void;
  setTabMovesFocus(on: boolean): void;
  setTabSize(size: 2 | 4): void;
  setWrap(on: boolean): void;
  /** Replaces the whole document; not counted as typing. */
  setDoc(doc: string): void;
  getDoc(): string;
  /** Moves the cursor to a line (and column), scrolls it into view and focuses. */
  jumpTo(line: number, col: number | null): void;
  focus(): void;
  destroy(): void;
}

function inserted(tr: { changes: { iterChanges: (fn: (a: number, b: number, c: number, d: number, text: { length: number }) => void) => void } }): number {
  let n = 0;
  tr.changes.iterChanges((_fa, _ta, _fb, _tb, text) => {
    n += text.length;
  });
  return n;
}

// The VS Code keys the replit keymap leaves out (cache/v3/design/editor.md section 3).

/** Mod+Shift+Enter: a new line above each cursor, at the line's indent. */
export const insertLineAbove: Command = (view) => {
  if (view.state.readOnly) return false;
  const tr = view.state.changeByRange((r) => {
    const line = view.state.doc.lineAt(r.head);
    const indent = /^\s*/.exec(line.text)?.[0] ?? '';
    return { changes: { from: line.from, insert: indent + view.state.lineBreak }, range: EditorSelection.cursor(line.from + indent.length) };
  });
  view.dispatch(view.state.update(tr, { scrollIntoView: true, userEvent: 'input' }));
  return true;
};

/** Scrolls by a line or a page without moving the cursor. */
const scrollBy =
  (page: boolean, dir: 1 | -1): Command =>
  (view) => {
    view.scrollDOM.scrollTop += dir * (page ? view.scrollDOM.clientHeight : view.defaultLineHeight);
    return true;
  };

// Shift+Alt+Right grows the selection to the enclosing syntax; Shift+Alt+Left
// walks back through the selections it grew from, as VS Code does.
const grown = new WeakMap<EditorView, { stack: EditorSelection[]; last: EditorSelection }>();

const expandSelection: Command = (view) => {
  const before = view.state.selection;
  const prev = grown.get(view);
  const stack = prev && prev.last.eq(before) ? prev.stack : [];
  if (!selectParentSyntax(view)) return false;
  if (!view.state.selection.eq(before)) grown.set(view, { stack: [...stack, before], last: view.state.selection });
  return true;
};

const shrinkSelection: Command = (view) => {
  const prev = grown.get(view);
  if (!prev || prev.stack.length === 0 || !prev.last.eq(view.state.selection)) return false;
  const back = prev.stack[prev.stack.length - 1];
  grown.set(view, { stack: prev.stack.slice(0, -1), last: back });
  view.dispatch({ selection: back, scrollIntoView: true });
  return true;
};

/** Alt+C, Alt+W, Alt+R in the find panel: match case, whole word, regular expression. */
const toggleQuery =
  (flag: 'caseSensitive' | 'wholeWord' | 'regexp'): Command =>
  (view) => {
    const q = getSearchQuery(view.state);
    const next = { search: q.search, replace: q.replace, caseSensitive: q.caseSensitive, wholeWord: q.wholeWord, regexp: q.regexp, literal: q.literal };
    next[flag] = !next[flag];
    view.dispatch({ effects: setSearchQuery.of(new SearchQuery(next)) });
    return true;
  };

const extraKeys: KeyBinding[] = [
  { key: 'Mod-Shift-Enter', run: insertLineAbove, preventDefault: true },
  { key: 'Mod-h', run: openSearchPanel, scope: 'editor search-panel', preventDefault: true },
  { key: 'F3', run: findNext, shift: findPrevious, scope: 'editor search-panel', preventDefault: true },
  { key: 'Alt-c', run: toggleQuery('caseSensitive'), scope: 'search-panel' },
  { key: 'Alt-w', run: toggleQuery('wholeWord'), scope: 'search-panel' },
  { key: 'Alt-r', run: toggleQuery('regexp'), scope: 'search-panel' },
  { win: 'Ctrl-ArrowUp', linux: 'Ctrl-ArrowUp', run: scrollBy(false, -1) },
  { win: 'Ctrl-ArrowDown', linux: 'Ctrl-ArrowDown', run: scrollBy(false, 1) },
  { key: 'Alt-PageUp', run: scrollBy(true, -1), preventDefault: true },
  { key: 'Alt-PageDown', run: scrollBy(true, 1), preventDefault: true },
  { key: 'Shift-Alt-ArrowRight', run: expandSelection, preventDefault: true },
  { key: 'Shift-Alt-ArrowLeft', run: shrinkSelection, preventDefault: true },
];

// Escape closes the completion list only while one is shown. CodeMirror's
// own closeCompletion also takes Escape when a source answered with no
// options, which would swallow the Escape of "Escape, then Tab" and trap a
// keyboard user in the editor.
const closeShownCompletion: Command = (view) => currentCompletions(view.state).length > 0 && closeCompletion(view);

// Tab is bound by the tab compartment, so "Tab moves focus" can turn it off;
// Escape for completion is the guarded one above.
const vscodeKeys = vscodeKeymap.filter((b) => !(b.key === 'Tab' && b.run === indentMore) && b.run !== closeCompletion);

/** C++ and Java have no completion source of their own, so they complete words from the file. */
const completion = (language: Language) =>
  autocompletion({ defaultKeymap: false, ...(language === 'cpp17' || language === 'java21' ? { override: [completeAnyWord] } : {}) });

// The find and go-to-line panels' words, in sentence case (design.md section 8).
const phrases = EditorState.phrases.of({
  next: 'Next',
  previous: 'Previous',
  all: 'Select all',
  'match case': 'Match case',
  regexp: 'Regular expression',
  'by word': 'Whole word',
  replace: 'Replace',
  'replace all': 'Replace all',
  close: 'Close',
  go: 'Go',
});

const tabSizeExt = (size: 2 | 4) => [EditorState.tabSize.of(size), indentUnit.of(' '.repeat(size))];

/** Creates the editor inside `parent`. */
export function createEditor(o: EditorOptions): EditorHandle {
  const languageConf = new Compartment();
  const completionConf = new Compartment();
  const labelConf = new Compartment();
  const tabConf = new Compartment();
  const tabSizeConf = new Compartment();
  const wrapConf = new Compartment();

  // Podium's own keys win over VS Code's (Mod-Enter would insert a line there).
  const own = keymap.of([
    { key: 'Mod-Enter', preventDefault: true, run: () => (o.onSubmit(), true) },
    { key: "Mod-'", preventDefault: true, run: () => (o.onRun(), true) },
  ]);

  const tabKeys = (on: boolean) => (on ? [] : keymap.of([indentWithTab]));
  const labelAttrs = (label: string) =>
    EditorView.contentAttributes.of({
      'aria-label': label,
      'aria-multiline': 'true',
      // Browser translation must never rewrite code (final audit 2026-10-21).
      translate: 'no',
      ...(o.describedBy ? { 'aria-describedby': o.describedBy } : {}),
    });

  const view = new EditorView({
    parent: o.parent,
    state: EditorState.create({
      doc: o.doc,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        drawSelection(),
        EditorState.allowMultipleSelections.of(true),
        // VS Code: Alt+Click adds a cursor, Shift+Alt+drag selects a box.
        EditorView.clickAddsSelectionRange.of((e) => e.altKey),
        rectangularSelection({ eventFilter: (e) => e.altKey && e.shiftKey }),
        crosshairCursor({ key: 'Alt' }),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        search({ top: true }),
        phrases,
        tabSizeConf.of(tabSizeExt(o.tabSize ?? 4)),
        wrapConf.of(o.wrap ? EditorView.lineWrapping : []),
        own,
        tabConf.of(tabKeys(o.tabMovesFocus)),
        keymap.of([{ key: 'Escape', run: closeShownCompletion }, ...closeBracketsKeymap, ...extraKeys, ...vscodeKeys, ...defaultKeymap, ...historyKeymap]),
        completionConf.of(completion(o.language)),
        syntaxHighlighting(highlight),
        theme,
        languageConf.of([]),
        labelConf.of(labelAttrs(o.label)),
        EditorView.updateListener.of((u) => {
          for (const tr of u.transactions) {
            if (tr.isUserEvent('input.paste')) o.onPaste(inserted(tr));
            else if (tr.isUserEvent('input.type')) o.onTyped(inserted(tr));
          }
          if (u.docChanged) o.onChange(u.state.doc.toString());
          if (u.focusChanged && !u.view.hasFocus) o.onBlur();
          if (u.selectionSet || u.docChanged) report(u.state);
        }),
      ],
    }),
  });

  function report(state: EditorState) {
    const head = state.selection.main.head;
    const line = state.doc.lineAt(head);
    o.onCursor?.(line.number, head - line.from + 1);
  }
  report(view.state);

  // The newest request wins, so a slow first mode never replaces a later choice.
  let wanted: Language = o.language;
  let destroyed = false;
  const applyMode = (language: Language) => {
    wanted = language;
    view.dispatch({ effects: completionConf.reconfigure(completion(language)) });
    loadMode(language)
      .then((ext) => {
        if (!destroyed && wanted === language) view.dispatch({ effects: languageConf.reconfigure(ext) });
      })
      .catch(() => undefined);
  };
  applyMode(o.language);

  return {
    setLanguage(language, label) {
      view.dispatch({ effects: labelConf.reconfigure(labelAttrs(label)) });
      if (language !== wanted) applyMode(language);
    },
    setTabMovesFocus(on) {
      view.dispatch({ effects: tabConf.reconfigure(tabKeys(on)) });
    },
    setTabSize(size) {
      view.dispatch({ effects: tabSizeConf.reconfigure(tabSizeExt(size)) });
    },
    setWrap(on) {
      view.dispatch({ effects: wrapConf.reconfigure(on ? EditorView.lineWrapping : []) });
    },
    setDoc(doc) {
      if (doc === view.state.doc.toString()) return;
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: doc } });
    },
    getDoc() {
      return view.state.doc.toString();
    },
    jumpTo(line, col) {
      const doc = view.state.doc;
      const l = doc.line(Math.min(Math.max(1, line), doc.lines));
      const pos = Math.min(l.from + Math.max(0, (col ?? 1) - 1), l.to);
      view.dispatch({ selection: { anchor: pos }, effects: EditorView.scrollIntoView(pos, { y: 'center' }) });
      view.focus();
    },
    focus() {
      view.focus();
    },
    destroy() {
      destroyed = true;
      view.destroy();
    },
  };
}
