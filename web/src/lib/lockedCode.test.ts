// The editor's join of locked code reads the same fixture as the Go test on
// problems.Compose (V202-34), so the two cannot drift.
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { LOCKED_HINT, choiceHint, compose, layoutDoc, lockedLabel, normalise, pointsIntoLocked, stripLocked } from './lockedCode';

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

interface StripCase {
  name: string;
  top: string;
  bottom: string;
  input: string;
  want: string;
}
const stripCases: StripCase[] = JSON.parse(readFileSync(new URL('../../tests/fixtures/strip.json', import.meta.url), 'utf8')).cases;

describe('stripLocked reads the shared strip fixture', () => {
  for (const c of stripCases) {
    it(c.name, () => {
      expect(stripLocked(c.input, normalise(c.top), normalise(c.bottom)).text).toBe(c.want);
    });
  }
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

describe('the list choice hint', () => {
  const javac = 'Main.java:2: error: method does not override or implement a method from a supertype\n    @Override\n    ^\n';
  it('names the pair when javac says the override is wrong', () => {
    expect(choiceHint(javac, ['int[]', 'ArrayList<Integer>'])).toBe('The function must keep its name and take int[] or ArrayList<Integer>.');
    expect(choiceHint(javac, ['long[]', 'ArrayList<Long>'])).toBe('The function must keep its name and take long[] or ArrayList<Long>.');
  });
  it('stays quiet without a choice or for other errors', () => {
    expect(choiceHint(javac, null)).toBe('');
    expect(choiceHint('Main.java:3: error: cannot find symbol\n', ['int[]', 'ArrayList<Integer>'])).toBe('');
    expect(choiceHint('', ['int[]', 'ArrayList<Integer>'])).toBe('');
  });
  it('does not replace the locked code hint', () => {
    expect(pointsIntoLocked(javac)).toBe(false);
  });
});
