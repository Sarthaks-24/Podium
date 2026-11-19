import { describe, expect, it } from 'vitest';
import { fieldId, fieldTree, labelOf, parseAnswer, sendableAnswer, setAt, showAnswer } from './answerFields';

const answer = {
  title: 'Array sum',
  time_limit_ms: 1000,
  hard: false,
  note: null,
  tags: ['arrays'],
  subtasks: [{ title: 'Small n', points: 40 }],
  languages: [{ language: 'cpp17', starter: 'int f();' }],
};

describe('fieldTree', () => {
  it('turns strings into text, numbers into numbers and objects and arrays into groups', () => {
    const tree = fieldTree(answer);
    expect(tree.map((n) => [n.label, n.kind])).toEqual([
      ['Title', 'text'],
      ['Time limit ms', 'number'],
      ['Hard', 'bool'],
      ['Note', 'null'],
      ['Tags', 'group'],
      ['Subtasks', 'group'],
      ['Languages', 'group'],
    ]);
  });

  it('labels list items by position, with the language or title when there is one', () => {
    const tree = fieldTree(answer);
    const labels = (i: number) => (tree[i] as { children: { label: string }[] }).children.map((c) => c.label);
    expect(labels(4)).toEqual(['Item 1']);
    expect(labels(5)).toEqual(['Item 1, Small n']);
    expect(labels(6)).toEqual(['Item 1, cpp17']);
  });

  it('has no fields for a value that is not an object', () => {
    expect(fieldTree([1, 2])).toEqual([]);
    expect(fieldTree('x')).toEqual([]);
    expect(fieldTree(null)).toEqual([]);
  });

  it('gives each field its path', () => {
    const subtasks = fieldTree(answer)[5] as unknown as { children: { children: { path: unknown[] }[] }[] };
    expect(subtasks.children[0].children[1].path).toEqual(['subtasks', 0, 'points']);
  });
});

describe('setAt', () => {
  it('changes one value and leaves the original and the rest alone', () => {
    const next = setAt(answer, ['subtasks', 0, 'points'], 60) as typeof answer;
    expect(next.subtasks[0].points).toBe(60);
    expect(answer.subtasks[0].points).toBe(40);
    expect(next.languages).toBe(answer.languages);
    expect(next.title).toBe('Array sum');
  });

  it('changes a value in a list of strings', () => {
    expect((setAt(answer, ['tags', 0], 'sums') as typeof answer).tags).toEqual(['sums']);
  });
});

describe('the JSON view', () => {
  it('prints with two spaces and parses back to the same answer', () => {
    const text = showAnswer(answer);
    expect(text).toContain('\n  "title": "Array sum"');
    expect(parseAnswer(text)).toEqual({ ok: true, value: answer });
  });

  it('keeps the parser error for text that is not JSON', () => {
    const bad = parseAnswer('{"title": ');
    expect(bad.ok).toBe(false);
    if (!bad.ok) expect(bad.error).not.toBe('');
  });

  it('sends compact JSON', () => {
    expect(sendableAnswer({ a: 1, b: ['x'] })).toBe('{"a":1,"b":["x"]}');
  });
});

describe('names', () => {
  it('writes labels in sentence case', () => {
    expect(labelOf('locked_top')).toBe('Locked top');
    expect(labelOf('statement_md')).toBe('Statement md');
  });

  it('makes an id from a path', () => {
    expect(fieldId(['languages', 1, 'locked_top'])).toBe('f-languages-1-locked_top');
  });
});
