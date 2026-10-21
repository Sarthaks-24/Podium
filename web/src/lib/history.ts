/**
 * Problem revisions, contest pins and the audit feeds as the web app sees
 * them (v2 P2, contracts C3 and C4 in docs/API.md "History and audit").
 * This is the one adapter for those routes: when a field name changes on
 * the server, this file changes and the screens do not.
 *
 * The shapes mirror the API: Revision and RevisionDiff from the problem
 * routes (organisers), StatementHistory from
 * GET /api/contests/{slug}/problems/{label}/revisions (students), the pin
 * fields on AdminContest, the answer of "Update contest to revision N", and
 * SiteAuditEntry from GET /api/admin/site-audit (People "History"). The
 * contest "Changes" feed reuses AuditEntry from lib/contests.ts.
 *
 * The rest is wording: one sentence per site audit row, the statement
 * banner's lead, and the line diff grouped for display. No route here ever
 * carries a test's input or output (D47); only titles and statements.
 */
import { countOf, formatDateTime, roleLabel } from './format';
import type { Role } from './session.svelte';

// Revisions (C4, organisers).

/** A contest that pins a revision now. */
export interface RevisionPin {
  contest_id: number;
  slug: string;
  label: string;
}

/** One revision in the list on GET /api/admin/problems/{id}, newest first. */
export interface Revision {
  id: number;
  rev: number;
  created_at: number;
  /** null for revisions the server made (revision 1 at the upgrade, generated outputs or inputs). */
  created_by: { user_id: number; email: string; name: string } | null;
  /** The setter's reason; required while a live contest pins the problem, else "". */
  reason: string;
  content_hash?: string;
  statement_changed: boolean;
  judging_changed: boolean;
  /** Made at the upgrade for an older contest pin: its statement was not recorded. */
  reconstructed: boolean;
  validated: boolean;
  pinned_in: RevisionPin[];
}

/** A contest running now that pins the problem (any revision). */
export interface LiveContest {
  contest_id: number;
  slug: string;
  title: string;
  label: string;
}

/** The revision fields the problem response gains. */
export interface ProblemHistory {
  revision?: { id: number; rev: number };
  revisions?: Revision[];
  revisions_total?: number;
  live_contests?: LiveContest[];
}

/** One line of a statement diff: in both, only in the older, or only in the newer. */
export interface DiffLine {
  op: 'same' | 'del' | 'add';
  text: string;
}

/** GET /api/admin/problems/{id}/revisions/diff?from=&to= (revision numbers). */
export interface RevisionDiff {
  from_rev: number;
  to_rev: number;
  title_from: string;
  title_to: string;
  /** What differs: title, statement, limits, checker, solutions, samples, open, subtasks, tests. */
  changed: string[];
  statement_only: boolean;
  coarse: boolean;
  lines: DiffLine[];
}

// Statement history (C3, students).

/** One revision pinned to a contest problem, as a student may see it. */
export interface PinnedRevision {
  revision_id: number;
  rev: number;
  pinned_at: number;
  title: string;
  statement_changed: boolean;
  statement_recorded: boolean;
}

/** GET /api/contests/{slug}/problems/{label}/revisions. */
export interface StatementHistory {
  label: string;
  current_revision_id: number;
  statement_updated_at: number | null;
  /** Newest first. */
  revisions: PinnedRevision[];
  /** By default the newest against the one before; null with one revision. */
  diff: {
    from_revision_id: number;
    to_revision_id: number;
    from_rev: number;
    to_rev: number;
    title_from: string;
    title_to: string;
    coarse: boolean;
    lines: DiffLine[];
  } | null;
  server_now?: number;
}

/** The C3 fields on Contest.problems[] and the contest Workspace. */
export interface StatementStamp {
  revision_id?: number;
  /** The newest pin change after the start that changed the statement or title, else null. */
  statement_updated_at?: number | null;
}

/** The `statement` event: an organiser moved a contest problem to another revision. */
export interface StatementEvent {
  contest: string;
  contest_id: number;
  label: string;
  revision_id: number;
  statement_changed: boolean;
  pinned_at: number;
}

/** The path of a contest problem's statement history. */
export function statementHistoryPath(slug: string, label: string): string {
  return `/api/contests/${encodeURIComponent(slug)}/problems/${encodeURIComponent(label)}/revisions`;
}

