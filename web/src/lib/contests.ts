/**
 * Contests as the web app sees them.
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
 * the console's opening sentence, and the templates and standard answers
 * the console offers.
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

/**
 * The exam options: measures an organiser turns on for an
 * examination, all off by default. They sit under Advanced in contest
 * setup, come to the student with the rules, and the workspace acts on
 * them; missing reads as off.
 */
export interface ExamOptions {
  /** Ask for full screen when the student starts, and count each exit. */
  full_screen?: boolean;
  /** Show organisers the focus losses (recorded in every contest). */
  count_focus?: boolean;
  /** Refuse pastes into the editor. */
  block_paste?: boolean;
  /** Refuse copies out of the statement. */
  block_copy?: boolean;
}

/** True when any exam option is on. */
export function anyExamOption(o: ExamOptions | null | undefined): boolean {
  return !!o && !!(o.full_screen || o.count_focus || o.block_paste || o.block_copy);
}

/**
 * The one line the student reads before starting, naming every measure
 * that is on ("This contest blocks pasting into the editor and opens in
 * full screen, counting each time you leave it."), or '' when none is:
 * nothing blocks silently.
 */
export function examLine(o: ExamOptions | null | undefined): string {
  if (!o) return '';
  const parts: string[] = [];
  if (o.block_paste) parts.push('blocks pasting into the editor');
  if (o.block_copy) parts.push('blocks copying from the statement');
  if (o.full_screen) parts.push('opens in full screen, counting each time you leave it');
  if (o.count_focus) parts.push('counts each time you switch tabs or leave the page');
  if (parts.length === 0) return '';
  const last = parts[parts.length - 1];
  const list = parts.length === 1 ? last : parts.length === 2 ? `${parts[0]} and ${last}` : `${parts.slice(0, -1).join(', ')}, and ${last}`;
  return `This contest ${list}.`;
}

/** The line a refused paste shows. */
export const PASTE_BLOCKED = 'Pasting is blocked in this contest. Nothing was pasted.';
/** The line a refused copy from the statement shows. */
export const COPY_BLOCKED = 'Copying from the statement is blocked in this contest.';

