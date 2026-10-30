import { describe, expect, it } from 'vitest';
import { organises } from './roles';

describe('organises', () => {
  it('is true for organisers, admins and owners only', () => {
    expect(['student', 'organiser', 'admin', 'owner', undefined].map((r) => organises(r as never))).toEqual([false, true, true, true, false]);
  });
});
