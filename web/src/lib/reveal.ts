/**
 * The reveal walk for the projector (cache/v1/m4-plan.md section 3,
 * design.md section 5.5).
 *
 * It starts from the frozen board. Each step resolves the lowest row that
 * still has something to show: its cells take their final values, the
 * pending counts go, and the row moves to the place its final total earns.
 * Rows with nothing pending and nothing changed are resolved from the start,
 * so every key press changes something. Ranks are recomputed after each
 * step the way the server ranks (total, then the earlier time to reach it;
 * equal totals and times share a rank), so when the last row resolves the
 * board equals the full standings.
 */
import type { Standings, StandingsCell, StandingsRow } from './standings';

export interface RevealRow {
  user_id: number;
  name: string;
  rank: number;
  total: number;
  last_improvement_s: number;
  cells: StandingsCell[];
  /** True once the row shows its final values. */
  resolved: boolean;
  /** The final row from the full board. */
  final: StandingsRow;
  /** The row's place on the full board, which settles ties the way the server listed them. */
  order: number;
}

export interface RevealState {
  rows: RevealRow[];
  /** Steps taken so far. */
  step: number;
}

/** One step's effect, for the animation: the row and its place before and after. */
export interface RevealMove {
  user_id: number;
  from: number;
  to: number;
  rankFrom: number;
  rankTo: number;
}

function sameCells(a: readonly StandingsCell[], b: readonly StandingsCell[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((c, i) => c.label === b[i].label && c.score === b[i].score && c.pending === 0);
}

/** Orders rows as the server does: total down, then time up, then the full board's order. */
export function compareRows(a: RevealRow, b: RevealRow): number {
  return b.total - a.total || a.last_improvement_s - b.last_improvement_s || a.order - b.order;
}

/** Competition ranks: equal totals and times share a rank, the next rank skips. */
export function assignRanks(rows: RevealRow[]): RevealRow[] {
  let rank = 0;
  return rows.map((r, i) => {
    const prev = rows[i - 1];
    if (!prev || prev.total !== r.total || prev.last_improvement_s !== r.last_improvement_s) rank = i + 1;
    return r.rank === rank ? r : { ...r, rank };
  });
}

/** The starting state: the frozen board, with rows missing from it added at zero. */
export function initialReveal(frozen: Pick<Standings, 'rows'>, full: Pick<Standings, 'rows'>): RevealState {
  const byUser = new Map(frozen.rows.map((r) => [r.user_id, r]));
  const rows: RevealRow[] = full.rows.map((fin, order) => {
    const before = byUser.get(fin.user_id);
    const cells = before
      ? before.cells.map((c) => ({ ...c }))
      : fin.cells.map((c) => ({ label: c.label, score: 0, max: c.max, pending: c.score > 0 ? 1 : 0 }));
    const total = before ? before.total : 0;
    const time = before ? before.last_improvement_s : 0;
    const unchanged = total === fin.total && time === fin.last_improvement_s && sameCells(cells, fin.cells);
    return {
      user_id: fin.user_id,
      name: fin.name,
      rank: 0,
      total,
      last_improvement_s: time,
      cells,
      resolved: unchanged,
      final: fin,
      order,
    };
  });
  return { rows: assignRanks(rows.sort(compareRows)), step: 0 };
}

/** True when every row shows its final values. */
export function isDone(state: RevealState): boolean {
  return state.rows.every((r) => r.resolved);
}

/** The index of the row the next step resolves: the lowest unresolved one, or -1. */
export function nextIndex(state: RevealState): number {
  for (let i = state.rows.length - 1; i >= 0; i--) if (!state.rows[i].resolved) return i;
  return -1;
}

/** Resolves the lowest unresolved row. Returns the new state and the move, or null when done. */
export function revealStep(state: RevealState): { state: RevealState; move: RevealMove } | null {
  const i = nextIndex(state);
  if (i < 0) return null;
  const row = state.rows[i];
  const fin = row.final;
  const resolved: RevealRow = {
    ...row,
    total: fin.total,
    last_improvement_s: fin.last_improvement_s,
    cells: fin.cells.map((c) => ({ ...c, pending: 0 })),
    resolved: true,
  };
  const others = state.rows.filter((_, j) => j !== i);
  const rows = assignRanks([...others, resolved].sort(compareRows));
  const to = rows.findIndex((r) => r.user_id === row.user_id);
  return {
    state: { rows, step: state.step + 1 },
    move: { user_id: row.user_id, from: i, to, rankFrom: row.rank, rankTo: rows[to].rank },
  };
}

/** Runs the walk to the end; the final order and ranks. */
export function revealAll(state: RevealState): RevealState {
  let s = state;
  for (let next = revealStep(s); next; next = revealStep(s)) s = next.state;
  return s;
}

/**
 * Resolves, without steps to show, every row still to come that is placed
 * below `top` (C6 "Skip to the top 10"), so the room goes straight to the
 * rows that matter. Rows that would climb into the top stay to be shown.
 */
export function skipToTop(state: RevealState, top = 10): RevealState {
  let s = state;
  for (;;) {
    const i = nextIndex(s);
    if (i < 0 || s.rows[i].rank <= top) return s;
    const next = revealStep(s);
    if (!next) return s;
    if (next.state.rows[next.move.to].rank <= top) return s;
    s = next.state;
  }
}

/** True when the frozen board already equals the full one: nothing is left to reveal. */
export function nothingToReveal(state: RevealState): boolean {
  return state.step === 0 && isDone(state);
}
