// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import {
  anyKept,
  keepHelp,
  KEEP_IGNORED_NOTICE,
  peopleSignedOut,
  savedToast,
  showsSharedWarning,
  signOutConfirm,
  takeKeepNotice,
  validDays,
  type KeepPolicy,
  type KeepRole,
} from './keep';

const NOBODY: KeepPolicy = { students: 'nobody', organisers: 'nobody', admins: 'nobody', owner: 'nobody', days: 30, until_sign_out: false };
const EVERYONE: KeepPolicy = { ...NOBODY, students: 'everyone', organisers: 'everyone', admins: 'everyone', owner: 'everyone' };
const COUNTS: Record<KeepRole, number> = { students: 40, organisers: 1, admins: 0, owner: 1 };

describe('help lines', () => {
  it('say the idle period, or until sign-out', () => {
    expect(keepHelp(30, false)).toBe('Stay signed in on this device for 30 days after you last use Podium.');
    expect(keepHelp(1, false)).toBe('Stay signed in on this device for 1 day after you last use Podium.');
    expect(keepHelp(30, true)).toBe('Stay signed in on this device until you sign out.');
  });

  it('keeps the notice words the plan fixes', () => {
    expect(KEEP_IGNORED_NOTICE).toBe(
      'Podium does not keep your account signed in, so the tick was not used. You are signed out when the browser closes or after 12 hours.',
    );
  });
});

describe('validDays', () => {
  it('accepts whole numbers from 1 to 365 only', () => {
    for (const ok of ['1', '30', '365', ' 7 ']) expect(validDays(ok), ok).toBe(true);
    for (const bad of ['', '0', '366', '-3', '1.5', '1e2', 'abc', '3000', '0030x']) expect(validDays(bad), bad).toBe(false);
  });
});

describe('what the setting needs', () => {
  it('knows when a role is kept and when the shared-computer banner shows', () => {
    expect(anyKept(NOBODY)).toBe(false);
    expect(anyKept({ ...NOBODY, admins: 'choose' })).toBe(true);
    expect(showsSharedWarning(NOBODY)).toBe(false);
    expect(showsSharedWarning({ ...NOBODY, students: 'choose' })).toBe(false);
    expect(showsSharedWarning({ ...NOBODY, students: 'everyone' })).toBe(true);
    expect(showsSharedWarning({ ...NOBODY, students: 'choose', until_sign_out: true })).toBe(true);
    // "Only when they sign out" with nobody kept is not used.
    expect(showsSharedWarning({ ...NOBODY, until_sign_out: true })).toBe(false);
  });
});

describe('the confirm before a switch to Nobody', () => {
  it('counts only the roles that become Nobody', () => {
    expect(peopleSignedOut(EVERYONE, { ...EVERYONE, students: 'nobody' }, COUNTS)).toBe(40);
    expect(peopleSignedOut(EVERYONE, NOBODY, COUNTS)).toBe(42);
    // A role that was already Nobody has nobody to sign out, whatever the count says.
    expect(peopleSignedOut({ ...EVERYONE, students: 'nobody' }, NOBODY, COUNTS)).toBe(2);
    expect(peopleSignedOut(NOBODY, EVERYONE, COUNTS)).toBe(0);
  });

  it('asks with the plan words, and names the owner when their own session goes', () => {
    expect(signOutConfirm(EVERYONE, NOBODY, COUNTS, true)).toEqual({
      title: 'Sign out 42 people?',
      body: '42 people kept signed in will be signed out now, you included. They can sign in again.',
      label: 'Sign out 42 people',
    });
    // The owner's role stays on Everyone: they stay signed in.
    expect(signOutConfirm(EVERYONE, { ...EVERYONE, students: 'nobody' }, COUNTS, true)?.body).toBe(
      '40 people kept signed in will be signed out now. They can sign in again. You stay signed in.',
    );
    expect(signOutConfirm(EVERYONE, NOBODY, COUNTS, false)?.body).toContain('You stay signed in.');
  });

  it('says person for one, and asks nothing when nobody would be signed out', () => {
    expect(signOutConfirm(EVERYONE, { ...EVERYONE, owner: 'nobody' }, { ...COUNTS, owner: 1 }, false)?.title).toBe('Sign out 1 person?');
    expect(signOutConfirm(EVERYONE, { ...EVERYONE, admins: 'nobody' }, COUNTS, false)).toBeNull();
    expect(signOutConfirm(NOBODY, EVERYONE, COUNTS, true)).toBeNull();
  });

  it('words the toast after a save', () => {
    expect(savedToast(0)).toBe('Saved');
    expect(savedToast(1)).toBe('Saved. 1 person was signed out.');
    expect(savedToast(42)).toBe('Saved. 42 people were signed out.');
  });
});

describe('the one-time notice cookie', () => {
  afterEach(() => {
    document.cookie = 'podium_notice=; Path=/; Max-Age=0';
  });

  it('is read once and then gone', () => {
    expect(takeKeepNotice()).toBe(false);
    document.cookie = 'podium_notice=keep_ignored; Path=/';
    expect(takeKeepNotice()).toBe(true);
    expect(takeKeepNotice()).toBe(false);
  });

  it('ignores another value', () => {
    document.cookie = 'podium_notice=something_else; Path=/';
    expect(takeKeepNotice()).toBe(false);
  });
});
