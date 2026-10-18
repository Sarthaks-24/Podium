import { describe, expect, it } from 'vitest';
import { countOf, formatDateTime, roleLabel } from './format';
import { formatTitle } from './head';

describe('formatDateTime', () => {
  it('formats local time as "16 Oct 2026 19:42"', () => {
    const ms = new Date(2026, 9, 16, 19, 42).getTime();
    expect(formatDateTime(ms)).toBe('16 Oct 2026 19:42');
    expect(formatDateTime(new Date(2026, 0, 3, 7, 5).getTime())).toBe('3 Jan 2026 07:05');
  });
});

describe('roleLabel', () => {
  it('uses sentence case', () => {
    expect(roleLabel('owner')).toBe('Owner');
    expect(roleLabel('admin')).toBe('Admin');
    expect(roleLabel('student')).toBe('Student');
  });
});

describe('formatTitle', () => {
  it('ends every title with the product name', () => {
    expect(formatTitle('Problem bank')).toBe('Problem bank · podium');
    expect(formatTitle('B', 'Weekly 12')).toBe('B · Weekly 12 · podium');
    expect(formatTitle()).toBe('podium');
    expect(formatTitle('', ' ')).toBe('podium');
  });
});

describe('countOf', () => {
  it('uses the singular for one and the plural otherwise', () => {
    expect(countOf(1, 'minute')).toBe('1 minute');
    expect(countOf(0, 'minute')).toBe('0 minutes');
    expect(countOf(15, 'minute')).toBe('15 minutes');
    expect(countOf(2, 'match', 'matches')).toBe('2 matches');
  });
});
