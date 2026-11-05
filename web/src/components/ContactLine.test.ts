// @vitest-environment jsdom
// The contest contact line: a sentence and a mailto link, or nothing.
import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import ContactLine from './ContactLine.svelte';

let app: Record<string, unknown> | null = null;

function render(props: Record<string, unknown>): HTMLElement {
  const target = document.createElement('div');
  document.body.append(target);
  app = mount(ContactLine, { target, props: props as never });
  flushSync();
  return target;
}

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = '';
});

describe('ContactLine', () => {
  it('names the address as a mailto link', () => {
    const el = render({ email: 'week6@college.edu' });
    expect(el.textContent?.trim()).toBe('Questions about this contest? Email week6@college.edu');
    expect(el.querySelector('a')?.getAttribute('href')).toBe('mailto:week6@college.edu');
  });

  it('renders nothing when the address is empty or missing', () => {
    expect(render({ email: '' }).innerHTML.trim().replace(/<!---->/g, '')).toBe('');
    expect(render({}).querySelector('p')).toBeNull();
  });
});
