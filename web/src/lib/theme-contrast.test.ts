// Checks every theme in tokens.css, with both verdict sets, against the
// accessibility rules: text contrast, control boundaries, the editor
// well, verdicts on their tints, and the distance between the action
// colour and the verdicts (also under simulated colour blindness for the
// colour-blind safe set).
import { describe, expect, it } from 'vitest';
import tokensCss from '../styles/tokens.css?raw';
import { COLOUR_TOKENS, contrast, distance, mixSrgb, parseTokenRules, resolveFamily, resolveTheme, type Vision } from './colour';
import { d41Failures, over } from './theme-rules';
import { EDITOR_SCHEMES } from './editor-colours';
import { THEMES, derive } from './theme';
import { PRESETS } from './theme-presets';

const rules = parseTokenRules(tokensCss);
const HIGH_CONTRAST = new Set(['hc-light', 'hc-dark']);
// About four just-noticeable differences in OKLab. The worst pair shipped is
// 0.091 (dark colour-blind set, deuteranopia).
const MIN_DISTANCE = 0.09;

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
  // --code-selection is color-mix(in srgb, var(--code-keyword) N%, var(--well)).
  const pct = Number(/var\(--code-keyword\) (\d+)%/.exec(root['--code-selection'] ?? '')?.[1]);
  need('--code-comment on --code-selection', root['--code-comment'], mixSrgb(root['--code-keyword'], t['--well'], pct / 100), 4.5);
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

