/**
 * The server's clock as seen from the browser (m3-plan.md section 7a).
 *
 * Every workspace, run and submission response carries server_now (Unix
 * ms), and the event stream sends a clock event every 25 s. The offset is
 * the server's time minus the browser's, measured at the midpoint of the
 * request so half the round trip is accounted for. Samples with a shorter
 * round trip are more trustworthy, so a new sample replaces the current
 * one when its round trip is no worse, or when the current one is stale.
 * M4's contest timer reads serverNow().
 */

/** The offset from one response: server time minus browser time, in ms. */
export function clockOffset(serverNow: number, sentAt: number, receivedAt: number): number {
  return Math.round(serverNow - (sentAt + receivedAt) / 2);
}

/** How long a sample is trusted before any newer one replaces it. */
export const STALE_MS = 60_000;

/** Keeps the best recent estimate of the server's clock. */
export class ServerClock {
  offset = 0;
  /** Round trip of the sample in use; Infinity before the first. */
  rtt = Number.POSITIVE_INFINITY;
  private takenAt = Number.NEGATIVE_INFINITY;
  private readonly now: () => number;

  constructor(now: () => number = () => Date.now()) {
    this.now = now;
  }

  /** Records a response's server_now; returns true when the offset changed. */
  observe(serverNow: number, sentAt: number, receivedAt: number): boolean {
    if (!Number.isFinite(serverNow) || serverNow <= 0) return false;
    const rtt = Math.max(0, receivedAt - sentAt);
    const stale = receivedAt - this.takenAt > STALE_MS;
    if (rtt > this.rtt && !stale) return false;
    this.offset = clockOffset(serverNow, sentAt, receivedAt);
    this.rtt = rtt;
    this.takenAt = receivedAt;
    return true;
  }

  /** The server's time now, in Unix ms. */
  serverNow(): number {
    return this.now() + this.offset;
  }
}

/** The clock shared by the whole app. */
export const serverClock = new ServerClock();
