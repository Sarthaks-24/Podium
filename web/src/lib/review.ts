/**
 * Integrity review as the web app sees it (cache/v1/integrity-plan.md
 * sections 5 and 7, docs/INTEGRITY.md).
 *
 * The shapes mirror the API: Run from GET /api/admin/contests/{id}/integrity,
 * PairSide from .../signals/{signal}/pair, Moment from .../timeline/{user},
 * Case from the case routes, and StudentCase from GET /api/integrity/cases.
 *
 * The rest is pure wording and maths. Signals are reasons to look, never
 * proof, so they are worded as strength ("Strong", "Moderate", "Weak") with
 * the raw number beside it, and their kind in words. Matched spans arrive
 * as byte offsets into the UTF-8 source; codeLines() turns them into
 * per-line segments of the JavaScript string (UTF-16), so a multibyte
 * character before a span does not shift the highlight. Clusters are
 * groups of students linked by similar-code signals on one problem,
 * worked out here with union-find (the API reports only their number).
 */
import { languageLabel } from './workspace';

export type SignalKind = 'similar' | 'wrong_output' | 'hardcoded' | 'probing';
export type Strength = 'strong' | 'moderate' | 'weak';
export type RunState = 'running' | 'done' | 'error';
export type CaseState = 'open' | 'awaiting_student' | 'cleared' | 'actioned';

/** A byte range in a UTF-8 source: start inclusive, end exclusive. */
export interface Span {
  start: number;
  end: number;
}

/** Evidence by kind; every field is optional because it depends on the kind. */
export interface SignalEvidence {
  shared?: number;
  spans_a?: Span[];
  spans_b?: Span[];
  language_a?: string;
  language_b?: string;
  tests?: number[];
  distinct?: number;
  attempts?: number;
}

export interface Signal {
  id: number;
  problem_id: number;
  kind: SignalKind;
  strength: Strength;
  score: number;
  user_a: number;
  user_b?: number;
  submission_a?: number;
  submission_b?: number;
  evidence: SignalEvidence | null;
}

export interface RunStats {
  students: number;
  submissions: number;
  problems: number;
  signals: Partial<Record<SignalKind, number>>;
  clusters: number;
}

export interface ReviewUser {
  name: string;
  email: string;
  roll_number?: string;
}

export interface ReviewProblem {
  label: string;
  title: string;
}

/** GET /api/admin/contests/{id}/integrity. */
export interface Run {
  id: number;
  state: RunState;
  started_at: number;
  finished_at: number | null;
  error?: string;
  stats: RunStats;
  signals: Signal[];
  users?: Record<string, ReviewUser>;
  problems?: Record<string, ReviewProblem>;
  server_now?: number;
}

/** One program of a compared pair. */
export interface PairSide {
  submission_id: number;
  user_id: number;
  language: string;
  source: string;
  created_at: number;
  spans: Span[];
}

export type MomentKind = 'submission' | 'paste' | 'blur' | 'focus' | 'second_session' | 'shared_device';

/** One entry of a student's timeline. */
export interface Moment {
  at: number;
  kind: MomentKind;
  problem_id?: number;
  text: string;
}

export interface CaseUser {
  user_id: number;
  name: string;
  shared_at: number | null;
  explanation: string;
  explained_at: number | null;
}

export interface Decision {
  by: number;
  state: CaseState;
  note: string;
  created_at: number;
  /** Not in the contract; used when the server sends it. */
  by_name?: string;
}

/** A case as organisers see it. */
export interface Case {
  id: number;
  contest_id: number;
  state: CaseState;
  title: string;
  evidence: Signal[];
  created_at: number;
  updated_at: number;
  users: CaseUser[];
  decisions: Decision[];
}

/** One piece of evidence worded for the student it concerns. */
export interface StudentEvidence {
  problem_id: number;
  kind: SignalKind;
  text: string;
  submission_id?: number;
  source?: string;
  spans?: Span[];
}

/** A case shared with the signed-in student. */
export interface StudentCase {
  id: number;
  contest_id: number;
  state: CaseState;
  title: string;
  evidence: StudentEvidence[];
  explanation: string;
  shared_at: number;
  decided: boolean;
}

/** GET /api/integrity/cases. */
export interface StudentCaseList {
  cases: StudentCase[];
  contests?: Record<string, { slug: string; title: string }>;
  problems?: Record<string, ReviewProblem>;
}

/** The shortest reason the server accepts for a case or a decision. */
export const MIN_NOTE = 20;

