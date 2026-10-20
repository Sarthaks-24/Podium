/**
 * Notices for the signed-in person (contract C9, docs/API.md "C9:
 * notices"; v2 P4 F9.1).
 *
 * The server writes every notice from fixed templates: what happened, what
 * to do, by when (`due_at`), and on a shared case or a reminder the line
 * "Nothing affects your marks until a person decides." It never carries
 * code, evidence, test data or anyone's name, and nothing is delivered
 * while the person is in a live contest: then the list is empty, `held`
 * is true and the unread count is 0.
 *
 * This module holds the shapes and the wording; the unread count the shell
 * shows lives in lib/notices.svelte.ts.
 */
import { formatDayTime } from './format';

export type NoticeKind = 'case_shared' | 'reminder' | 'case_decided' | 'appeal_decided' | 'standing_adjusted' | 'results_final';

export interface Notice {
  id: number;
  kind: NoticeKind | string;
  happened: string;
  todo: string;
  due_at: number | null;
  reassurance?: string;
  link?: string;
  case_id?: number | null;
  contest_id?: number | null;
  created_at: number;
  read_at: number | null;
}

/** GET /api/me/notifications. */
export interface NoticeList {
  notifications: Notice[];
  unread: number;
  held: boolean;
  has_more: boolean;
  server_now?: number;
}

/** A path inside the web app, or null for anything else (another origin, a scheme, a protocol-relative URL). */
export function internalPath(link: string | undefined | null): string | null {
  if (!link || !link.startsWith('/') || link.startsWith('//') || link.startsWith('/\\')) return null;
  return link;
}

/** Where a notice's button goes: the student case page for a case, else the notice's own link. */
export function noticeHref(n: Pick<Notice, 'kind' | 'case_id' | 'link'>): string | null {
  if (typeof n.case_id === 'number' && n.case_id > 0 && n.kind !== 'standing_adjusted' && n.kind !== 'results_final') {
    return `/integrity/cases/${n.case_id}`;
  }
  return internalPath(n.link);
}

/** The button's words, naming the action. */
export function noticeAction(kind: string): string {
  switch (kind) {
    case 'case_shared':
    case 'reminder':
      return 'Reply on the case page';
    case 'case_decided':
      return 'Read the decision';
    case 'appeal_decided':
      return 'Read the ruling';
    case 'standing_adjusted':
      return 'See your standing';
    case 'results_final':
      return 'See your result';
    default:
      return 'Open';
  }
}

/** The "by when" line: "Reply by 26 Oct, 18:00", "Appeal by 4 Nov, 18:00", or "". */
export function dueLine(n: Pick<Notice, 'kind' | 'due_at'>): string {
  if (n.due_at === null || n.due_at === undefined) return '';
  const at = formatDayTime(n.due_at);
  if (n.kind === 'case_shared' || n.kind === 'reminder') return `Reply by ${at}`;
  if (n.kind === 'case_decided' || n.kind === 'standing_adjusted') return `Appeal by ${at}`;
  return `By ${at}`;
}

/** The header link's accessible name: "Notices, 2 unread". */
export function noticesLabel(unread: number): string {
  return unread > 0 ? `Notices, ${unread} unread` : 'Notices';
}
