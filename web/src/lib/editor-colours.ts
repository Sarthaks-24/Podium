/**
 * The editor colour scheme: its own setting, separate from the interface
 * theme, because a person may want a light page with a dark well or the
 * other way round. Five schemes, each a block of tokens in styles/tokens.css
 * (data-editor on <html>); Night is today's look and the default, with no
 * block of its own. The account holds the choice (editor_colours) and a
 * localStorage copy lets public/theme-init.js paint the first frame.
 */

/** A scheme the picker offers. */
export interface EditorScheme {
  id: string;
  label: string;
  hint: string;
}

/** The five schemes, in picker order. The server checks ids against the same list. */
export const EDITOR_SCHEMES: readonly EditorScheme[] = [
  { id: 'night', label: 'Night', hint: 'Dark, the default' },
  { id: 'paper', label: 'Paper', hint: 'Light' },
  { id: 'midnight', label: 'Midnight', hint: 'Black' },
  { id: 'dusk', label: 'Dusk', hint: 'Warm dark' },
  { id: 'high-contrast', label: 'High contrast', hint: 'Every colour at 7:1 or more' },
];

export const DEFAULT_EDITOR_COLOURS = 'night';

/** localStorage key, also read by public/theme-init.js. */
export const EDITOR_COLOURS_KEY = 'podium-editor-colours';

const IDS = new Set(EDITOR_SCHEMES.map((s) => s.id));

/** Turns any value into a scheme id, defaulting to Night. */
export function parseEditorColours(value: unknown): string {
  return typeof value === 'string' && IDS.has(value) ? value : DEFAULT_EDITOR_COLOURS;
}

/** The scheme's name for the interface. */
export function editorLabel(id: string): string {
  return EDITOR_SCHEMES.find((s) => s.id === id)?.label ?? 'Night';
}

/** A few lines of code for the picker, as [kind, text] runs; kind is a --code-* name or "" for plain ink. */
export const SAMPLE: readonly (readonly (readonly [string, string])[])[] = [
  [['comment', '// sum of a list']],
  [['keyword', 'int'], ['', ' '], ['function', 'total'], ['', '(vector<'], ['type', 'int'], ['', '>& a) {']],
  [['', '  '], ['keyword', 'int'], ['', ' s = '], ['number', '0'], ['', ';']],
  [['', '  '], ['keyword', 'return'], ['', ' s + '], ['string', '"ok"'], ['', ';']],
];
