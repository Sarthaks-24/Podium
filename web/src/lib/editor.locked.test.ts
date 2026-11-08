// @vitest-environment jsdom
// Locked code in the editor (v2.0.2): changes touching the locked top or
// bottom are refused, a pasted whole file is cut back to the student's code,
// the cursor stays in the code, and every line, mark and callback counts from
// the student's first line.
import { afterEach, describe, expect, it } from 'vitest';
import { foldedRanges } from '@codemirror/language';
import { forEachDiagnostic } from '@codemirror/lint';
import { Text } from '@codemirror/state';
import { EditorView, runScopeHandlers } from '@codemirror/view';
import { createEditor, insertLineAbove, markDiagnostics, reindentFile, type EditorHandle, type EditorOptions } from './editor';

let handle: EditorHandle | null = null;

function open(over: Partial<EditorOptions> = {}): EditorView {
  const parent = document.createElement('div');
  document.body.append(parent);
  const noop = () => undefined;
  handle = createEditor({
    parent,
    doc: '',
    language: 'python3',
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

afterEach(() => {
  handle?.destroy();
  handle = null;
  document.body.innerHTML = '';
});

const TOP = 'import sys\nimport math\n';
const BOTTOM = 'print(f(1))\nprint(f(2))\n';
const CODE = 'def f(n):\n    return n\n';
const topEnd = TOP.length;
const codeEnd = topEnd + 'def f(n):\n    return n'.length;

function locked(over: Partial<EditorOptions> = {}) {
  const events = { changes: [] as string[], refused: 0, cursors: [] as [number, number][] };
  const view = open({
    doc: CODE,
    locked: { top: TOP, bottom: BOTTOM },
    onChange: (s) => events.changes.push(s),
    onLockedEdit: () => events.refused++,
    onCursor: (line, col) => events.cursors.push([line, col]),
    ...over,
  });
  return { view, events };
}

describe('locked code', () => {
  it('shows top, code and bottom, and hands out the code alone', () => {
    const { view } = locked();
    expect(view.state.doc.toString()).toBe(`${TOP}def f(n):\n    return n\nprint(f(1))\nprint(f(2))`);
    expect(handle?.getDoc()).toBe('def f(n):\n    return n');
  });

  it('allows typing at both edges of the code', () => {
    const { view, events } = locked();
    view.dispatch({ changes: { from: topEnd, insert: '# a\n' }, userEvent: 'input.type' });
    view.dispatch({ changes: { from: codeEnd + 4, insert: '\n# z' }, userEvent: 'input.type' });
    expect(handle?.getDoc()).toBe('# a\ndef f(n):\n    return n\n# z');
    expect(view.state.doc.toString().startsWith(`${TOP}# a`)).toBe(true);
    expect(view.state.doc.toString().endsWith('# z\nprint(f(1))\nprint(f(2))')).toBe(true);
    expect(events.refused).toBe(0);
    expect(events.changes.at(-1)).toBe('# a\ndef f(n):\n    return n\n# z');
  });

  it('refuses typing, deleting and replacing across a locked range', () => {
    const { view, events } = locked();
    const before = view.state.doc.toString();
    view.dispatch({ changes: { from: 3, insert: 'x' }, userEvent: 'input.type' });
    view.dispatch({ changes: { from: topEnd - 1, to: topEnd }, userEvent: 'delete.backward' });
    view.dispatch({ changes: { from: 0, to: topEnd + 3 }, userEvent: 'input.type' });
    view.dispatch({ changes: { from: codeEnd + 1, insert: 'x' }, userEvent: 'input.type' });
    view.dispatch({ changes: { from: codeEnd, to: codeEnd + 3 }, userEvent: 'delete.forward' });
    view.dispatch({ changes: { from: 0, to: view.state.doc.length }, userEvent: 'delete.selection' });
    expect(view.state.doc.toString()).toBe(before);
    expect(events.refused).toBe(6);
    expect(events.changes).toEqual([]);
  });

  it('refuses a paste of anything else over a locked range', () => {
    const { view, events } = locked();
    const before = view.state.doc.toString();
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: 'print(1)\n' }, userEvent: 'input.paste' });
    expect(view.state.doc.toString()).toBe(before);
    expect(events.refused).toBe(1);
  });

  it('cuts the exact locked parts off a pasted whole file', () => {
    const { view, events } = locked();
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: `${TOP}def g(n):\n    return 2 * n\n${BOTTOM}` }, userEvent: 'input.paste' });
    expect(handle?.getDoc()).toBe('def g(n):\n    return 2 * n');
    expect(view.state.doc.toString()).toBe(`${TOP}def g(n):\n    return 2 * n\nprint(f(1))\nprint(f(2))`);
    expect(events.refused).toBe(0);
    view.dispatch({ changes: { from: topEnd, insert: `${TOP}# new\n` }, userEvent: 'input.paste' });
    // A top alone is the student's own text and stays, as on the server.\n    expect(handle?.getDoc()).toBe(`${TOP}# new\ndef g(n):\n    return 2 * n`);
  });

  it('keeps the cursor and Select all inside the code', () => {
    const { view, events } = locked();
    view.dispatch({ selection: { anchor: 2 } });
    expect(view.state.selection.main.head).toBe(topEnd);
    view.dispatch({ selection: { anchor: view.state.doc.length } });
    expect(view.state.selection.main.head).toBe(codeEnd);
    runScopeHandlers(view, new KeyboardEvent('keydown', { key: 'a', keyCode: 65, ctrlKey: true, bubbles: true, cancelable: true }), 'editor');
    expect(view.state.selection.main.from).toBe(topEnd);
    expect(view.state.selection.main.to).toBe(codeEnd);
    view.dispatch({ selection: { anchor: topEnd + 'def f(n):\n    '.length } });
    expect(events.cursors.at(-1)).toEqual([2, 5]);
  });

  it('replace all skips matches in locked text and replaces the rest', () => {
    const { view, events } = locked({ doc: 'sys = 1\nsys = 2\n' });
    const at = (v: string, from: number) => v.indexOf('sys', from);
    const doc = view.state.doc.toString();
    const changes = [at(doc, 0), at(doc, topEnd), at(doc, topEnd + 5)].map((p) => ({ from: p, to: p + 3, insert: 'x' }));
    view.dispatch({ changes, userEvent: 'input.replace.all' });
    expect(view.state.doc.toString().startsWith(TOP)).toBe(true);
    expect(handle?.getDoc()).toBe('x = 1\nx = 2');
    expect(events.refused).toBe(0);
  });

  it('replaces the student code with setDoc and keeps the locked parts', () => {
    const { view, events } = locked();
    handle?.setDoc('def f(n):\n    return 3 * n\n');
    expect(view.state.doc.toString()).toBe(`${TOP}def f(n):\n    return 3 * n\nprint(f(1))\nprint(f(2))`);
    expect(events.changes.at(-1)).toBe('def f(n):\n    return 3 * n');
  });

  it('swaps the locked parts on a language switch and keeps the code', () => {
    const { view, events } = locked();
    const n = events.changes.length;
    handle?.setLocked({ top: '#include <bits/stdc++.h>', bottom: 'int main() {}' });
    expect(view.state.doc.toString()).toBe('#include <bits/stdc++.h>\ndef f(n):\n    return n\nint main() {}');
    expect(handle?.getDoc()).toBe('def f(n):\n    return n');
    expect(events.changes.length).toBe(n);
    view.dispatch({ changes: { from: 2, insert: 'x' }, userEvent: 'input.type' });
    expect(events.refused).toBe(1);
    handle?.setLocked(null);
    expect(view.state.doc.toString()).toBe('def f(n):\n    return n');
  });

  it('folds each block to a label and starts folded when asked', () => {
    const { view } = locked({ collapseLocked: true });
    expect(foldedRanges(view.state).size).toBe(2);
    const labels = [...view.dom.querySelectorAll('button.cm-foldPlaceholder')].map((b) => b.textContent);
    expect(labels).toEqual(['Locked code, 2 lines', 'Locked code, 2 lines']);
    handle?.setLockedCollapsed(false);
    expect(foldedRanges(view.state).size).toBe(0);
    handle?.setLockedCollapsed(true);
    expect(foldedRanges(view.state).size).toBe(2);
  });

  it('re-indents only the student code and adds lines only inside it', () => {
    const { view } = locked({ doc: 'def f(n):\nreturn n\n' });
    view.dispatch({ selection: { anchor: 0 } });
    expect(insertLineAbove(view)).toBe(true);
    expect(view.state.doc.toString()).toBe(`${TOP}\ndef f(n):\nreturn n\nprint(f(1))\nprint(f(2))`);
    reindentFile(view);
    const doc = view.state.doc.toString();
    expect(doc.startsWith(TOP)).toBe(true);
    expect(doc.endsWith('print(f(1))\nprint(f(2))')).toBe(true);
  });

  it('puts compiler marks on the right line of the document', () => {
    const doc = Text.of(`${TOP}def f(n):\n    return n\nprint(f(1))`.split('\n'));
    const [d] = markDiagnostics(doc, [{ line: 2, col: 5, text: 'bad', severity: 'error' }], 2);
    expect(doc.lineAt(d.from).number).toBe(4);
    expect(d.from - doc.lineAt(d.from).from).toBe(4);
    const [plain] = markDiagnostics(doc, [{ line: 2, col: 1, text: 'bad', severity: 'error' }]);
    expect(doc.lineAt(plain.from).number).toBe(2);
  });

  it('marks and jumps count from the student first line', () => {
    const { view } = locked();
    handle?.setMarks([{ line: 2, col: 5, text: 'bad', severity: 'error' }]);
    let at = -1;
    forEachDiagnostic(view.state, (_d, from) => (at = view.state.doc.lineAt(from).number));
    expect(at).toBe(4);
    handle?.jumpTo(2, 5);
    expect(view.state.doc.lineAt(view.state.selection.main.head).number).toBe(4);
  });

  it('leaves an editor without locked code as it was', () => {
    const view = open({ doc: 'a\nb\n' });
    expect(view.state.doc.toString()).toBe('a\nb\n');
    handle?.setDoc('c');
    expect(handle?.getDoc()).toBe('c');
    view.dispatch({ changes: { from: 0, insert: 'x' }, userEvent: 'input.type' });
    expect(handle?.getDoc()).toBe('xc');
  });
});
