<!--
  PageHeader: the top of every screen (v2 F2, audit 05-1 and 05-2). From the
  top: a breadcrumb trail on organiser screens or one back link on student
  screens, then the title row (the page's only h1, which takes focus after
  a navigation, its state word, and the actions at the end of the row),
  one muted lead line, any extra lines, and the contest sub-nav. It adds no
  space below itself; the first block of the page keeps its own top margin.
  Props: `title` (the h1 text) or a `heading` snippet for richer titles;
  `crumbs` (the whole trail, current page last; shown when it has two or
  more steps) or `back` ({ href, label }); `state` ({ word, kind }, kind
  being the contest state for the colour); `lead`; `compact` (the
  workspace's smaller title, so the editor keeps its height); and the
  snippets `actions`, `nav` and `children`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import Breadcrumbs, { type Crumb } from './Breadcrumbs.svelte';

  interface Props {
    title?: string;
    heading?: Snippet;
    crumbs?: Crumb[];
    back?: { href: string; label: string } | null;
    state?: { word: string; kind: string } | null;
    lead?: string;
    compact?: boolean;
    actions?: Snippet;
    nav?: Snippet;
    children?: Snippet;
  }

  let { title = '', heading, crumbs = [], back = null, state = null, lead = '', compact = false, actions, nav, children }: Props = $props();
</script>

<div class="page-header" class:compact>
  {#if crumbs.length > 1}
    <Breadcrumbs items={crumbs} />
  {:else if back}
    <a class="back" href={back.href}>{back.label}</a>
  {/if}
  <div class="title-row">
    <h1 tabindex="-1">{#if heading}{@render heading()}{:else}{title}{/if}</h1>
    {#if state && state.word}<span class="state-word" data-state={state.kind}>{state.word}</span>{/if}
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </div>
  {#if lead}<p class="lead-line">{lead}</p>{/if}
  {#if children}{@render children()}{/if}
  {#if nav}<div class="nav-slot">{@render nav()}</div>{/if}
</div>

<style>
  .page-header {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }

  .title-row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
  }

  h1 {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .compact h1 {
    font-size: var(--fs-4);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin-left: auto;
  }

  .lead-line {
    max-width: var(--measure-narrow);
    margin-top: var(--space-2);
    color: var(--ink-muted);
  }

  .nav-slot {
    margin-top: var(--space-3);
  }
</style>
