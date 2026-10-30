// The icon set is the list the app uses, and every icon has drawable shapes on the 24 x 24 grid.
import { describe, expect, it } from 'vitest';
import { ICONS } from './icons';

describe('icon set', () => {
  it('has the icons the workspace buttons use', () => {
    for (const name of ['play', 'send', 'pause', 'menu', 'plus', 'circle-check', 'circle-x', 'circle-alert', 'chevron-right']) {
      expect(Object.keys(ICONS), name).toContain(name);
    }
  });

  it('lists exactly these icons', () => {
    expect(Object.keys(ICONS).sort()).toEqual(
      [
        'alert-triangle', 'bell', 'check', 'chevron-down', 'chevron-left', 'chevron-right', 'chevron-up', 'circle-alert', 'circle-check',
        'circle-x', 'clock', 'copy', 'download', 'lock', 'maximize', 'menu', 'message-square', 'moon', 'pause', 'play',
        'plus', 'refresh-cw', 'send', 'sun', 'trash', 'upload', 'x',
      ].sort(),
    );
  });

  it('draws every icon with at least one shape', () => {
    for (const [name, shapes] of Object.entries(ICONS)) {
      expect(shapes.length, name).toBeGreaterThan(0);
      for (const s of shapes) if (s.tag === 'path') expect(s.d, name).toMatch(/^[Mm]/);
    }
  });
});
