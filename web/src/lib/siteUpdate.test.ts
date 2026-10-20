import { describe, expect, it } from 'vitest';
import { isNewUpdate, markUpdateSeen, seenUpdate, updateLine } from './siteUpdate';

function memory() {
  const data = new Map<string, string>();
  return { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) };
}

describe('the site update line (D112)', () => {
  it('shows once per update', () => {
    const store = memory();
    const at = new Date(2026, 9, 21, 14, 2).getTime();
    expect(isNewUpdate(null, seenUpdate(store))).toBe(false);
    expect(isNewUpdate(at, seenUpdate(store))).toBe(true);
    markUpdateSeen(at, store);
    expect(isNewUpdate(at, seenUpdate(store))).toBe(false);
    expect(isNewUpdate(at + 60_000, seenUpdate(store))).toBe(true);
    expect(updateLine(at)).toBe('The site was updated at 14:02.');
  });

  it('survives a storage that throws', () => {
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(seenUpdate(broken)).toBe(0);
    expect(() => markUpdateSeen(5, broken)).not.toThrow();
  });
});
