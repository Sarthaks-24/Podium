// The problem editor's readiness helpers (v2 P3 F5.2, lib/problems.ts).
import { describe, expect, it } from 'vitest';
import { imagesWithoutAlt, judgedFieldsChanged, pipelineLine, toDraft, validationResultLine, wordList, type Problem, type Validation } from './problems';

const PROBLEM: Problem = {
  id: 3,
  slug: 'a-plus-b',
  title: 'A plus B',
  difficulty: 'easy',
  tags: ['math'],
  origin: 'import',
  archived: false,
  test_count: 5,
  points: 100,
  validated: true,
  validated_at: 1792183192359,
  updated_at: 1792183192359,
  statement_md: 'Add $a$ and $b$.',
  time_limit_ms: 1000,
  memory_limit_mb: 256,
  checker: 'tokens',
  epsilon: 1e-6,
  checker_source: '',
  reference: { language: 'cpp17', source: 'int main(){}' },
  brute: null,
  validator: null,
  samples: [1, 2],
  subtasks: [{ idx: 1, points: 100, title: 'All', tests: '3-5' }],
  tests: [],
  content_hash: 'abc',
  validated_hash: 'abc',
  warnings: [],
};

describe('judgedFieldsChanged mirrors hash.go', () => {
  const saved = toDraft(PROBLEM);

  it('ignores what the content hash leaves out', () => {
    const now = toDraft(PROBLEM);
    now.title = 'Sum';
    now.slug = 'sum';
    now.statement_md = 'Print $a + b$.';
    now.tags = 'math, easy';
    now.difficulty = 'medium';
    now.subtasks[0].title = 'Everything';
    expect(judgedFieldsChanged(saved, now)).toEqual([]);
  });

  it('names every judged field that changed', () => {
    const now = toDraft(PROBLEM);
    now.time_limit_ms = 2000;
    now.checker = 'float';
    now.reference = { language: 'cpp17', source: 'int main(){return 0;}' };
    now.brute = { language: 'python3', source: 'print(3)' };
    now.validator = { language: 'python3', source: 'pass' };
    now.samples = '1';
    now.subtasks[0].points = 90;
    expect(judgedFieldsChanged(saved, now)).toEqual(['limits', 'checker', 'reference solution', 'brute force', 'validator', 'samples', 'subtasks']);
  });

  it('treats a program with empty source as none and reads lists as numbers', () => {
    const now = toDraft(PROBLEM);
    now.brute = { language: 'python3', source: '   ' };
    now.samples = '2, 1';
    now.subtasks[0].tests = '3, 4, 5';
    expect(judgedFieldsChanged(saved, now)).toEqual([]);
  });

  it('counts the custom checker source only for a custom checker', () => {
    const custom = toDraft({ ...PROBLEM, checker: 'custom', checker_source: 'a' });
    const now = toDraft({ ...PROBLEM, checker: 'custom', checker_source: 'b' });
    expect(judgedFieldsChanged(custom, now)).toEqual(['checker']);
    const tokens = toDraft(PROBLEM);
    expect(judgedFieldsChanged(tokens, { ...tokens, checker_source: 'x' })).toEqual([]);
  });

  it('counts the statement of an AI draft, which a person save unvalidates', () => {
    const now = { ...toDraft(PROBLEM), statement_md: 'New words.' };
    expect(judgedFieldsChanged(saved, now, true)).toEqual(['statement']);
  });
});

describe('readiness lines', () => {
  const base: Validation = {
    id: 1,
    state: 'passed',
    content_hash: 'abc',
    current: true,
    queued_at: 0,
    finished_at: 1,
    queue_position: null,
    compile: [],
    tests: [],
    summary: {
      reference_passed: 6,
      reference_total: 6,
      brute_agreed: 6,
      brute_total: 6,
      validator_passed: 6,
      validator_total: 6,
      max_time_ms: 11,
      time_limit_ms: 1000,
      problems: [],
      warnings: [],
    },
    error: null,
  };

  it('says the result of a validation in one line', () => {
    expect(validationResultLine(base)).toEqual({ tone: 'ok', text: 'Validation passed: 6 of 6 tests, slowest 11 ms.' });
    const failed: Validation = {
      ...base,
      state: 'failed',
      tests: [
        { idx: 7, role: 'reference', verdict: 'WA', time_ms: 1, memory_kb: 1, message: '' },
        { idx: 5, role: 'brute', verdict: 'TLE', time_ms: 1, memory_kb: 1, message: '' },
        { idx: 2, role: 'reference', verdict: 'AC', time_ms: 1, memory_kb: 1, message: '' },
      ],
    };
    expect(validationResultLine(failed)).toEqual({ tone: 'bad', text: 'Validation failed on test 5.' });
    expect(validationResultLine({ ...base, state: 'running' })).toBeNull();
    expect(validationResultLine(null)).toBeNull();
    expect(validationResultLine({ ...base, state: 'error' })?.text).toBe('Validation could not finish.');
  });

  it('counts statement images without a description', () => {
    expect(imagesWithoutAlt('![](a.png) and ![ ](b.png) and ![A graph](c.png)')).toBe(2);
    expect(imagesWithoutAlt('No images.')).toBe(0);
  });

  it('describes the chain after generating inputs while it runs', () => {
    expect(pipelineLine({ state: 'done' }, { state: 'running' }, null)).toBe('Inputs generated, outputs generating, validation waiting');
    expect(pipelineLine({ state: 'running' }, { state: 'done' }, { state: 'passed' })).toBe('Inputs generating, outputs waiting, validation waiting');
    expect(pipelineLine(null, { state: 'running' }, null)).toBe('Outputs generating, validation waiting');
    expect(pipelineLine(null, { state: 'done' }, { state: 'queued' })).toBe('');
    expect(pipelineLine({ state: 'done' }, { state: 'done' }, { state: 'passed' })).toBe('');
  });

  it('joins words', () => {
    expect(wordList(['limits'])).toBe('limits');
    expect(wordList(['limits', 'checker', 'samples'])).toBe('limits, checker and samples');
  });
});
