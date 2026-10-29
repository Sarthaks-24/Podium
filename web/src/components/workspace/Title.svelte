<!--
  Title: the workspace's title row. A contest's banner sits above it (so
  from 1024 px it never moves the editor), then the problem letter and
  title as the page's h1, and from 1024 px the limits line under it. Under
  1024 px the back link sits above the title outside a contest (a contest's
  row has Leave), the letter is for screen readers only, and the note
  takes the clock's place when there is no clock. Props:
  `layout`, `banner` and `rail` (snippets, optional), `label` (the problem
  letter, or ""), `title`, `limits`, `back` ({ href, label }), `note`,
  `hasClock`, and in a contest from 1024 px `statementHidden` with
  `ontogglestatement` ("Hide statement" gives the editor the width, E5).
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
    statementHidden?: boolean;
    ontogglestatement?: () => void;
  }

  let { layout, banner, rail, label, title, limits, back, note, hasClock, statementHidden = false, ontogglestatement }: Props = $props();

  const wide = $derived(layout === 'wide');
</script>

<div class="title-area">
  {#if banner}<div class="banner-slot">{@render banner()}</div>{/if}
  <div class="title-main">
    <!-- Under 1024 px a contest's row carries the letter and Leave (B4): the title is the title. -->
    <PageHeader compact back={wide || rail ? null : back}>
      {#snippet heading()}{#if label}<span class="mono letter" class:hidden-narrow={!wide}>{label}</span><span class="visually-hidden">.&nbsp;</span>{/if}{title}{/snippet}
    </PageHeader>
    {#if wide}<p class="limits">{limits}</p>{/if}
  </div>
  {#if wide && ontogglestatement}
    <button type="button" class="btn btn-quiet btn-compact" aria-expanded={!statementHidden} aria-controls="ws-problem" onclick={ontogglestatement}>
      {statementHidden ? 'Show statement' : 'Hide statement'}
    </button>
  {/if}
  <!-- Without a clock the note takes its place: under 1024 px, and beside a contest's bar (practice, time up). -->
  {#if (!wide || rail) && !hasClock && note}<p class="note">{note}</p>{/if}
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

  .banner-slot {
    flex-basis: 100%;
    min-width: 0;
  }

  .hidden-narrow {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
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
