import { describe, expect, it } from 'vitest';
import { boardLabels, cellView, formatElapsed, myRow, ordinalWord, placeFigure, placeSentence, rankAnnouncement, statusLine, type Standings } from './standings';

function board(over: Partial<Standings> = {}): Standings {
  return {
    variant: 'full',
    version: 3,
    frozen_at: null,
    final: false,
    judging_remaining: 0,
    me: 7,
    rows: [
      { rank: 1, user_id: 1, name: 'Priya Raman', total: 200, last_improvement_s: 3600, cells: [{ label: 'A', score: 100, max: 100, pending: 0 }, { label: 'B', score: 100, max: 100, pending: 0 }] },
      { rank: 2, user_id: 7, name: 'Asha Rao', total: 140, last_improvement_s: 4000, cells: [{ label: 'A', score: 100, max: 100, pending: 0 }, { label: 'B', score: 40, max: 100, pending: 1 }] },
      { rank: 2, user_id: 9, name: 'Ravi Kumar', total: 140, last_improvement_s: 4000, cells: [{ label: 'A', score: 40, max: 100, pending: 0 }, { label: 'B', score: 100, max: 100, pending: 0 }] },
    ],
    ...over,
  };
}

describe('standings cells', () => {
  it('shows full, partial, pending, untried and only-pending cells', () => {
    expect(cellView({ label: 'A', score: 100, max: 100, pending: 0 }, 'A')).toEqual({
      text: '100',
      pending: '',
      fraction: 1,
      tone: 'ok',
      sentence: 'Problem A, 100 of 100',
    });
    expect(cellView({ label: 'D', score: 70, max: 100, pending: 1 }, 'D')).toMatchObject({
      text: '70',
      pending: '+1',
      fraction: 0.7,
      tone: 'part',
      sentence: 'Problem D, 70 of 100, 1 pending',
    });
    expect(cellView(undefined, 'C')).toMatchObject({ text: '-', tone: 'none', sentence: 'Problem C, not tried' });
    expect(cellView({ label: 'C', score: 0, max: 100, pending: 0 }, 'C').text).toBe('-');
    expect(cellView({ label: 'C', score: 0, max: 100, pending: 0, tried: true }, 'C')).toMatchObject({ text: '0', tone: 'none' });
    expect(cellView({ label: 'E', score: 0, max: 100, pending: 2 }, 'E')).toMatchObject({
      text: '',
      pending: '+2',
      sentence: 'Problem E, no score before the freeze, 2 pending',
    });
  });

  it('finds labels from the board, the rows or a fallback', () => {
    expect(boardLabels(board())).toEqual(['A', 'B']);
    expect(boardLabels(board({ labels: ['A', 'B', 'C'] }))).toEqual(['A', 'B', 'C']);
    expect(boardLabels(board({ rows: [] }), ['X'])).toEqual(['X']);
  });
});

describe('standings wording', () => {
  it('says whether the board is frozen, final or live, and what still judges', () => {
    const frozenAt = new Date(2026, 9, 18, 20, 30).getTime();
    expect(statusLine(board({ variant: 'frozen', frozen_at: frozenAt }))).toBe('Frozen at 20:30. Updates when the contest ends.');
    expect(statusLine(board({ final: true }))).toBe('Final standings.');
    expect(statusLine(board({ judging_remaining: 1 }))).toBe('Live. Updates as submissions are judged. 1 submission is still being judged.');
    expect(statusLine(board({ final: true, judging_remaining: 3 }))).toBe('Final standings. 3 submissions are still being judged.');
    // After the end a frozen board is held for the reveal: no promise that has already passed.
    expect(statusLine(board({ variant: 'frozen', frozen_at: frozenAt }), true)).toBe(
      'Frozen at 20:30. The final standings appear when the organisers release them.',
    );
  });

  it('finds the student and says their place, ties included', () => {
    expect(myRow(board())?.name).toBe('Asha Rao');
    expect(placeSentence(board())).toBe('You are tied 2nd of 3 with 140 points.');
    expect(placeSentence(board({ me: 1 }))).toBe('You are 1st of 3 with 200 points.');
    expect(placeSentence(board({ me: 99 }))).toBe('You are not in the standings.');
  });

  it('never prints a frozen total beside the live score', () => {
    expect(placeSentence(board({ variant: 'frozen' }))).toBe('In the frozen standings you are tied 2nd of 3.');
    expect(placeSentence(board({ variant: 'frozen' }), true)).toBe(
      'In the frozen standings you are tied 2nd of 3. Your final place shows when the organisers release the results.',
    );
    expect(placeSentence(board({ variant: 'frozen' }), true)).not.toMatch(/points/);
  });

  it('formats elapsed time and ordinals', () => {
    expect(formatElapsed(3723)).toBe('1:02:03');
    expect(formatElapsed(723)).toBe('12:03');
    expect(formatElapsed(5)).toBe('0:05');
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 103, 111].map(ordinalWord)).toEqual([
      '1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '103rd', '111th',
    ]);
  });
});

describe('own place (S6)', () => {
  const row = (user_id: number, rank: number, total: number) => ({ rank, user_id, name: `S${user_id}`, total, last_improvement_s: 0, cells: [] });
  const board = (rows: ReturnType<typeof row>[], variant: 'full' | 'frozen' = 'full') => ({ rows, me: 7, variant });

  it('gives the figure for the own row', () => {
    expect(placeFigure(board([row(1, 1, 300), row(7, 2, 200), row(8, 2, 200)]))).toEqual({ rank: 2, of: 3, tied: true, label: 'Your place' });
    expect(placeFigure(board([row(1, 1, 300)]))).toBeNull();
    expect(placeFigure(board([row(7, 1, 300)], 'frozen'))?.label).toBe('Your place on the frozen board');
  });

  it('announces a change of place or score as a sentence', () => {
    const before = board([row(1, 1, 300), row(7, 2, 200)]);
    expect(rankAnnouncement(null, before)).toBe('');
    expect(rankAnnouncement(before, before)).toBe('');
    expect(rankAnnouncement(before, board([row(7, 1, 340), row(1, 2, 300)]))).toBe('You are now 1st with 340 points.');
    expect(rankAnnouncement(before, board([row(1, 1, 300), row(7, 2, 240)]))).toBe('You are now 2nd with 240 points.');
    expect(rankAnnouncement(board([row(7, 2, 0)], 'frozen'), board([row(7, 3, 0)], 'frozen'))).toBe('You are now 3rd in the frozen standings.');
  });
});
