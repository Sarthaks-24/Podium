import { describe, expect, it } from 'vitest';
import { starter, withLanguage } from './starters';

describe('a program moved to another language', () => {
  it('swaps an untouched starter or an empty source for the new starter', () => {
    expect(withLanguage({ language: 'cpp17', source: starter('cpp17') }, 'python3')).toEqual({ language: 'python3', source: starter('python3') });
    // Spaces around an untouched starter do not matter.
    expect(withLanguage({ language: 'cpp17', source: `\n${starter('cpp17')}  ` }, 'java21').source).toBe(starter('java21'));
    expect(withLanguage({ language: 'python3', source: '' }, 'node22').source).toBe(starter('node22'));
  });

  it('keeps code the setter wrote', () => {
    const mine = { language: 'cpp17' as const, source: 'int main() { return 0; }\n' };
    expect(withLanguage(mine, 'python3')).toEqual({ language: 'python3', source: mine.source });
  });
});
