/**
 * The "Hide my progress" preference (contract C9, `hide_progress` on
 * PUT /api/me/preferences; v2 P4 F9.2). The route takes the theme and the
 * verdict colours with it, so the call sends the ones on screen unchanged.
 * Hiding empties the person's own "Your progress" page and the Home line;
 * the results are kept, and organisers still see them.
 */
import { sendJSON } from './api';
import { preferences } from './preferences.svelte';
import { session } from './session.svelte';
import { toAccount } from './theme';

/** Saves the preference and updates the session; throws the ApiError when the server refuses. */
export async function saveHideProgress(hide: boolean): Promise<void> {
  await sendJSON<unknown>('PUT', '/api/me/preferences', { ...toAccount(preferences.current), hide_progress: hide });
  if (session.user) session.user = { ...session.user, hide_progress: hide };
}
