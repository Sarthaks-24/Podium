/**
 * Finds source locations in compiler output, so the verdict panel can turn
 * each one into a button that moves the editor's cursor there.
 *
 * Recognised shapes, one per compiler the judge runs:
 * - g++:    "main.cpp:12:5: error: ..."
 * - javac:  "Main.java:12: error: ..."
 * - Python: 'File "main.py", line 12' (frames inside the standard library
 *           are skipped, because they are not the student's code)
 * - Node:   "/box/main.js:12" as printed by node --check
 *
 * The output itself is never changed: segments() splits it into plain text
 * and location parts that together spell the original exactly.
 */

/** A place in the student's source; lines and columns count from 1. */
export interface Location {
  line: number;
  col: number | null;
}

/** Part of the output: plain text, or a location with the text that named it. */
export type Segment = string | { text: string; line: number; col: number | null };

interface Pattern {
  re: RegExp;
  /** Builds the location from a match, or null to leave the match as text. */
  read: (m: RegExpExecArray) => Location | null;
}

const num = (s: string | undefined): number | null => (s === undefined ? null : Number(s));

const CPP: Pattern = {
  // Headers such as /usr/include/c++/12/bits/stl_vector.h are not the
  // student's file, so only source extensions count.
  re: /[\w./-]*\.(?:cpp|cc|cxx):(\d+)(?::(\d+))?/g,
  read: (m) => ({ line: Number(m[1]), col: num(m[2]) }),
};

const JAVA: Pattern = {
  re: /[\w./-]*\.java:(\d+)(?::(\d+))?/g,
  read: (m) => ({ line: Number(m[1]), col: num(m[2]) }),
};

const PYTHON: Pattern = {
  re: /File "([^"]*)", line (\d+)/g,
  read: (m) => (/^<|\/usr\/|\/lib\/python|site-packages/.test(m[1]) ? null : { line: Number(m[2]), col: null }),
};

// SyntaxError summaries that name a line without a File frame, such as
// "IndentationError: unexpected indent (main.py, line 12)".
const PYTHON_BARE: Pattern = {
  re: /\(\w+\.py, line (\d+)\)/g,
  read: (m) => ({ line: Number(m[1]), col: null }),
};

const NODE: Pattern = {
  re: /[\w./-]*\.(?:js|mjs|cjs):(\d+)(?::(\d+))?/g,
  read: (m) => ({ line: Number(m[1]), col: num(m[2]) }),
};

const BY_LANGUAGE: Record<string, Pattern[]> = {
  cpp17: [CPP],
  java21: [JAVA],
  python3: [PYTHON, PYTHON_BARE],
  node22: [NODE],
};

const ALL = [CPP, JAVA, PYTHON, PYTHON_BARE, NODE];

/**
 * Splits compiler output into text and locations. With a language, only
 * that compiler's shapes are looked for; without one, all of them.
 */
export function segments(output: string, language?: string): Segment[] {
  const patterns = (language && BY_LANGUAGE[language]) || ALL;
  const found: { start: number; end: number; loc: Location }[] = [];
  for (const p of patterns) {
    const re = new RegExp(p.re.source, p.re.flags);
    let m: RegExpExecArray | null;
    while ((m = re.exec(output)) !== null) {
      const loc = p.read(m);
      if (loc && loc.line > 0) found.push({ start: m.index, end: m.index + m[0].length, loc });
    }
  }
  found.sort((a, b) => a.start - b.start);

  const out: Segment[] = [];
  let at = 0;
  for (const f of found) {
    if (f.start < at) continue; // overlapping match from another pattern
    if (f.start > at) out.push(output.slice(at, f.start));
    out.push({ text: output.slice(f.start, f.end), line: f.loc.line, col: f.loc.col });
    at = f.end;
  }
  if (at < output.length) out.push(output.slice(at));
  return out;
}

/** The locations in compiler output, in order, as segments() finds them. */
export function locations(output: string, language?: string): Location[] {
  return segments(output, language)
    .filter((s): s is Exclude<Segment, string> => typeof s !== 'string')
    .map((s) => ({ line: s.line, col: s.col }));
}
