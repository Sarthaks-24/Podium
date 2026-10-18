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

/**
 * The shortcuts shown in the dialog. Organisers also see the problem editor
 * and the reveal; students never see admin screens, so they do not see
 * those keys either.
 */
export function shortcutGroups(opts: { mac: boolean; organiser: boolean }): ShortcutGroup[] {
  const mod = opts.mac ? 'Cmd' : 'Ctrl';
  const groups: ShortcutGroup[] = [
    {
      title: 'Anywhere',
      items: [
        { keys: ['?'], action: 'Show these shortcuts' },
        { keys: ['Esc'], action: 'Close a dialog or the menu' },
      ],
    },
    {
      title: 'Solving a problem',
      items: [
        { keys: [`${mod}+Enter`], action: 'Submit' },
        { keys: [`${mod}+'`], action: 'Run on samples, or on your input when the custom input box has focus' },
        { keys: [`${mod}+S`], action: 'Save the draft now (it also saves every 10 seconds)' },
        { keys: ['Esc', 'Tab'], joiner: 'then', action: 'Leave the code editor' },
        { keys: ['[', ']'], joiner: 'or', action: 'Open the previous or next problem in a contest, when you are not typing' },
        { keys: ['Left arrow', 'Right arrow'], joiner: 'or', action: 'Switch between the Problem, Code and Result tabs on a small screen' },
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
