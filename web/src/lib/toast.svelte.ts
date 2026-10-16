/**
 * Toast notifications, shown bottom-left by components/Toasts.svelte.
 *
 * Info toasts dismiss themselves after TOAST_MS. Error toasts stay until the
 * person dismisses them, so an error is never missed.
 */

export type ToastKind = 'info' | 'error';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

/** How long an info toast stays on screen. */
export const TOAST_MS = 6000;

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
  toasts.items = [...toasts.items, { id, kind, message }];
  if (kind === 'info') setTimeout(() => dismissToast(id), TOAST_MS);
  return id;
}
