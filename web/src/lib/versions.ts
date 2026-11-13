/**
 * Bring back an earlier version of a problem (v2.1.0 stage H): the shapes of
 * the history list and the words of the confirm and the toast. The server
 * decides which versions can be brought back and refuses in its own words;
 * nothing here judges that.
 */
import { formatDateTime } from './format';

/** One version on GET /api/admin/problems/{id}/revisions. */
export interface Version {
  id: number;
  rev: number;
  created_at: number;
  created_by: number;
  reason: string;
  current: boolean;
  can_bring_back: boolean;
}

/** The answer of GET .../revisions. */
export interface History {
  revisions: Version[];
  total: number;
}

/** The answer of POST .../bring-back. */
export interface BroughtBack {
  validation_queued: boolean;
}

/** GET the history of problem `id`. */
export const historyPath = (id: number) => `/api/admin/problems/${id}/revisions`;

/** POST brings version `revision` of problem `id` back. */
export const bringBackPath = (id: number, revision: number) => `/api/admin/problems/${id}/revisions/${revision}/bring-back`;

/** Why a version has no button. */
export const NOT_KEPT = "This version's files were not kept, so it cannot be brought back.";

/** "3 Nov 2026" from a time in milliseconds, in the viewer's zone. */
export function versionDay(ms: number): string {
  return formatDateTime(ms).replace(/ \d{2}:\d{2}$/, '');
}

/** "3 Nov 2026, 14:20". */
export function versionTime(ms: number): string {
  return formatDateTime(ms).replace(/ (\d{2}:\d{2})$/, ', $1');
}

/** The words of the confirm dialog for the version made at `ms`. */
export function confirmText(ms: number): { title: string; body: string; confirm: string } {
  return {
    title: `Bring back the version from ${versionTime(ms)}?`,
    body:
      'Its statement, limits, tests, solutions, starting code and locked code become a new version. Tags, difficulty and the test plan stay as they are now. ' +
      'The current version stays in the history. The problem must pass Validate again. ' +
      'Contests that already hold it, scheduled ones included, keep their version until you press Update students now.',
    confirm: 'Bring back this version',
  };
}

/** The toast after a bring back. */
export function broughtBackToast(ms: number, out: BroughtBack): string {
  const day = versionDay(ms);
  return out.validation_queued ? `Brought back the version from ${day}. Validation has started.` : `Brought back the version from ${day}. Validate it before you use it.`;
}
