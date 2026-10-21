/**
 * The organisers' students pages as the web app sees them (contracts C9
 * and C12, docs/API.md "C9: organisers' students pages", "C9 and C12:
 * Standing", "C9: private notes and manual cases"; v2 P4 F9.2b to F9.5).
 *
 * Shapes: StudentsList from GET /api/admin/students, StudentDetail from
 * GET /api/admin/students/{id}, ClassView from GET
 * /api/admin/contests/{id}/class, StudentRecord from GET
 * /api/admin/students/{id}/record, Notes from GET
 * /api/admin/submissions/{id}/notes, Oversight and RecordViews from the
 * owner's routes. Only organisers ever load these; the rating number lives
 * here and nowhere a student page imports (lib/standing.ts has no field for
 * it, and guardrails.test.ts checks that no student route imports this
 * module).
 *
 * The rest is wording and small maths: the rating in tenths shown as
 * "8.5", the band a value falls in, the line that crossed a band, the
 * confirmation of an edit that names the old and new band, the checks the
 * edit form runs before it sends, sorting the students table, and parsing
 * the line ranges of a private note.
 */
import type { CaseAppeal } from './review';
import { bandName, type Band, type Standing, type StandingLine } from './standing';

/** One row of the students table. */
export interface StudentRow {
  id: number;
  name: string;
  email: string;
  graded_contests: number;
  last_graded_percent: number | null;
  /** null when the student took no graded contest (Go *string). */
  last_graded_slug: string | null;
  last_submission_at: number | null;
  rating_tenths: number;
}

export type StudentFilter = '' | 'missed_last_two' | 'no_accepted';

/** GET /api/admin/students. */
export interface StudentsList {
  students: StudentRow[];
  /** The filter as asked, "" for none (Go echoes the query). */
  filter: StudentFilter;
  last_graded: { contest_id: number; slug: string; title: string }[];
  server_now?: number;
}

/** One contest on the student page, newest first. */
export interface StudentContest {
  contest_id: number;
  slug: string;
  title: string;
  graded: boolean;
  ended_at: number;
  score: number;
  max_score: number;
  percent: number;
  class_median_percent: number | null;
  rank: number;
  ranked_count: number;
  problems_attempted: number;
  problems_full: number;
  submissions: number;
  accepted: number;
}

/** GET /api/admin/students/{id}. */
export interface StudentDetail {
  student: { id: number; name: string; email: string };
  contests: StudentContest[];
  server_now?: number;
}

export interface ClassSubtask {
  idx: number;
  points: number;
  solved: number;
  solve_rate: number;
}

export interface ClassProblem {
  label: string;
  title: string;
  max_score: number;
  attempted: number;
  full: number;
  median_score: number;
  subtasks?: ClassSubtask[];
  most_failed_test?: { subtask_idx: number; test: number; failed: number } | null;
}

/** GET /api/admin/contests/{id}/class: totals only, never a name. */
export interface ClassView {
  contest_id: number;
  participants: number;
  problems: ClassProblem[];
  server_now?: number;
}

/** A person named on an organiser route. */
export interface PersonRef {
  id: number;
  name: string;
  email?: string;
}

/** One line of the rating's history, newest first. */
export interface RatingLine {
  id: number;
  at: number;
  delta_tenths: number;
  value_tenths: number;
  cause: 'outcome' | 'manual' | 'appeal' | 'rollover';
  reason: string;
  case_id: number | null;
  decision_id: number | null;
  appeal_id: number | null;
  actor: PersonRef | null;
  confirmer: PersonRef | null;
  semester_label: string;
  band_before: Band;
  band_after: Band;
}

export interface Rating {
  value_tenths: number;
  band: Band;
  history: RatingLine[];
}

/** A band-lowering edit waiting for a second organiser. */
export interface PendingRating {
  id: number;
  from_tenths: number;
  to_tenths: number;
  reason: string;
  by: PersonRef;
  created_at: number;
  may_confirm: boolean;
}

