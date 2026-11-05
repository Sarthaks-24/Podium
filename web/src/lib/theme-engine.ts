/**
 * The theme engine: a background colour, an accent colour and a contrast
 * choice in, every interface token out, readable by construction.
 *
 * The person owns the hues; the engine owns lightness and moves it until
 * every D41 rule in theme-rules.ts passes for both verdict sets of the
 * theme's family. It returns the tokens,
 * the family (light or dark, from the background) and a list of what it
 * changed, in plain words, for the picker to show.
 *
 * What differs from a free-form theme builder, on purpose (V201-11): there
 * is one action colour, no second accent, and the verdict colours are never
 * derived from user input. They are fixed per family, per contrast level and
 * per verdict set (VERDICTS below; a test keeps them equal to tokens.css), so
 * "Accepted" and "Wrong" mean the same in every theme. The engine therefore
 * has to keep the background readable against those fixed colours and keep
 * the action colour a clear OKLab distance from each of them.
 *
 * The maths is ported from the AdminPanelManager project's derive.js (a
 * lightness search outward from the chosen colour), not its token names. No
 * dependency. The property test in theme-engine.test.ts runs it over
 * thousands of seeded random inputs.
 */
import { contrast, distance, fromOklch, luminance, mixSrgb, toOklch } from './colour';
import { MIN_DISTANCE, d41Failures } from './theme-rules';

export type Family = 'light' | 'dark';
export type ContrastLevel = 'standard' | 'high';
export type VerdictSetName = 'standard' | 'signal';

/** The three colours a person chooses. */
export interface ThemeInput {
  /** #rrggbb */
  background: string;
  /** #rrggbb */
  accent: string;
  contrast: ContrastLevel;
}

/** What the engine returns. */
export interface DerivedTheme {
  family: Family;
  contrast: ContrastLevel;
  /** The interface tokens, by custom property name including the leading "--". */
  vars: Record<string, string>;
  /** What the engine changed, in sentence case, for the picker. Empty when nothing moved. */
  adjustments: string[];
}

/** The custom properties the engine sets, in this order. */
export const ENGINE_TOKENS = [
  '--paper', '--surface', '--surface-2', '--ink', '--ink-muted', '--ink-faint', '--line', '--line-strong',
  '--act', '--act-ink', '--act-tint', '--scrim', '--shadow-menu',
] as const;

type Verdicts = Record<string, string>;
const v = (ok: string, okT: string, part: string, partT: string, bad: string, badT: string, wait: string, waitT: string): Verdicts => ({
  '--ok': ok, '--ok-tint': okT, '--part': part, '--part-tint': partT, '--bad': bad, '--bad-tint': badT, '--wait': wait, '--wait-tint': waitT,
});

/** The fixed verdict colours by family, level and set. tokens.css holds the same values. */
export const VERDICTS: Record<Family, Record<ContrastLevel, Record<VerdictSetName, Verdicts>>> = {
  light: {
    standard: {
      standard: v('#137A48', '#E2F2E8', '#9C5C12', '#FBEEDA', '#B42318', '#FBE4E1', '#5B6660', '#ECEEE9'),
      signal: v('#267260', '#E5EEEC', '#6E519D', '#EEEAF3', '#A73C00', '#F4E8E0', '#5B6660', '#ECEEE9'),
    },
    high: {
      standard: v('#0A5A2E', '#E3F1E8', '#6A4400', '#FBEEDA', '#8A1209', '#FBE4E1', '#333333', '#F0F0F0'),
      signal: v('#07565B', '#E0EFEF', '#59369F', '#EDE9F6', '#902602', '#F5E7E2', '#333333', '#F0F0F0'),
    },
  },
  dark: {
    standard: {
      standard: v('#5CC489', '#17301F', '#E9A23B', '#332611', '#F0776B', '#3A1C18', '#9AA39D', '#202521'),
      signal: v('#00DEB6', '#163D34', '#F098F7', '#3C323E', '#FF9766', '#3F3127', '#9AA39D', '#202521'),
    },
    high: {
      standard: v('#7EE2A8', '#0B2616', '#FFC56B', '#2B1C05', '#FF9A8F', '#300F0B', '#D0D0D0', '#1A1A1A'),
      signal: v('#00DEB6', '#0B2E28', '#CEA4FF', '#2C2534', '#FF9B5A', '#342419', '#D0D0D0', '#1A1A1A'),
    },
  },
};

