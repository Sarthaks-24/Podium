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
 * the freeze, the clock milestones a screen reader hears, problem labels,
 * and the templates and canned replies the console offers.
 */
import { countOf, formatDateTime } from './format';
import type { ProblemPin } from './history';
import { LANGUAGES, type Language } from './problems';

export type ContestState = 'upcoming' | 'live' | 'frozen' | 'ended' | 'practice';

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
  min_gap_s: number;
  max_per_problem: number;
  languages: string[];
  freeze_minutes: number;
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

/**
 * The rail's score cell in the wide layout (S1-09): the score where one
 * exists and nothing else; "not tried" without a submission, "tried"
 * while one waits for its score.
 */
export function railStatus(p: Pick<ContestProblem, 'my_score' | 'max_score' | 'my_submission_count'>): { score: string; word: string } {
  const count = p.my_submission_count ?? 0;
  if (p.my_score === null) {
    return { score: '', word: count > 0 ? 'tried' : 'not tried' };
  }
  return { score: `${p.my_score}/${p.max_score}`, word: '' };
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
  /** "public" is an answer an organiser gave everyone (B1). */
  kind: 'announcement' | 'answer' | 'public';
  body: string;
  question?: string;
  at: number;
}

/**
 * The banner queue: announcements, answered own questions and answers an
 * organiser made public for everyone (B1, audit 01-2) newer than the
 * student's last read time and not dismissed, newest first. Keys are
 * "a<id>" and "c<id>"; a public answer to the student's own question shows
 * once, as their own. `lastRead` is a server time kept per contest on the
 * device (messagesReadAt), not the time the page opened: a message posted
 * while the student was elsewhere still shows (audit 2026-10-20 S1).
 */
export function bannerItems(
  c: Pick<Contest, 'announcements' | 'my_clarifications'> & { public_clarifications?: Clarification[] },
  dismissed: ReadonlySet<string>,
  lastRead: number,
): BannerItem[] {
  const items: BannerItem[] = [];
  for (const a of c.announcements ?? []) {
    if (a.created_at > lastRead && !dismissed.has(`a${a.id}`)) items.push({ key: `a${a.id}`, kind: 'announcement', body: a.body_md, at: a.created_at });
  }
  for (const q of c.my_clarifications ?? []) {
    const at = q.answered_at ?? 0;
    if (q.answer && at > lastRead && !dismissed.has(`c${q.id}`)) items.push({ key: `c${q.id}`, kind: 'answer', body: q.answer, question: q.question, at });
  }
  const own = new Set((c.my_clarifications ?? []).map((q) => q.id));
  for (const q of c.public_clarifications ?? []) {
    const at = q.answered_at ?? 0;
    if (!q.answer || own.has(q.id) || at <= lastRead || dismissed.has(`c${q.id}`)) continue;
    items.push({ key: `c${q.id}`, kind: 'public', body: q.answer, question: q.question, at });
  }
  return items.sort((x, y) => y.at - x.at);
}

/** The storage key of a contest's last read time on this device. */
export function messagesReadKey(slug: string): string {
  return `podium-messages-read:${slug}`;
}

/** When the student last read this contest's messages on this device, in server ms; 0 when never or unreadable. */
export function messagesReadAt(slug: string, store: Pick<Storage, 'getItem'> | null = safeLocalStorage()): number {
  try {
    const n = Number(store?.getItem(messagesReadKey(slug)) ?? 0);
    return Number.isFinite(n) && n > 0 ? n : 0;
  } catch {
    return 0;
  }
}

/** Records the last read time; never moves it backwards. Returns the time kept. */
export function markMessagesRead(slug: string, at: number, store: Pick<Storage, 'getItem' | 'setItem'> | null = safeLocalStorage()): number {
  const kept = Math.max(messagesReadAt(slug, store), Number.isFinite(at) ? at : 0);
  try {
    store?.setItem(messagesReadKey(slug), String(kept));
  } catch {
    // Read for this page only; the banner shows the messages again next time.
  }
  return kept;
}

function safeLocalStorage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

/** The lead of a banner item. */
export function bannerLead(kind: BannerItem['kind']): string {
  if (kind === 'announcement') return 'Announcement';
  if (kind === 'public') return 'An organiser answered a question for everyone';
  return 'An organiser answered your question';
}

