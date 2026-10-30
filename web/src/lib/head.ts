/**
 * Document title helpers. Every screen sets a unique title of the form
 * "Problems · podium" so browser tabs and history entries are readable.
 */

/** The product name, always the last part of the title. */
export const SITE_NAME = 'podium';

/** Joins non-empty parts and the product name with a middle dot. */
export function formatTitle(...parts: string[]): string {
  return [...parts.map((p) => p.trim()).filter((p) => p !== ''), SITE_NAME].join(' · ');
}

/** Sets document.title from the given parts. */
export function setTitle(...parts: string[]): void {
  if (typeof document !== 'undefined') document.title = formatTitle(...parts);
}
