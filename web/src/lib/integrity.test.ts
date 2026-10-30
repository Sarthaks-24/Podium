import { describe, expect, it } from 'vitest';
import { IntegrityRecorder, MAX_BATCH, MIN_GAP_MS } from './integrity';

function recorder() {
  const sent: { url: string; body: { events: { kind: string; size: number; at: number }[] } }[] = [];
  let now = 100;
  const clock = { t: 1_000_000 };
  const timers: { fn: () => void; at: number; id: number }[] = [];
  let nextId = 1;
  const r = new IntegrityRecorder('/api/workspace/1/events', () => now++, (url, body) => sent.push({ url, body: JSON.parse(body) }), {
    clock: () => clock.t,
    setTimer: (fn, ms) => {
      timers.push({ fn, at: clock.t + ms, id: nextId });
      return nextId++;
    },
    clearTimer: (id) => {
      const i = timers.findIndex((t) => t.id === id);
      if (i >= 0) timers.splice(i, 1);
    },
  });
  /** Moves the clock on and runs the timers that are due. */
  const advance = (ms: number) => {
    clock.t += ms;
    for (const t of timers.filter((x) => x.at <= clock.t)) {
      timers.splice(timers.indexOf(t), 1);
      t.fn();
    }
  };
  return { r, sent, timers, advance };
}

describe('integrity recorder', () => {
  it('counts typed and pasted characters, focus losses and full-screen exits', () => {
    const { r } = recorder();
    r.typedChars(5);
    r.typedChars(0);
    r.paste(7);
    r.paste(0);
    r.blur();
    r.focus();
    r.blur();
    r.fullscreenExit();
    expect(r.counts()).toEqual({ typed_chars: 5, pasted_chars: 7, focus_losses: 2, fullscreen_exits: 1 });
    expect(r.pending.map((e) => e.kind)).toEqual(['paste', 'blur', 'focus', 'blur', 'fullscreen_exit']);
    r.resetCounts();
    expect(r.counts()).toEqual({ typed_chars: 0, pasted_chars: 0, focus_losses: 0, fullscreen_exits: 0 });
  });

  it('queues paste, blur and focus events with no text and sends them in one batch', () => {
    const { r, sent } = recorder();
    r.typedChars(3);
    r.paste(12);
    r.blur();
    r.focus();
    expect(r.pending.map((e) => e.kind)).toEqual(['paste', 'blur', 'focus']);
    r.flush();
    expect(sent).toHaveLength(1);
    expect(sent[0].url).toBe('/api/workspace/1/events');
    expect(sent[0].body.events).toEqual([
      { kind: 'paste', size: 12, at: 100 },
      { kind: 'blur', size: 0, at: 101 },
      { kind: 'focus', size: 0, at: 102 },
    ]);
    r.flush();
    expect(sent).toHaveLength(1);
  });

  it('never sends more than the batch limit in one request, one batch per gap', () => {
    const { r, sent, advance } = recorder();
    // The 100th event sends a batch at once; the next 100 wait for the gap.
    for (let i = 0; i < MAX_BATCH * 2 + 5; i++) r.blur();
    expect(sent.map((s) => s.body.events.length)).toEqual([MAX_BATCH]);
    r.flush();
    expect(sent).toHaveLength(1);
    advance(MIN_GAP_MS);
    expect(sent.map((s) => s.body.events.length)).toEqual([MAX_BATCH, MAX_BATCH]);
    advance(MIN_GAP_MS);
    expect(sent.map((s) => s.body.events.length)).toEqual([MAX_BATCH, MAX_BATCH, 5]);
  });

  it('keeps 5 s between batches, the server limit, except when the page closes', () => {
    const { r, sent, timers, advance } = recorder();
    r.paste(3);
    r.flush();
    expect(sent).toHaveLength(1);
    // A submit two seconds later flushes again: the batch waits for the gap.
    advance(2000);
    r.blur();
    r.flush();
    r.flush();
    expect(sent).toHaveLength(1);
    expect(timers).toHaveLength(1);
    advance(2999);
    expect(sent).toHaveLength(1);
    advance(1);
    expect(sent).toHaveLength(2);
    // Closing the page sends at once and leaves nothing waiting.
    r.focus();
    r.flush(true);
    expect(sent).toHaveLength(3);
    r.stop();
    expect(timers).toHaveLength(0);
  });
});
