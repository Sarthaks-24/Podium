import { describe, expect, it } from 'vitest';
import { jitter } from './jitter';

describe('jitter', () => {
  it('returns a deterministic delay within the inclusive range', () => {
    expect(jitter(3000, () => 0)).toBe(0);
    expect(jitter(3000, () => 0.5)).toBe(1500);
    expect(jitter(3000, () => 1)).toBe(3000);
  });
});
