<!--
  Strip: under 1024 px one strip sticks to the top of the window: a contest's one row (the problem letters, the clock and the Menu,
  drawn by the contest's `rail` snippet) or, without one, the clock, then
  the Problem, Code (and under 768 px Result) tabs. Run and Submit are not
  offered under 1024 px. Its height sets the page's scroll padding
  while it sticks (base.css), so focused fields never hide under it. Props: `layout`, `clock` and `rail`
  (snippets, optional), `tab`, `ontab` and `judging` (true while the shown
  submission is being judged: the Result tab carries a dot).
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
  }

  let { layout, clock, rail, tab, ontab, judging }: Props = $props();

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
  {#if rail}
    <div class="strip-top">{@render rail('strip')}</div>
  {:else if clock}
    <div class="strip-top"><div class="strip-clock">{@render clock(true)}</div></div>
  {/if}
  <Tabs {tabs} selected={tab} onselect={ontab} label="Workspace" idPrefix="ws-tab" {controls} compact={layout === 'phone'} />
</div>

<style>
  .strip {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: grid;
    grid-template-columns: minmax(0, 1fr);
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

</style>
