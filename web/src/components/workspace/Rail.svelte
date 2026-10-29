<!--
  Rail: what the workspace's left column holds from 1024 px. The clock
  slot (a contest clock, or a one-line note such as "You are trying this
  problem as a student.") with the back link under it (S6), then the
  problem's title with the best score and a 2 px bar (a partial score
  fills in the partial colour, as standings cells do). A contest has its
  bar instead (B6). Props: `clock` (a snippet, optional), `note`,
  `backHref`, `backLabel`, `title` and `best` ({ score, max } or null).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    clock?: Snippet<[boolean]>;
    note: string;
    backHref: string;
    backLabel: string;
    title: string;
    best: { score: number; max: number } | null;
  }

  let { clock, note, backHref, backLabel, title, best }: Props = $props();
</script>

<div class="rail-head">
  <div class="clock-slot">
    {#if clock}{@render clock(false)}{:else if note}<p class="note">{note}</p>{/if}
  </div>
  <a class="back" href={backHref}>{backLabel}</a>
</div>
<div class="rail-problem">
  <span class="rail-title">{title}</span>
  <span class="mono rail-score">{best ? `${best.score} of ${best.max}` : 'Not submitted'}</span>
  <span class="bar" aria-hidden="true">
    <span
      class="bar-fill"
      class:part={best !== null && best.score > 0 && best.score < best.max}
      style:width="{best && best.max > 0 ? Math.round((best.score / best.max) * 100) : 0}%"
    ></span>
  </span>
</div>

<style>
  .rail-head {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
  }

  .rail-head .back {
    margin-inline-start: calc(-1 * var(--space-1));
    padding-inline: var(--space-1);
  }

  .clock-slot:empty {
    display: none;
  }

  .note {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .rail-problem {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius);
    background: var(--act-tint);
  }

  .rail-title {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .rail-score {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .bar {
    display: block;
    height: calc(2 * var(--rule));
    background: var(--line);
  }

  .bar-fill {
    display: block;
    height: 100%;
    background: var(--ok);
  }

  .bar-fill.part {
    background: var(--part);
  }

  /* Forced colours drop backgrounds: the bar keeps its track and fill. */
  @media (forced-colors: active) {
    .bar {
      outline: var(--rule) solid CanvasText;
    }

    .bar-fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }
</style>
