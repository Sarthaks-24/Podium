import { describe, expect, it } from 'vitest';
import { buildReport, testFailureText, type Validation } from './problems';

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
