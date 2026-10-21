/**
 * One dismissal rule for the header and workspace popovers (account menu,
 * theme menu, editor settings; council C1-033), used as
 * `use:dismissable={{ open, onclose, trigger }}` on the element that holds
 * both the button and its panel:
 *
 * - Escape closes the panel and returns focus to `trigger`, unless a modal
 *   dialog is open (a dialog opened from the panel takes its own Escape)
 *   or something already handled the key.
 * - A pointer press outside the element closes it, focus left where it is.
 * - Focus moving to something outside the element closes it.
 *
 * Presses and focus inside an open dialog never count as outside, so a
 * dialog opened from a panel item does not close the panel under it.
 */

export interface DismissOptions {
  open: boolean;
  onclose: () => void;
  /** The button that opened the panel; Escape gives it focus back. */
  trigger?: HTMLElement | null;
}

function inDialog(node: Node | null): boolean {
  return node instanceof Element && node.closest('dialog') !== null;
}

/** The action: listens on the window and on node while it is mounted. */
export function dismissable(node: HTMLElement, options: DismissOptions) {
  let opts = options;

  function onKeydown(event: KeyboardEvent) {
    if (!opts.open || event.key !== 'Escape' || event.defaultPrevented) return;
    if (document.querySelector('dialog[open]')) return;
    event.preventDefault();
    opts.onclose();
    opts.trigger?.focus();
  }

  function onPointerdown(event: PointerEvent) {
    const target = event.target as Node | null;
    if (opts.open && target && !node.contains(target) && !inDialog(target)) opts.onclose();
  }

  function onFocusout(event: FocusEvent) {
    const next = event.relatedTarget as Node | null;
    if (opts.open && next && !node.contains(next) && !inDialog(next)) opts.onclose();
  }

  window.addEventListener('keydown', onKeydown);
  window.addEventListener('pointerdown', onPointerdown);
  node.addEventListener('focusout', onFocusout);
  return {
    update(next: DismissOptions) {
      opts = next;
    },
    destroy() {
      window.removeEventListener('keydown', onKeydown);
      window.removeEventListener('pointerdown', onPointerdown);
      node.removeEventListener('focusout', onFocusout);
    },
  };
}
