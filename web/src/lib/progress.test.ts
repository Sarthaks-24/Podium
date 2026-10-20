import { describe, expect, it } from 'vitest';
import {
  cellSentence,
  contestPercent,
  deriveByDifficulty,
  deriveByTag,
  deriveLosses,
  deriveRetry,
  deriveTotals,
  homeLine,
  percentOf,
  practiseNextIn,
  progressPath,
  rankText,
  scoreSummary,
  shortTitle,
  weakestFirst,
  withDerived,
  type Progress,
  type ProgressContest,
  type ProgressProblem,
} from './progress';

function problem(label: string, score: number | null, over: Partial<ProgressProblem> = {}): ProgressProblem {
  return {
    label,
    title: `Problem ${label}`,
    problem_id: label.charCodeAt(0),
    score,
    max_score: 100,
    attempts: score === null ? null : 2,
    full_at_minutes: score === 100 ? 14 : null,
    best_verdict: score === null ? null : score === 100 ? 'AC' : 'WA',
    tags: ['math'],
    difficulty: 'easy',
    practice_open: true,
    ...over,
  };
}

function contest(slug: string, endedAt: number, problems: ProgressProblem[], over: Partial<ProgressContest> = {}): ProgressContest {
  const score = problems.reduce((n, p) => n + (p.score ?? 0), 0);
  return {
    slug,
    title: `${slug} contest`,
    detail: 'full',
    ended_at: endedAt,
    score,
    max_score: problems.reduce((n, p) => n + p.max_score, 0),
    rank: 3,
    ranked_count: 20,
    problems,
    ...over,
  };
}

const SEMESTER: ProgressContest[] = [
  contest('week-6', 3000, [
    problem('A', 100, { tags: ['math'], difficulty: 'easy' }),
    problem('B', 40, { tags: ['graphs'], difficulty: 'medium', best_verdict: 'TLE', attempts: 3 }),
    problem('C', 0, { tags: ['graphs', 'ai draft'], difficulty: 'hard', best_verdict: 'WA', practice_open: false }),
  ]),
  contest('week-5', 2000, [problem('A', 100, { tags: ['strings'] }), problem('B', 70, { tags: ['graphs'], difficulty: 'medium' })]),
  // An exam-mode contest: totals only.
  contest('mid-sem', 1000, [problem('A', null, { max_score: 150 }), problem('B', null, { max_score: 150 })], { detail: 'totals', score: 120, max_score: 300 }),
];

describe('progress derivations', () => {
  it('handles a response with no contests', () => {
    const p: Progress = {
      user: { id: 7, name: 'Asha Rao' },
      totals: { contests_taken: 0, points: 0, max_points: 0, problems_full: 0, problems_partial: 0, problems_zero: 0 },
      contests: [],
    };
    const d = withDerived(p);
    expect(d.by_tag).toEqual([]);
    expect(d.by_difficulty).toEqual([]);
    expect(d.losses).toEqual([]);
    expect(d.retry).toEqual([]);
    expect(homeLine(p)).toBeNull();
    expect(scoreSummary([])).toBe('No contests yet.');
  });

  it('reads one contest', () => {
    const one = [SEMESTER[0]];
    expect(scoreSummary(one)).toBe('Score in 1 contest: 47 percent.');
    expect(deriveTotals(one)).toMatchObject({ contests_taken: 1, points: 140, max_points: 300, problems_full: 1, problems_partial: 1, problems_zero: 1 });
  });

  it('rounds percentages to whole numbers and never divides by zero', () => {
    expect(percentOf(1, 3)).toBe(33);
    expect(percentOf(2, 3)).toBe(67);
    expect(percentOf(5, 0)).toBe(0);
    expect(percentOf(-3, 10)).toBe(0);
    expect(contestPercent({ score: 245, max_score: 300 })).toBe(82);
  });

  it('leaves exam-mode contests out of topics, difficulty, losses and retry, but counts their points', () => {
    const tags = deriveByTag(SEMESTER);
    expect(tags.find((t) => t.tag === 'math')).toEqual({ tag: 'math', earned: 100, available: 100, problems: 1 });
    expect(tags.find((t) => t.tag === 'graphs')).toEqual({ tag: 'graphs', earned: 110, available: 300, problems: 3 });
    expect(tags.some((t) => t.tag === 'ai draft')).toBe(false);
    const totals = deriveTotals(SEMESTER);
    expect(totals.points).toBe(140 + 170 + 120);
    expect(totals.max_points).toBe(300 + 200 + 300);
    expect(totals.problems_full + totals.problems_partial + totals.problems_zero).toBe(5);
  });

  it('sorts topics weakest first and keeps difficulty in its natural order', () => {
    const rows = weakestFirst(deriveByTag(SEMESTER)).map((t) => t.tag);
    expect(rows).toEqual(['graphs', 'math', 'strings']);
    expect(deriveByDifficulty(SEMESTER).map((d) => d.difficulty)).toEqual(['easy', 'medium', 'hard']);
    // A server list in another order is put back in order too.
    const d = withDerived({
      user: { id: 7, name: '' },
      totals: deriveTotals(SEMESTER),
      contests: SEMESTER,
      by_difficulty: [
        { difficulty: 'hard', earned: 0, available: 100, problems: 1 },
        { difficulty: 'easy', earned: 300, available: 300, problems: 3 },
      ],
    });
    expect(d.by_difficulty.map((x) => x.difficulty)).toEqual(['easy', 'hard']);
  });

  it('counts losses by best verdict, largest first, and lists retries newest first', () => {
    expect(deriveLosses(SEMESTER)).toEqual([
      { verdict: 'WA', problems: 2 },
      { verdict: 'TLE', problems: 1 },
    ]);
    expect(deriveRetry([...SEMESTER].reverse()).map((r) => `${r.slug}/${r.label}`)).toEqual(['week-6/B', 'week-6/C', 'week-5/B']);
    expect(deriveRetry(SEMESTER).find((r) => r.label === 'C')?.practice_open).toBe(false);
  });

  it('derives totals equal to the server totals when both exist', () => {
    const server = { contests_taken: 3, points: 430, max_points: 800, problems_full: 2, problems_partial: 2, problems_zero: 1 };
    expect(deriveTotals(SEMESTER)).toEqual(server);
  });

  it('prefers the server lists when they are present', () => {
    const p: Progress = {
      user: { id: 7, name: 'Asha Rao' },
      totals: deriveTotals(SEMESTER),
      contests: SEMESTER,
      losses: [{ verdict: 'RE', problems: 9 }],
    };
    expect(withDerived(p).losses).toEqual([{ verdict: 'RE', problems: 9 }]);
    expect(withDerived(p).contests.map((c) => c.slug)).toEqual(['week-6', 'week-5', 'mid-sem']);
  });
});

