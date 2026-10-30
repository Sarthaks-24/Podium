/**
 * What the owner's Settings page reads from GET /api/admin/people, and the
 * words it uses for people. The roles above student are "organiser",
 * "admin" and "owner".
 */
import { ApiError, isSignedOut } from './api';
import { isRole, roleLabel } from './format';
import type { Organisation } from './mode.svelte';

export type OrganiserRole = 'organiser' | 'admin' | 'owner';

/** Someone with an organiser role. */
export interface Person {
  id: number;
  email: string;
  name: string;
  role: OrganiserRole;
  last_login_at: number | null;
  /** The owner named in the server configuration: shown, never changed here. */
  locked: boolean;
  disabled?: boolean;
}

/** An email domain whose Google accounts may sign in as students. */
export interface Domain {
  domain: string;
  created_at: number;
}

/** Any account, students included, as the account help sees it. */
export interface Account {
  id: number;
  email: string;
  name: string;
  role: string;
  disabled: boolean;
  last_login_at: number | null;
}

/** The answer of GET /api/admin/people. */
export interface PeopleData {
  people: Person[];
  domains: Domain[];
  disabled?: Account[];
  organisation?: Organisation;
}

/** A full email address, the shape the server accepts. */
export const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

/** The person's name, or their address when they gave none. */
export function personLabel(p: Pick<Person, 'name' | 'email'>): string {
  return p.name.trim() !== '' ? p.name : p.email;
}

/** A role in lower case for a sentence ("organiser"); an unknown role by its own name. */
export function roleWord(role: string): string {
  return isRole(role) ? roleLabel(role).toLowerCase() : role;
}

/** "an owner", "an admin" or "an organiser". */
export function withArticle(role: OrganiserRole): string {
  return `an ${roleWord(role)}`;
}

/**
 * Handles what any Settings request can hit: a 401 (lib/api.ts has
 * already signed the page out) and a 403 when the person is no longer an
 * owner, which closes the page through `onforbidden`. True when it did.
 */
export function settingsRefused(err: unknown, onforbidden: () => void): boolean {
  if (isSignedOut(err)) return true;
  if (err instanceof ApiError && err.status === 403 && err.code === 'forbidden') {
    onforbidden();
    return true;
  }
  return false;
}
