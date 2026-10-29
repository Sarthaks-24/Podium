import { describe, expect, it } from 'vitest';
import {
  byteSpansToText,
  clusters,
  codeLines,
  evidenceSize,
  filterSignals,
  formatScore,
  groupByProblem,
  noteCounter,
  noteLength,
  signalDetail,
  signalStudents,
  similarNumbers,
  similarReason,
  statsLine,
  type Signal,
} from './review';

const utf8 = (s: string) => new TextEncoder().encode(s).length;

/** The byte span of `part` in `source`, the way the server reports it. */
function bytesOf(source: string, part: string): { start: number; end: number } {
  const i = source.indexOf(part);
  return { start: utf8(source.slice(0, i)), end: utf8(source.slice(0, i + part.length)) };
}

function marked(source: string, spans: { start: number; end: number }[]): string[] {
  return codeLines(source, spans).map((l) => l.segments.map((s) => (s.mark ? `[${s.text}]` : s.text)).join(''));
}

describe('byte spans', () => {
  it('maps ASCII offsets one to one', () => {
    expect(byteSpansToText('int main()', [{ start: 4, end: 8 }])).toEqual([{ start: 4, end: 8 }]);
  });

  it('accounts for two, three and four byte characters before a span', () => {
    const source = 'é中😀 x = 1';
    // é is 2 bytes, 中 is 3, 😀 is 4 (two UTF-16 units).
    const span = bytesOf(source, 'x = 1');
    expect(span).toEqual({ start: 10, end: 15 });
    const [text] = byteSpansToText(source, [span]);
    expect(source.slice(text.start, text.end)).toBe('x = 1');
  });

  it('highlights multibyte characters inside a span whole', () => {
    const source = 'print("héllo 世界 🎉")';
    const span = bytesOf(source, 'héllo 世界 🎉');
    const [text] = byteSpansToText(source, [span]);
    expect(source.slice(text.start, text.end)).toBe('héllo 世界 🎉');
  });

  it('widens a span that starts or ends inside a character', () => {
    const source = 'a中b';
    // 中 occupies bytes 1 to 3; 2 is inside it.
    expect(byteSpansToText(source, [{ start: 2, end: 3 }])).toEqual([{ start: 1, end: 2 }]);
    const emoji = 'x😀y';
    // 😀 occupies bytes 1 to 4 and UTF-16 indices 1 and 2.
    expect(byteSpansToText(emoji, [{ start: 3, end: 4 }])).toEqual([{ start: 1, end: 3 }]);
  });

  it('sorts, merges and clamps spans', () => {
    const source = 'abcdefghij';
    expect(
      byteSpansToText(source, [
        { start: 6, end: 40 },
        { start: 0, end: 2 },
        { start: 1, end: 4 },
        { start: 5, end: 5 },
      ]),
    ).toEqual([
      { start: 0, end: 4 },
      { start: 6, end: 10 },
    ]);
  });
});

describe('code lines', () => {
  it('numbers lines and marks a passage across them', () => {
    const source = 'int a;\nint b;\nint c;\n';
    const lines = codeLines(source, [bytesOf(source, 'a;\nint b')]);
    expect(lines.map((l) => l.number)).toEqual([1, 2, 3]);
    expect(marked(source, [bytesOf(source, 'a;\nint b')])).toEqual(['int [a;]', '[int b];', 'int c;']);
    expect(lines[0].segments.find((s) => s.mark)).toMatchObject({ opens: true, closes: false });
    expect(lines[1].segments.find((s) => s.mark)).toMatchObject({ opens: false, closes: true });
    expect(lines.map((l) => l.matched)).toEqual([true, true, false]);
  });

  it('keeps carriage returns out of the text', () => {
    const source = 'a = 1\r\nb = 2\r\n';
    expect(marked(source, [bytesOf(source, '1\r\nb')])).toEqual(['a = [1]', '[b] = 2']);
  });

  it('places markers right after multibyte text on earlier lines', () => {
    const source = '// コメント\ns = "ü"\nreturn s';
    expect(marked(source, [bytesOf(source, 'return')])).toEqual(['// コメント', 's = "ü"', '[return] s']);
  });

  it('opens a passage that starts on a line break on the next line', () => {
    const source = 'ab\ncd';
    const lines = codeLines(source, [{ start: 2, end: 5 }]);
    expect(lines[0].matched).toBe(false);
    expect(lines[1].segments).toEqual([{ text: 'cd', mark: true, opens: true, closes: true }]);
  });

  it('gives an empty source one empty line', () => {
    expect(codeLines('', [])).toEqual([{ number: 1, segments: [], matched: false }]);
  });
});

const users = {
  '7': { name: 'Asha Rao', email: 'asha@college.edu' },
  '8': { name: '', email: 'ravi@college.edu' },
  '9': { name: 'Meera Iyer', email: 'meera@college.edu' },
  '10': { name: 'Kabir Shah', email: 'kabir@college.edu' },
};
const problems = { '11': { label: 'A', title: 'A plus B' }, '12': { label: 'B', title: 'Interval covering' } };

function signal(id: number, extra: Partial<Signal>): Signal {
  return { id, problem_id: 12, kind: 'similar', strength: 'strong', score: 0.94, user_a: 7, user_b: 8, evidence: null, ...extra };
}

