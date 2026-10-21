<!--
  TrendLine: one student's score over their graded contests beside the
  class median, on the organisers' student page (v2 P4 F9.4; the dataviz
  method). The emphasis form: the student is the one series in --act (a
  2 px line with 8 px filled markers ringed in the surface colour), the
  class median is the reference, a 2 px --ink-muted line with hollow
  square markers, so the two differ by shape as well as colour. One y
  axis from 0 to 100 percent with a solid hairline grid at 0, 50 and 100;
  short contest names on x, thinned so they never collide (the first and
  the latest always show). A legend above the plot names both series, and
  only the latest point of each carries a direct label. Drawn in pixels at
  the measured width, so its text never shrinks on a phone.

  A pointer anywhere over the plot snaps a crosshair to the nearest
  contest and shows "Week 6 contest: 80 percent, class median 62.5
  percent". The tooltip only adds: the SVG has role="img" named by the
  one-sentence summary, and "Show as table" swaps in a table of every
  contest, which is the keyboard and screen-reader path. No motion. In
  forced colours the student's series is Highlight and the median
  CanvasText. Props: `points` (oldest first, from lib/students
  trendPoints) and `summary` (the sentence).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { formatDayTime } from '../../lib/format';
  import { shortTitle } from '../../lib/progress';
  import { percentText, type TrendPoint } from '../../lib/students';

  let { points, summary }: { points: TrendPoint[]; summary: string } = $props();

  const HEIGHT = 208;
  const PAD = { top: 16, right: 52, bottom: 32, left: 44 };

  let width = $state(0);
  let asTable = $state(false);
  let active = $state<number | null>(null);

  const plotW = $derived(Math.max(0, width - PAD.left - PAD.right));
  const plotH = HEIGHT - PAD.top - PAD.bottom;
  const hasMedian = $derived(points.some((p) => p.median !== null));

  function x(i: number): number {
    const n = points.length;
    return PAD.left + (n <= 1 ? plotW / 2 : (i * plotW) / (n - 1));
  }

  function y(pct: number): number {
    return PAD.top + plotH - (Math.min(100, Math.max(0, pct)) / 100) * plotH;
  }

  function pathOf(values: (number | null)[]): string {
    let d = '';
    let pen = false;
    values.forEach((v, i) => {
      if (v === null) {
        pen = false;
        return;
      }
      d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
      pen = true;
    });
    return d.trim();
  }

  const studentPath = $derived(pathOf(points.map((p) => p.percent)));
  const medianPath = $derived(pathOf(points.map((p) => p.median)));

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
    if (n > 1 && step > 1) for (let i = n - 2; i > n - 1 - step && i >= 0; i--) keep.delete(i);
    keep.add(0);
    return keep;
  });

  function onMove(event: PointerEvent) {
    const box = (event.currentTarget as SVGSVGElement).getBoundingClientRect();
    const px = event.clientX - box.left;
    let best = 0;
    for (let i = 1; i < points.length; i++) if (Math.abs(x(i) - px) < Math.abs(x(best) - px)) best = i;
    active = best;
  }

  const tip = $derived(active !== null && points[active] ? points[active] : null);
  const tipLeft = $derived(active !== null ? Math.min(Math.max(x(active), 112), Math.max(112, width - 112)) : 0);
  const last = $derived(points.length > 0 ? points[points.length - 1] : null);
  /** The two end labels stay apart when the values are close. */
  const endLabels = $derived.by(() => {
    if (!last) return null;
    const ys = y(last.percent);
    if (last.median === null) return { student: ys, median: null };
    let ym = y(last.median);
    if (Math.abs(ys - ym) < 16) ym = ys + (ym >= ys ? 16 : -16);
    return { student: ys, median: ym };
  });

  function tipText(p: TrendPoint): string {
    return p.median === null ? `${p.title}: ${percentText(p.percent)} percent` : `${p.title}: ${percentText(p.percent)} percent, class median ${percentText(p.median)} percent`;
  }
</script>

