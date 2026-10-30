// @vitest-environment jsdom
// The five page states, rendered.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createRawSnippet, flushSync, mount, unmount } from 'svelte';
import PageState from './PageState.svelte';
import { runMode } from '../lib/mode.svelte';

let app: Record<string, unknown> | null = null;

function render(props: Record<string, unknown>): HTMLElement {
  const target = document.createElement('div');
  document.body.append(target);
  app = mount(PageState, { target, props: props as never });
  flushSync();
  return target;
}

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = '';
  runMode.contactEmail = '';
  vi.useRealTimers();
});

describe('PageState', () => {
  it('shows the loading line only after 300 ms, in a status region from the start', () => {
    vi.useFakeTimers();
    const el = render({ kind: 'loading', message: 'Loading the contest' });
    const status = el.querySelector('[role="status"]');
    expect(status?.textContent).toBe('');
    vi.advanceTimersByTime(300);
    flushSync();
    expect(status?.textContent).toBe('Loading the contest');
  });

  it('says what went wrong and offers to try again', () => {
    const retry = vi.fn();
    const el = render({ kind: 'error', message: 'Podium could not reach the server.', onretry: retry });
    expect(el.querySelector('[role="alert"]')?.textContent).toBe('Podium could not reach the server.');
    const button = el.querySelector('button');
    expect(button?.textContent).toBe('Try again');
    button?.click();
    expect(retry).toHaveBeenCalledOnce();
  });

  it('names the contact email as a mailto link when a screen is closed', () => {
    runMode.contactEmail = 'priya@college.edu';
    const el = render({ kind: 'forbidden', message: 'Only organisers can manage contests.' });
    const link = el.querySelector('a');
    expect(link?.getAttribute('href')).toBe('mailto:priya@college.edu');
    expect(el.textContent?.replace(/\s+/g, ' ').trim()).toBe('Only organisers can manage contests. Write to priya@college.edu if you need access.');
  });

  it('falls back to the ask sentence without a contact email', () => {
    const el = render({ kind: 'forbidden', message: 'Only organisers can manage contests.' });
    expect(el.querySelector('a')).toBeNull();
    expect(el.textContent?.replace(/\s+/g, ' ').trim()).toBe('Only organisers can manage contests. Ask an owner if you need access.');
  });

  it('links a missing thing to its parent', () => {
    const el = render({ kind: 'missing', message: 'There is no contest with that number.', parent: { href: '/admin/contests', label: 'Go back to the list.' } });
    expect(el.querySelector('a')?.getAttribute('href')).toBe('/admin/contests');
    expect(el.textContent?.trim()).toBe('There is no contest with that number. Go back to the list.');
  });

  it('shows the one next action when there is nothing yet', () => {
    const action = createRawSnippet(() => ({ render: () => '<button type="button">New contest</button>' }));
    const el = render({ kind: 'empty', message: 'No contests yet.', action });
    expect(el.querySelector('p')?.textContent).toBe('No contests yet.');
    expect(el.querySelector('.next button')?.textContent).toBe('New contest');
  });
});
