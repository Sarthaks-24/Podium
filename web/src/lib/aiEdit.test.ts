import { describe, expect, it } from 'vitest';
import { changeLine, checkInstruction, editPaths, fileDiff, MAX_INSTRUCTION, type EditFile } from './aiEdit';

const file = (path: string, before: string, after: string): EditFile => ({ path, status: 'changed', before, after });

describe('aiEdit', () => {
  it('checks the instruction', () => {
    expect(checkInstruction('  ')).toBe('Say what to change.');
    expect(checkInstruction('a'.repeat(MAX_INSTRUCTION + 1))).toContain('under 4,000 characters');
    expect(checkInstruction('Make it harder.')).toBe('');
  });

  it('builds the paths', () => {
    expect(editPaths.ask(7)).toBe('/api/admin/problems/7/ai-edit');
    expect(editPaths.accept(7, 'ab')).toBe('/api/admin/problems/7/ai-edit/ab/accept');
    expect(editPaths.one(7, 'a/b')).toBe('/api/admin/problems/7/ai-edit/a%2Fb');
  });

  it('diffs a file line by line', () => {
    expect(fileDiff(file('a', 'x\ny\n', 'x\nz\n'))).toEqual([
      { kind: ' ', text: 'x' },
      { kind: '-', text: 'y' },
      { kind: '+', text: 'z' },
    ]);
  });

  it('names the files', () => {
    expect(changeLine([])).toBe('The draft changes nothing.');
    expect(changeLine([file('statement.md', '', 'a')])).toBe('statement.md would change.');
    expect(changeLine([file('a.py', '', 'a'), file('b.py', '', 'b'), file('c.py', '', 'c')])).toBe('a.py, b.py and c.py would change.');
  });
});
