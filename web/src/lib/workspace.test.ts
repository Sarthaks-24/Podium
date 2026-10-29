import { describe, expect, it } from 'vitest';
import {
  announcement,
  bestScore,
  failureText,
  formatKb,
  formatMs,
  formatWhen,
  guidance,
  headline,
  isCustomResult,
  limitsText,
  newKey,
  runVerdictLabel,
  waitingText,
  problemScoreLine,
  runAnnouncement,
  runHeadline,
  revealedReason,
  revealedTitle,
  usedText,
  type Run,
  type Submission,
} from './workspace';

function sub(over: Partial<Submission> = {}): Submission {
  return {
    id: 1,
    problem_id: 1,
    language: 'cpp17',
    state: 'done',
    verdict: 'AC',
    score: 100,
    max_score: 100,
    max_time_ms: 10,
    max_memory_kb: 3000,
    created_at: 0,
    judged_at: 1,
    queue_position: null,
    compile_output: '',
    samples: [{ idx: 1, verdict: 'AC', time_ms: 1, memory_kb: 1 }],
    subtasks: [
      { idx: 1, points: 40, earned: 40, verdict: 'AC', tests: [{ idx: 2, verdict: 'AC', time_ms: 3, memory_kb: 1 }] },
      { idx: 2, points: 60, earned: 60, verdict: 'AC', tests: [{ idx: 3, verdict: 'AC', time_ms: 3, memory_kb: 1 }] },
    ],
    ...over,
  };
}

describe('guidance lines', () => {
  it('gives one sentence for each failing verdict and none for accepted or skipped', () => {
    for (const v of ['WA', 'TLE', 'MLE', 'RE', 'OLE', 'CE', 'IE']) {
      const lines = guidance(v, 'python3');
      expect(lines.length, v).toBeGreaterThanOrEqual(1);
      expect(lines[0].endsWith('.'), v).toBe(true);
    }
    expect(guidance('AC', 'cpp17')).toEqual([]);
    expect(guidance('SK', 'cpp17')).toEqual([]);
    expect(guidance('', 'cpp17')).toEqual([]);
  });

  it('adds the language hints where they are safe', () => {
    expect(guidance('CE', 'java21')).toContain('Name your public class Main.');
    expect(guidance('CE', 'cpp17')).toHaveLength(1);
    expect(guidance('TLE', 'python3')[1]).toMatch(/^Read input with sys\.stdin/);
    expect(guidance('TLE', 'cpp17')[1]).toContain('ios::sync_with_stdio(false)');
    expect(guidance('TLE', 'java21')[1]).toContain('BufferedReader');
    expect(guidance('TLE', 'node22')[1]).toContain('readFileSync');
    expect(guidance('WA', 'java21')).toHaveLength(1);
  });

  it('points a wrong answer on an example at Run', () => {
    expect(guidance('WA', 'cpp17', { sample: true })[0]).toBe('This test is an example. Run to see your output next to the expected one.');
  });

  it('uses no dashes or exclamation marks', () => {
    const all = ['WA', 'TLE', 'MLE', 'RE', 'OLE', 'CE', 'IE'].flatMap((v) =>
      ['cpp17', 'java21', 'python3', 'node22'].flatMap((l) => guidance(v, l)),
    );
    // Built from code points so this file stays free of the characters it bans.
    const banned = new RegExp(`[!${String.fromCharCode(0x2013, 0x2014)}]`);
    for (const line of all) expect(line).not.toMatch(banned);
  });
});

