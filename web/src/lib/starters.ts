/**
 * Starter code per language, put in the editor when there is no draft.
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

/** The starter code for a language. */
export function starter(language: Language): string {
  return STARTERS[language];
}

/** True when the source is empty or still one of the starters, untouched. */
export function isStarterOrEmpty(source: string): boolean {
  const s = source.trim();
  return s === '' || Object.values(STARTERS).some((t) => t.trim() === s);
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
