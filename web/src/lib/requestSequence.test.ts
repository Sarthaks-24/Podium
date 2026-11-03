import { describe, expect, it } from 'vitest';
import { RequestSequence } from './requestSequence';

describe('RequestSequence', () => {
  it('marks only the newest request current when responses arrive out of order', () => {
    const sequence = new RequestSequence();
    const older = sequence.next();
    const newer = sequence.next();

    expect(sequence.isCurrent(newer)).toBe(true);
    expect(sequence.isCurrent(older)).toBe(false);
  });
});
