import { describe, expect, it } from 'vitest';
import { NOT_FOUND, ROUTES, forbiddenText, mainNav, matchRoute, mayOpen, routeTitle } from './routing';

describe('route table', () => {
  it('gives every route a title', () => {
    for (const r of [...ROUTES, NOT_FOUND]) expect(r.title.trim(), r.name).not.toBe('');
  });

  it('reads titles from the table', () => {
    expect(routeTitle('settings')).toBe('Settings');
    expect(routeTitle('contest-console')).toBe('Live');
    expect(routeTitle('not-found')).toBe('Page not found');
  });

  it('puts every organiser screen in a section, and every signed-in screen too', () => {
    for (const r of ROUTES) {
      if (r.role) expect(r.section, r.name).toBeDefined();
      if (r.section === 'student') expect(r.role, r.name).toBeUndefined();
    }
  });
});

describe('who may open a screen', () => {
  const settings = matchRoute('/admin/people').route;
  const bank = matchRoute('/admin/problems').route;
  const contest = matchRoute('/contests/week-6').route;

  it('lets only owners into Settings', () => {
    expect(mayOpen(settings, 'owner')).toBe(true);
    expect(mayOpen(settings, 'organiser')).toBe(false);
    expect(mayOpen(settings, 'student')).toBe(false);
  });

  it('lets organisers of either role into the organiser screens', () => {
    expect(mayOpen(bank, 'organiser')).toBe(true);
    expect(mayOpen(bank, 'owner')).toBe(true);
    expect(mayOpen(bank, 'student')).toBe(false);
    expect(mayOpen(bank, undefined)).toBe(false);
  });

  it('lets anyone into a row without a role', () => {
    expect(mayOpen(contest, 'student')).toBe(true);
    expect(mayOpen(NOT_FOUND, undefined)).toBe(true);
  });

  it('says who may open a closed screen, by its section', () => {
    expect(forbiddenText(settings)).toBe('Only owners can change the settings.');
    expect(forbiddenText(bank)).toBe('Only organisers can manage problems.');
    expect(forbiddenText(matchRoute('/admin/contests/4/console').route)).toBe('Only organisers can manage contests.');
  });
});

describe('main navigation', () => {
  it('names the links by role, Settings for owners only', () => {
    expect(mainNav('student').map((l) => l.label)).toEqual(['Contests']);
    expect(mainNav('organiser').map((l) => l.label)).toEqual(['Contests', 'Problems']);
    expect(mainNav('owner').map((l) => l.label)).toEqual(['Contests', 'Problems', 'Settings']);
  });

  it('links each section to a screen of that section', () => {
    for (const l of mainNav('owner')) expect(matchRoute(l.href).route.section, l.href).toBe(l.section);
    for (const l of mainNav('student')) expect(matchRoute(l.href).route.section, l.href).toBe(l.section);
  });
});
