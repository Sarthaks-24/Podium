import { describe, expect, it } from 'vitest';
import themeInit from '../../public/theme-init.js?raw';
import {
  STORAGE_KEY,
  applyChoice,
  choiceLabel,
  nextChoice,
  parseChoice,
  readChoice,
  saveChoice,
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

describe('parseChoice', () => {
  it('accepts light and dark', () => {
    expect(parseChoice('light')).toBe('light');
    expect(parseChoice('dark')).toBe('dark');
  });

  it('falls back to system for anything else', () => {
    for (const value of [null, undefined, '', 'system', 'Dark', 'blue', 1]) {
      expect(parseChoice(value)).toBe('system');
    }
  });
});

describe('nextChoice', () => {
  it('cycles system, light, dark, system', () => {
    expect(nextChoice('system')).toBe('light');
    expect(nextChoice('light')).toBe('dark');
    expect(nextChoice('dark')).toBe('system');
  });
});

describe('choiceLabel', () => {
  it('uses sentence case', () => {
    expect(choiceLabel('system')).toBe('System');
    expect(choiceLabel('light')).toBe('Light');
    expect(choiceLabel('dark')).toBe('Dark');
  });
});

describe('readChoice and saveChoice', () => {
  it('round-trips a forced theme', () => {
    const storage = memoryStorage();
    expect(saveChoice(storage, 'dark')).toBe(true);
    expect(storage.data[STORAGE_KEY]).toBe('dark');
    expect(readChoice(storage)).toBe('dark');
  });

  it('clears the key for system', () => {
    const storage = memoryStorage({ [STORAGE_KEY]: 'light' });
    expect(saveChoice(storage, 'system')).toBe(true);
    expect(STORAGE_KEY in storage.data).toBe(false);
    expect(readChoice(storage)).toBe('system');
  });

  it('ignores a corrupt stored value', () => {
    expect(readChoice(memoryStorage({ [STORAGE_KEY]: 'neon' }))).toBe('system');
  });

  it('survives missing or blocked storage', () => {
    expect(readChoice(null)).toBe('system');
    expect(readChoice(throwingStorage)).toBe('system');
    expect(saveChoice(null, 'dark')).toBe(false);
    expect(saveChoice(throwingStorage, 'dark')).toBe(false);
  });
});

describe('applyChoice', () => {
  it('sets data-theme for light and dark and removes it for system', () => {
    const root = fakeRoot();
    applyChoice(root, 'dark');
    expect(root.attrs.get('data-theme')).toBe('dark');
    applyChoice(root, 'light');
    expect(root.attrs.get('data-theme')).toBe('light');
    applyChoice(root, 'system');
    expect(root.attrs.has('data-theme')).toBe(false);
  });
});

describe('pre-paint script', () => {
  it('reads the same storage key as theme.ts', () => {
    expect(themeInit).toContain(`'${STORAGE_KEY}'`);
  });
});
