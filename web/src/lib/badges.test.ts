import { describe, expect, it } from 'vitest';
import { contestTone, problemTone, scoreTone } from './badges';

describe('badge tones', () => {
  it('colours contest states', () => {
    expect(['live', 'frozen', 'upcoming', 'practice', 'ended', 'archived', 'draft'].map(contestTone)).toEqual([
      'act',
      'act',
      'neutral',
      'neutral',
      'muted',
      'muted',
      'neutral',
    ]);
  });

  it('colours scores', () => {
    expect(scoreTone(100, 100)).toBe('ok');
    expect(scoreTone(40, 100)).toBe('part');
    expect(scoreTone(0, 100)).toBe('bad');
    expect(scoreTone(null, 100)).toBe('wait');
  });

  it('colours bank status words', () => {
    expect(['Validated', 'Not validated', 'AI draft', 'Archived', 'Other'].map(problemTone)).toEqual(['ok', 'part', 'wait', 'muted', 'neutral']);
  });
});
