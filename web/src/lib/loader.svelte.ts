/**
 * The one way a screen loads what it shows.
 *
 * A Loader runs the screen's request and keeps the page state the template
 * branches on: "loading", "ready", "forbidden" (a 403), "missing" (a 404)
 * or "error" with the sentence to show beside Retry. Only the newest
 * request counts: an answer to an older one, or any answer after the
 * screen has gone (stop(), called on unmount), is dropped, so a slow
 * first response can never overwrite a newer one or touch a page that is
 * no longer there. A 401 changes nothing here, because lib/api.ts has
 * already signed the page out.
 *
 * Usage: `const page = loader(() => getJSON(url), (data) => (thing = data));`
 * then `onMount(() => { void page.load(); return () => page.stop(); })`.
 */
import { ApiError, errorText, isSignedOut } from './api';

/** What a screen shows while it loads, or why it cannot. */
export type LoadState = 'loading' | 'ready' | 'forbidden' | 'missing' | 'error';

export class Loader<T> {
  /** The page state the template branches on. */
  state = $state<LoadState>('loading');
  /** The sentence for the "error" state. */
  error = $state('');

  #fetch: () => Promise<T>;
  #onData: (data: T) => void;
  #seq = 0;
  #stopped = false;

  constructor(fetch: () => Promise<T>, onData: (data: T) => void) {
    this.#fetch = fetch;
    this.#onData = onData;
  }

  /** False once the screen has gone; side requests check it before they write. */
  get live(): boolean {
    return !this.#stopped;
  }

  /** Runs the request; the data reaches `onData` just before the state turns "ready". */
  async load(): Promise<void> {
    const seq = ++this.#seq;
    const current = () => seq === this.#seq && !this.#stopped;
    this.state = 'loading';
    try {
      const data = await this.#fetch();
      if (!current()) return;
      this.#onData(data);
      this.state = 'ready';
    } catch (e) {
      if (!current() || isSignedOut(e)) return;
      if (e instanceof ApiError && e.status === 403) this.state = 'forbidden';
      else if (e instanceof ApiError && e.status === 404) this.state = 'missing';
      else {
        this.error = errorText(e);
        this.state = 'error';
      }
    }
  }

  /** Shows the forbidden state without a request, for a role the page already knows. */
  forbid(): void {
    this.#seq++;
    this.state = 'forbidden';
  }

  /** Drops every answer from now on; call it when the screen unmounts. */
  stop(): void {
    this.#stopped = true;
  }
}

/** A Loader for one screen: `fetch` makes the request, `onData` takes the answer. */
export function loader<T>(fetch: () => Promise<T>, onData: (data: T) => void): Loader<T> {
  return new Loader(fetch, onData);
}