function clock(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** The banner's lead: "Statement updated 14:32." (the link "See what changed" follows). */
export function statementUpdatedText(at: number): string {
  return `Statement updated ${clock(at)}.`;
}

/** The key under which a student dismissed the banner for one update. */
export function statementBannerKey(label: string, at: number): string {
  return `s${label}:${at}`;
}

// Pins (C4, organisers).

/** The pin fields AdminContest.problems[] gains. */
export interface ProblemPin {
  label: string;
  problem_id: number;
  revision_id?: number;
  rev?: number;
  latest_revision_id?: number;
  latest_rev?: number;
  /** The newest revision differs from the pinned one and is validated. */
  update_available?: boolean;
  /** The newest revision differs only in its title or statement: no rejudge needed. */
  update_statement_only?: boolean;
  reconstructed?: boolean;
}

/** One pin change on AdminContest.pin_history, newest first. */
export interface PinChange {
  label: string;
  problem_id: number;
  revision_id: number;
  rev: number;
  from_at: number;
  /** null for pins the upgrade to v1.2.0 mapped. */
  actor: { user_id: number; email: string; role: string } | null;
}

/** PUT /api/admin/contests/{id}/problems/{label}/revision. */
export interface PinUpdate<C = unknown> {
  contest: C;
  /** A prefilled announcement for the console to offer; never posted by the server. */
  announcement_md: string;
  statement_changed: boolean;
  rejudge_needed: boolean;
  server_now?: number;
}

/** The path of "Update contest to revision N". */
export function pinPath(contestId: number | string, label: string): string {
  return `/api/admin/contests/${encodeURIComponent(String(contestId))}/problems/${encodeURIComponent(label)}/revision`;
}

/** The button: "Update contest to revision 3". */
export function updateLabel(rev: number): string {
  return `Update contest to revision ${rev}`;
}

/** What the update does, said before the organiser confirms it. */
export function updateEffect(p: Pick<ProblemPin, 'label' | 'rev' | 'latest_rev' | 'update_statement_only'>): string {
  const from = p.rev !== undefined ? `revision ${p.rev}` : 'the pinned revision';
  const to = p.latest_rev !== undefined ? `revision ${p.latest_rev}` : 'the newest revision';
  const lead = `Students get ${to} of problem ${p.label} instead of ${from} the next time they open it.`;
  return p.update_statement_only
    ? `${lead} Only the statement changed, so no submission is judged again.`
    : `${lead} The tests or limits changed too: judge the problem again afterwards so every score uses the new tests.`;
}

/** One line of pin history: "Problem A pinned to revision 2, 14:32 by priya@college.edu". */
export function pinChangeText(p: PinChange): string {
  const who = p.actor ? ` by ${p.actor.email}` : ', mapped at the upgrade';
  return `Problem ${p.label} pinned to revision ${p.rev}, ${formatDateTime(p.from_at)}${who}`;
}

/** The words for what a revision changed. */
export function revisionChangeText(r: Pick<Revision, 'statement_changed' | 'judging_changed' | 'reconstructed'> & { rev?: number }): string {
  if (r.reconstructed) return 'Rebuilt at the upgrade from a contest pin; its statement was not recorded';
  // Revision 1 has nothing before it to differ from.
  if (r.rev === 1) return 'First recorded version';
  if (r.statement_changed && r.judging_changed) return 'Statement and judging changed';
  if (r.statement_changed) return 'Statement changed';
  if (r.judging_changed) return 'Judging changed';
  return 'No change to the statement or judging';
}

/** The diff lines grouped into runs, so long unchanged stretches can fold. */
export interface DiffRun {
  op: DiffLine['op'];
  lines: string[];
}

export function diffRuns(lines: readonly DiffLine[]): DiffRun[] {
  const runs: DiffRun[] = [];
  for (const l of lines) {
    const last = runs[runs.length - 1];
    if (last && last.op === l.op) last.lines.push(l.text);
    else runs.push({ op: l.op, lines: [l.text] });
  }
  return runs;
}

/** How many lines were added and removed. */
export function diffCounts(lines: readonly DiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const l of lines) {
    if (l.op === 'add') added++;
    else if (l.op === 'del') removed++;
  }
  return { added, removed };
}