describe('headlines and announcements', () => {
  it('reads accepted, partial, failed, compile error and waiting states', () => {
    expect(headline(sub())).toEqual({ text: 'Accepted', tone: 'ok', detail: '' });

    const partial = sub({
      verdict: 'WA',
      score: 40,
      subtasks: [
        { idx: 1, points: 40, earned: 40, verdict: 'AC', tests: [{ idx: 2, verdict: 'AC', time_ms: 3, memory_kb: 1 }] },
        { idx: 2, points: 60, earned: 0, verdict: 'WA', tests: [{ idx: 5, verdict: 'WA', time_ms: 3, memory_kb: 1 }] },
      ],
    });
    expect(headline(partial)).toEqual({ text: 'Partial, 40 of 100', tone: 'part', detail: 'Wrong answer on test 5' });
    expect(announcement(partial)).toBe('Partial score. Wrong answer on test 5. Score 40 of 100.');

    const tle = sub({
      verdict: 'TLE',
      score: 0,
      samples: [{ idx: 1, verdict: 'AC', time_ms: 1, memory_kb: 1 }],
      subtasks: [{ idx: 1, points: 100, earned: 0, verdict: 'TLE', tests: [{ idx: 9, verdict: 'TLE', time_ms: 1000, memory_kb: 1 }] }],
    });
    expect(headline(tle).text).toBe('Time limit exceeded on test 9');
    expect(announcement(tle)).toBe('Time limit exceeded on test 9. Score 0 of 100.');

    expect(headline(sub({ verdict: 'CE', score: 0 })).text).toBe('Compile error');
    expect(headline(sub({ state: 'queued', verdict: '', queue_position: 3 })).text).toBe('Submitted, 3rd in queue');
    expect(headline(sub({ state: 'running', verdict: '' })).text).toBe('Submitted, judging');
    expect(headline(sub({ state: 'queued', verdict: '', queue_position: null })).text).toBe('Submitted, waiting for the judge');
    expect(announcement(sub())).toBe('Accepted. Score 100 of 100.');
  });

  it('formats waiting, failure, times and memory', () => {
    expect(waitingText({ state: 'queued', queue_position: 1 })).toBe('1st in queue');
    expect(waitingText({ state: 'queued', queue_position: null })).toBe('Waiting for the judge');
    expect(failureText('RE', 2)).toBe('Runtime error on test 2');
    expect(formatMs(14)).toBe('10 ms');
    expect(formatMs(15)).toBe('20 ms');
    // The server rounds to the nearest step, so a 0 is under half of it (D322).
    expect(formatMs(0)).toBe('under 5 ms');
    expect(formatMs(4)).toBe('under 5 ms');
    expect(formatMs(0, 100)).toBe('under 50 ms');
    expect(formatMs(100, 100)).toBe('100 ms');
    expect(formatMs(120)).toBe('120 ms');
    expect(formatKb(512)).toBe('512 KB');
    // A hidden test's memory is withheld from students as 0 (walk 2026-10-21).
    expect(formatKb(0)).toBe('');
    expect(formatKb(3300)).toBe('3.2 MB');
    expect(limitsText(1000, 256)).toBe('Time limit 1 s, memory 256 MB');
    expect(limitsText(1500, 64)).toBe('Time limit 1500 ms, memory 64 MB');
    // The limit a slower language really gets, with the reason (D323).
    expect(limitsText(1000, 256, { label: 'C++17', factor: 1 })).toBe('Time limit 1 s, memory 256 MB');
    expect(limitsText(1000, 256, { label: 'Java 21', factor: 2 })).toBe(
      "Time limit 2 s in Java 21 (twice the problem's 1 s, as it runs slower than C++), memory 256 MB",
    );
    expect(limitsText(1500, 256, { label: 'Python 3', factor: 3 })).toBe(
      "Time limit 4500 ms in Python 3 (3 times the problem's 1500 ms, as it runs slower than C++), memory 256 MB",
    );
    expect(limitsText(1000, 256, { label: 'Java 21' })).toBe('Time limit 1 s, memory 256 MB');
    expect(runVerdictLabel('OK')).toBe('Finished');
    expect(runVerdictLabel('WA')).toBe('Wrong answer');
  });

  it('dates rows by time today and by day otherwise', () => {
    const now = new Date(2026, 9, 18, 20, 0).getTime();
    expect(formatWhen(new Date(2026, 9, 18, 9, 5).getTime(), now)).toBe('09:05');
    expect(formatWhen(new Date(2026, 9, 16, 19, 42).getTime(), now)).toBe('16 Oct 19:42');
  });

  it('finds the best finished score', () => {
    expect(bestScore([])).toBeNull();
    expect(bestScore([sub({ score: 40 }), sub({ score: 70 }), sub({ state: 'running', score: 100 })])).toEqual({ score: 70, max: 100 });
  });

  it('makes a fresh UUID-shaped key per call', () => {
    const a = newKey();
    const b = newKey();
    expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(a).not.toBe(b);
  });
});

