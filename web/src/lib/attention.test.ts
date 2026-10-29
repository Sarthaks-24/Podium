// The console's opening sentence (stage C2) and feed wording (v2 P3 F6.2; audit O3).
import { describe, expect, it } from 'vitest';
import { consoleSentence, endNowFreeze, feedOutcome, organiserState, publishChecks, sinceStart } from './contests';

const MIN = 60_000;
const T = 1_792_300_000_000;

const base = {
  starts_at: T - 30 * MIN,
  ends_at: T + 90 * MIN,
  freeze_minutes: 30,
  paused: false,
  state: 'live' as const,
  problems: [
    { label: 'A', problem_id: 1, title: 'A plus B' },
    { label: 'B', problem_id: 2, title: 'Pair with sum' },
  ],
};

describe('consoleSentence (stage C2)', () => {
  const calm = { ...base, queue: { waiting: 0, judging: 0, errors: 0, oldest_wait_s: 0 }, judge_last_claim_at: T - 3000 };

  it('says how it is going in one sentence when nothing is wrong', () => {
    expect(consoleSentence(calm, T, { unanswered: 0, started: 31 })).toEqual({ trouble: '', text: 'Live, 31 started, judge fine.' });
  });

  it('counts the questions waiting, so a new one changes the sentence', () => {
    expect(consoleSentence(calm, T, { unanswered: 1, started: 31 }).text).toBe('Live, 31 started, 1 question waiting, judge fine.');
    expect(consoleSentence(calm, T, { unanswered: 2, started: 31 }).text).toBe('Live, 31 started, 2 questions waiting, judge fine.');
  });

  it('puts a late judge first, in minutes', () => {
    const late = consoleSentence({ ...calm, judge_last_claim_at: T - 3 * MIN - 5000 }, T, { unanswered: 2, started: 31 });
    expect(late).toEqual({ trouble: 'Judge not seen for 3 minutes.', text: 'Live, 31 started, 2 questions waiting.' });
    expect(consoleSentence({ ...calm, judge_last_claim_at: null }, T, { unanswered: 0 }).trouble).toBe('No judge has asked for work since the server started.');
    // A minute is the line: a judge seen 59 s ago is fine.
    expect(consoleSentence({ ...calm, judge_last_claim_at: T - 59_000 }, T, { unanswered: 0 }).trouble).toBe('');
  });

  it('says a slow queue, and paused judging instead of the judge', () => {
    const slow = consoleSentence({ ...calm, queue: { waiting: 14, judging: 2, errors: 0, oldest_wait_s: 125 } }, T, { unanswered: 0 });
    expect(slow.trouble).toBe('The oldest submission has waited 2 minutes for the judge.');
    const paused = consoleSentence({ ...calm, paused: true, queue: { waiting: 14, judging: 0, errors: 0, oldest_wait_s: 300 } }, T, { unanswered: 0 });
    expect(paused).toEqual({ trouble: '', text: 'Live, judging paused.' });
  });

  it('says when results are hidden from students, and the state before and after', () => {
    // The judge was seen 3 s before each moment asked about.
    const at = (now: number) => ({ ...calm, judge_last_claim_at: now - 3000 });
    // The board stops updating for students 30 min before the close.
    expect(consoleSentence(at(T + 61 * MIN), T + 61 * MIN, { unanswered: 0 }).text).toMatch(/^Live, results from \d\d:\d\d hidden from students, judge fine\.$/);
    expect(consoleSentence(at(T - 40 * MIN), T - 40 * MIN, { unanswered: 0, started: 0 }).text).toBe('Upcoming, judge fine.');
    expect(consoleSentence({ ...at(T + 95 * MIN), close_at: T + 100 * MIN }, T + 95 * MIN, { unanswered: 0 }).text).toMatch(/^Live, /);
    expect(consoleSentence(at(T + 91 * MIN), T + 91 * MIN, { unanswered: 0, started: 40 }).text).toBe('Ended, 40 started, judge fine.');
  });

  it('leaves out what an older server does not send', () => {
    expect(consoleSentence(base, T, { unanswered: 0 })).toEqual({ trouble: '', text: 'Live.' });
  });
});

