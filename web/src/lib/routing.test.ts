// Contract C2: the route table carries public, title and description for
// every route, and the routes.json fixture the server's tests read
// (server/internal/webui/testdata/routes.json) is exactly what the Vite
// build writes. After changing ROUTES, run `npm run build` and copy
// web/dist/routes.json over the fixture.
import { describe, expect, it } from 'vitest';
import fixture from '../../../server/internal/webui/testdata/routes.json?raw';
import { NOT_FOUND, ROUTES, routeManifest, routeTitle } from './routing';

describe('route table fields (C2)', () => {
  it('gives every route a title and a one-sentence description', () => {
    for (const r of [...ROUTES, NOT_FOUND]) {
      expect(r.title.trim(), r.name).not.toBe('');
      expect(r.description, r.name).toMatch(/^[A-Z].*\.$/);
      expect(r.description.length, r.name).toBeLessThanOrEqual(160);
    }
  });

  it('opens only sign-in and the not-found page to search engines', () => {
    expect(ROUTES.filter((r) => r.public).map((r) => r.name)).toEqual(['signin']);
    expect(NOT_FOUND.public).toBe(true);
    for (const r of ROUTES) if (r.public) expect(r.auth, r.name).toBe(false);
  });

  it('reads titles from the table', () => {
    expect(routeTitle('people')).toBe('People');
    expect(routeTitle('contest-console')).toBe('Console');
    expect(routeTitle('not-found')).toBe('Page not found');
  });

  it('matches the checked-in routes.json fixture', () => {
    expect(fixture).toBe(`${JSON.stringify(routeManifest(), null, 2)}\n`);
  });
});