describe('S4 wording and the run announcement (X1)', () => {
  const run = (over: Partial<Run>): Run => ({ id: 1, state: 'done', compile_output: '', results: [], ...over });
  const sample = (verdict: string) => ({ label: 'Example 1', verdict, time_ms: 10, memory_kb: 3000, output: '3\n', expected: '3\n' });

  it('says when a lower score leaves the problem score as it was', () => {
    const done = { state: 'done' as const, score: 20, hidden: false };
    expect(problemScoreLine(done, { score: 60, max: 100 })).toBe('Your score on this problem stays 60 of 100.');
    expect(problemScoreLine({ ...done, score: 60 }, { score: 60, max: 100 })).toBe('');
    expect(problemScoreLine({ ...done, state: 'queued' }, { score: 60, max: 100 })).toBe('');
    expect(problemScoreLine(done, { score: null, max: 100 })).toBe('');
    expect(problemScoreLine(done, null)).toBe('');
  });

  it('counts submissions used', () => {
    expect(usedText(3, 50)).toBe('3 of 50 submissions used');
  });

  it('announces each stage of a run once', () => {
    expect(runAnnouncement(run({ state: 'queued' }))).toBe('');
    expect(runAnnouncement(run({ state: 'running', results: [{ ...sample('') }] }))).toBe('Running on the examples.');
    expect(runAnnouncement(run({ state: 'running', results: [{ ...sample(''), label: 'Your input', expected: null }] }))).toBe('Running on your input.');
    expect(runAnnouncement(run({ results: [sample('AC'), { ...sample('WA'), label: 'Example 2' }] }))).toBe('1 of 2 examples failed.');
    expect(runAnnouncement(run({ compile_output: 'main.cpp:3: error', results: [sample('')] }))).toBe('Compile error.');
    expect(runHeadline(run({ state: 'error' })).tone).toBe('bad');
  });
});

describe('the run headline and the revealed test (P5 F7.13, D115)', () => {
  const run = (results: Run['results']): Run => ({ id: 1, state: 'done', compile_output: '', results });
  const row = (kind: 'sample' | 'custom', verdict: string, expected: string | null = '3\n') => ({
    kind, label: kind === 'custom' ? 'Your input' : 'x', verdict, time_ms: 10, memory_kb: 3000, output: '3\n', expected,
  });

  it('says how many examples failed, in design/student.md words', () => {
    expect(runHeadline(run([row('sample', 'AC'), row('sample', 'AC'), row('sample', 'WA')]))).toEqual({ text: '1 of 3 examples failed', tone: 'bad' });
    expect(runHeadline(run([row('sample', 'AC'), row('sample', 'AC')]))).toEqual({ text: 'All 2 examples passed', tone: 'ok' });
    expect(runHeadline(run([row('sample', 'WA')]))).toEqual({ text: 'The example failed', tone: 'bad' });
    expect(runHeadline(run([row('sample', 'AC')]))).toEqual({ text: 'The example passed', tone: 'ok' });
  });

  it('tells a custom input by its kind, and by a missing expected output from older servers', () => {
    expect(isCustomResult({ kind: 'sample', expected: null })).toBe(false);
    expect(isCustomResult({ kind: 'custom', expected: null })).toBe(true);
    expect(isCustomResult({ expected: null })).toBe(true);
    expect(isCustomResult({ expected: '3\n' })).toBe(false);
    expect(runAnnouncement({ ...run([row('sample', '', null)]), state: 'running' })).toBe('Running on the examples.');
  });

  it('says why a test is shown, and names it', () => {
    expect(revealedReason('submission')).toBe('This test is shown because this is practice.');
    expect(revealedReason('run')).toBe('This test is shown because this was a run.');
    expect(revealedTitle({ idx: 5, subtask_idx: 2 })).toBe('Test 5, in subtask 2');
    expect(revealedTitle({ idx: 1, subtask_idx: null })).toBe('Test 1');
  });
});
