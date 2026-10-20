import { describe, expect, it } from 'vitest';
import { parseConfig, parseMode, telHref } from './mode.svelte';

describe('site config (C5)', () => {
  it('reads the organisation and the retention periods', () => {
    const c = parseConfig({
      allowed_domains: ['college.edu', 7],
      contact_email: ' podium@college.edu ',
      mode: 'production',
      organisation: { name: 'Example College', contact_email: 'podium@college.edu', phone: '+91 80 1234 5678', privacy_contact: '' },
      retention: [
        { id: 'sessions', data: 'Sign-in sessions', amount: 12, unit: 'hours', after: 'sign_in', kept: '12 hours after sign-in' },
        { id: 'broken', data: '', kept: '' },
        'nonsense',
      ],
    });
    expect(c.allowed_domains).toEqual(['college.edu']);
    expect(c.contact_email).toBe('podium@college.edu');
    expect(c.organisation).toEqual({ name: 'Example College', contact_email: 'podium@college.edu', phone: '+91 80 1234 5678', privacy_contact: '' });
    expect(c.retention.map((r) => r.id)).toEqual(['sessions']);
  });

  it('never invents a value for a field the server left out', () => {
    const c = parseConfig({ mode: 'testing' });
    expect(c.mode).toBe('testing');
    expect(c.organisation).toEqual({ name: '', contact_email: '', phone: '', privacy_contact: '' });
    expect(c.retention).toEqual([]);
    expect(parseConfig(null).contact_email).toBe('');
    expect(parseMode('odd')).toBe('production');
  });

  it('makes a dialable tel: link', () => {
    expect(telHref('+91 80 1234 5678')).toBe('tel:+918012345678');
    expect(telHref('(080) 1234-5678')).toBe('tel:08012345678');
  });
});
