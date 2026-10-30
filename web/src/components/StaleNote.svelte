<!--
  StaleNote: one line on a screen that shows a kept answer whose refresh
  failed (lib/loader.svelte.ts, v4 U2): the page may be out of date, and
  Try again loads it once more. Nothing shows while the page is fresh.
  Props: `page` (the screen's Loader).
-->
<script lang="ts">
  interface Props {
    page: { stale: boolean; load: () => Promise<void> };
  }

  let { page }: Props = $props();
</script>

{#if page.stale}
  <div class="banner banner-info stale" role="status">
    <p>This may be out of date: the latest could not load.</p>
    <button type="button" class="btn btn-secondary btn-compact" onclick={() => void page.load()}>Try again</button>
  </div>
{/if}

<style>
  .stale {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
</style>