/** What the editor recorded with a submission, as the console's feed says it when an exam option is on. */
export function recordedText(
  row: Pick<FeedRow, 'pasted_chars' | 'focus_losses' | 'fullscreen_exits'>,
  o: ExamOptions,
): string {
  const times = (n: number) => (n === 1 ? '1 time' : `${n} times`);
  const parts = [`pasted ${row.pasted_chars ?? 0} characters`];
  if (o.count_focus) parts.push(`left the page ${times(row.focus_losses ?? 0)}`);
  if (o.full_screen) parts.push(`left full screen ${times(row.fullscreen_exits ?? 0)}`);
  const text = parts.join(', ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export interface ContestRules extends ExamOptions {
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
  /** "" until an organiser answers. */
  answer: string;
  public: boolean;
  created_at: number;
  answered_at: number | null;
  /** Organiser views only. */
  asked_by?: { user_id: number; email: string; name: string };
}

/** Who asked, for the organiser inbox. */
export function askerName(c: Pick<Clarification, 'asked_by'>): string {
  return c.asked_by?.name || c.asked_by?.email || 'A student';
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
  /** True once the student agreed to the honour code. */
  accepted: boolean;
  /** True when the server saw this student in a second session at once. */
  second_session?: boolean;
  /** True for an organiser, who sees this page as "Student view". */
  organiser: boolean;
}

/** A message shown in the banner above the workspace. */
export interface BannerItem {
  key: string;
  /** "public" is an answer an organiser gave everyone. */
  kind: 'announcement' | 'answer' | 'public';
  body: string;
  question?: string;
  at: number;
}

/**
 * The banner queue: announcements, answered own questions and answers an
 * organiser made public for everyone, newer than the
 * student's last read time and not dismissed, newest first. Keys are
 * "a<id>" and "c<id>"; a public answer to the student's own question shows
 * once, as their own. `lastRead` is a server time kept per contest on the
 * device (messagesReadAt), not the time the page opened: a message posted
 * while the student was elsewhere still shows.
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

/** The state word. */
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
 * The state word a student reads: a frozen contest is still
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
 * entered is not offered as "See your result" above their own.
 * Null when every contest is a past one they missed.
 */
export function pinnedContest<T extends Pick<ContestSummary, 'state' | 'my_score'>>(ordered: readonly T[]): T | null {
  const open = (c: T) => c.state === 'live' || c.state === 'frozen' || c.state === 'upcoming';
  return ordered.find((c) => open(c) || c.my_score !== null) ?? null;
}

/**
 * Where a student stands: "upcoming" before the start, "ready" when they may
 * agree to the honour code and start, "during" while their own window is
 * open, "after" once it closed (or the contest closed without them).
 */
export type Phase = 'upcoming' | 'ready' | 'during' | 'after';

export function studentPhase(c: Pick<Contest, 'starts_at' | 'ends_at' | 'my_window' | 'accepted'>, now: number): Phase {
  if (c.my_window && c.accepted) {
    if (now < c.my_window.started_at) return 'upcoming';
    return now < c.my_window.ends_at ? 'during' : 'after';
  }
  if (now < c.starts_at) return 'upcoming';
  if (now >= c.ends_at) return 'after';
  return 'ready';
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
 * `after` follows the time: ", newest results hidden" in the last minutes.
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
 * start, or, for a late arrival, that their time is already running.
 * `ms` is the countdown in the line.
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

/** The three sentences that teach the two buttons. */
export const WORKSPACE_SENTENCES: readonly string[] = [
  'Run tries your code on the examples in the statement. It never counts.',
  'Submit grades your code on hidden tests, in parts called subtasks.',
  'A later, worse submission never lowers your score.',
];

/** The line the workspace Rules dialog adds to the rules: what the editor records. */
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
  const exam = examLine(r);
  if (exam) out.push(exam);
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
  archived: boolean;
  problems: AdminContestProblem[];
  paused: boolean;
  /** ends_at plus the contest-wide extension. */
  close_at: number;
}

/**
 * The organiser list in display order: live and frozen contests
 * first by their end, so an extended contest never drops among the past
 * ones; then upcoming soonest first; then the rest newest first.
 */
export function organiserOrder<T extends Pick<AdminContestSummary, 'state' | 'starts_at' | 'ends_at'>>(list: readonly T[]): T[] {
  const live = list.filter((c) => c.state === 'live' || c.state === 'frozen').sort((a, b) => a.ends_at - b.ends_at);
  const upcoming = list.filter((c) => c.state === 'upcoming').sort((a, b) => a.starts_at - b.starts_at);
  const rest = list.filter((c) => c.state === 'ended' || c.state === 'practice').sort((a, b) => b.starts_at - a.starts_at);
  return [...live, ...upcoming, ...rest];
}

/**
 * The tab a contest opens on, by its state: a draft on Setup, an upcoming
 * or live contest on Live, an ended one on Results. The contests list and
 * the contest's own crumb link here, so a click lands where the work is.
 */
export function organiserHome(c: { id: number | string; state: ContestState; published?: boolean }): string {
  const base = `/admin/contests/${encodeURIComponent(String(c.id))}`;
  if (c.published === false) return base;
  if (c.state === 'ended' || c.state === 'practice') return `${base}/results`;
  return `${base}/console`;
}

/** A problem in the organiser's view; ready is false when it changed since it was added. The pin fields say which version of the problem it uses. */
export interface AdminContestProblem extends Partial<Omit<ProblemPin, 'label' | 'problem_id'>> {
  label: string;
  problem_id: number;
  title: string;
  max_score?: number;
  ready?: boolean;
}

/**
 * The settings form; also the body of POST and PUT /api/admin/contests.
 * The server derives the slug from the title when it makes the contest,
 *so the form never sends one.
 */
export interface ContestSettings extends ExamOptions {
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
  archived: boolean;
  paused: boolean;
  extended_minutes: number;
  editorial_md: string;
  editorial_published: boolean;
  revealed_at: number | null;
  problems: AdminContestProblem[];
  /** Left out when no student has extra time. */
  extensions?: { user_id: number; email: string; name: string; minutes: number; reason: string }[];
  /** ends_at plus the contest-wide extension. */
  close_at: number;
  default_honour_text: string;
  /** Queue health, the judge's last request for work and the unanswered count. */
  queue?: QueueHealth;
  judge_last_claim_at: number | null;
  unanswered: number;
  server_now: number;
}

/** One row of the console's submission feed. */
export interface FeedRow {
  id: number;
  user_id: number;
  name: string;
  email: string;
  problem_label: string;
  language: string;
  /** "submitted" while the server hides the result. */
  state: 'queued' | 'running' | 'done' | 'error' | 'submitted';
  verdict: string;
  score: number;
  max_score: number;
  created_at: number;
  judged_at: number | null;
  practice: boolean;
  rejudged: boolean;
  hidden?: boolean;
  /** The editor's counts since the student's previous submission, left out when none were recorded; full-screen exits only when full screen is on. */
  typed_chars?: number;
  pasted_chars?: number;
  focus_losses?: number;
  fullscreen_exits?: number;
}

export interface FeedPage {
  submissions: FeedRow[];
  page: number;
  has_more: boolean;
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
  full_screen: 'full screen on start',
  count_focus: 'count tab switches and focus loss',
  block_paste: 'block paste into the editor',
  block_copy: 'block copy out of the statement',
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
 * to stop carrying the email and reason, which only
 * organisers may see, so the feed must read without them.
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
      if (typeof d.reason_given === 'boolean') return d.reason_given ? `${gave}, with a reason (see Extra time)` : `${gave}, without a reason`;
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
  title: string;
  solved: number;
  attempted: number;
  median_attempts: number | null;
  /** Students per score; buckets are subtask score sums. */
  histogram: { score: number; students: number }[];
  max_score: number;
}

/** GET /api/admin/contests/{id}/stats; `started` counts the students taking part. */
export interface ContestStats {
  problems: ProblemStats[];
  started: number;
}

/**
 * A contest's state as organisers see it: a
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
 * What a contest needs before students can have it, from the
 * data the editor already holds: at least one problem and none changed
 * since it was added, a start still ahead (until it starts), and a
 * language. Who takes part is decided by the allowed domains. The
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
  if (c.hold_reveal && c.freeze_minutes <= 0) out.push({ tone: 'part', text: 'Results stay hidden until you reveal them, but none are hidden at the end, so there is nothing to reveal.' });
  return out;
}

/** Queue health on the organiser contest response. */
export interface QueueHealth {
  waiting: number;
  judging: number;
  errors: number;
  oldest_wait_s: number;
}

/** A judge asks for work every few seconds; a minute without that means none is working. */
export const JUDGE_SILENT_MS = 60_000;

/** A wait the queue should not reach in a calm contest. */
export const SLOW_WAIT_S = 60;

/** The console's opening line: trouble first when there is any, then how the contest is going. */
export interface ConsoleSentence {
  /** "Judge not seen for 3 minutes." or "" when nothing is wrong. */
  trouble: string;
  /** "Live, 31 started, 2 questions waiting, judge fine." */
  text: string;
}

/**
 * The one sentence at the top of the console: the contest's state (with the hidden results once the board
 * stops updating for students), how many students started, the questions
 * waiting, and whether the judge is working, as "Live, 31 started, 2
 * questions waiting, judge fine." A judge silent for over a minute, or none
 * since the server started, is said first on its own ("Judge not seen for 3
 * minutes."), as is a queue whose oldest submission waited a minute or
 * more. Older servers without the queue fields or the started count give a
 * shorter sentence.
 */
export function consoleSentence(
  c: Pick<AdminContest, 'starts_at' | 'ends_at' | 'freeze_minutes' | 'paused'> & {
    close_at?: number;
    queue?: QueueHealth;
    judge_last_claim_at?: number | null;
  },
  now: number,
  counts: { unanswered: number; started?: number | null },
): ConsoleSentence {
  const close = c.close_at ?? c.ends_at;
  const parts: string[] = [];
  if (now < c.starts_at) parts.push('Upcoming');
  else if (now < close) {
    const hiddenFrom = c.freeze_minutes > 0 ? close - c.freeze_minutes * 60_000 : null;
    parts.push(hiddenFrom !== null && now >= hiddenFrom ? `Live, results from ${hhmm(hiddenFrom)} hidden from students` : 'Live');
  } else parts.push('Ended');
  if (typeof counts.started === 'number' && now >= c.starts_at) parts.push(`${counts.started} started`);
  if (counts.unanswered > 0) parts.push(countOf(counts.unanswered, 'question waiting', 'questions waiting'));

  const trouble: string[] = [];
  if (c.paused) parts.push('judging paused');
  else if (c.judge_last_claim_at !== undefined) {
    const last = c.judge_last_claim_at;
    if (last === null) trouble.push('No judge has asked for work since the server started.');
    else if (now - last > JUDGE_SILENT_MS) trouble.push(`Judge not seen for ${countOf(Math.floor((now - last) / 60_000), 'minute')}.`);
    else parts.push('judge fine');
  }
  if (!c.paused && c.queue && c.queue.oldest_wait_s >= SLOW_WAIT_S) {
    trouble.push(`The oldest submission has waited ${countOf(Math.floor(c.queue.oldest_wait_s / 60), 'minute')} for the judge.`);
  }
  return { trouble: trouble.join(' '), text: `${parts.join(', ')}.` };
}

/** "14:02" in local time. */
function hhmm(ms: number): string {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * A feed row's outcome as students word it: "Accepted" for full
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
 * The freeze "End the contest now" sends with the new end.
 * The frozen moment is the close minus the freeze, so moving the end earlier
 * moves it into the past and hides results students already saw on the live
 * board. Before the freeze has begun the freeze is dropped (0): the board at
 * the end is the one everyone saw. Once the board is frozen, the freeze
 * shrinks so the frozen moment stays put, rounded up to whole minutes and
 * never below 1: the frozen moment moves at most 59 s earlier, never
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
