import { describe, expect, it } from 'vitest';
import themeInit from '../../public/theme-init.js?raw';
import {
  DEFAULT_PREFERENCES,
  GROUPS,
  THEMES,
  THEME_KEY,
  VERDICTS_KEY,
  applyPreferences,
  choiceLabel,
  fromAccount,
  parseChoice,
  parseVerdicts,
  readPreferences,
  savePreferences,
  themesIn,
  toAccount,
  type ThemeStorage,
} from './theme';

function memoryStorage(initial: Record<string, string> = {}): ThemeStorage & { data: Record<string, string> } {
  const data = { ...initial };
  return {
    data,
    getItem: (key) => (key in data ? data[key] : null),
    setItem: (key, value) => {
      data[key] = value;
    },
    removeItem: (key) => {
      delete data[key];
    },
  };
}

const throwingStorage: ThemeStorage = {
  getItem: () => {
    throw new Error('blocked');
  },
  setItem: () => {
    throw new Error('blocked');
  },
  removeItem: () => {
    throw new Error('blocked');
  },
};

function fakeRoot() {
  const attrs = new Map<string, string>();
  return {
    attrs,
    setAttribute: (name: string, value: string) => void attrs.set(name, value),
    removeAttribute: (name: string) => void attrs.delete(name),
  };
}

describe('the theme list', () => {
  it('has unique ids in the shape the server accepts', () => {
    const ids = THEMES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect(id).toMatch(/^[a-z0-9-]{1,32}$/);
  });

  it('puts every theme in a listed group', () => {
    expect(GROUPS.flatMap((g) => themesIn(g)).length).toBe(THEMES.length);
  });

  it('uses sentence-case labels', () => {
    for (const { label } of THEMES) {
      // Only the first letter and the OLED acronym are capitals.
      const rest = label.replace(/^OLED/, 'Oled').slice(1);
      expect(label[0]).toBe(label[0].toUpperCase());
      expect(rest).toBe(rest.toLowerCase());
    }
  });
});

describe('parsing', () => {
  it('accepts known theme ids and falls back to system', () => {
    expect(parseChoice('nordic-frost')).toBe('nordic-frost');
    for (const value of [null, undefined, '', 'system', 'Dark', 'neon', 1]) {
      expect(parseChoice(value)).toBe('system');
    }
  });

  it('accepts the signal verdict set only by its exact name', () => {
    expect(parseVerdicts('signal')).toBe('signal');
    for (const value of [null, '', 'standard', 'Signal', 2]) expect(parseVerdicts(value)).toBe('standard');
  });

  it('names choices in sentence case', () => {
    expect(choiceLabel('system')).toBe('System');
    expect(choiceLabel('hc-dark')).toBe('High contrast dark');
    expect(choiceLabel('gone')).toBe('System');
  });
});

describe('account round trip', () => {
  it('stores the system theme as an empty string', () => {
    expect(toAccount({ theme: 'system', verdicts: 'standard' })).toEqual({ theme: '', verdicts: 'standard' });
    expect(toAccount({ theme: 'overcast', verdicts: 'signal' })).toEqual({ theme: 'overcast', verdicts: 'signal' });
  });

  it('reads the account back, ignoring ids this build does not know', () => {
    expect(fromAccount('', 'standard')).toEqual(DEFAULT_PREFERENCES);
    expect(fromAccount('overcast', 'signal')).toEqual({ theme: 'overcast', verdicts: 'signal' });
    expect(fromAccount('retired', undefined)).toEqual(DEFAULT_PREFERENCES);
  });
});

describe('the pre-paint cache', () => {
  it('round-trips forced choices', () => {
    const storage = memoryStorage();
    expect(savePreferences(storage, { theme: 'hc-light', verdicts: 'signal' })).toBe(true);
    expect(storage.data).toEqual({ [THEME_KEY]: 'hc-light', [VERDICTS_KEY]: 'signal' });
    expect(readPreferences(storage)).toEqual({ theme: 'hc-light', verdicts: 'signal' });
  });

  it('clears both keys for the defaults', () => {
    const storage = memoryStorage({ [THEME_KEY]: 'dark', [VERDICTS_KEY]: 'signal' });
    expect(savePreferences(storage, DEFAULT_PREFERENCES)).toBe(true);
    expect(storage.data).toEqual({});
  });

  it('ignores corrupt values', () => {
    expect(readPreferences(memoryStorage({ [THEME_KEY]: 'neon', [VERDICTS_KEY]: 'x' }))).toEqual(DEFAULT_PREFERENCES);
  });

  it('survives missing or blocked storage', () => {
    expect(readPreferences(null)).toEqual(DEFAULT_PREFERENCES);
    expect(readPreferences(throwingStorage)).toEqual(DEFAULT_PREFERENCES);
    expect(savePreferences(null, DEFAULT_PREFERENCES)).toBe(false);
    expect(savePreferences(throwingStorage, DEFAULT_PREFERENCES)).toBe(false);
  });
});

describe('applyPreferences', () => {
  it('sets and clears data-theme and data-verdicts', () => {
    const root = fakeRoot();
    applyPreferences(root, { theme: 'cosmic-violet', verdicts: 'signal' });
    expect(root.attrs.get('data-theme')).toBe('cosmic-violet');
    expect(root.attrs.get('data-verdicts')).toBe('signal');
    applyPreferences(root, DEFAULT_PREFERENCES);
    expect(root.attrs.size).toBe(0);
  });
});

describe('pre-paint script', () => {
  it('reads the same storage keys as theme.ts', () => {
    expect(themeInit).toContain(`'${THEME_KEY}'`);
    expect(themeInit).toContain(`'${VERDICTS_KEY}'`);
  });

  it('knows exactly the themes theme.ts lists', () => {
    const list = /const themes = \[([^\]]*)\]/.exec(themeInit)?.[1] ?? '';
    const ids = [...list.matchAll(/'([^']+)'/g)].map((m) => m[1]);
    expect(ids).toEqual(THEMES.map((t) => t.id));
  });
});
