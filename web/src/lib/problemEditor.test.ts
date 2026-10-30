import { describe, expect, it } from 'vitest';
import { parseRole, roleOf, setRole, statusLine, subtaskCounts } from './problemEditor';
import { toEdit, type Draft, type Problem, type TestCase, type Validation } from './problems';

function draft(samples: string, ranges: string[]): Pick<Draft, 'samples' | 'subtasks'> {
  return { samples, subtasks: ranges.map((tests) => ({ points: 50, title: '', tests })) };
}

describe('test roles', () => {
  it('reads each test from the example list and the subtask ranges', () => {
    const d = draft('1, 2', ['3-4', '5, 8']);
    expect([1, 3, 5, 6, 8].map((i) => roleOf(i, d))).toEqual(['example', 1, 2, 'none', 2]);
  });

  it('moves a test to its new role and writes the lists in range form', () => {
    const d = draft('1, 2', ['3-5', '6']);
    setRole(d, 4, 2);
    expect(d).toEqual(draft('1, 2', ['3, 5', '4, 6']));
    setRole(d, 2, 'none');
    expect(d.samples).toBe('1');
    setRole(d, 7, 1);
    setRole(d, 6, 'example');
    expect(d).toEqual(draft('1, 6', ['3, 5, 7', '4']));
    const back = draft('1', ['2-3']);
    setRole(back, 3, 'none');
    setRole(back, 3, 1);
    expect(back).toEqual(draft('1', ['2-3']));
  });

  it('turns roles back into the body the server takes', () => {
    const full: Draft = {
      title: 'A', slug: 'a', statement_md: '', time_limit_ms: 1000, memory_limit_mb: 256, checker: 'tokens', epsilon: null,
      checker_source: '', reference: { language: 'cpp17', source: 'x' }, brute: null, validator: null, tags: '', difficulty: '',
      ...draft('1', ['2-3', '4']),
    };
    setRole(full, 4, 1);
    const empty = toEdit(full);
    expect(!empty.ok && empty.fields['subtasks.1.tests']).toBe("Give subtask 2 at least one test: choose Subtask 2 as a test's role.");
    setRole(full, 2, 2);
    const result = toEdit(full);
    expect(result.ok && result.edit.samples).toEqual([1]);
    expect(result.ok && result.edit.subtasks.map((s) => s.tests)).toEqual(['3-4', '2']);
  });

  it('parses select values and counts tests per subtask', () => {
    expect([parseRole('example'), parseRole('none'), parseRole('3')]).toEqual(['example', 'none', 3]);
    expect(subtaskCounts([{ idx: 1 }, { idx: 2 }, { idx: 3 }, { idx: 9 }], draft('1', ['2-3', '7']))).toEqual([2, 0]);
  });
});

describe('statusLine', () => {
  const tests = [1, 2, 3].map((idx) => ({ idx }) as TestCase);
  const p = { archived: false, validated: true, content_hash: 'h', tests } as Pick<Problem, 'archived' | 'validated' | 'content_hash' | 'tests'>;
  const v = (over: Partial<Validation>): Validation => ({
    id: 1, state: 'passed', content_hash: 'h', current: true, queued_at: 0, finished_at: 1, queue_position: null, compile: [], tests: [],
    summary: { reference_passed: 3, reference_total: 3, brute_agreed: 0, brute_total: 0, validator_passed: 0, validator_total: 0, max_time_ms: 286, time_limit_ms: 1000, problems: [], warnings: [] },
    error: null, ...over,
  });

  it('says the state, the tests and the slowest run in one line', () => {
    expect(statusLine(p, v({}), false)).toEqual({ tone: 'ok', text: 'Validated, 3 tests, slowest 286 ms' });
    expect(statusLine({ ...p, validated: false }, null, false).text).toBe('Not validated, 3 tests');
    expect(statusLine({ ...p, validated: false, tests: [] }, null, false).text).toBe('Not validated, no tests');
    expect(statusLine(p, v({ state: 'queued', queue_position: 2 }), false).text).toBe('Validating, 2nd in queue');
    expect(statusLine(p, null, true).text).toBe('Making outputs, then validating');
    expect(statusLine({ ...p, archived: true }, null, false).text).toBe('Archived');
    const failed = v({ state: 'failed', tests: [{ idx: 7, role: 'reference', verdict: 'WA', time_ms: 1, memory_kb: 1, message: '' }] });
    expect(statusLine({ ...p, validated: false }, failed, false)).toEqual({ tone: 'bad', text: 'Validation failed on test 7' });
  });
});
