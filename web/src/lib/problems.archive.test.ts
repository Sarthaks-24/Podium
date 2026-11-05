import { describe, expect, it } from 'vitest';
import { archiveWarnings } from './problems';

describe('archiveWarnings', () => {
  it('names each unended contest and counts the ones the viewer does not manage', () => {
    expect(archiveWarnings('Two sum', [{ title: 'Week 3 practice' }], 2)).toEqual([
      'Two sum is in Week 3 practice, which has not ended. Students keep seeing it there, but you cannot update it in that contest until you restore it.',
      'and 2 contests you do not manage',
    ]);
    expect(archiveWarnings('Two sum', [], 1)).toEqual(['and 1 contest you do not manage']);
  });

  it('says nothing when no contest is open', () => {
    expect(archiveWarnings('Two sum', [], 0)).toEqual([]);
  });
});
