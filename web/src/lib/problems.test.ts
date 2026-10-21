import { describe, expect, it } from 'vitest';
import {
  checkArgs,
  rangeText,
  failureSentence,
  nextPlanRow,
  readPlan,
  splitArgs,
  fieldKey,
  formatBytes,
  formatTestList,
  ordinal,
  parseTestList,
  progressText,
  reportLines,
  slugify,
  statusWord,
  toDraft,
  toEdit,
  uncheckedLine,
  verdictLabel,
  type Problem,
  type Validation,
  slowestReferenceText,
} from './problems';

const PROBLEM: Problem = {
  id: 3,
  slug: 'a-plus-b',
  title: 'A plus B',
  difficulty: 'easy',
  tags: ['math', 'intro'],
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
  brute: { language: 'python3', source: 'print(1)' },
  validator: null,
  samples: [1, 2],
  open: [6],
  subtasks: [
    { idx: 1, points: 40, title: 'Small', tests: '3-4' },
    { idx: 2, points: 60, title: 'Large', tests: '5' },
  ],
  tests: [],
  content_hash: 'abc',
  validated_hash: 'abc',
  warnings: [],
};

describe('parseTestList', () => {
  it('reads numbers and ranges', () => {
    expect(parseTestList('1, 2')).toEqual({ ok: true, tests: [1, 2] });
    expect(parseTestList('3-5')).toEqual({ ok: true, tests: [3, 4, 5] });
    expect(parseTestList(' 8 3-4,1 ')).toEqual({ ok: true, tests: [1, 3, 4, 8] });
    expect(parseTestList('2, 2, 1-2')).toEqual({ ok: true, tests: [1, 2] });
    expect(parseTestList('')).toEqual({ ok: true, tests: [] });
  });

  it('refuses what it cannot read', () => {
    expect(parseTestList('a').ok).toBe(false);
    expect(parseTestList('1-').ok).toBe(false);
    expect(parseTestList('5-3')).toEqual({ ok: false, error: 'The range 5-3 runs backwards. Write the smaller number first.' });
    expect(parseTestList('0')).toEqual({ ok: false, error: 'Test numbers run from 1 to 999.' });
    expect(parseTestList('998-1000').ok).toBe(false);
  });
});

describe('formatTestList', () => {
  it('joins runs of three or more into ranges', () => {
    expect(formatTestList([1, 2])).toBe('1, 2');
    expect(formatTestList([5, 3, 4, 1])).toBe('1, 3-5');
    expect(formatTestList([])).toBe('');
  });

  it('round-trips with parseTestList', () => {
    const tests = [1, 2, 4, 5, 6, 9];
    expect(parseTestList(formatTestList(tests))).toEqual({ ok: true, tests });
  });
});

describe('slugify', () => {
  it('suggests a slug from a title', () => {
    expect(slugify('Interval Covering')).toBe('interval-covering');
    expect(slugify('  A + B, again!  ')).toBe('a-b-again');
    expect(slugify('Crème brûlée')).toBe('creme-brulee');
    expect(slugify('x'.repeat(80))).toHaveLength(64);
  });
});

