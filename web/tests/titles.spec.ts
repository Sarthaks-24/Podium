// Titles across screens (v4 H4). Each screen sweep already checks its own
// page for exactly one h1 and the exact tab title; this spec reads those
// sweeps' expected titles without a browser and checks that every route in
// the router's table is swept, and that no two routes share a title, so a
// tab or a bookmark always says which page it is.
import { expect, test } from '@playwright/test';
import { matchRoute, ROUTES, type RouteName } from '../src/lib/routing';
import * as ai from './mocks/ai';
import * as contests from './mocks/contests';
import * as integrity from './mocks/integrity';
import * as problems from './mocks/problems';
import * as screens from './mocks/screens';

type Swept = { path: string; title: string | RegExp; organiser: boolean };

const SWEPT: Swept[] = [
  ...screens.SCREENS.map((s) => ({ path: s.path, title: s.title, organiser: s.who === 'owner' })),
  ...problems.SCREENS.map((s) => ({ path: s.path, title: s.title, organiser: true })),
  ...ai.SCREENS.map((s) => ({ path: s.path, title: s.title, organiser: true })),
  ...contests.SCREENS.map((s) => ({ path: s.path, title: s.title, organiser: s.path.startsWith('/admin/') })),
  ...integrity.SCREENS.map((s) => ({ path: s.path, title: s.title, organiser: true })),
  // workspace.spec.ts sweeps the student view of a problem.
  { path: '/admin/problems/1/try', title: 'Try A plus B · podium', organiser: true },
];

/** The route a swept screen ends on: an organiser's home redirects to their contests (App.svelte). */
function routeOf(s: Swept): RouteName {
  const name = matchRoute(new URL(s.path, 'http://x').pathname).route.name;
  return name === 'home' && s.organiser ? 'admin-contests' : name;
}

/** Titles used by more than one route, and empty titles. */
function badTitles(swept: Swept[]): string[] {
  const byTitle = new Map<string, Set<RouteName>>();
  const bad: string[] = [];
  for (const s of swept) {
    if (s.title === '') bad.push(`no title on ${s.path}`);
    // A regex title is checked by its own sweep; only exact titles can be compared.
    if (typeof s.title !== 'string') continue;
    // The live view counts new questions in front of its title: "(1) Live · ...".
    const title = s.title.replace(/^\(\d+\) /, '');
    byTitle.set(title, (byTitle.get(title) ?? new Set()).add(routeOf(s)));
  }
  for (const [title, routes] of byTitle) if (routes.size > 1) bad.push(`"${title}" on ${[...routes].join(', ')}`);
  return bad;
}

test('every route is swept, and no two routes share a tab title', () => {
  expect(badTitles(SWEPT)).toEqual([]);
  const swept = new Set(SWEPT.map(routeOf));
  const missing = [...ROUTES.map((r) => r.name), 'not-found'].filter((n) => !swept.has(n as RouteName));
  expect(missing, 'routes no sweep opens').toEqual([]);
});

test('the check fails on a shared or empty title', () => {
  expect(
    badTitles([
      { path: '/admin/contests', title: 'Contests · podium', organiser: true },
      { path: '/admin/problems', title: 'Contests · podium', organiser: true },
      { path: '/privacy', title: '', organiser: false },
    ]),
  ).toEqual(['no title on /privacy', '"Contests · podium" on admin-contests, problems']);
});
