import { describe, expect, it } from 'vitest';
import { confirmSlug, deleteRefusal, practiceClosing, slugConfirmed, type DeleteInfo } from './problems';
import { administers } from './roles';

const week = { contest_id: 4, slug: 'wk', title: 'Week 3', label: 'A' };
const lab = { contest_id: 5, slug: 'lab', title: 'Lab 2', label: 'B' };
const info = (over: Partial<DeleteInfo> = {}): DeleteInfo => ({ used: false, contests: [], unended: [], practice: [], ...over });

describe('deleteRefusal', () => {
  it('uses the server words for a contest that has not ended, whoever asks', () => {
    const want = 'This problem is in contests that have not ended: Week 3, Lab 2. Remove it from them or wait until they end.';
    expect(deleteRefusal(info({ used: true, unended: [week, lab] }), 'owner')).toBe(want);
    expect(deleteRefusal(info({ used: true, unended: [week, lab] }), 'admin')).toBe(want);
  });

  it('lets only the owner delete a used problem', () => {
    const want = 'Only the site owner can delete a problem that has been used. Archive it instead.';
    expect(deleteRefusal(info({ used: true, contests: [week] }), 'admin')).toBe(want);
    expect(deleteRefusal(info({ used: true, contests: [week] }), 'owner')).toBe('');
  });

  it('lets an admin delete a problem that was never used', () => {
    expect(deleteRefusal(info(), 'admin')).toBe('');
  });
});

describe('practiceClosing', () => {
  it('names the ended contests where practice closes', () => {
    expect(practiceClosing(info({ practice: [week, lab] }))).toBe('Practice for this problem closes in: Week 3, Lab 2.');
    expect(practiceClosing(info())).toBe('');
  });
});

describe('slug confirmation', () => {
  it('needs the exact slug, with spaces around it ignored', () => {
    expect(slugConfirmed(' two-sum ', 'two-sum')).toBe(true);
    expect(slugConfirmed('Two-sum', 'two-sum')).toBe(false);
    expect(slugConfirmed('', '')).toBe(false);
  });

  it('asks for the original slug of an archived problem', () => {
    expect(confirmSlug({ slug: 'two-sum-archived-4', archived: true, archived_slug: 'two-sum' })).toBe('two-sum');
    expect(confirmSlug({ slug: 'two-sum', archived: false, archived_slug: '' })).toBe('two-sum');
  });
});

describe('administers', () => {
  it('is true for admin and owner only', () => {
    expect(['student', 'organiser', 'admin', 'owner', undefined].map((r) => administers(r as never))).toEqual([false, false, true, true, false]);
  });
});