describe('progress wording', () => {
  it('summarises the line in one sentence', () => {
    expect(scoreSummary(SEMESTER)).toBe('Scores over 3 contests, from 40 to 85 percent. Latest 47 percent.');
  });

  it('gives each cell a sentence', () => {
    expect(cellSentence(problem('B', 40, { attempts: 3 }))).toBe('Problem B, 40 of 100, 3 attempts, best result wrong answer.');
    expect(cellSentence(problem('A', 100, { attempts: 1 }))).toBe('Problem A, 100 of 100, 1 attempt.');
    expect(cellSentence(problem('C', 0, { zero_by_decision: true }))).toBe('Problem C, 0 of 100, set to zero by a review decision.');
    expect(cellSentence(problem('D', null))).toBe('Problem D, Problem D: totals only in this contest.');
    expect(cellSentence(problem('E', 60, { upsolved: true }))).toBe('Problem E, 60 of 100, 2 attempts, best result wrong answer, solved later in practice.');
  });

  it('keeps the rank a plain fact', () => {
    expect(rankText({ rank: 12, ranked_count: 140 })).toBe('12 of 140');
    expect(rankText({ rank: null, ranked_count: null })).toBe('');
  });

  it('shortens contest names for the axis', () => {
    expect(shortTitle('Week 6 contest')).toBe('Week 6');
    expect(shortTitle('Mid-semester practical exam')).toBe('Mid-semeste…');
  });

  it('builds the Home line and hides it when asked', () => {
    const p: Progress = { user: { id: 7, name: '' }, totals: deriveTotals(SEMESTER), contests: SEMESTER };
    expect(homeLine(p)).toEqual({ contests: 3, points: 430, max: 800 });
    expect(homeLine({ ...p, hide_progress: true })).toBeNull();
  });

  it('picks the weakest problem to practise next, never a zero by decision', () => {
    expect(practiseNextIn(SEMESTER[0])?.label).toBe('C');
    const decided = contest('w', 1, [problem('A', 0, { zero_by_decision: true }), problem('B', 80)]);
    expect(practiseNextIn(decided)?.label).toBe('B');
    expect(practiseNextIn(SEMESTER[2])).toBeNull();
    expect(practiseNextIn(null)).toBeNull();
  });

  it('builds the API path for the student and for an organiser', () => {
    expect(progressPath()).toBe('/api/me/progress');
    expect(progressPath('7')).toBe('/api/admin/users/7/progress');
  });
});
