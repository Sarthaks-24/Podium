import { describe, expect, it } from 'vitest';
import {
  actorText,
  feedQuery,
  pinPath,
  siteAuditSummary,
  updateEffect,
} from './history';

describe('pins', () => {
  it('names the update and says what it does', () => {
    expect(pinPath(4, 'A')).toBe('/api/admin/contests/4/problems/A/revision');
    expect(updateEffect({ label: 'A', update_statement_only: true })).toBe(
      'Students get the edited problem A the next time they open it. Only the statement changed, so no submission is judged again.',
    );
    expect(updateEffect({ label: 'B', update_statement_only: false })).toMatch(/judge the problem again afterwards/);
  });

});

describe('site audit (People History)', () => {
  const row = (action: string, details: Record<string, unknown>, subject = 'ta@college.edu') => ({ action, details, subject_email: subject });

  it('writes one past-tense sentence per action', () => {
    expect(siteAuditSummary(row('role_granted', { role: 'admin', from_role: 'student' }))).toBe('Made ta@college.edu an organiser');
    expect(siteAuditSummary(row('role_granted', { role: 'owner', from_role: 'admin' }))).toBe('Changed ta@college.edu from organiser to owner');
    expect(siteAuditSummary(row('role_revoked', { from_role: 'admin', sessions_ended: 2 }))).toBe('Removed ta@college.edu as organiser; 2 sessions ended');
    expect(siteAuditSummary(row('domain_added', { domain: 'college.edu' }, ''))).toBe('Allowed @college.edu to sign in');
    expect(siteAuditSummary(row('domain_removed', { domain: 'cs.college.edu', sessions_ended: 0 }, ''))).toBe('Removed @cs.college.edu from the allowed list');
    expect(siteAuditSummary(row('account_disabled', { sessions_ended: 1 }))).toBe('Disabled ta@college.edu; 1 session ended');
    expect(siteAuditSummary(row('account_enabled', {}))).toBe('Enabled ta@college.edu');
    expect(siteAuditSummary(row('sessions_ended', { count: 3 }))).toBe('Ended 3 sessions of ta@college.edu');
    expect(siteAuditSummary(row('google_reset', { sessions_ended: 0 }))).toBe('Reset the Google account link of ta@college.edu');
    expect(siteAuditSummary(row('mode_changed', { from: 'testing', to: 'production', sessions_ended: 4 }, ''))).toBe(
      'The server started in production mode (was testing); 4 sessions ended',
    );
    // A later release may add actions; they show by name.
    expect(siteAuditSummary(row('settings_changed', {}, ''))).toBe('settings_changed');
  });

  it('names the actor, or the server', () => {
    expect(actorText({ user_id: 1, email: 'priya@college.edu', role: 'owner' })).toBe('priya@college.edu (Owner)');
    expect(actorText(null)).toBe('The server');
  });

  it('leaves empty filters out of the query', () => {
    expect(feedQuery({ page: 1, action: '', q: 'ta' })).toBe('?page=1&q=ta');
    expect(feedQuery({ action: '' })).toBe('');
  });
});
