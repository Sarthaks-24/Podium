/**
 * The signed-in user, as reactive state.
 *
 * loadSession() asks GET /api/me once at start-up: 200 means signed in, 401
 * means signed out. Any other failure also leaves the app signed out and
 * returns the error so the shell can say what happened. signOut() ends the
 * session on the server and goes to the sign-in page.
 */
import { ApiError, getJSON, sendJSON } from './api';
import { navigate } from './router.svelte';

export type Role = 'student' | 'admin' | 'owner';

/** The current user, as returned by GET /api/me. */
export interface User {
  id: number;
  email: string;
  name: string;
  avatar_url: string;
  role: Role;
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
    session.user = user;
    session.status = 'signed-in';
    return null;
  } catch (err) {
    session.user = undefined;
    session.status = 'signed-out';
    if (err instanceof ApiError && err.status === 401) return null;
    return err instanceof ApiError ? err : new ApiError(0, 'unexpected', String(err));
  }
}

/**
 * Ends the session and goes to /signin. The local state is cleared even when
 * the request fails, and the error is returned so the caller can report it.
 */
export async function signOut(): Promise<ApiError | null> {
  let failure: ApiError | null = null;
  try {
    await sendJSON<void>('POST', '/api/auth/logout');
  } catch (err) {
    failure = err instanceof ApiError ? err : new ApiError(0, 'unexpected', String(err));
  }
  session.user = undefined;
  session.status = 'signed-out';
  navigate('/signin');
  return failure;
}
