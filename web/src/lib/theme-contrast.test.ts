// Checks every theme in tokens.css, with both verdict sets, against the
// rules in decision D41: text contrast, control boundaries, the editor
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
// 0.091 (dark colour-blind set, deuteranopia); see D41.
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

// X5 (v2 frontend plan 6.7, audit 06-14): pairs the rules above leave out.
// P0 adds them to the test only; the fixes belong to F-V2 (P1), which
// empties X5_KNOWN as it fixes each pair. A pair that starts failing, or
// one that F-V2 fixes, changes the list and fails this test.
const X5_KNOWN: string[] = [
  'light: error toast Dismiss hover: 4.49 < 4.5',
  'light (signal): error toast Dismiss hover: 4.46 < 4.5',
  'nordic-frost: --code-comment on --code-selection: 4.24 < 4.5',
  'nordic-frost: error toast Dismiss hover: 4.49 < 4.5',
  'nordic-frost (signal): --code-comment on --code-selection: 4.24 < 4.5',
  'nordic-frost (signal): error toast Dismiss hover: 4.46 < 4.5',
  'soft-lavender: error toast Dismiss hover: 4.49 < 4.5',
  'soft-lavender (signal): error toast Dismiss hover: 4.46 < 4.5',
  'dark: --code-comment on --code-selection: 3.19 < 4.5',
  'dark (signal): --code-comment on --code-selection: 3.19 < 4.5',
  'overcast: --line-strong on --surface-2: 2.97 < 3',
  'overcast: --code-comment on --code-selection: 2.43 < 4.5',
  'overcast (signal): --line-strong on --surface-2: 2.97 < 3',
  'overcast (signal): --code-comment on --code-selection: 2.43 < 4.5',
  'oled-black: --line-strong on --surface-2: 2.84 < 3',
  'oled-black: --code-comment on --code-selection: 3.39 < 4.5',
  'oled-black (signal): --line-strong on --surface-2: 2.84 < 3',
  'oled-black (signal): --code-comment on --code-selection: 3.39 < 4.5',
  'nordic-slate: --line-strong on --surface-2: 2.58 < 3',
  'nordic-slate: --code-comment on --code-selection: 2.46 < 4.5',
  'nordic-slate (signal): --line-strong on --surface-2: 2.58 < 3',
  'nordic-slate (signal): --code-comment on --code-selection: 2.46 < 4.5',
  'cosmic-violet: --code-comment on --code-selection: 3.18 < 4.5',
  'cosmic-violet (signal): --code-comment on --code-selection: 3.18 < 4.5',
  'hc-light: error toast Dismiss hover: 6.43 < 7',
  'hc-light (signal): error toast Dismiss hover: 5.81 < 7',
  'hc-dark: --code-comment on --code-selection: 2.89 < 4.5',
  'hc-dark: error toast Dismiss hover: 6.82 < 7',
  'hc-dark (signal): --code-comment on --code-selection: 2.89 < 4.5',
  'hc-dark (signal): error toast Dismiss hover: 5.65 < 7',
];

function x5Failures(id: string, signal: boolean): string[] {
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
  // The error toast's Dismiss on hover: --bad on 12 percent --bad over --bad-tint (Toasts.svelte).
  need('error toast Dismiss hover', t['--bad'], mixSrgb(t['--bad'], t['--bad-tint'], 0.12), text);
  return out;
}

describe('X5 contrast pairs', () => {
  it('fails only on the pairs listed for F-V2', () => {
    const all = THEMES.flatMap(({ id }) => [...x5Failures(id, false), ...x5Failures(id, true)]);
    expect([...new Set(all)]).toEqual(X5_KNOWN);
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
