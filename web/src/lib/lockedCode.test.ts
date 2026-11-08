// The editor's join of locked code reads the same fixture as the Go test on
// problems.Compose (V202-34), so the two cannot drift.
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { LOCKED_HINT, compose, layoutDoc, lockedLabel, normalise, pointsIntoLocked, stripLocked } from './lockedCode';

interface Case {
  name: string;
  top: string;
  code: string;
  bottom: string;
  composed: string;
  top_lines: number;
  code_lines: number;
}
const cases: Case[] = JSON.parse(readFileSync(new URL('../../tests/fixtures/compose.json', import.meta.url), 'utf8')).cases;

describe('the shared compose fixture', () => {
  it('has the cases the Go test reads', () => {
    expect(cases.length).toBeGreaterThanOrEqual(4);
  });

  for (const c of cases) {
    it(`compose gives the judged file: ${c.name}`, () => {
      const got = compose(c.top, c.code, c.bottom);
      expect(got).toEqual({ source: c.composed, topLines: c.top_lines, codeLines: c.code_lines });
    });

    it(`the editor's document composes to the same file: ${c.name}`, () => {
      const l = layoutDoc(c.top, c.code, c.bottom);
      const region = l.doc.slice(l.topEnd, l.bottomStart < 0 ? undefined : l.bottomStart);
      expect(compose(c.top, region, c.bottom).source).toBe(c.composed);
      // The locked parts sit where the offsets say.
      expect(l.doc.slice(0, l.topEnd)).toBe(normalise(c.top));
      if (l.bottomStart >= 0) expect(l.doc.slice(l.bottomStart)).toBe(`\n${normalise(c.bottom).slice(0, -1)}`);
      // The student's first line is line top_lines + 1 of the document.
      expect(l.doc.slice(0, l.topEnd).split('\n').length - 1).toBe(c.top_lines);
    });
  }
});

describe('stripLocked', () => {
  const top = 'import sys\n';
  const bottom = 'print(f(1))\n';
  const body = 'def f(n):\n    return n\n';
  it('cuts an exact top and bottom', () => {
    expect(stripLocked(top + body + bottom, top, bottom)).toEqual({ text: body, top: true, bottom: true });
  });
  it('cuts a bottom with no newline at its end', () => {
    expect(stripLocked(top + body + 'print(f(1))', top, bottom).text).toBe(body);
  });
  it('leaves a near miss and text inside a line alone', () => {
    expect(stripLocked('import os\n' + body, top, bottom)).toEqual({ text: 'import os\n' + body, top: false, bottom: false });
    expect(stripLocked(body + 'x print(f(1))\n', top, bottom).bottom).toBe(false);
  });
  it('does nothing without locked parts', () => {
    expect(stripLocked(body, '', '')).toEqual({ text: body, top: false, bottom: false });
  });
});

describe('hints', () => {
  it('names the locked block and the line', () => {
    expect(lockedLabel(1)).toBe('Locked code, 1 line');
    expect(lockedLabel(5)).toBe('Locked code, 5 lines');
  });
  it('finds a compile error inside locked code', () => {
    expect(pointsIntoLocked("locked code, line 4: error: 'twice' was not declared")).toBe(true);
    expect(pointsIntoLocked('main.cpp:3:1: error: x')).toBe(false);
    expect(LOCKED_HINT).toBe('Your code no longer matches the function the locked code calls. Keep the name and parameters from the starter.');
  });
});
