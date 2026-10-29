<!--
  ActionMenu: a quiet button that opens a short list of actions under it,
  for the rarer actions of a screen's title row (Duplicate and Archive on a
  contest), so they stay one press away without each taking a button. It
  behaves like the account menu: Escape, a press outside or tabbing out
  closes the list, Escape gives focus back to the button, and choosing an
  item closes the list first and focuses the button, so a dialog the item
  opens hands focus back there.

  Props: `label` (the button's word, default "More"), `items` (each
  `{ label, onselect, danger? }`, a danger item drawn in the danger colour)
  and `id` (the list's id, unique on the page).
-->
<script lang="ts">
  import { dismissable } from '../lib/dismissable';
  import Icon from './Icon.svelte';

  interface Item {
    label: string;
    onselect: () => void;
    danger?: boolean;
  }

  interface Props {
    label?: string;
    items: Item[];
    id: string;
  }

  let { label = 'More', items, id }: Props = $props();

  let open = $state(false);
  let button: HTMLButtonElement | undefined = $state();

  function choose(item: Item) {
    open = false;
    button?.focus();
    item.onselect();
  }
</script>

<div class="menu" use:dismissable={{ open, onclose: () => (open = false), trigger: button }}>
  <button type="button" class="btn btn-quiet trigger" aria-expanded={open} aria-controls={id} bind:this={button} onclick={() => (open = !open)}>
    {label}
    <Icon name="chevron-down" />
  </button>
  <ul {id} class="panel" hidden={!open}>
    {#each items as item (item.label)}
      <li><button type="button" class="item" class:danger={item.danger} onclick={() => choose(item)}>{item.label}</button></li>
    {/each}
  </ul>
</div>

<style>
  .menu {
    position: relative;
  }

  .trigger {
    gap: var(--space-1);
  }

  .trigger[aria-expanded='true'] {
    background: var(--surface-2);
    color: var(--ink);
  }

  .panel {
    position: absolute;
    inset-inline-end: 0;
    top: calc(100% + var(--space-2));
    z-index: var(--z-bar);
    display: grid;
    width: max-content;
    min-width: 12rem;
    max-width: calc(100vw - 2 * var(--gutter));
    margin: 0;
    padding: var(--space-2);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
    list-style: none;
  }

  .panel[hidden] {
    display: none;
  }

  .item {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: var(--control-h);
    padding-inline: var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--ink);
    font: inherit;
    font-weight: 500;
    text-align: start;
  }

  .item:hover {
    background: var(--surface-2);
  }

  .item.danger {
    color: var(--bad);
  }
</style>
