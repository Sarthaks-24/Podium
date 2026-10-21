// @vitest-environment jsdom
// MonoDigits inside a flex container (a Badge): the parts are one item, so
// the spaces between words and digits survive.
import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import MonoDigits from './MonoDigits.svelte';

let app: Record<string, unknown> | null = null;

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = '';
});

describe('MonoDigits', () => {
  it('wraps every part in one inline span, so a flex parent sees one item', () => {
    const target = document.createElement('span');
    target.style.display = 'inline-flex';
    document.body.append(target);
    app = mount(MonoDigits, { target, props: { text: 'Partial, 40 of 100' } });
    flushSync();
    expect(target.children).toHaveLength(1);
    const outer = target.children[0];
    expect(outer.className).toBe('mono-digits');
    expect(outer.textContent).toBe('Partial, 40 of 100');
    expect([...outer.querySelectorAll('.mono')].map((m) => m.textContent)).toEqual(['40', '100']);
  });
});
