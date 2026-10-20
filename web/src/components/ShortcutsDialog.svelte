<!--
  ShortcutsDialog: the "Keyboard shortcuts" list (lib/shortcuts.ts), in a
  native modal dialog. The shell opens it when "?" is pressed outside a
  text field, or from the footer button. Escape and Close shut it, and the
  browser returns focus to whatever had it before. Focus starts on the
  heading, so a screen reader reads the title first. Organisers also see
  the problem editor and reveal keys. Props: `open`, `organiser` and
  `onclose`.
-->
<script lang="ts">
  import { isMacPlatform, shortcutGroups } from '../lib/shortcuts';
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    organiser: boolean;
    onclose: () => void;
  }

  let { open, organiser, onclose }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();

  const groups = $derived(shortcutGroups({ mac: isMacPlatform(), organiser }));

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      heading?.focus();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancel(event: Event) {
    event.preventDefault();
    onclose();
  }
</script>

<dialog bind:this={dialog} class="dialog" aria-labelledby="shortcuts-title" oncancel={onCancel}>
  <div class="dialog-head">
    <h2 id="shortcuts-title" tabindex="-1" bind:this={heading}>Keyboard shortcuts</h2>
    <button type="button" class="btn btn-quiet btn-compact" aria-label="Close keyboard shortcuts" title="Close" onclick={onclose}>
      <Icon name="x" />
    </button>
  </div>
  {#each groups as g (g.title)}
    <section class="group" aria-labelledby="shortcuts-{g.title.toLowerCase().replace(/\s+/g, '-')}">
      <h3 id="shortcuts-{g.title.toLowerCase().replace(/\s+/g, '-')}">{g.title}</h3>
      <dl>
        {#each g.items as item (item.action)}
          <div class="row">
            <dt>
              {#each item.keys as k, i (k)}{#if i > 0}<span class="joiner">{item.joiner ?? 'or'}</span>{/if}<kbd>{k}</kbd>{/each}
            </dt>
            <dd>{item.action}</dd>
          </div>
        {/each}
      </dl>
    </section>
  {/each}
</dialog>

<style>
  .group {
    margin-top: var(--space-5);
  }

  h3 {
    font-size: var(--fs-1);
  }

  dl {
    margin-top: var(--space-2);
    border-top: var(--rule) solid var(--line);
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 13rem) minmax(0, 1fr);
    gap: var(--space-2) var(--space-4);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  dt {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-2);
  }

  dd {
    margin: 0;
    color: var(--ink-muted);
  }

  .joiner {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  kbd {
    padding: 0 var(--space-1);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-2);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    white-space: nowrap;
  }

  @media (max-width: 767px) {
    .row {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--space-1);
    }
  }
</style>
