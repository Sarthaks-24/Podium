import { describe, expect, it } from 'vitest';
import { audienceLine, errorMessage, parseErrorCode, safeNext, startHref, type MessagePart, type SignInErrorCode } from './signin';

/** The message as plain text, the email links as their address. */
const messageText = (parts: readonly MessagePart[]) => parts.map((p) => (typeof p === 'string' ? p : p.email)).join('');

const CONTACT = 'owner@college.edu';

describe('audienceLine', () => {
  it('says the list decides who may sign in (V4-15)', () => {
    expect(audienceLine()).toBe('Sign in with the Google account of the email the organisers added.');
  });
});

describe('parseErrorCode', () => {
  it('accepts only known codes', () => {
    expect(parseErrorCode('not_listed')).toBe('not_listed');
    // A v3 link with ?error=domain still says something true.
    expect(parseErrorCode('domain')).toBe('not_listed');
    expect(parseErrorCode('google')).toBe('google');
    expect(parseErrorCode('account')).toBe('account');
    expect(parseErrorCode('server')).toBe('server');
    expect(parseErrorCode('rate_limited')).toBe('rate_limited');
    expect(messageText(errorMessage('rate_limited', '') ?? [])).toBe(
      'Too many people are signing in from this network at once. Wait a few seconds, then start again.',
    );
    expect(parseErrorCode('Domain')).toBeNull();
    expect(parseErrorCode('<script>')).toBeNull();
    expect(parseErrorCode(null)).toBeNull();
  });
});

describe('errorMessage', () => {
  const text = (code: SignInErrorCode | null, contact = CONTACT) => {
    const parts = errorMessage(code, contact);
    return parts === null ? null : messageText(parts);
  };

  it('says an address is not on the list, and whom to ask (V4-18)', () => {
    expect(text('not_listed')).toBe('Your email is not on the list for this site. Ask owner@college.edu to add it.');
  });

  it('renders the contact as a link part', () => {
    expect(errorMessage('not_listed', CONTACT)).toContainEqual({ email: CONTACT });
    expect(errorMessage('disabled', CONTACT)).toContainEqual({ email: CONTACT });
    expect(errorMessage('account', CONTACT)).toContainEqual({ email: CONTACT });
  });

  it('falls back to "an organiser" without a contact email', () => {
    expect(text('not_listed', '')).toBe('Your email is not on the list for this site. Ask an organiser to add it.');
    expect(text('disabled', '')).toBe('This account has been disabled. Ask an organiser if you think this is a mistake.');
    expect(text('account', '')).toBe(
      'This email is linked to a different Google account. Ask an organiser to reset the link if your address moved to a new Google account.',
    );
  });

  it('gives the fixed sentences', () => {
    expect(text('unverified')).toBe('Google has not verified this email address yet. Verify it with Google, then try again.');
    expect(text('disabled')).toBe('This account has been disabled. Ask owner@college.edu if you think this is a mistake.');
    expect(text('account')).toBe(
      'This email is linked to a different Google account. Ask owner@college.edu to reset the link if your address moved to a new Google account.',
    );
    expect(text('expired')).toBe('That sign-in attempt expired. Start again.');
    expect(text('google')).toBe('Google sign-in did not complete. Try again in a moment.');
    expect(text('server')).toBe('Something went wrong on the server while signing you in. Try again in a minute.');
    expect(text(null)).toBeNull();
  });
});

describe('safeNext and startHref', () => {
  it('keeps local paths', () => {
    expect(safeNext('/admin/people')).toBe('/admin/people');
    expect(safeNext('/?tab=past')).toBe('/?tab=past');
  });

  it('turns anything else into "/"', () => {
    for (const bad of [null, '', 'admin', '//evil.example', '/\\evil.example', 'https://evil.example', '/signin', '/signin?next=/', '/api/auth/logout']) {
      expect(safeNext(bad), String(bad)).toBe('/');
    }
  });

  it('builds the start link with an encoded next', () => {
    expect(startHref('/')).toBe('/api/auth/google/start?next=%2F');
    expect(startHref('/admin/people?x=1&y=2')).toBe('/api/auth/google/start?next=%2Fadmin%2Fpeople%3Fx%3D1%26y%3D2');
    expect(startHref('//evil.example')).toBe('/api/auth/google/start?next=%2F');
  });
});
