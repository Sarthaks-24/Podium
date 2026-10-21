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
 *   ahead of the default keymap, which would otherwise insert a line.
 * - Typed and pasted characters are reported from each transaction's
 *   userEvent ("input.type", "input.paste"), never with timing.
 * - `describedBy` names the element holding the editor's keyboard hint, so
 *   a screen reader hears how to leave the editor (X2).
 */
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { bracketMatching, HighlightStyle, indentOnInput, indentUnit, syntaxHighlighting } from '@codemirror/language';
import { Compartment, EditorState, type Extension } from '@codemirror/state';
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from '@codemirror/view';
import { tags as t } from '@lezer/highlight';
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
}

export interface EditorOptions extends EditorCallbacks {
  parent: HTMLElement;
  doc: string;
  language: Language;
  label: string;
  /** The id of the element that describes the editor (its keyboard hint). */
  describedBy?: string;
  tabMovesFocus: boolean;
}

/** The handle the component keeps. */
export interface EditorHandle {
  setLanguage(language: Language, label: string): void;
  setTabMovesFocus(on: boolean): void;
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

/** Creates the editor inside `parent`. */
export function createEditor(o: EditorOptions): EditorHandle {
  const languageConf = new Compartment();
  const labelConf = new Compartment();
  const tabConf = new Compartment();

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
        indentOnInput(),
        bracketMatching(),
        highlightActiveLine(),
        EditorState.tabSize.of(4),
        indentUnit.of('    '),
        own,
        tabConf.of(tabKeys(o.tabMovesFocus)),
        keymap.of([...defaultKeymap, ...historyKeymap]),
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
        }),
      ],
    }),
  });

  // The newest request wins, so a slow first mode never replaces a later choice.
  let wanted: Language = o.language;
  let destroyed = false;
  const applyMode = (language: Language) => {
    wanted = language;
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
