import { describe, expect, it } from 'vitest';
import { dueLine, internalPath, noticeAction, noticeHref, noticesLabel } from './notifications';
import { appealProblem, bandName, bandTone, caseBadge, caseFromHash, evidenceSpans, lineSpans, outcomeWords } from './standing';

describe('notices', () => {
  it('sends case notices to the student case page and keeps other links inside the app', () => {
    expect(noticeHref({ kind: 'case_shared', case_id: 12, link: '/integrity#case-12' })).toBe('/integrity/cases/12');
    expect(noticeHref({ kind: 'reminder', case_id: 12, link: '/integrity#case-12' })).toBe('/integrity/cases/12');
    expect(noticeHref({ kind: 'results_final', case_id: null, link: '/contests/week-6' })).toBe('/contests/week-6');
    expect(noticeHref({ kind: 'standing_adjusted', case_id: null, link: '/integrity' })).toBe('/integrity');
    expect(noticeHref({ kind: 'results_final', link: 'https://evil.example/' })).toBeNull();
    expect(internalPath('//evil.example/x')).toBeNull();
    expect(internalPath('javascript:alert(1)')).toBeNull();
  });

  it('names the action on each kind', () => {
    expect(noticeAction('case_shared')).toBe('Reply on the case page');
    expect(noticeAction('case_decided')).toBe('Read the decision');
    expect(noticeAction('results_final')).toBe('See your result');
  });

  it('says by when, in words', () => {
    const at = new Date(2026, 9, 26, 18, 0).getTime();
    expect(dueLine({ kind: 'case_shared', due_at: at })).toBe('Reply by 26 Oct, 18:00');
    expect(dueLine({ kind: 'case_decided', due_at: at })).toBe('Appeal by 26 Oct, 18:00');
    expect(dueLine({ kind: 'appeal_decided', due_at: null })).toBe('');
    expect(noticesLabel(2)).toBe('Notices, 2 unread');
    expect(noticesLabel(0)).toBe('Notices');
  });
});

describe('standing', () => {
  it('names and colours the bands, never with a number', () => {
    expect(bandName({ band: 'good' })).toBe('Good standing');
    expect(bandName({ band: 'repeated', band_name: '' })).toBe('Repeated outcomes');
    expect(bandName({ band: 'on_record', band_name: 'Outcomes on record' })).toBe('Outcomes on record');
    expect(bandTone('good')).toBe('ok');
    expect(bandTone('on_record')).toBe('part');
  });

  it('words outcomes and case states', () => {
    expect(outcomeWords({ kind: 'zero_problem', problem_label: 'B' })).toBe('Zero for problem B');
    expect(outcomeWords({ kind: 'warning' })).toBe('A warning');
    const base = { state: 'awaiting_student' as const, decided: false, explanation: '', outcome: null, appeal: null };
    expect(caseBadge(base).word).toBe('Waiting for you');
    expect(caseBadge({ ...base, explanation: 'I wrote it.' }).word).toBe('Explanation saved');
    expect(caseBadge({ ...base, state: 'cleared', decided: true }).word).toBe('Cleared');
    expect(caseBadge({ ...base, state: 'actioned', decided: true, outcome: { kind: 'warning', decided_at: 1, reason: 'x', appeal_by: 2 } }).word).toBe('Outcome decided');
    expect(caseBadge({ ...base, state: 'actioned', decided: true, appeal: { state: 'open', created_at: 1 } }).word).toBe('Under appeal');
  });

  it('checks an appeal before sending it', () => {
    expect(appealProblem('   ')).toBe('Write why you think the decision is wrong first.');
    expect(appealProblem('too short')).toMatch(/at least 20/);
    expect(appealProblem('I wrote this myself in the lab on Tuesday.')).toBe('');
  });

  it('turns line ranges into byte spans, multibyte characters included', () => {
    const src = 'int a;\nint é = 1;\nreturn 0;\n';
    // Line 2 starts at byte 7 and is 11 bytes long (é is two bytes).
    expect(lineSpans(src, [{ from: 2, to: 2 }])).toEqual([{ start: 7, end: 18 }]);
    expect(lineSpans(src, [{ from: 1, to: 3 }])).toEqual([{ start: 0, end: 28 }]);
    expect(lineSpans(src, [{ from: 3, to: 1 }, { from: 9, to: 12 }])).toEqual([]);
    expect(evidenceSpans({ kind: 'manual', problem_id: 1, text: '', source: src, lines: [{ from: 1, to: 1 }] })).toEqual([{ start: 0, end: 6 }]);
    expect(evidenceSpans({ kind: 'similar', problem_id: 1, text: '', spans: [{ start: 1, end: 2 }] })).toEqual([{ start: 1, end: 2 }]);
  });

  it('reads the case from a notice address', () => {
    expect(caseFromHash('#case-12')).toBe(12);
    expect(caseFromHash('#main')).toBeNull();
  });
});
