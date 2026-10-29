import { describe, expect, it } from 'vitest';
import {
  allowedLanguages,
  auditSummary,
  clockMilestone,
  formatCountdown,
  formatMinutes,
  formatSpan,
  freezeAt,
  fromLocalInput,
  hasAccepted,
  isForContest,
  labelFor,
  lengthText,
  listCountdown,
  organiserOrder,
  studentState,
  studentStateWord,
  orderedContests,
  rulesSentences,
  bannerItems,
  bannerLead,
  markMessagesRead,
  pinnedContest,
  messagesReadAt,
  railStatus,
  startLine,
  studentPhase,
  toLocalInput,
  type Contest,
  type ContestSummary,
} from './contests';

const MIN = 60_000;
const T = 1_800_000_000_000;

function summary(over: Partial<ContestSummary> = {}): ContestSummary {
  return {
    slug: 'week-6',
    title: 'Week 6',
    starts_at: T,
    ends_at: T + 120 * MIN,
    problem_count: 3,
    state: 'live',
    my_score: null,
    my_window: null,
    ...over,
  };
}

function contest(over: Partial<Contest> = {}): Contest {
  return {
    ...summary(),
    description_md: '',
    rules: {
      min_gap_s: 30,
      max_per_problem: 15,
      languages: ['cpp17', 'python3'],
      freeze_minutes: 30,
    },
    honour_text: '',
    problems: null,
    announcements: [],
    my_clarifications: [],
    public_clarifications: [],
    editorial_md: null,
    server_now: T,
    ...over,
  };
}

describe('countdowns', () => {
  it('formats a clock with hours, rounding seconds up and never going negative', () => {
    expect(formatCountdown(4_368_000)).toBe('01:12:48');
    expect(formatCountdown(999)).toBe('00:00:01');
    expect(formatCountdown(0)).toBe('00:00:00');
    expect(formatCountdown(-5000)).toBe('00:00:00');
    expect(formatCountdown(100 * 3_600_000)).toBe('100:00:00');
  });

  it('describes spans coarsely', () => {
    expect(formatSpan(2 * 86_400_000 + 4 * 3_600_000 + 5 * MIN)).toBe('2 d 4 h');
    expect(formatSpan(86_400_000)).toBe('1 d');
    expect(formatSpan(3 * 3_600_000 + 12 * MIN)).toBe('3 h 12 min');
    expect(formatSpan(32 * MIN)).toBe('32 min');
    expect(formatSpan(40_000)).toBe('40 s');
  });

  it('formats lengths in minutes', () => {
    expect(formatMinutes(150)).toBe('2 h 30 min');
    expect(formatMinutes(45)).toBe('45 min');
    expect(formatMinutes(60)).toBe('1 h');
    expect(lengthText(summary())).toBe('2 h');
  });

  it('announces milestones once as they are crossed', () => {
    expect(clockMilestone(31 * MIN, 30 * MIN)).toBe('30 minutes left.');
    expect(clockMilestone(30 * MIN, 29 * MIN)).toBeNull();
    expect(clockMilestone(10 * MIN + 1, 10 * MIN - 1)).toBe('10 minutes left.');
    expect(clockMilestone(61_000, 59_000)).toBe('1 minute left.');
    expect(clockMilestone(500, 0)).toBe('Time is up. Your submissions are saved.');
    expect(clockMilestone(0, -1000)).toBeNull();
    // A long jump (a sleeping laptop) names the smallest crossed.
    expect(clockMilestone(31 * MIN, 4 * MIN)).toBe('30 minutes left.');
  });

  it('picks the list countdown by state', () => {
    expect(listCountdown(summary({ state: 'upcoming' }), T - 5 * MIN)).toEqual({ lead: 'Starts in', ms: 5 * MIN, after: '' });
    expect(listCountdown(summary(), T + 20 * MIN)).toEqual({ lead: 'Ends in', ms: 100 * MIN, after: '' });
    expect(listCountdown(summary({ state: 'ended' }), T + 200 * MIN)).toBeNull();
    // A personal extension counts to the student's own end.
    expect(listCountdown(summary({ my_window: { started_at: T, ends_at: T + 130 * MIN } }), T)).toEqual({ lead: 'Ends in', ms: 130 * MIN, after: '' });
  });

  it('reads a frozen contest as live for students, with the freeze in the countdown (D84)', () => {
    expect(studentStateWord('frozen')).toBe('Live');
    expect(studentStateWord('practice')).toBe('Practice');
    expect(studentState('frozen')).toBe('live');
    expect(listCountdown(summary({ state: 'frozen' }), T + 100 * MIN)).toEqual({ lead: 'Ends in', ms: 20 * MIN, after: ', newest results hidden' });
  });
});

