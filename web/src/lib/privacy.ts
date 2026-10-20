/**
 * The privacy notice (P3 FP.1, held for the owner's approval under D85).
 * The text is src/content/privacy.md; the line "<!-- retention -->" marks
 * where the table of retention periods goes. The table is never typed by
 * hand: the page renders it from GET /api/auth/config (contract C5), which
 * reads server/internal/retention/periods.go. The same Markdown is
 * rendered at build time into dist/privacy-body.html (vite.config.ts) for
 * the server's no-JavaScript body block, with a marked block in place of
 * the table.
 */
import source from '../content/privacy.md?raw';

/** The marker line in the Markdown. */
export const RETENTION_MARKER = '<!-- retention -->';

/** The Markdown before and after the retention table. */
export function privacyParts(markdown: string = source): { before: string; after: string } {
  const i = markdown.indexOf(RETENTION_MARKER);
  if (i < 0) return { before: markdown, after: '' };
  return { before: markdown.slice(0, i).trimEnd(), after: markdown.slice(i + RETENTION_MARKER.length).trimStart() };
}

/** The drafted notice, as written. */
export const PRIVACY_SOURCE: string = source;
