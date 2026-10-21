import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, NETWORK_MESSAGE, UNEXPECTED_MESSAGE, errorText, getJSON, sendBody, sendJSON } from './api';

function respond(status: number, body?: unknown): Response {
  const text = body === undefined ? null : typeof body === 'string' ? body : JSON.stringify(body);
  return new Response(text, { status, headers: { 'Content-Type': 'application/json' } });
}

function fakeFetch(response: Response | Error) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    void input;
    void init;
    if (response instanceof Error) throw response;
    return response;
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

async function caught(promise: Promise<unknown>): Promise<ApiError> {
  try {
    await promise;
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError);
    return err as ApiError;
  }
  throw new Error('expected the request to fail');
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('getJSON', () => {
  it('sends a same-origin JSON request and returns the body', async () => {
    const fn = fakeFetch(respond(200, { id: 1 }));
    await expect(getJSON('/api/me')).resolves.toEqual({ id: 1 });
    const [path, init] = fn.mock.calls[0];
    expect(path).toBe('/api/me');
    expect(init?.method).toBe('GET');
    expect(init?.credentials).toBe('same-origin');
    expect((init?.headers as Record<string, string>).Accept).toBe('application/json');
    expect(init?.body).toBeUndefined();
  });

  it('maps the API error shape to ApiError', async () => {
    fakeFetch(respond(401, { error: { code: 'signed_out', message: 'Sign in to continue.' } }));
    const err = await caught(getJSON('/api/me'));
    expect(err.status).toBe(401);
    expect(err.code).toBe('signed_out');
    expect(err.message).toBe('Sign in to continue.');
  });

  it('maps a network failure to code "network"', async () => {
    fakeFetch(new TypeError('Failed to fetch'));
    const err = await caught(getJSON('/api/me'));
    expect(err.status).toBe(0);
    expect(err.code).toBe('network');
    expect(err.message).toBe(NETWORK_MESSAGE);
  });

  it('reports non-JSON error bodies with a generic sentence', async () => {
    fakeFetch(respond(502, '<html>Bad gateway</html>'));
    const err = await caught(getJSON('/api/me'));
    expect(err.status).toBe(502);
    expect(err.code).toBe('unexpected');
    expect(err.message).toBe(UNEXPECTED_MESSAGE);
  });

  it('reports JSON error bodies without the error shape with a generic sentence', async () => {
    fakeFetch(respond(500, { oops: true }));
    const err = await caught(getJSON('/api/me'));
    expect(err.code).toBe('unexpected');
    expect(err.message).toBe(UNEXPECTED_MESSAGE);
  });

  it('rejects a 200 whose body is not JSON', async () => {
    fakeFetch(respond(200, 'not json'));
    const err = await caught(getJSON('/api/me'));
    expect(err.code).toBe('unexpected');
  });
});

describe('sendJSON', () => {
  it('sends a JSON body with the method', async () => {
    const fn = fakeFetch(respond(201, { domain: 'college.edu', created_at: 1 }));
    await expect(sendJSON('POST', '/api/admin/domains', { domain: 'college.edu' })).resolves.toEqual({
      domain: 'college.edu',
      created_at: 1,
    });
    const [, init] = fn.mock.calls[0];
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe('{"domain":"college.edu"}');
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
  });

  it('sends Content-Type: application/json with every body, whatever the method', async () => {
    for (const method of ['POST', 'PUT', 'PATCH', 'DELETE'] as const) {
      const fn = fakeFetch(new Response(null, { status: 204 }));
      await sendJSON(method, '/api/admin/accounts/reset-google', { email: 'a@college.edu' });
      const [, init] = fn.mock.calls[0];
      expect((init?.headers as Record<string, string>)['Content-Type'], method).toBe('application/json');
      expect(init?.body, method).toBe('{"email":"a@college.edu"}');
    }
  });

  it('returns undefined for 204', async () => {
    fakeFetch(new Response(null, { status: 204 }));
    await expect(sendJSON('DELETE', '/api/admin/people/3')).resolves.toBeUndefined();
  });

  it('omits the body and content type when there is no body', async () => {
    const fn = fakeFetch(new Response(null, { status: 204 }));
    await sendJSON('POST', '/api/auth/logout');
    const [, init] = fn.mock.calls[0];
    expect(init?.body).toBeUndefined();
    expect((init?.headers as Record<string, string>)['Content-Type']).toBeUndefined();
  });

  it('passes conflict messages through as is', async () => {
    fakeFetch(respond(409, { error: { code: 'conflict', message: 'The configured owner cannot be changed here.' } }));
    const err = await caught(sendJSON('DELETE', '/api/admin/people/1'));
    expect(err.status).toBe(409);
    expect(err.code).toBe('conflict');
    expect(err.message).toBe('The configured owner cannot be changed here.');
  });
});

describe('error details', () => {
  it('keeps field errors and problem lists from the error shape', async () => {
    fakeFetch(
      respond(400, {
        error: {
          code: 'bad_request',
          message: 'The pack has 2 problems.',
          fields: { slug: 'That slug is taken.', bad: 3 },
          problems: ['tests/03.out is missing.', 7, 'problem.yaml: unknown key "foo".'],
        },
      }),
    );
    const err = await caught(sendJSON('PUT', '/api/admin/problems/1', {}));
    expect(err.fields).toEqual({ slug: 'That slug is taken.' });
    expect(err.problems).toEqual(['tests/03.out is missing.', 'problem.yaml: unknown key "foo".']);
  });

  it('defaults to no details', async () => {
    fakeFetch(respond(404, { error: { code: 'not_found', message: 'No such problem.' } }));
    const err = await caught(getJSON('/api/admin/problems/9'));
    expect(err.fields).toEqual({});
    expect(err.problems).toEqual([]);
  });
});

describe('sendBody', () => {
  it('sends raw bytes with the given content type', async () => {
    const fn = fakeFetch(respond(201, { id: 4 }));
    const zip = new Blob([new Uint8Array([0x50, 0x4b, 3, 4])], { type: 'application/zip' });
    await expect(sendBody('POST', '/api/admin/problems/import', zip, 'application/zip')).resolves.toEqual({ id: 4 });
    const [path, init] = fn.mock.calls[0];
    expect(path).toBe('/api/admin/problems/import');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe(zip);
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/zip');
    expect((init?.headers as Record<string, string>).Accept).toBe('application/json');
  });
});

describe('errorText', () => {
  it("gives the server's sentence, or the unexpected-response one", () => {
    expect(errorText(new ApiError(409, 'busy', 'A check is already running.'))).toBe('A check is already running.');
    expect(errorText(new Error('boom'))).toBe(UNEXPECTED_MESSAGE);
  });
});
