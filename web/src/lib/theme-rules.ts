/**
 * The D41 readability rules, written once. A theme here is a map of the
 * colour tokens (--paper, --ink, --act, --ok and the rest) to #rrggbb, plus
 * --scrim as an rgba() string. d41Failures() lists every rule the map
 * breaks, for one verdict set; an empty list means it passes.
 *
 * The theme engine (theme-engine.ts) searches until this list is empty for
 * both verdict sets, and the tests (theme-contrast.test.ts,
 * theme-engine.test.ts) call the same function for the built-in themes, the
 * presets and thousands of random custom themes. The app's runtime does not
 * use it for anything else.
 */
import { contrast, distance, mixSrgb, type Vision } from './colour';

/** Smallest OKLab distance between the action colour and each verdict, and between verdicts. */
export const MIN_DISTANCE = 0.09;

/** Options for one run of the rules. */
export interface RuleOptions {
  /** The 7:1 level (the high-contrast themes and the "high" contrast choice). */
  high: boolean;
  /** The colour-blind safe verdict set is in `t`: distances are also checked under protanopia and deuteranopia. */
  signal: boolean;
}

/** rgba(r, g, b, a) composited over an opaque #rrggbb. */
export function over(rgba: string, hex: string): string {
  const m = /rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)/.exec(rgba);
  if (!m) throw new Error(`not rgba: ${rgba}`);
  const top = `#${[1, 2, 3].map((i) => Number(m[i]).toString(16).padStart(2, '0')).join('')}`;
  return mixSrgb(top, hex, Number(m[4]));
}

/** Every D41 rule the token map `t` breaks, as readable lines. */
export function d41Failures(t: Record<string, string>, { high, signal }: RuleOptions): string[] {
  const out: string[] = [];
  const text = high ? 7 : 4.5;
  const need = (name: string, fg: string, bg: string, min: number) => {
    const ratio = contrast(fg, bg);
    if (ratio < min) out.push(`${name}: ${ratio.toFixed(2)} < ${min}`);
  };
  const tok = (fg: string, bg: string, min: number) => need(`${fg} on ${bg}`, t[fg], t[bg], min);

  for (const bg of ['--paper', '--surface', '--surface-2']) {
    tok('--ink', bg, text);
    tok('--ink-muted', bg, text);
    tok('--ink-faint', bg, 3);
    tok('--act', bg, text);
  }
  tok('--act-ink', '--act', text);
  tok('--ink', '--act-tint', text);
  tok('--ink-muted', '--act-tint', text);
  tok('--act', '--act-tint', 4.5);
  tok('--line-strong', '--paper', 3);
  tok('--line-strong', '--surface', 3);
  tok('--line-strong', '--surface-2', 3);
  for (const v of ['--ok', '--part', '--bad', '--wait']) {
    tok(v, `${v}-tint`, text);
    tok(v, '--surface', text);
    tok(v, '--paper', text);
    tok(v, '--surface-2', text);
    tok('--ink', `${v}-tint`, text);
  }

  const pairs: [string, string][] = [
    ['--act', '--ok'], ['--act', '--part'], ['--act', '--bad'],
    ['--ok', '--part'], ['--ok', '--bad'], ['--part', '--bad'],
  ];
  const visions: Vision[] = signal ? ['normal', 'protan', 'deutan'] : ['normal'];
  for (const [a, b] of pairs) {
    for (const vision of visions) {
      if (vision !== 'normal' && a === '--act') continue;
      const d = distance(t[a], t[b], vision);
      if (d < MIN_DISTANCE) out.push(`${a} and ${b} (${vision}): distance ${d.toFixed(3)} < ${MIN_DISTANCE}`);
    }
  }

  // Primary buttons, hovered and pressed; secondary hover; actionable rows;
  // table headers; selected text.
  tok('--act-ink', '--act', text);
  need('--act-ink on hovered primary', t['--act-ink'], mixSrgb(t['--act'], t['--ink'], 0.86), text);
  need('--act-ink on pressed primary', t['--act-ink'], mixSrgb(t['--act'], t['--ink'], 0.76), text);
  need('--ink on secondary hover', t['--ink'], mixSrgb(t['--ink'], t['--surface'], 0.04), text);
  const row = mixSrgb(t['--surface-2'], t['--surface'], 0.5);
  for (const fg of ['--ink', '--ink-muted', '--act', '--ok', '--part', '--bad', '--wait']) need(`${fg} on row hover`, t[fg], row, text);

  // The scrim dims the page, and the dialog's surface stands apart from it.
  const dimmed = over(t['--scrim'], t['--paper']);
  if (contrast(dimmed, '#000000') > contrast(t['--paper'], '#000000')) out.push('--scrim does not dim --paper');
  if (contrast(t['--surface'], dimmed) <= 1.05) out.push('--surface does not stand apart from the dimmed page');
  return out;
}
