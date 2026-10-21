// The console's attention strip and feed wording (v2 P3 F6.1, F6.2; audit O3).
import { describe, expect, it } from 'vitest';
import { attentionItems, endNowFreeze, feedOutcome, organiserState, publishChecks, sinceStart } from './contests';

const MIN = 60_000;
const T = 1_792_300_000_000;

const base = {
  starts_at: T - 30 * MIN,
  ends_at: T + 90 * MIN,
  start_mode: 'fixed' as const,
  freeze_minutes: 30,
  paused: false,
  state: 'live' as const,
  problems: [
    { label: 'A', problem_id: 1, title: 'A plus B' },
    { label: 'B', problem_id: 2, title: 'Pair with sum' },
  ],
};

describe('attentionItems', () => {
  it('stays quiet when nothing needs action', () => {
    const items = attentionItems({ ...base, queue: { waiting: 0, judging: 0, errors: 0, oldest_wait_s: 0 }, judge_last_claim_at: T - 3000 }, T, 0);
    expect(items.map((i) => i.text)).toEqual(['Ends in 01:30:00', 'Board freezes in 1 h', 'Judging on', 'Queue empty', 'Judge seen 3 s ago', 'No unanswered questions']);
    expect(items.every((i) => i.tone === 'note')).toBe(true);
  });

  it('marks what needs action: a slow queue, judge errors, a silent judge, questions, drift', () => {
    const items = attentionItems(
      {
        ...base,
        problems: [{ ...base.problems[0] }, { ...base.problems[1], toolchain_drift: true }],
        queue: { waiting: 14, judging: 2, errors: 1, oldest_wait_s: 95 },
        judge_last_claim_at: T - 3 * MIN,
      },
      T,
      2,
    );
    const by = Object.fromEntries(items.map((i) => [i.key, i]));
    expect(by.queue).toMatchObject({ text: '14 waiting, 2 judging, oldest 1 min', tone: 'part' });
    expect(by.errors).toMatchObject({ text: '1 judge error', tone: 'bad' });
    expect(by.judge).toMatchObject({ text: 'No judge for 3 min', tone: 'bad' });
    expect(by.questions).toMatchObject({ text: '2 unanswered questions', tone: 'bad', href: '#clar-title' });
    expect(by.drift).toMatchObject({ text: 'Problem B was validated on another judge toolchain', tone: 'part' });
  });

  it('says paused judging and never-seen judges, and leaves out what an older server does not send', () => {
    const paused = attentionItems({ ...base, paused: true, judge_last_claim_at: null }, T, 0);
    expect(paused.find((i) => i.key === 'judging')).toMatchObject({ text: 'Judging paused', tone: 'part' });
    expect(paused.find((i) => i.key === 'judge')?.tone).toBe('bad');
    const old = attentionItems(base, T, 0);
    expect(old.map((i) => i.key)).toEqual(['time', 'freeze', 'judging', 'questions']);
  });

  it('names a problem whose newer revision students do not have yet (walk 2026-10-21)', () => {
    const items = attentionItems({ ...base, problems: [{ ...base.problems[0], update_available: true }, { ...base.problems[1] }] }, T, 0);
    expect(items.find((i) => i.key === 'update')).toMatchObject({ text: 'Problem A has a newer revision for students', tone: 'part', href: '#versions-title' });
    expect(attentionItems(base, T, 0).find((i) => i.key === 'update')).toBeUndefined();
  });

  it('counts down to the start before it and says Ended after the close', () => {
    expect(attentionItems(base, T - 40 * MIN, 0)[0].text).toBe('Starts in 00:10:00');
    expect(attentionItems({ ...base, close_at: T + 100 * MIN }, T + 95 * MIN, 0)[0].text).toBe('Ends in 00:05:00');
    expect(attentionItems(base, T + 91 * MIN, 0)[0].text).toBe('Ended');
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
  const c = { problems: [{ label: 'A', ready: true }], access: 'roster' as const, roster_count: 0, languages: ['cpp17'], starts_at: T + MIN, freeze_minutes: 0, hold_reveal: true };

  it('blocks on an empty roster and warns about a held board with no freeze', () => {
    const out = publishChecks(c, false, T);
    expect(out.filter((x) => x.tone === 'bad').map((x) => x.text)).toEqual(['The roster is empty, so nobody can take part. Add the roster under More.']);
    expect(out.at(-1)).toEqual({ tone: 'part', text: 'The board is held for a reveal, but with no freeze there is nothing to reveal.' });
  });

  it('names changed problems and needs one problem at least', () => {
    expect(publishChecks({ ...c, roster_count: 3, problems: [{ label: 'A', ready: true }, { label: 'B', ready: false }] }, false, T)[0]).toEqual({
      tone: 'bad',
      text: 'Problem B has changed since it was added. Validate, then add again.',
    });
    expect(publishChecks({ ...c, problems: [] }, true, T)[0].text).toBe('Add at least one validated problem.');
  });
});

describe('endNowFreeze (walk 2026-10-21)', () => {
  const c = { start_mode: 'fixed' as const, freeze_minutes: 10, ends_at: T + 60 * MIN, extended_minutes: 0 };
  it('drops a freeze that has not begun, so the board students saw stays', () => {
    expect(endNowFreeze(c, T, T)).toBe(0);
  });
  it('keeps the frozen moment once the board is frozen, rounding so nothing shown is hidden', () => {
    // Frozen at T + 50 min; ended at T + 55 min 30 s: 5 whole minutes keep the frozen moment within a minute.
    expect(endNowFreeze(c, T + 55.5 * MIN, T + 55.5 * MIN)).toBe(5);
    // Time added for everyone moves the frozen moment too.
    expect(endNowFreeze({ ...c, extended_minutes: 5 }, T + 52 * MIN, T + 57 * MIN)).toBe(2);
  });
  it('leaves window mode and contests without a freeze alone', () => {
    expect(endNowFreeze({ ...c, start_mode: 'window' }, T, T)).toBe(10);
    expect(endNowFreeze({ ...c, freeze_minutes: 0 }, T, T)).toBe(0);
  });
});
