/**
 * "Your progress" as the web app sees it (contract C8, docs/API.md
 * "Students"; v2 frontend PLAN.md 6.12, decisions D92 to D96).
 *
 * The shapes mirror GET /api/me/progress and GET
 * /api/admin/users/{id}/progress. The server sends only contests whose
 * results are final for students (never a frozen or held board), already
 * without hidden tests, practice scores or the "ai draft" tag, and it
 * computes by_tag, by_difficulty, losses and retry. The web app still
 * derives those four from `contests` when a field is missing, so the page
 * never depends on the server's optional work, and the unit tests check
 * that the derived totals equal the server's.
 *
 * Everything else here is wording: percentages as whole numbers, the one
 * sentence each chart needs, the hidden sentence of each problem cell, and
 * the Home line (D92). Neutral words only: no praise, no warnings, no
 * comparison with anyone else (D94); the rank is a small muted fact (D95).
 */
import { verdictLabel } from './problems';
import { guidance } from './workspace';

/** A judge verdict code that can end a problem's best attempt. */
export type VerdictCode = 'AC' | 'WA' | 'TLE' | 'MLE' | 'RE' | 'CE' | string;

export interface ProgressSubtask {
  idx: number;
  points: number;
  earned: number;
}

/** One problem of one contest. In an exam-mode contest (`detail: "totals"`) the result fields are null. */
export interface ProgressProblem {
  label: string;
  title: string;
  problem_id: number;
  score: number | null;
  max_score: number;
  attempts: number | null;
  full_at_minutes: number | null;
  best_verdict: VerdictCode | null;
  subtasks?: ProgressSubtask[] | null;
  tags?: string[];
  difficulty?: string;
  practice_open?: boolean;
  upsolved?: boolean;
  zero_by_decision?: boolean;
}

export interface ProgressContest {
  contest_id?: number;
  slug: string;
  title: string;
  graded?: boolean;
  feedback_mode?: string;
  /** "full", or "totals" for an exam-mode contest. */
  detail?: 'full' | 'totals';
  ended_at: number;
  finalised_at?: number;
  score: number;
  max_score: number;
  percent?: number;
  rank: number | null;
  ranked_count: number | null;
  problems: ProgressProblem[];
}

export interface ProgressTotals {
  contests_taken: number;
  graded_taken?: number;
  points: number;
  max_points: number;
  problems_full: number;
  problems_partial: number;
  problems_zero: number;
  personal_best_percent?: number | null;
  upsolved?: number;
}

export interface TagRow {
  tag: string;
  earned: number;
  available: number;
  problems: number;
}

export interface DifficultyRow {
  difficulty: string;
  earned: number;
  available: number;
  problems: number;
}

export interface LossRow {
  verdict: VerdictCode;
  problems: number;
}

export interface RetryRow {
  slug: string;
  label: string;
  title: string;
  score: number;
  max_score: number;
  practice_open: boolean;
}

/** The C8 response. */
export interface Progress {
  user: { id: number; name: string };
  /** The person's own preference; absent on the organisers' route. */
  hide_progress?: boolean;
  generated_at?: number;
  totals: ProgressTotals;
  contests: ProgressContest[];
  by_tag?: TagRow[];
  by_difficulty?: DifficultyRow[];
  losses?: LossRow[];
  retry?: RetryRow[];
  server_now?: number;
}

/** The API path for the signed-in person's progress, or an organiser's view of one account. */
export function progressPath(userId?: string | number): string {
  return userId === undefined || userId === '' ? '/api/me/progress' : `/api/admin/users/${encodeURIComponent(String(userId))}/progress`;
}

/** Whether a contest shows per-problem results (not an exam-mode contest). */
export function isFull(c: Pick<ProgressContest, 'detail'>): boolean {
  return c.detail !== 'totals';
}

/** A share as a whole percentage, 0 when nothing was available. */
export function percentOf(earned: number, available: number): number {
  if (!(available > 0)) return 0;
  return Math.round((Math.max(0, earned) / available) * 100);
}

/** A contest's score as a whole percentage of its maximum. */
export function contestPercent(c: Pick<ProgressContest, 'score' | 'max_score'>): number {
  return percentOf(c.score, c.max_score);
}

/** Problems below full score with a result, in contests that show per-problem results. */
function belowFull(contests: readonly ProgressContest[]): { contest: ProgressContest; problem: ProgressProblem }[] {
  const out: { contest: ProgressContest; problem: ProgressProblem }[] = [];
  for (const c of contests) {
    if (!isFull(c)) continue;
    for (const p of c.problems) {
      if (p.score !== null && p.score < p.max_score) out.push({ contest: c, problem: p });
    }
  }
  return out;
}

