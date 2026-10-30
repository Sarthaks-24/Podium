/**
 * The client side of GET /api/events. The tab keeps one shared
 * EventSource, opened by the first LiveSubscription and closed when the
 * last one stops; each event goes to the subscribers that named it. A
 * failed stream reopens after a jittered, growing delay capped at 30 s, so
 * a server restart does not bring every student back in the same second,
 * and only counts as healthy after 30 s open; three failures in a row
 * switch to polling every 15 s through the subscribers' poll(). A stream
 * the server replaces for a newer tab stops for good, so open tabs never
 * evict each other in a loop, and every stream stops at sign-out.
 */

import { ApiError } from './api';

// Every started stream, so a sign-out can stop them all.
const started = new Set<EventStream>();

/**
 * Stops every stream in this tab. The shell calls it when the session ends:
 * an EventSource cannot see a 401, so without it a signed-out page would
 * keep polling and retrying the stream.
 */
export function stopAllStreams(): void {
  for (const s of [...started]) s.stop();
}

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

/** live: open; connecting: retrying; polling: fallback; replaced: stopped for a newer tab. */
export type StreamMode = 'live' | 'polling' | 'connecting' | 'replaced';

/** The event the server sends a stream it is about to drop for a newer one. */
export const REPLACED_EVENT = 'replaced';

export interface StreamOptions {
  url: string;
  /** Event names to listen for. */
  events: readonly string[];
  /** Called with each event's name and parsed data. */
  onEvent: (type: string, data: unknown) => void;
  /** Called when the stream opens again after a failure. */
  onReconnect: () => void;
  /** Called every pollMs while the stream is down. A promise that rejects with a 401 stops the stream. */
  poll: () => void | Promise<unknown>;
  /** Called when the mode changes, for a quiet status line. */
  onMode?: (mode: StreamMode) => void;
  pollMs?: number;
  /** How long a stream must stay open before the failure count resets. */
  stableMs?: number;
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
  private stableTimer: unknown = null;
  private polls = 0;
  private stopped = false;
  private readonly events: Set<string>;
  mode: StreamMode = 'connecting';

  private readonly o: Required<Omit<StreamOptions, 'onMode'>> & Pick<StreamOptions, 'onMode'>;

  constructor(options: StreamOptions) {
    this.o = {
      pollMs: 15_000,
      stableMs: 30_000,
      maxErrors: 3,
      random: Math.random,
      create: (url) => new EventSource(withDevice(url), { withCredentials: true }) as unknown as EventSourceLike,
      setTimer: (fn, ms) => setTimeout(fn, ms),
      clearTimer: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
      ...options,
    };
    this.events = new Set(options.events);
  }

  /** True between start() and a stop, a 401 from a poll or a replacement. */
  get running(): boolean {
    return !this.stopped && started.has(this);
  }

  /** Listens for one more event name, on the open source too. */
  listen(type: string): void {
    if (this.events.has(type)) return;
    this.events.add(type);
    if (this.es) this.addListener(this.es, type);
  }

  /** Opens the stream. */
  start(): void {
    this.stopped = false;
    started.add(this);
    this.connect();
  }

  /** Closes the stream and stops every timer. */
  stop(): void {
    this.stopped = true;
    started.delete(this);
    this.es?.close();
    this.es = null;
    this.clearTimers();
  }

  /** Connects again after the stream was replaced by another tab. */
  resume(): void {
    this.stop();
    this.errors = 0;
    this.hadError = true;
    this.stopped = false;
    started.add(this);
    this.setMode('connecting');
    this.connect();
  }

  private clearTimers() {
    for (const t of [this.retryTimer, this.pollTimer, this.stableTimer]) if (t !== null) this.o.clearTimer(t);
    this.retryTimer = null;
    this.pollTimer = null;
    this.stableTimer = null;
  }

