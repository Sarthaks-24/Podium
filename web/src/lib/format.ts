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

/** Formats Unix milliseconds as "21 Oct, 00:11" in local time, for a sentence. */
export function formatDayTime(ms: number): string {
  const d = new Date(ms);
  return `${d.getDate()} ${MONTHS[d.getMonth()]}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
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

/** A run of text, set in Fragment Mono when it is a number (v2 S2, audit 01-19). */
export interface TextPart {
  text: string;
  mono: boolean;
}

/**
 * Splits text into number runs and word runs, so only the digits are set in
 * mono: "2 d 23 h" keeps "d" and "h" in the interface face. A clock such as
 * "01:12:48" is one number run.
 */
export function numberParts(text: string): TextPart[] {
  const parts: TextPart[] = [];
  for (const m of text.matchAll(/(\d+(?:[:.]\d+)*)|(\D+)/g)) {
    parts.push({ text: m[0], mono: m[1] !== undefined });
  }
  return parts;
}

/** A score as "240 of 300", with mono digits and the word in the interface face. */
export function scoreParts(score: number, max?: number | null): TextPart[] {
  if (max === undefined || max === null) return [{ text: String(score), mono: true }];
  return [
    { text: String(score), mono: true },
    { text: ' of ', mono: false },
    { text: String(max), mono: true },
  ];
}
