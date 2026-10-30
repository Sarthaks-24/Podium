import { describe, expect, it } from 'vitest';
import { firstDifference, lineDiff, showWhitespace } from './diff';

describe('sample diff', () => {
  it('finds nothing when outputs match, ignoring line endings and one final newline', () => {
    expect(firstDifference('3\n', '3\n')).toBeNull();
    expect(firstDifference('3\n', '3')).toBeNull();
    expect(firstDifference('1 2\r\n3\r\n', '1 2\n3\n')).toBeNull();
    expect(firstDifference('', '')).toBeNull();
  });

  it('reports the first differing line', () => {
    expect(firstDifference('1\n2\n3\n', '1\n5\n3\n')).toEqual({ line: 2, expected: '2', actual: '5' });
  });

  it('counts trailing spaces as a difference', () => {
    expect(firstDifference('2\n', '2 \n')).toEqual({ line: 1, expected: '2', actual: '2 ' });
  });

  it('reports a missing or extra line as null on the short side', () => {
    expect(firstDifference('1\n2\n', '1\n')).toEqual({ line: 2, expected: '2', actual: null });
    expect(firstDifference('1\n', '1\n2\n')).toEqual({ line: 2, expected: null, actual: '2' });
    expect(firstDifference('1\n', '')).toEqual({ line: 1, expected: '1', actual: null });
  });

  it('treats an extra blank line as a difference', () => {
    expect(firstDifference('1\n', '1\n\n')).toEqual({ line: 2, expected: null, actual: '' });
  });

  it('makes spaces and tabs visible', () => {
    expect(showWhitespace('a b\tc ')).toBe('a·b⇥c·');
  });
});

describe('line diff (v4 E2)', () => {
  const show = (d: ReturnType<typeof lineDiff>) => d?.map((l) => l.kind + l.text).join('\n');

  it('keeps shared lines and marks the rest - for a and + for b', () => {
    expect(show(lineDiff('a\nb\nc\nd\n', 'a\nx\nc\nd\ne'))).toBe(' a\n-b\n+x\n c\n d\n+e');
  });

  it('reads two equal programs as all kept, whatever their line endings', () => {
    expect(show(lineDiff('int main() {\r\n}\r\n', 'int main() {\n}'))).toBe(' int main() {\n }');
  });

  it('handles an empty side', () => {
    expect(show(lineDiff('', 'a\nb'))).toBe('+a\n+b');
    expect(show(lineDiff('a', ''))).toBe('-a');
    expect(lineDiff('', '')).toEqual([]);
  });

  it('gives up past the limit once the shared start and end are taken off', () => {
    const many = (p: string) => Array.from({ length: 1001 }, (_, i) => p + i).join('\n');
    expect(lineDiff(many('a'), many('b'))).toBeNull();
    expect(lineDiff('top\n' + many('a'), 'top\n' + many('a') + '\nend')?.length).toBe(1003);
  });
});
