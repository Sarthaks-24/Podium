/**
 * What the Dashboard reads from GET /api/admin/people, the words it uses
 * for people, and the screen's copy of who may change whose account (the
 * server decides, server/internal/auth/rules.go; this only hides what it
 * would refuse). The roles above student are "organiser", "admin" and
 * "owner".
 */
import { ApiError, isSignedOut } from './api';
import { isRole, roleLabel } from './format';
import type { Organisation } from './mode.svelte';
import type { Role } from './session.svelte';

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

/** Any account, students included, as the account help sees it. */
export interface Account {
  id: number;
  email: string;
  name: string;
  role: string;
  disabled: boolean;
  last_login_at: number | null;
}

/** One person on the sign-in list, students included (V4-15). */
export interface ListedPerson {
  id: number;
  email: string;
  name: string;
  role: Role;
  last_login_at: number | null;
  disabled: boolean;
  /** The owner named in the server configuration: shown, never changed here. */
  locked: boolean;
}

/** People's three counts; disabled accounts are in none of them. */
export interface ListCounts {
  on_list: number;
  signed_in: number;
  never_came: number;
}

/** A verified Google address that was refused because it is not on the list. */
export interface Refusal {
  email: string;
  count: number;
  first_at: number;
  last_at: number;
}

/** What a paste would add (a dry run) or added. */
export interface ImportResult {
  added: string[];
  duplicates: string[];
  invalid: string[];
}

/** The answer of GET /api/admin/people. */
export interface PeopleData {
  people: Person[];
  list?: ListedPerson[];
  counts?: ListCounts;
  refused?: Refusal[];
  /** False while sign-in is closed (V4-17). */
  signin_open?: boolean;
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

/** The person signed in, as far as the account rules need them. */
export interface Viewer {
  id: number;
  role: Role;
}

/** True when the viewer may change this person's account at all (V4-16). */
export function mayChange(viewer: Viewer, person: Pick<ListedPerson, 'id' | 'role' | 'locked'>): boolean {
  if (person.locked) return false;
  if (viewer.role === 'owner') return true;
  if (viewer.role !== 'admin' || person.id === viewer.id) return false;
  return person.role === 'student' || person.role === 'organiser';
}

/** The roles the viewer may give: admins only student and organiser. */
export function rolesFor(viewer: Viewer): Role[] {
  if (viewer.role === 'owner') return ['student', 'organiser', 'admin', 'owner'];
  return viewer.role === 'admin' ? ['student', 'organiser'] : [];
}