/** The sentence a screen reader hears for a diff: "2 lines added and 1 removed." */
export function diffSummary(lines: readonly DiffLine[]): string {
  const { added, removed } = diffCounts(lines);
  if (added === 0 && removed === 0) return 'The text is the same.';
  return `${countOf(added, 'line')} added and ${removed} removed.`;
}

// Site audit (C4, People "History").

/** One row of GET /api/admin/site-audit. */
export interface SiteAuditEntry {
  id: number;
  at: number;
  action: string;
  /** null for changes the server made itself, such as a change of run mode. */
  actor: { user_id: number; email: string; role: string } | null;
  subject_email: string;
  details: Record<string, unknown>;
}

/** GET /api/admin/site-audit. */
export interface SiteAuditPage {
  entries: SiteAuditEntry[];
  page: number;
  has_more: boolean;
  server_now?: number;
}

/** The actions the History filter offers, with their words. Unknown ones still show by name. */
export const SITE_ACTIONS: readonly { id: string; label: string }[] = [
  { id: 'role_granted', label: 'Role given' },
  { id: 'role_revoked', label: 'Role removed' },
  { id: 'domain_added', label: 'Domain added' },
  { id: 'domain_removed', label: 'Domain removed' },
  { id: 'account_disabled', label: 'Account disabled' },
  { id: 'account_enabled', label: 'Account enabled' },
  { id: 'sessions_ended', label: 'Sessions ended' },
  { id: 'google_reset', label: 'Google link reset' },
  { id: 'mode_changed', label: 'Run mode changed' },
  { id: 'deploy_forced', label: 'Emergency deploy' },
];

function roleWord(v: unknown): string {
  return v === 'owner' || v === 'admin' || v === 'student' ? roleLabel(v as Role).toLowerCase() : String(v ?? '');
}

function ended(n: unknown): string {
  return typeof n === 'number' && n > 0 ? `; ${countOf(n, 'session')} ended` : '';
}

/** One sentence for a site audit row, in the past tense. */
export function siteAuditSummary(e: Pick<SiteAuditEntry, 'action' | 'subject_email' | 'details'>): string {
  const d = e.details ?? {};
  const who = e.subject_email || 'someone';
  const str = (k: string) => (typeof d[k] === 'string' ? (d[k] as string) : '');
  switch (e.action) {
    case 'role_granted':
      return d.from_role && d.from_role !== 'student'
        ? `Changed ${who} from ${roleWord(d.from_role)} to ${roleWord(d.role)}`
        : `Made ${who} ${d.role === 'owner' ? 'an owner' : 'an admin'}`;
    case 'role_revoked':
      return `Removed ${who} as ${roleWord(d.from_role)}${ended(d.sessions_ended)}`;
    case 'domain_added':
      return `Allowed @${str('domain')} to sign in`;
    case 'domain_removed':
      return `Removed @${str('domain')} from the allowed list${ended(d.sessions_ended)}`;
    case 'account_disabled':
      return `Disabled ${who}${ended(d.sessions_ended)}`;
    case 'account_enabled':
      return `Enabled ${who}`;
    case 'sessions_ended':
      return `Ended ${countOf(typeof d.count === 'number' ? d.count : 0, 'session')} of ${who}`;
    case 'google_reset':
      return `Reset the Google account link of ${who}${ended(d.sessions_ended)}`;
    case 'mode_changed':
      return `The server started in ${str('to') || 'another'} mode${str('from') ? ` (was ${str('from')})` : ''}${ended(d.sessions_ended)}`;
    case 'deploy_forced': {
      const release = str('release');
      return `Deployed ${release ? `release ${release}` : 'a new release'} inside a contest window: ${str('reason')}`;
    }
  }
  return e.action;
}

/** Who made a site audit change: "priya@college.edu (Owner)", or "The server". */
export function actorText(actor: SiteAuditEntry['actor']): string {
  if (!actor) return 'The server';
  return actor.role === 'owner' || actor.role === 'admin' || actor.role === 'student'
    ? `${actor.email} (${roleLabel(actor.role as Role)})`
    : actor.email;
}

/** The query string of a History or Changes request; empty filters are left out. */
export function feedQuery(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v));
  }
  const s = q.toString();
  return s === '' ? '' : `?${s}`;
}
