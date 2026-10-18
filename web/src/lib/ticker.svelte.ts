/**
 * One shared once-a-second tick on the server's clock, for countdowns.
 *
 * `ticker.now` is serverClock.serverNow() refreshed every second while at
 * least one screen uses it; useTicker() starts it and returns the function
 * that stops using it, so a page without a countdown runs no timer.
 */
import { serverClock } from './clock';

class Ticker {
  /** The server's time at the last tick, in Unix ms. */
  now = $state(serverClock.serverNow());
}

/** Shared tick state. */
export const ticker = new Ticker();

let timer: ReturnType<typeof setInterval> | null = null;
let users = 0;

/** Brings `ticker.now` up to date at once, for example after a new server_now. */
export function refreshTicker(): void {
  ticker.now = serverClock.serverNow();
}

/** Starts ticking for one user; returns the function that stops. */
export function useTicker(): () => void {
  users++;
  refreshTicker();
  if (timer === null) timer = setInterval(refreshTicker, 1000);
  let done = false;
  return () => {
    if (done) return;
    done = true;
    users--;
    if (users === 0 && timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  };
}
