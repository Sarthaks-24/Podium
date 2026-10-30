/**
 * The one test for "this person organises": organisers, admins and owners
 * (V4-16). Screens and the router call it instead of comparing role names,
 * so a new role is added in one place.
 */
import type { Role } from './session.svelte';

/** True for every role that runs contests and problems: organiser, admin and owner. */
export function organises(role: Role | undefined): boolean {
  return role === 'organiser' || role === 'admin' || role === 'owner';
}
