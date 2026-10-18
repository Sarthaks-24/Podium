<!--
  StreamNotice: one quiet line about live updates (lib/stream.ts). While
  the stream is down and polling it says results are checked every 15 s;
  when the server replaced this page's stream with a newer tab's, it says
  live updates stopped and offers "Resume here", which reconnects (and
  takes live updates back from the other tab). Nothing shows while live.
  Props: `mode` (the stream's mode) and `onresume`.
-->
<script lang="ts">
  import type { StreamMode } from '../lib/stream';

  let { mode, onresume }: { mode: StreamMode; onresume: () => void } = $props();
</script>

{#if mode === 'replaced'}
  <div class="notice" role="status">
    <p>This page stopped live updates because Podium is open in another tab or window.</p>
    <button type="button" class="btn btn-secondary btn-compact" onclick={onresume}>Resume here</button>
  </div>
{:else if mode === 'polling'}
  <p class="line" role="status">Live updates are paused. Checking for results every 15 s.</p>
{/if}

<style>
  .notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface-2);
    font-size: var(--fs-0);
  }

  .line {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }
</style>
