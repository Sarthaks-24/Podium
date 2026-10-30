import { describe, expect, it } from 'vitest';
import { compileMarks, firstError, locations, segments } from './compileErrors';

const GPP = `main.cpp: In function 'int main()':
main.cpp:12:5: error: expected ';' before 'return'
   12 |     return 0;
      |     ^~~~~~
In file included from /usr/include/c++/12/vector:66,
                 from main.cpp:2:
/usr/include/c++/12/bits/stl_vector.h:1123:7: note: candidate`;

const JAVAC = `Main.java:12: error: ';' expected
        int a = 1
                 ^
Main.java:20: error: cannot find symbol
2 errors`;

const PYTHON = `Traceback (most recent call last):
  File "/usr/lib/python3.12/py_compile.py", line 144, in compile
  File "main.py", line 12
    print(2.5
         ^
SyntaxError: '(' was never closed`;

const PYTHON_BARE = 'Sorry: IndentationError: unexpected indent (main.py, line 7)';

const NODE = `/box/main.js:12
const x = ;
          ^

SyntaxError: Unexpected token ';'`;

describe('compile error locations', () => {
  it('reads g++ file:line:col and skips system headers', () => {
    expect(locations(GPP, 'cpp17')).toEqual([
      { line: 12, col: 5 },
      { line: 2, col: null },
    ]);
  });

  it('reads javac file:line', () => {
    expect(locations(JAVAC, 'java21')).toEqual([
      { line: 12, col: null },
      { line: 20, col: null },
    ]);
  });

  it('reads Python line numbers in the student file only', () => {
    expect(locations(PYTHON, 'python3')).toEqual([{ line: 12, col: null }]);
    expect(locations(PYTHON_BARE, 'python3')).toEqual([{ line: 7, col: null }]);
  });

  it('reads node file:line', () => {
    expect(locations(NODE, 'node22')).toEqual([{ line: 12, col: null }]);
  });

  it('keeps the output exactly when split into segments', () => {
    for (const [text, lang] of [
      [GPP, 'cpp17'],
      [JAVAC, 'java21'],
      [PYTHON, 'python3'],
      [NODE, 'node22'],
    ] as const) {
      const joined = segments(text, lang)
        .map((s) => (typeof s === 'string' ? s : s.text))
        .join('');
      expect(joined).toBe(text);
    }
  });

  it('names the matched text so the button shows what the compiler said', () => {
    const parts = segments('main.cpp:3:9: error: x', 'cpp17');
    expect(parts[0]).toEqual({ text: 'main.cpp:3:9', line: 3, col: 9 });
    expect(parts[1]).toBe(': error: x');
  });

  it('finds every shape when the language is not known', () => {
    expect(locations(`${JAVAC}\n${NODE}`)).toEqual([
      { line: 12, col: null },
      { line: 20, col: null },
      { line: 12, col: null },
    ]);
  });

  it('parses a 16 KB identifier in linear time, for every language', () => {
    const word = 'a'.repeat(16 * 1024);
    const nasty = [
      `main.cpp:3:1: error: '${word}' was not declared in this scope`,
      `${word}.${word}/${word}`,
      `File "${word}`,
      `(${word}`,
      `#error ${word}.cpp${word}`,
    ].join('\n');
    for (const lang of ['cpp17', 'java21', 'python3', 'node22', undefined]) {
      const t0 = performance.now();
      const parts = segments(nasty, lang);
      const ms = performance.now() - t0;
      expect(ms, `${lang ?? 'any language'} took ${ms.toFixed(1)} ms`).toBeLessThan(50);
      expect(parts.map((s) => (typeof s === 'string' ? s : s.text)).join('')).toBe(nasty);
    }
    expect(locations(nasty, 'cpp17')).toEqual([{ line: 3, col: 1 }]);
  });

  it('only looks at the start of very long lines', () => {
    const late = `${' '.repeat(2500)}main.cpp:4:2: error`;
    expect(locations(late, 'cpp17')).toEqual([]);
    expect(locations(`main.cpp:4:2: error ${'x'.repeat(5000)}`, 'cpp17')).toEqual([{ line: 4, col: 2 }]);
  });

  it('returns plain text when nothing matches', () => {
    expect(segments('Killed', 'cpp17')).toEqual(['Killed']);
    expect(segments('', 'cpp17')).toEqual([]);
  });
});

describe('the first error, shown above the output', () => {
  it('takes the place and the words from one g++ or javac line', () => {
    expect(firstError(GPP, 'cpp17')).toEqual({ line: 12, col: 5, text: "expected ';' before 'return'" });
    expect(firstError(JAVAC, 'java21')).toEqual({ line: 12, col: null, text: "';' expected" });
  });

  it('pairs the place with the later message for Python and Node', () => {
    expect(firstError(PYTHON, 'python3')).toEqual({ line: 12, col: null, text: "SyntaxError: '(' was never closed" });
    expect(firstError(NODE, 'node22')).toEqual({ line: 12, col: null, text: "SyntaxError: Unexpected token ';'" });
  });

  it('gives nothing when no place in the code is named', () => {
    expect(firstError('Killed', 'cpp17')).toBeNull();
  });
});

describe('the marks in the editor (v4 E4)', () => {
  it('marks each g++ or javac line that says error, warning or note, and skips an include line', () => {
    expect(compileMarks(GPP, 'cpp17')).toEqual([{ line: 12, col: 5, text: "expected ';' before 'return'", severity: 'error' }]);
    expect(compileMarks(JAVAC, 'java21')).toEqual([
      { line: 12, col: null, text: "';' expected", severity: 'error' },
      { line: 20, col: null, text: 'cannot find symbol', severity: 'error' },
    ]);
    expect(compileMarks("main.cpp:3:9: warning: unused variable 'x'\nmain.cpp:4:1: note: here", 'cpp17').map((m) => m.severity)).toEqual(['warning', 'info']);
  });

  it('marks the first error alone for Python and Node', () => {
    expect(compileMarks(PYTHON, 'python3')).toEqual([{ line: 12, col: null, text: "SyntaxError: '(' was never closed", severity: 'error' }]);
    expect(compileMarks(NODE, 'node22')).toEqual([{ line: 12, col: null, text: "SyntaxError: Unexpected token ';'", severity: 'error' }]);
    expect(compileMarks('Killed', 'cpp17')).toEqual([]);
  });
});
