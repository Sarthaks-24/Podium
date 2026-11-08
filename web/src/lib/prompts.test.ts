import { describe, expect, it } from 'vitest';
import { counter, lastChanged, sourceLine } from './prompts';
import { SITE_ACTIONS, siteAuditSummary, type SiteAuditEntry } from './history';

const v = { id: 4, kind: 'preferences' as const, body: 'x', is_reset: false, default_sha: '', by: 'priya@college.edu', at: 1792183192359 };

describe('prompt helpers', () => {
  it('counts characters, not bytes', () => {
    expect(counter('é😀a', 2000)).toBe('3 of 2,000 characters');
  });
  it('names who changed it and when', () => {
    expect(lastChanged(undefined)).toBe('Never changed.');
    expect(lastChanged(v)).toMatch(/^Last changed by priya@college.edu on /);
    expect(lastChanged({ ...v, is_reset: true })).toMatch(/^Reset by /);
  });
  it('words the source', () => {
    expect(sourceLine({ text: '', source: 'built-in' })).toBe('The built-in text.');
    expect(sourceLine({ text: '', source: 'file', file_sha: 'abcdef0123456789' })).toContain('abcdef012345');
    expect(sourceLine({ text: '', source: 'database', version: v })).toContain('version 4 by priya@college.edu');
  });
  it('summarises the audit rows without any text', () => {
    const row = (action: string, details: Record<string, unknown>) => ({ id: 1, at: 0, action, actor: null, subject_email: '', details }) as SiteAuditEntry;
    expect(siteAuditSummary(row('prompt_saved', { kind: 'wizard_rules', version_id: 3, sha: 'x' }))).toBe('Saved the wizard rules (version 3)');
    expect(siteAuditSummary(row('prompt_reset', { kind: 'preferences', version_id: 5 }))).toBe('Reset the preferences (version 5)');
    expect(siteAuditSummary(row('prompt_restored', { kind: 'pack_rules', version_id: 6 }))).toBe('Restored the outside-AI rules (version 6)');
    expect(SITE_ACTIONS.filter((a) => a.id.startsWith('prompt_')).length).toBe(3);
  });
});
