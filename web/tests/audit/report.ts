// Builds cache/audit-<date>/REPORT.md from the cells the spec wrote, after the
// run and whether or not checks failed. The exit code comes from the failed tests.
import type { FullConfig } from '@playwright/test';
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { auditDir, rawDir, THEMES, WIDTHS, type Cell } from './paths';

export default function report(config: FullConfig) {
  const raw = rawDir(config.rootDir);
  const cells: Cell[] = existsSync(raw)
    ? readdirSync(raw)
        .filter((f) => f.endsWith('.json'))
        .flatMap((f) => JSON.parse(readFileSync(`${raw}/${f}`, 'utf8')) as Cell[])
    : [];
  const names = [...new Set(cells.map((c) => c.screen))].sort();

  const lines: string[] = [];
  const failing = cells.filter((c) => c.failures.length > 0);
  lines.push(`# Screen audit, ${process.env.AUDIT_DATE}`, '');
  lines.push(`Screens: ${names.length}. Cells (screen x width x theme): ${cells.length}. Passing: ${cells.length - failing.length}. Failing: ${failing.length}.`, '');
  lines.push('Screenshots: `<screen>-<width>-<theme>.png` in this folder. Checks per cell: no Vite error overlay, no horizontal overflow, axe (WCAG 2.2 A and AA) with the state open, and for a dialog, focus inside the page and not on body after Escape.', '');

  lines.push('## Screens', '');
  for (const name of names) {
    lines.push(`### ${name}`, '');
    lines.push('| Width | ' + THEMES.join(' | ') + ' |', '|---|' + THEMES.map(() => '---').join('|') + '|');
    for (const width of WIDTHS) {
      const row = THEMES.map((theme) => {
        const c = cells.find((x) => x.screen === name && x.width === width && x.theme === theme);
        if (!c) return 'not run';
        return c.failures.length === 0 ? 'pass' : c.failures.join('; ').replaceAll('|', '/').replaceAll('\n', ' ');
      });
      lines.push(`| ${width} | ${row.join(' | ')} |`);
    }
    lines.push('');
  }

  lines.push('## Axe violations', '');
  const violations = cells.flatMap((c) => c.axe.map((v) => ({ c, v })));
  if (violations.length === 0) lines.push('None.', '');
  else {
    lines.push('| Screen | Width | Theme | Rule | Impact | Targets |', '|---|---|---|---|---|---|');
    for (const { c, v } of violations) {
      lines.push(`| ${c.screen} | ${c.width} | ${c.theme} | ${v.rule}: ${v.help} | ${v.impact} | ${v.targets.map((t) => `\`${t.replaceAll('|', '/')}\``).join(', ')} |`);
    }
    lines.push('');
  }

  const byCheck = new Map<string, number>();
  for (const c of failing) for (const f of c.failures) byCheck.set(f.split(':')[0], (byCheck.get(f.split(':')[0]) ?? 0) + 1);
  lines.push('## Totals', '');
  lines.push(`- Failing cells: ${failing.length} of ${cells.length}`, `- Axe violations: ${violations.length}`);
  for (const [check, n] of [...byCheck].sort()) lines.push(`- ${check}: ${n}`);
  if (cells.length === 0) lines.push('- No results were written: the run did not reach any screen.');
  lines.push('');

  writeFileSync(`${auditDir(config.rootDir)}/REPORT.md`, lines.join('\n'));
  console.log(`Audit report: cache/audit-${process.env.AUDIT_DATE}/REPORT.md (${failing.length} of ${cells.length} cells failing)`);
}
