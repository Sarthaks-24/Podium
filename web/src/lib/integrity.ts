/**
 * Integrity counts from the editor: characters typed and pasted, each
 * paste's length, focus lost and regained, and full-screen exits when the
 * contest asks for full screen; never keystroke timing, the text itself,
 * or anything outside this page. Events go out in paced batches with fetch
 * keepalive, so a batch survives the page closing, and sending never
 * blocks a submission; a failed batch is dropped rather than retried,
 * because these are signals for an organiser to look closer, not evidence
 * on their own. The counts since the last submission go with the next
 * submit.
 */

export type IntegrityKind = 'paste' | 'blur' | 'focus' | 'fullscreen_exit';

/** One event as POST .../events takes it; `at` is server Unix ms. */
export interface IntegrityEvent {
  kind: IntegrityKind;
  size: number;
  at: number;
}

/** The counts that go with a submission. */
export interface IntegrityCounts {
  typed_chars: number;
  pasted_chars: number;
  focus_losses: number;
  fullscreen_exits: number;
}

/** How often queued events are sent, in ms. */
export const FLUSH_MS = 30_000;

/** The most events one request may carry. */
export const MAX_BATCH = 100;

/** The least time between two batches; the server refuses closer ones. */
export const MIN_GAP_MS = 5_000;

type Sender = (url: string, body: string) => void;

/** Timers for pacing; tests pass fakes. */
export interface Pacing {
  clock: () => number;
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (id: unknown) => void;
}

const realPacing: Pacing = {
  clock: () => Date.now(),
  setTimer: (fn, ms) => setTimeout(fn, ms),
  clearTimer: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
};

/** Sends a JSON body that should outlive the page. */
export function sendKeepalive(url: string, body: string): void {
  try {
    void fetch(url, {
      method: 'POST',
      keepalive: true,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body,
    }).catch(() => undefined);
  } catch {
    try {
      navigator.sendBeacon?.(url, new Blob([body], { type: 'application/json' }));
    } catch {
      // Dropped: see the module comment.
    }
  }
}

/** Counts and queues integrity events for one workspace. */
export class IntegrityRecorder {
  private typed = 0;
  private pasted = 0;
  private losses = 0;
  private exits = 0;
  private queue: IntegrityEvent[] = [];
  private readonly url: string;
  private readonly now: () => number;
  private readonly send: Sender;
  private readonly pace: Pacing;
  private lastSent = Number.NEGATIVE_INFINITY;
  private timer: unknown = null;

  constructor(url: string, now: () => number, send: Sender = sendKeepalive, pace: Pacing = realPacing) {
    this.url = url;
    this.now = now;
    this.send = send;
    this.pace = pace;
  }

  /** Characters typed in the editor. */
  typedChars(n: number): void {
    if (n > 0) this.typed += n;
  }

  /** A paste into the editor of `n` characters. */
  paste(n: number): void {
    if (n <= 0) return;
    this.pasted += n;
    this.push({ kind: 'paste', size: n, at: this.now() });
  }

  /** The window lost focus. */
  blur(): void {
    this.losses++;
    this.push({ kind: 'blur', size: 0, at: this.now() });
  }

  /** The window got focus back. */
  focus(): void {
    this.push({ kind: 'focus', size: 0, at: this.now() });
  }

  /** The student left full screen. */
  fullscreenExit(): void {
    this.exits++;
    this.push({ kind: 'fullscreen_exit', size: 0, at: this.now() });
  }

  private push(ev: IntegrityEvent) {
    this.queue.push(ev);
    // A burst is sent at once rather than held in memory.
    if (this.queue.length >= MAX_BATCH) this.flush();
  }

  /** Events waiting to be sent. */
  get pending(): readonly IntegrityEvent[] {
    return this.queue;
  }

  /**
   * Sends one batch of what is queued, now or as soon as the gap since the
   * last batch allows; the rest follows one batch per gap. `final` (the
   * page is closing) sends a batch at once.
   */
  flush(final = false): void {
    if (this.queue.length === 0) return;
    const wait = this.lastSent + MIN_GAP_MS - this.pace.clock();
    if (wait > 0 && !final) {
      this.schedule(wait);
      return;
    }
    if (this.timer !== null) this.pace.clearTimer(this.timer);
    this.timer = null;
    const batch = this.queue.splice(0, MAX_BATCH);
    this.lastSent = this.pace.clock();
    this.send(this.url, JSON.stringify({ events: batch }));
    if (this.queue.length > 0 && !final) this.schedule(MIN_GAP_MS);
  }

  private schedule(ms: number) {
    if (this.timer !== null) return;
    this.timer = this.pace.setTimer(() => {
      this.timer = null;
      this.flush();
    }, ms);
  }

  /** Stops a waiting send; call when the workspace closes, after the final flush. */
  stop(): void {
    if (this.timer !== null) this.pace.clearTimer(this.timer);
    this.timer = null;
  }

  /** The counts for a submission. */
  counts(): IntegrityCounts {
    return { typed_chars: this.typed, pasted_chars: this.pasted, focus_losses: this.losses, fullscreen_exits: this.exits };
  }

  /** Starts counting again after a submission was accepted. */
  resetCounts(): void {
    this.typed = 0;
    this.pasted = 0;
    this.losses = 0;
    this.exits = 0;
  }
}
