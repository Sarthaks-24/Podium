/**
 * The signed-in user, as reactive state.
 *
 * loadSession() asks GET /api/me once at start-up: 200 means signed in, 401
 * means signed out. Any other failure also leaves the app signed out and
 * returns the error so the shell can say what happened. A loaded session
 * brings the account's display preferences with it. signOut() ends the
 * session on the server, returns the display to the defaults (the next
 * person at a shared machine starts fresh), removes every local draft
 * mirror (lib/drafts.ts), and goes to the sign-in page. Signing in as a
 * different account from the last one on this device also removes them.
 *
 * Screens holding unsaved work register a save with beforeSignOut(); sign
 * out waits for those saves (at most SIGN_OUT_WAIT_MS) before it ends the
 * session, so the workspace's last edits reach the server first (audit
 * 01-9). During a live contest the workspace also registers a guard with
 * guardSignOut(): the shell then asks before signing out (P5 F7.6, N2),
 * and the saves still run first.
 */
import { ApiError, getJSON, sendJSON } from './api';
import { forgetDraftsAtSignOut, noteSignedInUser } from './drafts';
import { adoptAccountPreferences, resetPreferences } from './preferences.svelte';
import { navigate } from './router.svelte';

export type Role = 'student' | 'admin' | 'owner';

/** The current user, as returned by GET /api/me. */
export interface User {
  id: number;
  email: string;
  name: string;
  avatar_url: string;
  role: Role;
  /** Theme id, or "" to follow the system (lib/theme.ts). */
  theme: string;
  verdicts: string;
  /** When an emergency deploy restarted the site in the last 12 hours (Unix ms), else null (D112). */
  site_updated_at?: number | null;
  /** Unread notices; 0 while the person is in a live contest (v1.4.0, contract C9). */
  notifications_unread?: number;
  /** The person hid their own progress page (v1.4.0, contract C9). */
  hide_progress?: boolean;
}

export type SessionStatus = 'loading' | 'signed-out' | 'signed-in';

class SessionState {
  status = $state<SessionStatus>('loading');
  user = $state<User | undefined>(undefined);
}

/** Shared session state. */
export const session = new SessionState();

/** The name to show for a user: their name, or their email when it is empty. */
export function displayName(user: Pick<User, 'name' | 'email'>): string {
  return user.name.trim() !== '' ? user.name : user.email;
}

/**
 * Loads the session from the server. Resolves to null on success or when
 * signed out, or to the ApiError when the server could not answer.
 */
export async function loadSession(): Promise<ApiError | null> {
  try {
    const user = await getJSON<User>('/api/me');
    noteSignedInUser(user.id);
    session.user = user;
    session.status = 'signed-in';
    adoptAccountPreferences(user.theme, user.verdicts);
    return null;
  } catch (err) {
    session.user = undefined;
    session.status = 'signed-out';
    if (err instanceof ApiError && err.status === 401) return null;
    return err instanceof ApiError ? err : new ApiError(0, 'unexpected', String(err));
  }
}

/**
 * Reads /api/me again for its site_updated_at only, after a live stream
 * reconnects (D112). Failures are quiet: the next page load tries again.
 */
export async function refreshSiteUpdated(): Promise<void> {
  if (session.status !== 'signed-in' || !session.user) return;
  try {
    const user = await getJSON<User>('/api/me');
    if (session.user && session.user.id === user.id) session.user = { ...session.user, site_updated_at: user.site_updated_at ?? null };
  } catch {
    // Nothing to show; the line waits for the next load.
  }
}

/** The longest sign out waits for registered saves. */
export const SIGN_OUT_WAIT_MS = 3000;

// Not reactive state: nothing renders from it.
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const savers = new Set<() => Promise<unknown>>();

/**
 * Registers a save to run before signing out; returns the function that
 * removes it. A failing or slow save never blocks the sign-out.
 */
export function beforeSignOut(save: () => Promise<unknown>): () => void {
  savers.add(save);
  return () => savers.delete(save);
}

// The screens that want sign out confirmed, newest last. Not reactive state either.
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const guards = new Set<string>();

/**
 * Asks the shell to confirm a sign out while this screen is open, with
 * `why` as the dialog's line; returns the function that removes it.
 */
export function guardSignOut(why: string): () => void {
  guards.add(why);
  return () => guards.delete(why);
}

/** The line to confirm sign out with, or null when nothing asks for it. */
export function signOutGuard(): string | null {
  let last: string | null = null;
  for (const g of guards) last = g;
  return last;
}

/** Runs every registered save, waiting at most SIGN_OUT_WAIT_MS in all. */
export async function runSignOutSaves(wait = SIGN_OUT_WAIT_MS): Promise<void> {
  if (savers.size === 0) return;
  const all = Promise.allSettled([...savers].map((save) => Promise.resolve().then(save)));
  let timer: ReturnType<typeof setTimeout> | undefined;
  const limit = new Promise<void>((resolve) => (timer = setTimeout(resolve, wait)));
  await Promise.race([all, limit]);
  clearTimeout(timer);
}

/**
 * Ends the session and goes to /signin. The local state is cleared even when
 * the request fails, and the error is returned so the caller can report it.
 */
export async function signOut(): Promise<ApiError | null> {
  let failure: ApiError | null = null;
  await runSignOutSaves();
  try {
    await sendJSON<void>('POST', '/api/auth/logout');
  } catch (err) {
    failure = err instanceof ApiError ? err : new ApiError(0, 'unexpected', String(err));
  }
  session.user = undefined;
  session.status = 'signed-out';
  resetPreferences();
  forgetDraftsAtSignOut();
  navigate('/signin');
  return failure;
}
