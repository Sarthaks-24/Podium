/**
 * Contests as the web app sees them (cache/v1/m4-plan.md sections 2 and 4,
 * design.md sections 5.2, 5.3 and 8).
 *
 * The shapes mirror the API: ContestSummary from GET /api/contests, Contest
 * from GET /api/contests/{slug}, and the organiser shapes from
 * /api/admin/contests. Times are Unix milliseconds from the server's clock;
 * screens compare them with serverClock.serverNow(), never with the
 * browser's own clock.
 *
 * The rest is pure maths and wording: which phase a student is in (before
 * the start, ready to start, during their window, after it), countdowns,
 * the freeze, the clock milestones a screen reader hears, roster CSV
 * parsing for the preview, problem labels, and the templates and canned
 * replies the console offers.
 */
import { LANGUAGES, type Language } from './problems';

export type ContestState = 'upcoming' | 'live' | 'frozen' | 'ended' | 'practice';
export type StartMode = 'fixed' | 'window';
export type FeedbackMode = 'subtask' | 'exam';
export type ScoringMode = 'best_subtask' | 'best_submission';
export type Access = 'domain' | 'roster';

/** A student's own time in a contest. */
export interface StudentWindow {
  started_at: number;
  ends_at: number;
}

/** One row of GET /api/contests. ends_at includes any contest-wide extension. */
export interface ContestSummary {
  slug: string;
  title: string;
  starts_at: number;
  ends_at: number;
  start_mode: StartMode;
  duration_minutes: number;
  problem_count: number;
  state: ContestState;
  my_score: number | null;
  /** The total available, when the server sends it. */
  max_score?: number;
  my_window: StudentWindow | null;
}

/** GET /api/contests. */
export interface ContestList {
  live: ContestSummary[];
  upcoming: ContestSummary[];
  past: ContestSummary[];
  server_now?: number;
}

export interface ContestRules {
  feedback_mode: FeedbackMode;
  scoring_mode: ScoringMode;
  min_gap_s: number;
  max_per_problem: number;
  languages: string[];
  freeze_minutes: number;
  graded: boolean;
}

/** A problem as a student sees it in the contest. */
export interface ContestProblem {
  label: string;
  title: string;
  my_score: number | null;
  max_score: number;
  /** How many times the student submitted it; read as 0 when missing. */
  my_submission_count?: number;
}

export interface Announcement {
  id: number;
  body_md: string;
  created_at: number;
}

export interface Clarification {
  id: number;
  problem_label: string | null;
  question: string;
  answer: string | null;
  public: boolean;
  created_at: number;
  answered_at: number | null;
  /** Organiser views only. */
  asked_by?: number | { user_id: number; email: string; name: string };
  asker_name?: string;
}

/** Who asked, for the organiser inbox. */
export function askerName(c: Pick<Clarification, 'asked_by' | 'asker_name'>): string {
  if (c.asked_by && typeof c.asked_by === 'object') return c.asked_by.name || c.asked_by.email || 'A student';
  return c.asker_name || 'A student';
}

/** GET /api/contests/{slug}. */
export interface Contest extends ContestSummary {
  id?: number;
  description_md: string;
  rules: ContestRules;
  honour_text: string;
  /** Only once the student's window started or ended. */
  problems: ContestProblem[] | null;
  announcements: Announcement[];
  my_clarifications: Clarification[];
  public_clarifications: Clarification[];
  editorial_md: string | null;
  server_now: number;
  /** When the student agreed to the honour code, when the server sends it. */
  honour_accepted_at?: number | null;
  accepted?: boolean;
  /** True when the server saw this student in a second session at once. */
  second_session?: boolean;
}

/** A message shown in the banner above the workspace. */
export interface BannerItem {
  key: string;
  kind: 'announcement' | 'answer';
  body: string;
  question?: string;
  at: number;
}

/**
 * The banner queue: announcements and answered own questions not yet
 * dismissed, newest first. Keys are "a<id>" and "c<id>".
 */
export function bannerItems(c: Pick<Contest, 'announcements' | 'my_clarifications'>, dismissed: ReadonlySet<string>, since: number): BannerItem[] {
  const items: BannerItem[] = [];
  for (const a of c.announcements ?? []) {
    if (a.created_at >= since && !dismissed.has(`a${a.id}`)) items.push({ key: `a${a.id}`, kind: 'announcement', body: a.body_md, at: a.created_at });
  }
  for (const q of c.my_clarifications ?? []) {
    const at = q.answered_at ?? 0;
    if (q.answer && at >= since && !dismissed.has(`c${q.id}`)) items.push({ key: `c${q.id}`, kind: 'answer', body: q.answer, question: q.question, at });
  }
  return items.sort((x, y) => y.at - x.at);
}

