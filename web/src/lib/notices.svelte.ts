/**
 * The unread notice count the header and the account menu show (v2 P4
 * F9.1). It starts from GET /api/me (`notifications_unread`), is set again
 * from each list the inbox loads, drops when notices are marked read, and
 * is read again from /api/me when a live stream reconnects or the tab
 * comes back after a while (refreshUnread), so a notice released when a
 * contest ends shows without a reload. The count is 0 while the person is
 * in a live contest: the server holds notices until it ends.
 */
import { getJSON, sendJSON } from './api';
import type { Notice } from './notifications';
import { session } from './session.svelte';

class NoticeState {
  unread = $state(0);
}

/** The unread count the shell shows. */
export const notices = new NoticeState();

/** The shortest gap between two background reads of the count. */
export const REFRESH_GAP_MS = 60_000;

let lastRefresh = 0;

/** Sets the count from the session's /api/me. */
export function adoptUnread(count: number | undefined | null): void {
  notices.unread = typeof count === 'number' && count > 0 ? Math.floor(count) : 0;
}

/**
 * Reads the unread count again from /api/me, at most once a minute unless
 * `force`. Failures are quiet: the count stays as it was.
 */
export async function refreshUnread(force = false, now = Date.now()): Promise<void> {
  if (session.status !== 'signed-in') return;
  if (!force && now - lastRefresh < REFRESH_GAP_MS) return;
  lastRefresh = now;
  try {
    const me = await getJSON<{ notifications_unread?: number }>('/api/me');
    adoptUnread(me.notifications_unread);
  } catch {
    // The count waits for the next read.
  }
}

/** Marks notices read; the count drops by the ones that were unread. */
export async function markRead(list: readonly Notice[]): Promise<void> {
  const ids = list.filter((n) => n.read_at === null).map((n) => n.id);
  if (ids.length === 0) return;
  await sendJSON<void>('POST', '/api/me/notifications/read', { ids });
  notices.unread = Math.max(0, notices.unread - ids.length);
}