/** The longest case title the server accepts. */
export const MAX_TITLE = 120;

/** The kinds in the order the filters list them, with their words. */
export const KINDS: readonly { id: SignalKind; label: string }[] = [
  { id: 'similar', label: 'Similar code' },
  { id: 'wrong_output', label: 'Same wrong output' },
  { id: 'hardcoded', label: 'Hard-coded answers' },
  { id: 'probing', label: 'Probing' },
];

/** The strengths, strongest first, with their words. */
export const STRENGTHS: readonly { id: Strength; label: string }[] = [
  { id: 'strong', label: 'Strong' },
  { id: 'moderate', label: 'Moderate' },
  { id: 'weak', label: 'Weak' },
];

/** A signal's kind in words. */
export function kindWord(kind: string): string {
  return KINDS.find((k) => k.id === kind)?.label ?? kind;
}

/** A signal's strength in words. */
export function strengthWord(strength: string): string {
  return STRENGTHS.find((s) => s.id === strength)?.label ?? strength;
}

/** The raw number beside the strength: a share to two places, a count as is. */
export function formatScore(score: number): string {
  return Number.isInteger(score) ? String(score) : score.toFixed(2);
}

/** Joins words as "a", "a and b", "a, b and c". */
export function joinWords(words: readonly string[]): string {
  if (words.length <= 1) return words[0] ?? '';
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
}

/** A user's name from the run's users map, falling back to the email, then the id. */
export function userName(users: Record<string, ReviewUser> | undefined, id: number | undefined): string {
  if (id === undefined || id === 0) return '';
  const u = users?.[String(id)];
  return u?.name || u?.email || `Student ${id}`;
}

/** The students a signal names, in words. */
export function signalStudents(s: Pick<Signal, 'user_a' | 'user_b'>, users: Record<string, ReviewUser> | undefined): string {
  return joinWords([s.user_a, s.user_b].filter((u): u is number => !!u).map((u) => userName(users, u)));
}

/** "B. Interval covering", or "Whole contest" for a signal tied to no problem. */
export function problemName(problems: Record<string, ReviewProblem> | undefined, id: number | undefined): string {
  if (!id) return 'Whole contest';
  const p = problems?.[String(id)];
  return p ? `${p.label}. ${p.title}` : `Problem ${id}`;
}

function testsWord(tests: readonly number[]): string {
  return `${tests.length === 1 ? 'test' : 'tests'} ${joinWords(tests.map(String))}`;
}

/** What a signal found, in one line for the organiser. */
export function signalDetail(s: Pick<Signal, 'kind' | 'evidence'>): string {
  const ev = s.evidence ?? {};
  switch (s.kind) {
    case 'similar': {
      const passages = Math.max(ev.spans_a?.length ?? 0, ev.spans_b?.length ?? 0);
      const a = ev.language_a ? languageLabel(ev.language_a) : '';
      const b = ev.language_b ? languageLabel(ev.language_b) : '';
      const langs = a && b ? (a === b ? `both in ${a}` : `in ${a} and ${b}`) : '';
      const count = passages > 0 ? `${passages} shared ${passages === 1 ? 'passage' : 'passages'}` : 'Shared passages';
      return langs ? `${count}, ${langs}` : count;
    }
    case 'wrong_output': {
      const tests = ev.tests ?? [];
      if (tests.length === 0) return 'Same wrong output on hidden tests';
      return `Same wrong output on ${tests.length} hidden ${tests.length === 1 ? 'test' : 'tests'}: ${joinWords(tests.map(String))}`;
    }
    case 'hardcoded': {
      const tests = ev.tests ?? [];
      return tests.length === 0 ? 'Contains expected outputs of hidden tests' : `Contains the expected output of ${testsWord(tests)}`;
    }
    case 'probing': {
      const distinct = ev.distinct ?? 0;
      const over = ev.attempts ? ` over ${ev.attempts} attempts` : '';
      return `${distinct} different results from near-identical code${over}`;
    }
    default:
      return '';
  }
}

/** The organiser's word for a case state. */
export function caseStateWord(state: string): string {
  switch (state) {
    case 'open':
      return 'Open';
    case 'awaiting_student':
      return 'Shared with the students';
    case 'cleared':
      return 'Cleared';
    case 'actioned':
      return 'Action taken';
    default:
      return state;
  }
}

