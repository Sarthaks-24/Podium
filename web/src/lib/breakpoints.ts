/**
 * Breakpoints, the one source (v2 F8). Three width tiers and one height
 * tier; tokens.css repeats the table in a comment, and guardrails.test.ts
 * fails on a media query or matchMedia call that uses any other width.
 *
 * | Tier   | From    | Layout                                           |
 * |--------|---------|--------------------------------------------------|
 * | phone  | 0       | one column, the workspace in three tabs          |
 * | tablet | 768 px  | wider gutters, the workspace in two tabs         |
 * | laptop | 1024 px | the three-column workspace, side by side tables  |
 * | wide   | 1440 px | the widest measures                              |
 * | short  | laptop and at most 45rem tall: the workspace keeps the editor tall (P5) |
 *
 * CSS writes a tier as (min-width: 768px) for "from the tier up" or as the
 * exact complement (width < 768px) for "below it", never max-width: 767px,
 * which leaves a gap at fractional widths on zoomed screens (D126).
 */

/** The width tiers, in CSS pixels. */
export const TIERS = { tablet: 768, laptop: 1024, wide: 1440 } as const;

/** The media queries for each tier, for window.matchMedia. */
export const QUERIES = {
  tablet: `(min-width: ${TIERS.tablet}px)`,
  laptop: `(min-width: ${TIERS.laptop}px)`,
  wide: `(min-width: ${TIERS.wide}px)`,
  short: `(min-width: ${TIERS.laptop}px) and (max-height: 45rem)`,
} as const;

export type Query = keyof typeof QUERIES;

/** The MediaQueryList for a tier, to read .matches and listen for changes. */
export function media(query: Query): MediaQueryList {
  return window.matchMedia(QUERIES[query]);
}
