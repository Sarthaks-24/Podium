import { describe, expect, it } from 'vitest';
import { ApiError, PAGE_ERROR_MESSAGE } from './api';
import { forgetAnswers, loader } from './loader.svelte';

/** A request the test answers by hand. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('loader', () => {
  it('hands the data over, then turns ready', async () => {
    const got: string[] = [];
    const page = loader(
      () => Promise.resolve('week 6'),
      (d) => got.push(d),
    );
    expect(page.state).toBe('loading');
    await page.load();
    expect(got).toEqual(['week 6']);
    expect(page.state).toBe('ready');
  });

  it('keeps only the newest answer when an older one comes back last', async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const queue = [first, second];
    const got: string[] = [];
    const page = loader(
      () => queue.shift()!.promise,
      (d) => got.push(d),
    );
    const a = page.load();
    const b = page.load();
    second.resolve('new');
    await b;
    first.resolve('old');
    await a;
    expect(got).toEqual(['new']);
    expect(page.state).toBe('ready');
  });

  it('ignores a failure of an older request', async () => {
    const first = deferred<string>();
    const queue = [first.promise, Promise.resolve('new')];
    const page = loader(
      () => queue.shift()!,
      () => {},
    );
    const a = page.load();
    await page.load();
    first.reject(new ApiError(500, 'internal', 'The server failed.'));
    await a;
    expect(page.state).toBe('ready');
  });

  it('ignores answers after the screen has gone', async () => {
    const req = deferred<string>();
    const got: string[] = [];
    const page = loader(
      () => req.promise,
      (d) => got.push(d),
    );
    const done = page.load();
    page.stop();
    expect(page.live).toBe(false);
    req.resolve('late');
    await done;
    expect(got).toEqual([]);
    expect(page.state).toBe('loading');
  });

  it('maps 403 and 404 to page states and keeps the sentence of anything else', async () => {
    const fail = (e: unknown) =>
      loader(
        () => Promise.reject(e),
        () => {},
      );
    const forbidden = fail(new ApiError(403, 'forbidden', 'No.'));
    await forbidden.load();
    expect(forbidden.state).toBe('forbidden');
    expect(forbidden.error).toBe('No.');
    expect(forbidden.code).toBe('forbidden');
    const missing = fail(new ApiError(404, 'not_found', 'There is no contest with that number.'));
    await missing.load();
    expect(missing.state).toBe('missing');
    const broken = fail(new ApiError(500, 'internal', 'The server failed. Try again.'));
    await broken.load();
    expect(broken.state).toBe('error');
    expect(broken.error).toBe('The server failed. Try again.');
    const fault = fail(new TypeError('x is undefined'));
    await fault.load();
    expect(fault.error).toBe(PAGE_ERROR_MESSAGE);
  });

  it('leaves a 401 to the shell, which has signed the page out', async () => {
    const page = loader(
      () => Promise.reject(new ApiError(401, 'unauthenticated', 'Sign in again.')),
      () => {},
    );
    await page.load();
    expect(page.state).toBe('loading');
  });

  it('shows forbidden without a request, and a late answer cannot undo it', async () => {
    const req = deferred<string>();
    const page = loader(
      () => req.promise,
      () => {},
    );
    const done = page.load();
    page.forbid();
    req.resolve('late');
    await done;
    expect(page.state).toBe('forbidden');
  });

  it('keeps a ready page on screen while a quiet refetch runs and when it fails', async () => {
    const req = deferred<string>();
    const queue = [Promise.resolve('first'), req.promise];
    const got: string[] = [];
    const page = loader(
      () => queue.shift()!,
      (d) => got.push(d),
    );
    await page.load();
    const done = page.load({ quiet: true });
    expect(page.state).toBe('ready');
    req.reject(new ApiError(500, 'internal', 'The server failed.'));
    await done;
    expect(page.state).toBe('ready');
    expect(got).toEqual(['first']);
  });
});

describe('kept answers (v4 U2)', () => {
  /** A screen that answers from `replies` in turn, each a value or an error to throw. */
  function screen(key: string, replies: unknown[]) {
    const got: unknown[] = [];
    const page = loader(
      () => {
        const r = replies.shift();
        return r instanceof Error ? Promise.reject(r) : Promise.resolve(r);
      },
      (d) => got.push(d),
      () => key,
    );
    return { page, got };
  }

  it('shows the kept answer at once on a revisit, then the fresh one', async () => {
    forgetAnswers();
    await screen('/a', ['first']).page.load();
    const again = screen('/a', ['second']);
    const loading = again.page.load();
    expect(again.got).toEqual(['first']);
    expect(again.page.state).toBe('ready');
    await loading;
    expect(again.got).toEqual(['first', 'second']);
    expect(again.page.stale).toBe(false);
  });

  it('keeps the old answer and turns stale when the refresh fails, until a load works', async () => {
    forgetAnswers();
    await screen('/b', ['old']).page.load();
    const again = screen('/b', [new ApiError(0, 'network', 'offline'), 'new']);
    await again.page.load();
    expect(again.got).toEqual(['old']);
    expect(again.page.state).toBe('ready');
    expect(again.page.stale).toBe(true);
    await again.page.load();
    expect(again.page.stale).toBe(false);
    expect(again.got.at(-1)).toBe('new');
  });

  it('drops the kept answer on a 403 or 404 and shows the refusal', async () => {
    forgetAnswers();
    await screen('/c', ['mine']).page.load();
    const again = screen('/c', [new ApiError(403, 'forbidden', 'Not yours.')]);
    await again.page.load();
    expect(again.page.state).toBe('forbidden');
    expect(again.page.stale).toBe(false);
    const third = screen('/c', ['later']);
    const loading = third.page.load();
    expect(third.page.state).toBe('loading');
    await loading;
  });

  it('forgets every kept answer at sign out, and a screen without a key keeps nothing', async () => {
    forgetAnswers();
    await screen('/d', ['before']).page.load();
    forgetAnswers();
    const after = screen('/d', ['after']);
    const loading = after.page.load();
    expect(after.got).toEqual([]);
    expect(after.page.state).toBe('loading');
    await loading;
    const plain = loader(() => Promise.resolve('x'), () => {});
    await plain.load();
    const again = loader(() => Promise.resolve('y'), () => {});
    const p = again.load();
    expect(again.state).toBe('loading');
    await p;
  });
});
