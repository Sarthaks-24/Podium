// Enforces repository rules on the web sources (CLAUDE.md sections 7 and 8):
// colour literals live only in styles/tokens.css, no file contains an en
// dash or em dash, and every media query uses the breakpoint tiers of
// lib/breakpoints.ts (v2 F8).
import { describe, expect, it } from 'vitest';
import { QUERIES, TIERS } from './lib/breakpoints';

const sources = import.meta.glob<string>(['/src/**/*.{ts,svelte,css,html}', '/index.html', '/public/**/*.{js,svg,webmanifest}'], {
  query: '?raw',
  import: 'default',
  eager: true,
});

const HEX_COLOUR = /#[0-9a-fA-F]{3,8}\b/;
const TOKENS = '/src/styles/tokens.css';
// Browser chrome metas and the favicon cannot use CSS variables, so they
// carry copies of token values; the test below keeps the copies exact.
const MIRRORS_TOKENS = ['/index.html', '/public/favicon.svg'];
// Tests may name colours to check the contrast maths.
const isTest = (path: string) => path.endsWith('.test.ts');

function hexes(text: string): string[] {
  return (text.match(new RegExp(HEX_COLOUR.source, 'g')) ?? []).map((h) => h.toUpperCase());
}
// Built from code points so this file stays free of the characters it bans.
const DASHES = new RegExp(`[${String.fromCharCode(0x2013, 0x2014)}]`);

