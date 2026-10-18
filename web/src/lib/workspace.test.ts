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
  limitsText,
  newKey,
  runVerdictLabel,
  waitingText,
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

  it('points a wrong answer on a sample at Run on samples', () => {
    expect(guidance('WA', 'cpp17', { sample: true })[0]).toContain('Run on samples');
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
    expect(headline(sub({ state: 'queued', verdict: '', queue_position: 3 })).text).toBe('3rd in queue');
    expect(headline(sub({ state: 'running', verdict: '' })).text).toBe('Judging');
    expect(announcement(sub())).toBe('Accepted. Score 100 of 100.');
  });

  it('formats waiting, failure, times and memory', () => {
    expect(waitingText({ state: 'queued', queue_position: 1 })).toBe('1st in queue');
    expect(waitingText({ state: 'queued', queue_position: null })).toBe('Waiting for the judge');
    expect(failureText('RE', 2)).toBe('Runtime error on test 2');
    expect(formatMs(14)).toBe('10 ms');
    expect(formatMs(15)).toBe('20 ms');
    expect(formatKb(512)).toBe('512 KB');
    expect(formatKb(3300)).toBe('3.2 MB');
    expect(limitsText(1000, 256)).toBe('Time limit 1 s, memory 256 MB');
    expect(limitsText(1500, 64)).toBe('Time limit 1500 ms, memory 64 MB');
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
