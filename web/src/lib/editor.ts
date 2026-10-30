/**
 * The code editor: CodeMirror 6 set up for the workspace, loaded with a
 * dynamic import by components/CodeEditor.svelte only, so CodeMirror stays
 * out of the main bundle; each language mode is its own chunk and a mode
 * that fails to load leaves plain text that still edits and submits.
 * Colours come from CSS custom properties, so the editor follows every
 * theme without being rebuilt. Keys follow VS Code (the full table is
 * lib/shortcuts.ts), Escape then Tab always leaves the editor so a
 * keyboard user is never trapped, and typed and pasted characters are
 * counted from each transaction's user event, never from timing. A
 * contest that blocks paste gets a compartment that refuses every paste
 * and drop and tells the caller, so the workspace can say why.
 */
import { autocompletion, closeBrackets, closeBracketsKeymap, closeCompletion, completeAnyWord, completeFromList, currentCompletions, snippetCompletion, type Completion } from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap, indentMore, indentWithTab, selectParentSyntax } from '@codemirror/commands';
import { bracketMatching, foldGutter, HighlightStyle, indentOnInput, indentRange, indentUnit, syntaxHighlighting } from '@codemirror/language';
import { forEachDiagnostic, lintGutter, previousDiagnostic, setDiagnostics, setDiagnosticsEffect, type Diagnostic } from '@codemirror/lint';
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

/** A program language, or plain text (the statement). */
export type EditorLanguage = Language | 'text';

const loaded = new Map<Language, Promise<Extension>>();

/** The language mode, fetched once per page; a failed fetch is tried again next time. */
export function loadMode(language: EditorLanguage): Promise<Extension> {
  if (language === 'text') return Promise.resolve([]);
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
    // Compiler marks (v4 E4): each kind has its own shape, not only its colour.
    '.cm-lintRange': { backgroundImage: 'none', textDecorationLine: 'underline', textUnderlineOffset: '0.25em' },
    '.cm-lintRange-error': { textDecorationStyle: 'wavy', textDecorationColor: 'var(--code-invalid)' },
    '.cm-lintRange-warning': { textDecorationStyle: 'wavy', textDecorationColor: 'var(--code-function)' },
    '.cm-lintRange-info': { textDecorationStyle: 'dotted', textDecorationColor: 'var(--code-gutter)' },
    '.cm-lint-marker': { content: 'normal', display: 'block', width: '0.6em', height: '0.6em', margin: '0.45em auto 0' },
    '.cm-lint-marker-error': { borderRadius: '50%', backgroundColor: 'var(--code-invalid)' },
    '.cm-lint-marker-warning': { clipPath: 'polygon(50% 0, 100% 100%, 0 100%)', backgroundColor: 'var(--code-function)' },
    '.cm-lint-marker-info': { borderRadius: '50%', border: 'calc(2 * var(--rule)) solid var(--code-gutter)' },
    '.cm-diagnostic': { padding: 'var(--space-1) var(--space-2)', fontFamily: 'var(--font-mono)', fontSize: 'var(--fs-0)' },
    '.cm-diagnostic-error': { borderLeftColor: 'var(--code-invalid)' },
    '.cm-diagnostic-warning': { borderLeftColor: 'var(--code-function)' },
    '.cm-diagnostic-info': { borderLeftColor: 'var(--code-gutter)' },
    '.cm-foldGutter .cm-gutterElement': { padding: '0 var(--space-1)', cursor: 'pointer' },
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
  /** A paste or drop was refused because `blockPaste` is on. */
  onPasteBlocked?: () => void;
}

export interface EditorOptions extends EditorCallbacks {
  parent: HTMLElement;
  doc: string;
  language: EditorLanguage;
  label: string;
  /** An id for the editable element. */
  id?: string;
  /** The id of the element that describes the editor (its keyboard hint). */
  describedBy?: string;
  tabMovesFocus: boolean;
  tabSize?: 2 | 4;
  wrap?: boolean;
  /** Under 1024 px code is read, not written: no edits, and no on-screen keyboard. */
  readOnly?: boolean;
  /** Refuse pastes and drops into the editor (a contest's exam option). */
  blockPaste?: boolean;
}

