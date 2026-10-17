// Enforces two repository rules on the web sources (CLAUDE.md sections 7 and 8):
// colour literals live only in styles/tokens.css, and no file contains an
// en dash or em dash.
import { describe, expect, it } from 'vitest';

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
