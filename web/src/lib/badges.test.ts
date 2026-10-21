import { describe, expect, it } from 'vitest';
import { problemTone } from './badges';

describe('badge tones', () => {
  it('colours bank status words', () => {
    expect(['Validated', 'Not validated', 'AI draft', 'Archived', 'Other'].map(problemTone)).toEqual(['ok', 'part', 'wait', 'muted', 'neutral']);
  });
});
