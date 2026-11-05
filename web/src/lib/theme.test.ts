import { describe, expect, it } from 'vitest';
import themeInit from '../../public/theme-init.js?raw';
import { EDITOR_COLOURS_KEY, EDITOR_SCHEMES } from './editor-colours';
import {
  CUSTOM_KEY,
  DEFAULT_PREFERENCES,
  GROUPS,
  THEMES,
  THEME_KEY,
  VARS_KEY,
  VERDICTS_KEY,
  applyPreferences,
  choiceLabel,
  fromAccount,
  parseChoice,
  parseCustom,
  parseVerdicts,
  readPreferences,
  resolveInterface,
  savePreferences,
  themesIn,
  toAccount,
  varsBlob,
  type Preferences,
  type ThemeStorage,
} from './theme';
import { ENGINE_TOKENS } from './theme-engine';
import { PRESETS } from './theme-presets';

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
  const props = new Map<string, string>();
  return {
    attrs,
    props,
    setAttribute: (name: string, value: string) => void attrs.set(name, value),
    removeAttribute: (name: string) => void attrs.delete(name),
    style: {
      setProperty: (name: string, value: string) => void props.set(name, value),
      removeProperty: (name: string) => (props.delete(name) ? '' : ''),
    },
  };
}

const CUSTOM = { background: '#1a2b3c', accent: '#e07a5f', contrast: 'high' as const };
const pref = (over: Partial<Preferences>): Preferences => ({ ...DEFAULT_PREFERENCES, ...over });

describe('the theme list', () => {
  it('has unique ids in the shape the server accepts', () => {
    const ids = [...THEMES.map((t) => t.id), ...PRESETS.map((p) => p.id), 'custom'];
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect(id).toMatch(/^[a-z0-9-]{1,32}$/);
  });

  it('puts every theme in a listed group', () => {
    expect(GROUPS.flatMap((g) => themesIn(g)).length).toBe(THEMES.length);
  });

  it('uses sentence-case labels', () => {
    for (const { label } of [...THEMES, ...PRESETS]) {
      // Only the first letter and the OLED acronym are capitals.
      const rest = label.replace(/^OLED/, 'Oled').slice(1);
      expect(label[0]).toBe(label[0].toUpperCase());
      expect(rest).toBe(rest.toLowerCase());
    }
  });

  it('has six presets, three light and three dark', () => {
    expect(PRESETS).toHaveLength(6);
    const families = PRESETS.map((p) => resolveInterface({ theme: p.id, custom: null })?.family);
    expect(families.filter((f) => f === 'light')).toHaveLength(3);
    expect(families.filter((f) => f === 'dark')).toHaveLength(3);
  });
});

describe('parsing', () => {
  it('accepts known theme ids, preset ids and custom, and falls back to system', () => {
    expect(parseChoice('hc-dark')).toBe('hc-dark');
    expect(parseChoice('nordic-slate')).toBe('nordic-slate');
    expect(parseChoice('custom')).toBe('custom');
    for (const value of [null, undefined, '', 'system', 'Dark', 'neon', 1]) {
      expect(parseChoice(value)).toBe('system');
    }
  });

  it('accepts the signal verdict set only by its exact name', () => {
    expect(parseVerdicts('signal')).toBe('signal');
    for (const value of [null, '', 'standard', 'Signal', 2]) expect(parseVerdicts(value)).toBe('standard');
  });

  it('accepts custom inputs only as two #RRGGBB colours and a contrast word', () => {
    expect(parseCustom({ background: '#1A2B3C', accent: '#E07A5F', contrast: 'high' })).toEqual(CUSTOM);
    for (const bad of [null, 'x', {}, { background: '#123', accent: '#E07A5F', contrast: 'high' }, { background: '#1A2B3C', accent: 'red', contrast: 'high' }, { background: '#1A2B3C', accent: '#E07A5F', contrast: 'extreme' }]) {
      expect(parseCustom(bad)).toBeNull();
    }
  });

  it('names choices in sentence case', () => {
    expect(choiceLabel('system')).toBe('System');
    expect(choiceLabel('hc-dark')).toBe('High contrast dark');
    expect(choiceLabel('cosmic-violet')).toBe('Cosmic violet');
    expect(choiceLabel('custom')).toBe('Custom');
    expect(choiceLabel('gone')).toBe('System');
  });
});

