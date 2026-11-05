// The engine is what makes a custom theme and a preset safe by construction
// (V201-11), so this file is the proof: a fixed-seed run over thousands of
// random inputs plus the named edge cases, each result checked against every
// D41 rule in both verdict sets (standard and colour-blind safe), at both
// contrast levels. It also keeps the engine's constants equal to tokens.css.
import { describe, expect, it } from 'vitest';
import tokensCss from '../styles/tokens.css?raw';
import { contrast, distance, fromOklch, parseTokenRules, resolveFamily, resolveTheme } from './colour';
import {
  ENGINE_TOKENS, FALLBACK, VERDICTS, deriveTheme, familyOf,
  type ContrastLevel, type DerivedTheme, type Family, type ThemeInput, type VerdictSetName,
} from './theme-engine';
import { d41Failures } from './theme-rules';

const rules = parseTokenRules(tokensCss);

/** mulberry32: a small seeded generator, so every run sees the same inputs. */
function rng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const hex = (r: number, g: number, b: number) => `#${[r, g, b].map((x) => x.toString(16).padStart(2, '0')).join('')}`;

/** Every rule, both verdict sets. Returns the broken rules, labelled. */
function violations(theme: DerivedTheme): string[] {
  const high = theme.contrast === 'high';
  const out: string[] = [];
  for (const set of ['standard', 'signal'] as VerdictSetName[]) {
    const tokens = { ...theme.vars, ...VERDICTS[theme.family][theme.contrast][set] };
    for (const f of d41Failures(tokens, { high, signal: set === 'signal' })) out.push(`${set}: ${f}`);
  }
  for (const k of ENGINE_TOKENS) if (!theme.vars[k]) out.push(`missing ${k}`);
  return out;
}

const EDGE_COLOURS = ['#000000', '#FFFFFF', '#808080', '#777777', '#7F7F7F', '#FF0000', '#00FF00', '#0000FF', '#FFFF00', '#FF00FF', '#00FFFF', '#F3F4F1', '#121513'];

