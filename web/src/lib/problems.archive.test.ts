import { describe, expect, it } from 'vitest';
import { archiveWarnings, importQuery, parseExisting, replaceBlocked, sameTitle } from './problems';

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

describe('import conflict helpers', () => {
  const contest = { contest_id: 4, slug: 'wk', title: 'Week 3 practice', label: 'A' };
  const existing = { id: 1, title: 'Two sum', slug: 'two-sum', live_contests: [], hidden_live_contests: 0, open_contests: [], hidden_open_contests: 0 };

  it('reads the existing problem of a conflict and ignores junk', () => {
    expect(parseExisting({ id: 1, title: 'Two sum', slug: 'two-sum', open_contests: [contest], hidden_open_contests: 2 })).toEqual({
      ...existing,
      open_contests: [contest],
      hidden_open_contests: 2,
    });
    expect(parseExisting(null)).toBeNull();
    expect(parseExisting({ id: 'x' })).toBeNull();
  });

  it('builds the query of each choice', () => {
    expect(importQuery('rename', 'two-sum-2', 'a b.zip')).toBe('?on_conflict=rename&slug=two-sum-2&file_name=a+b.zip');
    expect(importQuery('archive_existing', 'ignored', '')).toBe('?on_conflict=archive_existing');
    expect(importQuery('replace', '', 'p.zip')).toBe('?on_conflict=replace&file_name=p.zip');
  });

  it('blocks replace only while a contest is running', () => {
    expect(replaceBlocked(existing)).toBe('');
    expect(replaceBlocked({ ...existing, open_contests: [contest] })).toBe('');
    expect(replaceBlocked({ ...existing, live_contests: [contest] })).toBe(
      'Two sum is in Week 3 practice, which is running. Edit it from the problem page, where you can update students.',
    );
    expect(replaceBlocked({ ...existing, hidden_live_contests: 1 })).toContain('a running contest you do not manage');
  });

  it('finds a repeated title, active before archived, ignoring case and spaces', () => {
    const list = [
      { id: 1, title: 'Two  Sum', archived: true },
      { id: 2, title: 'two sum', archived: false },
      { id: 3, title: 'Other', archived: false },
    ];
    expect(sameTitle('TWOSUM', list)?.id).toBe(2);
    expect(sameTitle('two sum', [list[0]])?.id).toBe(1);
    expect(sameTitle('   ', list)).toBeNull();
    expect(sameTitle('Three sum', list)).toBeNull();
  });
});
