/**
 * Toast notifications, shown bottom-left by components/Toasts.svelte.
 *
 * Info toasts dismiss themselves after TOAST_MS and are spoken through the
 * app's polite announcer (lib/announce.svelte.ts, X1), which is always in
 * the page, so the first toast is read too. Error toasts stay until the
 * person dismisses them, so an error is never missed. At most MAX_TOASTS
 * show at once: a new toast replaces the oldest, so a busy console never
 * stacks them over its controls.
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

/** Shows a toast and returns its id. */
export function showToast(message: string, kind: ToastKind = 'info'): number {
  const id = nextId++;
  toasts.items = [...toasts.items, { id, kind, message }].slice(-MAX_TOASTS);
  if (kind === 'info') {
    polite(message);
    setTimeout(() => dismissToast(id), TOAST_MS);
  }
  return id;
}
