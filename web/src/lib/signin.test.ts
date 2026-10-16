import { describe, expect, it } from 'vitest';
import {
  audienceLine,
  errorMessage,
  formatDomains,
  messageText,
  parseErrorCode,
  safeNext,
  startHref,
  type SignInErrorCode,
} from './signin';

const NONE: string[] = [];
const ONE = ['college.edu'];
const THREE = ['a.edu', 'b.edu', 'c.edu'];
const CONTACT = 'owner@college.edu';

describe('formatDomains', () => {
  it('lists domains with commas and "or"', () => {
    expect(formatDomains(NONE)).toBe('');
    expect(formatDomains(ONE)).toBe('@college.edu');
    expect(formatDomains(['a.edu', 'b.edu'])).toBe('@a.edu or @b.edu');
    expect(formatDomains(THREE)).toBe('@a.edu, @b.edu or @c.edu');
  });
});

describe('audienceLine', () => {
  it('names the domains that may sign in', () => {
    expect(audienceLine(NONE)).toBe('Sign in with your Google account.');
    expect(audienceLine(ONE)).toBe('Sign in with your @college.edu Google account.');
    expect(audienceLine(THREE)).toBe('Sign in with a Google account from @a.edu, @b.edu or @c.edu.');
  });
});

describe('parseErrorCode', () => {
  it('accepts only known codes', () => {
    expect(parseErrorCode('domain')).toBe('domain');
    expect(parseErrorCode('google')).toBe('google');
    expect(parseErrorCode('account')).toBe('account');
    expect(parseErrorCode('server')).toBe('server');
    expect(parseErrorCode('Domain')).toBeNull();
    expect(parseErrorCode('<script>')).toBeNull();
    expect(parseErrorCode(null)).toBeNull();
  });
});

describe('errorMessage', () => {
  const text = (code: SignInErrorCode | null, domains: string[], contact = CONTACT) => {
    const parts = errorMessage(code, domains, contact);
    return parts === null ? null : messageText(parts);
  };

  it('explains a rejected domain for 0, 1 and 3 domains', () => {
    expect(text('domain', NONE)).toBe(
      'This account is not on the allowed list. Ask owner@college.edu to add your email.',
    );
    expect(text('domain', ONE)).toBe(
      'This account is not on the allowed list. Sign in with your @college.edu account, or ask owner@college.edu to add your email.',
    );
    expect(text('domain', THREE)).toBe(
      'This account is not on the allowed list. Sign in with an account from @a.edu, @b.edu or @c.edu, or ask owner@college.edu to add your email.',
    );
  });

  it('renders the contact as a link part', () => {
    expect(errorMessage('domain', ONE, CONTACT)).toContainEqual({ email: CONTACT });
    expect(errorMessage('disabled', ONE, CONTACT)).toContainEqual({ email: CONTACT });
    expect(errorMessage('account', ONE, CONTACT)).toContainEqual({ email: CONTACT });
  });

  it('falls back to "an organiser" without a contact email', () => {
    expect(text('domain', NONE, '')).toBe('This account is not on the allowed list. Ask an organiser to add your email.');
    expect(text('disabled', ONE, '')).toBe('This account has been disabled. Ask an organiser if you think this is a mistake.');
    expect(text('account', ONE, '')).toBe(
      'This email is linked to a different Google account. Ask an organiser to reset the link if your address moved to a new Google account.',
    );
  });

  for (const domains of [NONE, ONE, THREE]) {
    it(`gives the fixed sentences with ${domains.length} domains`, () => {
      expect(text('unverified', domains)).toBe(
        'Google has not verified this email address yet. Verify it with Google, then try again.',
      );
      expect(text('disabled', domains)).toBe(
        'This account has been disabled. Ask owner@college.edu if you think this is a mistake.',
      );
      expect(text('account', domains)).toBe(
        'This email is linked to a different Google account. Ask owner@college.edu to reset the link if your address moved to a new Google account.',
      );
      expect(text('expired', domains)).toBe('That sign-in attempt expired. Start again.');
      expect(text('google', domains)).toBe('Google sign-in did not complete. Try again in a moment.');
      expect(text('server', domains)).toBe(
        'Something went wrong on the server while signing you in. Try again in a minute.',
      );
      expect(text(null, domains)).toBeNull();
    });
  }
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