<div class="trend">
  <div class="head">
    <ul class="legend" aria-label="Legend">
      <li><span class="key key-student" aria-hidden="true"></span>This student</li>
      {#if hasMedian}<li><span class="key key-median" aria-hidden="true"></span>Class median</li>{/if}
    </ul>
    <button type="button" class="btn btn-quiet btn-compact" aria-controls="trend-view" onclick={() => (asTable = !asTable)}>
      {asTable ? 'Show as chart' : 'Show as table'}
    </button>
  </div>
  <p class="muted small" id="trend-summary">{summary}</p>

  <div id="trend-view">
    {#if asTable}
      <div class="table-wrap">
        <table class="table table-dense">
          <caption class="visually-hidden">Score in each graded contest beside the class median, oldest first</caption>
          <thead>
            <tr>
              <th scope="col">Contest</th>
              <th scope="col">Ended</th>
              <th scope="col" class="right">Score</th>
              <th scope="col" class="right">Class median</th>
            </tr>
          </thead>
          <tbody>
            {#each points as p (p.slug)}
              <tr>
                <th scope="row">{p.title}</th>
                <td><MonoDigits text={formatDayTime(p.ended_at)} /></td>
                <td class="right num">{percentText(p.percent)}%</td>
                <td class="right num">{p.median === null ? '-' : `${percentText(p.median)}%`}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <div class="plot" bind:clientWidth={width}>
        {#if width > 0}
          <svg
            {width}
            height={HEIGHT}
            role="img"
            aria-labelledby="trend-summary"
            onpointermove={onMove}
            onpointerdown={onMove}
            onpointerleave={() => (active = null)}
          >
            {#each [0, 50, 100] as g (g)}
              <line class="grid" x1={PAD.left} x2={PAD.left + plotW} y1={y(g)} y2={y(g)} />
              <text class="tick" x={PAD.left - 8} y={y(g)} dy="0.32em" text-anchor="end">{g}%</text>
            {/each}
            {#each points as p, i (p.slug)}
              {#if labelled.has(i)}
                <text class="tick" x={x(i)} y={HEIGHT - 10} text-anchor={points.length > 1 && i === 0 ? 'start' : points.length > 1 && i === points.length - 1 ? 'end' : 'middle'}>{shortTitle(p.title)}</text>
              {/if}
            {/each}
            {#if tip && active !== null}
              <line class="cross" x1={x(active)} x2={x(active)} y1={PAD.top} y2={PAD.top + plotH} />
            {/if}
            {#if medianPath}<path class="median" d={medianPath} />{/if}
            <path class="series" d={studentPath} />
            {#each points as p, i (p.slug)}
              {#if p.median !== null}
                <rect class="median-marker" x={x(i) - 4} y={y(p.median) - 4} width="8" height="8" />
              {/if}
              <circle class="marker" class:on={active === i} cx={x(i)} cy={y(p.percent)} r="4" />
            {/each}
            {#if last && endLabels}
              <text class="end-label" x={x(points.length - 1) + 8} y={endLabels.student} dy="0.32em">{percentText(last.percent)}%</text>
              {#if last.median !== null && endLabels.median !== null}
                <text class="end-label muted-label" x={x(points.length - 1) + 8} y={endLabels.median} dy="0.32em">{percentText(last.median)}%</text>
              {/if}
            {/if}
          </svg>
          {#if tip}
            <p class="tip" aria-hidden="true" style:left="{tipLeft}px">{tipText(tip)}</p>
          {/if}
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .trend {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-4);
    padding: 0;
    list-style: none;
    font-size: var(--fs-0);
  }

  .legend li {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .key {
    display: inline-block;
    width: var(--space-4);
    height: calc(2 * var(--rule));
  }

  .key-student {
    background: var(--act);
  }

  .key-median {
    background: var(--ink-muted);
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

  .series,
  .median {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }

  .series {
    stroke: var(--act);
  }

  .median {
    stroke: var(--ink-muted);
  }

  .marker {
    fill: var(--act);
    stroke: var(--surface);
    stroke-width: 2;
  }

  .marker.on {
    r: 5;
  }

  .median-marker {
    fill: var(--surface);
    stroke: var(--ink-muted);
    stroke-width: 2;
  }

  .end-label {
    fill: var(--ink);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
  }

  .muted-label {
    fill: var(--ink-muted);
  }

  .tip {
    position: absolute;
    top: 0;
    z-index: 1;
    max-width: 14rem;
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
    color: var(--ink);
    font-size: var(--fs-0);
    pointer-events: none;
    transform: translateX(-50%);
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

    .median,
    .median-marker {
      stroke: CanvasText;
    }

    .median-marker {
      fill: Canvas;
    }

    .key-student {
      background: Highlight;
    }

    .key-median {
      background: CanvasText;
    }
  }
</style>
