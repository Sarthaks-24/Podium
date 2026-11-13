import { describe, expect, it } from 'vitest';
import { bringBackPath, broughtBackToast, confirmText, historyPath, NOT_KEPT, versionDay, versionTime } from './versions';

// 3 Nov 2026 14:20 in the machine's zone, so the expected text holds anywhere.
const AT = new Date(2026, 10, 3, 14, 20).getTime();

describe('version words', () => {
  it('dates a version to the minute and to the day', () => {
    expect(versionTime(AT)).toBe('3 Nov 2026, 14:20');
    expect(versionDay(AT)).toBe('3 Nov 2026');
  });

  it('asks in the design words and says what stays', () => {
    const words = confirmText(AT);
    expect(words.title).toBe('Bring back the version from 3 Nov 2026, 14:20?');
    expect(words.confirm).toBe('Bring back this version');
    expect(words.body).toContain('Tags, difficulty and the test plan stay as they are now.');
    expect(words.body).toContain('keep their version until you press Update students now.');
  });

  it('says whether validation started', () => {
    expect(broughtBackToast(AT, { validation_queued: true })).toBe('Brought back the version from 3 Nov 2026. Validation has started.');
    expect(broughtBackToast(AT, { validation_queued: false })).toBe('Brought back the version from 3 Nov 2026. Validate it before you use it.');
  });

  it('names the routes and the line for a version that was not kept', () => {
    expect(historyPath(4)).toBe('/api/admin/problems/4/revisions');
    expect(bringBackPath(4, 9)).toBe('/api/admin/problems/4/revisions/9/bring-back');
    expect(NOT_KEPT).toBe("This version's files were not kept, so it cannot be brought back.");
  });
});
