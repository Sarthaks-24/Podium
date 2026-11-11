import { describe, expect, it } from 'vitest';
import { d41Failures, MIN_DISTANCE, over } from './theme-rules';

const GOOD: Record<string, string> = {
  '--paper': '#ffffff', '--surface': '#fafafa', '--surface-2': '#f0f0f0', '--well': '#101010', '--well-ink': '#f0f0f0',
  '--ink': '#111111', '--ink-muted': '#404040', '--ink-faint': '#6b6b6b', '--line': '#d0d0d0', '--line-strong': '#707070',
  '--act': '#1a3d99', '--act-ink': '#ffffff', '--act-tint': '#e6ecfa',
  '--ok': '#0b5e2a', '--ok-tint': '#e3f3e8', '--part': '#7a4a00', '--part-tint': '#f7ecd9',
  '--bad': '#a01818', '--bad-tint': '#f8e3e3', '--wait': '#3a3a3a', '--wait-tint': '#ececec',
  '--scrim': 'rgba(0, 0, 0, 0.5)',
};

describe('over', () => {
  it('composites rgba over a solid colour', () => {
    expect(over('rgba(0, 0, 0, 0.5)', '#ffffff')).toBe('#808080');
    expect(over('rgba(255, 0, 0, 1)', '#ffffff')).toBe('#ff0000');
  });
  it('rejects a non-rgba string', () => {
    expect(() => over('#000000', '#ffffff')).toThrow('not rgba');
  });
});

describe('d41Failures', () => {
  it('passes a theme that keeps every rule', () => {
    expect(d41Failures(GOOD, { high: false, signal: false })).toEqual([]);
  });
  it('names a text colour that is too faint', () => {
    const out = d41Failures({ ...GOOD, '--ink-muted': '#b0b0b0' }, { high: false, signal: false });
    expect(out.some((l) => l.startsWith('--ink-muted on --paper'))).toBe(true);
  });
  it('asks for 7:1 at the high level', () => {
    expect(d41Failures(GOOD, { high: true, signal: false }).some((l) => l.endsWith('< 7'))).toBe(true);
  });
  it('names two verdict colours that sit too close', () => {
    const out = d41Failures({ ...GOOD, '--part': GOOD['--bad'] }, { high: false, signal: false });
    expect(out.some((l) => l.includes('--part and --bad'))).toBe(true);
    expect(MIN_DISTANCE).toBe(0.09);
  });
  it('checks vision simulation only when the signal set is in use', () => {
    const t = { ...GOOD, '--ok': '#7a4a00', '--part': '#006a2a' };
    const plain = d41Failures(t, { high: false, signal: false });
    const signal = d41Failures(t, { high: false, signal: true });
    expect(signal.filter((l) => /\((protan|deutan)\)/.test(l)).length).toBeGreaterThan(0);
    expect(plain.some((l) => /\((protan|deutan)\)/.test(l))).toBe(false);
  });
  it('flags a scrim that does not dim the page', () => {
    const out = d41Failures({ ...GOOD, '--paper': '#202020', '--scrim': 'rgba(255, 255, 255, 0.5)' }, { high: false, signal: false });
    expect(out).toContain('--scrim does not dim --paper');
  });
});
