/**
 * The server's run mode (PODIUM_MODE, decision D48) as reactive state,
 * read from GET /api/auth/config. "testing" means sign-in is a form with no
 * Google account behind it; the shell shows a banner so nobody mistakes a
 * testing server for the real one. Unknown until the config has loaded.
 * The same response carries the organisers' contact email, which
 * PageState names when a screen is closed to the viewer.
 */
import { getJSON } from './api';

export type RunMode = 'testing' | 'dev' | 'production';

class ModeState {
  mode = $state<RunMode | 'unknown'>('unknown');
  /** The organisers' contact email, or "" until the config loads or when none is set. */
  contactEmail = $state('');
}

/** Shared run-mode state. */
export const runMode = new ModeState();

/** Turns any value into a run mode; anything unexpected counts as production. */
export function parseMode(value: unknown): RunMode {
  return value === 'testing' || value === 'dev' ? value : 'production';
}

/** Loads the mode once; failures leave it unknown, which shows no banner. */
export async function loadMode(): Promise<void> {
  try {
    const c = await getJSON<{ mode?: string; contact_email?: string }>('/api/auth/config');
    runMode.mode = parseMode(c.mode);
    runMode.contactEmail = typeof c.contact_email === 'string' ? c.contact_email : '';
  } catch {
    // The sign-in page reports configuration errors itself.
  }
}
