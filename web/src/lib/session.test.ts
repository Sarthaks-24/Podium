import { afterEach, describe, expect, it, vi } from 'vitest';
import { beforeSignOut, displayName, loadSession, runSignOutSaves, session, signOut, type User } from './session.svelte';
import { NETWORK_MESSAGE } from './api';
import { preferences } from './preferences.svelte';

const student: User = {
  id: 2,
  email: 'asha@college.edu',
  name: 'Asha Rao',
  avatar_url: '',
  role: 'student',
  theme: 'hc-dark',
  verdicts: 'signal',
};

function stubFetch(handler: (path: string, init?: RequestInit) => Response | Promise<Response>) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => handler(String(input), init));
  vi.stubGlobal('fetch', fn);
  return fn;
}

function fakeStorage() {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
    key: (i: number) => [...data.keys()][i] ?? null,
    get length() {
      return data.size;
    },
  };
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

afterEach(() => {
  vi.unstubAllGlobals();
  session.status = 'loading';
  session.user = undefined;
});

describe('loadSession', () => {
  it('starts in the loading state', () => {
    expect(session.status).toBe('loading');
    expect(session.user).toBeUndefined();
  });

  it('is signed in when /api/me answers 200', async () => {
    const fn = stubFetch(() => json(200, student));
    await expect(loadSession()).resolves.toBeNull();
    expect(fn.mock.calls[0][0]).toBe('/api/me');
    expect(session.status).toBe('signed-in');
    expect(session.user).toEqual(student);
  });

  it('removes the mirrors of another account when a different user signs in', async () => {
    const store = fakeStorage();
    vi.stubGlobal('localStorage', store);
    store.setItem('podium-last-user', '9');
    store.setItem('podium-draft:9:/api/workspace/1', '{"source":"secret"}');
    stubFetch(() => json(200, student));
    await loadSession();
    expect(store.getItem('podium-draft:9:/api/workspace/1')).toBeNull();
    expect(store.getItem('podium-last-user')).toBe('2');
  });

  it("adopts the account's display preferences", async () => {
    stubFetch(() => json(200, student));
    await loadSession();
    expect(preferences.current).toEqual({ theme: 'hc-dark', verdicts: 'signal' });
  });

  it('treats an unknown or empty theme as the system theme', async () => {
    stubFetch(() => json(200, { ...student, theme: 'retired-theme', verdicts: 'standard' }));
    await loadSession();
    expect(preferences.current).toEqual({ theme: 'system', verdicts: 'standard' });
  });

  it('is signed out without an error when /api/me answers 401', async () => {
    stubFetch(() => json(401, { error: { code: 'signed_out', message: 'Sign in to continue.' } }));
    await expect(loadSession()).resolves.toBeNull();
    expect(session.status).toBe('signed-out');
    expect(session.user).toBeUndefined();
  });

  it('is signed out and returns the error when the server cannot answer', async () => {
    stubFetch(() => {
      throw new TypeError('Failed to fetch');
    });
    const err = await loadSession();
    expect(err?.code).toBe('network');
    expect(err?.message).toBe(NETWORK_MESSAGE);
    expect(session.status).toBe('signed-out');
  });

  it('returns server errors other than 401', async () => {
    stubFetch(() => json(500, { error: { code: 'internal', message: 'Something broke on the server.' } }));
    const err = await loadSession();
    expect(err?.status).toBe(500);
    expect(session.status).toBe('signed-out');
  });
});

describe('signOut', () => {
  it('posts to /api/auth/logout and clears the session', async () => {
    session.status = 'signed-in';
    session.user = student;
    const fn = stubFetch(() => new Response(null, { status: 204 }));
    await expect(signOut()).resolves.toBeNull();
    expect(fn.mock.calls[0][0]).toBe('/api/auth/logout');
    expect(fn.mock.calls[0][1]?.method).toBe('POST');
    expect(session.status).toBe('signed-out');
    expect(session.user).toBeUndefined();
  });

  it('returns the display to the defaults for the next person', async () => {
    session.status = 'signed-in';
    session.user = student;
    preferences.current = { theme: 'hc-dark', verdicts: 'signal' };
    stubFetch(() => new Response(null, { status: 204 }));
    await signOut();
    expect(preferences.current).toEqual({ theme: 'system', verdicts: 'standard' });
  });

  it('removes every local draft mirror, so the next person never sees them', async () => {
    const store = fakeStorage();
    vi.stubGlobal('localStorage', store);
    store.setItem('podium-draft:2:/api/workspace/1', '{"source":"secret"}');
    store.setItem('podium-editor-font', '16');
    session.status = 'signed-in';
    session.user = student;
    stubFetch(() => new Response(null, { status: 204 }));
    await signOut();
    expect(store.getItem('podium-draft:2:/api/workspace/1')).toBeNull();
    expect(store.getItem('podium-editor-font')).toBe('16');
  });

  it('clears the session even when the request fails', async () => {
    session.status = 'signed-in';
    session.user = student;
    stubFetch(() => {
      throw new TypeError('Failed to fetch');
    });
    const err = await signOut();
    expect(err?.code).toBe('network');
    expect(session.status).toBe('signed-out');
  });
});

describe('beforeSignOut', () => {
  it('runs registered saves before the logout request', async () => {
    session.status = 'signed-in';
    session.user = student;
    const order: string[] = [];
    const remove = beforeSignOut(async () => {
      order.push('save');
    });
    stubFetch((path) => {
      order.push(path);
      return new Response(null, { status: 204 });
    });
    await signOut();
    remove();
    expect(order).toEqual(['save', '/api/auth/logout']);
  });

  it('never waits longer than the limit, and a failing save does not block', async () => {
    const removeSlow = beforeSignOut(() => new Promise(() => undefined));
    const removeBad = beforeSignOut(async () => {
      throw new Error('offline');
    });
    const started = Date.now();
    await runSignOutSaves(50);
    removeSlow();
    removeBad();
    expect(Date.now() - started).toBeLessThan(1000);
  });

  it('forgets a save once removed', async () => {
    const save = vi.fn(async () => undefined);
    beforeSignOut(save)();
    await runSignOutSaves(50);
    expect(save).not.toHaveBeenCalled();
  });
});

describe('displayName', () => {
  it('falls back to the email when the name is empty', () => {
    expect(displayName(student)).toBe('Asha Rao');
    expect(displayName({ name: '  ', email: 'ta@college.edu' })).toBe('ta@college.edu');
  });
});
