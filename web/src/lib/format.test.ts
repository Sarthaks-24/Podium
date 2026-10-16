import { describe, expect, it } from 'vitest';
import { formatDateTime, roleLabel } from './format';
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
