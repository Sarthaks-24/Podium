/**
 * Focus helpers. keepHeadingFocus() keeps focus on the page heading when a
 * screen swaps its loading view for the loaded one; focusFirstInvalid()
 * takes focus to the first field with an error.
 *
 * After a client-side navigation the shell moves focus to the new page's
 * h1 (App.svelte). A screen that loads data first shows a loading h1, then
 * replaces it, and focus would fall back to the body. Call
 * keepHeadingFocus() once the data has arrived, just before setting the
 * state that swaps the view, and the returned function right after: when
 * the heading had focus and lost it in the swap, the new heading takes it.
 */
import { tick } from 'svelte';

export function keepHeadingFocus(): () => Promise<void> {
  if (typeof document === 'undefined') return async () => undefined;
  const active = document.activeElement;
  const had = active instanceof HTMLElement && active.matches('main h1');
  return async () => {
    if (!had) return;
    await tick();
    const now = document.activeElement;
    if (now && now !== document.body && now.isConnected) return;
    document.querySelector<HTMLElement>('main h1')?.focus();
  };
}

/**
 * After the next render, focuses the first control marked invalid inside
 * `scope` (a selector for a form, or the element itself, such as a
 * dialog), so an error the server returned moves focus the way a check in
 * the page does. A group marked data-invalid="true" gives focus to its
 * first input.
 */
export async function focusFirstInvalid(scope: string | ParentNode | null | undefined): Promise<void> {
  await tick();
  const root = typeof scope === 'string' ? document.querySelector(scope) : scope;
  root?.querySelector<HTMLElement>(':is([aria-invalid="true"], [data-invalid="true"] input)')?.focus();
}
