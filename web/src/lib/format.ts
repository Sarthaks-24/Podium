/**
 * Formatting for dates, counts and role names shown in the interface.
 *
 * Dates use the viewer's local time in one fixed shape, "16 Oct 2026 19:42",
 * so tables line up and read the same in every browser locale.
 */
import type { Role } from './session.svelte';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function pad(n: number): string {
  return String(n).padStart(2, '0');
}

/** Formats Unix milliseconds as "16 Oct 2026 19:42" in local time. */
export function formatDateTime(ms: number): string {
  const d = new Date(ms);
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** A count with its noun: "1 minute", "3 minutes". The plural defaults to noun + "s". */
export function countOf(n: number, noun: string, plural = `${noun}s`): string {
  return `${n} ${n === 1 ? noun : plural}`;
}

const ROLE_LABELS: Record<Role, string> = { student: 'Student', admin: 'Admin', owner: 'Owner' };

/** Sentence-case name of a role. */
export function roleLabel(role: Role): string {
  return ROLE_LABELS[role];
}
