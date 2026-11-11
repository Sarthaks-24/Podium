import { describe, expect, it } from 'vitest';
import { privacyParts } from './privacy';

describe('privacyParts', () => {
  it('splits at the retention marker and trims the seams', () => {
    expect(privacyParts('one\n\n<!-- retention -->\n\ntwo')).toEqual({ before: 'one', after: 'two' });
  });
  it('returns everything as before when there is no marker', () => {
    expect(privacyParts('only text\n')).toEqual({ before: 'only text\n', after: '' });
  });
  it('splits at the first marker only', () => {
    const { before, after } = privacyParts('a<!-- retention -->b<!-- retention -->c');
    expect(before).toBe('a');
    expect(after).toBe('b<!-- retention -->c');
  });
  it('splits the shipped notice with text on both sides of the table', () => {
    const { before, after } = privacyParts();
    expect(before.length).toBeGreaterThan(100);
    expect(after.length).toBeGreaterThan(0);
    expect(before).not.toContain('<!-- retention -->');
    expect(after).not.toContain('<!-- retention -->');
  });
});