describe('theme engine', () => {
  it('passes every D41 rule for 2,400 seeded random inputs', () => {
    const next = rng(20261105);
    const bad: string[] = [];
    let replaced = 0;
    let adjusted = 0;
    const N = 2400;
    for (let i = 0; i < N; i++) {
      // Three kinds of background: any RGB, near-grey, and the mid-tones where hard cases live.
      const kind = i % 3;
      const pick = () => Math.floor(next() * 256);
      const background = kind === 0 ? hex(pick(), pick(), pick()) : kind === 1 ? (() => { const g = pick(); return hex(g, g, Math.min(255, g + Math.floor(next() * 12))); })() : (() => { const g = 90 + Math.floor(next() * 90); return hex(g, Math.max(0, g - Math.floor(next() * 40)), Math.min(255, g + Math.floor(next() * 40))); })();
      const input: ThemeInput = { background, accent: hex(pick(), pick(), pick()), contrast: i % 4 === 3 ? 'high' : 'standard' };
      const theme = deriveTheme(input);
      const v = violations(theme);
      if (v.length > 0) bad.push(`${JSON.stringify(input)}: ${v.join('; ')}`);
      if (theme.family !== familyOf(background)) bad.push(`${JSON.stringify(input)}: wrong family ${theme.family}`);
      if (theme.adjustments.some((a) => a.startsWith('Colours replaced'))) replaced++;
      if (theme.adjustments.length > 0) adjusted++;
    }
    expect(bad.slice(0, 5)).toEqual([]);
    // The last resort must stay rare, or the engine is not doing its job.
    expect(replaced / N).toBeLessThan(0.02);
    expect(adjusted).toBeGreaterThan(0);
  });

  describe('edge cases', () => {
    for (const level of ['standard', 'high'] as ContrastLevel[]) {
      for (const background of EDGE_COLOURS) {
        for (const accent of EDGE_COLOURS) {
          it(`${background} with ${accent} (${level})`, () => {
            expect(violations(deriveTheme({ background, accent, contrast: level }))).toEqual([]);
          });
        }
      }
    }

    it('handles an accent equal to each verdict colour in every set', () => {
      for (const family of ['light', 'dark'] as Family[]) {
        const background = family === 'light' ? '#F3F4F1' : '#121513';
        for (const level of ['standard', 'high'] as ContrastLevel[]) {
          for (const set of ['standard', 'signal'] as VerdictSetName[]) {
            for (const k of ['--ok', '--part', '--bad', '--wait']) {
              const accent = VERDICTS[family][level][set][k];
              const theme = deriveTheme({ background, accent, contrast: level });
              expect(violations(theme), `${family} ${level} ${set} ${k}`).toEqual([]);
              for (const set2 of ['standard', 'signal'] as VerdictSetName[]) {
                for (const k2 of ['--ok', '--part', '--bad']) {
                  expect(distance(theme.vars['--act'], VERDICTS[family][level][set2][k2])).toBeGreaterThanOrEqual(0.09);
                }
              }
            }
          }
        }
      }
    });

    it('moves the accent away from a verdict it matched and says so', () => {
      const said: string[] = [];
      for (const family of ['light', 'dark'] as Family[]) {
        const background = family === 'light' ? '#FAFBF8' : '#0B0D0C';
        for (let h = 0; h < 360; h += 5) {
          const accent = fromOklch({ l: family === 'light' ? 0.5 : 0.8, c: 0.14, h });
          const theme = deriveTheme({ background, accent, contrast: 'standard' });
          expect(violations(theme), `${family} hue ${h}`).toEqual([]);
          said.push(...theme.adjustments.filter((a) => a.startsWith('Accent moved away')));
        }
      }
      expect(said.length).toBeGreaterThan(0);
      expect(deriveTheme({ background: '#FAFBF8', accent: '#137A48', contrast: 'standard' }).adjustments.join(' ')).toContain('Accent moved away from the green used for Accepted');
      expect(said.every((a) => /^Accent moved away from the (green|amber|red) used for (Accepted|Partial|Wrong)$/.test(a))).toBe(true);
    });

    it('handles an accent equal to the background', () => {
      for (const background of ['#F3F4F1', '#121513', '#FFFFFF', '#000000']) {
        const theme = deriveTheme({ background, accent: background, contrast: 'standard' });
        expect(violations(theme), background).toEqual([]);
        expect(contrast(theme.vars['--act'], theme.vars['--paper'])).toBeGreaterThanOrEqual(4.5);
      }
    });

    it('keeps the family from the background and the pure extremes calm', () => {
      expect(deriveTheme({ background: '#000000', accent: '#2B3FD6', contrast: 'standard' }).family).toBe('dark');
      expect(deriveTheme({ background: '#FFFFFF', accent: '#2B3FD6', contrast: 'standard' }).family).toBe('light');
      expect(deriveTheme({ background: '#000000', accent: '#2B3FD6', contrast: 'standard' }).vars['--paper']).toBe('#000000');
    });

    it('reports nothing when the inputs already pass', () => {
      expect(deriveTheme({ background: '#FAFBF8', accent: '#2B3FD6', contrast: 'standard' }).adjustments).toEqual([]);
    });

    it('says what it changed when a mid-tone background is pushed to an extreme', () => {
      const theme = deriveTheme({ background: '#808080', accent: '#808080', contrast: 'standard' });
      expect(theme.adjustments.length).toBeGreaterThan(0);
      expect(theme.adjustments.every((a) => /^[A-Z]/.test(a) && !/[!–—]/.test(a))).toBe(true);
    });

    it('is deterministic', () => {
      const input: ThemeInput = { background: '#3A4A6B', accent: '#E07A5F', contrast: 'high' };
      expect(deriveTheme(input)).toEqual(deriveTheme(input));
    });

    it('never touches the verdict colours', () => {
      const theme = deriveTheme({ background: '#202030', accent: '#00FF00', contrast: 'standard' });
      for (const k of ['--ok', '--part', '--bad', '--wait']) expect(theme.vars[k]).toBeUndefined();
    });
  });
});

describe('the engine and tokens.css agree', () => {
  for (const family of ['light', 'dark'] as Family[]) {
    for (const level of ['standard', 'high'] as ContrastLevel[]) {
      for (const set of ['standard', 'signal'] as VerdictSetName[]) {
        it(`${family} ${level} ${set} verdicts`, () => {
          const css = resolveFamily(rules, family, level === 'high', set === 'signal');
          for (const [k, value] of Object.entries(VERDICTS[family][level][set])) expect(css[k]?.toUpperCase(), k).toBe(value.toUpperCase());
        });
      }
    }
  }

  it('keeps the fallback tokens equal to the four built-in themes', () => {
    const ids: [Family, ContrastLevel, string][] = [
      ['light', 'standard', 'light'], ['dark', 'standard', 'dark'], ['light', 'high', 'hc-light'], ['dark', 'high', 'hc-dark'],
    ];
    for (const [family, level, id] of ids) {
      const t = resolveTheme(rules, id, false);
      for (const [k, value] of Object.entries(FALLBACK[family][level])) {
        if (k === '--scrim' || k === '--shadow-menu') {
          const dark = family === 'dark';
          expect(value, `${id} ${k}`).toContain(dark ? '0, 0, 0' : '22, 33, 28');
        } else {
          expect(t[k].toUpperCase(), `${id} ${k}`).toBe(value.toUpperCase());
        }
      }
    }
  });

  it('gives the family blocks the verdict names tokens.css defines for every set', () => {
    for (const family of ['light', 'dark'] as Family[]) {
      for (const high of [false, true]) {
        for (const signal of [false, true]) {
          const css = resolveFamily(rules, family, high, signal);
          for (const k of ['--ok', '--ok-tint', '--part', '--part-tint', '--bad', '--bad-tint', '--wait', '--wait-tint']) {
            expect(css[k], `${family} ${high} ${signal} ${k}`).toMatch(/^#[0-9a-f]{6}$/i);
          }
        }
      }
    }
  });
});