describe('toDraft and toEdit', () => {
  it('round-trips a problem', () => {
    const result = toEdit(toDraft(PROBLEM));
    expect(result).toEqual({
      ok: true,
      edit: {
        title: 'A plus B',
        slug: 'a-plus-b',
        statement_md: 'Add $a$ and $b$.',
        time_limit_ms: 1000,
        memory_limit_mb: 256,
        checker: 'tokens',
        epsilon: 1e-6,
        checker_source: '',
        reference: { language: 'cpp17', source: 'int main(){}' },
        brute: { language: 'python3', source: 'print(1)' },
        validator: null,
        tags: ['math', 'intro'],
        difficulty: 'easy',
        samples: [1, 2],
        open: [6],
        subtasks: [
          { points: 40, title: 'Small', tests: '3-4' },
          { points: 60, title: 'Large', tests: '5' },
        ],
      },
    });
  });

  it('copies programs instead of sharing them', () => {
    const draft = toDraft(PROBLEM);
    draft.reference.source = 'changed';
    expect(PROBLEM.reference.source).toBe('int main(){}');
  });

  it('reports unreadable input as field errors keyed like the server', () => {
    const draft = toDraft(PROBLEM);
    draft.time_limit_ms = null;
    draft.samples = '1, x';
    draft.subtasks[1].tests = '9-7';
    draft.subtasks[0].points = null;
    const result = toEdit(draft);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(Object.keys(result.fields).sort()).toEqual(['samples', 'subtasks.0.points', 'subtasks.1.tests', 'time_limit_ms']);
  });

  it('leaves the generator out for a server before M5', () => {
    const draft = toDraft(PROBLEM);
    expect('generator' in draft).toBe(false);
    expect('plan' in draft).toBe(false);
    const edit = toEdit(draft);
    expect(edit.ok && ('generator' in edit.edit || 'plan' in edit.edit)).toBe(false);
  });

  it('round-trips a generator, its plan and per-subtask arguments', () => {
    const p: Problem = {
      ...PROBLEM,
      tags: ['math', 'ai draft'],
      generator: { language: 'python3', source: 'import sys' },
      subtasks: [
        { idx: 1, points: 40, title: 'Small', tests: '3-4', generator_args: ['n=10'] },
        { idx: 2, points: 60, title: 'Large', tests: '5', generator_args: [] },
      ],
      plan: [
        { idx: 3, subtask: 1, seed: 1, args: [] },
        { idx: 5, subtask: 2, seed: 7, args: ['max', 'k=3'] },
      ],
    };
    const draft = toDraft(p);
    expect(draft.tags).toBe('math');
    expect(draft.subtasks.map((s) => s.args)).toEqual(['n=10', '']);
    expect(draft.plan).toEqual([
      { idx: 3, subtask: 1, seed: 1, args: '' },
      { idx: 5, subtask: 2, seed: 7, args: 'max k=3' },
    ]);
    const result = toEdit(draft);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.edit.generator).toEqual({ language: 'python3', source: 'import sys' });
    expect(result.edit.plan).toEqual(p.plan);
    expect(result.edit.subtasks.map((s) => s.generator_args)).toEqual([['n=10'], []]);
  });

  it('reads a plan that names the test "test"', () => {
    expect(readPlan([{ test: 4, subtask: 2, seed: 9, args: ['a=1'] }])).toEqual([{ idx: 4, subtask: 2, seed: 9, args: ['a=1'] }]);
    expect(readPlan(null)).toEqual([]);
  });

  it('reports bad plan rows and arguments under their own keys', () => {
    const draft = toDraft({ ...PROBLEM, generator: null, plan: [] });
    draft.subtasks[0].args = 'n=10 {bad}';
    draft.plan = [
      { idx: 6, subtask: 1, seed: 1, args: '' },
      { idx: 6, subtask: 3, seed: null, args: 'x y' },
    ];
    const result = toEdit(draft);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(Object.keys(result.fields).sort()).toEqual(['plan', 'plan.1.idx', 'plan.1.seed', 'plan.1.subtask', 'subtasks.0.generator_args']);
    expect(result.fields['plan.1.idx']).toBe('Test 6 is planned twice.');
    expect(result.fields.plan).toBe('Planned tests need a generator. Add one, or remove the planned tests.');
  });

  it('checks and splits generator arguments', () => {
    expect(splitArgs('  n=10   max ')).toEqual(['n=10', 'max']);
    expect(checkArgs('n=10 k=-3 r=1.5,2')).toBe('');
    expect(checkArgs('a $x')).toBe('"$x" is not a valid argument. Use letters, digits and _ . , : = + -, separated by spaces.');
    expect(checkArgs(Array.from({ length: 17 }, (_, i) => `a${i}`).join(' '))).toBe('Use at most 16 arguments.');
  });

  it('suggests the next planned test after the stored tests and the plan', () => {
    expect(nextPlanRow([], [1, 2, 3], 2)).toEqual({ idx: 4, subtask: 2, seed: 1, args: '' });
    expect(nextPlanRow([{ idx: 9, subtask: 1, seed: 4 }], [1, 2], 2)).toEqual({ idx: 10, subtask: 1, seed: 5, args: '' });
  });

  it('asks for epsilon only with the float checker, and drops checker source unless custom', () => {
    const draft = toDraft(PROBLEM);
    draft.epsilon = null;
    draft.checker_source = 'leftover';
    expect(toEdit(draft).ok).toBe(true);
    const edit = toEdit(draft);
    if (edit.ok) expect(edit.edit.checker_source).toBe('');
    draft.checker = 'float';
    expect(toEdit(draft).ok).toBe(false);
  });
});

describe('words', () => {
  it('names statuses, verdicts and ordinals', () => {
    expect(statusWord({ archived: true, validated: true })).toBe('Archived');
    expect(statusWord({ archived: false, validated: true })).toBe('Validated');
    expect(statusWord({ archived: false, validated: false })).toBe('Not validated');
    expect(verdictLabel('WA')).toBe('Wrong answer');
    expect(verdictLabel('TLE')).toBe('Time limit exceeded');
    expect(verdictLabel('SK')).toBe('Skipped');
    expect(['1', '2', '3', '4', '11', '12', '13', '21', '22', '101'].map((n) => ordinal(Number(n)))).toEqual([
      '1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '101st',
    ]);
    expect(fieldKey('subtasks[1].tests')).toBe('subtasks.1.tests');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(2048)).toBe('2.0 KB');
  });

  it('describes failures with the test, role and verdict', () => {
    expect(failureSentence({ idx: 7, role: 'reference', verdict: 'WA' })).toBe('Test 7: reference solution got wrong answer');
    expect(failureSentence({ idx: 2, role: 'validator', verdict: 'WA' })).toBe('Test 2: validator rejected the input');
    expect(failureSentence({ idx: 9, role: 'brute', verdict: 'TLE' })).toBe('Test 9: brute force exceeded the time limit');
  });

  it('describes the queue', () => {
    expect(progressText({ state: 'queued', queue_position: 2 })).toBe('Waiting for the judge (2nd in queue)');
    expect(progressText({ state: 'queued', queue_position: null })).toBe('Waiting for the judge');
    expect(progressText({ state: 'running', queue_position: null })).toBe('Running');
  });
});

