/**
 * "The site was updated at 14:02" (D112): after an emergency deploy inside
 * the 48 hours before a contest, GET /api/me carries `site_updated_at` for
 * 12 hours. A signed-in student sees one line in the shell, once per
 * update: the time is remembered on this device (localStorage
 * podium-site-updated-seen) as soon as the line shows, so a reload does not
 * bring it back, and it stays on the page it appeared on until dismissed.
 * The deploy restarts the server, so App.svelte reads /api/me again after
 * any live stream reconnects (onStreamReconnect in lib/stream.ts).
 */

export const SEEN_KEY = 'podium-site-updated-seen';

type Store = Pick<Storage, 'getItem' | 'setItem'> | null;

function local(): Store {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

/** The last update time this device has shown, or 0. */
export function seenUpdate(store: Store = local()): number {
  try {
    const n = Number(store?.getItem(SEEN_KEY) ?? 0);
    return Number.isFinite(n) ? n : 0;
  } catch {
    return 0;
  }
}

/** Remembers that the update at `at` was shown. */
export function markUpdateSeen(at: number, store: Store = local()): void {
  try {
    store?.setItem(SEEN_KEY, String(Math.max(at, seenUpdate(store))));
  } catch {
    // Shown again after a reload; nothing else depends on it.
  }
}

/** True when an update time is set and newer than the one this device showed. */
export function isNewUpdate(updatedAt: number | null | undefined, seen: number): boolean {
  return typeof updatedAt === 'number' && updatedAt > 0 && updatedAt > seen;
}

/** The line itself, with the local time as "14:02". */
export function updateLine(updatedAt: number): string {
  const d = new Date(updatedAt);
  return `The site was updated at ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}.`;
}
