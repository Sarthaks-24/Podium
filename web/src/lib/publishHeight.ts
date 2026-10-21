/**
 * Publishes an element's rendered height as a custom property on the root
 * element while it is mounted, for layouts that must clear it:
 * `use:publishHeight={'--shell-head'}` on the shell header (the workspace
 * fills the rest of the window) and `'--sticky-bar-h'` on a bar that
 * sticks to the top (base.css scroll padding, the problem editor's sticky
 * preview). Wrapped rows and touch-sized controls are included because it
 * measures, and it writes only when the height changes.
 */
export function publishHeight(node: HTMLElement, prop: string) {
  if (typeof ResizeObserver === 'undefined') return;
  const root = document.documentElement;
  let last = -1;
  const ro = new ResizeObserver(() => {
    const height = node.offsetHeight;
    if (height === last) return;
    last = height;
    root.style.setProperty(prop, `${height}px`);
  });
  ro.observe(node);
  return {
    destroy() {
      ro.disconnect();
      root.style.removeProperty(prop);
    },
  };
}
