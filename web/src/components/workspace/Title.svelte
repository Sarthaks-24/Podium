<!--
  Title: the workspace's title row. A contest's banner sits above it (so
  from 1024 px it never moves the editor), then the problem letter and
  title as the page's h1, and from 1024 px the limits line under it. Under
  1024 px the back link sits above the title, the note takes the clock's
  place when there is no clock, and the contest's problems follow. Props:
  `layout`, `banner` and `rail` (snippets, optional), `label` (the problem
  letter, or ""), `title`, `limits`, `back` ({ href, label }), `note` and
  `hasClock`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import PageHeader from '../PageHeader.svelte';

  interface Props {
    layout: 'wide' | 'tablet' | 'phone';
    banner?: Snippet;
    rail?: Snippet<[string]>;
    label: string;
    title: string;
    limits: string;
    back: { href: string; label: string };
    note: string;
    hasClock: boolean;
  }

  let { layout, banner, rail, label, title, limits, back, note, hasClock }: Props = $props();

  const wide = $derived(layout === 'wide');
</script>

<div class="title-area">
  {#if banner}<div class="banner-slot">{@render banner()}</div>{/if}
  <div class="title-main">
    <PageHeader compact back={wide ? null : back}>
      {#snippet heading()}{#if label}<span class="mono letter">{label}</span><span class="visually-hidden">.&nbsp;</span>{/if}{title}{/snippet}
    </PageHeader>
    {#if wide}<p class="limits">{limits}</p>{/if}
  </div>
  {#if !wide && !hasClock && note}<p class="note">{note}</p>{/if}
  {#if !wide && rail}<div class="rail-inline">{@render rail(layout)}</div>{/if}
</div>

<style>
  .title-area {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
  }

  .title-main {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }

  .limits,
  .note {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .note {
    max-width: 22rem;
  }

  .letter {
    margin-right: var(--space-3);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .banner-slot,
  .rail-inline {
    flex-basis: 100%;
    min-width: 0;
  }

  .banner-slot:empty {
    display: none;
  }

  @media (min-width: 1024px) {
    .title-area :global(h1) {
      font-size: var(--fs-3);
    }
  }

  /* The short tier (F7.3): the title shares one line with the limits. */
  @media (min-width: 1024px) and (max-height: 45rem) {
    .title-main {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 0 var(--space-3);
    }

    .title-area :global(h1) {
      font-size: var(--fs-2);
    }
  }
</style>
