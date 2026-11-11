import { describe, expect, it } from 'vitest';
import { answerLabel, changeLine, checkInstruction, editPaths, fileDiff, keptLine, MAX_INSTRUCTION, parseSentBack, type EditFile } from './aiEdit';

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

describe('what was kept and what the AI sent back', () => {
  it('writes the kept note from the names the server sends', () => {
    expect(keptLine([])).toBe('');
    expect(keptLine(['the test generator'])).toBe('The AI left these out, so your current ones were kept: the test generator.');
    expect(keptLine(['the test generator', 'the limits'])).toBe('The AI left these out, so your current ones were kept: the test generator, the limits.');
  });

  it('reads the answers of a refusal and ignores anything else', () => {
    expect(parseSentBack(undefined)).toEqual([]);
    expect(parseSentBack({ answers: 'x' })).toEqual([]);
    expect(
      parseSentBack({
        answers: [
          { summary: 'Second.', files: [{ path: 'problem.yaml', content: 'a: 1\n' }, { path: 3 }], cut: true },
          { files: [] },
          'junk',
        ],
      }),
    ).toEqual([
      { summary: 'Second.', files: [{ path: 'problem.yaml', content: 'a: 1\n' }], cut: true },
      { summary: '', files: [], cut: false },
    ]);
  });

  it('labels the answers when the retry ran', () => {
    expect(answerLabel(0, 1)).toBe('');
    expect(answerLabel(0, 2)).toBe('Second answer');
    expect(answerLabel(1, 2)).toBe('First answer');
  });
});