describe('wording', () => {
  it('names the students, falling back to the email', () => {
    expect(signalStudents({ user_a: 7, user_b: 8 }, users)).toBe('Asha Rao and ravi@college.edu');
    expect(signalStudents({ user_a: 9 }, users)).toBe('Meera Iyer');
    expect(signalStudents({ user_a: 99 }, users)).toBe('Student 99');
  });

  it('formats the number beside the strength', () => {
    expect(formatScore(0.9, 'similar')).toBe('0.90');
    // A similarity of 1 is a share, never a count.
    expect(formatScore(1, 'similar')).toBe('1.00');
    expect(formatScore(4, 'wrong_output')).toBe('4');
    expect(formatScore(8, 'probing')).toBe('8');
  });

  it('says how much code is behind a similarity', () => {
    expect(evidenceSize({ shared: 41, fingerprints: 44, tokens: 210 })).toBe('41 of 44 fingerprints shared, shorter program 210 tokens');
    expect(evidenceSize({ shared: 12 })).toBe('12 fingerprints shared');
    expect(evidenceSize({ spans_a: [] })).toBe('');
    expect(evidenceSize(null)).toBe('');
  });

  it('words the strength of similar code with its reason, never a bare number (pilot P11)', () => {
    // The pilot's "Weak 1.00": 15 of 15 fingerprints shared.
    expect(similarReason(1, { shared: 15, fingerprints: 15 })).toBe('Short programs, identical');
    expect(similarReason(0.94, { shared: 41 })).toBe('Long programs, nearly identical');
    expect(similarReason(0.78, { shared: 30 })).toBe('Medium-length programs, mostly the same');
    expect(similarReason(0.62, { shared: 60 })).toBe('Long programs, partly the same');
    // Signals stored before the counts say only how alike the code is.
    expect(similarReason(0.81, null)).toBe('Mostly the same');
    expect(similarNumbers(1, { shared: 15, fingerprints: 15, tokens: 48 })).toBe('15 of 15 fingerprints shared, shorter program 48 tokens, score 1.00');
    expect(similarNumbers(0.8, null)).toBe('Score 0.80');
  });

  it('says what each kind found', () => {
    expect(signalDetail({ kind: 'similar', evidence: { spans_a: [{ start: 0, end: 1 }, { start: 3, end: 9 }], spans_b: [], language_a: 'cpp17', language_b: 'cpp17' } })).toBe(
      '2 shared passages, both in C++17',
    );
    expect(signalDetail({ kind: 'similar', evidence: { language_a: 'cpp17', language_b: 'python3' } })).toBe('Shared passages, in C++17 and Python 3');
    expect(signalDetail({ kind: 'wrong_output', evidence: { tests: [3, 5, 7, 9] } })).toBe('Same wrong output on 4 hidden tests: 3, 5, 7 and 9');
    expect(signalDetail({ kind: 'hardcoded', evidence: { tests: [7] } })).toBe('Contains the expected output of test 7');
    expect(signalDetail({ kind: 'probing', evidence: { distinct: 8, attempts: 14 } })).toBe('8 different results from near-identical code over 14 attempts');
  });

  it('counts a reason the way the server does', () => {
    expect(noteLength('  héllo 😀  ')).toBe(7);
    expect(noteCounter('short')).toBe('5 of at least 10 characters');
    expect(noteCounter('a'.repeat(25))).toBe('25 characters');
  });

  it('writes the stats line', () => {
    expect(statsLine({ students: 30, submissions: 84, problems: 2, signals: { similar: 3, probing: 1 }, clusters: 1 })).toBe(
      'Checked 30 students, 84 submissions and 2 problems. 4 signals: 3 similar code and 1 probing. 1 group of similar code.',
    );
    expect(statsLine({ students: 1, submissions: 1, problems: 1, signals: {}, clusters: 0 })).toBe('Checked 1 student, 1 submission and 1 problem. No signals.');
  });
});

describe('grouping', () => {
  const list = [
    signal(1, { problem_id: 12 }),
    signal(2, { problem_id: 11, kind: 'wrong_output', strength: 'moderate', score: 2 }),
    signal(3, { problem_id: 0, kind: 'probing', user_b: undefined }),
    signal(4, { problem_id: 12, user_a: 8, user_b: 9, strength: 'weak', score: 0.61 }),
    signal(5, { problem_id: 12, user_a: 10, user_b: 7, strength: 'moderate', score: 0.8 }),
  ];

  it('filters by kind and strength', () => {
    expect(filterSignals(list, 'similar', '').map((s) => s.id)).toEqual([1, 4, 5]);
    expect(filterSignals(list, '', 'moderate').map((s) => s.id)).toEqual([2, 5]);
    expect(filterSignals(list, 'similar', 'weak').map((s) => s.id)).toEqual([4]);
  });

  it('groups by problem in label order, whole-contest signals last', () => {
    expect(groupByProblem(list, problems).map((g) => [g.problemId, g.signals.map((s) => s.id)])).toEqual([
      [11, [2]],
      [12, [1, 4, 5]],
      [0, [3]],
    ]);
  });

  it('finds clusters from similar-code pairs only', () => {
    expect(clusters(list)).toEqual([[7, 8, 9, 10]]);
    expect(clusters([signal(1, {}), signal(2, { user_a: 9, user_b: 10 })])).toEqual([
      [7, 8],
      [9, 10],
    ]);
  });
});
