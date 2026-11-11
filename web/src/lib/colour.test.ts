import { describe, expect, it } from 'vitest';
import { contrast, distance, fromOklch, luminance, mixSrgb, parseTokenRules, resolveFamily, resolveTheme, toOklch } from './colour';

describe('contrast and luminance', () => {
  it('gives black and white 21 and equal colours 1', () => {
    expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 5);
    expect(contrast('#ffffff', '#000000')).toBeCloseTo(21, 5);
    expect(contrast('#336699', '#336699')).toBeCloseTo(1, 5);
  });
  it('matches the known ratio of #777777 on white', () => {
    expect(contrast('#777777', '#ffffff')).toBeCloseTo(4.48, 2);
  });
  it('uses the linear segment for very dark channels', () => {
    expect(luminance('#0a0a0a')).toBeCloseTo(0.003035, 6);
  });
  it('rejects anything that is not #rrggbb', () => {
    expect(() => luminance('#fff')).toThrow('not a #rrggbb colour');
    expect(() => luminance('red')).toThrow();
  });
});

describe('distance', () => {
  it('is zero for equal colours and large for black against white', () => {
    expect(distance('#336699', '#336699')).toBeCloseTo(0, 8);
    expect(distance('#000000', '#ffffff')).toBeGreaterThan(0.9);
  });
  it('shrinks red against green under red-green vision loss', () => {
    const normal = distance('#cc0000', '#00aa00');
    expect(distance('#cc0000', '#00aa00', 'protan')).toBeLessThan(normal);
    expect(distance('#cc0000', '#00aa00', 'deutan')).toBeLessThan(normal);
  });
});

describe('mixSrgb', () => {
  it('mixes the encoded channels', () => {
    expect(mixSrgb('#000000', '#ffffff', 0.5)).toBe('#808080');
    expect(mixSrgb('#ff0000', '#0000ff', 1)).toBe('#ff0000');
    expect(mixSrgb('#ff0000', '#0000ff', 0)).toBe('#0000ff');
  });
});

describe('oklch round trip', () => {
  it('returns the same colour within one channel step', () => {
    for (const hex of ['#336699', '#cc0000', '#00aa55', '#808080']) {
      const back = fromOklch(toOklch(hex));
      for (const i of [1, 3, 5]) {
        expect(Math.abs(parseInt(back.slice(i, i + 2), 16) - parseInt(hex.slice(i, i + 2), 16))).toBeLessThanOrEqual(1);
      }
    }
  });
  it('pulls an out-of-gamut chroma in and keeps lightness', () => {
    const hex = fromOklch({ l: 0.6, c: 0.5, h: 150 });
    expect(hex).toMatch(/^#[0-9a-f]{6}$/);
    expect(toOklch(hex).l).toBeCloseTo(0.6, 1);
  });
});

const CSS = `
/* comment */
:root, [data-theme="paper"] { color-scheme: light; --paper: #ffffff; --ink: #111111; --ok: #006600; }
[data-theme="paper"] { --ink: #222222; }
[data-family="dark"] { color-scheme: dark; --paper: #000000; --ink: #eeeeee; --ok: #00cc00; }
[data-family="dark"][data-contrast="high"] { --ok: #66ff66; }
[data-verdicts="signal"] { --ok: #0000cc; }
[data-verdicts="signal"][data-family="dark"] { --ok: #6666ff; }
@media (prefers-color-scheme: dark) { :root { --paper: #123456; } }
`;

describe('parseTokenRules', () => {
  const rules = parseTokenRules(CSS);
  it('reads plain rules and marks media rules', () => {
    expect(rules.find((r) => r.selector === '[data-theme="paper"]')?.vars['--ink']).toBe('#222222');
    const media = rules.find((r) => r.media !== null);
    expect(media?.media).toBe('(prefers-color-scheme: dark)');
    expect(media?.vars['--paper']).toBe('#123456');
  });
  it('reads color-scheme and drops comments', () => {
    expect(rules[0].vars['color-scheme']).toBe('light');
    expect(rules.some((r) => r.selector.includes('comment'))).toBe(false);
  });
});

describe('resolveTheme and resolveFamily', () => {
  const rules = parseTokenRules(CSS);
  it('layers the theme block over its family', () => {
    const t = resolveTheme(rules, 'paper', false);
    expect(t['--ink']).toBe('#222222');
    expect(t['--paper']).toBe('#ffffff');
  });
  it('applies the signal verdicts on top', () => {
    expect(resolveTheme(rules, 'paper', true)['--ok']).toBe('#0000cc');
  });
  it('throws for a theme with no family rule', () => {
    expect(() => resolveTheme(rules, 'nope', false)).toThrow('no family rule');
  });
  it('resolves a family with the high-contrast and signal layers in order', () => {
    expect(resolveFamily(rules, 'dark', false, false)['--ok']).toBe('#00cc00');
    expect(resolveFamily(rules, 'dark', true, false)['--ok']).toBe('#66ff66');
    expect(resolveFamily(rules, 'dark', true, true)['--ok']).toBe('#6666ff');
    expect(resolveFamily(rules, 'light', false, false)['--paper']).toBe('#ffffff');
  });
});