describe('the banner queue (B1)', () => {
  const q = (id: number, answered_at: number | null, pub: boolean, answer: string | null = 'Yes.') => ({
    id, problem_label: 'B', question: 'Zero length?', answer, public: pub, created_at: 1, answered_at,
  });
  it('adds answers given to everyone, newest first, and skips own and old ones', () => {
    const items = bannerItems(
      {
        announcements: [{ id: 3, body_md: 'Problem A fixed.', created_at: 500 }],
        my_clarifications: [q(10, 600, true)],
        public_clarifications: [q(10, 600, true), q(11, 700, true), q(12, 50, true), q(13, null, true, null)],
      },
      new Set(),
      100,
    );
    expect(items.map((i) => [i.key, i.kind])).toEqual([
      ['c11', 'public'],
      ['c10', 'answer'],
      ['a3', 'announcement'],
    ]);
    expect(bannerItems({ announcements: [], my_clarifications: [], public_clarifications: [q(11, 700, true)] }, new Set(['c11']), 0)).toEqual([]);
    expect(bannerLead('public')).toBe('An organiser answered a question for everyone');
    expect(bannerLead('answer')).toBe('An organiser answered your question');
  });
});

describe('the pinned contest on Your contests (S10)', () => {
  it('features a contest the student can enter or took part in, never one they missed (D324)', () => {
    const missed = { slug: 'week-8', state: 'ended' as const, my_score: null };
    const mine = { slug: 'week-7', state: 'practice' as const, my_score: 140 };
    const next = { slug: 'week-9', state: 'upcoming' as const, my_score: null };
    expect(pinnedContest([missed, mine])?.slug).toBe('week-7');
    expect(pinnedContest([next, missed, mine])?.slug).toBe('week-9');
    expect(pinnedContest([{ slug: 'now', state: 'live' as const, my_score: null }, mine])?.slug).toBe('now');
    expect(pinnedContest([missed])).toBeNull();
  });
});

describe('messages read time (audit S1)', () => {
  function memory(): Pick<Storage, 'getItem' | 'setItem'> & { data: Map<string, string> } {
    const data = new Map<string, string>();
    return { data, getItem: (k) => data.get(k) ?? null, setItem: (k, v) => void data.set(k, v) };
  }

  it('shows a message older than the page load but newer than the last read', () => {
    const store = memory();
    markMessagesRead('week-6', 1_000, store);
    const pageLoadedAt = 5_000;
    const items = bannerItems(
      {
        announcements: [
          { id: 1, body_md: 'Old news.', created_at: 900 },
          { id: 2, body_md: 'Sample 2 of B is fixed.', created_at: 3_000 },
        ],
        my_clarifications: [{ id: 7, problem_label: 'A', question: 'Is n at most 10?', answer: 'Yes.', public: false, created_at: 2_000, answered_at: 4_000 }],
      },
      new Set(),
      messagesReadAt('week-6', store),
    );
    expect(items.map((i) => i.key)).toEqual(['c7', 'a2']);
    expect(items.every((i) => i.at < pageLoadedAt)).toBe(true);
  });

  it('starts at zero, never moves back, and survives a broken store', () => {
    const store = memory();
    expect(messagesReadAt('week-6', store)).toBe(0);
    expect(markMessagesRead('week-6', 4_000, store)).toBe(4_000);
    expect(markMessagesRead('week-6', 2_000, store)).toBe(4_000);
    expect(store.data.get('podium-messages-read:week-6')).toBe('4000');
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(messagesReadAt('week-6', broken)).toBe(0);
    expect(markMessagesRead('week-6', 10, broken)).toBe(10);
    expect(messagesReadAt('week-6', null)).toBe(0);
  });
});

describe('rail status (S1-09)', () => {
  const p = (my_score: number | null, my_submission_count: number, max_score = 100) => ({ my_score, max_score, my_submission_count });
  it('shows the score and nothing else, or "not tried"', () => {
    expect(railStatus(p(null, 0))).toEqual({ score: '', word: 'not tried' });
    expect(railStatus(p(null, 2))).toEqual({ score: '', word: 'tried' });
    expect(railStatus(p(40, 2))).toEqual({ score: '40/100', word: '' });
    expect(railStatus(p(0, 1))).toEqual({ score: '0/100', word: '' });
    expect(railStatus(p(100, 3))).toEqual({ score: '100/100', word: '' });
  });
});

