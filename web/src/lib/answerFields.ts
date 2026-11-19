// The fields view of an AI answer (v2.1.1 stage C). The answer is parsed JSON;
// this walks it into labelled nodes for the review screen, changes one value
// at a path without touching the rest, and parses and prints the JSON view.
// The view only edits values: it never adds or removes a key, because the
// server's decoder refuses an answer whose keys are not the schema's.

export type Path = readonly (string | number)[];

export type FieldNode =
  | { kind: 'text' | 'number' | 'bool'; path: Path; label: string; value: string | number | boolean }
  | { kind: 'null'; path: Path; label: string }
  | { kind: 'group'; path: Path; label: string; children: FieldNode[] };

/** "locked_top" becomes "Locked top". */
export function labelOf(key: string): string {
  const words = key.replace(/_/g, ' ').trim();
  return words === '' ? key : words[0].toUpperCase() + words.slice(1);
}

/** The label of one list item: "Item 2", with the item's language or title when it has one. */
function itemLabel(index: number, value: unknown): string {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    const o = value as Record<string, unknown>;
    for (const k of ['language', 'title']) {
      if (typeof o[k] === 'string' && o[k] !== '') return `Item ${index + 1}, ${o[k]}`;
    }
  }
  return `Item ${index + 1}`;
}

function node(value: unknown, path: Path, label: string): FieldNode {
  if (typeof value === 'string') return { kind: 'text', path, label, value };
  if (typeof value === 'number') return { kind: 'number', path, label, value };
  if (typeof value === 'boolean') return { kind: 'bool', path, label, value };
  if (Array.isArray(value)) return { kind: 'group', path, label, children: value.map((v, i) => node(v, [...path, i], itemLabel(i, v))) };
  if (value !== null && typeof value === 'object') {
    return { kind: 'group', path, label, children: Object.entries(value).map(([k, v]) => node(v, [...path, k], labelOf(k))) };
  }
  return { kind: 'null', path, label };
}

/** The fields of an answer: its top-level keys, each a field or a group. A value that is not an object has no fields. */
export function fieldTree(value: unknown): FieldNode[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return [];
  return Object.entries(value).map(([k, v]) => node(v, [k], labelOf(k)));
}

/** A copy of value with the item at path replaced, sharing everything else. */
export function setAt(value: unknown, path: Path, next: unknown): unknown {
  if (path.length === 0) return next;
  const [head, ...rest] = path;
  if (Array.isArray(value)) {
    const copy = value.slice();
    copy[head as number] = setAt(value[head as number], rest, next);
    return copy;
  }
  const o = value as Record<string, unknown>;
  return { ...o, [head]: setAt(o[head as string], rest, next) };
}

export type Parsed = { ok: true; value: unknown } | { ok: false; error: string };

/** Parses the JSON view's text. The error is the parser's sentence, which names the place. */
export function parseAnswer(text: string): Parsed {
  try {
    return { ok: true, value: JSON.parse(text) };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'The text is not valid JSON.' };
  }
}

/** The JSON view of an answer. */
export function showAnswer(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

/** The text Create draft sends for an edited answer: compact, so the 2 MB limit counts the least. */
export function sendableAnswer(value: unknown): string {
  return JSON.stringify(value);
}

/** The id of a field's input, from its path. */
export function fieldId(path: Path): string {
  return 'f-' + path.join('-').replace(/[^A-Za-z0-9_-]/g, '_');
}
