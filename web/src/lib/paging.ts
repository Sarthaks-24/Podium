/** Paging for lists the browser already holds whole (the People tab, V211-21). */

export interface Paged<T> {
  rows: T[];
  /** The page shown, clamped to 1..pages. */
  page: number;
  pages: number;
  /** 1-based position of the first and last row shown; both 0 when empty. */
  first: number;
  last: number;
}

/** One page of `items`, with the page clamped to the valid range. */
export function pageOf<T>(items: readonly T[], page: number, size: number): Paged<T> {
  const pages = Math.max(1, Math.ceil(items.length / size));
  const at = Number.isFinite(page) ? Math.min(Math.max(1, Math.trunc(page)), pages) : 1;
  const start = (at - 1) * size;
  const rows = items.slice(start, start + size);
  return { rows, page: at, pages, first: rows.length === 0 ? 0 : start + 1, last: start + rows.length };
}