/** The SSE events a contest page listens for, besides submissions. */
export const CONTEST_EVENTS = ['standings', 'announcement', 'clarification', 'contest'] as const;

/** True when an event's `contest` field names this contest (or names none). */
export function isForContest(data: unknown, c: { id?: number; slug: string }): boolean {
  if (typeof data !== 'object' || data === null) return false;
  const tag = (data as { contest?: unknown }).contest;
  if (tag === undefined || tag === null) return true;
  return tag === c.slug || (c.id !== undefined && tag === c.id) || String(tag) === String(c.id ?? c.slug);
}

/** The state word (design.md section 8). */
export function stateWord(state: ContestState): string {
  switch (state) {
    case 'upcoming':
      return 'Upcoming';
    case 'live':
      return 'Live';
    case 'frozen':
      return 'Frozen';
    case 'practice':
      return 'Practice';
    default:
      return 'Ended';
  }
}

/** The list in display order: live first, then upcoming, then past. */
export function orderedContests(list: ContestList): ContestSummary[] {
  const live = [...(list.live ?? [])].sort((a, b) => a.ends_at - b.ends_at);
  const upcoming = [...(list.upcoming ?? [])].sort((a, b) => a.starts_at - b.starts_at);
  const past = [...(list.past ?? [])].sort((a, b) => b.ends_at - a.ends_at);
  return [...live, ...upcoming, ...past];
}

/** True once the student agreed to the honour code. */
export function hasAccepted(c: Pick<Contest, 'accepted' | 'honour_accepted_at' | 'problems' | 'my_window'>): boolean {
  if (typeof c.accepted === 'boolean') return c.accepted;
  if (c.honour_accepted_at !== undefined) return c.honour_accepted_at !== null && c.honour_accepted_at > 0;
  return c.my_window !== null && (c.problems?.length ?? 0) > 0;
}

/**
 * Where a student stands: "upcoming" before the start, "ready" when they may
 * agree to the honour code and start, "during" while their own window is
 * open, "after" once it closed (or the contest closed without them).
 */
export type Phase = 'upcoming' | 'ready' | 'during' | 'after';

export function studentPhase(
  c: Pick<Contest, 'starts_at' | 'ends_at' | 'my_window' | 'accepted' | 'honour_accepted_at' | 'problems'>,
  now: number,
): Phase {
  if (c.my_window && hasAccepted(c)) {
    if (now < c.my_window.started_at) return 'upcoming';
    return now < c.my_window.ends_at ? 'during' : 'after';
  }
  if (now < c.starts_at) return 'upcoming';
  if (now >= c.ends_at) return 'after';
  return 'ready';
}

/** The moment the board freezes for a fixed-start contest, or null. */
export function freezeAt(c: Pick<ContestSummary, 'start_mode' | 'ends_at'> & { rules: Pick<ContestRules, 'freeze_minutes'> }): number | null {
  if (c.start_mode !== 'fixed' || c.rules.freeze_minutes <= 0) return null;
  return c.ends_at - c.rules.freeze_minutes * 60_000;
}

function pad(n: number): string {
  return String(n).padStart(2, '0');
}

/** A clock: "01:12:48"; never negative; whole seconds rounded up. */
export function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return `${pad(h)}:${pad(m)}:${pad(s)}`;
}

/** A coarse span for words: "2 d 4 h", "3 h 12 min", "12 min", "40 s". */
export function formatSpan(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const d = Math.floor(total / 86_400);
  const h = Math.floor((total % 86_400) / 3600);
  const m = Math.floor((total % 3600) / 60);
  if (d > 0) return h > 0 ? `${d} d ${h} h` : `${d} d`;
  if (h > 0) return m > 0 ? `${h} h ${m} min` : `${h} h`;
  if (m > 0) return `${m} min`;
  return `${total} s`;
}