/** The student's sentence for a case state. */
export function studentStateText(c: Pick<StudentCase, 'state' | 'explanation' | 'decided'>): string {
  if (c.state === 'cleared') return 'Decided: cleared. No action will be taken.';
  if (c.state === 'actioned') return 'Decided: an organiser took action. They will tell you what it means for you.';
  if (c.explanation.trim() !== '') return 'Your explanation is saved. An organiser reads it before deciding anything.';
  return 'Waiting for your explanation. Nothing has been decided.';
}

/** Characters as the server counts them: code points of the trimmed text. */
export function noteLength(text: string): number {
  return [...text.trim()].length;
}

/** The live counter under a reason field. */
export function noteCounter(text: string, min = MIN_NOTE): string {
  const n = noteLength(text);
  return n >= min ? `${n} characters` : `${n} of at least ${min} characters`;
}

/** The signals that pass the kind and strength filters ("" passes all). */
export function filterSignals(signals: readonly Signal[], kind: string, strength: string): Signal[] {
  return signals.filter((s) => (kind === '' || s.kind === kind) && (strength === '' || s.strength === strength));
}

/** Signals grouped by problem, in the order of the contest's labels; whole-contest signals last. */
export function groupByProblem(
  signals: readonly Signal[],
  problems: Record<string, ReviewProblem> | undefined,
): { problemId: number; signals: Signal[] }[] {
  const groups = new Map<number, Signal[]>();
  for (const s of signals) {
    const id = s.problem_id || 0;
    if (!groups.has(id)) groups.set(id, []);
    groups.get(id)!.push(s);
  }
  const label = (id: number) => (id === 0 ? '￿' : (problems?.[String(id)]?.label ?? `~${id}`));
  return [...groups.entries()]
    .sort(([a], [b]) => label(a).localeCompare(label(b)) || a - b)
    .map(([problemId, list]) => ({ problemId, signals: list }));
}

/**
 * Students linked by similar-code signals on one problem, largest group
 * first, each group's ids ascending.
 */
export function clusters(signals: readonly Signal[]): number[][] {
  const parent = new Map<number, number>();
  const find = (x: number): number => {
    let root = x;
    while (parent.get(root) !== root) root = parent.get(root)!;
    let cur = x;
    while (cur !== root) {
      const next = parent.get(cur)!;
      parent.set(cur, root);
      cur = next;
    }
    return root;
  };
  for (const s of signals) {
    if (s.kind !== 'similar' || !s.user_b) continue;
    for (const u of [s.user_a, s.user_b]) if (!parent.has(u)) parent.set(u, u);
    const a = find(s.user_a);
    const b = find(s.user_b);
    if (a !== b) parent.set(Math.max(a, b), Math.min(a, b));
  }
  const groups = new Map<number, number[]>();
  for (const u of parent.keys()) {
    const r = find(u);
    if (!groups.has(r)) groups.set(r, []);
    groups.get(r)!.push(u);
  }
  return [...groups.values()].map((g) => g.sort((x, y) => x - y)).sort((x, y) => y.length - x.length || x[0] - y[0]);
}

/** The stats line above the signals. */
export function statsLine(stats: RunStats): string {
  const n = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;
  const parts = KINDS.map((k) => ({ k, count: stats.signals?.[k.id] ?? 0 }))
    .filter((x) => x.count > 0)
    .map((x) => `${x.count} ${x.k.label.toLowerCase()}`);
  const total = KINDS.reduce((sum, k) => sum + (stats.signals?.[k.id] ?? 0), 0);
  const checked = `Checked ${n(stats.students, 'student', 'students')}, ${n(stats.submissions, 'submission', 'submissions')} and ${n(stats.problems, 'problem', 'problems')}.`;
  const found = total === 0 ? 'No signals.' : `${n(total, 'signal', 'signals')}: ${joinWords(parts)}.`;
  const groups = stats.clusters > 0 ? ` ${n(stats.clusters, 'group', 'groups')} of similar code.` : '';
  return `${checked} ${found}${groups}`;
}

/**
 * A default title for a case opened from these signals. It names the kind
 * and the problem, never the students: a case is shared with each student
 * in it, and one must not learn who the other is.
 */
export function defaultTitle(
  signals: readonly Signal[],
  _users: Record<string, ReviewUser> | undefined,
  problems: Record<string, ReviewProblem> | undefined,
): string {
  if (signals.length === 0) return '';
  const kinds = [...new Set(signals.map((s) => s.kind))];
  const probs = [...new Set(signals.map((s) => s.problem_id))];
  const what = kinds.length === 1 ? kindWord(kinds[0]) : 'Several signals';
  const where = probs.length === 1 && probs[0] ? ` on ${problems?.[String(probs[0])]?.label ?? `problem ${probs[0]}`}` : '';
  return `${what}${where}`.slice(0, MAX_TITLE);
}

