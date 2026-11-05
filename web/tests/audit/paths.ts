// Where the audit keeps its output: cache/audit-<date>/ at the repository root
// (git-ignored), found from the audit's test directory (web/tests/audit).
export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;

export function auditDir(testDir: string): string {
  return `${testDir}/../../../cache/audit-${process.env.AUDIT_DATE}`;
}

/** One file per screen and theme, written by the spec and read by the report. */
export function rawDir(testDir: string): string {
  return `${auditDir(testDir)}/.raw`;
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