describe('account round trip', () => {
  it('stores the system theme as an empty string and sends every field', () => {
    expect(toAccount(DEFAULT_PREFERENCES)).toEqual({ theme: '', verdicts: 'standard', editor_colours: 'night', theme_custom: null });
    expect(toAccount(pref({ theme: 'dark', verdicts: 'signal', editor: 'dusk', custom: CUSTOM }))).toEqual({
      theme: 'dark', verdicts: 'signal', editor_colours: 'dusk', theme_custom: CUSTOM,
    });
  });

  it('reads the account back, ignoring ids this build does not know', () => {
    expect(fromAccount({ theme: '', verdicts: 'standard' })).toEqual(DEFAULT_PREFERENCES);
    expect(fromAccount({ theme: 'dark', verdicts: 'signal' })).toEqual(pref({ theme: 'dark', verdicts: 'signal' }));
    expect(fromAccount({ theme: 'retired' })).toEqual(DEFAULT_PREFERENCES);
    expect(fromAccount({ editor_colours: 'neon' }).editor).toBe('night');
    expect(fromAccount({ theme: 'custom', theme_custom: CUSTOM })).toEqual(pref({ theme: 'custom', custom: CUSTOM }));
  });

  it('falls back to system for "custom" with nothing valid saved', () => {
    expect(fromAccount({ theme: 'custom', theme_custom: null }).theme).toBe('system');
    expect(fromAccount({ theme: 'custom', theme_custom: { background: 'x' } }).theme).toBe('system');
  });
});

describe('the pre-paint cache', () => {
  it('round-trips a built-in choice and the editor scheme', () => {
    const storage = memoryStorage();
    expect(savePreferences(storage, pref({ theme: 'hc-light', verdicts: 'signal', editor: 'paper' }))).toBe(true);
    expect(storage.data).toEqual({ [THEME_KEY]: 'hc-light', [VERDICTS_KEY]: 'signal', [EDITOR_COLOURS_KEY]: 'paper' });
    expect(readPreferences(storage)).toEqual(pref({ theme: 'hc-light', verdicts: 'signal', editor: 'paper' }));
  });

  it('keeps the derived variables for a preset or custom theme', () => {
    const storage = memoryStorage();
    savePreferences(storage, pref({ theme: 'custom', custom: CUSTOM }));
    const blob = JSON.parse(storage.data[VARS_KEY]);
    expect(blob.theme).toBe('custom');
    expect(Object.keys(blob.vars)).toEqual([...ENGINE_TOKENS]);
    expect(JSON.parse(storage.data[CUSTOM_KEY])).toEqual(CUSTOM);
    expect(readPreferences(storage)).toEqual(pref({ theme: 'custom', custom: CUSTOM }));
    savePreferences(storage, pref({ theme: 'nordic-frost' }));
    expect(JSON.parse(storage.data[VARS_KEY]).theme).toBe('nordic-frost');
    savePreferences(storage, pref({ theme: 'dark' }));
    expect(storage.data[VARS_KEY]).toBeUndefined();
  });

  it('clears every key for the defaults', () => {
    const storage = memoryStorage({ [THEME_KEY]: 'dark', [VERDICTS_KEY]: 'signal', [EDITOR_COLOURS_KEY]: 'dusk', [CUSTOM_KEY]: '{}', [VARS_KEY]: '{}' });
    expect(savePreferences(storage, DEFAULT_PREFERENCES)).toBe(true);
    expect(storage.data).toEqual({});
  });

  it('ignores corrupt values', () => {
    expect(readPreferences(memoryStorage({ [THEME_KEY]: 'neon', [VERDICTS_KEY]: 'x', [EDITOR_COLOURS_KEY]: 'y', [CUSTOM_KEY]: '{{' }))).toEqual(DEFAULT_PREFERENCES);
    expect(readPreferences(memoryStorage({ [THEME_KEY]: 'custom' })).theme).toBe('system');
  });

  it('survives missing or blocked storage', () => {
    expect(readPreferences(null)).toEqual(DEFAULT_PREFERENCES);
    expect(readPreferences(throwingStorage)).toEqual(DEFAULT_PREFERENCES);
    expect(savePreferences(null, DEFAULT_PREFERENCES)).toBe(false);
    expect(savePreferences(throwingStorage, DEFAULT_PREFERENCES)).toBe(false);
  });
});

