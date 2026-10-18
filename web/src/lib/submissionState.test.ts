import { describe, expect, it } from 'vitest';
import { isNewer, mergeRun, mergeSubmission, mergeSubmissions, reduceEvent, type JudgingState } from './submissionState';
import type { Run, Submission } from './workspace';

function sub(id: number, state: Submission['state'], judged = 0, over: Partial<Submission> = {}): Submission {
  const tests = [3, 4, 5].map((idx, i) => ({ idx, verdict: i < judged ? 'AC' : '', time_ms: 1, memory_kb: 1 }));
  return {
    id,
    problem_id: 1,
    language: 'cpp17',
    state,
    verdict: state === 'done' ? 'AC' : '',
    score: state === 'done' ? 100 : 0,
    max_score: 100,
    max_time_ms: 0,
    max_memory_kb: 0,
    created_at: id,
    judged_at: null,
    queue_position: state === 'queued' ? 2 : null,
    compile_output: '',
    samples: [],
    subtasks: [{ idx: 1, points: 100, earned: 0, verdict: '', tests }],
    ...over,
  };
}

function run(id: number, state: Run['state'], results = 0): Run {
  return {
    id,
    state,
    compile_output: '',
    results: Array.from({ length: results }, (_, i) => ({
      label: `Sample ${i + 1}`,
      verdict: 'AC',
      time_ms: 1,
      memory_kb: 1,
      output: '',
      expected: '',
    })),
  };
}

const empty: JudgingState = { submissions: [], run: null, serverNow: null };

describe('submission merging', () => {
  it('orders by stage, then by tests judged', () => {
    expect(isNewer(sub(1, 'queued'), sub(1, 'running'))).toBe(true);
    expect(isNewer(sub(1, 'running', 2), sub(1, 'running', 1))).toBe(false);
    expect(isNewer(sub(1, 'running', 1), sub(1, 'running', 2))).toBe(true);
    expect(isNewer(sub(1, 'done', 3), sub(1, 'running', 3))).toBe(false);
    expect(isNewer(sub(1, 'done'), sub(1, 'done'))).toBe(true);
  });

  it('keeps the list newest first and never goes backwards', () => {
    let list = mergeSubmission([], sub(1, 'done', 3));
    list = mergeSubmission(list, sub(3, 'queued'));
    list = mergeSubmission(list, sub(2, 'done', 3));
    expect(list.map((s) => s.id)).toEqual([3, 2, 1]);

    const ahead = mergeSubmission(list, sub(3, 'running', 2));
    expect(ahead[0].state).toBe('running');
    const late = mergeSubmission(ahead, sub(3, 'queued'));
    expect(late).toBe(ahead);
  });

  it('merges a refetched list', () => {
    const list = [sub(2, 'running', 1), sub(1, 'done', 3)];
    const merged = mergeSubmissions(list, [sub(3, 'queued'), sub(2, 'done', 3), sub(1, 'running', 1)]);
    expect(merged.map((s) => [s.id, s.state])).toEqual([
      [3, 'queued'],
      [2, 'done'],
      [1, 'done'],
    ]);
  });
});

describe('stream reducer', () => {
  it('turns submission events into state as the strip fills', () => {
    let state = reduceEvent(empty, { type: 'submission', data: sub(7, 'queued') }, 1);
    expect(state.submissions[0].queue_position).toBe(2);
    state = reduceEvent(state, { type: 'submission', data: sub(7, 'running', 1) }, 1);
    state = reduceEvent(state, { type: 'submission', data: sub(7, 'running', 2) }, 1);
    expect(state.submissions[0].subtasks[0].tests.filter((t) => t.verdict !== '')).toHaveLength(2);
    // A late, older event is ignored.
    const before = state;
    state = reduceEvent(state, { type: 'submission', data: sub(7, 'running', 1) }, 1);
    expect(state).toBe(before);
    state = reduceEvent(state, { type: 'submission', data: sub(7, 'done', 3, { server_now: 99 }) }, 1);
    expect(state.submissions[0]).toMatchObject({ state: 'done', verdict: 'AC', score: 100 });
    expect(state.serverNow).toBe(99);
  });

  it('ignores submissions for another problem', () => {
    const state = reduceEvent(empty, { type: 'submission', data: sub(8, 'queued', 0, { problem_id: 2 }) }, 1);
    expect(state).toBe(empty);
  });

  it('follows only the run this page started', () => {
    const started = { ...empty, run: run(5, 'queued') };
    const other = reduceEvent(started, { type: 'run', data: run(6, 'done', 2) }, 1);
    expect(other).toBe(started);
    const done = reduceEvent(started, { type: 'run', data: run(5, 'done', 2) }, 1);
    expect(done.run?.state).toBe('done');
    expect(reduceEvent(empty, { type: 'run', data: run(5, 'done') }, 1)).toBe(empty);
  });

  it('records the server clock', () => {
    expect(reduceEvent(empty, { type: 'clock', data: { server_now: 1234 } }, 1).serverNow).toBe(1234);
  });

  it('merges runs by id and stage', () => {
    expect(mergeRun(null, run(1, 'queued'))?.id).toBe(1);
    expect(mergeRun(run(2, 'queued'), run(1, 'done'))?.id).toBe(2);
    expect(mergeRun(run(2, 'done', 2), run(2, 'running', 1))?.state).toBe('done');
    expect(mergeRun(run(2, 'running', 1), run(2, 'done', 2))?.state).toBe('done');
  });
});