/** The SSE events a contest page listens for, besides submissions. */
export const CONTEST_EVENTS = ['standings', 'announcement', 'clarification', 'contest', 'statement'] as const;

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

/**
 * The state word a student reads (decision D84): a frozen contest is still
 * running for them, so it reads 'Live' and the countdown line adds
 * "newest results hidden". Organiser screens keep stateWord.
 */
export function studentStateWord(state: ContestState): string {
  return state === 'frozen' ? 'Live' : stateWord(state);
}

/** The state a student's state word is styled as: frozen looks like live. */
export function studentState(state: ContestState): ContestState {
  return state === 'frozen' ? 'live' : state;
}

/** The list in display order: live first, then upcoming, then past. */
export function orderedContests(list: ContestList): ContestSummary[] {
  const live = [...(list.live ?? [])].sort((a, b) => a.ends_at - b.ends_at);
  const upcoming = [...(list.upcoming ?? [])].sort((a, b) => a.starts_at - b.starts_at);
  const past = [...(list.past ?? [])].sort((a, b) => b.ends_at - a.ends_at);
  return [...live, ...upcoming, ...past];
}

/**
 * The contest the student home features: the first in list order (live,
 * upcoming, then past newest first) that the student can still enter or
 * took part in. A past or practice contest counts only when it has the student's score
 * (`my_score` is null until they accept the rules), so a contest they never
 * entered is not offered as "See your result" above their own (walk
 * 2026-10-21, D324). Null when every contest is a past one they missed.
 */