// The engine also keeps verdict text readable on --surface-2 (the table header); every
// built-in theme meets it with no exceptions.
describe('the shared D41 rules', () => {
  for (const { id } of THEMES) {
    for (const signal of [false, true]) {
      it(`${id}${signal ? ' with colour-blind safe verdicts' : ''} passes theme-rules.ts`, () => {
        const found = d41Failures(resolveTheme(rules, id, signal), { high: HIGH_CONTRAST.has(id), signal });
        expect(found).toEqual([]);
      });
    }
  }

  it('gives a dark family the Dark theme verdicts, and the high contrast family the hc-dark ones', () => {
    for (const [family, id, high] of [['dark', 'dark', false], ['dark', 'hc-dark', true], ['light', 'light', false], ['light', 'hc-light', true]] as const) {
      for (const signal of [false, true]) {
        const f = resolveFamily(rules, family, high, signal);
        const t = resolveTheme(rules, id, signal);
        for (const k of ['--ok', '--ok-tint', '--part', '--part-tint', '--bad', '--bad-tint', '--wait', '--wait-tint']) {
          expect(f[k], `${family} high=${high} signal=${signal} ${k}`).toBe(t[k]);
        }
      }
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

// The editor colour schemes (styles/tokens.css, lib/editor-colours.ts). Each
// replaces the well and every --code-* colour as a set, so it is checked on
// its own: syntax on the well, on the selection and on the active line,
// the gutter, the focus ring and the shared-passage tint.
const plainRoot = rules.find((r) => r.media === null && r.selector === ':root' && '--code-selection' in r.vars)?.vars ?? {};

/** Tokens of a scheme: the Night defaults in :root, then the scheme's own block. Night takes `well` from a theme. */
function scheme(id: string, theme: Record<string, string>): Record<string, string> {
  if (id === 'night') return { ...plainRoot, '--well': theme['--well'], '--well-ink': theme['--well-ink'] };
  const own = rules.find((r) => r.media === null && r.selector.split(',').map((x) => x.trim()).includes(`:root[data-editor="${id}"]`));
  if (!own) throw new Error(`no block for editor scheme ${id}`);
  return { ...plainRoot, ...own.vars };
}

function schemeFailures(id: string, e: Record<string, string>): string[] {
  const out: string[] = [];
  const high = id === 'high-contrast';
  const syntax = high ? 7 : 4.5;
  const need = (name: string, fg: string, bg: string, min: number) => {
    const ratio = contrast(fg, bg);
    if (ratio < min) out.push(`${id}: ${name}: ${ratio.toFixed(2)} < ${min}`);
  };
  const pct = (token: string, fallback: string) => Number(new RegExp(String.raw`var\(${fallback}\) (\d+)%`).exec(e[token] ?? '')?.[1]);
  const selection = mixSrgb(e['--code-keyword'], e['--well'], pct('--code-selection', '--code-keyword') / 100);
  const active = mixSrgb(e['--well-ink'], e['--well'], pct('--code-active-line', '--well-ink') / 100);
  const match = mixSrgb(e['--code-number'], e['--well'], pct('--code-match', '--code-match-edge') / 100);
  need('--well-ink on --well', e['--well-ink'], e['--well'], 7);
  for (const k of ['keyword', 'string', 'number', 'comment', 'type', 'function', 'meta', 'invalid']) {
    need(`--code-${k} on --well`, e[`--code-${k}`], e['--well'], syntax);
    need(`--code-${k} on selection`, e[`--code-${k}`], selection, syntax);
    need(`--code-${k} on active line`, e[`--code-${k}`], active, syntax);
  }
  need('--well-ink on selection', e['--well-ink'], selection, syntax);
  need('--code-gutter on --well', e['--code-gutter'], e['--well'], 4.5);
  // The active line's own number is drawn in --well-ink (lib/editor.ts), not the gutter colour.
  need('--well-ink on active line', e['--well-ink'], active, 7);
  need('--code-keyword (focus ring) on --well', e['--code-keyword'], e['--well'], 3);
  need('--well-ink on shared passage', e['--well-ink'], match, 4.5);
  need('shared-passage edge on --well', e['--code-number'], e['--well'], 3);
  return out;
}

describe('editor colour schemes', () => {
  it('lists the same five ids as the server', () => {
    expect(EDITOR_SCHEMES.map((s) => s.id)).toEqual(['night', 'paper', 'midnight', 'dusk', 'high-contrast']);
  });

  it('keeps the selection and active-line mixes in :root', () => {
    expect(plainRoot['--code-selection']).toMatch(/^color-mix\(in srgb, var\(--code-keyword\) \d+%, var\(--well\)\)$/);
    expect(plainRoot['--code-active-line']).toMatch(/^color-mix\(in srgb, var\(--well-ink\) \d+%, var\(--well\)\)$/);
  });

  for (const { id } of EDITOR_SCHEMES) {
    if (id === 'night') continue;
    it(`${id} defines every well and code colour`, () => {
      const e = scheme(id, {});
      const own = rules.find((r) => r.media === null && r.selector.split(',').map((x) => x.trim()).includes(`:root[data-editor="${id}"]`));
      const names = ['--well', '--well-ink', '--well-edge', ...['keyword', 'string', 'number', 'comment', 'type', 'function', 'meta', 'invalid', 'gutter'].map((k) => `--code-${k}`)];
      expect(names.filter((k) => !(k in (own?.vars ?? {})))).toEqual([]);
      for (const k of names.filter((n) => n !== '--well-edge')) expect(e[k], k).toMatch(/^#[0-9a-f]{6}$/i);
    });
    it(`${id} paints its picker snippet from the same values`, () => {
      const sample = rules.find((r) => r.media === null && r.selector.split(',').map((x) => x.trim()).includes(`[data-editor-sample="${id}"]`));
      const own = rules.find((r) => r.media === null && r.selector.split(',').map((x) => x.trim()).includes(`:root[data-editor="${id}"]`));
      expect(sample).toBe(own);
    });
  }

  for (const { id } of EDITOR_SCHEMES) {
    // Night keeps each theme's own well, so it is checked on all of them.
    const wells = id === 'night' ? THEMES.map((t) => [t.id, resolveTheme(rules, t.id, false)] as const) : [['any', {}] as const];
    for (const [theme, well] of wells) {
      it(`${id}${id === 'night' ? ` on the ${theme} well` : ''} meets the contrast rules`, () => {
        expect(schemeFailures(id, scheme(id, well as Record<string, string>))).toEqual([]);
      });
    }
  }

  it('Night as a sample equals the Night defaults', () => {
    const sample = rules.find((r) => r.media === null && r.selector === '[data-editor-sample="night"]')?.vars ?? {};
    for (const k of Object.keys(sample)) {
      if (k === '--well' || k === '--well-ink') continue;
      expect(sample[k], k).toBe(plainRoot[k]);
    }
    expect(sample['--well-ink']).toBe(plainRoot['--well-ink'] ?? sample['--well-ink']);
  });

  it('is checked by the same rules for the old per-theme code tests', () => {
    expect(plainRoot['--code-focus']).toBe('var(--code-keyword)');
  });
});

// Presets and custom themes: the interface tokens come from the engine and
// everything else (verdicts, color-scheme) from the family rules in
// tokens.css, exactly as the browser composes them from inline properties plus
// data-family and data-contrast. A dark custom theme must get the dark
// verdicts (E4b): the id-keyed rules match no preset or custom id.
describe('presets and custom themes through the family rules', () => {
  const inputs = [
    ...PRESETS.map((p) => [p.id, p.input] as const),
    ['custom dark', { background: '#1c2430', accent: '#e07a5f', contrast: 'standard' }] as const,
    ['custom dark high contrast', { background: '#0a0a14', accent: '#44ff88', contrast: 'high' }] as const,
    ['custom light', { background: '#f4efe6', accent: '#2f6f4f', contrast: 'standard' }] as const,
    ['custom light high contrast', { background: '#ffffff', accent: '#0000ff', contrast: 'high' }] as const,
  ];
  for (const [name, input] of inputs) {
    const d = derive(input);
    const high = d.contrast === 'high';
    for (const signal of [false, true]) {
      it(`${name}${signal ? ' with colour-blind safe verdicts' : ''} meets the contrast rules`, () => {
        const family = resolveFamily(rules, d.family, high, signal);
        expect(family['color-scheme']).toBe(d.family);
        expect(d41Failures({ ...family, ...d.vars }, { high, signal })).toEqual([]);
      });
    }
  }

  it('gives a dark family the dark verdicts and a light family the light ones', () => {
    const dark = resolveFamily(rules, 'dark', false, false);
    const light = resolveFamily(rules, 'light', false, false);
    expect(dark['--ok']).toBe(resolveTheme(rules, 'dark', false)['--ok']);
    expect(light['--ok']).toBe(resolveTheme(rules, 'light', false)['--ok']);
    expect(dark['--ok']).not.toBe(light['--ok']);
    expect(dark['color-scheme']).toBe('dark');
  });

  it('keeps the six presets apart from each other', () => {
    const papers = PRESETS.map((p) => derive(p.input).vars['--paper']);
    expect(new Set(papers).size).toBe(PRESETS.length);
  });

  it('leaves no preset needing the engine fallback', () => {
    for (const p of PRESETS) expect(derive(p.input).adjustments.some((a) => a.startsWith('Colours replaced')), p.id).toBe(false);
  });
});
