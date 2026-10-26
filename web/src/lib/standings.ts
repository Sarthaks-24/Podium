/**
 * Standings as the API returns them, and how each part reads on screen
 * (cache/v1/m4-plan.md section 3, design.md section 5.4).
 *
 * The server ranks; the web app only draws. Each problem cell is a score,
 * a two-tone fill (earned fraction over the rest) and, on a frozen board,
 * "+N" for submissions made after the freeze. A cell nobody tried shows a
 * plain hyphen. Every cell also has a sentence for screen readers, so
 * "70+1" is heard as "Problem D, 70 of 100, 1 pending".
 */

/** One problem cell. tried is optional; without it a cell counts as tried when it scored or has pending. */
export interface StandingsCell {
  label: string;
  score: number;
  max: number;
  pending: number;
  tried?: boolean;
}

export interface StandingsRow {
  rank: number;
  user_id: number;
  name: string;
  /** Only on the organisers' board for a site admin: the console tells students apart by it. */
  email?: string;
  total: number;
  /** Seconds from the start (or the student's own start) to the final total. */
  last_improvement_s: number;
  cells: StandingsCell[];
}

/** GET /api/contests/{slug}/standings and the organiser variants. */
export interface Standings {
  variant: 'full' | 'frozen';
  version: number;
  frozen_at: number | null;
  final: boolean;
  judging_remaining: number;
  rows: StandingsRow[];
  me: number | null;
  labels?: string[];
  server_now?: number;
}

/** The problem labels for the header: the board's own, else the first row's cells, else the fallback. */
export function boardLabels(s: Pick<Standings, 'labels' | 'rows'>, fallback: readonly string[] = []): string[] {
  if (s.labels && s.labels.length > 0) return [...s.labels];
  const first = s.rows.find((r) => r.cells.length > 0);
  if (first) return first.cells.map((c) => c.label);
  return [...fallback];
}

/** The cell for a label in a row, if any. */
export function cellFor(row: Pick<StandingsRow, 'cells'>, label: string): StandingsCell | undefined {
  return row.cells.find((c) => c.label === label);
}

/** True when a cell was attempted. */
export function isTried(cell: StandingsCell | undefined): boolean {
  if (!cell) return false;
  if (typeof cell.tried === 'boolean') return cell.tried || cell.pending > 0;
  return cell.score > 0 || cell.pending > 0;
}

/** ok full marks, part some points, zero tried for no points, none not tried (or only pending). */
export type CellTone = 'ok' | 'part' | 'zero' | 'none';

/** What a cell shows. */
export interface CellView {
  /** The score, or "-" when not tried, or "" when only pending. */
  text: string;
  /** "+2" for pending submissions after the freeze, or "". */
  pending: string;
  /** Earned fraction from 0 to 1, for the two-tone fill. */
  fraction: number;
  tone: CellTone;
  /** The cell as a sentence. */
  sentence: string;
}

/** How a cell reads. */
export function cellView(cell: StandingsCell | undefined, label: string): CellView {
  if (!isTried(cell) || !cell) {
    return { text: '-', pending: '', fraction: 0, tone: 'none', sentence: `Problem ${label}, not tried` };
  }
  const fraction = cell.max > 0 ? Math.min(1, Math.max(0, cell.score / cell.max)) : 0;
  // A cell whose only attempts came after the freeze reads "+2" alone.
  const text = cell.score > 0 || cell.pending === 0 ? String(cell.score) : '';
  const tone: CellTone = cell.max > 0 && cell.score >= cell.max ? 'ok' : cell.score > 0 ? 'part' : text === '0' ? 'zero' : 'none';
  const pending = cell.pending > 0 ? `+${cell.pending}` : '';
  let sentence = `Problem ${label}, `;
  sentence += text === '' ? 'no score before the freeze' : `${cell.score} of ${cell.max}`;
  if (cell.pending > 0) sentence += `, ${cell.pending} pending`;
  return { text, pending, fraction, tone, sentence };
}

/** Seconds as "1:02:03" or "12:03". */
export function formatElapsed(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  const mm = String(m).padStart(h > 0 ? 2 : 1, '0');
  return h > 0 ? `${h}:${mm}:${String(r).padStart(2, '0')}` : `${mm}:${String(r).padStart(2, '0')}`;
}