export function pinnedContest<T extends Pick<ContestSummary, 'state' | 'my_score'>>(ordered: readonly T[]): T | null {
  const open = (c: T) => c.state === 'live' || c.state === 'frozen' || c.state === 'upcoming';
  return ordered.find((c) => open(c) || c.my_score !== null) ?? null;
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

/** The moment the board freezes, or null without a freeze. */
export function freezeAt(c: Pick<ContestSummary, 'ends_at'> & { rules: Pick<ContestRules, 'freeze_minutes'> }): number | null {
  if (c.rules.freeze_minutes <= 0) return null;
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

/** How long a contest lasts, in words. */
export function lengthText(c: Pick<ContestSummary, 'starts_at' | 'ends_at'>): string {
  return formatMinutes(Math.round((c.ends_at - c.starts_at) / 60_000));
}

/**
 * The countdown line for a row of the contest list, or null for none.
 * `after` follows the time: ", newest results hidden" in the last minutes (D84).
 */
export function listCountdown(c: ContestSummary, now: number): { lead: string; ms: number; after: string } | null {
  if (c.state === 'upcoming') return { lead: 'Starts in', ms: c.starts_at - now, after: '' };
  if (c.state === 'live' || c.state === 'frozen') {
    const after = c.state === 'frozen' ? ', newest results hidden' : '';
    // The student's own end includes any personal extension.
    return { lead: 'Ends in', ms: (c.my_window?.ends_at ?? c.ends_at) - now, after };
  }
  return null;
}

/**
 * The line under the title before a student starts: a countdown before the
 * start, or, for a late arrival, that their time is already running (audit
 * 01-17). `ms` is the countdown in the line.
 */
export function startLine(c: Pick<Contest, 'starts_at' | 'ends_at'>, now: number): { kind: 'before' | 'late'; ms: number } {
  if (now < c.starts_at) return { kind: 'before', ms: c.starts_at - now };
  return { kind: 'late', ms: c.ends_at - now };
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

/** The three sentences that teach the two buttons (v2 S3, audit 01-4). */
export const WORKSPACE_SENTENCES: readonly string[] = [
  'Run tries your code on the examples in the statement. It never counts.',
  'Submit grades your code on hidden tests, in parts called subtasks.',
  'A later, worse submission never lowers your score.',
];

/** The line the workspace Rules dialog adds to the rules (D114, what D47 records). */
export const RECORDED_SENTENCE = 'Podium records how many characters you paste and when you leave the page.';

/** The rules shown before the start, and again in the workspace Rules dialog. */
export function rulesSentences(c: Pick<Contest, 'starts_at' | 'ends_at' | 'rules'>): string[] {
  const r = c.rules;
  const out: string[] = [`Everyone starts together and has ${lengthText(c)}.`, ...WORKSPACE_SENTENCES];
  out.push('Each problem adds up your best result on every subtask across your submissions.');
  out.push('Each submission shows which subtasks passed and a verdict for every test, never the hidden inputs or outputs.');
  out.push(`You can submit up to ${r.max_per_problem} times per problem, at least ${r.min_gap_s} s apart.`);
  const langs = allowedLanguages(r.languages).map((l) => l.label);
  if (langs.length > 0) out.push(`Languages: ${langs.join(', ')}.`);
  if (r.freeze_minutes > 0) {
    out.push(`The standings freeze ${formatMinutes(r.freeze_minutes)} before the end; later results appear after the contest.`);
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
  state: ContestState;
  published: boolean;
  archived?: boolean;
  /** Older shape; the server sends the problems themselves. */
  problem_count?: number;
  problems?: AdminContestProblem[];
  paused?: boolean;
  /** ends_at plus the contest-wide extension. */
  close_at?: number;
}

/**
 * The organiser list in display order (v2 N6): live and frozen contests
 * first by their end, so an extended contest never drops among the past
 * ones; then upcoming soonest first; then the rest newest first.
 */
export function organiserOrder<T extends Pick<AdminContestSummary, 'state' | 'starts_at' | 'ends_at'>>(list: readonly T[]): T[] {
  const live = list.filter((c) => c.state === 'live' || c.state === 'frozen').sort((a, b) => a.ends_at - b.ends_at);
  const upcoming = list.filter((c) => c.state === 'upcoming').sort((a, b) => a.starts_at - b.starts_at);
  const rest = list.filter((c) => c.state === 'ended' || c.state === 'practice').sort((a, b) => b.starts_at - a.starts_at);
  return [...live, ...upcoming, ...rest];
}

/** How many problems an organiser's contest row has. */
export function problemCount(c: Pick<AdminContestSummary, 'problem_count' | 'problems'>): number {
  return c.problem_count ?? c.problems?.length ?? 0;
}

/** A problem in the organiser's view; ready is false when it changed since it was added. The pin fields are C4 (v1.2.0). */
export interface AdminContestProblem extends Partial<Omit<ProblemPin, 'label' | 'problem_id'>> {
  label: string;
  problem_id: number;
  title: string;
  max_score?: number;
  ready?: boolean;
}

/**
 * The settings form; also the body of POST and PUT /api/admin/contests.
 * The server derives the slug from the title when it makes the contest
 * (V3-15), so the form never sends one.
 */
export interface ContestSettings {
  title: string;
  description_md: string;
  starts_at: number;
  ends_at: number;
  freeze_minutes: number;
  published: boolean;
  practice_after_end: boolean;
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
  /** Derived from the title when the contest was made; it never changes. */
  slug: string;
  state: ContestState;
  paused: boolean;
  extended_minutes: number;
  editorial_md: string;
  editorial_published: boolean;
  revealed_at: number | null;
  problems: AdminContestProblem[];
  extensions?: { user_id: number; email: string; name: string; minutes: number; reason: string }[];
  /** ends_at plus the contest-wide extension. */
  close_at?: number;
  default_honour_text?: string;
  /** Queue health, the judge's last request for work and the unanswered count (C10, v1.3.0). */
  queue?: QueueHealth;
  judge_last_claim_at?: number | null;
  unanswered?: number;
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
  /** "submitted" while the server hides the result. */
  state: 'queued' | 'running' | 'done' | 'error' | 'submitted';
  verdict: string;
  score: number;
  max_score: number;
  created_at: number;
  practice?: boolean;
  rejudged?: boolean;
  hidden?: boolean;
  /** v1.4.0, reviewers only: private notes on the submission. */
  note_count?: number;
  /** v1.4.0, reviewers only: the case opened from this submission. */
  case_id?: number | null;
}

export interface FeedPage {
  submissions: FeedRow[];
  page?: number;
  has_more?: boolean;
  total?: number;
}

/** One row of GET /api/admin/contests/{id}/audit: an organiser action that can change results. */
export interface AuditEntry {
  id: number;
  action: string;
  /** null for rows the server wrote itself (from v1.2.0). */
  actor: { user_id: number; email: string; role: string } | null;
  details: Record<string, unknown>;
  created_at: number;
}

/** The kinds of change the console's Changes filter offers, with their words. */
export const AUDIT_ACTIONS: readonly { id: string; label: string }[] = [
  { id: 'settings', label: 'Settings' },
  { id: 'problems', label: 'Problems' },
  { id: 'revision', label: 'Problem revisions' },
  { id: 'extension', label: 'Extra time for a student' },
  { id: 'extend', label: 'Contest extended' },
  { id: 'pause', label: 'Judging paused' },
  { id: 'resume', label: 'Judging resumed' },
  { id: 'rejudge', label: 'Judged again' },
  { id: 'reveal', label: 'Reveal' },
  { id: 'editorial', label: 'Editorial' },
  { id: 'ai_problem', label: 'AI problems graded as written' },
  { id: 'archive', label: 'Archived' },
  { id: 'integrity_decision', label: 'Integrity decisions' },
];

/** The contest form's fields as the audit log names them. */
const AUDIT_FIELDS: Record<string, string> = {
  slug: 'slug',
  title: 'title',
  description_md: 'description',
  honour_text: 'honour code',
  starts_at: 'start',
  ends_at: 'end',
  freeze_minutes: 'freeze',
  published: 'published',
  practice_after_end: 'practice after the end',
  min_gap_s: 'gap between submissions',
  max_per_problem: 'submissions per problem',
  languages: 'languages',
  hold_reveal: 'hold the reveal',
};

function auditValue(field: string, v: unknown): string {
  if ((field === 'starts_at' || field === 'ends_at') && typeof v === 'number') return formatDateTime(v);
  if (typeof v === 'boolean') return v ? 'yes' : 'no';
  if (Array.isArray(v)) return v.length === 0 ? 'none' : v.join(', ');
  return String(v);
}

/**
 * One sentence for an audit row, in the past tense. `names` maps user ids
 * to names, for rows that name a student only by id: the extension row is
 * to stop carrying the email and reason (P3 security review, D162's
 * amendment), so the feed must read without them.
 */
export function auditSummary(e: Pick<AuditEntry, 'action' | 'details'>, names?: ReadonlyMap<number, string>): string {
  const d = e.details ?? {};
  const str = (k: string) => (typeof d[k] === 'string' ? (d[k] as string) : '');
  const num = (k: string) => (typeof d[k] === 'number' ? (d[k] as number) : 0);
  switch (e.action) {
    case 'settings': {
      const changes = (d.changes ?? {}) as Record<string, { from: unknown; to: unknown }>;
      const parts = Object.keys(changes)
        .sort()
        .map((k) => `${AUDIT_FIELDS[k] ?? k} from ${auditValue(k, changes[k].from)} to ${auditValue(k, changes[k].to)}`);
      return parts.length === 0 ? 'Saved the settings' : `Changed ${parts.join('; ')}`;
    }
    case 'problems': {
      const to = Array.isArray(d.to) ? (d.to as { label: string }[]).map((p) => p.label) : [];
      return to.length === 0 ? 'Removed every problem' : `Set the problems to ${to.join(', ')}`;
    }
    case 'ai_problem':
      return `Graded AI problem ${str('label')} as written: ${str('reason')}`;
    case 'revision': {
      const what = d.statement_changed === true && d.judging_changed === true ? 'statement and tests' : d.judging_changed === true ? 'tests' : 'statement';
      return `Moved problem ${str('label')} from revision ${num('from_rev')} to revision ${num('to_rev')} (${what} changed)`;
    }
    case 'extension': {
      const who = str('email') || names?.get(num('user_id')) || `user ${num('user_id')}`;
      if (num('minutes') === 0) return `Removed the extension for ${who}`;
      const gave = `Gave ${who} ${countOf(num('minutes'), 'extra minute')}`;
      // From v1.4.0 the row says only whether a reason was written; the reason stays on the extension.
      if (typeof d.reason_given === 'boolean') return d.reason_given ? `${gave}, with a reason (see Time)` : `${gave}, without a reason`;
      const reason = str('reason');
      return `${gave}${reason ? ` (${reason})` : ''}`;
    }
    case 'integrity_decision': {
      const who = names?.get(num('user_id')) || `user ${num('user_id')}`;
      const word = str('decision') === 'excluded' ? 'excluded from this contest' : str('decision') === 'warning' ? 'warning' : 'no action';
      return `Recorded an integrity decision about ${who}: ${word}`;
    }
    case 'extend':
      return `Extended the contest by ${countOf(num('minutes'), 'minute')}`;
    case 'pause':
      return 'Paused judging';
    case 'resume':
      return 'Resumed judging';
    case 'rejudge':
      return str('problem_label') !== ''
        ? `Judged problem ${str('problem_label')} again (${countOf(num('queued'), 'submission')})`
        : `Judged submission ${num('submission_id')} again`;
    case 'reveal':
      return 'Revealed the final board';
    case 'editorial':
      if (d.published !== true) return 'Unpublished the editorial';
      return d.was_published === true ? 'Edited the published editorial' : 'Published the editorial';
    case 'archive':
      return 'Archived the contest';
  }
  return e.action;
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

/**
 * A contest's state as organisers see it (audit 2026-10-20 O1, O8): a
 * contest students cannot see yet is a draft, whatever its dates, and once
 * it has ended it reads as ended even while practice is open. The kind
 * picks the state word's colour (styles/controls.css .state-word).
 */
export function organiserState(c: { state: ContestState; published?: boolean; archived?: boolean }): { word: string; kind: string } {
  if (c.archived) return { word: 'Archived', kind: 'ended' };
  if (c.published === false) return { word: 'Draft', kind: 'draft' };
  if (c.state === 'practice') return { word: 'Ended, practice open', kind: 'ended' };
  return { word: stateWord(c.state), kind: c.state };
}

/** One line of the contest editor's publish checklist; "bad" items stop publishing. */
export interface PublishCheck {
  tone: 'ok' | 'part' | 'bad';
  text: string;
}

/**
 * What a contest needs before students can have it (F6.3, C5), from the
 * data the editor already holds: at least one problem and none changed
 * since it was added, a start still ahead (until it starts), and a
 * language. Who takes part is decided by the allowed domains (V3-15). The
 * server checks again when it publishes.
 */
export function publishChecks(
  c: { problems: Pick<AdminContestProblem, 'label' | 'ready'>[]; languages: string[]; starts_at: number; freeze_minutes: number; hold_reveal?: boolean },
  started: boolean,
  now: number,
): PublishCheck[] {
  const out: PublishCheck[] = [];
  const n = c.problems.length;
  const changed = c.problems.filter((p) => p.ready === false).map((p) => p.label);
  if (n === 0) out.push({ tone: 'bad', text: 'Add at least one validated problem.' });
  else if (changed.length > 0)
    out.push({
      tone: 'bad',
      text: `${changed.length === 1 ? `Problem ${changed[0]} has` : `Problems ${changed.join(', ')} have`} changed since ${changed.length === 1 ? 'it was' : 'they were'} added. Validate, then add again.`,
    });
  else out.push({ tone: 'ok', text: `${n} ${n === 1 ? 'problem' : 'problems'}, all validated.` });
  if (!started) {
    out.push(c.starts_at > now ? { tone: 'ok', text: `Starts ${formatDateTime(c.starts_at)}.` } : { tone: 'part', text: 'The start time has passed. Move it ahead, or students start at once.' });
  }
  out.push({ tone: 'ok', text: 'Anyone from the allowed domains can take part.' });
  out.push(c.languages.length > 0 ? { tone: 'ok', text: `${c.languages.length} ${c.languages.length === 1 ? 'language' : 'languages'} allowed.` } : { tone: 'bad', text: 'Allow at least one language.' });
  if (c.hold_reveal && c.freeze_minutes <= 0) out.push({ tone: 'part', text: 'The board is held for a reveal, but with no freeze there is nothing to reveal.' });
  return out;
}

/** Queue health on the organiser contest response (C10, v1.3.0). */
export interface QueueHealth {
  waiting: number;
  judging: number;
  errors: number;
  oldest_wait_s: number;
}

/** One item of the console's attention strip; only "part" and "bad" ask for action. */
export interface AttentionItem {
  key: 'time' | 'freeze' | 'judging' | 'queue' | 'errors' | 'judge' | 'questions' | 'update';
  text: string;
  tone: 'note' | 'part' | 'bad';
  href: string;
}

/** A judge asks for work every few seconds; a minute without that means none is working (C10). */
export const JUDGE_SILENT_MS = 60_000;

/** A wait the queue should not reach in a calm contest. */
export const SLOW_WAIT_S = 60;

/**
 * The console's attention strip (F6.1, C1 with the C10 fields): time to
 * the start or end, when the board freezes, judging on or paused, the
 * queue, judge errors, whether a judge is asking for work, unanswered
 * questions and a newer revision waiting for students. Each item
 * links to where it is handled; an item turns "part" or "bad" only when it
 * needs action. Older servers without the C10 fields simply give fewer
 * items.
 */
export function attentionItems(
  c: Pick<AdminContest, 'starts_at' | 'ends_at' | 'freeze_minutes' | 'paused' | 'problems' | 'state'> & {
    close_at?: number;
    queue?: QueueHealth;
    judge_last_claim_at?: number | null;
  },
  now: number,
  unanswered: number,
): AttentionItem[] {
  const out: AttentionItem[] = [];
  const close = c.close_at ?? c.ends_at;
  if (now < c.starts_at) out.push({ key: 'time', text: `Starts in ${formatCountdown(c.starts_at - now)}`, tone: 'note', href: '#time-title' });
  else if (now < close) out.push({ key: 'time', text: `Ends in ${formatCountdown(close - now)}`, tone: 'note', href: '#time-title' });
  else out.push({ key: 'time', text: 'Ended', tone: 'note', href: '#time-title' });

  const freeze = c.freeze_minutes > 0 ? c.ends_at - c.freeze_minutes * 60_000 : null;
  if (freeze !== null && now >= c.starts_at && now < close) {
    out.push({ key: 'freeze', text: now < freeze ? `Board freezes in ${formatSpan(freeze - now)}` : 'Board frozen', tone: 'note', href: '#time-title' });
  }

  out.push(c.paused ? { key: 'judging', text: 'Judging paused', tone: 'part', href: '#time-title' } : { key: 'judging', text: 'Judging on', tone: 'note', href: '#time-title' });

  if (c.queue) {
    const q = c.queue;
    if (q.waiting === 0 && q.judging === 0) out.push({ key: 'queue', text: 'Queue empty', tone: 'note', href: '#submissions-title' });
    else {
      const wait = q.oldest_wait_s > 0 ? `, oldest ${formatSpan(q.oldest_wait_s * 1000)}` : '';
      out.push({ key: 'queue', text: `${q.waiting} waiting, ${q.judging} judging${wait}`, tone: q.oldest_wait_s >= SLOW_WAIT_S && !c.paused ? 'part' : 'note', href: '#submissions-title' });
    }
    if (q.errors > 0) out.push({ key: 'errors', text: countOf(q.errors, 'judge error'), tone: 'bad', href: '#submissions-title' });
  }

  if (c.judge_last_claim_at !== undefined) {
    const last = c.judge_last_claim_at;
    if (last === null) out.push({ key: 'judge', text: 'No judge has asked for work since the server started', tone: 'bad', href: '#submissions-title' });
    else if (now - last > JUDGE_SILENT_MS) out.push({ key: 'judge', text: `No judge for ${formatSpan(now - last)}`, tone: 'bad', href: '#submissions-title' });
    else out.push({ key: 'judge', text: `Judge seen ${formatSpan(Math.max(1000, now - last))} ago`, tone: 'note', href: '#submissions-title' });
  }

  out.push(
    unanswered > 0
      ? { key: 'questions', text: countOf(unanswered, 'unanswered question'), tone: 'bad', href: '#clar-title' }
      : { key: 'questions', text: 'No unanswered questions', tone: 'note', href: '#clar-title' },
  );

  // A saved edit reaches students only through the console (walk 2026-10-21);
  // after the close it no longer needs anyone's attention.
  const waiting = c.problems.filter((p) => p.update_available).map((p) => p.label);
  if (waiting.length > 0 && now < close) {
    out.push({
      key: 'update',
      text: `${waiting.length === 1 ? `Problem ${waiting[0]} has` : `Problems ${waiting.join(', ')} have`} a newer revision for students`,
      tone: 'part',
      href: '#versions-title',
    });
  }

  return out;
}

/**
 * A feed row's outcome as students word it (audit O3): "Accepted" for full
 * marks, "Partial, 40 of 100" for some points, the verdict for none; the
 * tone colours the badge.
 */
export function feedOutcome(row: Pick<FeedRow, 'state' | 'verdict' | 'score' | 'max_score'>): { text: string; tone: 'ok' | 'part' | 'bad' | 'wait' | 'neutral' } {
  if (row.state === 'submitted') return { text: 'Submitted', tone: 'neutral' };
  if (row.state === 'queued') return { text: 'Queued', tone: 'wait' };
  if (row.state === 'running') return { text: 'Judging', tone: 'wait' };
  if (row.state === 'error') return { text: 'Judge error', tone: 'bad' };
  if (row.max_score > 0 && row.score >= row.max_score) return { text: 'Accepted', tone: 'ok' };
  if (row.score > 0) return { text: `Partial, ${row.score} of ${row.max_score}`, tone: 'part' };
  return { text: verdictWord(row.verdict), tone: 'bad' };
}

const VERDICT_WORDS: Record<string, string> = {
  AC: 'Accepted',
  WA: 'Wrong answer',
  TLE: 'Time limit exceeded',
  MLE: 'Memory limit exceeded',
  RE: 'Runtime error',
  CE: 'Compile error',
  OLE: 'Output limit exceeded',
};

function verdictWord(code: string): string {
  return VERDICT_WORDS[code] ?? (code === '' ? 'No score' : code);
}

/** When a submission came in, from the contest start: "+7 min"; before the start, null. */
export function sinceStart(at: number, startsAt: number): string | null {
  if (at < startsAt) return null;
  return `+${Math.floor((at - startsAt) / 60_000)} min`;
}

/** Defaults for a new contest: tomorrow at 18:00 local, two hours. */
export function defaultSettings(now: number): ContestSettings {
  const d = new Date(now + 86_400_000);
  d.setHours(18, 0, 0, 0);
  const start = d.getTime();
  return {
    title: '',
    description_md: '',
    starts_at: start,
    ends_at: start + 2 * 3_600_000,
    freeze_minutes: 0,
    published: false,
    practice_after_end: true,
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

// Console wording.

/** Announcement templates; {label} and {minutes} are filled in by the console. */
export const ANNOUNCEMENT_TEMPLATES: readonly { id: string; label: string; text: string }[] = [
  { id: 'correction', label: 'Statement corrected', text: 'Problem {label}: the statement has been corrected. Reload the problem to read the change.' },
  { id: 'extended', label: 'Contest extended', text: 'The contest has been extended by {minutes} minutes.' },
  { id: 'paused', label: 'Judging paused', text: 'Judging is paused for a few minutes. Keep submitting: your submissions are queued and nothing is lost.' },
  { id: 'resumed', label: 'Judging resumed', text: 'Judging has resumed. Queued submissions are being judged now.' },
  { id: 'rejudge', label: 'Problem judged again', text: 'Every submission to problem {label} is being judged again after a test was fixed. Scores in the standings may change.' },
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

/**
 * The freeze "End the contest now" sends with the new end (walk 2026-10-21).
 * The frozen moment is the close minus the freeze, so moving the end earlier
 * moves it into the past and hides results students already saw on the live
 * board. Before the freeze has begun the freeze is dropped (0): the board at
 * the end is the one everyone saw. Once the board is frozen, the freeze
 * shrinks so the frozen moment stays put, rounded up to whole minutes and
 * never below 1 (V3-18): the frozen moment moves at most 59 s earlier, never
 * later, so a held board is never released by ending early.
 */
export function endNowFreeze(
  c: Pick<AdminContest, 'freeze_minutes' | 'ends_at'> & { extended_minutes?: number },
  newEndsAt: number,
  now: number,
): number {
  if (c.freeze_minutes <= 0) return c.freeze_minutes;
  const added = (c.extended_minutes ?? 0) * 60_000;
  const frozenAt = c.ends_at + added - c.freeze_minutes * 60_000;
  if (now < frozenAt) return 0;
  return Math.max(1, Math.ceil((newEndsAt + added - frozenAt) / 60_000));
}
