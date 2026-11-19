import { describe, expect, it } from 'vitest';
import { pageOf } from './paging';

const items = (n: number) => Array.from({ length: n }, (_, i) => i + 1);

describe('pageOf', () => {
  it('returns the first page with its range', () => {
    const p = pageOf(items(120), 1, 50);
    expect(p.rows).toHaveLength(50);
    expect(p.rows[0]).toBe(1);
    expect(p).toMatchObject({ page: 1, pages: 3, first: 1, last: 50 });
  });

  it('returns a short last page', () => {
    const p = pageOf(items(120), 3, 50);
    expect(p.rows).toHaveLength(20);
    expect(p).toMatchObject({ page: 3, pages: 3, first: 101, last: 120 });
  });

  it('clamps a page past the end and below 1', () => {
    expect(pageOf(items(120), 9, 50).page).toBe(3);
    expect(pageOf(items(120), 0, 50).page).toBe(1);
    expect(pageOf(items(120), -4, 50).page).toBe(1);
    expect(pageOf(items(120), Number.NaN, 50).page).toBe(1);
  });

  it('treats an empty list as one empty page', () => {
    expect(pageOf([], 1, 50)).toEqual({ rows: [], page: 1, pages: 1, first: 0, last: 0 });
  });

  it('fits exactly one full page', () => {
    expect(pageOf(items(50), 2, 50)).toMatchObject({ page: 1, pages: 1, first: 1, last: 50 });
  });
});