  /** The server dropped this stream for a newer one: stop without retrying. */
  private replaced() {
    this.stopped = true;
    started.delete(this);
    this.es?.close();
    this.es = null;
    this.clearTimers();
    this.setMode('replaced');
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
      this.hadError = false;
      this.stopPolling();
      // The failure count resets only once this stream has proved stable.
      if (this.stableTimer !== null) this.o.clearTimer(this.stableTimer);
      this.stableTimer = this.o.setTimer(() => {
        this.stableTimer = null;
        if (this.es === es) this.errors = 0;
      }, this.o.stableMs);
      this.setMode('live');
      if (wasDown) {
        this.o.onReconnect();
      }
    };
    es.onerror = () => {
      if (this.es !== es) return;
      es.close();
      this.es = null;
      if (this.stableTimer !== null) this.o.clearTimer(this.stableTimer);
      this.stableTimer = null;
      this.fail();
    };
    es.addEventListener(REPLACED_EVENT, () => {
      if (this.es === es) this.replaced();
    });
    for (const type of this.events) this.addListener(es, type);
  }

  private addListener(es: EventSourceLike, type: string) {
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
      this.callPoll();
      // Try the stream again now and then; a successful open stops polling.
      if (this.polls % 4 === 0 && this.es === null) this.connect();
      this.pollTimer = this.o.setTimer(tick, this.o.pollMs);
    };
    this.callPoll();
    this.pollTimer = this.o.setTimer(tick, this.o.pollMs);
  }

  /** Runs the caller's poll; a 401 from it means signed out, so the stream stops. */
  private callPoll() {
    const r = this.o.poll();
    if (r instanceof Promise) {
      r.catch((e: unknown) => {
        if (e instanceof ApiError && e.status === 401) this.stop();
        else throw e;
      });
    }
  }

  private stopPolling() {
    if (this.pollTimer !== null) this.o.clearTimer(this.pollTimer);
    this.pollTimer = null;
    this.polls = 0;
  }
}

/** What one page asks of the tab's shared stream. */
export type LiveOptions = Pick<StreamOptions, 'events' | 'onEvent' | 'onReconnect' | 'poll' | 'onMode'>;

const subscribers = new Set<LiveOptions>();
let shared: EventStream | null = null;

/** One page's share of the tab's single stream on GET /api/events. */
export class LiveSubscription {
  /** `make` sets the shared stream's other options when this one opens it (tests). */
  constructor(
    private readonly o: LiveOptions,
    private readonly make: Partial<StreamOptions> = {},
  ) {}

  /** The shared stream's mode. */
  get mode(): StreamMode {
    return shared?.mode ?? 'connecting';
  }

  start(): void {
    subscribers.add(this.o);
    if (!shared?.running) {
      shared = new EventStream({
        url: '/api/events',
        events: [],
        onEvent: (type, data) => {
          for (const s of [...subscribers]) if (s.events.includes(type)) s.onEvent(type, data);
        },
        onReconnect: () => {
          for (const s of [...subscribers]) s.onReconnect();
        },
        poll: () => Promise.all([...subscribers].map((s) => s.poll())),
        onMode: (m) => {
          for (const s of [...subscribers]) s.onMode?.(m);
        },
        ...this.make,
      });
      for (const s of subscribers) for (const t of s.events) shared.listen(t);
      shared.start();
      return;
    }
    for (const t of this.o.events) shared.listen(t);
    this.o.onMode?.(shared.mode);
  }

  /** Leaves the stream; the last subscriber to leave closes it. */
  stop(): void {
    subscribers.delete(this.o);
    if (subscribers.size === 0 && shared) {
      shared.stop();
      shared = null;
    }
  }

  /** Connects the shared stream again after another tab replaced it. */
  resume(): void {
    subscribers.add(this.o);
    if (shared) shared.resume();
    else this.start();
  }
}

const DEVICE_KEY = 'podium-device';
const DEVICE_ID = /^[A-Za-z0-9]{1,64}$/;

/**
 * This browser's random id, kept in localStorage: the server tells
 * two phones of one model apart by it for the second-session signal. Empty
 * when the browser refuses storage; a new id is made when none is kept.
 */
export function deviceId(store: Pick<Storage, 'getItem' | 'setItem'> | null = typeof localStorage === 'undefined' ? null : localStorage): string {
  try {
    const kept = store?.getItem(DEVICE_KEY) ?? '';
    if (DEVICE_ID.test(kept)) return kept;
    const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    const id = Array.from(crypto.getRandomValues(new Uint8Array(32)), (b) => letters[b % letters.length]).join('');
    store?.setItem(DEVICE_KEY, id);
    return store ? id : '';
  } catch {
    return '';
  }
}

/** The event stream's address with `?device=` (only /api/events takes it; docs/API.md). */
export function withDevice(url: string, id = deviceId()): string {
  if (id === '' || !url.startsWith('/api/events')) return url;
  return `${url}${url.includes('?') ? '&' : '?'}device=${id}`;
}
