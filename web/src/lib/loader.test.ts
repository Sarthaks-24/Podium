import { describe, expect, it } from 'vitest';
import { ApiError, PAGE_ERROR_MESSAGE } from './api';
import { loader } from './loader.svelte';

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