/** Points by tag over the full contests, in first-seen order. */
export function deriveByTag(contests: readonly ProgressContest[]): TagRow[] {
  const rows = new Map<string, TagRow>();
  for (const c of contests) {
    if (!isFull(c)) continue;
    for (const p of c.problems) {
      for (const tag of p.tags ?? []) {
        if (tag.trim().toLowerCase() === 'ai draft') continue;
        const row = rows.get(tag) ?? { tag, earned: 0, available: 0, problems: 0 };
        row.earned += p.score ?? 0;
        row.available += p.max_score;
        row.problems++;
        rows.set(tag, row);
      }
    }
  }
  return [...rows.values()];
}

const DIFFICULTY_ORDER = ['easy', 'medium', 'hard'];

/** Points by difficulty over the full contests, in the order easy, medium, hard, then any other. */
export function deriveByDifficulty(contests: readonly ProgressContest[]): DifficultyRow[] {
  const rows = new Map<string, DifficultyRow>();
  for (const c of contests) {
    if (!isFull(c)) continue;
    for (const p of c.problems) {
      const d = (p.difficulty ?? '').trim();
      if (d === '') continue;
      const row = rows.get(d) ?? { difficulty: d, earned: 0, available: 0, problems: 0 };
      row.earned += p.score ?? 0;
      row.available += p.max_score;
      row.problems++;
      rows.set(d, row);
    }
  }
  return sortDifficulties([...rows.values()]);
}

/** Difficulty rows in their natural order, never by score. */
export function sortDifficulties(rows: readonly DifficultyRow[]): DifficultyRow[] {
  const rank = (d: string) => {
    const i = DIFFICULTY_ORDER.indexOf(d.toLowerCase());
    return i < 0 ? DIFFICULTY_ORDER.length : i;
  };
  return [...rows].sort((a, b) => rank(a.difficulty) - rank(b.difficulty));
}