/** The interface tokens of the four built-in themes: the engine's last resort. */
export const FALLBACK: Record<Family, Record<ContrastLevel, Record<string, string>>> = {
  light: {
    standard: {
      '--paper': '#F3F4F1', '--surface': '#FFFFFF', '--surface-2': '#ECEEE9', '--ink': '#16211C', '--ink-muted': '#5B6660',
      '--ink-faint': '#828B85', '--line': '#D8DBD3', '--line-strong': '#858981', '--act': '#2B3FD6', '--act-ink': '#FFFFFF',
      '--act-tint': '#E7EAFB', '--scrim': 'rgba(22, 33, 28, 0.4)', '--shadow-menu': '0 8px 24px rgba(22, 33, 28, 0.14)',
    },
    high: {
      '--paper': '#FFFFFF', '--surface': '#FFFFFF', '--surface-2': '#F0F0F0', '--ink': '#000000', '--ink-muted': '#333333',
      '--ink-faint': '#595959', '--line': '#767676', '--line-strong': '#3D3D3D', '--act': '#0025A2', '--act-ink': '#FFFFFF',
      '--act-tint': '#E5E9F7', '--scrim': 'rgba(22, 33, 28, 0.4)', '--shadow-menu': '0 8px 24px rgba(22, 33, 28, 0.14)',
    },
  },
  dark: {
    standard: {
      '--paper': '#121513', '--surface': '#1A1E1B', '--surface-2': '#202521', '--ink': '#E9ECE6', '--ink-muted': '#9AA39D',
      '--ink-faint': '#6F7872', '--line': '#2C322E', '--line-strong': '#68706B', '--act': '#8A97FF', '--act-ink': '#0E1012',
      '--act-tint': '#232846', '--scrim': 'rgba(0, 0, 0, 0.6)', '--shadow-menu': '0 8px 24px rgba(0, 0, 0, 0.5)',
    },
    high: {
      '--paper': '#000000', '--surface': '#0D0D0D', '--surface-2': '#1A1A1A', '--ink': '#FFFFFF', '--ink-muted': '#E6E6E6',
      '--ink-faint': '#9A9A9A', '--line': '#5C5C5C', '--line-strong': '#9A9A9A', '--act': '#9CCBFF', '--act-ink': '#000000',
      '--act-tint': '#14263A', '--scrim': 'rgba(0, 0, 0, 0.6)', '--shadow-menu': '0 8px 24px rgba(0, 0, 0, 0.5)',
    },
  },
};

/** True for #rrggbb in either case. */
export function isHex(value: unknown): value is string {
  return typeof value === 'string' && /^#[0-9a-fA-F]{6}$/.test(value);
}

/** The family a background belongs to: light when black text reads better on it than white. */
export function familyOf(background: string): Family {
  return luminance(background) > 0.179 ? 'light' : 'dark';
}

const VERDICT_WORD: Record<string, string> = { '--ok': 'Accepted', '--part': 'Partial', '--bad': 'Wrong' };
const VERDICT_COLOUR: Record<VerdictSetName, Record<string, string>> = {
  standard: { '--ok': 'green', '--part': 'amber', '--bad': 'red' },
  signal: { '--ok': 'teal', '--part': 'purple', '--bad': 'orange' },
};

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));

/** Steps outward from `from` in one direction until `pass` holds. Returns that lightness, or null at the end of the range. */
function search(from: number, dir: 1 | -1, step: number, pass: (l: number) => boolean): number | null {
  for (let i = 0; ; i++) {
    const l = from + dir * i * step;
    if (l < -1e-9 || l > 1 + 1e-9) return null;
    if (pass(clamp01(l))) return clamp01(l);
  }
}

