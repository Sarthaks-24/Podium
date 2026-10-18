import { describe, expect, it } from 'vitest';
import { IntegrityRecorder, MAX_BATCH } from './integrity';

function recorder() {
  const sent: { url: string; body: { events: { kind: string; size: number; at: number }[] } }[] = [];
  let now = 100;
  const r = new IntegrityRecorder('/api/workspace/1/events', () => now++, (url, body) => sent.push({ url, body: JSON.parse(body) }));
  return { r, sent };
}

describe('integrity recorder', () => {
  it('counts typed and pasted characters and focus losses', () => {
    const { r } = recorder();
    r.typedChars(5);
    r.typedChars(0);
    r.paste(7);
    r.paste(0);
    r.blur();
    r.focus();
    r.blur();
    expect(r.counts()).toEqual({ typed_chars: 5, pasted_chars: 7, focus_losses: 2 });
    r.resetCounts();
    expect(r.counts()).toEqual({ typed_chars: 0, pasted_chars: 0, focus_losses: 0 });
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

  it('never sends more than the batch limit in one request', () => {
    const { r, sent } = recorder();
    for (let i = 0; i < MAX_BATCH * 2 + 5; i++) r.blur();
    r.flush();
    expect(sent.map((s) => s.body.events.length)).toEqual([MAX_BATCH, MAX_BATCH, 5]);
  });
});
