/**
 * Toast notifications, shown bottom-left by components/Toasts.svelte.
 *
 * Info toasts dismiss themselves after TOAST_MS and are spoken through the
 * app's polite announcer (lib/announce.svelte.ts), which is always in
 * the page, so the first toast is read too. Error toasts stay until the
 * person dismisses them, so an error is never missed: the cap never
 * removes one. An info toast said again replaces its copy on screen, so
 * two pastes refused in a row show one line. Info toasts fill what is left of MAX_TOASTS (at least one),
 * a new one replacing the oldest, so a busy console never stacks them over
 * its controls.
 */

import { polite } from './announce.svelte';

export type ToastKind = 'info' | 'error';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

/** How long an info toast stays on screen. */
export const TOAST_MS = 6000;

/** The most toasts on screen at once. */
export const MAX_TOASTS = 3;

class ToastState {
  items = $state<Toast[]>([]);
}

/** The visible toasts, oldest first. */
export const toasts = new ToastState();

let nextId = 1;

/** Removes a toast. */
export function dismissToast(id: number): void {
  toasts.items = toasts.items.filter((t) => t.id !== id);
}

/** Every error toast, and the newest info toasts that fit beside them. */
function capped(items: Toast[]): Toast[] {
  const errors = items.filter((t) => t.kind === 'error').length;
  const keep = new Set(
    items
      .filter((t) => t.kind !== 'error')
      .slice(-Math.max(1, MAX_TOASTS - errors))
      .map((t) => t.id),
  );
  return items.filter((t) => t.kind === 'error' || keep.has(t.id));
}

/** Shows a toast and returns its id. */
export function showToast(message: string, kind: ToastKind = 'info'): number {
  const id = nextId++;
  // The same info sentence again replaces the one on screen, so it shows once.
  const others = kind === 'info' ? toasts.items.filter((t) => t.kind === 'error' || t.message !== message) : toasts.items;
  toasts.items = capped([...others, { id, kind, message }]);
  if (kind === 'info') {
    polite(message);
    setTimeout(() => dismissToast(id), TOAST_MS);
  }
  return id;
}
