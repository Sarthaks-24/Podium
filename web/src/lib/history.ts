/**
 * Contest pins and the site audit feed as the web app sees them: the pin
 * fields on AdminContest, the answer of "Update students now" (PUT
 * .../problems/{label}/revision), and SiteAuditEntry from
 * GET /api/admin/site-audit (People "History"). The contest "Changes" feed
 * reuses AuditEntry from lib/contests.ts. No route here ever carries a
 * test's input or output (D47).
 */
import { countOf, roleLabel } from './format';
import type { Role } from './session.svelte';

// Pins (organisers).

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

/** PUT /api/admin/contests/{id}/problems/{label}/revision. */
export interface PinUpdate<C = unknown> {
  contest: C;
  /** A prefilled announcement for the console to offer; never posted by the server. */
  announcement_md: string;
  statement_changed: boolean;
  rejudge_needed: boolean;
  server_now?: number;
}

/** The path of "Update students now". */
export function pinPath(contestId: number | string, label: string): string {
  return `/api/admin/contests/${encodeURIComponent(String(contestId))}/problems/${encodeURIComponent(label)}/revision`;
}

/**
 * What the update does, said before the organiser confirms it, in the
 * words of design/words.md ("Update students now", never "revision").
 */
export function updateEffect(p: Pick<ProblemPin, 'label' | 'update_statement_only'>): string {
  const lead = `Students get the edited problem ${p.label} the next time they open it.`;
  return p.update_statement_only
    ? `${lead} Only the statement changed, so no submission is judged again.`
    : `${lead} The tests or limits changed too: judge the problem again afterwards so every score uses the new tests.`;
}

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
        : `Made ${who} ${d.role === 'owner' ? 'an owner' : 'an organiser'}`;
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
