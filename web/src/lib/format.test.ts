import { describe, expect, it } from 'vitest';
import { countOf, formatDateTime, formatDayTime, numberParts, roleLabel, scoreParts } from './format';
import { formatTitle } from './head';

describe('formatDateTime', () => {
  it('formats local time as "16 Oct 2026 19:42"', () => {
    const ms = new Date(2026, 9, 16, 19, 42).getTime();
    expect(formatDateTime(ms)).toBe('16 Oct 2026 19:42');
    expect(formatDateTime(new Date(2026, 0, 3, 7, 5).getTime())).toBe('3 Jan 2026 07:05');
  });

  it('formats a day and time for a sentence as "21 Oct, 00:11"', () => {
    expect(formatDayTime(new Date(2026, 9, 21, 0, 11).getTime())).toBe('21 Oct, 00:11');
  });
});

describe('roleLabel', () => {
  it('uses sentence case', () => {
    expect(roleLabel('owner')).toBe('Owner');
    expect(roleLabel('admin')).toBe('Organiser');
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

describe('numberParts and scoreParts', () => {
  it('sets only the digits in mono', () => {
    expect(numberParts('2 d 23 h')).toEqual([
      { text: '2', mono: true },
      { text: ' d ', mono: false },
      { text: '23', mono: true },
      { text: ' h', mono: false },
    ]);
    expect(numberParts('01:12:48')).toEqual([{ text: '01:12:48', mono: true }]);
    expect(numberParts('Ends in')).toEqual([{ text: 'Ends in', mono: false }]);
  });

  it('writes a score as digits around a Grotesk "of"', () => {
    expect(scoreParts(240, 300)).toEqual([
      { text: '240', mono: true },
      { text: ' of ', mono: false },
      { text: '300', mono: true },
    ]);
    expect(scoreParts(140)).toEqual([{ text: '140', mono: true }]);
    expect(scoreParts(0, null)).toEqual([{ text: '0', mono: true }]);
  });
});
