// @vitest-environment jsdom
// X6: a jump button's accessible name starts with the location it shows,
// so a voice user saying "main.cpp:3:5" reaches it (WCAG 2.5.3).
import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import CompileOutput from './CompileOutput.svelte';

let app: Record<string, unknown> | null = null;

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = '';
});

describe('CompileOutput', () => {
  it('names each jump button by its visible text first', () => {
    const target = document.createElement('div');
    document.body.append(target);
    const output = "main.cpp:3:5: error: expected ';' before 'return'\n";
    app = mount(CompileOutput, { target, props: { output, language: 'cpp17', onjump: () => {} } });
    flushSync();
    const button = target.querySelector('pre button');
    expect(button?.textContent).toBe('main.cpp:3:5');
    expect(button?.getAttribute('aria-label')).toBe('main.cpp:3:5, go to line 3, column 5 in the editor');
    expect(target.querySelector('pre')?.textContent).toBe(output);
    // P4: the first error and its line come first, once.
    expect(target.querySelector('.first')?.textContent?.replace(/\s+/g, ' ').trim()).toBe("Line 3: expected ';' before 'return'");
  });

  it('shows plain text when there is no editor to jump to', () => {
    const target = document.createElement('div');
    document.body.append(target);
    app = mount(CompileOutput, { target, props: { output: 'Main.java:12: error', language: 'java21', onjump: null } });
    flushSync();
    expect(target.querySelector('button')).toBeNull();
  });
});
