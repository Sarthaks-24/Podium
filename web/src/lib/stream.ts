/**
 * The client side of GET /api/events (m3-plan.md section 6).
 *
 * One EventSource per page. When it fails, it is closed and reopened after
 * a jittered, growing delay (half to all of 1 s, 2 s, 4 s ... capped at
 * 30 s), so a server restart does not bring every student back in the same
 * second. Three failures in a row without a successful open switch to
 * polling every 15 s through the caller's poll(); while polling, the stream
 * is tried again every fourth poll and polling stops once it opens. Each
 * open after a failure calls onReconnect(), so the caller can refetch what
 * it may have missed.
 */

/** The delay before reconnect attempt `attempt` (0 for the first), in ms. */
export function reconnectDelay(attempt: number, random: () => number = Math.random): number {
  const base = Math.min(30_000, 1000 * 2 ** Math.max(0, attempt));
  return Math.round(base / 2 + random() * (base / 2));
}

/** The minimal EventSource the stream needs; tests pass a fake. */
export interface EventSourceLike {
  onopen: ((ev: Event) => void) | null;
  onerror: ((ev: Event) => void) | null;
  addEventListener(type: string, fn: (ev: MessageEvent) => void): void;
  close(): void;
}

export interface StreamOptions {
  url: string;
  /** Event names to listen for. */
  events: readonly string[];
  /** Called with each event's name and parsed data. */
  onEvent: (type: string, data: unknown) => void;
  /** Called when the stream opens again after a failure. */
  onReconnect: () => void;
  /** Called every pollMs while the stream is down. */
  poll: () => void;
  /** Called when the mode changes, for a quiet status line. */
  onMode?: (mode: 'live' | 'polling' | 'connecting') => void;
  pollMs?: number;
  maxErrors?: number;
  random?: () => number;
  create?: (url: string) => EventSourceLike;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (id: unknown) => void;
}

/** A self-healing event stream with a polling fallback. */
export class EventStream {
  private es: EventSourceLike | null = null;
  private errors = 0;
  private hadError = false;
  private retryTimer: unknown = null;
  private pollTimer: unknown = null;
  private polls = 0;
  private stopped = false;
  mode: 'live' | 'polling' | 'connecting' = 'connecting';

  private readonly o: Required<Omit<StreamOptions, 'onMode'>> & Pick<StreamOptions, 'onMode'>;

  constructor(options: StreamOptions) {
    this.o = {
      pollMs: 15_000,
      maxErrors: 3,
      random: Math.random,
      create: (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
      setTimer: (fn, ms) => setTimeout(fn, ms),
      clearTimer: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
      ...options,
    };
  }

  /** Opens the stream. */
  start(): void {
    this.stopped = false;
    this.connect();
  }

  /** Closes the stream and stops every timer. */
  stop(): void {
    this.stopped = true;
    this.es?.close();
    this.es = null;
    if (this.retryTimer !== null) this.o.clearTimer(this.retryTimer);
    if (this.pollTimer !== null) this.o.clearTimer(this.pollTimer);
    this.retryTimer = null;
    this.pollTimer = null;
  }

  private setMode(mode: EventStream['mode']) {
    if (this.mode === mode) return;
    this.mode = mode;
    this.o.onMode?.(mode);
  }

  private connect() {
    if (this.stopped) return;
    this.retryTimer = null;
    let es: EventSourceLike;
    try {
      es = this.o.create(this.o.url);
    } catch {
      this.fail();
      return;
    }
    this.es = es;
    es.onopen = () => {
      if (this.es !== es) return;
      const wasDown = this.hadError;
      this.errors = 0;
      this.hadError = false;
      this.stopPolling();
      this.setMode('live');
      if (wasDown) this.o.onReconnect();
    };
    es.onerror = () => {
      if (this.es !== es) return;
      es.close();
      this.es = null;
      this.fail();
    };
    for (const type of this.o.events) {
      es.addEventListener(type, (ev: MessageEvent) => {
        if (this.es !== es) return;
        let data: unknown;
        try {
          data = JSON.parse(String(ev.data));
        } catch {
          return;
        }
        this.o.onEvent(type, data);
      });
    }
  }

  private fail() {
    if (this.stopped) return;
    this.errors++;
    this.hadError = true;
    if (this.errors >= this.o.maxErrors) {
      this.startPolling();
      return;
    }
    this.setMode('connecting');
    this.retryTimer = this.o.setTimer(() => this.connect(), reconnectDelay(this.errors - 1, this.o.random));
  }

  private startPolling() {
    this.setMode('polling');
    if (this.pollTimer !== null) return;
    const tick = () => {
      if (this.stopped) return;
      this.polls++;
      this.o.poll();
      // Try the stream again now and then; a successful open stops polling.
      if (this.polls % 4 === 0 && this.es === null) this.connect();
      this.pollTimer = this.o.setTimer(tick, this.o.pollMs);
    };
    this.o.poll();
    this.pollTimer = this.o.setTimer(tick, this.o.pollMs);
  }

  private stopPolling() {
    if (this.pollTimer !== null) this.o.clearTimer(this.pollTimer);
    this.pollTimer = null;
    this.polls = 0;
  }
}
