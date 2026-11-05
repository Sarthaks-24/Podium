// public/theme-init.js runs before the app, from storage a person (or a
// script on the same origin) could have edited. This runs the real file
// against fake documents and hostile storage: only an allowlisted variable
// name with a colour-shaped value is ever applied, and anything else leaves
// the page on the system theme. The file is a static script so the CSP can
// stay at script-src 'self'.
import { describe, expect, it } from 'vitest';
import themeInit from '../../public/theme-init.js?raw';
import { CUSTOM_KEY, THEME_KEY, VARS_KEY, VERDICTS_KEY, savePreferences, DEFAULT_PREFERENCES, type ThemeStorage } from './theme';
import { EDITOR_COLOURS_KEY } from './editor-colours';
import { ENGINE_TOKENS } from './theme-engine';

/** Runs the script with a fake document and storage; returns what it set on <html>. */
function run(stored: Record<string, string>, throws = false) {
  const attrs = new Map<string, string>();
  const props = new Map<string, string>();
  const root = {
    setAttribute: (k: string, v: string) => void attrs.set(k, v),
    style: { setProperty: (k: string, v: string) => void props.set(k, v) },
  };
  const storage = {
    getItem: (k: string) => {
      if (throws) throw new Error('blocked');
      return k in stored ? stored[k] : null;
    },
  };
  new Function('document', 'localStorage', themeInit)({ documentElement: root }, storage);
  return { attrs, props };
}

const goodVars = () => ({
  '--paper': '#101418', '--surface': '#161b20', '--surface-2': '#1c2228', '--ink': '#eef1f4', '--ink-muted': '#aab2ba',
  '--ink-faint': '#7d858d', '--line': '#252b31', '--line-strong': '#6d757d', '--act': '#8fb4ff', '--act-ink': '#0E1012',
  '--act-tint': '#1d2a44', '--scrim': 'rgba(0, 0, 0, 0.6)', '--shadow-menu': '0 8px 24px rgba(0, 0, 0, 0.5)',
});
const blob = (over: Record<string, unknown> = {}, vars: Record<string, unknown> = goodVars()) =>
  JSON.stringify({ theme: 'custom', family: 'dark', contrast: 'standard', vars, ...over });

describe('theme-init.js', () => {
  it('applies a built-in theme, the verdict set and the editor scheme', () => {
    const { attrs, props } = run({ [THEME_KEY]: 'hc-dark', [VERDICTS_KEY]: 'signal', [EDITOR_COLOURS_KEY]: 'dusk' });
    expect(Object.fromEntries(attrs)).toEqual({ 'data-theme': 'hc-dark', 'data-verdicts': 'signal', 'data-editor': 'dusk' });
    expect(props.size).toBe(0);
  });

  it('applies a custom theme from its stored variables', () => {
    const { attrs, props } = run({ [THEME_KEY]: 'custom', [VARS_KEY]: blob({ contrast: 'high' }) });
    expect(Object.fromEntries(attrs)).toEqual({ 'data-theme': 'custom', 'data-family': 'dark', 'data-contrast': 'high' });
    expect([...props.keys()]).toEqual([...ENGINE_TOKENS]);
  });

  it('applies exactly what the app wrote for a preset', () => {
    const store: Record<string, string> = {};
    const storage: ThemeStorage = { getItem: (k) => store[k] ?? null, setItem: (k, v) => void (store[k] = v), removeItem: (k) => void delete store[k] };
    savePreferences(storage, { ...DEFAULT_PREFERENCES, theme: 'soft-lavender' });
    expect(store[CUSTOM_KEY]).toBeUndefined();
    const { attrs, props } = run(store);
    expect(attrs.get('data-theme')).toBe('soft-lavender');
    expect(attrs.get('data-family')).toBe('light');
    expect(props.size).toBe(ENGINE_TOKENS.length);
  });

  it('ignores an unknown editor scheme and unknown built-in ids', () => {
    expect(run({ [EDITOR_COLOURS_KEY]: 'night' }).attrs.size).toBe(0);
    expect(run({ [EDITOR_COLOURS_KEY]: 'neon' }).attrs.size).toBe(0);
    expect(run({ [THEME_KEY]: 'neon' }).attrs.size).toBe(0);
  });

  describe('falls back to the system theme and sets nothing for', () => {
    const cases: [string, Record<string, string>][] = [
      ['a custom id with no variables', { [THEME_KEY]: 'custom' }],
      ['variables that are not JSON', { [THEME_KEY]: 'custom', [VARS_KEY]: '{{' }],
      ['variables for another theme', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({ theme: 'other' }) }],
      ['an unknown family', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({ family: 'neon' }) }],
      ['an unknown contrast word', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({ contrast: 'extreme' }) }],
      ['a variable name off the allowlist', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--evil': '#000000' }) }],
      ['a missing variable', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, (() => { const v: Record<string, string> = goodVars(); delete v['--act']; return v; })()) }],
      ['a swapped name', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, (() => { const v: Record<string, string> = goodVars(); delete v['--act']; return { ...v, '--other': '#ffffff' }; })()) }],
      ['a url() value', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--paper': 'url(https://evil.example/x)' }) }],
      ['an expression value', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--ink': 'red; background: url(x)' }) }],
      ['a short hex', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--act': '#fff' }) }],
      ['an eight-digit hex', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--act': '#ffffffff' }) }],
      ['a colour name', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--act': 'red' }) }],
      ['a scrim that is not rgba', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--scrim': '#000000' }) }],
      ['a shadow with extra declarations', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--shadow-menu': '0 8px 24px rgba(0, 0, 0, 0.5); x: y' }) }],
      ['a non-string value', { [THEME_KEY]: 'custom', [VARS_KEY]: blob({}, { ...goodVars(), '--act': 5 }) }],
      ['a null blob', { [THEME_KEY]: 'custom', [VARS_KEY]: 'null' }],
      ['an id with a bad shape', { [THEME_KEY]: 'Cus tom', [VARS_KEY]: blob({ theme: 'Cus tom' }) }],
    ];
    for (const [name, stored] of cases) {
      it(name, () => {
        const { attrs, props } = run(stored);
        expect(attrs.has('data-theme'), 'data-theme').toBe(false);
        expect(attrs.has('data-family'), 'data-family').toBe(false);
        expect(props.size).toBe(0);
      });
    }
  });

  it('keeps going to the verdict set when the theme variables are refused', () => {
    const { attrs } = run({ [THEME_KEY]: 'custom', [VARS_KEY]: '{{', [VERDICTS_KEY]: 'signal' });
    expect(Object.fromEntries(attrs)).toEqual({ 'data-verdicts': 'signal' });
  });

  it('does nothing when storage throws', () => {
    const { attrs, props } = run({}, true);
    expect(attrs.size + props.size).toBe(0);
  });
});
