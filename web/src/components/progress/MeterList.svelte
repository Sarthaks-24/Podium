<!--
  MeterList: rows of a label, a single-hue bar and its value, for "By
  topic", "By difficulty" and "Where points were lost" on "Your progress"
  (v2 frontend PLAN.md 6.12, items 4 and 5). The bar is --act on a
  --surface-2 track; the words carry the meaning, so verdict colours are
  not reused here. A row can be muted with a note (a topic with only one
  problem says "(1 problem)", so one result is not over-read). The value
  reads "120 of 300" or "4 problems", digits in mono. It is a description
  list: each label is heard with its value; the bar is hidden from screen
  readers. In forced colours the fill uses Highlight. Props: `rows`
  ({ key, label, value, max, valueText, note?, muted? }) and `stacked`
  (the label above its bar at every width, for a narrow column). The
  section heading above names the list.
-->
<script lang="ts" module>
  export interface MeterRow {
    key: string;
    label: string;
    value: number;
    max: number;
    /** The value as words, such as "120 of 300" or "4 problems". */
    valueText: string;
    note?: string;
    muted?: boolean;
  }
</script>

<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';

  let { rows, stacked = false }: { rows: MeterRow[]; stacked?: boolean } = $props();

  function width(r: MeterRow): number {
    if (!(r.max > 0)) return 0;
    return Math.round(Math.min(1, Math.max(0, r.value / r.max)) * 100);
  }
</script>

<dl class="meters" class:stacked>
  {#each rows as r (r.key)}
    <div class="row" class:muted-row={r.muted}>
      <dt>{r.label}{#if r.note}&nbsp;<span class="note">{r.note}</span>{/if}</dt>
      <dd>
        <span class="track" aria-hidden="true"><span class="fill" style:width="{width(r)}%"></span></span>
        <span class="value"><MonoDigits text={r.valueText} /></span>
      </dd>
    </div>
  {/each}
</dl>

<style>
  .meters {
    display: grid;
    gap: 0;
    margin: 0;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 12rem) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-1) var(--space-4);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .row:first-child {
    border-top: var(--rule) solid var(--line);
  }

  dt {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  dd {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 7rem;
    align-items: center;
    gap: var(--space-3);
    margin: 0;
  }

  .track {
    display: block;
    height: var(--space-2);
    background: var(--surface-2);
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--act);
  }

  .value {
    color: var(--ink);
    font-size: var(--fs-0);
    text-align: right;
    white-space: nowrap;
  }

  .note {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .muted-row dt {
    color: var(--ink-muted);
  }

  .muted-row .fill {
    background: var(--ink-muted);
  }

  @media (forced-colors: active) {
    .track {
      outline: var(--rule) solid CanvasText;
    }

    .fill,
    .muted-row .fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }

  /* A narrow column: the label above its bar and value. */
  .stacked .row {
    grid-template-columns: minmax(0, 1fr);
  }

  .stacked dd {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  /* Phones: the same. */
  @media (width < 768px) {
    .row {
      grid-template-columns: minmax(0, 1fr);
    }

    dd {
      grid-template-columns: minmax(0, 1fr) auto;
    }
  }
</style>
