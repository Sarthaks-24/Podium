<!--
  PlaceFigure: the student's own place as the page's one display figure
  (D80, design.md amendment: "7 of 24", mono number at --fs-6, Grotesk
  words at --fs-2, a muted label above, no box). Used on the standings
  page and on the contest result (audit 2026-10-20 S9). Props: `place`,
  from lib/standings.ts placeFigure.
-->
<script lang="ts">
  let { place }: { place: { rank: number; of: number; tied: boolean; label: string } } = $props();
</script>

<p class="place">
  <span class="place-label">{place.label}</span>
  <span class="place-figure">
    {#if place.tied}<span class="place-of">tied</span>{/if}
    <span class="mono place-rank">{place.rank}</span>
    <span class="place-of">of {place.of}</span>
  </span>
</p>

<style>
  .place {
    display: grid;
    gap: var(--space-1);
  }

  .place-label {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .place-figure {
    line-height: var(--lh-tight);
  }

  /* Beats the inline mono step (base.css) on purpose: this is the display figure. */
  .place-figure .place-rank {
    font-size: var(--fs-6);
    line-height: 1;
  }

  .place-of {
    font-size: var(--fs-2);
  }
</style>