describe('organiser order (N6)', () => {
  it('puts live and frozen first, then upcoming soonest, then the rest newest', () => {
    const row = (id: number, state: string, starts: number, ends: number) => ({ id, state: state as 'live', starts_at: starts, ends_at: ends });
    const list = [row(1, 'ended', 10, 20), row(2, 'upcoming', 500, 600), row(3, 'frozen', 100, 400), row(4, 'upcoming', 300, 350), row(5, 'practice', 30, 40), row(6, 'live', 90, 200)];
    expect(organiserOrder(list).map((c) => c.id)).toEqual([6, 3, 4, 2, 5, 1]);
  });
});

describe('student phase and window maths', () => {
  it('walks a fixed-start contest from upcoming to after', () => {
    const c = contest();
    expect(studentPhase(c, T - 1)).toBe('upcoming');
    expect(studentPhase(c, T)).toBe('ready');
    const started = contest({ my_window: { started_at: T, ends_at: T + 120 * MIN }, accepted: true });
    expect(studentPhase(started, T + 10 * MIN)).toBe('during');
    expect(studentPhase(started, T + 120 * MIN)).toBe('after');
    expect(studentPhase(c, T + 120 * MIN)).toBe('after');
  });

  it('keeps a fixed window closed until the honour code is accepted', () => {
    const shown = contest({ my_window: { started_at: T, ends_at: T + 120 * MIN }, accepted: false });
    expect(studentPhase(shown, T + MIN)).toBe('ready');
  });

  it('infers acceptance from problems when the server sends no flag', () => {
    const base = { my_window: { started_at: T, ends_at: T + MIN }, problems: [{ label: 'A', title: 'x', my_score: null, max_score: 100 }] };
    expect(hasAccepted(base)).toBe(true);
    expect(hasAccepted({ ...base, problems: null })).toBe(false);
    expect(hasAccepted({ ...base, honour_accepted_at: null })).toBe(false);
    expect(hasAccepted({ ...base, accepted: false })).toBe(false);
  });

  it('freezes the board only with a freeze', () => {
    const c = contest();
    expect(freezeAt(c)).toBe(T + 90 * MIN);
    expect(freezeAt({ ...c, rules: { ...c.rules, freeze_minutes: 0 } })).toBeNull();
  });

  it('writes the rules as sentences', () => {
    const lines = rulesSentences(contest());
    expect(lines[0]).toBe('Everyone starts together and has 2 h.');
    expect(lines.join(' ')).toContain('Each problem adds up your best result on every subtask across your submissions.');
    expect(lines.join(' ')).toContain('Languages: C++17, Python 3.');
    expect(lines.at(-1)).toBe('The standings freeze 30 min before the end; later results appear after the contest.');
    const open = rulesSentences(contest({ rules: { ...contest().rules, freeze_minutes: 0 } }));
    expect(open.join(' ')).not.toContain('freeze');
  });

  it('teaches Run and Submit right after the timing (S3)', () => {
    const lines = rulesSentences(contest());
    expect(lines.slice(1, 4)).toEqual([
      'Run tries your code on the examples in the statement. It never counts.',
      'Submit grades your code on hidden tests, in parts called subtasks.',
      'A later, worse submission never lowers your score.',
    ]);
  });

  it('never promises hidden tests during a contest (V3-6)', () => {
    expect(rulesSentences(contest()).join(' ')).toContain('never the hidden inputs or outputs');
  });

  it('says the time of a late arrival is already running', () => {
    const c = contest();
    expect(startLine(c, c.starts_at - MIN)).toEqual({ kind: 'before', ms: MIN });
    expect(startLine(c, c.starts_at + MIN)).toEqual({ kind: 'late', ms: 119 * MIN });
  });
});

