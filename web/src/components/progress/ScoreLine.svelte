<!--
  ScoreLine: "Score over time" on "Your progress" (v2 frontend PLAN.md
  6.12, item 2; the dataviz method). One series: each contest's score as a
  percentage of its maximum, oldest to newest, as a 2 px line in --act with
  8 px markers ringed in the surface colour, on a solid hairline grid at 0,
  50 and 100 percent with one y axis and short contest names on x (thinned
  when they would collide; the first and the latest always show). Only the
  latest point carries a direct label. The SVG is drawn in pixels at the
  measured width, so its text never shrinks on a phone.

  A pointer anywhere over the plot snaps a crosshair to the nearest
  contest and shows "Week 6 contest: 240 of 300 (80 percent)" beside it.
  The tooltip only adds: the SVG has role="img" with a one-sentence
  summary, and "Show as table" swaps in a table of every contest, which is
  the keyboard and screen-reader path. No motion. In forced colours the
  line and markers use system colours. The parent leaves it out below two
  contests. Props: `contests` (newest first or in any order).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { formatDayTime, scoreParts } from '../../lib/format';
  import { chronological, contestPercent, pointLabel, scoreSummary, shortTitle, type ProgressContest } from '../../lib/progress';

  let { contests }: { contests: ProgressContest[] } = $props();

  const HEIGHT = 208;
  const PAD = { top: 16, right: 44, bottom: 32, left: 44 };

  let width = $state(0);
  let asTable = $state(false);
  let active = $state<number | null>(null);

  const points = $derived(chronological(contests));
  const summary = $derived(scoreSummary(contests));
  const plotW = $derived(Math.max(0, width - PAD.left - PAD.right));
  const plotH = HEIGHT - PAD.top - PAD.bottom;

  function x(i: number): number {
    const n = points.length;
    return PAD.left + (n <= 1 ? plotW / 2 : (i * plotW) / (n - 1));
  }

  function y(pct: number): number {
    return PAD.top + plotH - (Math.min(100, Math.max(0, pct)) / 100) * plotH;
  }

  const path = $derived(points.map((c, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)} ${y(contestPercent(c)).toFixed(1)}`).join(' '));

  /** Which x labels show: every step-th, plus the first and the last, so names never collide. */
  const labelled = $derived.by(() => {
    const n = points.length;
    const room = Math.max(1, Math.floor(plotW / 72));
    const step = Math.max(1, Math.ceil(n / room));
    // A local set, rebuilt on every change; nothing renders from its mutations.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity
    const keep = new Set<number>();
    for (let i = 0; i < n; i += step) keep.add(i);
    keep.add(n - 1);
    // The last label wins over a neighbour that would touch it.
    if (n > 1 && step > 1) for (let i = n - 2; i > n - 1 - step && i >= 0; i--) keep.delete(i);
    keep.add(0);
    return keep;
  });

  function onMove(event: PointerEvent) {
    const svg = event.currentTarget as SVGSVGElement;
    const box = svg.getBoundingClientRect();
    const px = event.clientX - box.left;
    let best = 0;
    for (let i = 1; i < points.length; i++) if (Math.abs(x(i) - px) < Math.abs(x(best) - px)) best = i;
    active = best;
  }

  const tip = $derived(active !== null && points[active] ? points[active] : null);
  const tipLeft = $derived(active !== null ? Math.min(Math.max(x(active), 96), Math.max(96, width - 96)) : 0);
  const last = $derived(points.length > 0 ? points.length - 1 : null);
</script>

<div class="score-line">
  <div class="head">
    <p class="muted small" id="score-line-summary">{summary}</p>
    <button type="button" class="btn btn-quiet btn-compact" aria-controls="score-line-view" onclick={() => (asTable = !asTable)}>
      {asTable ? 'Show as chart' : 'Show as table'}
    </button>
  </div>

  <div id="score-line-view">
    {#if asTable}
      <div class="table-wrap">
        <table class="table table-dense">
          <caption class="visually-hidden">Score in each contest, oldest first</caption>
          <thead>
            <tr>
              <th scope="col">Contest</th>
              <th scope="col">Ended</th>
              <th scope="col" class="right">Score</th>
              <th scope="col" class="right">Percent</th>
            </tr>
          </thead>
          <tbody>
            {#each points as c (c.slug)}
              <tr>
                <th scope="row">{c.title}</th>
                <td><MonoDigits text={formatDayTime(c.ended_at)} /></td>
                <td class="right"><MonoDigits parts={scoreParts(c.score, c.max_score)} /></td>
                <td class="right num mono">{contestPercent(c)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <div class="plot" bind:clientWidth={width}>
        {#if width > 0}
          <svg
            width={width}
            height={HEIGHT}
            role="img"
            aria-labelledby="score-line-summary"
            onpointermove={onMove}
            onpointerdown={onMove}
            onpointerleave={() => (active = null)}
          >
            {#each [0, 50, 100] as g (g)}
              <line class="grid" x1={PAD.left} x2={PAD.left + plotW} y1={y(g)} y2={y(g)} />
              <text class="tick" x={PAD.left - 8} y={y(g)} dy="0.32em" text-anchor="end">{g}%</text>
            {/each}
            {#each points as c, i (c.slug)}
              {#if labelled.has(i)}
                <text class="tick" x={x(i)} y={HEIGHT - 10} text-anchor={points.length > 1 && i === 0 ? 'start' : points.length > 1 && i === points.length - 1 ? 'end' : 'middle'}>{shortTitle(c.title)}</text>
              {/if}
            {/each}
            {#if tip && active !== null}
              <line class="cross" x1={x(active)} x2={x(active)} y1={PAD.top} y2={PAD.top + plotH} />
            {/if}
            <path class="series" d={path} />
            {#each points as c, i (c.slug)}
              <circle class="marker" class:on={active === i} cx={x(i)} cy={y(contestPercent(c))} r="4" />
            {/each}
            {#if last !== null}
              <text class="end-label" x={x(last) + 8} y={y(contestPercent(points[last]))} dy="0.32em">{contestPercent(points[last])}%</text>
            {/if}
          </svg>
          {#if tip}
            <p class="tip" aria-hidden="true" style:left="{tipLeft}px">
              <strong class="mono">{contestPercent(tip)}%</strong>
              <span>{pointLabel(tip)}</span>
            </p>
          {/if}
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .score-line {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
  }

  .plot {
    position: relative;
    min-width: 0;
    min-height: 13rem;
  }

  svg {
    display: block;
    overflow: visible;
    touch-action: pan-y;
  }

  .grid {
    stroke: var(--line);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  .cross {
    stroke: var(--line-strong);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  .tick {
    fill: var(--ink-muted);
    font-family: var(--font-ui);
    font-size: var(--fs-0);
  }

  .series {
    fill: none;
    stroke: var(--act);
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }

  .marker {
    fill: var(--act);
    stroke: var(--surface);
    stroke-width: 2;
  }

  .marker.on {
    r: 5;
  }

  .end-label {
    fill: var(--ink);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
  }

  .tip {
    position: absolute;
    top: 0;
    z-index: 1;
    display: grid;
    gap: 0;
    max-width: 12rem;
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
    color: var(--ink-muted);
    font-size: var(--fs-0);
    pointer-events: none;
    transform: translateX(-50%);
  }

  .tip strong {
    color: var(--ink);
    font-size: var(--fs-1);
    font-weight: 400;
  }

  .right,
  .table .right {
    text-align: right;
  }

  @media (forced-colors: active) {
    svg {
      forced-color-adjust: none;
    }

    .grid,
    .cross {
      stroke: GrayText;
    }

    .tick,
    .end-label {
      fill: CanvasText;
    }

    .series {
      stroke: Highlight;
    }

    .marker {
      fill: Highlight;
      stroke: Canvas;
    }
  }
</style>
