<!--
  ContestBanner: the newest announcement or answered question that arrived
  while the student was in the contest (design.md section 6, Banner), shown
  in one quiet strip above the workspace. It is a polite status, never an
  alert: it does not take focus or interrupt typing. Dismiss hides it;
  "Show all messages" opens the messages dialog. When several arrived, the
  strip says how many more are waiting. Props: `items` (newest first),
  `ondismiss` (with the item's key) and `onopen`.
-->
<script lang="ts">
  import type { BannerItem } from '../lib/contests';
  import { formatWhen } from '../lib/workspace';
  import Icon from './Icon.svelte';
  import Markdown from './Markdown.svelte';

  interface Props {
    items: BannerItem[];
    ondismiss: (key: string) => void;
    onopen: () => void;
  }

  let { items, ondismiss, onopen }: Props = $props();

  const top = $derived(items[0] ?? null);
</script>

<div class="wrap" role="status">
  {#if top}
    <div class="banner banner-info strip">
      <Icon name={top.kind === 'announcement' ? 'bell' : 'message-square'} />
      <div class="text">
        <p class="lead">
          {top.kind === 'announcement' ? 'Announcement' : 'Your question was answered'}
          <span class="mono when">{formatWhen(top.at)}</span>
        </p>
        {#if top.question}<p class="question">{top.question}</p>{/if}
        <Markdown source={top.body} compact />
        {#if items.length > 1}<p class="more">{items.length - 1} more {items.length === 2 ? 'message' : 'messages'} waiting.</p>{/if}
      </div>
      <div class="actions">
        <button type="button" class="btn btn-quiet btn-compact" onclick={onopen}>Show all messages</button>
        <button type="button" class="btn btn-quiet btn-compact" onclick={() => ondismiss(top.key)}>Dismiss</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .wrap:empty {
    display: none;
  }

  .strip {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: start;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
    padding-block: var(--space-2);
  }

  .strip > :global(.icon) {
    margin-top: var(--space-1);
    color: var(--act);
  }

  .text {
    min-width: 0;
    font-size: var(--fs-0);
  }

  .lead {
    font-weight: 600;
  }

  .when {
    margin-left: var(--space-2);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .question {
    color: var(--ink-muted);
  }

  .more {
    margin-top: var(--space-1);
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-1);
  }

  @media (max-width: 767px) {
    .strip {
      grid-template-columns: auto minmax(0, 1fr);
    }

    .actions {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }
  }
</style>
