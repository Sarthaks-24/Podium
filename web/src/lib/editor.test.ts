// @vitest-environment jsdom
// The editor carries what the shortcut table (lib/shortcuts.ts) promises:
// closing brackets, completion on Ctrl+Space, line wrapping, find at the
// top, and folding on the VS Code keys. Keys are pressed through
// CodeMirror's own key handling, so a binding lost from the keymap fails here.
import { afterEach, describe, expect, it } from 'vitest';
import { completionStatus } from '@codemirror/autocomplete';
import { foldedRanges } from '@codemirror/language';
import { searchPanelOpen } from '@codemirror/search';
import { EditorView, runScopeHandlers } from '@codemirror/view';
import { SNIPPETS, completionOptions, createEditor, loadMode, type EditorHandle, type EditorOptions } from './editor';

let handle: EditorHandle | null = null;

function open(over: Partial<EditorOptions> = {}): EditorView {
  const parent = document.createElement('div');
  document.body.append(parent);
  const noop = () => undefined;
  handle = createEditor({
    parent,
    doc: '',
    language: 'text',
    label: 'Code',
    tabMovesFocus: false,
    onChange: noop,
    onTyped: noop,
    onPaste: noop,
    onBlur: noop,
    onSubmit: noop,
    onRun: noop,
    ...over,
  });
  const view = EditorView.findFromDOM(parent.querySelector('.cm-editor') as HTMLElement);
  if (!view) throw new Error('no editor view');
  return view;
}

/** Presses a key with Ctrl (jsdom is not a Mac, so Mod is Ctrl). */
function press(view: EditorView, key: string, keyCode: number, shift = false): boolean {
  const event = new KeyboardEvent('keydown', { key, keyCode, ctrlKey: true, shiftKey: shift, bubbles: true, cancelable: true });
  return runScopeHandlers(view, event, 'editor');
}

afterEach(() => {
  handle?.destroy();
  handle = null;
  document.body.innerHTML = '';
});

describe('createEditor extensions', () => {
  it('types the closing bracket too', () => {
    const view = open();
    const typed = view.state.facet(EditorView.inputHandler).some((h) => h(view, 0, 0, '(', () => view.state.update({ changes: { from: 0, insert: '(' } })));
    expect(typed).toBe(true);
    expect(view.state.doc.toString()).toBe('()');
  });

  it('opens completion on Ctrl+Space', () => {
    const view = open({ doc: 'alpha al', language: 'cpp17' });
    view.dispatch({ selection: { anchor: 8 } });
    expect(press(view, ' ', 32)).toBe(true);
    expect(completionStatus(view.state)).not.toBeNull();
  });

  it('wraps lines when asked, and stops when turned off', () => {
    const view = open({ wrap: true });
    expect(view.contentDOM.classList.contains('cm-lineWrapping')).toBe(true);
    handle?.setWrap(false);
    expect(view.contentDOM.classList.contains('cm-lineWrapping')).toBe(false);
  });

  it('opens find at the top on Ctrl+F', () => {
    const view = open({ doc: 'a b a' });
    expect(press(view, 'f', 70)).toBe(true);
    expect(searchPanelOpen(view.state)).toBe(true);
    expect(view.dom.querySelector('.cm-panels-top .cm-search')).not.toBeNull();
  });

  it('folds and unfolds the block on Ctrl+Shift+[ and Ctrl+Shift+]', async () => {
    await loadMode('python3');
    const view = open({ doc: 'def f():\n    a = 1\n    return a\n', language: 'python3' });
    // The mode is applied after its (already loaded) chunk resolves.
    await Promise.resolve();
    await Promise.resolve();
    view.dispatch({ selection: { anchor: 3 } });
    expect(press(view, '{', 219, true)).toBe(true);
    expect(foldedRanges(view.state).size).toBe(1);
    expect(press(view, '}', 221, true)).toBe(true);
    expect(foldedRanges(view.state).size).toBe(0);
  });
});

describe('snippets (v4 E7)', () => {
  it('puts the C++ and Java snippets in the completion list, after the words', () => {
    const cpp = completionOptions('cpp17').map((c) => c.label);
    expect(cpp).toEqual(expect.arrayContaining(['main', 'fastio', 'fori', 'tests', 'readvec', 'll', 'vector']));
    expect(cpp.indexOf('main')).toBeGreaterThan(cpp.indexOf('vector'));
    const java = completionOptions('java21').map((c) => c.label);
    expect(java).toEqual(expect.arrayContaining(['main', 'tokens', 'readint', 'fori', 'tests']));
    expect(completionOptions('python3').map((c) => c.label)).toEqual([]);
  });

  it('gives Java the class name the judge runs', () => {
    expect(SNIPPETS.java21?.find((s) => s.label === 'main')?.template).toContain('public class Main {');
  });
});
