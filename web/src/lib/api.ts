/**
 * JSON helpers for the podium API (docs/API.md).
 *
 * Every request is same-origin with the session cookie, asks for JSON and
 * sends JSON. A non-2xx response throws ApiError built from the API's error
 * shape, {"error":{"code","message"}}; the message is a sentence meant for
 * people and is shown as is. A network failure throws ApiError with code
 * "network". A 204 response resolves to undefined.
 */

/** An API failure with the server's machine code and human sentence. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

/** Shown when the request never reached the server. */
export const NETWORK_MESSAGE = 'Could not reach the server. Check your connection and try again.';

/** Shown when the server answered with something that is not the API's JSON. */
export const UNEXPECTED_MESSAGE = 'The server sent an unexpected response. Try again in a moment.';

function errorFromBody(status: number, body: unknown): ApiError {
  if (typeof body === 'object' && body !== null && 'error' in body) {
    const err = (body as { error: unknown }).error;
    if (typeof err === 'object' && err !== null) {
      const { code, message } = err as { code?: unknown; message?: unknown };
      if (typeof code === 'string' && typeof message === 'string' && message !== '') {
        return new ApiError(status, code, message);
      }
    }
  }
  return new ApiError(status, 'unexpected', UNEXPECTED_MESSAGE);
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const init: RequestInit = { method, headers, credentials: 'same-origin', cache: 'no-store' };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(path, init);
  } catch {
    throw new ApiError(0, 'network', NETWORK_MESSAGE);
  }

  if (res.status === 204) return undefined as T;

  let text: string;
  try {
    text = await res.text();
  } catch {
    throw new ApiError(0, 'network', NETWORK_MESSAGE);
  }

  let parsed: unknown = undefined;
  if (text !== '') {
    try {
      parsed = JSON.parse(text);
    } catch {
      throw new ApiError(res.status, 'unexpected', UNEXPECTED_MESSAGE);
    }
  }

  if (!res.ok) throw errorFromBody(res.status, parsed);
  return parsed as T;
}

/** GET a JSON resource. */
export function getJSON<T>(path: string): Promise<T> {
  return request<T>('GET', path);
}

/** Send a request with an optional JSON body and read the JSON answer. */
export function sendJSON<T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown): Promise<T> {
  return request<T>(method, path, body);
}
