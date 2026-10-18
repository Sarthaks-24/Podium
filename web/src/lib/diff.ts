/**
 * The sample diff shown when a run gets a wrong answer on a sample: the
 * first line where the program's output and the expected output differ,
 * with whitespace made visible so a stray space or a missing line can be
 * seen. Line endings are normalised (\r\n reads as \n) and one final
 * newline is ignored; everything else counts.
 */

/** The first difference between two outputs. A missing line is null. */
export interface Difference {
  /** Line number, counting from 1. */
  line: number;
  expected: string | null;
  actual: string | null;
}

function lines(text: string): string[] {
  const normal = text.replace(/\r\n?/g, '\n');
  const trimmed = normal.endsWith('\n') ? normal.slice(0, -1) : normal;
  return trimmed === '' ? [] : trimmed.split('\n');
}

/** Finds the first differing line, or null when the outputs match. */
export function firstDifference(expected: string, actual: string): Difference | null {
  const e = lines(expected);
  const a = lines(actual);
  const n = Math.max(e.length, a.length);
  for (let i = 0; i < n; i++) {
    const ev = i < e.length ? e[i] : null;
    const av = i < a.length ? a[i] : null;
    if (ev !== av) return { line: i + 1, expected: ev, actual: av };
  }
  return null;
}

/** Marks spaces with a middle dot and tabs with an arrow to a bar. */
export function showWhitespace(text: string): string {
  return text.replace(/ /g, '·').replace(/\t/g, '⇥');
}
