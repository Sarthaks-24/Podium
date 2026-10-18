import { describe, expect, it } from 'vitest';
import {
  allowedLanguages,
  auditSummary,
  clockMilestone,
  formatCountdown,
  formatMinutes,
  formatSpan,
  freezeAt,
  freezeText,
  fromLocalInput,
  hasAccepted,
  isForContest,
  labelFor,
  lengthText,
  listCountdown,
  orderedContests,
  parseCsv,
  parseRoster,
  rosterCsv,
  rulesSentences,
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
    start_mode: 'fixed',
    duration_minutes: 0,
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
      feedback_mode: 'subtask',
      scoring_mode: 'best_subtask',
      min_gap_s: 30,
      max_per_problem: 15,
      languages: ['cpp17', 'python3'],
      freeze_minutes: 30,
      graded: true,
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
    expect(lengthText(summary({ start_mode: 'window', duration_minutes: 90 }))).toBe('1 h 30 min each');
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

  it('picks the list countdown by state and window', () => {
    expect(listCountdown(summary({ state: 'upcoming' }), T - 5 * MIN)).toEqual({ lead: 'Starts in', ms: 5 * MIN });
    expect(listCountdown(summary(), T + 20 * MIN)).toEqual({ lead: 'Ends in', ms: 100 * MIN });
    const win = summary({ start_mode: 'window', duration_minutes: 60, my_window: { started_at: T + 10 * MIN, ends_at: T + 70 * MIN } });
    expect(listCountdown(win, T + 20 * MIN)).toEqual({ lead: 'Your time left', ms: 50 * MIN });
    expect(listCountdown(summary({ start_mode: 'window', duration_minutes: 60 }), T)).toEqual({ lead: 'Open for', ms: 120 * MIN });
    expect(listCountdown(summary({ state: 'ended' }), T + 200 * MIN)).toBeNull();
    // A fixed start with a personal extension counts to the student's own end.
    expect(listCountdown(summary({ my_window: { started_at: T, ends_at: T + 130 * MIN } }), T)).toEqual({ lead: 'Ends in', ms: 130 * MIN });
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

  it('runs a window-mode clock from the student start, with a personal extension', () => {
    const c = contest({ start_mode: 'window', duration_minutes: 60, ends_at: T + 6 * 60 * MIN });
    expect(studentPhase(c, T + 3 * 60 * MIN)).toBe('ready');
    const started = { ...c, my_window: { started_at: T + 3 * 60 * MIN, ends_at: T + 4 * 60 * MIN + 15 * MIN }, honour_accepted_at: T + 3 * 60 * MIN };
    expect(studentPhase(started, T + 4 * 60 * MIN + 10 * MIN)).toBe('during');
    expect(studentPhase(started, T + 4 * 60 * MIN + 15 * MIN)).toBe('after');
    // Never started and the contest closed: after, with nothing to show.
    expect(studentPhase(c, T + 6 * 60 * MIN)).toBe('after');
  });

  it('infers acceptance from problems when the server sends no flag', () => {
    const base = { my_window: { started_at: T, ends_at: T + MIN }, problems: [{ label: 'A', title: 'x', my_score: null, max_score: 100 }] };
    expect(hasAccepted(base)).toBe(true);
    expect(hasAccepted({ ...base, problems: null })).toBe(false);
    expect(hasAccepted({ ...base, honour_accepted_at: null })).toBe(false);
    expect(hasAccepted({ ...base, accepted: false })).toBe(false);
  });

  it('freezes fixed-start boards only', () => {
    const c = contest();
    expect(freezeAt(c)).toBe(T + 90 * MIN);
    expect(freezeAt({ ...c, start_mode: 'window' })).toBeNull();
    expect(freezeAt({ ...c, rules: { ...c.rules, freeze_minutes: 0 } })).toBeNull();
    expect(freezeText(T + 90 * MIN, T + 58 * MIN)).toBe('Standings freeze in 32 min');
    expect(freezeText(T + 90 * MIN, T + 90 * MIN)).toBe('Standings frozen');
    expect(freezeText(null, T)).toBeNull();
  });

  it('writes the rules as sentences', () => {
    const lines = rulesSentences(contest({ rules: { ...contest().rules, feedback_mode: 'exam' } }));
    expect(lines[0]).toBe('Everyone starts together and has 2 h.');
    expect(lines.join(' ')).toContain('only says "Submitted"');
    expect(lines.join(' ')).toContain('Languages: C++17, Python 3.');
    expect(lines.join(' ')).toContain('The standings freeze 30 min before the end');
    const win = rulesSentences(contest({ start_mode: 'window', duration_minutes: 90 }));
    expect(win[0]).toContain('runs 1 h 30 min');
    expect(win.at(-1)).toBe('The standings open when the contest closes.');
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

describe('roster CSV', () => {
  it('splits quoted fields and CRLF lines', () => {
    expect(parseCsv('a,"b, c","d ""e"""\r\nf,g\n\n')).toEqual([
      ['a', 'b, c', 'd "e"'],
      ['f', 'g'],
    ]);
  });

  it('reads positional columns and flags bad lines', () => {
    const r = parseRoster('asha@college.edu,Asha Rao,21CS001,A\nnot-an-email,X\nASHA@college.edu,Again\n,No email\n');
    expect(r.valid).toBe(1);
    expect(r.invalid).toBe(3);
    expect(r.lines[0]).toEqual({ email: 'asha@college.edu', display_name: 'Asha Rao', roll_number: '21CS001', section: 'A', line: 1, error: '' });
    expect(r.lines.map((l) => l.error)).toEqual(['', 'Not an email address', 'Listed twice', 'No email']);
  });

  it('follows a header row in any order', () => {
    const r = parseRoster('Section,Roll No,Email,Name\nB,21CS002,ravi@college.edu,"Ravi, K"\n');
    expect(r.lines).toEqual([{ email: 'ravi@college.edu', display_name: 'Ravi, K', roll_number: '21CS002', section: 'B', line: 2, error: '' }]);
  });

  it('writes CSV that parses back', () => {
    const entries = [{ email: 'a@b.co', display_name: 'A "Q", R', roll_number: '1', section: '' }];
    expect(parseRoster(rosterCsv(entries)).lines[0]).toMatchObject(entries[0]);
  });
});

describe('auditSummary', () => {
  it('names each change in the past tense', () => {
    expect(
      auditSummary({
        action: 'settings',
        details: {
          changes: {
            feedback_mode: { from: 'subtask', to: 'exam' },
            graded: { from: true, to: false },
            languages: { from: ['cpp17'], to: [] },
          },
        },
      }),
    ).toBe('Changed feedback from subtask to exam; graded from yes to no; languages from cpp17 to none');
    expect(auditSummary({ action: 'extension', details: { email: 'asha@college.edu', minutes: 20, reason: 'laptop crashed' } })).toBe(
      'Gave asha@college.edu 20 extra minutes (laptop crashed)',
    );
    expect(auditSummary({ action: 'extension', details: { email: 'asha@college.edu', minutes: 0 } })).toBe(
      'Removed the extension for asha@college.edu',
    );
    expect(auditSummary({ action: 'rejudge', details: { problem_label: 'B', queued: 1 } })).toBe('Rejudged problem B (1 submission)');
    expect(auditSummary({ action: 'rejudge', details: { submission_id: 42, queued: 1 } })).toBe('Rejudged submission 42');
    expect(auditSummary({ action: 'problems', details: { from: [], to: [{ label: 'A' }, { label: 'B' }] } })).toBe('Set the problems to A, B');
    expect(auditSummary({ action: 'ai_problem', details: { label: 'B', reason: 'Checked by hand.' } })).toBe(
      'Graded AI problem B as written: Checked by hand.',
    );
    expect(auditSummary({ action: 'editorial', details: { published: true, was_published: false } })).toBe('Published the editorial');
    expect(auditSummary({ action: 'pause', details: {} })).toBe('Paused judging');
    expect(auditSummary({ action: 'something_new', details: {} })).toBe('something_new');
  });
});