/** The handle the component keeps. */
export interface EditorHandle {
  setLanguage(language: EditorLanguage, label: string): void;
  setTabMovesFocus(on: boolean): void;
  setTabSize(size: 2 | 4): void;
  setWrap(on: boolean): void;
  setReadOnly(on: boolean): void;
  setBlockPaste(on: boolean): void;
  /** Replaces the whole document; not counted as typing. */
  setDoc(doc: string): void;
  getDoc(): string;
  /** Marks compiler messages in the code (underline, gutter, hover); an empty list clears them. */
  setMarks(marks: readonly EditorMark[]): void;
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

// The VS Code keys the replit keymap leaves out.

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

/**
 * Shift+Alt+F: re-indents the whole file with the language's own indent
 * rules, VS Code's "Format Document" key (V4-5). Not a formatter: spacing
 * inside a line is left as typed. The cursor stays on its code.
 */
export const reindentFile: Command = (view) => {
  if (view.state.readOnly) return false;
  const changes = indentRange(view.state, 0, view.state.doc.length);
  if (changes.empty) return true;
  view.dispatch({ changes, userEvent: 'input.indent' });
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
  { key: 'Shift-Alt-f', run: reindentFile, preventDefault: true },
  // F8 (next) comes from the VS Code keymap; it leaves Shift+F8 out.
  { key: 'Shift-F8', run: previousDiagnostic, preventDefault: true },
];

/** A compiler message to mark in the code (lib/compileErrors.ts compileMarks). */
export interface EditorMark {
  line: number;
  col: number | null;
  text: string;
  severity: 'error' | 'warning' | 'info';
}

/**
 * The marks as diagnostics, from the column to the line's end. With no
 * column, or one at the end ("expected ';'" points past the last
 * character), the line's code is underlined from its first non-blank.
 */
export function markDiagnostics(doc: EditorState['doc'], marks: readonly EditorMark[]): Diagnostic[] {
  return marks.map((m) => {
    const l = doc.line(Math.min(Math.max(1, m.line), doc.lines));
    const at = l.from + Math.max(0, (m.col ?? 1) - 1);
    const from = m.col !== null && at < l.to ? at : l.from + (l.text.length - l.text.trimStart().length);
    return { from: Math.min(from, l.to), to: l.to, severity: m.severity, message: m.text };
  });
}

// A mark goes when its line is edited; the rest follow the code.
const dropEditedMarks = EditorState.transactionExtender.of((tr) => {
  if (!tr.docChanged) return null;
  const kept: Diagnostic[] = [];
  let dropped = false;
  forEachDiagnostic(tr.startState, (d, from, to) => {
    const start = tr.startState.doc.lineAt(from).from;
    const end = tr.startState.doc.lineAt(to).to;
    let touched = false;
    tr.changes.iterChangedRanges((a, b) => {
      if (a <= end && b >= start) touched = true;
    });
    if (touched) dropped = true;
    else kept.push({ ...d, from: tr.changes.mapPos(from), to: tr.changes.mapPos(to) });
  });
  return dropped ? { effects: setDiagnosticsEffect.of(kept) } : null;
});

// Escape closes the completion list only while one is shown. CodeMirror's
// own closeCompletion also takes Escape when a source answered with no
// options, which would swallow the Escape of "Escape, then Tab" and trap a
// keyboard user in the editor.
const closeShownCompletion: Command = (view) => currentCompletions(view.state).length > 0 && closeCompletion(view);

// Tab is bound by the tab compartment, so "Tab moves focus" can turn it off;
// Escape for completion is the guarded one above.
const vscodeKeys = vscodeKeymap.filter((b) => !(b.key === 'Tab' && b.run === indentMore) && b.run !== closeCompletion);

/**
 * C++ and Java modes bring no completion of their own: words from the file
 * plus the names a contest solution reaches for, since there is no
 * language server.
 */
const KEYWORDS: Partial<Record<Language, string[]>> = {
  cpp17: [
    'auto', 'bool', 'break', 'char', 'const', 'continue', 'double', 'else', 'false', 'for', 'if', 'int', 'long', 'return',
    'size_t', 'sizeof', 'static', 'struct', 'true', 'unsigned', 'void', 'while', 'using', 'namespace', 'include',
    'std', 'cin', 'cout', 'endl', 'string', 'vector', 'pair', 'map', 'set', 'unordered_map', 'unordered_set', 'queue',
    'priority_queue', 'stack', 'deque', 'sort', 'reverse', 'min', 'max', 'swap', 'abs', 'push_back', 'emplace_back',
    'begin', 'end', 'size', 'empty', 'lower_bound', 'upper_bound', 'make_pair', 'first', 'second',
  ],
  java21: [
    'public', 'private', 'static', 'final', 'class', 'void', 'int', 'long', 'double', 'boolean', 'char', 'String',
    'return', 'if', 'else', 'for', 'while', 'break', 'continue', 'new', 'true', 'false', 'null', 'import', 'throws',
    'IOException', 'BufferedReader', 'InputStreamReader', 'StringTokenizer', 'PrintWriter', 'Scanner', 'System',
    'Integer', 'Long', 'Math', 'Arrays', 'ArrayList', 'HashMap', 'HashSet', 'List', 'Map', 'Set', 'parseInt',
    'parseLong', 'println', 'nextInt', 'nextLong', 'readLine', 'length', 'size', 'sort',
  ],
};

/**
 * Snippets a contest solution starts from (v4 E7): the label is what to
 * type, Enter or Tab takes it, and Tab then moves between its fields.
 * A "\t" becomes the file's indent unit.
 */
export const SNIPPETS: Partial<Record<Language, { label: string; detail: string; template: string }[]>> = {
  cpp17: [
    { label: 'main', detail: 'program with fast input', template: '#include <bits/stdc++.h>\nusing namespace std;\n\nint main() {\n\tios::sync_with_stdio(false);\n\tcin.tie(nullptr);\n\t${}\n\treturn 0;\n}' },
    { label: 'fastio', detail: 'fast cin and cout', template: 'ios::sync_with_stdio(false);\ncin.tie(nullptr);' },
    { label: 'fori', detail: 'for loop', template: 'for (int ${i} = 0; ${i} < ${n}; ${i}++) {\n\t${}\n}' },
    { label: 'tests', detail: 'loop over test cases', template: 'int t;\ncin >> t;\nwhile (t--) {\n\t${}\n}' },
    { label: 'readvec', detail: 'read n numbers', template: 'vector<long long> ${a}(${n});\nfor (auto &x : ${a}) cin >> x;' },
    { label: 'll', detail: 'long long alias', template: 'using ll = long long;' },
  ],
  java21: [
    { label: 'main', detail: 'class Main with fast input', template: 'import java.io.*;\nimport java.util.*;\n\npublic class Main {\n\tpublic static void main(String[] args) throws IOException {\n\t\tBufferedReader in = new BufferedReader(new InputStreamReader(System.in));\n\t\tPrintWriter out = new PrintWriter(new BufferedWriter(new OutputStreamWriter(System.out)));\n\t\t${}\n\t\tout.flush();\n\t}\n}' },
    { label: 'tokens', detail: 'split the next line', template: 'StringTokenizer st = new StringTokenizer(in.readLine());' },
    { label: 'readint', detail: 'next number from the line', template: 'int ${n} = Integer.parseInt(st.nextToken());' },
    { label: 'fori', detail: 'for loop', template: 'for (int ${i} = 0; ${i} < ${n}; ${i}++) {\n\t${}\n}' },
    { label: 'tests', detail: 'loop over test cases', template: 'int t = Integer.parseInt(in.readLine().trim());\nwhile (t-- > 0) {\n\t${}\n}' },
  ],
};

/** The completion list for a language: its words, then its snippets. */
export function completionOptions(language: Language): Completion[] {
  return [
    ...(KEYWORDS[language] ?? []).map((label) => ({ label })),
    ...(SNIPPETS[language] ?? []).map((s) => snippetCompletion(s.template, { label: s.label, detail: s.detail, type: 'text' })),
  ];
}

const completion = (language: EditorLanguage) => {
  const words = language === 'text' || !KEYWORDS[language] ? undefined : completionOptions(language);
  return autocompletion({ defaultKeymap: false, ...(words ? { override: [completeAnyWord, completeFromList(words)] } : {}) });
};

// The find and go-to-line panels' words, in sentence case.
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
  const readOnlyConf = new Compartment();
  const readOnlyExt = (on: boolean) => (on ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []);
  const blockPasteConf = new Compartment();
  // Handled before CodeMirror's own paste and drop, so nothing is inserted.
  const refuse = (event: Event) => {
    event.preventDefault();
    o.onPasteBlocked?.();
    return true;
  };
  const blockPasteExt = (on: boolean) => (on ? EditorView.domEventHandlers({ paste: refuse, drop: refuse }) : []);

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
      // Browser translation must never rewrite code.
      translate: 'no',
      ...(o.id ? { id: o.id } : {}),
      // Plain text (the statement) is one field of a form and scrolls inside
      // a fixed height: an explicit tab stop lets every checker see that its
      // scrolling region is reachable from the keyboard.
      ...(o.language === 'text' ? { tabindex: '0' } : {}),
      ...(o.describedBy ? { 'aria-describedby': o.describedBy } : {}),
    });

  const view = new EditorView({
    parent: o.parent,
    state: EditorState.create({
      doc: o.doc,
      extensions: [
        lineNumbers(),
        // Code only: the statement is prose.
        o.language === 'text' ? [] : [foldGutter(), lintGutter(), dropEditedMarks],
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
        readOnlyConf.of(readOnlyExt(o.readOnly ?? false)),
        blockPasteConf.of(blockPasteExt(o.blockPaste ?? false)),
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
  let wanted: EditorLanguage = o.language;
  let destroyed = false;
  const applyMode = (language: EditorLanguage) => {
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
    setReadOnly(on) {
      view.dispatch({ effects: readOnlyConf.reconfigure(readOnlyExt(on)) });
    },
    setBlockPaste(on) {
      view.dispatch({ effects: blockPasteConf.reconfigure(blockPasteExt(on)) });
    },
    setDoc(doc) {
      if (doc === view.state.doc.toString()) return;
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: doc } });
    },
    getDoc() {
      return view.state.doc.toString();
    },
    setMarks(marks) {
      view.dispatch(setDiagnostics(view.state, markDiagnostics(view.state.doc, marks)));
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
