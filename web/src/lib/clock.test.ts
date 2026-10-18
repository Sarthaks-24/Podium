import { describe, expect, it } from 'vitest';
import { STALE_MS, ServerClock, clockOffset } from './clock';

describe('clock offset', () => {
  it('measures at the midpoint of the round trip', () => {
    // Sent at 1000, answered at 1200: the server read its clock around 1100.
    expect(clockOffset(6100, 1000, 1200)).toBe(5000);
    expect(clockOffset(900, 1000, 1000)).toBe(-100);
  });

  it('keeps the sample with the shorter round trip until it goes stale', () => {
    let now = 0;
    const clock = new ServerClock(() => now);
    expect(clock.observe(10_100, 0, 200)).toBe(true);
    expect(clock.offset).toBe(10_000);

    // A slower sample does not replace a fresh, faster one.
    expect(clock.observe(20_000, 1000, 3000)).toBe(false);
    expect(clock.offset).toBe(10_000);

    // A faster one does.
    expect(clock.observe(12_050, 2000, 2100)).toBe(true);
    expect(clock.offset).toBe(10_000);

    // Once stale, any sample is taken.
    expect(clock.observe(2100 + STALE_MS + 1000 + 500 + 9000, 2100 + STALE_MS + 1000, 2100 + STALE_MS + 2000)).toBe(true);
    expect(clock.offset).toBe(9000);

    now = 5000;
    expect(clock.serverNow()).toBe(14_000);
  });

  it('ignores missing server times', () => {
    const clock = new ServerClock(() => 0);
    expect(clock.observe(Number.NaN, 0, 0)).toBe(false);
    expect(clock.observe(0, 0, 0)).toBe(false);
    expect(clock.offset).toBe(0);
  });
});
