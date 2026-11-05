/**
 * Colour maths and a reader for styles/tokens.css, used by the theme tests
 * to check contrast. The app itself does not import this module.
 *
 * - contrast(): the WCAG 2 contrast ratio of two #rrggbb colours.
 * - distance(): Euclidean distance in OKLab, optionally after simulating
 *   protanopia or deuteranopia (Machado, Oliveira and Fernandes 2009,
 *   severity 1), to check that two colours stay apart.
 * - resolveTheme(): the colour tokens a theme ends up with, following the
 *   layering tokens.css documents: family base, theme block, then the
 *   colour-blind safe verdict layer. A Playwright test checks the browser
 *   agrees, so this simplified cascade cannot drift from the real one.
 * - resolveFamily(): the same for a custom theme or preset, which has only
 *   a family (light or dark), an optional high-contrast flag and the verdict
 *   set; the engine supplies the interface tokens.
 * - toOklch(), fromOklch(), mixSrgb(): the lightness maths the theme engine
 *   (theme-engine.ts) searches with.
 */

type RGB = [number, number, number];

function channels(hex: string): RGB {
  const m = /^#([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) throw new Error(`not a #rrggbb colour: ${hex}`);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((c) => c / 255) as RGB;
}

const linear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);

export function luminance(hex: string): number {
  const [r, g, b] = channels(hex).map(linear);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG 2 contrast ratio, from 1 to 21. */
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

export type Vision = 'normal' | 'protan' | 'deutan';

const SIMULATE: Record<Exclude<Vision, 'normal'>, number[][]> = {
  protan: [
    [0.152286, 1.052583, -0.204868],
    [0.114503, 0.786281, 0.099216],
    [-0.003882, -0.048116, 1.051998],
  ],
  deutan: [
    [0.367322, 0.860646, -0.227968],
    [0.280085, 0.672501, 0.047413],
    [-0.01182, 0.04294, 0.968881],
  ],
};

function oklab(hex: string, vision: Vision): RGB {
  let [r, g, b] = channels(hex).map(linear);
  if (vision !== 'normal') {
    const m = SIMULATE[vision];
    const clamp = (x: number) => Math.min(1, Math.max(0, x));
    [r, g, b] = m.map((row) => clamp(row[0] * r + row[1] * g + row[2] * b));
  }
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

/** Distance between two colours in OKLab, as seen with the given vision. */
export function distance(a: string, b: string, vision: Vision = 'normal'): number {
  const p = oklab(a, vision);
  const q = oklab(b, vision);
  return Math.hypot(p[0] - q[0], p[1] - q[1], p[2] - q[2]);
}

/** One rule from tokens.css: its selector and custom properties. */
export interface TokenRule {
  selector: string;
  media: string | null;
  vars: Record<string, string>;
}

/** Reads every rule in tokens.css, including those inside @media. */
export function parseTokenRules(css: string): TokenRule[] {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const rules: TokenRule[] = [];
  const walk = (src: string, media: string | null) => {
    let i = 0;
    while (i < src.length) {
      const open = src.indexOf('{', i);
      if (open < 0) break;
      const head = src.slice(i, open).trim();
      let depth = 1;
      let j = open + 1;
      for (; j < src.length && depth > 0; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') depth--;
      }
      const body = src.slice(open + 1, j - 1);
      if (head.startsWith('@media')) {
        walk(body, head.slice('@media'.length).trim());
      } else {
        const vars: Record<string, string> = {};
        for (const m of body.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/gi)) vars[m[1]] = m[2].trim();
        const scheme = /color-scheme\s*:\s*([a-z]+)/.exec(body);
        if (scheme) vars['color-scheme'] = scheme[1];
        rules.push({ selector: head.replace(/\s+/g, ' '), media, vars });
      }
      i = j;
    }
  };
  walk(text, null);
  return rules;
}

/** The colour tokens every theme must define. */
export const COLOUR_TOKENS = [
  '--paper', '--surface', '--surface-2', '--well', '--well-ink',
  '--ink', '--ink-muted', '--ink-faint', '--line', '--line-strong',
  '--act', '--act-ink', '--act-tint',
  '--ok', '--ok-tint', '--part', '--part-tint', '--bad', '--bad-tint', '--wait', '--wait-tint',
] as const;

const themeSel = (id: string) => `[data-theme="${id}"]`;
const SIGNAL = '[data-verdicts="signal"]';

/**
 * The tokens theme id ends up with, with or without the colour-blind safe
 * verdicts, following the layering documented at the top of tokens.css.
 */
export function resolveTheme(rules: TokenRule[], id: string, signal: boolean): Record<string, string> {
  const plain = rules.filter((r) => r.media === null);
  const selectors = (r: TokenRule) => r.selector.split(/,(?![^(]*\))/).map((s) => s.trim());
  const family = plain.find((r) => 'color-scheme' in r.vars && selectors(r).includes(themeSel(id)));
  if (!family) throw new Error(`theme ${id} has no family rule`);
  const own = plain.find((r) => r.selector === themeSel(id));
  const out = { ...family.vars, ...(own?.vars ?? {}) };
  if (!signal) return out;
  const exact = plain.find((r) => r.selector === `${SIGNAL}${themeSel(id)}`);
  const group = plain.find((r) => r.selector.startsWith(`${SIGNAL}:is(`) && r.selector.includes(themeSel(id)));
  const base = plain.find((r) => r.selector === SIGNAL);
  return { ...out, ...(exact ?? group ?? base)?.vars };
}

/** color-mix(in srgb, a p, b): a straight mix of the encoded channels, as #rrggbb. */
export function mixSrgb(a: string, b: string, p: number): string {
  const ch = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const [x, y] = [ch(a), ch(b)];
  return `#${x.map((v, i) => Math.round(v * p + y[i] * (1 - p)).toString(16).padStart(2, '0')).join('')}`;
}

/** A colour as OKLCH: lightness 0 to 1, chroma, hue in degrees. */
export interface Oklch {
  l: number;
  c: number;
  h: number;
}

/** #rrggbb to OKLCH. */
export function toOklch(hex: string): Oklch {
  const [l, a, b] = oklab(hex, 'normal');
  return { l, c: Math.hypot(a, b), h: ((Math.atan2(b, a) * 180) / Math.PI + 360) % 360 };
}

function oklchToLinear({ l: L, c, h }: Oklch): RGB {
  const A = c * Math.cos((h * Math.PI) / 180);
  const B = c * Math.sin((h * Math.PI) / 180);
  const l = (L + 0.3963377774 * A + 0.2158037573 * B) ** 3;
  const m = (L - 0.1055613458 * A - 0.0638541728 * B) ** 3;
  const s = (L - 0.0894841775 * A - 1.291485548 * B) ** 3;
  return [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
}

const hex2 = (v: number) => Math.round(Math.min(255, Math.max(0, v))).toString(16).padStart(2, '0');

/**
 * OKLCH to #rrggbb. Chroma is pulled in until the colour fits sRGB, so
 * lightness and hue are kept.
 */
export function fromOklch({ l, c, h }: Oklch): string {
  const L = Math.min(1, Math.max(0, l));
  let chroma = c;
  let rgb = oklchToLinear({ l: L, c: chroma, h });
  for (let i = 0; i < 40 && rgb.some((v) => v < -0.0005 || v > 1.0005); i++) {
    chroma *= 0.85;
    rgb = oklchToLinear({ l: L, c: chroma, h });
  }
  const enc = (v: number) => {
    const x = Math.min(1, Math.max(0, v));
    return 255 * (x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055);
  };
  return `#${rgb.map((v) => hex2(enc(v))).join('')}`;
}

/**
 * The tokens a theme with only a family ends up with: the family rule, the
 * high-contrast verdicts when `high`, then the colour-blind safe set.
 * Mirrors the [data-family] and [data-contrast] rules in tokens.css.
 */
export function resolveFamily(rules: TokenRule[], family: 'light' | 'dark', high: boolean, signal: boolean): Record<string, string> {
  const plain = rules.filter((r) => r.media === null);
  const selectors = (r: TokenRule) => r.selector.split(/,(?![^(]*\))/).map((s) => s.trim());
  const fam = `[data-family="${family}"]`;
  const base = plain.find((r) => 'color-scheme' in r.vars && (family === 'light' ? selectors(r).includes(':root') : selectors(r).includes(fam)));
  if (!base) throw new Error(`no family rule for ${family}`);
  const get = (sel: string) => plain.find((r) => r.selector === sel)?.vars ?? {};
  const out = { ...base.vars };
  if (high) Object.assign(out, get(`${fam}[data-contrast="high"]`));
  if (signal) {
    Object.assign(out, get('[data-verdicts="signal"]'), get(`[data-verdicts="signal"]${fam}`));
    if (high) Object.assign(out, get(`[data-verdicts="signal"]${fam}[data-contrast="high"]`));
  }
  return out;
}
