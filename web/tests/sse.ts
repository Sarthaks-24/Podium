// A long-lived event stream for mocked Playwright runs.
//
// page.route() can only answer a request with a whole body, so a mocked
// GET /api/events ends as soon as it is sent, and a real EventSource then
// counts that as a dropped stream. lib/stream.ts treats streams dropped
// within 30 s as failures and falls back to polling after three, so the
// mocked runs would end up polling. This replaces window.EventSource with
// one that opens once and stays open: it fetches the (mocked) URL again
// every `intervalMs`, parses each body as server-sent events and passes
// them to its listeners. A failed fetch or a non-2xx answer is reported
// as an error, like a dropped stream, and the source stops.
import type { Page } from '@playwright/test';

export async function longLivedEvents(page: Page, intervalMs = 250): Promise<void> {
  await page.addInitScript((interval: number) => {
    type Listener = (ev: MessageEvent) => void;

    class LoopingEventSource {
      onopen: ((ev: Event) => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      readyState = 0;
      readonly url: string;
      private readonly listeners = new Map<string, Listener[]>();
      private closed = false;

      constructor(url: string) {
        this.url = url;
        setTimeout(() => void this.loop(), 0);
      }

      addEventListener(type: string, fn: Listener) {
        const list = this.listeners.get(type) ?? [];
        list.push(fn);
        this.listeners.set(type, list);
      }

      removeEventListener(type: string, fn: Listener) {
        this.listeners.set(
          type,
          (this.listeners.get(type) ?? []).filter((f) => f !== fn),
        );
      }

      close() {
        this.closed = true;
        this.readyState = 2;
      }

      private dispatch(text: string) {
        for (const block of text.split('\n\n')) {
          let type = 'message';
          const data: string[] = [];
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) type = line.slice(6).trim();
            else if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
          }
          if (data.length === 0) continue;
          const ev = new MessageEvent(type, { data: data.join('\n') });
          for (const fn of this.listeners.get(type) ?? []) {
            if (this.closed) return;
            fn(ev);
          }
        }
      }

      private async loop() {
        let opened = false;
        while (!this.closed) {
          let text: string;
          try {
            const res = await fetch(this.url, { credentials: 'same-origin', headers: { Accept: 'text/event-stream' } });
            if (!res.ok) throw new Error(String(res.status));
            text = await res.text();
          } catch {
            if (!this.closed) {
              this.readyState = 2;
              this.onerror?.(new Event('error'));
            }
            return;
          }
          if (this.closed) return;
          if (!opened) {
            opened = true;
            this.readyState = 1;
            this.onopen?.(new Event('open'));
          }
          this.dispatch(text);
          await new Promise((resolve) => setTimeout(resolve, interval));
        }
      }
    }

    (window as unknown as { EventSource: unknown }).EventSource = LoopingEventSource;
  }, intervalMs);
}