// Spans and source lines.

/**
 * Converts byte spans into UTF-16 index ranges of `source`, sorted and
 * merged, clamped to the source. A start inside a multibyte character moves
 * back to the character's start and an end inside one moves forward to its
 * end, so a highlight never splits a character.
 */
export function byteSpansToText(source: string, spans: readonly Span[]): Span[] {
  // charAt[b] is the UTF-16 index of the character holding byte b;
  // boundary[b] is 1 when byte b starts a character.
  const bytes: number[] = [];
  const boundary: number[] = [];
  const widths: number[] = [];
  for (let i = 0; i < source.length; ) {
    const cp = source.codePointAt(i)!;
    const units = cp > 0xffff ? 2 : 1;
    const n = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    for (let k = 0; k < n; k++) {
      bytes.push(i);
      boundary.push(k === 0 ? 1 : 0);
      widths.push(units);
    }
    i += units;
  }
  const total = bytes.length;
  const toStart = (b: number) => (b >= total ? source.length : bytes[Math.max(0, b)]);
  const toEnd = (b: number) => {
    if (b >= total) return source.length;
    if (b <= 0) return 0;
    return boundary[b] ? bytes[b] : bytes[b] + widths[b];
  };
  const out: Span[] = [];
  const sorted = spans
    .filter((s) => Number.isFinite(s.start) && Number.isFinite(s.end) && s.end > s.start)
    .map((s) => ({ start: toStart(Math.floor(s.start)), end: toEnd(Math.ceil(s.end)) }))
    .filter((s) => s.end > s.start)
    .sort((a, b) => a.start - b.start || a.end - b.end);
  for (const s of sorted) {
    const last = out[out.length - 1];
    if (last && s.start <= last.end) last.end = Math.max(last.end, s.end);
    else out.push({ ...s });
  }
  return out;
}

/** A run of text on one line, inside or outside a shared passage. */
export interface Segment {
  text: string;
  mark: boolean;
  /** The first segment of a shared passage. */
  opens: boolean;
  /** The last segment of a shared passage. */
  closes: boolean;
}

export interface CodeLine {
  number: number;
  segments: Segment[];
  /** True when any part of the line is in a shared passage. */
  matched: boolean;
}

/**
 * Splits a source into numbered lines of segments, marking the parts the
 * byte spans cover. Line breaks (\n or \r\n) are never part of a segment;
 * a passage that crosses lines opens on its first visible character and
 * closes on its last.
 */
export function codeLines(source: string, spans: readonly Span[]): CodeLine[] {
  const isBreak = (i: number) => source[i] === '\n' || source[i] === '\r';
  const ranges: { start: number; end: number }[] = [];
  for (const s of byteSpansToText(source, spans)) {
    let start = s.start;
    let end = s.end;
    while (start < end && isBreak(start)) start++;
    while (end > start && isBreak(end - 1)) end--;
    if (end > start) ranges.push({ start, end });
  }

  const lines: CodeLine[] = [];
  let lineStart = 0;
  let r = 0;
  for (let number = 1; ; number++) {
    const nl = source.indexOf('\n', lineStart);
    let lineEnd = nl < 0 ? source.length : nl;
    if (lineEnd > lineStart && source[lineEnd - 1] === '\r') lineEnd--;

    const segments: Segment[] = [];
    let pos = lineStart;
    while (r < ranges.length && ranges[r].end <= lineStart) r++;
    for (let k = r; k < ranges.length && ranges[k].start < lineEnd; k++) {
      const from = Math.max(ranges[k].start, lineStart);
      const to = Math.min(ranges[k].end, lineEnd);
      if (from > pos) segments.push({ text: source.slice(pos, from), mark: false, opens: false, closes: false });
      if (to > from) {
        segments.push({ text: source.slice(from, to), mark: true, opens: from === ranges[k].start, closes: to === ranges[k].end });
      }
      pos = Math.max(pos, to);
    }
    if (lineEnd > pos) segments.push({ text: source.slice(pos, lineEnd), mark: false, opens: false, closes: false });
    lines.push({ number, segments, matched: segments.some((s) => s.mark) });

    // A final line break leaves no empty last line.
    if (nl < 0 || nl + 1 >= source.length) break;
    lineStart = nl + 1;
  }
  return lines;
}
