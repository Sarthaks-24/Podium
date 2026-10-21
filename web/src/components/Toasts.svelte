<!--
  Toasts: the stack of notifications in the bottom-left corner. It renders
  the shared state in lib/toast.svelte.ts. Info toasts are spoken by the
  app's polite announcer (lib/toast.svelte.ts calls it), so their stack is
  not a live region of its own; each
  error toast has role="alert" and a Dismiss button, because errors do not
  go away on their own, and sits outside the polite region so it is
  announced once. Errors stay however many there are; info toasts fill the
  rest of three (lib/toast.svelte.ts).
  The stack lets clicks through to the page; only an error toast, which has
  a button, takes them. Toasts appear at --dur with a short rise and leave
  with a fade at --dur-fast (design.md 7), both without motion when reduced
  motion is requested. Takes no props.
-->
<script lang="ts">
  import type { TransitionConfig } from 'svelte/transition';
  import { dismissToast, toasts } from '../lib/toast.svelte';

  /** The exit: an opacity fade at --dur-fast, none under reduced motion. */
  function leave(node: HTMLElement): TransitionConfig {
    const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
    const ms = Number.parseFloat(getComputedStyle(node).getPropertyValue('--dur-fast'));
    return { duration: reduced || !Number.isFinite(ms) ? 0 : ms, css: (t) => `opacity: ${t}` };
  }
</script>

<div class="toasts">
  <div class="stack">
    {#each toasts.items.filter((t) => t.kind === 'error') as item (item.id)}
      <div class="toast toast-error" role="alert" out:leave>
        <p>{item.message}</p>
        <button type="button" class="btn btn-quiet btn-compact dismiss" onclick={() => dismissToast(item.id)}>
          Dismiss
        </button>
      </div>
    {/each}
  </div>
  <div class="stack">
    {#each toasts.items.filter((t) => t.kind !== 'error') as item (item.id)}
      <div class="toast" out:leave>
        <p>{item.message}</p>
      </div>
    {/each}
  </div>
</div>

<style>
  .toasts {
    position: fixed;
    left: var(--gutter);
    bottom: var(--space-4);
    z-index: var(--z-toast);
    display: grid;
    gap: var(--space-2);
    width: min(24rem, calc(100vw - 2 * var(--gutter)));
    pointer-events: none;
  }

  .stack {
    display: grid;
    gap: var(--space-2);
  }

  .stack:empty {
    display: none;
  }

  .toast {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    min-height: var(--space-7);
    padding: var(--space-2) var(--space-4);
    border: var(--rule) solid var(--ink);
    border-radius: var(--radius);
    background: var(--ink);
    color: var(--paper);
    box-shadow: var(--shadow-menu);
    pointer-events: none;
    animation: toast-in var(--dur) var(--ease);
  }

  .toast-error {
    pointer-events: auto;
    border-color: var(--bad);
    background: var(--bad-tint);
    color: var(--bad);
  }

  .dismiss {
    flex: none;
    color: inherit;
  }

  /* Hover is a shape, not a fill, so the text keeps its 4.5:1 (7:1 in
     the high-contrast themes) on the toast's own tint (X5). */
  .dismiss:hover {
    border-color: var(--bad);
    background: transparent;
  }

  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateY(var(--space-2));
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .toast {
      animation: none;
    }
  }
</style>
