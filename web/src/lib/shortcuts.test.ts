// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { NOT_AVAILABLE, adjacentLabel, isMacPlatform, isPlainKey, isTypingTarget, shortcutGroups, shortcutKeys } from './shortcuts';

function el(html: string): HTMLElement {
  const host = document.createElement('div');
  host.innerHTML = html;
  document.body.append(host);
  return host.firstElementChild as HTMLElement;
}

describe('isTypingTarget', () => {
  it('treats text fields, textareas, selects and editable content as typing', () => {
    expect(isTypingTarget(el('<input type="text">'))).toBe(true);
    expect(isTypingTarget(el('<input>'))).toBe(true);
    expect(isTypingTarget(el('<input type="email">'))).toBe(true);
    expect(isTypingTarget(el('<textarea></textarea>'))).toBe(true);
    expect(isTypingTarget(el('<select><option>a</option></select>'))).toBe(true);
    const editor = el('<div contenteditable="true"><span>x</span></div>');
    expect(isTypingTarget(editor.firstElementChild)).toBe(true);
  });

  it('lets buttons, links, checkboxes and the page through', () => {
    expect(isTypingTarget(el('<button>Submit</button>'))).toBe(false);
    expect(isTypingTarget(el('<a href="/">Home</a>'))).toBe(false);
    expect(isTypingTarget(el('<input type="checkbox">'))).toBe(false);
    expect(isTypingTarget(el('<input type="radio">'))).toBe(false);
    expect(isTypingTarget(document.body)).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});

describe('isPlainKey', () => {
  it('allows Shift but not Ctrl, Cmd or Alt', () => {
    expect(isPlainKey({ ctrlKey: false, metaKey: false, altKey: false })).toBe(true);
    expect(isPlainKey({ ctrlKey: true, metaKey: false, altKey: false })).toBe(false);
    expect(isPlainKey({ ctrlKey: false, metaKey: true, altKey: false })).toBe(false);
    expect(isPlainKey({ ctrlKey: false, metaKey: false, altKey: true })).toBe(false);
  });
});

describe('adjacentLabel', () => {
  const labels = ['A', 'B', 'C'];

  it('steps one problem back or forward', () => {
    expect(adjacentLabel(labels, 'B', -1)).toBe('A');
    expect(adjacentLabel(labels, 'B', 1)).toBe('C');
  });

  it('stops at either end instead of wrapping', () => {
    expect(adjacentLabel(labels, 'A', -1)).toBeNull();
    expect(adjacentLabel(labels, 'C', 1)).toBeNull();
  });

  it('gives nothing for a label that is not in the contest', () => {
    expect(adjacentLabel(labels, 'Z', 1)).toBeNull();
    expect(adjacentLabel([], '', 1)).toBeNull();
  });
});

describe('isMacPlatform', () => {
  it('reads the platform, then the user agent', () => {
    expect(isMacPlatform({ platform: 'MacIntel' })).toBe(true);
    expect(isMacPlatform({ platform: '', userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)' })).toBe(true);
    expect(isMacPlatform({ platform: 'Win32' })).toBe(false);
    expect(isMacPlatform(undefined)).toBe(false);
  });
});

describe('shortcutGroups', () => {
  it('names Ctrl or Cmd to match the platform', () => {
    const win = shortcutGroups({ mac: false, organiser: false }).flatMap((g) => g.items.flatMap((i) => i.keys));
    const mac = shortcutGroups({ mac: true, organiser: false }).flatMap((g) => g.items.flatMap((i) => i.keys));
    expect(win).toContain('Ctrl+Enter');
    expect(win).not.toContain('Cmd+Enter');
    expect(mac).toContain('Cmd+Enter');
  });

  it('lists the keys the workspace and contest page bind', () => {
    const keys = shortcutGroups({ mac: false, organiser: false }).flatMap((g) => g.items.flatMap((i) => i.keys));
    for (const k of ['?', 'Ctrl+Enter', "Ctrl+'", 'Ctrl+S', '[', ']', 'Esc', 'Tab']) expect(keys).toContain(k);
  });

  it('shows the problem editor keys to organisers only', () => {
    const titles = (organiser: boolean) => shortcutGroups({ mac: false, organiser }).map((g) => g.title);
    const editor = ['Editing', 'Selection and cursors', 'Moving around', 'Find and replace', NOT_AVAILABLE];
    expect(titles(false)).toEqual(['Anywhere', 'Solving a problem', ...editor]);
    expect(titles(true)).toEqual(['Anywhere', 'Solving a problem', ...editor, 'Problem editor']);
  });

  it('lists the VS Code editing keys students asked for', () => {
    const actions = shortcutGroups({ mac: false, organiser: false }).flatMap((g) => g.items.map((i) => `${i.keys.join(' ')}: ${i.action}`));
    for (const want of ['Ctrl+/: Comment', 'Ctrl+F: Find', 'Ctrl+D: Select the word', 'Alt+Up Alt+Down: Move the line', 'Ctrl+Z: Undo']) {
      expect(actions.some((a) => a.startsWith(want))).toBe(true);
    }
  });

  it('names Cmd and Option on a Mac', () => {
    const keys = shortcutGroups({ mac: true, organiser: false }).flatMap((g) => g.items.flatMap((i) => i.keys));
    expect(keys).toContain('Cmd+/');
    expect(keys).toContain('Option+Up');
    expect(keys.some((k) => k.startsWith('Alt+'))).toBe(false);
  });

  it('writes every action in sentence case with no ending full stop', () => {
    for (const g of shortcutGroups({ mac: false, organiser: true })) {
      expect(g.title[0]).toBe(g.title[0].toUpperCase());
      for (const item of g.items) {
        expect(item.action[0]).toBe(item.action[0].toUpperCase());
        expect(item.action.endsWith('.')).toBe(false);
        expect(item.action).not.toMatch(/!/);
      }
    }
  });
});

describe('shortcut hints (v4 U6)', () => {
  it("reads a row's keys in the table's words and in the ARIA form", () => {
    expect(shortcutKeys('Submit', false)).toEqual({ text: 'Ctrl+Enter', aria: 'Control+Enter' });
    expect(shortcutKeys('Submit', true)).toEqual({ text: 'Cmd+Enter', aria: 'Meta+Enter' });
    expect(shortcutKeys('Leave the code editor', false)).toEqual({ text: 'Esc then Tab', aria: '' });
    expect(() => shortcutKeys('No such row', false)).toThrow();
  });
});
