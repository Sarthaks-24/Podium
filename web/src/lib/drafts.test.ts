import { describe, expect, it } from 'vitest';
import {
  DEFAULT_FONT_SIZE,
  forgetDrafts,
  forgetDraftsAtSignOut,
  mayMirror,
  languageMirrorKey,
  mirrorKey,
  noteSignedInUser,
  pickDraft,
  readEditorPrefs,
  readMirror,
  writeEditorPrefs,
  RESULT_SHARE,
  clampShare,
  readResultPanel,
  writeResultPanel,
  writeMirror,
  type DraftStorage,
} from './drafts';
import type { Draft } from './workspace';

function memory(): DraftStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
    key: (i) => [...data.keys()][i] ?? null,
    get length() {
      return data.size;
    },
  };
}

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

const server: Draft = { language: 'cpp17', source: 'server code', updated_at: 1000 };

describe('draft merge', () => {
  it('takes the newer copy', () => {
    const newer: Draft = { language: 'python3', source: 'local code', updated_at: 2000 };
    const older: Draft = { language: 'python3', source: 'local code', updated_at: 500 };
    expect(pickDraft(server, newer)).toEqual({ draft: newer, origin: 'local' });
    expect(pickDraft(server, older)).toEqual({ draft: server, origin: 'server' });
  });

  it('lets the server win a tie and identical code', () => {
    expect(pickDraft(server, { ...server, source: 'other', updated_at: 1000 }).origin).toBe('server');
    expect(pickDraft(server, { ...server, updated_at: 9999 }).origin).toBe('server');
  });

  it('uses whichever exists, or none', () => {
    expect(pickDraft(server, null)).toEqual({ draft: server, origin: 'server' });
    expect(pickDraft(null, server)).toEqual({ draft: server, origin: 'local' });
    expect(pickDraft(null, null)).toEqual({ draft: null, origin: 'none' });
  });
});

describe('local mirror', () => {
  it('round-trips a draft under the user and workspace key', () => {
    const s = memory();
    const key = mirrorKey(7, '/api/workspace/3');
    expect(key).toBe('podium-draft:7:/api/workspace/3');
    expect(writeMirror(s, key, server, 7)).toBe(true);
    expect(JSON.parse(s.data.get(key)!)).toEqual({ ...server, owner: 7 });
    expect(readMirror(s, key, 7)).toEqual(server);
  });

  it('never hands one user the code of another', () => {
    const s = memory();
    // A mirror written for student 7, read back as student 8 under any key.
    writeMirror(s, mirrorKey(7, '/api/workspace/3'), server, 7);
    expect(readMirror(s, mirrorKey(8, '/api/workspace/3'), 8)).toBeNull();
    s.setItem(mirrorKey(8, '/api/workspace/3'), s.data.get(mirrorKey(7, '/api/workspace/3'))!);
    expect(readMirror(s, mirrorKey(8, '/api/workspace/3'), 8)).toBeNull();
    // The M3 shape with no owner is ignored too.
    s.setItem('podium-draft:/api/workspace/3', JSON.stringify(server));
    expect(readMirror(s, 'podium-draft:/api/workspace/3', 8)).toBeNull();
  });

  it('removes every mirror at sign-out and when another account signs in', () => {
    const s = memory();
    s.setItem('podium-editor-font', '16');
    noteSignedInUser(7, s);
    writeMirror(s, mirrorKey(7, '/api/workspace/3'), server, 7);
    writeMirror(s, mirrorKey(7, '/api/contests/w/problems/A'), server, 7);
    s.setItem('podium-draft:/api/workspace/1', JSON.stringify(server));

    noteSignedInUser(7, s);
    expect(readMirror(s, mirrorKey(7, '/api/workspace/3'), 7)).toEqual(server);

    noteSignedInUser(8, s);
    expect([...s.data.keys()].filter((k) => k.startsWith('podium-draft:'))).toEqual([]);
    expect(s.getItem('podium-editor-font')).toBe('16');

    writeMirror(s, mirrorKey(8, '/api/workspace/3'), server, 8);
    expect(mayMirror(8)).toBe(true);
    expect(mayMirror(7)).toBe(false);
    forgetDraftsAtSignOut(s);
    expect([...s.data.keys()]).toEqual(['podium-editor-font']);
    // A workspace closing after sign-out may not write its mirror back.
    expect(mayMirror(8)).toBe(false);
    noteSignedInUser(8, s);
    expect(mayMirror(8)).toBe(true);
    expect(() => forgetDrafts(broken)).not.toThrow();
    expect(() => noteSignedInUser(1, broken)).not.toThrow();
  });

  it('ignores junk and survives storage that throws', () => {
    const s = memory();
    s.setItem('k', '{not json');
    expect(readMirror(s, 'k', 1)).toBeNull();
    s.setItem('k', JSON.stringify({ language: 'cobol', source: '', updated_at: 1, owner: 1 }));
    expect(readMirror(s, 'k', 1)).toBeNull();
    expect(readMirror(broken, 'k', 1)).toBeNull();
    expect(writeMirror(broken, 'k', server, 1)).toBe(false);
    expect(readMirror(null, 'k', 1)).toBeNull();
  });
});