describe('uncheckedLine', () => {
  const summary = {
    reference_passed: 6,
    reference_total: 6,
    brute_agreed: 4,
    brute_total: 4,
    validator_passed: 6,
    validator_total: 6,
    max_time_ms: 10,
    time_limit_ms: 1000,
    problems: [],
    warnings: [],
  };
  it('counts the tests the brute force did not finish', () => {
    expect(uncheckedLine(summary)).toBeNull();
    expect(uncheckedLine({ ...summary, brute_unchecked: 2 })).toEqual({
      text: '2 of 6 tests not cross-checked: the brute force did not finish them',
      tone: 'note',
    });
    expect(uncheckedLine({ ...summary, brute_unchecked: 4 })?.tone).toBe('bad');
  });
});

describe('reportLines', () => {
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
      reference_passed: 15,
      reference_total: 15,
      brute_agreed: 15,
      brute_total: 15,
      validator_passed: 15,
      validator_total: 15,
      max_time_ms: 212,
      time_limit_ms: 1000,
      problems: [],
      warnings: ['Subtask points add up to 90, not 100.'],
    },
    error: null,
  };

  it('writes one line per check', () => {
    expect(reportLines(base).map((l) => [l.text, l.tone])).toEqual([
      ['Reference solution passed 15 of 15 tests', 'ok'],
      ['Brute force agreed on 15 of 15 tests', 'ok'],
      ['Validator accepted 15 of 15 inputs', 'ok'],
      ['Slowest reference run 212 ms of 1000 ms', 'ok'],
      ['Subtask points add up to 90, not 100.', 'note'],
    ]);
  });

  it('names failing tests and compile errors, and skips skipped runs', () => {
    const v: Validation = {
      ...base,
      state: 'failed',
      compile: [
        { role: 'reference', verdict: 'OK', output: '' },
        { role: 'brute', verdict: 'CE', output: 'brute.py: SyntaxError' },
      ],
      tests: [
        { idx: 6, role: 'reference', verdict: 'AC', time_ms: 10, memory_kb: 100, message: '' },
        { idx: 7, role: 'reference', verdict: 'WA', time_ms: 10, memory_kb: 100, message: 'expected 3, found 4' },
        { idx: 7, role: 'brute', verdict: 'SK', time_ms: 10000, memory_kb: 100, message: '' },
      ],
      summary: {
        ...base.summary!,
        reference_passed: 14,
        brute_total: 0,
        warnings: [],
        problems: ['Test 7: reference solution got wrong answer. expected 3, found 4'],
      },
    };
    const lines = reportLines(v);
    expect(lines[0]).toEqual({ text: 'Brute force did not compile', tone: 'bad', detail: 'brute.py: SyntaxError' });
    expect(lines.map((l) => l.text)).toContain('Reference solution passed 14 of 15 tests');
    expect(lines.map((l) => l.text)).not.toContain('Brute force agreed on 0 of 0 tests');
    // Once summarised, each failure appears once, in the server's words.
    expect(lines.filter((l) => l.text.startsWith('Test 7'))).toEqual([
      { text: 'Test 7: reference solution got wrong answer. expected 3, found 4', tone: 'bad' },
    ]);

    // While running, failures are listed as they arrive.
    const running = reportLines({ ...v, state: 'running', summary: null });
    expect(running.at(-1)).toEqual({ text: 'Test 7: reference solution got wrong answer', tone: 'bad', detail: 'expected 3, found 4' });
    expect(running.filter((l) => l.text.startsWith('Test 7: brute'))).toEqual([]);
  });
});

describe('slowestReferenceText', () => {
  it("says when the reference's language gets more than the problem's limit (D323)", () => {
    const s = { max_time_ms: 129, time_limit_ms: 2000 };
    expect(slowestReferenceText({ ...s, reference_language: 'java21', time_factor: 2 })).toBe(
      "Slowest reference run 129 ms of the 2000 ms Java 21 gets (twice the problem's 1000 ms)",
    );
    expect(slowestReferenceText({ max_time_ms: 62, time_limit_ms: 1000, reference_language: 'cpp17', time_factor: 1 })).toBe('Slowest reference run 62 ms of 1000 ms');
    // Summaries stored before the factor was recorded.
    expect(slowestReferenceText(s)).toBe('Slowest reference run 129 ms of 2000 ms');
  });
});

describe('rangeText', () => {
  it('writes test numbers as ranges, so six loose tests read as one line (walk 2026-10-21)', () => {
    expect(rangeText([3, 4, 5, 6])).toBe('3-6');
    expect(rangeText([7, 1, 2, 3, 5])).toBe('1-3, 5, 7');
    expect(rangeText([4])).toBe('4');
  });
});
