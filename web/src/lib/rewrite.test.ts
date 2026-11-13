import { describe, expect, it } from 'vitest';
import { browserZone, confirmText, forgetRewrite, MAX_CHANGE, recallRewrite, rememberRewrite, replacedToast, rewriteHref, rewritePath, rewriteRequest } from './rewrite';
import type { DraftStorage } from './drafts';

function memory(): DraftStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
  };
}

const LINK = { problem_id: 3, base: 'abc123', title: 'Two sum' };

describe('the rewrite request', () => {
  it('asks for what should change and sends the problem to rewrite', () => {
    expect(rewriteRequest('  The limits are wrong.  ', 3, 'program', 'array')).toEqual({
      ok: true,
      body: { mode: 'paste', text: 'The limits are wrong.', rewrite_of: 3 },
    });
  });

  it('adds the function choices only for one function', () => {
    expect(rewriteRequest('Fix it.', 3, 'function', 'either')).toEqual({
      ok: true,
      body: { mode: 'paste', text: 'Fix it.', rewrite_of: 3, students_write: 'function', java_lists: 'either' },
    });
  });

  it('refuses an empty box and a box that is too long, with the page sentence', () => {
    expect(rewriteRequest(' \n ', 3, 'program', 'array')).toEqual({ ok: false, fields: { text: 'Say what should change.' } });
    const long = rewriteRequest('a'.repeat(MAX_CHANGE + 1), 3, 'program', 'array');
    expect(long).toEqual({ ok: false, fields: { text: 'Keep what should change under 4,000 characters.' } });
    expect(rewriteRequest('a'.repeat(MAX_CHANGE), 3, 'program', 'array').ok).toBe(true);
  });
});

describe('the confirm dialog', () => {
  it('uses the words of the design with the title and the date', () => {
    const c = confirmText('Two sum', new Date(2026, 10, 11, 9, 30));
    expect(c.title).toBe('Replace Two sum?');
    expect(c.confirm).toBe('Replace Two sum');
    expect(c.body).toBe(
      "Two sum gets this rewrite's statement, tests and solutions. Your own reference solution and brute force are replaced by the AI's, so a contest will ask before taking the problem until you review it. The current version is kept as an archived problem, Two sum (before rewrite, 11 Nov 2026). A contest that already holds Two sum keeps the old version until you press Update students now.",
    );
  });

  it('writes September as the server does', () => {
    expect(confirmText('X', new Date(2026, 8, 3)).body).toContain('X (before rewrite, 3 Sep 2026)');
  });
});

describe('what the page says after Replace', () => {
  it('says validation has started when it did', () => {
    expect(replacedToast('Two sum', { validation_queued: true })).toBe('Replaced Two sum. Validation has started.');
  });

  it('does not claim validation when it was not queued', () => {
    expect(replacedToast('Two sum', { validation_queued: false })).toBe('Replaced Two sum. Validate it before you use it.');
  });
});

describe('the link between a rewrite and the problem it replaces', () => {
  it('is kept per rewrite and read back', () => {
    const s = memory();
    rememberRewrite(s, 12, LINK);
    expect(recallRewrite(s, 12)).toEqual(LINK);
    expect(recallRewrite(s, 13)).toBeNull();
    forgetRewrite(s, 12);
    expect(recallRewrite(s, 12)).toBeNull();
  });

  it('ignores anything that is not a link', () => {
    const s = memory();
    for (const bad of ['{', 'null', '[]', '{"problem_id":"3","base":"a","title":"t"}', '{"problem_id":3,"base":"","title":"t"}', '{"problem_id":0,"base":"a","title":"t"}']) {
      s.data.set('podium-rewrite:5', bad);
      expect(recallRewrite(s, 5), bad).toBeNull();
    }
  });

  it('works without storage and with storage that throws', () => {
    expect(() => rememberRewrite(null, 1, LINK)).not.toThrow();
    expect(recallRewrite(null, 1)).toBeNull();
    expect(() => forgetRewrite(null, 1)).not.toThrow();
    const broken: DraftStorage = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
      removeItem: () => {
        throw new Error('blocked');
      },
    };
    expect(() => rememberRewrite(broken, 1, LINK)).not.toThrow();
    expect(recallRewrite(broken, 1)).toBeNull();
    expect(() => forgetRewrite(broken, 1)).not.toThrow();
  });
});

describe('the zone the server dates the copy in', () => {
  it('is the browser zone, or empty when the browser cannot say', () => {
    expect(browserZone()).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
    const real = Intl.DateTimeFormat;
    Intl.DateTimeFormat = (() => {
      throw new Error('no Intl');
    }) as unknown as typeof Intl.DateTimeFormat;
    try {
      expect(browserZone()).toBe('');
    } finally {
      Intl.DateTimeFormat = real;
    }
  });
});

describe('addresses', () => {
  it('opens the wizard for one problem and posts the replace to it', () => {
    expect(rewriteHref(7)).toBe('/admin/ai/new?rewrite=7');
    expect(rewritePath(7)).toBe('/api/admin/problems/7/replace-with-rewrite');
  });
});
