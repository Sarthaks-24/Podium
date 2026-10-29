<!--
  Strip: under 1024 px one strip sticks to the top of the window (F7.2,
  audit S7): the contest clock with the previous and next problem, then
  the Problem, Code (and under 768 px Result) tabs and Submit, so Submit
  is never a tab away. Its height sets the page's scroll padding while it
  sticks (X3, base.css). Props: `layout`, `clock` and `rail` (snippets,
  optional), `tab`, `ontab`, `judging` (true while the shown submission is
  being judged: the Result tab carries a dot) and `submit` (a snippet).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { publishHeight } from '../../lib/publishHeight';
  import Tabs from '../Tabs.svelte';

  type Tab = 'problem' | 'code' | 'result';

  interface Props {
    layout: 'tablet' | 'phone';
    clock?: Snippet<[boolean]>;
    rail?: Snippet<[string]>;
    tab: Tab;
    ontab: (tab: Tab) => void;
    judging: boolean;
    submit: Snippet;
  }

  let { layout, clock, rail, tab, ontab, judging, submit }: Props = $props();

  const tabs = $derived(
    layout === 'phone'
      ? [
          { id: 'problem' as const, label: 'Problem' },
          { id: 'code' as const, label: 'Code' },
          { id: 'result' as const, label: 'Result', busy: judging },
        ]
      : [
          { id: 'problem' as const, label: 'Problem' },
          { id: 'code' as const, label: 'Code' },
        ],
  );

  const controls = (id: Tab) => (id === 'problem' ? 'ws-problem' : id === 'code' && layout === 'tablet' ? 'ws-code ws-result' : `ws-${id}`);
</script>

<div class="strip" data-sticky-bar use:publishHeight={'--sticky-bar-h'}>
  {#if clock || rail}
    <div class="strip-top">
      {#if clock}<div class="strip-clock">{@render clock(true)}</div>{/if}
      {#if rail}{@render rail('strip')}{/if}
    </div>
  {/if}
  <div class="strip-tabs">
    <Tabs {tabs} selected={tab} onselect={ontab} label="Workspace" idPrefix="ws-tab" {controls} compact={layout === 'phone'} />
    {@render submit()}
  </div>
</div>

<style>
  .strip {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: grid;
    margin-top: var(--space-3);
    margin-inline: calc(-1 * var(--gutter));
    padding-inline: var(--gutter);
    border-bottom: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .strip-top {
    display: flex;
    align-items: center;
    gap: 0 var(--space-3);
    min-height: var(--control-h-touch);
    border-bottom: var(--rule) solid var(--line);
  }

  .strip-clock {
    flex: 1;
    min-width: 0;
    padding-block: var(--space-1);
  }

  .strip-tabs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0 var(--space-2);
  }

  .strip-tabs :global(.submit) {
    margin-block: var(--space-1);
  }

  /* On a phone the row holds the three tabs and Submit: the word carries
     Submit, and the button may grow while it counts down, since it ends
     the row and moves nothing. */
  @media (width < 768px) {
    .strip-tabs :global(.submit) {
      min-width: 0;
    }

    .strip-tabs :global(.submit svg) {
      display: none;
    }
  }
</style>
