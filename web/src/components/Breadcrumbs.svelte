<!--
  Breadcrumbs: the trail above the title on organiser screens (v2 F4),
  rendered by PageHeader. A nav named "Breadcrumb" holding an ordered list;
  every item but the last is a link followed by a chevron hidden from
  assistive technology, and the last item is the current page, marked with
  aria-current. Each link is at least --control-h tall (X4). Props:
  `items`, the trail from the top level down to the current page.
-->
<script lang="ts" module>
  /** One step of the trail. The current page needs no href. */
  export interface Crumb {
    label: string;
    href?: string;
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte';

  let { items }: { items: Crumb[] } = $props();
</script>

<nav class="crumbs" aria-label="Breadcrumb">
  <ol>
    {#each items as item, i (i)}
      <li>
        {#if i < items.length - 1 && item.href}
          <a href={item.href}>{item.label}</a>
          <Icon name="chevron-right" />
        {:else}
          <span aria-current="page">{item.label}</span>
        {/if}
      </li>
    {/each}
  </ol>
</nav>

<style>
  ol {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    padding: 0;
    list-style: none;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  li {
    display: inline-flex;
    align-items: center;
    min-width: 0;
  }

  a,
  span {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
    border-radius: var(--radius);
    overflow-wrap: anywhere;
  }

  span {
    color: var(--ink-muted);
  }

  li :global(.icon) {
    width: 1em;
    height: 1em;
    margin-inline: var(--space-1);
    color: var(--ink-muted);
  }
</style>
