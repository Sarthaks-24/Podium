import { describe, expect, it } from 'vitest';
import { NOT_FOUND, ROUTES, routeTitle } from './routing';

describe('route table', () => {
  it('gives every route a title', () => {
    for (const r of [...ROUTES, NOT_FOUND]) expect(r.title.trim(), r.name).not.toBe('');
  });

  it('reads titles from the table', () => {
    expect(routeTitle('people')).toBe('People');
    expect(routeTitle('contest-console')).toBe('Live');
    expect(routeTitle('not-found')).toBe('Page not found');
  });
});
