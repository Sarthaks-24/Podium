/**
 * Keyboard shortcuts: the list the "Keyboard shortcuts" dialog shows, and
 * the small rules the global keys follow (PLAN.md section 12, design.md
 * section 6).
 *
 * - "?" opens the list from anywhere, unless the person is typing.
 * - "[" and "]" open the previous and next problem in a contest, also only
 *   when the person is not typing: both are common characters in code.
 * - The rest (Ctrl+Enter, Ctrl+', Ctrl+S, the reveal keys) are bound where
 *   they work; this file only describes them, so the list and the
 *   behaviour are kept side by side in the tests.
 */

/** True on macOS and iOS, where Cmd takes the place of Ctrl. */
export function isMacPlatform(nav: { platform?: string; userAgent?: string } | undefined = globalThis.navigator): boolean {
  if (!nav) return false;
  return /Mac|iPhone|iPad/.test(nav.platform || nav.userAgent || '');
}

const TEXT_INPUTS = new Set(['text', 'search', 'email', 'url', 'tel', 'password', 'number', 'date', 'datetime-local', 'time', 'month', 'week']);

/**
 * True when a key press on `target` is typing: a text field, a textarea, a
 * select or anything editable (the code editor is contenteditable). Single
 * character shortcuts never fire there.
 */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!target || typeof (target as Element).closest !== 'function') return false;
  const el = target as HTMLElement;
  if (el.isContentEditable || el.closest('[contenteditable=""], [contenteditable="true"]')) return true;
  const tag = el.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') return TEXT_INPUTS.has(((el as HTMLInputElement).type || 'text').toLowerCase());
  return false;
}

/** True for a plain key press with no Ctrl, Cmd or Alt held (Shift is allowed, "?" needs it). */
export function isPlainKey(event: Pick<KeyboardEvent, 'ctrlKey' | 'metaKey' | 'altKey'>): boolean {
  return !event.ctrlKey && !event.metaKey && !event.altKey;
}

/**
 * The problem label one step before (delta -1) or after (delta 1) the
 * current one, or null at either end. The list does not wrap, so holding
 * "]" stops at the last problem.
 */
export function adjacentLabel(labels: readonly string[], current: string, delta: -1 | 1): string | null {
  const i = labels.indexOf(current);
  if (i < 0) return null;
  return labels[i + delta] ?? null;
}

/** One line of the list: the keys, each a separate <kbd>, and what they do. */
export interface Shortcut {
  keys: string[];
  /** Alternatives such as "[" or "]" are separate entries in `keys`, joined by `joiner`. */
  joiner?: 'then' | 'or';
  action: string;
}

export interface ShortcutGroup {
  title: string;
  items: Shortcut[];
}

/** The group of editor keys the "Not available" rows belong to; the key test skips it. */
export const NOT_AVAILABLE = 'Not available, and why';

/**
 * The shortcuts shown in the Help dialog: the contract of
 * cache/v3/design/editor.md section 3. Every row outside "Anywhere",
 * "Solving a problem" and "Not available" is pressed by
 * web/tests/editor-keys.spec.ts, which fails on a row it has no check for.
 * Organisers also see the problem editor and the reveal; students never
 * see admin screens, so they do not see those keys either.
 */
