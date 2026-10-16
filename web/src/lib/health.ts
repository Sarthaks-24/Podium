/**
 * Server health probe used by the home screen.
 *
 * GET /healthz is served by the Go server (proxied by Vite in development).
 * Any 2xx response counts as running; a non-2xx status, a network error or
 * a timeout counts as not reachable.
 */

export type HealthState = 'checking' | 'up' | 'down';

/** How long to wait for /healthz before reporting the server as not reachable. */
export const HEALTH_TIMEOUT_MS = 5000;

/** Asks the server whether it is running. Never throws. */
export async function checkHealth(
  fetcher: typeof fetch = fetch,
  timeoutMs: number = HEALTH_TIMEOUT_MS,
): Promise<Exclude<HealthState, 'checking'>> {
  try {
    const res = await fetcher('/healthz', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      signal: AbortSignal.timeout(timeoutMs),
    });
    return res.ok ? 'up' : 'down';
  } catch {
    return 'down';
  }
}
