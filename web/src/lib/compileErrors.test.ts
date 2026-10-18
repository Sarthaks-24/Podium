import { describe, expect, it } from 'vitest';
import { locations, segments } from './compileErrors';

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

  it('returns plain text when nothing matches', () => {
    expect(segments('Killed', 'cpp17')).toEqual(['Killed']);
    expect(segments('', 'cpp17')).toEqual([]);
  });
});
