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

/** One button on an info toast, such as Undo (v4 U4). Choosing it closes the toast. */
export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
  action?: ToastAction;
}

/** How long an info toast stays on screen. */
export const TOAST_MS = 6000;

/** How long an info toast with an action stays, so there is time to undo. */
export const ACTION_TOAST_MS = 8000;

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

/** Shows a toast and returns its id. An info toast may carry one action, and then stays ACTION_TOAST_MS. */
export function showToast(message: string, kind: ToastKind = 'info', action?: ToastAction): number {
  const id = nextId++;
  // The same info sentence again replaces the one on screen, so it shows once.
  const others = kind === 'info' ? toasts.items.filter((t) => t.kind === 'error' || t.message !== message) : toasts.items;
  toasts.items = capped([...others, { id, kind, message, ...(kind === 'info' && action ? { action } : {}) }]);
  if (kind === 'info') {
    polite(action ? `${message}. ${action.label} is in the corner for ${ACTION_TOAST_MS / 1000} seconds.` : message);
    setTimeout(() => dismissToast(id), action ? ACTION_TOAST_MS : TOAST_MS);
  }
  return id;
}

/** Chooses a toast's action: the toast closes, then the action runs. */
export function runToastAction(id: number): void {
  const action = toasts.items.find((t) => t.id === id)?.action;
  dismissToast(id);
  action?.run();
}