describe('feed wording', () => {
  it('words a partial score as students read it, apart from a zero', () => {
    expect(feedOutcome({ state: 'done', verdict: 'WA', score: 40, max_score: 100 })).toEqual({ text: 'Partial, 40 of 100', tone: 'part' });
    expect(feedOutcome({ state: 'done', verdict: 'WA', score: 0, max_score: 100 })).toEqual({ text: 'Wrong answer', tone: 'bad' });
    expect(feedOutcome({ state: 'done', verdict: 'AC', score: 100, max_score: 100 })).toEqual({ text: 'Accepted', tone: 'ok' });
    expect(feedOutcome({ state: 'running', verdict: '', score: 0, max_score: 100 }).text).toBe('Judging');
    expect(feedOutcome({ state: 'error', verdict: '', score: 0, max_score: 100 })).toEqual({ text: 'Judge error', tone: 'bad' });
  });

  it('gives the time since the start', () => {
    expect(sinceStart(T + 7 * MIN + 59_000, T)).toBe('+7 min');
    expect(sinceStart(T - 1, T)).toBeNull();
  });
});

describe('organiser state words', () => {
  it('reads Draft until published and Ended while practice is open', () => {
    expect(organiserState({ state: 'upcoming', published: false })).toEqual({ word: 'Draft', kind: 'draft' });
    expect(organiserState({ state: 'practice', published: true })).toEqual({ word: 'Ended, practice open', kind: 'ended' });
    expect(organiserState({ state: 'live', published: true })).toEqual({ word: 'Live', kind: 'live' });
    expect(organiserState({ state: 'live' })).toEqual({ word: 'Live', kind: 'live' });
  });
});

describe('publishChecks', () => {
  const c = { problems: [{ label: 'A', ready: true }], languages: ['cpp17'], starts_at: T + MIN, freeze_minutes: 0, hold_reveal: true };

  it('lets the allowed domains take part and warns about a held board with no freeze', () => {
    const out = publishChecks(c, false, T);
    expect(out.filter((x) => x.tone === 'bad')).toEqual([]);
    expect(out).toContainEqual({ tone: 'ok', text: 'Anyone from the allowed domains can take part.' });
    expect(out.at(-1)).toEqual({ tone: 'part', text: 'The board is held for a reveal, but with no freeze there is nothing to reveal.' });
  });

  it('blocks with no language allowed', () => {
    expect(publishChecks({ ...c, languages: [] }, false, T).filter((x) => x.tone === 'bad').map((x) => x.text)).toEqual(['Allow at least one language.']);
  });

  it('names changed problems and needs one problem at least', () => {
    expect(publishChecks({ ...c, problems: [{ label: 'A', ready: true }, { label: 'B', ready: false }] }, false, T)[0]).toEqual({
      tone: 'bad',
      text: 'Problem B has changed since it was added. Validate, then add again.',
    });
    expect(publishChecks({ ...c, problems: [] }, true, T)[0].text).toBe('Add at least one validated problem.');
  });
});

describe('endNowFreeze (walk 2026-10-21)', () => {
  const c = { freeze_minutes: 10, ends_at: T + 60 * MIN, extended_minutes: 0 };
  it('drops a freeze that has not begun, so the board students saw stays', () => {
    expect(endNowFreeze(c, T, T)).toBe(0);
  });
  it('keeps the frozen moment once the board is frozen, rounding up so it never moves later', () => {
    // Frozen at T + 50 min; ended at T + 55 min 30 s: 6 whole minutes put the frozen moment 30 s earlier.
    expect(endNowFreeze(c, T + 55.5 * MIN, T + 55.5 * MIN)).toBe(6);
    // Time added for everyone moves the frozen moment too.
    expect(endNowFreeze({ ...c, extended_minutes: 5 }, T + 52 * MIN, T + 57 * MIN)).toBe(2);
  });
  it('never drops a freeze that has begun, even in its first minute (V3-18, walk bug B1)', () => {
    // Frozen at T + 50 min; ended 52 s later. Rounding down stored 0 and released the held board.
    expect(endNowFreeze(c, T + 50 * MIN + 52_000, T + 50 * MIN + 52_000)).toBe(1);
    expect(endNowFreeze(c, T + 50 * MIN, T + 50 * MIN)).toBe(1);
  });
  it('leaves contests without a freeze alone', () => {
    expect(endNowFreeze({ ...c, freeze_minutes: 0 }, T, T)).toBe(0);
  });
});
