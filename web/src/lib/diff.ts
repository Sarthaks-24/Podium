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

/** One line of a code comparison: kept in both, only in `a` (-), or only in `b` (+). */
export interface DiffLine {
  kind: ' ' | '-' | '+';
  text: string;
}

/** Above this many line pairs left after the shared start and end, lineDiff gives up. */
export const DIFF_LIMIT = 1_000_000;

/**
 * A line diff of two programs by longest common subsequence, after taking
 * off the lines they share at the start and the end. Null when what is left
 * is too big to compare in the browser (more than DIFF_LIMIT line pairs).
 */
export function lineDiff(a: string, b: string): DiffLine[] | null {
  const x = lines(a);
  const y = lines(b);
  let start = 0;
  while (start < x.length && start < y.length && x[start] === y[start]) start++;
  let end = 0;
  while (end < x.length - start && end < y.length - start && x[x.length - 1 - end] === y[y.length - 1 - end]) end++;
  const xs = x.slice(start, x.length - end);
  const ys = y.slice(start, y.length - end);
  const n = xs.length;
  const m = ys.length;
  if (n * m > DIFF_LIMIT) return null;
  // len[i * w + j]: the longest common subsequence of xs[i..] and ys[j..].
  const w = m + 1;
  const len = new Uint32Array((n + 1) * w);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      len[i * w + j] = xs[i] === ys[j] ? len[(i + 1) * w + j + 1] + 1 : Math.max(len[(i + 1) * w + j], len[i * w + j + 1]);
    }
  }
  const out: DiffLine[] = x.slice(0, start).map((text) => ({ kind: ' ', text }));
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && xs[i] === ys[j]) {
      out.push({ kind: ' ', text: xs[i] });
      i++;
      j++;
    } else if (j >= m || (i < n && len[(i + 1) * w + j] >= len[i * w + j + 1])) {
      out.push({ kind: '-', text: xs[i++] });
    } else {
      out.push({ kind: '+', text: ys[j++] });
    }
  }
  for (const text of x.slice(x.length - end)) out.push({ kind: ' ', text });
  return out;
}
