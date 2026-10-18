/**
 * Integrity counts from the editor (decision D47, m3-plan.md section 7).
 *
 * What is recorded: the number of characters typed and pasted (from
 * CodeMirror's input.type and input.paste transactions), each paste's
 * length, and the window losing and regaining focus. What is not: keystroke
 * timing, the text itself, or anything outside this page.
 *
 * Paste, blur and focus events are queued and sent in batches of at most
 * 100 every 30 s and when a submission is made, with fetch keepalive (or
 * navigator.sendBeacon when fetch is unavailable), so a batch survives the
 * page closing. Sending never blocks or delays a submission, and a failed
 * batch is dropped rather than retried: these are signals for an organiser
 * to look closer, not evidence on their own. The character counts and the
 * number of focus losses since the last submission go with the next submit.
 */

export type IntegrityKind = 'paste' | 'blur' | 'focus';

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
}

/** How often queued events are sent, in ms. */
export const FLUSH_MS = 30_000;

/** The most events one request may carry. */
export const MAX_BATCH = 100;

type Sender = (url: string, body: string) => void;

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
  private queue: IntegrityEvent[] = [];
  private readonly url: string;
  private readonly now: () => number;
  private readonly send: Sender;

  constructor(url: string, now: () => number, send: Sender = sendKeepalive) {
    this.url = url;
    this.now = now;
    this.send = send;
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

  private push(ev: IntegrityEvent) {
    this.queue.push(ev);
    // A burst is sent at once rather than held in memory.
    if (this.queue.length >= MAX_BATCH) this.flush();
  }

  /** Events waiting to be sent. */
  get pending(): readonly IntegrityEvent[] {
    return this.queue;
  }

  /** Sends what is queued, in batches of at most MAX_BATCH. */
  flush(): void {
    while (this.queue.length > 0) {
      const batch = this.queue.splice(0, MAX_BATCH);
      this.send(this.url, JSON.stringify({ events: batch }));
    }
  }

  /** The counts for a submission. */
  counts(): IntegrityCounts {
    return { typed_chars: this.typed, pasted_chars: this.pasted, focus_losses: this.losses };
  }

  /** Starts counting again after a submission was accepted. */
  resetCounts(): void {
    this.typed = 0;
    this.pasted = 0;
    this.losses = 0;
  }
}
