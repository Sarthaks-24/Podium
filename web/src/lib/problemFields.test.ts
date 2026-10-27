import { describe, expect, it } from 'vitest';
import { describedBy, fieldErrors, fieldTarget, planErrors, programError } from './problemFields';
import type { Draft } from './problems';

const DRAFT: Draft = {
  title: 'A plus B',
  slug: 'a-plus-b',
  statement_md: '',
  time_limit_ms: 1000,
  memory_limit_mb: 256,
  checker: 'tokens',
  epsilon: null,
  checker_source: '',
  reference: { language: 'cpp17', source: '' },
  brute: null,
  validator: null,
  tags: '',
  difficulty: '',
  samples: '1',
  subtasks: [{ points: 100, title: 'All', tests: '2-5', args: '' }],
  generator: null,
  plan: [{ idx: 6, subtask: 1, seed: 1, args: '' }],
};

describe('fieldTarget', () => {
  it('points row errors at their control while the row exists', () => {
    expect(fieldTarget('subtasks.0.points', DRAFT, false)).toEqual({ id: 'subtask-0-points', label: 'Subtask 1 points' });
    expect(fieldTarget('subtasks.3.points', DRAFT, false).id).toBeNull();
    expect(fieldTarget('plan.0.seed', DRAFT, true)).toEqual({ id: 'plan-0-seed', label: 'Planned test 1 seed' });
  });

  it('has no control for a program or field that is not on the page', () => {
    expect(fieldTarget('brute.source', DRAFT, false)).toEqual({ id: null, label: 'Brute force' });
    expect(fieldTarget('reference.language', DRAFT, false).id).toBe('reference-language');
    expect(fieldTarget('epsilon', DRAFT, false).id).toBeNull();
    expect(fieldTarget('generator', DRAFT, true).id).toBeNull();
    expect(fieldTarget('subtasks.0.generator_args', DRAFT, false).id).toBeNull();
    expect(fieldTarget('subtasks.0.generator_args', DRAFT, true).id).toBe('subtask-0-args');
  });

  it('names unknown keys with no label', () => {
    expect(fieldTarget('something', DRAFT, false)).toEqual({ id: null, label: '' });
  });
});

describe('field helpers', () => {
  const fields = { title: 'Enter a title.', 'reference.source': 'Add a reference solution.', 'plan.0.args': 'Bad arguments.' };

  it('lists every error with its control', () => {
    expect(fieldErrors(fields, DRAFT, true).map((f) => f.id)).toEqual(['title', 'reference-source', 'plan-0-args']);
  });

  it('finds a program error under any of its keys', () => {
    expect(programError(fields, 'reference')).toBe('Add a reference solution.');
    expect(programError(fields, 'brute')).toBe('');
  });

  it('joins help and error ids for aria-describedby', () => {
    expect(describedBy(fields, 'title')).toBe('title-error');
    expect(describedBy(fields, 'slug', 'slug-help')).toBe('slug-help');
    expect(describedBy(fields, 'slug')).toBeUndefined();
    expect(describedBy(fields, 'plan.0.args', 'h')).toBe('h plan-0-args-error');
  });

  it('keeps a plan row errors in column order', () => {
    expect(planErrors({ 'plan.1.seed': 'b', 'plan.1.idx': 'a' }, 1)).toEqual([
      { key: 'plan.1.idx', message: 'a' },
      { key: 'plan.1.seed', message: 'b' },
    ]);
  });
});