/** A length in minutes: "2 h 30 min", "45 min", "1 h". */
export function formatMinutes(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m} min`;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

/** How long a contest lasts for one student, in words. */
export function lengthText(c: Pick<ContestSummary, 'start_mode' | 'duration_minutes' | 'starts_at' | 'ends_at'>): string {
  if (c.start_mode === 'window') return `${formatMinutes(c.duration_minutes)} each`;
  return formatMinutes(Math.round((c.ends_at - c.starts_at) / 60_000));
}

/** The countdown line for a row of the contest list, or "" for none. */
export function listCountdown(c: ContestSummary, now: number): { lead: string; ms: number } | null {
  if (c.state === 'upcoming') return { lead: 'Starts in', ms: c.starts_at - now };
  if (c.state === 'live' || c.state === 'frozen') {
    if (c.start_mode === 'window') {
      if (!c.my_window) return { lead: 'Open for', ms: c.ends_at - now };
      if (now < c.my_window.ends_at) return { lead: 'Your time left', ms: c.my_window.ends_at - now };
      return null;
    }
    // Fixed start: the student's own end includes any personal extension.
    return { lead: 'Ends in', ms: (c.my_window?.ends_at ?? c.ends_at) - now };
  }
  return null;
}

/** Minutes left at which the clock is announced, largest first. */
export const MILESTONES_MIN = [30, 10, 5, 1] as const;

/**
 * The sentence for a clock milestone crossed between two readings of the
 * time left, or null. Used sparingly in a polite live region.
 */
export function clockMilestone(prevLeftMs: number, leftMs: number): string | null {
  if (prevLeftMs > 0 && leftMs <= 0) return 'Time is up. Your submissions are saved.';
  for (const m of MILESTONES_MIN) {
    const at = m * 60_000;
    if (prevLeftMs > at && leftMs <= at) return m === 1 ? '1 minute left.' : `${m} minutes left.`;
  }
  return null;
}

/** The freeze line under the clock, or null when the contest has no freeze. */
export function freezeText(freeze: number | null, now: number): string | null {
  if (freeze === null) return null;
  if (now >= freeze) return 'Standings frozen';
  return `Standings freeze in ${formatSpan(freeze - now)}`;
}

/** Problem labels: A to Z, then AA, AB and on. */
export function labelFor(index: number): string {
  const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  if (index < 26) return letters[index];
  return letters[Math.floor(index / 26) - 1] + letters[index % 26];
}

/** The languages a contest allows, in the usual order; all when the list is empty. */
export function allowedLanguages(ids: readonly string[] | undefined): { id: Language; label: string }[] {
  if (!ids || ids.length === 0) return [...LANGUAGES];
  return LANGUAGES.filter((l) => ids.includes(l.id));
}

/** The rules paragraph shown before the start. */
export function rulesSentences(c: Pick<Contest, 'start_mode' | 'duration_minutes' | 'starts_at' | 'ends_at' | 'rules'>): string[] {
  const r = c.rules;
  const out: string[] = [];
  if (c.start_mode === 'window') {
    out.push(`Your clock starts when you agree and start, and runs ${formatMinutes(c.duration_minutes)}, ending no later than the contest closes.`);
  } else {
    out.push(`Everyone starts together and has ${lengthText(c)}.`);
  }
  out.push(
    r.scoring_mode === 'best_submission'
      ? 'Each problem scores your best single submission.'
      : 'Each problem adds up your best result on every subtask across your submissions.',
  );
  out.push(
    r.feedback_mode === 'exam'
      ? 'This is an exam: a submission only says "Submitted". Scores appear after the end.'
      : 'Each submission shows which subtasks passed and a verdict for every test, never the hidden inputs or outputs.',
  );
  out.push(`You can submit up to ${r.max_per_problem} times per problem, at least ${r.min_gap_s} s apart.`);
  const langs = allowedLanguages(r.languages).map((l) => l.label);
  if (langs.length > 0) out.push(`Languages: ${langs.join(', ')}.`);
  if (c.start_mode === 'fixed' && r.freeze_minutes > 0) {
    out.push(`The standings freeze ${formatMinutes(r.freeze_minutes)} before the end; later results appear after the contest.`);
  } else if (c.start_mode === 'window') {
    out.push('The standings open when the contest closes.');
  }
  return out;
}

// Organiser shapes.

/** One row of GET /api/admin/contests. */
export interface AdminContestSummary {
  id: number;
  slug: string;
  title: string;
  starts_at: number;
  ends_at: number;
  start_mode: StartMode;
  duration_minutes: number;
  state: ContestState;
  published: boolean;
  archived?: boolean;
  /** Older shape; the server sends the problems themselves. */
  problem_count?: number;
  problems?: AdminContestProblem[];
  paused?: boolean;
}

/** How many problems an organiser's contest row has. */
export function problemCount(c: Pick<AdminContestSummary, 'problem_count' | 'problems'>): number {
  return c.problem_count ?? c.problems?.length ?? 0;
}

/** A problem in the organiser's view; ready is false when it changed since it was added. */
export interface AdminContestProblem {
  label: string;
  problem_id: number;
  title: string;
  max_score?: number;
  ready?: boolean;
}

export interface RosterEntry {
  email: string;
  display_name: string;
  roll_number: string;
  section: string;
}

/** A roster row as GET .../roster returns it. */
export interface RosterRow {
  email: string;
  name?: string;
  display_name?: string;
  roll_number: string;
  section: string;
}

export interface CoOrganiser {
  user_id: number;
  email: string;
  name: string;
}

/** The settings form; also the body of POST and PUT /api/admin/contests. */
export interface ContestSettings {
  slug: string;
  title: string;
  description_md: string;
  starts_at: number;
  ends_at: number;
  start_mode: StartMode;
  duration_minutes: number;
  freeze_minutes: number;
  scoring_mode: ScoringMode;
  access: Access;
  feedback_mode: FeedbackMode;
  graded: boolean;
  published: boolean;
  practice_after_end: boolean;
  stop_on_sample_fail: boolean;
  min_gap_s: number;
  max_per_problem: number;
  languages: string[];
  honour_text: string;
  /** Keep students on the frozen board after the end until an organiser reveals. */
  hold_reveal?: boolean;
}

/** GET /api/admin/contests/{id}. */
export interface AdminContest extends ContestSettings {
  id: number;
  state: ContestState;
  paused: boolean;
  extended_minutes: number;
  editorial_md: string;
  editorial_published: boolean;
  revealed_at: number | null;
  problems: AdminContestProblem[];
  /** The roster comes from GET .../roster; older shapes inlined it. */
  roster?: RosterRow[];
  roster_count?: number;
  admins: CoOrganiser[];
  extensions?: { user_id: number; email: string; name: string; minutes: number; reason: string }[];
  /** ends_at plus the contest-wide extension. */
  close_at?: number;
  default_honour_text?: string;
  server_now?: number;
}

/** One row of the console's submission feed. */
export interface FeedRow {
  id: number;
  user_id: number;
  name: string;
  email?: string;
  problem_label?: string;
  /** Older shape of problem_label. */
  label?: string;
  language: string;
  state: 'queued' | 'running' | 'done' | 'error';
  verdict: string;
  score: number;
  max_score: number;
  created_at: number;
  practice?: boolean;
  rejudged?: boolean;
}

export interface FeedPage {
  submissions: FeedRow[];
  page?: number;
  has_more?: boolean;
  total?: number;
}

export interface ProblemStats {
  label: string;
  title?: string;
  solved: number;
  attempted?: number;
  median_attempts: number | null;
  /** Students per score; buckets are subtask score sums. */
  histogram: { score: number; students?: number; count?: number }[];
  max_score?: number;
}

/** Defaults for a new contest: tomorrow at 18:00 local, two hours, graded. */
export function defaultSettings(now: number): ContestSettings {
  const d = new Date(now + 86_400_000);
  d.setHours(18, 0, 0, 0);
  const start = d.getTime();
  return {
    slug: '',
    title: '',
    description_md: '',
    starts_at: start,
    ends_at: start + 2 * 3_600_000,
    start_mode: 'fixed',
    duration_minutes: 120,
    freeze_minutes: 0,
    scoring_mode: 'best_subtask',
    access: 'domain',
    feedback_mode: 'subtask',
    graded: true,
    published: false,
    practice_after_end: true,
    stop_on_sample_fail: true,
    min_gap_s: 30,
    max_per_problem: 15,
    languages: LANGUAGES.map((l) => l.id),
    honour_text: '',
  };
}

/** Unix ms as a datetime-local value in local time: "2026-10-18T18:00". */
export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** A datetime-local value back to Unix ms, or null when it is not a time. */
export function fromLocalInput(value: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value.trim());
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]), 0, 0);
  return Number.isNaN(d.getTime()) ? null : d.getTime();
}

// Roster CSV.

/** A parsed roster line for the preview; error explains a line that will be refused. */
export interface RosterLine extends RosterEntry {
  line: number;
  error: string;
}

/** Splits CSV text into rows of fields; handles quotes, doubled quotes and CRLF. */
export function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += ch;
      }
    } else if (ch === '"' && field === '') {
      quoted = true;
    } else if (ch === ',') {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else {
      field += ch;
    }
  }
  if (field !== '' || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.some((f) => f.trim() !== ''));
}

const EMAIL = /^[^@\s,;]+@[^@\s,;]+\.[^@\s,;]+$/;

const HEADERS: Record<string, keyof RosterEntry> = {
  email: 'email',
  'e-mail': 'email',
  mail: 'email',
  name: 'display_name',
  display_name: 'display_name',
  'display name': 'display_name',
  'full name': 'display_name',
  roll: 'roll_number',
  roll_number: 'roll_number',
  'roll number': 'roll_number',
  'roll no': 'roll_number',
  'roll no.': 'roll_number',
  section: 'section',
};

/**
 * Parses a roster CSV for the preview. Columns are email, name, roll
 * number and section in that order, or in any order under a header row that
 * names them. Each line carries an error when it will be refused: a missing
 * or malformed email, or an email already listed above.
 */
export function parseRoster(text: string): { lines: RosterLine[]; valid: number; invalid: number } {
  const rows = parseCsv(text);
  let order: (keyof RosterEntry | null)[] = ['email', 'display_name', 'roll_number', 'section'];
  let first = 0;
  if (rows.length > 0 && rows[0].some((f) => HEADERS[f.trim().toLowerCase()] === 'email')) {
    order = rows[0].map((f) => HEADERS[f.trim().toLowerCase()] ?? null);
    first = 1;
  }
  const seen = new Set<string>();
  const lines: RosterLine[] = [];
  for (let r = first; r < rows.length; r++) {
    const entry: RosterEntry = { email: '', display_name: '', roll_number: '', section: '' };
    rows[r].forEach((value, i) => {
      const key = order[i];
      if (key) entry[key] = value.trim();
    });
    entry.email = entry.email.toLowerCase();
    let error = '';
    if (entry.email === '') error = 'No email';
    else if (!EMAIL.test(entry.email)) error = 'Not an email address';
    else if (seen.has(entry.email)) error = 'Listed twice';
    seen.add(entry.email);
    lines.push({ ...entry, line: r + 1, error });
  }
  const invalid = lines.filter((l) => l.error !== '').length;
  return { lines, valid: lines.length - invalid, invalid };
}

/** Writes roster entries back as CSV with a header row, quoting where needed. */
export function rosterCsv(entries: readonly RosterEntry[]): string {
  const cell = (v: string) => (/[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
  const head = 'email,name,roll_number,section';
  return [head, ...entries.map((e) => [e.email, e.display_name, e.roll_number, e.section].map(cell).join(','))].join('\n') + '\n';
}

// Console wording.

/** Announcement templates; {label} and {minutes} are filled in by the console. */
export const ANNOUNCEMENT_TEMPLATES: readonly { id: string; label: string; text: string }[] = [
  { id: 'correction', label: 'Statement corrected', text: 'Problem {label}: the statement has been corrected. Reload the problem to read the change.' },
  { id: 'extended', label: 'Contest extended', text: 'The contest has been extended by {minutes} minutes.' },
  { id: 'paused', label: 'Judging paused', text: 'Judging is paused for a few minutes. Keep submitting: your submissions are queued and nothing is lost.' },
  { id: 'resumed', label: 'Judging resumed', text: 'Judging has resumed. Queued submissions are being judged now.' },
  { id: 'rejudge', label: 'Problem rejudged', text: 'Problem {label} has been rejudged after a test was fixed. Scores in the standings may change.' },
  { id: 'time', label: 'Time check', text: '{minutes} minutes left.' },
];

/** Canned clarification replies. */
export const CANNED_REPLIES: readonly string[] = [
  'Read the statement again; the answer is there.',
  'No comment. Solve the problem as stated.',
  'Yes.',
  'No.',
  'The input is exactly as the constraints describe. No other cases occur.',
  'The statement has been corrected. Reload the problem.',
];

/** Fills a template's {label} and {minutes}. */
export function fillTemplate(text: string, values: { label?: string; minutes?: number }): string {
  return text.replace(/\{label\}/g, values.label ?? 'A').replace(/\{minutes\}/g, String(values.minutes ?? 10));
}