function clock(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/**
 * The line under the heading: frozen, final or live, and what is still
 * judging. `ended` says the contest is over: a board still frozen then is
 * held for the reveal, and promising an update "when the contest ends"
 * would be wrong.
 */
export function statusLine(
  s: Pick<Standings, 'variant' | 'frozen_at' | 'final' | 'judging_remaining'>,
  ended = false,
): string {
  let line: string;
  if (s.variant === 'frozen') {
    const at = s.frozen_at ? `Frozen at ${clock(s.frozen_at)}.` : 'Frozen.';
    line = ended ? `${at} The final standings appear when the organisers release them.` : `${at} Updates when the contest ends.`;
  } else if (s.final) {
    line = 'Final standings.';
  } else {
    line = 'Live. Updates as submissions are judged.';
  }
  if (s.judging_remaining > 0) {
    line += s.judging_remaining === 1 ? ' 1 submission is still being judged.' : ` ${s.judging_remaining} submissions are still being judged.`;
  }
  return line;
}

/** The signed-in student's own row, if they are on the board. */
export function myRow(s: Pick<Standings, 'rows' | 'me'>): StandingsRow | undefined {
  if (s.me === null || s.me === undefined) return undefined;
  return s.rows.find((r) => r.user_id === s.me);
}

/**
 * The student's place as a sentence. On a frozen board it gives the place
 * only, never the frozen total, which would contradict the student's own
 * live score printed beside it.
 */
export function placeSentence(s: Pick<Standings, 'rows' | 'me'> & Partial<Pick<Standings, 'variant'>>, ended = false): string {
  const row = myRow(s);
  if (!row) return 'You are not in the standings.';
  const tied = s.rows.filter((r) => r.rank === row.rank).length > 1;
  const place = `${tied ? 'tied ' : ''}${ordinalWord(row.rank)} of ${s.rows.length}`;
  if (s.variant === 'frozen') {
    return ended
      ? `In the frozen standings you are ${place}. Your final place shows when the organisers release the results.`
      : `In the frozen standings you are ${place}.`;
  }
  return `You are ${place} with ${row.total} points.`;
}

/**
 * The own-place figure on the standings page (v2 S6, D80: Direction A with
 * B's one display figure): the rank, the board size, whether it is a tie
 * and the label above it. Null when the student is not on the board.
 */
export function placeFigure(s: Pick<Standings, 'rows' | 'me'> & Partial<Pick<Standings, 'variant'>>): { rank: number; of: number; tied: boolean; label: string } | null {
  const row = myRow(s);
  if (!row) return null;
  const tied = s.rows.filter((r) => r.rank === row.rank).length > 1;
  return { rank: row.rank, of: s.rows.length, tied, label: s.variant === 'frozen' ? 'Your place on the frozen board' : 'Your place' };
}

/**
 * The sentence a screen reader hears when the student's own place or score
 * changes after a refetch (S6, audit 06-6): "You are now 12th with 240
 * points." On a frozen board only the place moves. Empty when nothing
 * changed or the student is not on both boards.
 */
export function rankAnnouncement(prev: Pick<Standings, 'rows' | 'me' | 'variant'> | null, next: Pick<Standings, 'rows' | 'me' | 'variant'>): string {
  if (!prev) return '';
  const a = myRow(prev);
  const b = myRow(next);
  if (!a || !b) return '';
  if (a.rank === b.rank && (next.variant === 'frozen' || a.total === b.total)) return '';
  const tied = next.rows.filter((r) => r.rank === b.rank).length > 1 ? 'tied ' : '';
  if (next.variant === 'frozen') return `You are now ${tied}${ordinalWord(b.rank)} in the frozen standings.`;
  return `You are now ${tied}${ordinalWord(b.rank)} with ${b.total} points.`;
}

/** 1st, 2nd, 3rd, 4th, 11th, 12th, 13th, 21st. */
export function ordinalWord(n: number): string {
  const tens = n % 100;
  if (tens >= 11 && tens <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

/**
 * How the console names a student in its pickers: the board's name with the
 * email beside it, since two students may share a name and an account with
 * no name or roster entry is only "Student 12". Without an email (an older
 * server, a co-organiser) the name alone.
 */
export function studentChoice(row: Pick<StandingsRow, 'name' | 'email'>): string {
  const email = row.email?.trim() ?? '';
  if (email === '') return row.name;
  if (row.name.trim() === '' || row.name === email || /^Student \d+$/.test(row.name)) return email;
  return `${row.name}, ${email}`;
}
