/**
 * Rewrite with AI (v2.1.0 stage W): the pure helpers behind the "Rewrite with
 * AI" start of the wizard and the Replace banner on the rewrite draft.
 *
 * The wizard run tells the page which problem it rewrites and a fingerprint
 * of that problem when the run began. The page keeps that link in the
 * browser, per rewrite draft, until Replace or Keep both; Replace sends it
 * back and the server checks it. A browser that refuses storage simply shows
 * no banner: the draft is then an ordinary new problem.
 */
import type { JavaLists, RewriteLink, StudentsWrite, WizardRequestResult } from './ai';
import type { DraftStorage } from './drafts';

/** The most characters "What should change" takes; the server refuses more. */
export const MAX_CHANGE = 4000;

/** The wizard page that rewrites problem `id`. */
export const rewriteHref = (id: number) => `/admin/ai/new?rewrite=${id}`;

/** POST puts a validated rewrite on problem `id`. */
export const rewritePath = (id: number) => `/api/admin/problems/${id}/replace-with-rewrite`;

/** The answer to POST .../replace-with-rewrite. */
export interface Replaced {
  validation_queued: boolean;
}

/**
 * The body of the wizard request for a rewrite: the paste mode with
 * the problem to rewrite, and what should change as the text. Refuses an
 * empty box and one that is too long.
 */
export function rewriteRequest(change: string, problemId: number, studentsWrite: StudentsWrite, javaLists: JavaLists): WizardRequestResult {
  const text = change.trim();
  if (text === '') return { ok: false, fields: { text: 'Say what should change.' } };
  if ([...text].length > MAX_CHANGE) return { ok: false, fields: { text: `Keep what should change under ${MAX_CHANGE.toLocaleString('en-GB')} characters.` } };
  const choices = studentsWrite === 'function' ? { students_write: 'function' as const, java_lists: javaLists } : {};
  return { ok: true, body: { mode: 'paste', text, rewrite_of: problemId, ...choices } };
}

/** The browser's IANA zone ("Asia/Kolkata"), which the server dates the archived copy in; "" when it cannot say. */
export function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
  } catch {
    return '';
  }
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** "11 Nov 2026", the way the server names the archived copy. */
function dayName(d: Date): string {
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

/** The words of the Replace confirm dialog for the problem called `title`. */
export function confirmText(title: string, today: Date): { title: string; body: string; confirm: string } {
  return {
    title: `Replace ${title}?`,
    confirm: `Replace ${title}`,
    body:
      `${title} gets this rewrite's statement, tests and solutions. Your own reference solution and brute force are replaced by the AI's, ` +
      `so a contest will ask before taking the problem until you review it. The current version is kept as an archived problem, ` +
      `${title} (before rewrite, ${dayName(today)}). A contest that already holds ${title} keeps the old version until you press Update students now.`,
  };
}

/** The toast after a Replace: validation has started only when the server queued it. */
export function replacedToast(title: string, r: Replaced): string {
  return r.validation_queued ? `Replaced ${title}. Validation has started.` : `Replaced ${title}. Validate it before you use it.`;
}

const PREFIX = 'podium-rewrite:';

function isLink(v: unknown): v is RewriteLink {
  if (typeof v !== 'object' || v === null) return false;
  const l = v as Record<string, unknown>;
  return typeof l.problem_id === 'number' && l.problem_id > 0 && typeof l.base === 'string' && l.base !== '' && typeof l.title === 'string';
}

/** Keeps the link of rewrite draft `draftId`. Storage that refuses is ignored. */
export function rememberRewrite(storage: DraftStorage | null, draftId: number, link: RewriteLink): void {
  try {
    storage?.setItem(PREFIX + draftId, JSON.stringify({ problem_id: link.problem_id, base: link.base, title: link.title }));
  } catch {
    // The draft stays an ordinary new problem.
  }
}

/** The link kept for rewrite draft `draftId`, or null when there is none or it is unreadable. */
export function recallRewrite(storage: DraftStorage | null, draftId: number): RewriteLink | null {
  try {
    const raw = storage?.getItem(PREFIX + draftId);
    if (!raw) return null;
    const v: unknown = JSON.parse(raw);
    return isLink(v) ? v : null;
  } catch {
    return null;
  }
}

/** Drops the link of rewrite draft `draftId`. */
export function forgetRewrite(storage: DraftStorage | null, draftId: number): void {
  try {
    storage?.removeItem(PREFIX + draftId);
  } catch {
    // Nothing was kept that could be dropped.
  }
}
