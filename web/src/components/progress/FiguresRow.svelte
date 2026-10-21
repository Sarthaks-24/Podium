<!--
  FiguresRow: the ruled row of headline figures at the top of "Your
  progress" (v2 frontend PLAN.md 6.12, item 1). Four figures, not tiles:
  contests taken, points ("820 of 1500"), problems at full score and
  problems with partial score, each a mono number above a muted label,
  separated by hairline rules. From 768 px they sit in one row; below
  that two by two. Under the row, one muted line names the best contest
  and the problems solved later in practice when there are any. It is a
  description list, so a screen reader hears each label with its value.
  Props: `totals` (the C8 totals).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { countOf, scoreParts } from '../../lib/format';
  import type { ProgressTotals } from '../../lib/progress';

  let { totals }: { totals: ProgressTotals } = $props();

  const best = $derived(typeof totals.personal_best_percent === 'number' ? Math.round(totals.personal_best_percent) : null);
  const upsolved = $derived(totals.upsolved ?? 0);
</script>

<dl class="figures">
  <div class="figure">
    <dt>Contests taken</dt>
    <dd class="mono value">{totals.contests_taken}</dd>
  </div>
  <div class="figure">
    <dt>Points</dt>
    <dd class="value"><MonoDigits parts={scoreParts(totals.points, totals.max_points)} /></dd>
  </div>
  <div class="figure">
    <dt>Problems at full score</dt>
    <dd class="mono value">{totals.problems_full}</dd>
  </div>
  <div class="figure">
    <dt>Problems with partial score</dt>
    <dd class="mono value">{totals.problems_partial}</dd>
  </div>
</dl>
{#if best !== null || upsolved > 0}
  <p class="muted small aside">
    {#if best !== null}Best graded contest: <MonoDigits text="{best} percent" />.{/if}
    {#if upsolved > 0}{best !== null ? ' ' : ''}Solved later in practice: <MonoDigits text={countOf(upsolved, 'problem')} />.{/if}
  </p>
{/if}

<style>
  .figures {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    margin: 0;
    border-block: var(--rule) solid var(--line);
  }

  .figure {
    display: flex;
    flex-direction: column-reverse;
    justify-content: flex-end;
    gap: var(--space-1);
    min-width: 0;
    padding: var(--space-4) var(--space-4) var(--space-4) 0;
  }

  /* Two by two on phones: a rule between the columns and between the rows. */
  .figure:nth-child(even) {
    padding-left: var(--space-4);
    border-left: var(--rule) solid var(--line);
  }

  .figure:nth-child(n + 3) {
    border-top: var(--rule) solid var(--line);
  }

  dt {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  dd {
    margin: 0;
  }

  /* The words inside "820 of 1500" stay smaller than the digits. */
  .value {
    color: var(--ink);
    font-size: var(--fs-2);
    line-height: var(--lh-tight);
  }

  .value.mono,
  .value :global(.mono) {
    font-size: var(--fs-4);
  }

  /* "830 of 1400" stays on one line in a narrow column. */
  @media (width < 1024px) {
    .value.mono,
    .value :global(.mono) {
      font-size: var(--fs-3);
    }
  }

  .aside {
    margin-top: var(--space-2);
  }

  @media (min-width: 768px) {
    .figures {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }

    .figure,
    .figure:nth-child(even) {
      padding: var(--space-4) var(--space-4) var(--space-4) var(--space-4);
      border-left: var(--rule) solid var(--line);
    }

    .figure:first-child {
      padding-left: 0;
      border-left: 0;
    }

    .figure:nth-child(n + 3) {
      border-top: 0;
    }
  }
</style>