/** A case on the record page. */
export interface RecordCase {
  id: number;
  contest_id: number;
  contest_title: string;
  state: string;
  source: 'signals' | 'manual';
  /** "" while undecided or cleared (Go string). */
  outcome: string;
  created_at: number;
  decided_at: number | null;
}

/** A private note on a submission. */
export interface Note {
  id: number;
  submission_id: number;
  by: number;
  by_name: string;
  text: string;
  lines: { from: number; to: number }[];
  created_at: number;
  /** When the note is deleted; null once a case was opened from the submission. */
  expires_at: number | null;
}

/** GET /api/admin/students/{id}/record. */
export interface StudentRecord {
  student: { id: number; name: string; email: string; disabled: boolean };
  standing: Pick<Standing, 'band' | 'band_name' | 'lines'>;
  rating: Rating;
  pending_rating: PendingRating | null;
  cases: RecordCase[];
  notes: Note[];
  appeals: CaseAppeal[];
  server_now?: number;
}

/** The answer to a rating edit or confirmation. */
export interface RatingAnswer {
  rating: Rating;
  pending_rating: PendingRating | null;
}

/** GET /api/admin/submissions/{id}/notes. */
export interface Notes {
  submission: { id: number; contest_id: number; user_id: number; problem_label: string; language: string; practice: boolean; created_at: number };
  /** The program, as plain text (shown in CodeView, never as HTML). */
  source?: string;
  notes: Note[];
  case_id: number | null;
  server_now?: number;
}

/** One reviewer on the owner's oversight table. */
export interface ReviewerLoad {
  user_id: number;
  name: string;
  email: string;
  manual_opened: number;
  cleared: number;
  actioned: number;
  overdue: number;
  proposals: number;
  confirmations: number;
  rulings: number;
}

/** One row of the record view log. */
export interface RecordView {
  id: number;
  at: number;
  kind: string;
  /** The case or submission read, 0 for a whole record (Go int64). */
  ref_id: number;
  viewer: { id: number; name: string; email: string };
  subject: { id: number; name: string };
}

/** The rating's range in tenths, and the reason's limits (docs/API.md "Record page"). */
export const RATING_MAX = 100;
export const MIN_RATING_REASON = 20;
export const MAX_RATING_REASON = 1000;

/** The rating in tenths as people read it: 85 is "8.5", 100 is "10". */
export function ratingText(tenths: number): string {
  const t = Math.round(tenths);
  return t % 10 === 0 ? String(t / 10) : (t / 10).toFixed(1);
}

/** A change in tenths with its sign: "-1.5", "+0.5". */
export function deltaText(tenths: number): string {
  if (tenths === 0) return '0';
  return `${tenths > 0 ? '+' : '-'}${ratingText(Math.abs(tenths))}`;
}

/** The band a rating falls in: 9 to 10 good, 6 to 8.9 on record, below 6 repeated. */
export function bandOf(tenths: number): Band {
  if (tenths >= 90) return 'good';
  return tenths >= 60 ? 'on_record' : 'repeated';
}

/** The band's name from its id. */
export function bandWord(band: Band): string {
  return bandName({ band });
}

/** How far down the bands go, so "lowers" can be decided by order. */
const BAND_ORDER: Record<Band, number> = { good: 2, on_record: 1, repeated: 0 };

/** Whether moving from one value to another drops the band (that edit waits for a second organiser). */
export function lowersBand(fromTenths: number, toTenths: number): boolean {
  return BAND_ORDER[bandOf(toTenths)] < BAND_ORDER[bandOf(fromTenths)];
}

/** The sentence on a history line that crossed a band, or "" for one that did not. */
export function crossingText(line: Pick<RatingLine, 'band_before' | 'band_after'>): string {
  if (line.band_before === line.band_after) return '';
  const down = BAND_ORDER[line.band_after] < BAND_ORDER[line.band_before];
  return `${down ? 'Dropped' : 'Rose'} from ${bandWord(line.band_before)} to ${bandWord(line.band_after)}`;
}

/** What caused a history line, in words. */
export function causeWord(cause: string): string {
  switch (cause) {
    case 'outcome':
      return 'Outcome confirmed';
    case 'manual':
      return 'Edited by an organiser';
    case 'appeal':
      return 'Appeal upheld';
    case 'rollover':
      return 'Semester rollover';
    default:
      return cause;
  }
}