/** Derives a whole theme from three inputs. Inputs must be #rrggbb; callers validate. */
export function deriveTheme(input: ThemeInput): DerivedTheme {
  const level: ContrastLevel = input.contrast === 'high' ? 'high' : 'standard';
  const high = level === 'high';
  const text = high ? 7 : 4.5;
  const family = familyOf(input.background);
  const light = family === 'light';
  const notes: string[] = [];
  const note = (s: string) => {
    if (!notes.includes(s)) notes.push(s);
  };
  const sets = (['standard', 'signal'] as const).map((name) => ({ name, tokens: VERDICTS[family][level][name] }));
  const give = (vars: Record<string, string>): DerivedTheme => ({ family, contrast: level, vars, adjustments: notes });
  const fallback = (why: string): DerivedTheme => {
    notes.length = 0;
    note(`Colours replaced with the built-in ${light ? 'light' : 'dark'}${high ? ' high contrast' : ''} colours because ${why} could not be made readable`);
    return give({ ...FALLBACK[family][level] });
  };

  // Background: keep its hue, keep it a calm base, and push it to an extreme
  // far enough that the fixed verdict colours stay readable on every surface.
  const bg = toOklch(input.background);
  const cap = light ? 0.035 : 0.05;
  if (bg.c > cap + 0.01) note('Background made less saturated so it stays a calm base');
  // Ink takes a trace of the background's hue; a neutral background (pure black or white) gets neutral ink.
  const inkChroma = bg.c < 0.004 ? 0 : 0.012;
  const pole = fromOklch({ l: light ? 0.2 : 0.96, c: inkChroma, h: bg.h });
  const surfaces = (paper: string) => ({
    paper,
    surface: light ? mixSrgb('#FFFFFF', paper, 0.7) : mixSrgb(pole, paper, 0.04),
    surface2: light ? mixSrgb(pole, paper, 0.05) : mixSrgb(pole, paper, 0.08),
  });
  const verdictsReadable = (s: ReturnType<typeof surfaces>) =>
    sets.every((set) =>
      ['--ok', '--part', '--bad', '--wait'].every((k) => [s.paper, s.surface, s.surface2].every((g) => contrast(set.tokens[k], g) >= text)),
    );
  const paperAt = (l: number) => fromOklch({ l, c: Math.min(bg.c, cap), h: bg.h });
  const start = light ? Math.max(bg.l, 0.9) : Math.min(bg.l, 0.3);
  const paperL = search(start, light ? 1 : -1, 0.005, (l) => verdictsReadable(surfaces(paperAt(l))));
  if (paperL === null) return fallback('the background');
  if (Math.abs(paperL - bg.l) > 0.04) {
    note(light ? 'Background lightened so verdict colours stay readable' : 'Background darkened so verdict colours stay readable');
  }
  const paper = paperAt(paperL);
  const { surface, surface2 } = surfaces(paper);
  const grounds = [paper, surface, surface2];
  const rowHover = mixSrgb(surface2, surface, 0.5);

  // Ink: from the pole toward black or white until 7:1 on every ground and
  // text-level on every verdict tint.
  const allTints = sets.flatMap((s) => ['--ok-tint', '--part-tint', '--bad-tint', '--wait-tint'].map((k) => s.tokens[k]));
  const inkOf = (l: number) => fromOklch({ l, c: inkChroma, h: bg.h });
  const inkL = search(light ? 0.2 : 0.96, light ? -1 : 1, 0.01, (l) => {
    const c = inkOf(l);
    return grounds.every((g) => contrast(c, g) >= 7) && allTints.every((g) => contrast(c, g) >= text);
  });
  if (inkL === null) return fallback('the text colour');
  const ink = inkOf(inkL);
  const darkInk = '#0E1012';
  const verdictsAll = sets.flatMap((s) => ['--ok', '--part', '--bad'].map((k) => ({ k, set: s.name, hex: s.tokens[k] })));

  // Accent: dark enough (light family) or light enough (dark family) to read
  // as text on the background and carry button text, and clear of every verdict.
  const tintOf = (a: string) => mixSrgb(a, paper, light ? 0.12 : 0.18);
  const inkOn = (a: string) => (contrast('#FFFFFF', a) >= contrast(darkInk, a) ? '#FFFFFF' : darkInk);
  const actPasses = (a: string) => {
    if (!grounds.every((g) => contrast(a, g) >= text)) return false;
    const onAct = inkOn(a);
    if (contrast(onAct, a) < text || contrast(onAct, mixSrgb(a, ink, 0.86)) < text || contrast(onAct, mixSrgb(a, ink, 0.76)) < text) return false;
    const tint = tintOf(a);
    if (contrast(a, tint) < 4.5 || contrast(ink, tint) < text || contrast(a, rowHover) < text) return false;
    return verdictsAll.every((x) => distance(a, x.hex) >= MIN_DISTANCE);
  };
  const acc = toOklch(input.accent);
  const fit = (hue: number): string | null => {
    const l = search(acc.l, light ? -1 : 1, 0.01, (x) => actPasses(fromOklch({ l: x, c: acc.c, h: hue })));
    return l === null ? null : fromOklch({ l, c: acc.c, h: hue });
  };
  const act = fit(acc.h);
  if (act === null) return fallback('the accent colour');
  // Name the verdict colour the chosen accent sat on top of, if any; that is
  // the reason a person would not see in a plain "darkened".
  const clash = verdictsAll.filter((x) => distance(input.accent, x.hex) < MIN_DISTANCE).sort((p, q) => distance(input.accent, p.hex) - distance(input.accent, q.hex))[0];
  if (clash && act.toLowerCase() !== input.accent.toLowerCase()) {
    const where = clash.set === 'signal' ? ' in the colour-blind safe set' : '';
    note(`Accent moved away from the ${VERDICT_COLOUR[clash.set][clash.k]} used for ${VERDICT_WORD[clash.k]}${where}`);
  } else if (Math.abs(toOklch(act).l - acc.l) > 0.02) {
    note(light ? 'Accent darkened so text on buttons stays readable' : 'Accent lightened so it stays readable on the background');
  }
  const actTint = tintOf(act);

  // Secondary ink, faint ink and the control border, each stepped from a
  // mix of ink and paper toward ink until it reads.
  const stepTo = (from: number, ok: (c: string) => boolean): string => {
    for (let w = from; w <= 1.0001; w += 0.02) {
      const c = mixSrgb(ink, paper, Math.min(1, w));
      if (ok(c)) return c;
    }
    return ink;
  };
  const muted = stepTo(0.72, (c) => [...grounds, actTint, rowHover].every((g) => contrast(c, g) >= text));
  const faint = stepTo(0.55, (c) => grounds.every((g) => contrast(c, g) >= 3));
  const strong = stepTo(0.5, (c) => grounds.every((g) => contrast(c, g) >= 3));
  const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)).join(', ');

  const vars: Record<string, string> = {
    '--paper': paper,
    '--surface': surface,
    '--surface-2': surface2,
    '--ink': ink,
    '--ink-muted': muted,
    '--ink-faint': faint,
    '--line': mixSrgb(ink, paper, 0.13),
    '--line-strong': strong,
    '--act': act,
    '--act-ink': inkOn(act),
    '--act-tint': actTint,
    '--scrim': light ? `rgba(${rgb(ink)}, 0.4)` : 'rgba(0, 0, 0, 0.6)',
    '--shadow-menu': light ? `0 8px 24px rgba(${rgb(ink)}, 0.14)` : '0 8px 24px rgba(0, 0, 0, 0.5)',
  };
  for (const set of sets) {
    if (d41Failures({ ...vars, ...set.tokens }, { high, signal: set.name === 'signal' }).length > 0) {
      return fallback('the colours');
    }
  }
  return give(vars);
}