describe('editor settings', () => {
  it('defaults, remembers, and rejects sizes off the list', () => {
    const s = memory();
    expect(readEditorPrefs(s, 7)).toEqual({ fontSize: DEFAULT_FONT_SIZE, tabMovesFocus: false, tabSize: 4, wrap: false, language: null });
    writeEditorPrefs(s, { fontSize: 20, tabMovesFocus: true, tabSize: 2, wrap: true, language: 'java21' }, 7);
    expect(readEditorPrefs(s, 7)).toEqual({ fontSize: 20, tabMovesFocus: true, tabSize: 2, wrap: true, language: 'java21' });
    s.setItem('podium-editor-font', '99');
    expect(readEditorPrefs(s, 7).fontSize).toBe(DEFAULT_FONT_SIZE);
    expect(readEditorPrefs(broken, 7).fontSize).toBe(DEFAULT_FONT_SIZE);
    expect(() => writeEditorPrefs(broken, { fontSize: 12, tabMovesFocus: false, tabSize: 4, wrap: false, language: 'cpp17' }, 7)).not.toThrow();
  });

  it('remembers the language per account, across problems (P5)', () => {
    const s = memory();
    writeEditorPrefs(s, { fontSize: 14, tabMovesFocus: false, tabSize: 4, wrap: false, language: 'python3' }, 7);
    expect(readEditorPrefs(s, 7).language).toBe('python3');
    expect(readEditorPrefs(s, 8).language).toBeNull();
  });

  it('starts word wrap on for a narrow screen until it is set', () => {
    const s = memory();
    expect(readEditorPrefs(s, 7, true).wrap).toBe(true);
    s.setItem('podium-editor-wrap', '0');
    expect(readEditorPrefs(s, 7, true).wrap).toBe(false);
  });

  it('keeps code per language under the mirror prefix, so sign-out removes it', () => {
    const key = languageMirrorKey(7, '/api/workspace/3', 'python3');
    expect(key.startsWith(mirrorKey(7, '/api/workspace/3'))).toBe(true);
    expect(key).not.toBe(languageMirrorKey(7, '/api/workspace/3', 'cpp17'));
  });
});

describe('the result panel size (P5 F7.5)', () => {
  it('keeps the share inside 20 to 80 percent, whole', () => {
    expect(clampShare(10)).toBe(RESULT_SHARE.min);
    expect(clampShare(95)).toBe(RESULT_SHARE.max);
    expect(clampShare(42.6)).toBe(43);
    expect(clampShare(NaN)).toBe(RESULT_SHARE.default);
  });

  it('reads back what was written, and falls back when nothing usable is stored', () => {
    const s = memory();
    expect(readResultPanel(s)).toEqual({ share: 40, collapsed: false });
    expect(readResultPanel(s, 30)).toEqual({ share: 30, collapsed: false });
    writeResultPanel(s, { share: 55, collapsed: true });
    expect(readResultPanel(s, 30)).toEqual({ share: 55, collapsed: true });
    s.data.set('podium-workspace-result', 'not json');
    expect(readResultPanel(s)).toEqual({ share: 40, collapsed: false });
    s.data.set('podium-workspace-result', '{"collapsed":true}');
    expect(readResultPanel(s, 30)).toEqual({ share: 30, collapsed: false });
    expect(readResultPanel(null)).toEqual({ share: 40, collapsed: false });
  });
});