describe('applyPreferences', () => {
  it('sets and clears the attributes for a built-in theme', () => {
    const root = fakeRoot();
    applyPreferences(root, pref({ theme: 'hc-light', verdicts: 'signal', editor: 'midnight' }));
    expect(root.attrs.get('data-theme')).toBe('hc-light');
    expect(root.attrs.get('data-family')).toBe('light');
    expect(root.attrs.get('data-verdicts')).toBe('signal');
    expect(root.attrs.get('data-editor')).toBe('midnight');
    expect(root.attrs.has('data-contrast')).toBe(false);
    expect(root.props.size).toBe(0);
    applyPreferences(root, DEFAULT_PREFERENCES);
    expect(root.attrs.size).toBe(0);
  });

  it('sets the family and the derived variables for a preset, and removes them again', () => {
    const root = fakeRoot();
    applyPreferences(root, pref({ theme: 'nordic-slate' }));
    expect(root.attrs.get('data-theme')).toBe('nordic-slate');
    expect(root.attrs.get('data-family')).toBe('dark');
    expect([...root.props.keys()]).toEqual([...ENGINE_TOKENS]);
    applyPreferences(root, pref({ theme: 'dark' }));
    expect(root.props.size).toBe(0);
    expect(root.attrs.get('data-family')).toBe('dark');
  });

  it('marks a high-contrast custom theme with data-contrast', () => {
    const root = fakeRoot();
    applyPreferences(root, pref({ theme: 'custom', custom: CUSTOM }));
    expect(root.attrs.get('data-theme')).toBe('custom');
    expect(root.attrs.get('data-contrast')).toBe('high');
    applyPreferences(root, pref({ theme: 'custom', custom: { ...CUSTOM, contrast: 'standard' } }));
    expect(root.attrs.has('data-contrast')).toBe(false);
  });

  it('treats custom with no inputs as system', () => {
    const root = fakeRoot();
    applyPreferences(root, pref({ theme: 'custom', custom: null }));
    expect(root.attrs.has('data-theme')).toBe(false);
  });
});

describe('the blob the pre-paint script reads', () => {
  it('exists only for a preset or custom theme', () => {
    expect(varsBlob({ theme: 'system', custom: null })).toBeNull();
    expect(varsBlob({ theme: 'dark', custom: null })).toBeNull();
    expect(varsBlob({ theme: 'overcast', custom: null })).not.toBeNull();
  });
});

describe('pre-paint script', () => {
  it('reads the same storage keys as the app', () => {
    for (const key of [THEME_KEY, VERDICTS_KEY, VARS_KEY, EDITOR_COLOURS_KEY]) expect(themeInit).toContain(`'${key}'`);
  });

  it('knows exactly the built-in themes theme.ts lists', () => {
    const list = /const themes = \[([^\]]*)\]/.exec(themeInit)?.[1] ?? '';
    const ids = [...list.matchAll(/'([^']+)'/g)].map((m) => m[1]);
    expect(ids).toEqual(THEMES.map((t) => t.id));
  });

  it('knows exactly the editor schemes except Night, and the engine variables', () => {
    const list = /const editors = \[([^\]]*)\]/.exec(themeInit)?.[1] ?? '';
    expect([...list.matchAll(/'([^']+)'/g)].map((m) => m[1])).toEqual(EDITOR_SCHEMES.filter((s) => s.id !== 'night').map((s) => s.id));
    const names = [...themeInit.matchAll(/'(--[a-z0-9-]+)':/g)].map((m) => m[1]);
    expect(names).toEqual([...ENGINE_TOKENS]);
  });
});
