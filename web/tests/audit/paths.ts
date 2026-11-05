// Where the audit keeps its output: cache/audit-<date>/ at the repository root
// (git-ignored), found from this file (web/tests/audit), so it is the same
// whichever Playwright config runs the spec.
import { fileURLToPath } from 'node:url';

export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;

const ROOT = fileURLToPath(new URL('../../../', import.meta.url));

export function auditDir(): string {
  return `${ROOT}cache/audit-${process.env.AUDIT_DATE ?? new Date().toLocaleDateString('en-CA')}`;
}

/** One file per screen and theme, written by the spec and read by the report. */
export function rawDir(): string {
  return `${auditDir()}/.raw`;
}

/** What one screen at one width and theme found. */
export interface Cell {
  screen: string;
  width: number;
  theme: string;
  /** Failed checks, one sentence each; empty means pass. */
  failures: string[];
  axe: { rule: string; impact: string; help: string; targets: string[] }[];
}
