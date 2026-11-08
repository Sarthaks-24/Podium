/**
 * Locked code for function problems (v2.0.2): how the editor joins the
 * locked top, the student's code and the locked bottom into one document,
 * and how a pasted whole file is cut back to the student's part.
 *
 * `compose` is the same join the server does (problems.Compose in Go);
 * web/tests/fixtures/compose.json is read by both tests, so the two cannot
 * drift (V202-34). `layoutDoc` is what the editor shows: the same text, but
 * the student's part keeps its own ending and the bottom has no final
 * newline, so the document does not end in an empty line. This module has no
 * CodeMirror import, so the problem editor's preview can use it without
 * pulling the editor into the main bundle.
 */

/** The locked parts of one language, as the workspace payload carries them. */
export interface LockedParts {
  top: string;
  bottom: string;
}

/** Cuts a part after its last line and ends it with one newline; empty stays empty. */
export function normalise(s: string): string {
  const t = s.replace(/[\r\n]+$/, '');
  return t === '' ? '' : `${t}\n`;
}

const lines = (s: string) => (s.match(/\n/g) ?? []).length;

/** The file the judge compiles, with the line offsets of the parts (Go: problems.Compose). */
export function compose(top: string, code: string, bottom: string): { source: string; topLines: number; codeLines: number } {
  const t = normalise(top);
  const c = normalise(code);
  return { source: t + c + normalise(bottom), topLines: lines(t), codeLines: lines(c) };
}

/**
 * The editor's document. `topEnd` is where the student's part starts (the
 * top, with its newline, is locked before it); `bottomStart` is where the
 * locked bottom starts, at the newline that ends the student's last line,
 * or -1 when the problem has no bottom. The student's part is the text
 * between them and has no newline at its end when a bottom follows.
 */
export interface Layout {
  doc: string;
  topEnd: number;
  bottomStart: number;
}

export function layoutDoc(top: string, code: string, bottom: string): Layout {
  const t = normalise(top);
  const b = normalise(bottom);
  if (b === '') return { doc: t + code, topEnd: t.length, bottomStart: -1 };
  const c = code.replace(/[\r\n]+$/, '');
  return { doc: `${t}${c}\n${b.slice(0, -1)}`, topEnd: t.length, bottomStart: t.length + c.length };
}

/** True when offset `i` of `s` is the start of a line. */
const lineStart = (s: string, i: number) => i === 0 || s[i - 1] === '\n';

/**
 * Cuts the exact locked top off the start of `text` and the exact locked
 * bottom off its end, each on whole lines; anything else is left as written
 * (the Go side does the same, problems.StripLocked). `top` and `bottom` are
 * the locked parts as normalised text.
 */
export function stripLocked(text: string, top: string, bottom: string): { text: string; top: boolean; bottom: boolean } {
  let out = text;
  let cutTop = false;
  let cutBottom = false;
  if (top !== '' && out.startsWith(top)) {
    out = out.slice(top.length);
    cutTop = true;
  }
  if (bottom !== '') {
    const bare = bottom.slice(0, -1);
    for (const tail of [bottom, bare]) {
      if (out.endsWith(tail) && lineStart(out, out.length - tail.length)) {
        out = out.slice(0, out.length - tail.length);
        cutBottom = true;
        break;
      }
    }
  }
  return { text: out, top: cutTop, bottom: cutBottom };
}

/** The "Locked code, N lines" label of a fold. */
export function lockedLabel(lineCount: number): string {
  return `Locked code, ${lineCount} ${lineCount === 1 ? 'line' : 'lines'}`;
}

/** The hint under a compile error that points into locked code (PLAN 3.8). */
export const LOCKED_HINT = 'Your code no longer matches the function the locked code calls. Keep the name and parameters from the starter.';

/** True when compiler output, as the server rewrote it, names a line of locked code. */
export function pointsIntoLocked(output: string): boolean {
  return /locked code, line \d+/.test(output);
}

/** The line "Common imports are already added" hint shown with a function problem. */
export const IMPORTS_HINT = 'Common imports are already added. Add more if you need them.';
