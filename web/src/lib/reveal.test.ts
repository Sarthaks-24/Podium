import { describe, expect, it } from 'vitest';
import { initialReveal, isDone, nextIndex, revealAll, revealStep } from './reveal';
import type { Standings, StandingsRow } from './standings';

function row(user_id: number, name: string, rank: number, scores: [number, number], time: number, pending: [number, number] = [0, 0]): StandingsRow {
  return {
    rank,
    user_id,
    name,
    total: scores[0] + scores[1],
    last_improvement_s: time,
    cells: [
      { label: 'A', score: scores[0], max: 100, pending: pending[0] },
      { label: 'B', score: scores[1], max: 100, pending: pending[1] },
    ],
  };
}

const frozen: Pick<Standings, 'rows'> = {
  rows: [
    row(1, 'Priya', 1, [100, 100], 3000),
    row(2, 'Arjun', 2, [100, 40], 2000, [0, 1]),
    row(3, 'Asha', 3, [100, 0], 1000, [0, 2]),
    row(4, 'Ravi', 4, [40, 0], 900),
  ],
};

const full: Pick<Standings, 'rows'> = {
  rows: [
    row(1, 'Priya', 1, [100, 100], 3000),
    row(3, 'Asha', 2, [100, 100], 5000),
    row(2, 'Arjun', 3, [100, 40], 2000),
    row(4, 'Ravi', 4, [40, 0], 900),
    row(5, 'Late', 5, [0, 30], 6000),
  ],
};

describe('reveal ordering', () => {
  it('starts from the frozen board and resolves unchanged rows up front', () => {
    const s = initialReveal(frozen, full);
    expect(s.rows.map((r) => r.name)).toEqual(['Priya', 'Arjun', 'Asha', 'Ravi', 'Late']);
    expect(s.rows.map((r) => r.rank)).toEqual([1, 2, 3, 4, 5]);
    // Priya and Ravi have nothing pending; Arjun's pending did not change his score but must still be shown.
    expect(s.rows.map((r) => r.resolved)).toEqual([true, false, false, true, false]);
    // Late was not on the frozen board: zero, with a pending mark where they later scored.
    expect(s.rows[4].cells.map((c) => [c.score, c.pending])).toEqual([[0, 0], [0, 1]]);
  });

  it('walks from the bottom one row per step', () => {
    let s = initialReveal(frozen, full);
    expect(s.rows[nextIndex(s)].name).toBe('Late');

    let r = revealStep(s)!;
    expect(r.move).toMatchObject({ from: 4, to: 4, rankFrom: 5, rankTo: 5 });
    s = r.state;
    expect(s.rows[4]).toMatchObject({ name: 'Late', total: 30, resolved: true });

    // Ravi is resolved already, so Asha is next: she climbs from third to second.
    expect(s.rows[nextIndex(s)].name).toBe('Asha');
    r = revealStep(s)!;
    expect(r.move).toMatchObject({ user_id: 3, from: 2, to: 1, rankFrom: 3, rankTo: 2 });
    s = r.state;
    expect(s.rows.map((x) => x.name)).toEqual(['Priya', 'Asha', 'Arjun', 'Ravi', 'Late']);
    expect(s.rows[1].cells.every((c) => c.pending === 0)).toBe(true);

    r = revealStep(s)!;
    expect(r.move).toMatchObject({ user_id: 2, from: 2, to: 2 });
    s = r.state;
    expect(isDone(s)).toBe(true);
    expect(revealStep(s)).toBeNull();
    expect(s.step).toBe(3);
  });

  it('ends equal to the full board, ties sharing a rank', () => {
    const tieFull: Pick<Standings, 'rows'> = {
      rows: [row(1, 'Priya', 1, [100, 0], 100), row(2, 'Arjun', 1, [0, 100], 100), row(3, 'Asha', 3, [50, 0], 50)],
    };
    const tieFrozen: Pick<Standings, 'rows'> = {
      rows: [row(3, 'Asha', 1, [50, 0], 50), row(2, 'Arjun', 2, [0, 0], 0, [0, 1]), row(1, 'Priya', 2, [0, 0], 0, [1, 0])],
    };
    const end = revealAll(initialReveal(tieFrozen, tieFull));
    expect(end.rows.map((r) => [r.name, r.rank])).toEqual([
      ['Priya', 1],
      ['Arjun', 1],
      ['Asha', 3],
    ]);
    expect(end.rows.map((r) => r.total)).toEqual(tieFull.rows.map((r) => r.total));
  });
});

describe('skipping to the top', () => {
  it('resolves the rows below the top and stops before one that would climb into it', async () => {
    const { skipToTop, initialReveal, nextIndex, nothingToReveal } = await import('./reveal');
    const row = (user_id: number, rank: number, total: number, pending: number) => ({
      user_id, name: `S${user_id}`, rank, total, last_improvement_s: user_id,
      cells: [{ label: 'A', score: total, pending, max: 100 }],
    });
    const frozen = { rows: [row(1, 1, 100, 0), row(2, 2, 50, 1), row(3, 3, 40, 1), row(4, 4, 10, 1)] };
    const full = { rows: [row(1, 1, 100, 0), row(2, 2, 60, 0), row(3, 3, 45, 0), row(4, 4, 15, 0)] };
    const start = initialReveal(frozen as never, full as never);
    // Below the top 2, rows 4 and 3 resolve quietly; row 2 is in the top, so it is shown.
    const s = skipToTop(start, 2);
    expect(s.rows[nextIndex(s)].user_id).toBe(2);
    expect(s.rows.filter((r) => r.resolved).map((r) => r.user_id).sort()).toEqual([1, 3, 4]);
    // A row that would climb into the top is never skipped.
    const climb = { rows: [row(1, 1, 100, 0), row(4, 2, 90, 0), row(2, 3, 50, 0), row(3, 4, 40, 0)] };
    const c = skipToTop(initialReveal(frozen as never, climb as never), 2);
    expect(c.rows[nextIndex(c)].user_id).toBe(4);
    expect(nothingToReveal(start)).toBe(false);
    expect(nothingToReveal(initialReveal(full as never, full as never))).toBe(true);
  });
});
