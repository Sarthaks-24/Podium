import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CLEAR_MS, assertive, hush, polite, regions } from './announce.svelte';

describe('announcer', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('announces the same text twice by clearing the region first', () => {
    const seen: string[] = [];
    polite('Saved');
    seen.push(regions.polite);
    vi.advanceTimersByTime(CLEAR_MS);
    seen.push(regions.polite);
    polite('Saved');
    seen.push(regions.polite);
    vi.advanceTimersByTime(CLEAR_MS);
    seen.push(regions.polite);
    expect(seen).toEqual(['', 'Saved', '', 'Saved']);
  });

  it('keeps the latest text when two arrive at once', () => {
    polite('Run finished');
    polite('Draft saved');
    vi.advanceTimersByTime(CLEAR_MS);
    expect(regions.polite).toBe('Draft saved');
  });

  it('keeps the two regions apart', () => {
    polite('Draft saved');
    assertive('Time is up');
    vi.advanceTimersByTime(CLEAR_MS);
    expect(regions.polite).toBe('Draft saved');
    expect(regions.assertive).toBe('Time is up');
  });

  it('hush empties the polite region, drops a pending sentence and leaves the assertive one', () => {
    polite('Accepted. Score 100 of 100.');
    assertive('Time is up');
    vi.advanceTimersByTime(CLEAR_MS);
    polite('Run finished');
    hush();
    vi.advanceTimersByTime(CLEAR_MS);
    expect(regions.polite).toBe('');
    expect(regions.assertive).toBe('Time is up');
  });
});
