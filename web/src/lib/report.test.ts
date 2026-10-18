import { describe, expect, it } from 'vitest';
import { buildReport, draftGroups, editorStatus, testFailureText, type TestCase, type Validation } from './problems';

function validation(over: Partial<Validation> = {}): Validation {
  return {
    id: 1,
    state: 'failed',
    content_hash: 'h',
    current: true,
    queued_at: 0,
    finished_at: 1,
    queue_position: null,
    compile: [],
    tests: [],
    summary: null,
    error: null,
    ...over,
  };
}

const SUMMARY = {
  reference_passed: 14,
  reference_total: 15,
  brute_agreed: 0,
  brute_total: 0,
  validator_passed: 0,
  validator_total: 0,
  max_time_ms: 62,
  time_limit_ms: 1000,
  problems: ['The brute force did not compile, so nothing was compared.'],
  warnings: ['There is no validator.'],
};

describe('validation report', () => {
  it('leads with the first thing to fix and groups lines by program', () => {
    const r = buildReport(
      validation({
        compile: [{ role: 'brute', verdict: 'CE', output: 'SyntaxError' }],
        tests: [
          { idx: 7, role: 'reference', verdict: 'WA', time_ms: 31, memory_kb: 1, message: 'Line 1: expected 2.5' },
          { idx: 8, role: 'reference', verdict: 'AC', time_ms: 31, memory_kb: 1, message: '' },
        ],
        summary: SUMMARY,
      }),
    );
    expect(r.lead).toBe('Fix the brute force: it did not compile.');
    expect(r.sections.map((s) => s.title)).toEqual(['Reference solution', 'Brute force']);
    const ref = r.sections[0].lines;
    expect(ref.map((l) => l.text)).toEqual([
      'Reference solution passed 14 of 15 tests',
      'Slowest reference run 62 ms of 1000 ms',
      'Wrong answer on test 7',
    ]);
    expect(ref[2]).toMatchObject({ tone: 'bad', detail: 'Line 1: expected 2.5' });
    expect(ref[2].guidance).toContain('expected output');
    expect(r.sections[1].lines[0]).toMatchObject({ text: 'Brute force did not compile', detail: 'SyntaxError' });
    expect(r.notes.map((n) => n.tone)).toEqual(['bad', 'note']);
  });

  it('names a failing reference test when everything compiled', () => {
    const r = buildReport(validation({ tests: [{ idx: 3, role: 'reference', verdict: 'TLE', time_ms: 2000, memory_kb: 1, message: '' }] }));
    expect(r.lead).toBe('Fix the reference solution: time limit exceeded on test 3.');
  });

  it('has no lead when validation passed or is still running', () => {
    expect(buildReport(validation({ state: 'passed', summary: { ...SUMMARY, reference_passed: 15, problems: [] } })).lead).toBe('');
    expect(buildReport(validation({ state: 'running', tests: [{ idx: 1, role: 'reference', verdict: 'WA', time_ms: 1, memory_kb: 1, message: '' }] })).lead).toBe('');
  });

  it('falls back to the server problem or the judge error', () => {
    expect(buildReport(validation({ summary: { ...SUMMARY, reference_passed: 15 } })).lead).toBe(SUMMARY.problems[0]);
    expect(buildReport(validation({ state: 'error', error: 'The judge stopped.' })).lead).toBe('The judge stopped.');
  });

  it('words failures by program', () => {
    expect(testFailureText({ idx: 4, role: 'validator', verdict: 'WA' })).toBe('Rejected the input of test 4');
    expect(testFailureText({ idx: 4, role: 'brute', verdict: 'WA' })).toBe('Disagreed with the reference on test 4');
    expect(testFailureText({ idx: 4, role: 'checker', verdict: 'IE' })).toBe('Failed on test 4');
    expect(testFailureText({ idx: 4, role: 'reference', verdict: 'RE' })).toBe('Runtime error on test 4');
  });
});

describe('editor status', () => {
  const p = { archived: false, validated: true, validated_at: new Date(2026, 9, 16, 19, 42).getTime(), content_hash: 'h' };

  it('reads each state in words', () => {
    expect(editorStatus(p, null)).toEqual({ text: 'Validated 16 Oct 19:42', tone: 'ok' });
    expect(editorStatus({ ...p, validated: false, validated_at: null }, null).text).toBe('Not validated');
    expect(editorStatus({ ...p, archived: true }, null).text).toBe('Archived');
    expect(editorStatus(p, validation({ state: 'queued', queue_position: 3 })).text).toBe('Validating, 3rd in queue');
    expect(editorStatus(p, validation({ state: 'running' })).text).toBe('Validating');
    expect(editorStatus({ ...p, validated: false }, validation({ summary: { ...SUMMARY, problems: ['a', 'b'] } }))).toEqual({
      text: 'Validation failed, 2 problems',
      tone: 'bad',
    });
    // A failure for an older version does not count.
    expect(editorStatus(p, validation({ content_hash: 'old' })).text).toBe('Validated 16 Oct 19:42');
  });
});

describe('groups from the draft', () => {
  const t = (idx: number, sample: boolean, subtask_idx: number | null): TestCase => ({
    idx,
    sample,
    subtask_idx,
    input_size: 1,
    output_size: 1,
    input_preview: '',
    output_preview: '',
  });
  const tests = [t(1, true, null), t(2, false, 1), t(3, false, 1), t(4, false, null)];

  it('follows the subtasks as typed', () => {
    const g = draftGroups(tests, { samples: '1', subtasks: [{ points: 50, title: '', tests: '2' }, { points: 50, title: '', tests: '3-4' }] });
    expect([...g.entries()]).toEqual([
      [1, 'Sample'],
      [2, 'Subtask 1'],
      [3, 'Subtask 2'],
      [4, 'Subtask 2'],
    ]);
  });

  it('names tests in no subtask', () => {
    const g = draftGroups(tests, { samples: '1', subtasks: [{ points: 100, title: '', tests: '2-3' }] });
    expect(g.get(4)).toBe('Not in a subtask');
  });

  it('falls back to the saved groups while a list cannot be read', () => {
    const g = draftGroups(tests, { samples: '1, x', subtasks: [] });
    expect(g.get(2)).toBe('Subtask 1');
    expect(g.get(4)).toBe('Not in a subtask');
  });
});
