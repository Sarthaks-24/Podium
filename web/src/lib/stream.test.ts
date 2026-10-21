import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import { EventStream, deviceId, reconnectDelay, stopAllStreams, withDevice, type EventSourceLike } from './stream';

class FakeSource implements EventSourceLike {
  onopen: ((ev: Event) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  listeners = new Map<string, (ev: MessageEvent) => void>();
  closed = false;
  addEventListener(type: string, fn: (ev: MessageEvent) => void) {
    this.listeners.set(type, fn);
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.(new Event('open'));
  }
  fail() {
    this.onerror?.(new Event('error'));
  }
  send(type: string, data: unknown) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

function harness() {
  const sources: FakeSource[] = [];
  const timers: { fn: () => void; ms: number; id: number }[] = [];
  let nextId = 1;
  const events: [string, unknown][] = [];
  const modes: string[] = [];
  let polls = 0;
  let reconnects = 0;
  const stream = new EventStream({
    url: '/api/events',
    events: ['submission', 'clock'],
    onEvent: (t, d) => events.push([t, d]),
    onReconnect: () => reconnects++,
    poll: () => {
      polls++;
    },
    onMode: (m) => modes.push(m),
    random: () => 0.5,
    create: () => {
      const s = new FakeSource();
      sources.push(s);
      return s;
    },
    setTimer: (fn, ms) => {
      const id = nextId++;
      timers.push({ fn, ms, id });
      return id;
    },
    clearTimer: (id) => {
      const i = timers.findIndex((t) => t.id === id);
      if (i >= 0) timers.splice(i, 1);
    },
  });
  const fire = () => {
    const t = timers.shift();
    t?.fn();
    return t;
  };
  return { stream, sources, timers, events, modes, fire, polls: () => polls, reconnects: () => reconnects };
}

describe('reconnect delay', () => {
  it('grows from 1 s, capped at 30 s, with half of it jittered', () => {
    expect(reconnectDelay(0, () => 0)).toBe(500);
    expect(reconnectDelay(0, () => 1)).toBe(1000);
    expect(reconnectDelay(2, () => 0.5)).toBe(3000);
    expect(reconnectDelay(10, () => 1)).toBe(30_000);
    expect(reconnectDelay(10, () => 0)).toBe(15_000);
  });
});

describe('event stream', () => {
  it('passes parsed events through and ignores junk', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].open();
    h.sources[0].send('submission', { id: 1 });
    h.sources[0].listeners.get('clock')?.({ data: 'not json' } as MessageEvent);
    expect(h.events).toEqual([['submission', { id: 1 }]]);
    expect(h.modes).toEqual(['live']);
  });

  it('reconnects with a jittered delay and refetches after it opens again', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].open();
    h.sources[0].fail();
    expect(h.sources[0].closed).toBe(true);
    expect(h.timers[0].ms).toBe(750);
    h.fire();
    expect(h.sources).toHaveLength(2);
    h.sources[1].open();
    expect(h.reconnects()).toBe(1);
  });

  it('falls back to polling every 15 s after three failures, and stops once the stream opens', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].fail();
    h.fire();
    h.sources[1].fail();
    h.fire();
    h.sources[2].fail();
    expect(h.modes.at(-1)).toBe('polling');
    expect(h.polls()).toBe(1);
    expect(h.timers).toHaveLength(1);
    expect(h.timers[0].ms).toBe(15_000);

    // Polls continue; every fourth one tries the stream again.
    for (let i = 0; i < 4; i++) h.fire();
    expect(h.polls()).toBe(5);
    expect(h.sources).toHaveLength(4);
    h.sources[3].open();
    expect(h.modes.at(-1)).toBe('live');
    expect(h.reconnects()).toBe(1);
    // Only the 30 s stability timer is left.
    expect(h.timers.map((t) => t.ms)).toEqual([30_000]);
  });

  it('keeps the backoff growing when a stream is dropped soon after it opens', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].open();
    h.sources[0].fail();
    expect(h.timers.map((t) => t.ms)).toEqual([750]);
    h.fire();
    h.sources[1].open();
    h.sources[1].fail();
    // Second failure in a row: the delay doubled instead of starting again.
    expect(h.timers.map((t) => t.ms)).toEqual([1500]);
    h.fire();
    h.sources[2].open();
    h.sources[2].fail();
    expect(h.modes.at(-1)).toBe('polling');
  });

  it('resets the backoff once a stream stays open for 30 s', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].open();
    h.sources[0].fail();
    h.fire();
    h.sources[1].open();
    expect(h.timers.map((t) => t.ms)).toEqual([30_000]);
    h.fire();
    h.sources[1].fail();
    expect(h.timers.map((t) => t.ms)).toEqual([750]);
  });

  it('stops for good when replaced by a newer tab, and resumes on request', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].open();
    h.sources[0].send('replaced', {});
    expect(h.sources[0].closed).toBe(true);
    expect(h.modes.at(-1)).toBe('replaced');
    expect(h.timers).toHaveLength(0);
    // A late error from the dropped source changes nothing.
    h.sources[0].fail();
    expect(h.timers).toHaveLength(0);
    expect(h.sources).toHaveLength(1);
    expect(h.polls()).toBe(0);

    h.stream.resume();
    expect(h.sources).toHaveLength(2);
    h.sources[1].open();
    expect(h.modes.at(-1)).toBe('live');
    expect(h.reconnects()).toBe(1);
  });

  it('stops every timer and the source on stop', () => {
    const h = harness();
    h.stream.start();
    h.sources[0].fail();
    h.stream.stop();
    expect(h.timers).toHaveLength(0);
    h.sources[0].open();
    expect(h.modes).not.toContain('live');
  });
});

