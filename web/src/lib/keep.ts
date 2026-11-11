/**
 * Copy and rules for "Keep me signed in" (v2.1.0, PF-14), as pure functions:
 * the sign-in page's tick, the account menu's switch and the Dashboard's
 * "Staying signed in" section. The server decides everything; this only
 * words it and counts who a change would sign out.
 */
import { countOf } from './format';

/** Who of one role is kept signed in. */
export type KeepMode = 'nobody' | 'choose' | 'everyone';

/** The roles the setting names, in the order the Dashboard lists them. */
export const KEEP_ROLES = [
  { key: 'students', label: 'Students' },
  { key: 'organisers', label: 'Organisers' },
  { key: 'admins', label: 'Admins' },
  { key: 'owner', label: 'Owner' },
] as const;

export type KeepRole = (typeof KEEP_ROLES)[number]['key'];

/** GET and PUT /api/admin/keep-signed-in, the setting. */
export interface KeepPolicy {
  students: KeepMode;
  organisers: KeepMode;
  admins: KeepMode;
  owner: KeepMode;
  days: number;
  until_sign_out: boolean;
}

/** GET /api/admin/keep-signed-in. */
export interface KeepSettings {
  policy: KeepPolicy;
  /** People holding a kept session, by role. */
  remembered: Record<KeepRole, number>;
  /** True when the owner's own session is a kept one. */
  you_remembered: boolean;
}

/** What the public config says about the sign-in page's tick. */
export interface KeepConfig {
  offerTick: boolean;
  days: number;
  untilSignOut: boolean;
}

/** The tick's setting before the config loads: not offered. */
export const NO_KEEP: KeepConfig = { offerTick: false, days: 30, untilSignOut: false };

/** The first help line under the tick and the account menu's switch. */
export function keepHelp(days: number, untilSignOut: boolean): string {
  return untilSignOut
    ? 'Stay signed in on this device until you sign out.'
    : `Stay signed in on this device for ${countOf(days, 'day')} after you last use Podium.`;
}

/** The second help line: the lab and shared computer warning. */
export const KEEP_WARNING = 'Leave this off on a lab or shared computer.';

/** Shown once after a sign-in whose tick the setting did not honour. */
export const KEEP_IGNORED_NOTICE =
  'Podium does not keep your account signed in, so the tick was not used. You are signed out when the browser closes or after 12 hours.';

export const DAYS_ERROR = 'Enter a whole number of days from 1 to 365.';

/** The line under a role set to Everyone for admins or the owner. */
export const ADMIN_EVERYONE_WARNING = 'A signed-in admin can change contests and paste AI keys. Use this only on your own computer.';

/** The banner while some role is kept signed in without choosing, or until sign-out. */
export const SHARED_COMPUTER_WARNING = 'On a shared or lab computer the next person will be signed in as the last one unless they sign out.';

/** True when the number of days is a whole number from 1 to 365. */
export function validDays(text: string): boolean {
  return /^\d{1,3}$/.test(text.trim()) && Number(text) >= 1 && Number(text) <= 365;
}

/** True when some role is kept signed in at all, so "Sign out after" is used. */
export function anyKept(p: KeepPolicy): boolean {
  return KEEP_ROLES.some((r) => p[r.key] !== 'nobody');
}

/** True when the banner about shared computers applies. */
export function showsSharedWarning(p: KeepPolicy): boolean {
  return KEEP_ROLES.some((r) => p[r.key] === 'everyone') || (anyKept(p) && p.until_sign_out);
}

/** How many people a save would sign out: those holding a kept session in the roles that become Nobody. */
export function peopleSignedOut(was: KeepPolicy, now: KeepPolicy, counts: Record<KeepRole, number>): number {
  return KEEP_ROLES.reduce((n, r) => n + (was[r.key] !== 'nobody' && now[r.key] === 'nobody' ? counts[r.key] : 0), 0);
}

/** The confirm before a save that signs people out, or null when nobody would be. */
export function signOutConfirm(
  was: KeepPolicy,
  now: KeepPolicy,
  counts: Record<KeepRole, number>,
  youRemembered: boolean,
): { title: string; body: string; label: string } | null {
  const n = peopleSignedOut(was, now, counts);
  if (n === 0) return null;
  const youOut = youRemembered && was.owner !== 'nobody' && now.owner === 'nobody';
  const people = countOf(n, 'person', 'people');
  return {
    title: `Sign out ${people}?`,
    body: `${people} who chose to stay signed in will be signed out now${youOut ? ', you included' : ''}. They can sign in again.${youOut ? '' : ' You stay signed in.'}`,
    label: `Sign out ${people}`,
  };
}

/** The toast after a save. */
export function savedToast(signedOut: number): string {
  return signedOut > 0 ? `Saved. ${countOf(signedOut, 'person', 'people')} ${signedOut === 1 ? 'was' : 'were'} signed out.` : 'Saved';
}

const NOTICE_COOKIE = 'podium_notice';

/**
 * Reads and clears the one-time notice cookie the server sets when a tick
 * was not used. True once, on the first page after that sign-in.
 */
export function takeKeepNotice(): boolean {
  const found = document.cookie.split('; ').some((c) => c === `${NOTICE_COOKIE}=keep_ignored`);
  if (found) document.cookie = `${NOTICE_COOKIE}=; Path=/; Max-Age=0; SameSite=Lax`;
  return found;
}
