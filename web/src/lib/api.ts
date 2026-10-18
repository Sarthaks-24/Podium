/**
 * JSON helpers for the podium API (docs/API.md).
 *
 * Every request is same-origin with the session cookie and asks for JSON.
 * Bodies are JSON, except with sendBody(), which sends raw bytes (a .zip
 * pack) with the given content type. A non-2xx response throws ApiError
 * built from the API's error shape, {"error":{"code","message"}}; the
 * message is a sentence meant for people and is shown as is. The error may
 * also carry "fields" (a request field name to a sentence about it) and
 * "problems" (a list of sentences, for example everything wrong with an
 * imported pack), and "retry_after_s" (seconds to wait, on a 429 from a
 * rate limit). A network failure throws ApiError with code "network". A
 * 204 response resolves to undefined.
 */

/** An API failure with the server's machine code and human sentence. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  /** Sentences about single request fields, keyed by the field's name. */
  readonly fields: Readonly<Record<string, string>>;
  /** Further sentences that belong with the message, in order. */
  readonly problems: readonly string[];
  /** Seconds until a refused request may be tried again, or null. */
  readonly retryAfter: number | null;

  constructor(
    status: number,
    code: string,
    message: string,
    details: { fields?: Record<string, string>; problems?: string[]; retryAfter?: number | null } = {},
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = details.fields ?? {};
    this.problems = details.problems ?? [];
    this.retryAfter = details.retryAfter ?? null;
  }
}

/** Shown when the request never reached the server. */
export const NETWORK_MESSAGE = 'Could not reach the server. Check your connection and try again.';

/** Shown when the server answered with something that is not the API's JSON. */
export const UNEXPECTED_MESSAGE = 'The server sent an unexpected response. Try again in a moment.';

function stringMap(value: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return out;
  for (const [k, v] of Object.entries(value)) if (typeof v === 'string' && v !== '') out[k] = v;
  return out;
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && v !== '') : [];
}

function errorFromBody(status: number, body: unknown): ApiError {
  if (typeof body === 'object' && body !== null && 'error' in body) {
    const err = (body as { error: unknown }).error;
    if (typeof err === 'object' && err !== null) {
      const { code, message, fields, problems, retry_after_s: retry } = err as Record<string, unknown>;
      if (typeof code === 'string' && typeof message === 'string' && message !== '') {
        return new ApiError(status, code, message, {
          fields: stringMap(fields),
          problems: stringList(problems),
          retryAfter: typeof retry === 'number' && Number.isFinite(retry) && retry >= 0 ? retry : null,
        });
      }
    }
  }
  return new ApiError(status, 'unexpected', UNEXPECTED_MESSAGE);
}

type Body = { json: unknown } | { raw: Blob; type: string };

async function request<T>(method: string, path: string, body?: Body): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const init: RequestInit = { method, headers, credentials: 'same-origin', cache: 'no-store' };
  if (body !== undefined && 'raw' in body) {
    headers['Content-Type'] = body.type;
    init.body = body.raw;
  } else if (body !== undefined && body.json !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body.json);
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
  return request<T>(method, path, { json: body });
}

/**
 * Send raw bytes, such as a .zip file picked in a file input, as the request
 * body with the given content type, and read the JSON answer.
 */
export function sendBody<T>(method: 'POST' | 'PUT', path: string, body: Blob, contentType: string): Promise<T> {
  return request<T>(method, path, { raw: body, type: contentType });
}
