/** Returns an integer delay from zero through maxMs, inclusive. */
export function jitter(maxMs: number, random: () => number = Math.random): number {
  const maximum = Math.max(0, Math.floor(maxMs));
  const sample = Math.max(0, Math.min(1, random()));
  return Math.min(maximum, Math.floor(sample * (maximum + 1)));
}