/** Problems below full score by the verdict of their best attempt, largest first. */
export function deriveLosses(contests: readonly ProgressContest[]): LossRow[] {
  const counts = new Map<string, number>();
  for (const { problem } of belowFull(contests)) {
    const v = problem.best_verdict;
    if (!v || v === 'AC') continue;
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  return [...counts.entries()].map(([verdict, problems]) => ({ verdict, problems })).sort((a, b) => b.problems - a.problems);
}

/** Problems below full score, newest contest first (the server's `retry`). */
export function deriveRetry(contests: readonly ProgressContest[]): RetryRow[] {
  const sorted = [...contests].sort((a, b) => b.ended_at - a.ended_at);
  return belowFull(sorted).map(({ contest, problem }) => ({
    slug: contest.slug,
    label: problem.label,
    title: problem.title,
    score: problem.score ?? 0,
    max_score: problem.max_score,
    practice_open: problem.practice_open === true,
  }));
}

/** Totals worked out from the contests, to check against the server's. */
export function deriveTotals(contests: readonly ProgressContest[]): ProgressTotals {
  const t: ProgressTotals = { contests_taken: contests.length, points: 0, max_points: 0, problems_full: 0, problems_partial: 0, problems_zero: 0 };
  for (const c of contests) {
    t.points += c.score;
    t.max_points += c.max_score;
    if (!isFull(c)) continue;
    for (const p of c.problems) {
      if (p.score === null) continue;
      if (p.max_score > 0 && p.score >= p.max_score) t.problems_full++;
      else if (p.score > 0) t.problems_partial++;
      else t.problems_zero++;
    }
  }
  return t;
}

/** The response with every optional list filled in, contests newest first. */
export function withDerived(p: Progress): Required<Pick<Progress, 'by_tag' | 'by_difficulty' | 'losses' | 'retry'>> & Progress {
  const contests = [...(p.contests ?? [])].sort((a, b) => b.ended_at - a.ended_at);
  return {
    ...p,
    contests,
    by_tag: p.by_tag ?? deriveByTag(contests),
    by_difficulty: sortDifficulties(p.by_difficulty ?? deriveByDifficulty(contests)),
    losses: p.losses ?? deriveLosses(contests),
    retry: p.retry ?? deriveRetry(contests),
  };
}

/**
 * Topics weakest first, so the list reads as where to focus: by share
 * earned, then the topic with more points at stake, then by name.
 */
export function weakestFirst(rows: readonly TagRow[]): TagRow[] {
  return [...rows].sort((a, b) => {
    const pa = a.available > 0 ? a.earned / a.available : 1;
    const pb = b.available > 0 ? b.earned / b.available : 1;
    if (pa !== pb) return pa - pb;
    if (a.available !== b.available) return b.available - a.available;
    return a.tag.localeCompare(b.tag);
  });
}

/** Oldest first, for the line over time. */
export function chronological(contests: readonly ProgressContest[]): ProgressContest[] {
  return [...contests].sort((a, b) => a.ended_at - b.ended_at);
}

/** The line's one-sentence summary: "Scores over 5 contests, from 45 to 80 percent." */
export function scoreSummary(contests: readonly ProgressContest[]): string {
  if (contests.length === 0) return 'No contests yet.';
  const ps = contests.map(contestPercent);
  const lo = Math.min(...ps);
  const hi = Math.max(...ps);
  const n = contests.length;
  if (n === 1) return `Score in 1 contest: ${ps[0]} percent.`;
  const first = chronological(contests).map(contestPercent);
  const latest = first[first.length - 1];
  return lo === hi
    ? `Scores over ${n} contests, ${lo} percent in each.`
    : `Scores over ${n} contests, from ${lo} to ${hi} percent. Latest ${latest} percent.`;
}

/** A point's tooltip and table row: "Week 6 contest: 240 of 300 (80 percent)". */
export function pointLabel(c: Pick<ProgressContest, 'title' | 'score' | 'max_score'>): string {
  return `${c.title}: ${c.score} of ${c.max_score} (${contestPercent(c)} percent)`;
}

/** A short name for the x axis: "Week 6" from "Week 6 contest", at most 12 characters. */
export function shortTitle(title: string): string {
  const t = title.replace(/\s+contest$/i, '').trim();
  return t.length <= 12 ? t : `${t.slice(0, 11).trimEnd()}…`;
}

/** A verdict code as words inside a sentence: "wrong answer". */
export function verdictWords(code: string | null | undefined): string {
  if (!code) return 'no judged submission';
  return verdictLabel(code).toLowerCase();
}

/** The hidden sentence of a problem cell: "Problem B, 40 of 100, 3 attempts, best result wrong answer." */
export function cellSentence(p: ProgressProblem): string {
  if (p.score === null) return `Problem ${p.label}, ${p.title}: totals only in this contest.`;
  let s = `Problem ${p.label}, ${p.score} of ${p.max_score}`;
  if (p.zero_by_decision) return `${s}, set to zero by a review decision.`;
  if (p.attempts !== null && p.attempts !== undefined) s += `, ${p.attempts === 1 ? '1 attempt' : `${p.attempts} attempts`}`;
  if (p.best_verdict && !(p.max_score > 0 && p.score >= p.max_score)) s += `, best result ${verdictWords(p.best_verdict)}`;
  if (p.upsolved) s += ', solved later in practice';
  return `${s}.`;
}

/** The tone of a problem's score: full, partial, zero, or none for totals only. */
export function problemTone(p: Pick<ProgressProblem, 'score' | 'max_score'>): 'ok' | 'part' | 'zero' | 'none' {
  if (p.score === null) return 'none';
  if (p.max_score > 0 && p.score >= p.max_score) return 'ok';
  return p.score > 0 ? 'part' : 'zero';
}

/** The rank as a small fact: "12th of 140", or "" without one (D95). */
export function rankText(c: Pick<ProgressContest, 'rank' | 'ranked_count'>): string {
  if (c.rank === null || c.rank === undefined || !c.ranked_count) return '';
  return `${c.rank} of ${c.ranked_count}`;
}

/** The one line of guidance under the largest loss, reused from the workspace's verdict guidance. */
export function lossGuidance(verdict: string): string {
  return guidance(verdict, '')[0] ?? '';
}

/**
 * The Home line (D92): "Across 5 contests: 820 of 1500 points." Null when
 * there is nothing final yet or the person hid their progress.
 */
export function homeLine(p: Pick<Progress, 'totals' | 'contests' | 'hide_progress'> | null): { contests: number; points: number; max: number } | null {
  if (!p || p.hide_progress || (p.contests ?? []).length === 0) return null;
  return { contests: p.totals.contests_taken, points: p.totals.points, max: p.totals.max_points };
}

/** The progress entry of one contest, if its results are final and the person took part. */
export function contestEntry(p: Pick<Progress, 'contests'> | null, slug: string): ProgressContest | null {
  return p?.contests.find((c) => c.slug === slug) ?? null;
}

/** The one problem to practise next from a contest: its lowest share below full score, or null. */
export function practiseNextIn(c: ProgressContest | null): ProgressProblem | null {
  if (!c || !isFull(c)) return null;
  const below = c.problems.filter((p) => p.score !== null && p.score < p.max_score && !p.zero_by_decision);
  if (below.length === 0) return null;
  return [...below].sort((a, b) => (a.score ?? 0) / (a.max_score || 1) - (b.score ?? 0) / (b.max_score || 1))[0];
}

/** "Week 6 contest" from a slug, through the contests in the response; the slug when unknown. */
export function contestTitle(p: Pick<Progress, 'contests'>, slug: string): string {
  return p.contests.find((c) => c.slug === slug)?.title ?? slug;
}

/** The path of a contest problem in the web app. */
export function problemHref(slug: string, label: string): string {
  return `/contests/${encodeURIComponent(slug)}/problems/${encodeURIComponent(label)}`;
}

/** Problems set to zero by a review decision, newest contest first. */
export function zeroedProblems(contests: readonly ProgressContest[]): { contest: ProgressContest; problem: ProgressProblem }[] {
  const out: { contest: ProgressContest; problem: ProgressProblem }[] = [];
  for (const c of contests) for (const p of c.problems) if (p.zero_by_decision) out.push({ contest: c, problem: p });
  return out;
}
