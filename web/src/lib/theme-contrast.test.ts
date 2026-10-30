// Checks every theme in tokens.css, with both verdict sets, against the
// accessibility rules: text contrast, control boundaries, the editor
// well, verdicts on their tints, and the distance between the action
// colour and the verdicts (also under simulated colour blindness for the
// colour-blind safe set).
import { describe, expect, it } from 'vitest';
import tokensCss from '../styles/tokens.css?raw';
import { COLOUR_TOKENS, contrast, distance, parseTokenRules, resolveTheme, type Vision } from './colour';
import { THEMES } from './theme';

const rules = parseTokenRules(tokensCss);
const HIGH_CONTRAST = new Set(['hc-light', 'hc-dark']);
// About four just-noticeable differences in OKLab. The worst pair shipped is
// 0.091 (dark colour-blind set, deuteranopia).
const MIN_DISTANCE = 0.09;

/** color-mix(in srgb, a p, b): a straight mix of the encoded channels. */
function mixSrgb(a: string, b: string, p: number): string {
  const ch = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const [x, y] = [ch(a), ch(b)];
  return `#${x.map((v, i) => Math.round(v * p + y[i] * (1 - p)).toString(16).padStart(2, '0')).join('')}`;
}

function failures(id: string, signal: boolean): string[] {
  const t = resolveTheme(rules, id, signal);
  const out: string[] = [];
  const need = (fg: string, bg: string, min: number) => {
    const ratio = contrast(t[fg], t[bg]);
    if (ratio < min) out.push(`${fg} on ${bg}: ${ratio.toFixed(2)} < ${min}`);
  };
  const text = HIGH_CONTRAST.has(id) ? 7 : 4.5;
  for (const bg of ['--paper', '--surface', '--surface-2']) {
    need('--ink', bg, text);
    need('--ink-muted', bg, text);
    need('--ink-faint', bg, 3);
    need('--act', bg, text);
  }
  need('--act-ink', '--act', text);
  need('--ink', '--act-tint', text);
  need('--act', '--act-tint', 4.5);
  need('--line-strong', '--paper', 3);
  need('--line-strong', '--surface', 3);
  need('--well-ink', '--well', 7);
  for (const v of ['--ok', '--part', '--bad', '--wait']) {
    need(v, `${v}-tint`, text);
    need(v, '--surface', text);
    need(v, '--paper', text);
    need('--ink', `${v}-tint`, text);
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
  return out;
}

describe('themes in tokens.css', () => {
  it('has a rule for every theme in theme.ts', () => {
    for (const { id } of THEMES) {
      expect(() => resolveTheme(rules, id, false), id).not.toThrow();
    }
  });

  it('defines every colour token in every theme', () => {
    for (const { id } of THEMES) {
      const t = resolveTheme(rules, id, true);
      const missing = COLOUR_TOKENS.filter((k) => !/^#[0-9a-f]{6}$/i.test(t[k] ?? ''));
      expect(missing, id).toEqual([]);
    }
  });

  it('puts each theme in the family theme.ts says', () => {
    for (const { id, family } of THEMES) {
      expect(resolveTheme(rules, id, false)['color-scheme'], id).toBe(family);
    }
  });

  for (const { id } of THEMES) {
    for (const signal of [false, true]) {
      it(`${id}${signal ? ' with colour-blind safe verdicts' : ''} meets the contrast rules`, () => {
        expect(failures(id, signal)).toEqual([]);
      });
    }
  }

  it('keeps code readable on the shared-passage tint in every theme', () => {
    const root = rules.find((r) => r.media === null && r.selector === ':root' && '--code-match' in r.vars);
    expect(root?.vars['--code-match']).toBe('color-mix(in srgb, var(--code-match-edge) 28%, var(--well))');
    const edge = root?.vars['--code-number'] ?? '';
    for (const { id } of THEMES) {
      const t = resolveTheme(rules, id, false);
      const tint = mixSrgb(edge, t['--well'], 0.28);
      expect(contrast(t['--well-ink'], tint), id).toBeGreaterThanOrEqual(4.5);
      expect(contrast(edge, t['--well']), `${id} edge`).toBeGreaterThanOrEqual(3);
    }
  });

  it('draws the editor focus ring at 3:1 or more on the well in every theme', () => {
    const root = rules.find((r) => r.media === null && r.selector === ':root' && '--code-focus' in r.vars);
    expect(root?.vars['--code-focus']).toBe('var(--code-keyword)');
    const ring = root?.vars['--code-keyword'] ?? '';
    for (const { id } of THEMES) {
      const t = resolveTheme(rules, id, false);
      expect(contrast(ring, t['--well']), id).toBeGreaterThanOrEqual(3);
    }
  });

  it('repeats the Dark theme exactly for a system that prefers dark', () => {
    const system = rules.find((r) => r.media !== null && r.selector === ':root:not([data-theme])');
    const dark = resolveTheme(rules, 'dark', false);
    expect(system).toBeDefined();
    for (const k of COLOUR_TOKENS) expect(system?.vars[k], k).toBe(dark[k]);
  });

  it('repeats the dark colour-blind set for a system that prefers dark', () => {
    const system = rules.find((r) => r.media !== null && r.selector === '[data-verdicts="signal"]:not([data-theme])');
    const dark = resolveTheme(rules, 'dark', true);
    for (const k of ['--ok', '--ok-tint', '--part', '--part-tint', '--bad', '--bad-tint']) {
      expect(system?.vars[k], k).toBe(dark[k]);
    }
  });
});

// Pairs the rules above leave out. 30 of them once failed; a token pass
// fixed them all (the comment colour, a 30 percent selection mix, --line-strong in three dark
// themes, and a Dismiss hover drawn as a border). Any new failure fails
// this test.
const EXTRA_KNOWN: string[] = [];

function extraFailures(id: string, signal: boolean): string[] {
  const t = resolveTheme(rules, id, signal);
  const root = rules.find((r) => r.media === null && r.selector === ':root' && '--code-selection' in r.vars)?.vars ?? {};
  const out: string[] = [];
  const need = (name: string, fg: string, bg: string, min: number) => {
    const ratio = contrast(fg, bg);
    if (ratio < min) out.push(`${id}${signal ? ' (signal)' : ''}: ${name}: ${ratio.toFixed(2)} < ${min}`);
  };
  const text = HIGH_CONTRAST.has(id) ? 7 : 4.5;
  need('--line-strong on --surface-2', t['--line-strong'], t['--surface-2'], 3);
  need('--ink-muted on --act-tint', t['--ink-muted'], t['--act-tint'], text);
  for (const k of ['keyword', 'string', 'number', 'comment', 'type', 'function', 'meta', 'invalid', 'gutter']) {
    need(`--code-${k} on --well`, root[`--code-${k}`], t['--well'], 4.5);
  }
  // --code-selection is color-mix(in srgb, var(--act) N%, var(--well)).
  const pct = Number(/var\(--act\) (\d+)%/.exec(root['--code-selection'] ?? '')?.[1]);
  need('--code-comment on --code-selection', root['--code-comment'], mixSrgb(t['--act'], t['--well'], pct / 100), 4.5);
  // The error toast's Dismiss on hover keeps the toast's --bad-tint fill (Toasts.svelte).
  need('error toast Dismiss hover', t['--bad'], t['--bad-tint'], text);
  return out;
}

describe('pairs the theme rules leave out', () => {
  it('passes every pair', () => {
    const all = THEMES.flatMap(({ id }) => [...extraFailures(id, false), ...extraFailures(id, true)]);
    expect([...new Set(all)]).toEqual(EXTRA_KNOWN);
  });
});

// The fills the controls mix from theme colours, with the text drawn on them.
function fillFailures(id: string, signal: boolean): string[] {
  const t = resolveTheme(rules, id, signal);
  const out: string[] = [];
  const text = HIGH_CONTRAST.has(id) ? 7 : 4.5;
  const need = (name: string, fg: string, bg: string) => {
    const ratio = contrast(fg, bg);
    if (ratio < text) out.push(`${id}${signal ? ' (signal)' : ''}: ${name}: ${ratio.toFixed(2)} < ${text}`);
  };
  // Primary buttons, hovered and pressed.
  need('--act-ink on hovered primary', t['--act-ink'], mixSrgb(t['--act'], t['--ink'], 0.86));
  need('--act-ink on pressed primary', t['--act-ink'], mixSrgb(t['--act'], t['--ink'], 0.76));
  // Secondary hover.
  need('--ink on secondary hover', t['--ink'], mixSrgb(t['--ink'], t['--surface'], 0.04));
  // The hover fill of an actionable table row, with everything a row shows.
  const row = mixSrgb(t['--surface-2'], t['--surface'], 0.5);
  for (const fg of ['--ink', '--ink-muted', '--act', '--ok', '--part', '--bad', '--wait']) need(`${fg} on row hover`, t[fg], row);
  // Muted table headers.
  need('--ink-muted on table header', t['--ink-muted'], t['--surface-2']);
  // Selected text.
  need('--ink on selection', t['--ink'], t['--act-tint']);
  return out;
}

/** rgba(r, g, b, a) composited over an opaque #rrggbb. */
function over(rgba: string, hex: string): string {
  const m = /rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)/.exec(rgba);
  if (!m) throw new Error(`not rgba: ${rgba}`);
  const a = Number(m[4]);
  const top = `#${[1, 2, 3].map((i) => Number(m[i]).toString(16).padStart(2, '0')).join('')}`;
  return mixSrgb(top, hex, a);
}

describe('text on mixed control fills', () => {
  it('keeps text readable on every mixed control fill', () => {
    const all = THEMES.flatMap(({ id }) => [...fillFailures(id, false), ...fillFailures(id, true)]);
    expect(all).toEqual([]);
  });

  it('has a scrim that dims the page in every theme, light or dark', () => {
    for (const { id } of THEMES) {
      const t = resolveTheme(rules, id, false);
      expect(t['--scrim'], id).toMatch(/^rgba\(/);
      const dimmed = over(t['--scrim'], t['--paper']);
      // Darker than the page, and the dialog's surface stands apart from it.
      expect(contrast(dimmed, '#000000'), id).toBeLessThanOrEqual(contrast(t['--paper'], '#000000'));
      expect(contrast(t['--surface'], dimmed), id).toBeGreaterThan(1.05);
    }
  });
});

describe('colour maths', () => {
  it('matches known contrast ratios', () => {
    expect(contrast('#000000', '#FFFFFF')).toBeCloseTo(21, 5);
    expect(contrast('#767676', '#FFFFFF')).toBeCloseTo(4.54, 2);
  });

  it('measures no distance between a colour and itself', () => {
    expect(distance('#2B3FD6', '#2B3FD6')).toBe(0);
    expect(distance('#2B3FD6', '#2B3FD6', 'deutan')).toBe(0);
  });

  it('shows red and green collapsing under deuteranopia', () => {
    expect(distance('#137A48', '#B42318')).toBeGreaterThan(0.2);
    expect(distance('#137A48', '#B42318', 'deutan')).toBeLessThan(distance('#137A48', '#B42318'));
  });
});