/**
 * Reads the rating field ("7", "7.5", "7,5") into tenths. Returns the value
 * or the sentence that says what to fix.
 */
export function parseRating(text: string): { tenths: number } | { error: string } {
  const t = text.trim().replace(',', '.');
  if (t === '') return { error: 'Enter the new rating, from 0 to 10.' };
  if (!/^\d+(\.\d)?$/.test(t)) return { error: 'Enter a number from 0 to 10 with at most one decimal, such as 7.5.' };
  const tenths = Math.round(Number(t) * 10);
  if (tenths > RATING_MAX) return { error: 'The rating goes up to 10.' };
  return { tenths };
}

/** Characters as the server counts them: code points of the trimmed text. */
function length(text: string): number {
  return [...text.trim()].length;
}

/** What is wrong with an edit's reason, or "". The reason is required: the student reads it. */
export function ratingReasonProblem(reason: string): string {
  const n = length(reason);
  if (n === 0) return 'Write the reason first. The student reads it on their standing.';
  if (n < MIN_RATING_REASON) return `Write at least ${MIN_RATING_REASON} characters, so the student can understand the change.`;
  if (n > MAX_RATING_REASON) return `Keep the reason to ${MAX_RATING_REASON} characters.`;
  return '';
}

/** The checks the edit form runs before it asks: one sentence per field, empty when the field is fine. */
export function ratingEditProblems(valueText: string, reason: string, currentTenths: number): { value: string; reason: string } {
  const parsed = parseRating(valueText);
  let value = 'error' in parsed ? parsed.error : '';
  if (value === '' && 'tenths' in parsed && parsed.tenths === currentTenths) value = `The rating is already ${ratingText(currentTenths)}. Enter a different value.`;
  return { value, reason: ratingReasonProblem(reason) };
}

/**
 * The confirmation of a rating edit: it names the old and new value and
 * band, and says when the change waits for a second organiser.
 */
export function ratingConfirm(name: string, fromTenths: number, toTenths: number): { title: string; body: string; items: string[]; label: string; waits: boolean } {
  const waits = lowersBand(fromTenths, toTenths);
  const from = bandWord(bandOf(fromTenths));
  const to = bandWord(bandOf(toTenths));
  const items = [
    from === to ? `The band stays ${from}.` : `The band goes from ${from} to ${to}.`,
    'The student sees a dated line, "Standing adjusted by an organiser", with your reason, and can appeal it within 14 days.',
  ];
  if (waits) items.push('The band drops, so the change waits until a second owner or admin confirms it.');
  return {
    title: `Change ${name}'s rating from ${ratingText(fromTenths)} to ${ratingText(toTenths)}?`,
    body: waits ? 'Nothing changes for the student until a second organiser confirms.' : 'The change applies at once.',
    items,
    label: waits ? 'Propose the change' : 'Change the rating',
    waits,
  };
}

/** A standing line on the record page: its kind in words. */
export function standingLineWord(line: Pick<StandingLine, 'kind'>): string {
  return line.kind === 'outcome' ? 'Outcome' : 'Adjustment';
}

/** The students table filters, with their words. */
export const FILTERS: readonly { id: StudentFilter; label: string }[] = [
  { id: '', label: 'Every student' },
  { id: 'missed_last_two', label: 'Missed the last 2 graded contests' },
  { id: 'no_accepted', label: '10 or more submissions, nothing accepted' },
];

/** The sentence under the filter: which contests it looked at. */
export function filterHelp(filter: StudentFilter, lastGraded: readonly { title: string }[]): string {
  if (filter === '') return 'Sorted by name. Practice never counts towards a filter.';
  const titles = lastGraded.map((c) => c.title);
  if (titles.length === 0) return 'No graded contest is final yet, so nobody matches.';
  const which = titles.length === 1 ? titles[0] : `${titles.slice(0, -1).join(', ')} and ${titles[titles.length - 1]}`;
  if (filter === 'missed_last_two') return `Students who could enter ${which} and are on neither board. Practice does not count.`;
  return `Students with 10 or more submissions in ${which} and none accepted. Practice does not count.`;
}

