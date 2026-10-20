// Enforces repository rules on the web sources (CLAUDE.md sections 7 and 8):
// colour literals live only in styles/tokens.css, no file contains an en
// dash or em dash, and every media query uses the breakpoint tiers of
// lib/breakpoints.ts (v2 F8).
import { describe, expect, it } from 'vitest';
import { QUERIES, TIERS } from './lib/breakpoints';

const sources = import.meta.glob<string>(['/src/**/*.{ts,svelte,css,html}', '/index.html', '/public/**/*.{js,svg}'], {
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
const KNOWN_WIDTHS: Record<string, string[]> = {
  // The verdict panel's wider split; P5 (F-V7) moves the workspace to the shell's tiers and the short tier.
  '/src/components/Workspace.svelte': ['(min-width: 1280px)'],
};

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
      wide: '(min-width: 1440px)',
      short: '(min-width: 1024px) and (max-height: 45rem)',
    });
  });

  it('catches a query off the tiers', () => {
    expect(widthAllowed('(max-width: 767px)')).toBe(false);
    expect(widthAllowed('(min-width: 1280px)')).toBe(false);
    expect(widthAllowed('(width < 768px)')).toBe(true);
    expect(widthAllowed('(768px <= width < 1024px)')).toBe(true);
  });
});
