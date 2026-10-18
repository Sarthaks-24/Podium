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
