import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import type { Language } from './workspace';
import { isStarterOrEmpty, splitStarter, starter, starterForms, withLanguage } from './starters';

describe('a program moved to another language', () => {
  it('swaps an untouched starter or an empty source for the new starter', () => {
    expect(withLanguage({ language: 'cpp17', source: starter('cpp17') }, 'python3')).toEqual({ language: 'python3', source: starter('python3') });
    // Spaces around an untouched starter do not matter.
    expect(withLanguage({ language: 'cpp17', source: `\n${starter('cpp17')}  ` }, 'java21').source).toBe(starter('java21'));
    expect(withLanguage({ language: 'python3', source: '' }, 'node22').source).toBe(starter('node22'));
  });

  it('keeps code the setter wrote', () => {
    const mine = { language: 'cpp17' as const, source: 'int main() { return 0; }\n' };
    expect(withLanguage(mine, 'python3')).toEqual({ language: 'python3', source: mine.source });
  });
});

// The same fixture the Go test on problems.SplitStarter reads, so the two
// readers cannot drift.
interface ChoiceCase {
  name: string;
  language: Language;
  source: string;
  shared?: string;
  forms?: { name: string; code: string }[];
  error?: string;
}
const choiceCases: ChoiceCase[] = JSON.parse(readFileSync(new URL('../../tests/fixtures/choices.json', import.meta.url), 'utf8')).cases;

describe('the shared choices fixture', () => {
  it('has the cases the Go test reads', () => {
    expect(choiceCases.length).toBeGreaterThanOrEqual(15);
  });

  for (const c of choiceCases) {
    it(`starterForms gives single forms: ${c.name}`, () => {
      // An empty own starter falls back to the built-in one, so it is read directly.
      const got = c.source === '' ? splitStarter('') : starterForms(c.language, { [c.language]: c.source });
      if (c.error) {
        // A refused starting code never reaches a student; if one does, it is plain.
        expect(got).toEqual({ forms: [{ name: '', code: c.source }], default: c.source });
        return;
      }
      // The editor lists the array form first whatever the file's order (Go keeps file order).
      const forms = c
        .forms!.map((f) => ({ name: f.name, code: (c.forms!.length > 1 ? c.shared! : '') + f.code }))
        .sort((a, b) => Number(b.name.endsWith('[]')) - Number(a.name.endsWith('[]')));
      expect(got.forms).toEqual(c.forms!.length > 1 ? forms : [{ name: '', code: c.source }]);
      expect(got.default).toBe(got.forms[0].code);
    });
  }

  it('splitStarter reads the Java cases without a language', () => {
    for (const c of choiceCases.filter((x) => x.language === 'java21' && !x.error && x.forms!.length > 1)) {
      expect(splitStarter(c.source).forms.map((f) => f.name).sort()).toEqual(c.forms!.map((f) => f.name).sort());
    }
  });

  it('never leaves marker text in a form', () => {
    for (const c of choiceCases.filter((x) => x.language === 'java21' && !x.error && x.forms!.length > 1)) {
      for (const f of splitStarter(c.source).forms) expect(f.code).not.toMatch(/^\/\/ choice:/m);
    }
  });
});

describe('starters with a choice', () => {
  const two = '// choice: int[]\nclass A {}\n// choice: ArrayList<Integer>\nclass B {}\n';

  it('gives the array form first and as the default when the file lists the ArrayList first', () => {
    const listFirst = '// choice: ArrayList<Integer>\nclass B {}\n// choice: int[]\nclass A {}\n';
    expect(starter('java21', { java21: listFirst })).toBe('class A {}\n');
    expect(starterForms('java21', { java21: listFirst }).forms.map((f) => f.name)).toEqual(['int[]', 'ArrayList<Integer>']);
  });

  it('starter() returns the first form and never the marker text', () => {
    expect(starter('java21', { java21: two })).toBe('class A {}\n');
  });

  it('a problem with no marker gives its starter byte for byte', () => {
    expect(starter('java21', { java21: 'class A {}\n' })).toBe('class A {}\n');
    expect(starter('java21')).toBe(starterForms('java21').default);
  });

  it('every form counts as an untouched starter', () => {
    const own = { java21: two };
    expect(isStarterOrEmpty('class A {}\n', own)).toBe(true);
    expect(isStarterOrEmpty('class B {}', own)).toBe(true);
    expect(isStarterOrEmpty('class C {}\n', own)).toBe(false);
    expect(isStarterOrEmpty(two, own)).toBe(false);
  });

  it('a marker in another language is ordinary text', () => {
    expect(starter('cpp17', { cpp17: two })).toBe(two);
  });
});