describe('lists, labels and events', () => {
  it('orders live first, then upcoming soonest, then past newest', () => {
    const list = orderedContests({
      live: [summary({ slug: 'l' })],
      upcoming: [summary({ slug: 'u2', starts_at: T + 2 }), summary({ slug: 'u1', starts_at: T + 1 })],
      past: [summary({ slug: 'p1', ends_at: 1 }), summary({ slug: 'p2', ends_at: 2 })],
    });
    expect(list.map((c) => c.slug)).toEqual(['l', 'u1', 'u2', 'p2', 'p1']);
  });

  it('labels problems A to Z then AA', () => {
    expect([0, 1, 25, 26, 27].map(labelFor)).toEqual(['A', 'B', 'Z', 'AA', 'AB']);
  });

  it('filters languages to the contest list', () => {
    expect(allowedLanguages(['python3']).map((l) => l.id)).toEqual(['python3']);
    expect(allowedLanguages([]).length).toBe(4);
  });

  it('matches events by contest id or slug', () => {
    expect(isForContest({ contest: 4 }, { id: 4, slug: 'w' })).toBe(true);
    expect(isForContest({ contest: 'w' }, { slug: 'w' })).toBe(true);
    expect(isForContest({ contest: 5 }, { id: 4, slug: 'w' })).toBe(false);
    expect(isForContest({ version: 2 }, { slug: 'w' })).toBe(true);
    expect(isForContest(null, { slug: 'w' })).toBe(false);
  });

  it('round-trips datetime-local values', () => {
    const ms = new Date(2026, 9, 18, 18, 30).getTime();
    expect(toLocalInput(ms)).toBe('2026-10-18T18:30');
    expect(fromLocalInput('2026-10-18T18:30')).toBe(ms);
    expect(fromLocalInput('tomorrow')).toBeNull();
  });
});

describe('auditSummary', () => {
  it('names revision moves (v1.2.0)', () => {
    expect(auditSummary({ action: 'revision', details: { label: 'A', from_rev: 2, to_rev: 3, statement_changed: true, judging_changed: false } })).toBe(
      'Moved problem A from revision 2 to revision 3 (statement changed)',
    );
    expect(auditSummary({ action: 'revision', details: { label: 'B', from_rev: 1, to_rev: 2, statement_changed: false, judging_changed: true } })).toContain('(tests changed)');
  });

  it('names each change in the past tense', () => {
    expect(
      auditSummary({
        action: 'settings',
        details: {
          changes: {
            freeze_minutes: { from: 0, to: 10 },
            graded: { from: true, to: false },
            languages: { from: ['cpp17'], to: [] },
          },
        },
      }),
    ).toBe('Changed freeze from 0 to 10; graded from yes to no; languages from cpp17 to none');
    expect(auditSummary({ action: 'extension', details: { email: 'asha@college.edu', minutes: 20, reason: 'laptop crashed' } })).toBe(
      'Gave asha@college.edu 20 extra minutes (laptop crashed)',
    );
    // Without the email and reason (the shape after the P3 review), the name comes from the console's list.
    expect(auditSummary({ action: 'extension', details: { user_id: 7, minutes: 15 } }, new Map([[7, 'Asha Rao']]))).toBe('Gave Asha Rao 15 extra minutes');
    expect(auditSummary({ action: 'extension', details: { user_id: 7, minutes: 15 } })).toBe('Gave user 7 15 extra minutes');
    expect(auditSummary({ action: 'extension', details: { email: 'asha@college.edu', minutes: 0 } })).toBe(
      'Removed the extension for asha@college.edu',
    );
    // v1.4.0: the row says whether a reason was written, never the reason.
    expect(auditSummary({ action: 'extension', details: { user_id: 7, minutes: 15, reason_given: true } }, new Map([[7, 'Asha Rao']]))).toBe(
      'Gave Asha Rao 15 extra minutes, with a reason (see Extra time)',
    );
    expect(auditSummary({ action: 'extension', details: { user_id: 7, minutes: 15, reason_given: false } }, new Map([[7, 'Asha Rao']]))).toBe(
      'Gave Asha Rao 15 extra minutes, without a reason',
    );
    expect(auditSummary({ action: 'integrity_decision', details: { user_id: 7, decision: 'excluded' } }, new Map([[7, 'Asha Rao']]))).toBe(
      'Recorded an integrity decision about Asha Rao: excluded from this contest',
    );
    expect(auditSummary({ action: 'rejudge', details: { problem_label: 'B', queued: 1 } })).toBe('Judged problem B again (1 submission)');
    expect(auditSummary({ action: 'rejudge', details: { submission_id: 42, queued: 1 } })).toBe('Judged submission 42 again');
    expect(auditSummary({ action: 'problems', details: { from: [], to: [{ label: 'A' }, { label: 'B' }] } })).toBe('Set the problems to A, B');
    expect(auditSummary({ action: 'ai_problem', details: { label: 'B', reason: 'Checked by hand.' } })).toBe(
      'Graded AI problem B as written: Checked by hand.',
    );
    expect(auditSummary({ action: 'editorial', details: { published: true, was_published: false } })).toBe('Published the editorial');
    expect(auditSummary({ action: 'pause', details: {} })).toBe('Paused judging');
    expect(auditSummary({ action: 'something_new', details: {} })).toBe('something_new');
  });
});

