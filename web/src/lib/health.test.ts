import { describe, expect, it } from 'vitest';
import { checkHealth } from './health';

function respondWith(status: number): typeof fetch {
  return async () => new Response(null, { status });
}

describe('checkHealth', () => {
  it('reports up for a 2xx response', async () => {
    expect(await checkHealth(respondWith(200))).toBe('up');
  });

  it('reports down for a non-2xx response', async () => {
    expect(await checkHealth(respondWith(502))).toBe('down');
  });

  it('reports down when the request fails', async () => {
    const failing: typeof fetch = async () => {
      throw new TypeError('Failed to fetch');
    };
    expect(await checkHealth(failing)).toBe('down');
  });

  it('requests /healthz', async () => {
    let requested = '';
    const recording: typeof fetch = async (input) => {
      requested = String(input);
      return new Response(null, { status: 200 });
    };
    await checkHealth(recording);
    expect(requested).toBe('/healthz');
  });

  it('reports down when the server does not answer in time', async () => {
    const hanging: typeof fetch = (_input, init) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(init.signal?.reason));
      });
    expect(await checkHealth(hanging, 10)).toBe('down');
  });
});