describe('web source guardrails', () => {
  it('finds the source files', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(5);
  });

  // C2-071 (C1-035): a text or page measure is a --measure-* token. Any ch
  // width, or a rem width of 40 or more, is a measure; smaller rem widths
  // size a single control and may stay literal.
  it('keeps page measures in tokens', () => {
    const offenders: string[] = [];
    for (const [path, text] of Object.entries(sources)) {
      if (path === TOKENS || isTest(path)) continue;
      for (const m of text.matchAll(/(?<![-\w])max-width:\s*([\d.]+)(rem|ch)\b/g)) {
        if (m[2] === 'ch' || Number(m[1]) >= 40) offenders.push(`${path} ${m[0]}`);
      }
    }
    expect(offenders).toEqual([]);
  });

  it('keeps hex colours inside tokens.css', () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => path !== TOKENS && !MIRRORS_TOKENS.includes(path) && !isTest(path))
      .filter(([, text]) => HEX_COLOUR.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  it('only copies token values into files that cannot read CSS variables', () => {
    const tokenHexes = new Set(hexes(sources[TOKENS]));
    for (const path of MIRRORS_TOKENS) {
      const stray = hexes(sources[path]).filter((h) => !tokenHexes.has(h));
      expect(stray, path).toEqual([]);
    }
  });

  // --focus is a box-shadow list; inside an outline it makes the whole
  // declaration invalid, so the ring silently disappears (C1-009).
  it('never uses the --focus shadow as an outline colour', () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => !isTest(path))
      .filter(([, text]) => /outline(-color)?\s*:[^;]*var\(--focus\)/.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  // Page-level stacking uses the --z-* tokens, so a toast never ties with
  // the bar; small local orders (1 to 9) inside a component are fine (C1-034).
  it('takes page-level z-index values from the tokens', () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => path !== TOKENS && !isTest(path))
      .filter(([, text]) => /z-index\s*:\s*\d{2,}/.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  // The rating number is read on organiser screens only (docs/API.md
  // "Students"): no screen a student sees imports the module that holds it.

  // One error fallback, from lib/api.ts errorText (C1-032): no screen
  // carries its own copy of the sentence.
  it('words an unexpected error only in lib/api.ts', () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => path !== '/src/lib/api.ts' && !isTest(path))
      .filter(([, text]) => text.includes('Something went wrong. Try again in a moment.') || /function messageOf\(/.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  // Widths and rules come from the tokens (CLAUDE.md section 8, C1-035):
  // a component names --measure-* instead of its value, draws outlines,
  // borders and underlines in --rule multiples, and uses the shared .small.
  it('names the measure and rule tokens in components instead of their values', () => {
    const components = Object.entries(sources).filter(([path]) => path.endsWith('.svelte'));
    const measure = components.filter(([, text]) => /max-width:\s*(40rem|48rem|64rem|72ch|26rem)\s*;/.test(text)).map(([p]) => p);
    const px = components
      .filter(([, text]) => /(outline(-offset)?|border(-[a-z-]+)?|text-decoration-thickness|text-underline-offset)\s*:[^;]*\d+px/.test(text))
      .map(([p]) => p);
    const small = components.filter(([, text]) => /\n {2}\.small \{\n {4}font-size: var\(--fs-0\);\n {2}\}/.test(text)).map(([p]) => p);
    expect({ measure, px, small }).toEqual({ measure: [], px: [], small: [] });
  });

  it('contains no en dashes or em dashes', () => {
    const offenders = Object.entries(sources)
      .filter(([, text]) => DASHES.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});

// Widths a media query may use: the tiers, as min-width or as the exact
// complement "width < tier", and the range "tier <= width < tier".
const TIER_PX = new Set(Object.values(TIERS).map(String));
// Queries outside the tiers that a later plan item removes, by file.
const KNOWN_WIDTHS: Record<string, string[]> = {};

/** Every width condition in a media query prelude, as written. */
function widthConditions(prelude: string): string[] {
  return prelude.match(/\([^()]*width[^()]*\)/g) ?? [];
}

function widthAllowed(cond: string): boolean {
  const min = /^\(min-width: (\d+)px\)$/.exec(cond);
  if (min) return TIER_PX.has(min[1]);
  const below = /^\(width < (\d+)px\)$/.exec(cond);
  if (below) return TIER_PX.has(below[1]);
  const range = /^\((\d+)px <= width < (\d+)px\)$/.exec(cond);
  if (range) return TIER_PX.has(range[1]) && TIER_PX.has(range[2]);
  return false;
}

describe('breakpoints', () => {
  it('keeps every media query on the tiers', () => {
    const offenders: string[] = [];
    let seen = 0;
    for (const [path, text] of Object.entries(sources)) {
      if (isTest(path) || path === '/src/lib/breakpoints.ts') continue;
      for (const m of text.matchAll(/@media([^{]+)\{/g)) {
        for (const cond of widthConditions(m[1])) {
          seen++;
          if (!widthAllowed(cond) && !(KNOWN_WIDTHS[path] ?? []).includes(cond)) offenders.push(`${path}: ${cond}`);
        }
      }
    }
    expect(seen, 'width conditions found').toBeGreaterThan(20);
    expect(offenders).toEqual([]);
  });

  it('calls matchMedia with a width only through lib/breakpoints.ts', () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => !isTest(path) && path !== '/src/lib/breakpoints.ts')
      .flatMap(([path, text]) => [...text.matchAll(/matchMedia\(\s*['"`]([^'"`]*)['"`]/g)].filter((m) => /width|height/.test(m[1])).map((m) => `${path}: ${m[1]}`));
    expect(offenders).toEqual([]);
  });

  it('builds the matchMedia queries from the tiers', () => {
    expect(QUERIES).toEqual({
      tablet: '(min-width: 768px)',
      laptop: '(min-width: 1024px)',
      desk: '(min-width: 1280px)',
      wide: '(min-width: 1440px)',
      short: '(min-width: 1024px) and (max-height: 45rem)',
    });
  });

  it('catches a query off the tiers', () => {
    expect(widthAllowed('(max-width: 767px)')).toBe(false);
    expect(widthAllowed('(min-width: 1200px)')).toBe(false);
    expect(widthAllowed('(min-width: 1280px)')).toBe(true);
    expect(widthAllowed('(width < 768px)')).toBe(true);
    expect(widthAllowed('(768px <= width < 1024px)')).toBe(true);
  });
});

// F8.1 and F8.2: what a student's first load carries.
describe('first load', () => {
  // The screens a student can open; every other route is an organiser's and
  // is loaded with import() from App.svelte.
  const STUDENT_SCREENS = ['ContestPage', 'Home', 'NotFound', 'Privacy', 'SignIn', 'StandingsPage'];

  it('imports only student screens statically in App.svelte', () => {
    const app = sources['/src/App.svelte'];
    const statics = [...app.matchAll(/^\s*import (\w+) from '\.\/routes\/(\w+)\.svelte';/gm)].map((m) => m[2]);
    expect(statics.sort()).toEqual([...STUDENT_SCREENS].sort());
    const routeFiles = Object.keys(sources).filter((p) => /^\/src\/routes\/\w+\.svelte$/.test(p)).map((p) => p.slice('/src/routes/'.length, -'.svelte'.length));
    const lazy = routeFiles.filter((r) => !STUDENT_SCREENS.includes(r));
    expect(lazy.filter((r) => !app.includes(`import('./routes/${r}.svelte')`))).toEqual([]);
  });

  it('preloads every font face the stylesheet declares (D91)', () => {
    const faces = [...sources['/src/styles/base.css'].matchAll(/url\("(\/fonts\/[\w-]+\.woff2)"\)/g)].map((m) => m[1]);
    const preloads = [...sources['/index.html'].matchAll(/<link rel="preload" href="([^"]+)" as="font"/g)].map((m) => m[1]);
    expect(faces.length).toBe(4);
    expect(preloads.sort()).toEqual(faces.sort());
  });
});

// C2-042 (C1-019): a toast's words are also in the visually hidden polite
// announcer, so `getByText(...).first()` finds them even when no toast is
// drawn. Playwright specs check toasts inside `.toasts`.
describe('toast checks in the Playwright specs', () => {
  const specs = import.meta.glob<string>('/tests/**/*.spec.ts', { query: '?raw', import: 'default', eager: true });

  /**
   * The fixed words that start each toast: every literal on a line that
   * calls showToast, or a template's text before its first ${. Toasts
   * passed in a variable are not seen; the cited specs were fixed by hand.
   */
  function toastStarts(): string[] {
    const out: string[] = [];
    for (const text of Object.values(sources)) {
      for (const line of text.split('\n').filter((l) => l.includes('showToast('))) {
        for (const m of line.matchAll(/'([^']*)'|"([^"]*)"|`([^`$]*)/g)) {
          const words = m[1] ?? m[2] ?? m[3] ?? '';
          if (words.trim().length >= 8) out.push(words);
        }
      }
    }
    return out;
  }

  it('finds the specs and the toasts', () => {
    expect(Object.keys(specs).length).toBeGreaterThan(5);
    expect(toastStarts().length).toBeGreaterThan(10);
  });

  it('never checks a toast with getByText(...).first() outside .toasts', () => {
    const starts = toastStarts();
    const offenders: string[] = [];
    for (const [path, text] of Object.entries(specs)) {
      text.split('\n').forEach((line, i) => {
        for (const m of line.matchAll(/getByText\('([^']+)'[^)]*\)\.first\(\)/g)) {
          if (line.includes(".toasts')")) continue;
          if (starts.some((s) => m[1].startsWith(s) || s.startsWith(m[1]))) offenders.push(`${path}:${i + 1} ${m[1]}`);
        }
      });
    }
    expect(offenders).toEqual([]);
  });
});