export type SortKey = 'name' | 'rating';

/** Sorts the students table; names compare as people read them, equal ratings fall back to the name. */
export function sortStudents(rows: readonly StudentRow[], key: SortKey, dir: 'asc' | 'desc'): StudentRow[] {
  const byName = (a: StudentRow, b: StudentRow) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id - b.id;
  const sign = dir === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => (key === 'name' ? sign * byName(a, b) : sign * (a.rating_tenths - b.rating_tenths) || byName(a, b)));
}

/** A share of participants as a whole percent for a meter, and its words: "62 percent". */
export function percentText(rate: number): string {
  const r = Math.round(rate * 10) / 10;
  return Number.isInteger(r) ? String(r) : r.toFixed(1);
}

/** One point of the student page's trend: the student's percent and the class median, oldest first. */
export interface TrendPoint {
  slug: string;
  title: string;
  ended_at: number;
  percent: number;
  median: number | null;
}

/** The graded contests of a student page, oldest first, for the trend line. */
export function trendPoints(contests: readonly StudentContest[]): TrendPoint[] {
  return contests
    .filter((c) => c.graded)
    .slice()
    .sort((a, b) => a.ended_at - b.ended_at)
    .map((c) => ({ slug: c.slug, title: c.title, ended_at: c.ended_at, percent: c.percent, median: c.class_median_percent }));
}

/** The one-sentence summary of the trend, for the chart's accessible name. */
export function trendSummary(name: string, points: readonly TrendPoint[]): string {
  if (points.length === 0) return `${name} has no final graded contest yet.`;
  const last = points[points.length - 1];
  const vs =
    last.median === null
      ? ''
      : last.percent === last.median
        ? ', the same as the class median'
        : `, ${last.percent > last.median ? 'above' : 'below'} the class median of ${percentText(last.median)} percent`;
  const n = points.length === 1 ? 'one graded contest' : `${points.length} graded contests`;
  return `Score as a percentage over ${n}. Latest, ${last.title}: ${percentText(last.percent)} percent${vs}.`;
}

/**
 * Line ranges for a private note, from "7" or "3-5, 12": 1-based, each
 * from <= to, at most 20. Returns the ranges or what to fix; empty text is
 * no ranges.
 */
export function parseLines(text: string): { lines: { from: number; to: number }[] } | { error: string } {
  const t = text.trim();
  if (t === '') return { lines: [] };
  const lines: { from: number; to: number }[] = [];
  for (const part of t.split(/\s*[,;]\s*|\s+/).filter((p) => p !== '')) {
    const m = /^(\d+)(?:\s*-\s*(\d+))?$/.exec(part);
    if (!m) return { error: 'Write lines as numbers or ranges, such as 7 or 3-5, 12.' };
    const from = Number(m[1]);
    const to = m[2] ? Number(m[2]) : from;
    if (from < 1) return { error: 'Lines start at 1.' };
    if (to < from) return { error: `In ${part}, the first line comes first.` };
    lines.push({ from, to });
  }
  if (lines.length > 20) return { error: 'Highlight at most 20 ranges.' };
  return { lines };
}

/** "lines 3 to 5 and 12" for a note's ranges, or "" without any. */
export function linesText(lines: readonly { from: number; to: number }[]): string {
  if (lines.length === 0) return '';
  const words = lines.map((r) => (r.from === r.to ? String(r.from) : `${r.from} to ${r.to}`));
  const joined = words.length === 1 ? words[0] : `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
  return `${lines.length === 1 && lines[0].from === lines[0].to ? 'line' : 'lines'} ${joined}`;
}

/** The record view log's kinds, in words. */
export function viewKindWord(kind: string): string {
  switch (kind) {
    case 'record':
      return 'Record page';
    case 'case':
      return 'Case';
    case 'notes':
      return 'Notes on a submission';
    case 'appeal':
      return 'Appeal';
    default:
      return kind;
  }
}
