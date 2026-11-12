/**
 * Starter code per language, put in the editor when there is no draft. A
 * problem may bring its own for a language (v4 E5, the workspace's
 * `starters`); these are used where it does not.
 * Each one reads input the fast way for its language, because a slow read
 * is the most common reason a correct idea gets "Time limit exceeded".
 */
import type { Language } from './workspace';

const STARTERS: Record<Language, string> = {
  cpp17: `#include <bits/stdc++.h>
using namespace std;

int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);

    return 0;
}
`,
  java21: `import java.io.*;
import java.util.*;

public class Main {
    public static void main(String[] args) throws IOException {
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in));
        PrintWriter out = new PrintWriter(new BufferedWriter(new OutputStreamWriter(System.out)));

        out.flush();
    }
}
`,
  python3: `import sys

input = sys.stdin.readline


def main():
    pass


main()
`,
  node22: `const data = require('fs').readFileSync(0, 'utf8').split(/\\s+/).filter(Boolean);
let pos = 0;
const next = () => data[pos++];

const out = [];

console.log(out.join('\\n'));
`,
};

/** The starter code for a language: the problem's own when it has one, and for Java its first form. */
export function starter(language: Language, own: Partial<Record<string, string>> = {}): string {
  return starterForms(language, own).default;
}

/** True when the source is empty or still one of the starters, every form of the problem's own included, untouched. */
export function isStarterOrEmpty(source: string, own: Partial<Record<string, string>> = {}): boolean {
  const s = source.trim();
  const ownForms = Object.entries(own).flatMap(([language, code]) =>
    code === undefined ? [] : language === 'java21' ? splitStarter(code).forms.map((f) => f.code) : [code],
  );
  return s === '' || [...Object.values(STARTERS), ...ownForms].some((t) => t.trim() === s);
}

/**
 * A program (a problem's reference solution, brute force or validator)
 * moved to another language: a source that is empty or still a starter
 * becomes the new language's starter, so a C++ starter never sits under
 * "Python 3"; written code stays as it is.
 */
export function withLanguage<P extends { language: Language; source: string }>(program: P, next: Language): P {
  return { ...program, language: next, source: isStarterOrEmpty(program.source) ? starter(next) : program.source };
}

/** One way to write a Java function: its marker name ('' when there is no choice) and the code a student sees. */
export interface StarterForm {
  name: string;
  code: string;
}

/** A starting code as the editor uses it: single forms only, never marker text. */
export interface StarterForms {
  forms: StarterForm[];
  default: string;
}

const MARKER_LIKE = /^\s*\/\/\s*choice\s*:/i;
const PAIRS: [string, string][] = [
  ['int[]', 'ArrayList<Integer>'],
  ['long[]', 'ArrayList<Long>'],
];

/**
 * Splits a Java starting code at its choice markers, the same rules as
 * problems.SplitStarter and the same fixture. A marker is a line that is
 * exactly `// choice: Name`; the text before the first one is shared and
 * starts every form. Each form holds shared text plus its own code and never
 * a marker line. No marker, or a code the server would have refused, gives
 * one plain form: no choice.
 */
export function splitStarter(source: string): StarterForms {
  const plain = { forms: [{ name: '', code: source }], default: source };
  let shared = '';
  const found: { name: string; code: string }[] = [];
  for (const line of source.split(/(?<=\n)/)) {
    const bare = line.replace(/\r?\n$/, '');
    if (MARKER_LIKE.test(bare)) {
      const name = bare.startsWith('// choice: ') ? bare.slice('// choice: '.length) : '';
      if (name === '' || /[ \t]/.test(name)) return plain;
      found.push({ name, code: '' });
    } else if (found.length === 0) {
      shared += line;
    } else {
      found[found.length - 1].code += line;
    }
  }
  if (found.length === 0) return plain;
  const [a, b] = found.map((f) => f.name);
  const paired = found.length === 2 && PAIRS.some(([x, y]) => (a === x && b === y) || (a === y && b === x));
  if (!paired || found.some((f) => f.code.trim() === '')) return plain;
  const forms = found.map((f) => ({ name: f.name, code: shared + f.code }));
  return { forms, default: forms[0].code };
}

/** The forms of a language's starting code: only Java is split; the problem's own starter wins over the built-in one. */
export function starterForms(language: Language, own: Partial<Record<string, string>> = {}): StarterForms {
  const code = own[language] || STARTERS[language];
  return language === 'java21' ? splitStarter(code) : { forms: [{ name: '', code }], default: code };
}

/** The forms of a problem's Java starting code when it offers a list choice, the array form first; empty when it does not. */
export function listChoice(own: Partial<Record<string, string>> = {}): StarterForm[] {
  const { forms } = starterForms('java21', own);
  return forms.length > 1 ? [...forms].sort((a, b) => Number(b.name.endsWith('[]')) - Number(a.name.endsWith('[]'))) : [];
}
