import { describe, expect, it } from 'vitest';
import { NOT_FOUND, internalHref, matchRoute, type ClickLike, type RouteDef } from './routing';

describe('matchRoute', () => {
  it('matches the known screens', () => {
    expect(matchRoute('/').route.name).toBe('home');
    expect(matchRoute('/signin').route.name).toBe('signin');
    expect(matchRoute('/admin/people').route.name).toBe('people');
    expect(matchRoute('/admin/problems').route.name).toBe('problems');
    expect(matchRoute('/admin/problems/12')).toEqual({ route: expect.objectContaining({ name: 'problem' }), params: { id: '12' } });
  });

  it('matches the contest screens', () => {
    expect(matchRoute('/contests/week-6')).toEqual({ route: expect.objectContaining({ name: 'contest' }), params: { slug: 'week-6' } });
    expect(matchRoute('/contests/week-6/standings').route.name).toBe('standings');
    expect(matchRoute('/contests/week-6/problems/B').params).toEqual({ slug: 'week-6', label: 'B' });
    expect(matchRoute('/admin/contests').route.name).toBe('admin-contests');
    expect(matchRoute('/admin/contests/4').route.name).toBe('admin-contest');
    expect(matchRoute('/admin/contests/4/console').route.name).toBe('contest-console');
    expect(matchRoute('/admin/contests/4/results').route.name).toBe('contest-results');
    expect(matchRoute('/contests/week-6/standings').route.auth).toBe(true);
  });

  it('matches the integrity screens', () => {
    expect(matchRoute('/admin/contests/4/integrity')).toEqual({ route: expect.objectContaining({ name: 'contest-integrity' }), params: { id: '4' } });
    expect(matchRoute('/admin/contests/4/integrity/signals/31').params).toEqual({ id: '4', signal: '31' });
    expect(matchRoute('/admin/integrity/cases/3')).toEqual({ route: expect.objectContaining({ name: 'integrity-case' }), params: { case: '3' } });
    expect(matchRoute('/integrity').route).toMatchObject({ name: 'my-integrity', auth: true });
  });

  it('ignores trailing slashes', () => {
    expect(matchRoute('/signin/').route.name).toBe('signin');
    expect(matchRoute('/admin/people/').route.name).toBe('people');
  });

  it('returns not-found for unknown or partial paths', () => {
    expect(matchRoute('/nope').route).toBe(NOT_FOUND);
    expect(matchRoute('/admin').route).toBe(NOT_FOUND);
    expect(matchRoute('/admin/people/7').route).toBe(NOT_FOUND);
    expect(matchRoute('/SignIn').route).toBe(NOT_FOUND);
  });

  it('marks which screens need sign-in', () => {
    expect(matchRoute('/').route.auth).toBe(true);
    expect(matchRoute('/admin/people').route.auth).toBe(true);
    expect(matchRoute('/signin').route.auth).toBe(false);
    expect(matchRoute('/missing').route.auth).toBe(false);
  });

  it('captures and decodes parameters', () => {
    const table: RouteDef[] = [{ pattern: '/contests/:id/problems/:letter', name: 'home', auth: true }];
    expect(matchRoute('/contests/12/problems/B', table).params).toEqual({ id: '12', letter: 'B' });
    expect(matchRoute('/contests/a%20b/problems/C', table).params).toEqual({ id: 'a b', letter: 'C' });
    expect(matchRoute('/contests/%E0%A4/problems/C', table).route).toBe(NOT_FOUND);
  });
});

describe('internalHref', () => {
  const origin = 'http://localhost:5173';
  const plain: ClickLike = {
    button: 0,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
  };
  const link = (href: string | null, extra: { target?: string; download?: boolean } = {}) => ({
    href,
    target: extra.target ?? '',
    download: extra.download ?? false,
  });

  it('intercepts plain same-origin links', () => {
    expect(internalHref(plain, link('/admin/people'), origin)).toBe('/admin/people');
    expect(internalHref(plain, link('/signin?next=%2F'), origin)).toBe('/signin?next=%2F');
    expect(internalHref(plain, link(`${origin}/`), origin)).toBe('/');
    expect(internalHref(plain, link('/', { target: '_self' }), origin)).toBe('/');
  });

  it('leaves modified and non-primary clicks to the browser', () => {
    for (const key of ['metaKey', 'ctrlKey', 'shiftKey', 'altKey'] as const) {
      expect(internalHref({ ...plain, [key]: true }, link('/'), origin), key).toBeNull();
    }
    expect(internalHref({ ...plain, button: 1 }, link('/'), origin)).toBeNull();
    expect(internalHref({ ...plain, defaultPrevented: true }, link('/'), origin)).toBeNull();
  });

  it('leaves targets, downloads and missing hrefs to the browser', () => {
    expect(internalHref(plain, link('/', { target: '_blank' }), origin)).toBeNull();
    expect(internalHref(plain, link('/report.csv', { download: true }), origin)).toBeNull();
    expect(internalHref(plain, link(null), origin)).toBeNull();
  });

  it('never intercepts server paths', () => {
    expect(internalHref(plain, link('/api/auth/google/start?next=%2F'), origin)).toBeNull();
    expect(internalHref(plain, link('/api'), origin)).toBeNull();
    expect(internalHref(plain, link('/healthz'), origin)).toBeNull();
  });

  it('never intercepts other origins or schemes', () => {
    expect(internalHref(plain, link('https://accounts.google.com/'), origin)).toBeNull();
    expect(internalHref(plain, link('//evil.example/'), origin)).toBeNull();
    expect(internalHref(plain, link('mailto:owner@college.edu'), origin)).toBeNull();
  });

  it('lets hash jumps on the current page scroll natively', () => {
    const here = { path: '/admin/people', search: '' };
    expect(internalHref(plain, link('#main'), origin, here)).toBeNull();
    expect(internalHref(plain, link('/#main'), origin, here)).toBe('/#main');
  });
});
