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
 */

type RGB = [number, number, number];

function channels(hex: string): RGB {
  const m = /^#([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) throw new Error(`not a #rrggbb colour: ${hex}`);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((c) => c / 255) as RGB;
}

const linear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);

function luminance(hex: string): number {
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
