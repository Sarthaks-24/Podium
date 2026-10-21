import { describe, expect, it } from 'vitest';
import {
  bandOf,
  crossingText,
  deltaText,
  filterHelp,
  linesText,
  lowersBand,
  parseLines,
  parseRating,
  ratingConfirm,
  ratingEditProblems,
  ratingText,
  sortStudents,
  trendPoints,
  trendSummary,
  type StudentContest,
  type StudentRow,
} from './students';

describe('rating words', () => {
  it('shows tenths as a value out of 10', () => {
    expect(ratingText(85)).toBe('8.5');
    expect(ratingText(100)).toBe('10');
    expect(ratingText(0)).toBe('0');
    expect(deltaText(-15)).toBe('-1.5');
    expect(deltaText(5)).toBe('+0.5');
  });

  it('puts a value in its band at the edges', () => {
    expect(bandOf(100)).toBe('good');
    expect(bandOf(90)).toBe('good');
    expect(bandOf(89)).toBe('on_record');
    expect(bandOf(60)).toBe('on_record');
    expect(bandOf(59)).toBe('repeated');
    expect(lowersBand(90, 89)).toBe(true);
    expect(lowersBand(85, 70)).toBe(false);
    expect(lowersBand(55, 95)).toBe(false);
  });

  it('marks only the history line that crossed a band', () => {
    expect(crossingText({ band_before: 'good', band_after: 'on_record' })).toBe('Dropped from Good standing to Outcomes on record');
    expect(crossingText({ band_before: 'repeated', band_after: 'on_record' })).toBe('Rose from Repeated outcomes to Outcomes on record');
    expect(crossingText({ band_before: 'on_record', band_after: 'on_record' })).toBe('');
  });
});

describe('the rating edit form', () => {
  it('reads one decimal, with a comma too', () => {
    expect(parseRating('7.5')).toEqual({ tenths: 75 });
    expect(parseRating(' 7,5 ')).toEqual({ tenths: 75 });
    expect(parseRating('10')).toEqual({ tenths: 100 });
    expect(parseRating('10.5')).toHaveProperty('error');
    expect(parseRating('7.55')).toHaveProperty('error');
    expect(parseRating('-1')).toHaveProperty('error');
    expect(parseRating('')).toHaveProperty('error');
  });

  it('refuses an empty reason, a short one and an unchanged value', () => {
    expect(ratingEditProblems('7', '', 85).reason).toBe('Write the reason first. The student reads it on their standing.');
    expect(ratingEditProblems('7', '   ', 85).reason).not.toBe('');
    expect(ratingEditProblems('7', 'Too short', 85).reason).toContain('at least 20');
    expect(ratingEditProblems('8.5', 'A reason long enough to send.', 85).value).toContain('already 8.5');
    expect(ratingEditProblems('7', 'A reason long enough to send.', 85)).toEqual({ value: '', reason: '' });
  });

  it('confirms with the old and new band, and waits when the band drops', () => {
    const down = ratingConfirm('Asha Rao', 85, 55);
    expect(down.title).toBe("Change Asha Rao's rating from 8.5 to 5.5?");
    expect(down.items[0]).toBe('The band goes from Outcomes on record to Repeated outcomes.');
    expect(down.waits).toBe(true);
    expect(down.label).toBe('Propose the change');
    const same = ratingConfirm('Asha Rao', 85, 70);
    expect(same.items[0]).toBe('The band stays Outcomes on record.');
    expect(same.waits).toBe(false);
    expect(same.label).toBe('Change the rating');
  });
});

describe('the students table', () => {
  const row = (id: number, name: string, rating: number): StudentRow => ({
    id,
    name,
    email: `${id}@c.edu`,
    graded_contests: 1,
    last_graded_percent: 50,
    last_graded_slug: 'w',
    last_submission_at: null,
    rating_tenths: rating,
  });
  const rows = [row(1, 'zoë', 90), row(2, 'Asha', 60), row(3, 'bilal', 90)];

  it('sorts by name as people read it, and by rating with the name as tie-break', () => {
    expect(sortStudents(rows, 'name', 'asc').map((r) => r.id)).toEqual([2, 3, 1]);
    expect(sortStudents(rows, 'name', 'desc').map((r) => r.id)).toEqual([1, 3, 2]);
    expect(sortStudents(rows, 'rating', 'asc').map((r) => r.id)).toEqual([2, 3, 1]);
    expect(sortStudents(rows, 'rating', 'desc').map((r) => r.id)).toEqual([3, 1, 2]);
  });

  it('names the contests a filter used', () => {
    const last = [{ title: 'Week 6' }, { title: 'Week 5' }];
    expect(filterHelp('missed_last_two', last)).toContain('Week 6 and Week 5');
    expect(filterHelp('no_accepted', [{ title: 'Week 6' }])).toContain('in Week 6 and none accepted');
    expect(filterHelp('no_accepted', [])).toContain('nobody matches');
  });
});

describe('the trend', () => {
  const c = (slug: string, ended: number, percent: number, median: number | null, graded = true): StudentContest => ({
    contest_id: ended,
    slug,
    title: slug,
    graded,
    ended_at: ended,
    score: 0,
    max_score: 100,
    percent,
    class_median_percent: median,
    rank: 1,
    ranked_count: 1,
    problems_attempted: 0,
    problems_full: 0,
    submissions: 0,
    accepted: 0,
  });

  it('keeps graded contests, oldest first, and sums up against the median', () => {
    const points = trendPoints([c('w6', 3, 80, 62.5), c('practice', 2, 10, 10, false), c('w4', 1, 40, 50)]);
    expect(points.map((p) => p.slug)).toEqual(['w4', 'w6']);
    expect(trendSummary('Asha', points)).toBe('Score as a percentage over 2 graded contests. Latest, w6: 80 percent, above the class median of 62.5 percent.');
    expect(trendSummary('Asha', [])).toBe('Asha has no final graded contest yet.');
  });
});

describe('note lines', () => {
  it('reads single lines and ranges, and refuses reversed ones', () => {
    expect(parseLines('')).toEqual({ lines: [] });
    expect(parseLines('7')).toEqual({ lines: [{ from: 7, to: 7 }] });
    expect(parseLines('3-5, 12')).toEqual({ lines: [{ from: 3, to: 5 }, { from: 12, to: 12 }] });
    expect(parseLines('5-3')).toHaveProperty('error');
    expect(parseLines('0')).toHaveProperty('error');
    expect(parseLines('a')).toHaveProperty('error');
    expect(linesText([{ from: 3, to: 5 }, { from: 12, to: 12 }])).toBe('lines 3 to 5 and 12');
    expect(linesText([{ from: 7, to: 7 }])).toBe('line 7');
  });
});