describe('after sign-out (C1-041)', () => {
  it('stops polling and retrying when a poll answers 401', async () => {
    const timers: { fn: () => void; id: number }[] = [];
    let id = 1;
    let polls = 0;
    const stream = new EventStream({
      url: '/api/events',
      events: [],
      onEvent: () => undefined,
      onReconnect: () => undefined,
      poll: () => {
        polls++;
        return Promise.reject(new ApiError(401, 'signed_out', 'Sign in to continue.'));
      },
      maxErrors: 1,
      random: () => 0.5,
      create: () => {
        throw new Error('no stream');
      },
      setTimer: (fn) => {
        timers.push({ fn, id });
        return id++;
      },
      clearTimer: (t) => {
        const i = timers.findIndex((x) => x.id === t);
        if (i >= 0) timers.splice(i, 1);
      },
    });
    stream.start();
    expect(stream.mode).toBe('polling');
    await Promise.resolve();
    await Promise.resolve();
    expect(polls).toBe(1);
    expect(timers).toHaveLength(0);
  });

  it('stopAllStreams stops every started stream', () => {
    const closed: number[] = [];
    const make = (n: number) =>
      new EventStream({
        url: '/api/events',
        events: [],
        onEvent: () => undefined,
        onReconnect: () => undefined,
        poll: () => undefined,
        create: () => ({ onopen: null, onerror: null, addEventListener: () => undefined, close: () => closed.push(n) }),
        setTimer: () => 0,
        clearTimer: () => undefined,
      });
    const a = make(1);
    const b = make(2);
    a.start();
    b.start();
    stopAllStreams();
    expect(closed.sort()).toEqual([1, 2]);
  });
});

describe('the device id on the event stream (C2-034)', () => {
  function memory() {
    const m = new Map<string, string>();
    return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
  }

  it('makes one id of letters and digits and keeps it', () => {
    const store = memory();
    const id = deviceId(store);
    expect(id).toMatch(/^[A-Za-z0-9]{32}$/);
    expect(deviceId(store)).toBe(id);
  });

  it('replaces a kept id the server would refuse, and gives none without storage', () => {
    const store = memory();
    store.setItem('podium-device', 'not valid!');
    expect(deviceId(store)).toMatch(/^[A-Za-z0-9]{32}$/);
    expect(deviceId(null)).toBe('');
  });

  it('adds it to the event stream only', () => {
    expect(withDevice('/api/events', 'abc123')).toBe('/api/events?device=abc123');
    expect(withDevice('/api/events?x=1', 'abc123')).toBe('/api/events?x=1&device=abc123');
    expect(withDevice('/api/contests', 'abc123')).toBe('/api/contests');
    expect(withDevice('/api/events', '')).toBe('/api/events');
  });
});