export function shortcutGroups(opts: { mac: boolean; organiser: boolean }): ShortcutGroup[] {
  const mod = opts.mac ? 'Cmd' : 'Ctrl';
  const alt = opts.mac ? 'Option' : 'Alt';
  const groups: ShortcutGroup[] = [
    {
      title: 'Anywhere',
      items: [
        { keys: ['?'], action: 'Show this help' },
        { keys: ['Esc'], action: 'Close a dialog or the menu' },
      ],
    },
    {
      title: 'Solving a problem',
      items: [
        { keys: [`${mod}+Enter`], action: 'Submit' },
        { keys: [`${mod}+'`], action: 'Run on the examples, or on your input when that box has focus' },
        { keys: [`${mod}+S`], action: 'Save your code now (it also saves every 10 seconds)' },
        { keys: ['Esc', 'Tab'], joiner: 'then', action: 'Leave the code editor' },
        { keys: ['[', ']'], joiner: 'or', action: 'Open the previous or next problem in a contest, when you are not typing' },
        { keys: ['Left arrow', 'Right arrow'], joiner: 'or', action: 'Switch between the Problem, Code and Result tabs on a small screen' },
      ],
    },
    {
      title: 'Editing',
      items: [
        { keys: [`${mod}+Z`], action: 'Undo' },
        { keys: [`${mod}+Y`, `${mod}+Shift+Z`], joiner: 'or', action: 'Redo' },
        { keys: [`${mod}+X`], action: 'Cut the selection, or the whole line when nothing is selected' },
        { keys: [`${mod}+/`], action: 'Comment or uncomment the line or selection' },
        { keys: [`Shift+${alt}+A`], action: 'Block comment' },
        { keys: ['Tab', 'Shift+Tab'], joiner: 'or', action: 'Indent or outdent the line or selection' },
        { keys: [`${mod}+]`, `${mod}+[`], joiner: 'or', action: 'Indent or outdent' },
        { keys: [`${alt}+Up`, `${alt}+Down`], joiner: 'or', action: 'Move the line up or down' },
        { keys: [`Shift+${alt}+Up`, `Shift+${alt}+Down`], joiner: 'or', action: 'Copy the line up or down' },
        { keys: [`${mod}+Shift+K`], action: 'Delete the line' },
        { keys: [`${mod}+Shift+Enter`], action: 'Insert a line above' },
        { keys: [opts.mac ? `${alt}+Backspace` : `${mod}+Backspace`], action: 'Delete the word before the cursor' },
        { keys: [`${mod}+Shift+\\`], action: 'Jump to the matching bracket' },
        { keys: ['Ctrl+Space'], action: 'Open completion' },
        { keys: ['Enter', 'Tab'], joiner: 'or', action: 'Accept the completion' },
        { keys: opts.mac ? [`${mod}+${alt}+[`, `${mod}+${alt}+]`] : [`${mod}+Shift+[`, `${mod}+Shift+]`], joiner: 'or', action: 'Fold or unfold the block' },
        { keys: [`${mod}+K`, `${mod}+0`], joiner: 'then', action: 'Fold all' },
        { keys: [`${mod}+K`, `${mod}+J`], joiner: 'then', action: 'Unfold all' },
        { keys: ['(', '[', '{', '"'], joiner: 'or', action: 'Type the closing bracket or quote too' },
      ],
    },
    {
      title: 'Selection and cursors',
      items: [
        { keys: [`${mod}+A`], action: 'Select all' },
        { keys: [`${mod}+L`], action: 'Select the line' },
        { keys: [`${mod}+D`], action: 'Select the word, then the next match on each press' },
        { keys: [`${mod}+Shift+L`], action: 'Select every match of the selection' },
        { keys: [`${mod}+U`], action: 'Undo the last cursor move' },
        { keys: [`${alt}+Click`], action: 'Add a cursor' },
        { keys: [`${mod}+${alt}+Up`, `${mod}+${alt}+Down`], joiner: 'or', action: 'Add a cursor above or below' },
        { keys: [`Shift+${alt}+drag`], action: 'Select a box of text' },
        { keys: [`Shift+${alt}+Right`], action: 'Grow the selection to the enclosing block' },
        { keys: [`Shift+${alt}+Left`], action: 'Shrink the selection back' },
        { keys: [`${mod}+Shift+Home`, `${mod}+Shift+End`], joiner: 'or', action: 'Select to the start or the end of the file' },
      ],
    },
    {
      title: 'Moving around',
      items: [
        { keys: ['Ctrl+G'], action: 'Go to a line' },
        { keys: [`${mod}+Home`, `${mod}+End`], joiner: 'or', action: 'Go to the start or the end of the file' },
        { keys: ['Home'], action: 'Go to the first character of the line, then column 1' },
        { keys: [opts.mac ? `${alt}+Right` : `${mod}+Right`], action: 'Go to the next word' },
        ...(opts.mac ? [] : [{ keys: [`${mod}+Up`, `${mod}+Down`], joiner: 'or' as const, action: 'Scroll a line without moving the cursor' }]),
        { keys: [`${alt}+Page Up`, `${alt}+Page Down`], joiner: 'or', action: 'Scroll a page without moving the cursor' },
      ],
    },
    {
      title: 'Find and replace',
      items: [
        { keys: [`${mod}+F`], action: 'Find' },
        { keys: [`${mod}+H`], action: 'Replace' },
        { keys: ['F3', 'Shift+F3'], joiner: 'or', action: 'Next or previous match' },
        { keys: ['Enter', 'Shift+Enter'], joiner: 'or', action: 'Next or previous match, in the find box' },
        { keys: [`${mod}+${alt}+Enter`], action: 'Replace all, in the replace box' },
        { keys: [`${alt}+C`, `${alt}+W`, `${alt}+R`], joiner: 'or', action: 'Match case, whole word or regular expression, in the find box' },
        { keys: ['Esc'], action: 'Close find' },
      ],
    },
    {
      title: NOT_AVAILABLE,
      items: [
        { keys: [`${mod}+Shift+P`, 'F1'], joiner: 'or', action: 'Command palette: there is none, the actions are the buttons and these keys' },
        { keys: [`${mod}+P`], action: 'Quick open: the browser prints' },
        { keys: [`${mod}+W`, `${mod}+T`, `${mod}+N`, `${mod}+Tab`], joiner: 'or', action: 'Close, open or switch tabs: the browser owns these keys' },
        { keys: ['F5', `${mod}+R`], joiner: 'or', action: 'The browser reloads the page; your code is kept' },
        { keys: ['F11'], action: 'Full screen: the browser owns it' },
        { keys: [`${mod}+Shift+O`, 'F12'], joiner: 'or', action: 'Go to a symbol or a definition: there is no language server' },
        { keys: [`${mod}+Shift+K`], action: 'In Firefox, Delete line opens the console instead; use Shift+Home then Delete' },
      ],
    },
  ];
  if (opts.organiser) {
    groups.push(
      {
        title: 'Problem editor',
        items: [{ keys: [`${mod}+S`], action: 'Save changes' }],
      },
      {
        title: 'Reveal',
        items: [
          { keys: ['Space', 'Right arrow'], joiner: 'or', action: 'Resolve the next row' },
          { keys: ['Left arrow'], action: 'Go back one row' },
          { keys: ['Esc'], action: 'Leave the reveal' },
        ],
      },
    );
  }
  return groups;
}
