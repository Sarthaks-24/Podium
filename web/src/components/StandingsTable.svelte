<!--
  StandingsTable: the leaderboard (design.md 5.4). A dense table in its own
  scroll box, so the header row stays put while rows scroll and, on
  phones, the rank and name columns stay pinned while the problem columns
  scroll sideways, with a line saying so while columns wait to the right.
  Rank and scores are mono with tabular figures. Each tried problem cell
  has a 4 px bar along its bottom, the earned share in --ok (full) or
  --part (partial) on a --line track, on a --surface cell (audit
  2026-10-20 S8); a tried zero is "0" in --bad, untried a muted dash, and
  "+N" in --ink-muted counts submissions made after the freeze; screen
  readers hear each cell as a sentence (lib/standings.ts). The signed-in
  student's row is tinted with --act-tint and marked "(you)", and "Jump to
  you" scrolls to it and moves focus there; it hides (keeping its space)
  while that row is already in view. The box is sized to the screen and scrolls
  both ways at every width, so the header row sticks on phones too; under
  768 px a vertical swipe that reaches the end of the box goes on to the
  page, so it is never trapped. From 768 px the board is as wide as its
  content (names stop at 16rem), so scores sit beside names (audit 01-10);
  on phones a long name wraps to two lines instead of being cut (audit
  07-9). Props: `standings`, `labels` (the problem
  letters, in order), `caption` (the table's accessible name), `large`
  (bigger type, for a projector) and `nameHref` (organiser views only:
  each name links to that student's page, web-quality #16; the board
  itself carries no mark of any review decision).
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { cellFor, cellView, formatElapsed, myRow, type Standings } from '../lib/standings';

  interface Props {
    standings: Standings;
    labels: string[];
    caption: string;
    large?: boolean;
    nameHref?: (userId: number) => string;
  }

  let { standings, labels, caption, large = false, nameHref }: Props = $props();

  let wrap: HTMLDivElement | undefined = $state();
  const mine = $derived(myRow(standings));
  /** The own row is inside the box's view, so "Jump to you" has nothing to do (S8). */
  let mineInView = $state(false);
  /** More problem columns wait to the right of the box (phones, S8). */
  let moreRight = $state(false);

  function measure() {
    if (!wrap) return;
    moreRight = wrap.scrollWidth - wrap.clientWidth - wrap.scrollLeft > 1;
    const row = mine ? wrap.querySelector<HTMLElement>(`[data-user="${mine.user_id}"]`) : null;
    if (!row) {
      mineInView = false;
      return;
    }
    const box = wrap.getBoundingClientRect();
    const r = row.getBoundingClientRect();
    const head = wrap.querySelector('thead')?.getBoundingClientRect().height ?? 0;
    mineInView = r.top >= box.top + head - 1 && r.bottom <= box.bottom + 1 && r.top >= 0 && r.bottom <= window.innerHeight;
  }

  onMount(() => {
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(() => measure()) : null;
    if (wrap && ro) ro.observe(wrap);
    window.addEventListener('scroll', measure, { passive: true });
    window.addEventListener('resize', measure);
    measure();
    return () => {
      ro?.disconnect();
      window.removeEventListener('scroll', measure);
      window.removeEventListener('resize', measure);
    };
  });

  // Rows move as the board refreshes.
  $effect(() => {
    void standings;
    void tick().then(measure);
  });

  async function jump() {
    if (!mine || !wrap) return;
    await tick();
    const row = wrap.querySelector<HTMLTableRowElement>(`[data-user="${mine.user_id}"]`);
    if (!row) return;
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    row.scrollIntoView({ block: 'center', behavior: reduce ? 'auto' : 'smooth' });
    row.focus({ preventScroll: true });
  }
</script>

{#if mine}
  <!-- Hidden, not removed, while the own row is in view, so the board never shifts. -->
  <p class="tools" class:away={mineInView} inert={mineInView}>
    <button type="button" class="btn btn-secondary btn-compact" onclick={jump}>Jump to you</button>
  </p>
{/if}
{#if moreRight}<p class="more muted">Scroll the board sideways to see every problem.</p>{/if}

{#if standings.rows.length === 0}
  <p class="muted empty">Nobody is in the standings yet. Rows appear as students submit.</p>
{:else}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="board-wrap" class:large bind:this={wrap} tabindex="0" role="region" aria-label={caption} onscroll={measure}>
    <table class="board">
      <caption class="visually-hidden">{caption}</caption>
      <thead>
        <tr>
          <th scope="col" class="rank"><span aria-hidden="true">#</span><span class="visually-hidden">Rank</span></th>
          <th scope="col" class="name">Name</th>
          <th scope="col" class="total">Score</th>
          {#each labels as l (l)}<th scope="col" class="pcell head"><span class="visually-hidden">Problem </span>{l}</th>{/each}
        </tr>
      </thead>
      <tbody>
        {#each standings.rows as r (r.user_id)}
          {@const me = mine?.user_id === r.user_id}
          <tr class:me data-user={r.user_id} tabindex={me ? -1 : undefined}>
            <td class="rank mono">{r.rank}</td>
            <th scope="row" class="name">
              {#if nameHref}
                <a class="name-text name-link" href={nameHref(r.user_id)} title={r.name}>{r.name}</a>
              {:else}
                <span class="name-text" title={r.name}>{r.name}{#if me}<span class="you">&nbsp;(you)</span>{/if}</span>
              {/if}
            </th>
            <td class="total mono">
              {r.total}<span class="visually-hidden">, reached after</span>
              <span class="time" title="Time to reach this score">{formatElapsed(r.last_improvement_s)}</span>
            </td>
            {#each labels as l (l)}
              {@const v = cellView(cellFor(r, l), l)}
              <td class="pcell tone-{v.tone}">
                {#if v.tone !== 'none'}
                  <span class="meter" aria-hidden="true"><span class="meter-fill" style:width="{Math.round(v.fraction * 100)}%"></span></span>
                {/if}
                <span class="ctext mono" aria-hidden="true">
                  {#if v.text !== ''}<span class:untried={v.tone === 'none'}>{v.text}</span>{/if}{#if v.pending}<span class="pending">{v.pending}</span>{/if}
                </span>
                <span class="visually-hidden">{v.sentence}</span>
              </td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .tools {
    margin-bottom: var(--space-3);
  }

  .tools.away {
    visibility: hidden;
  }

  .more {
    margin-bottom: var(--space-2);
    font-size: var(--fs-0);
  }

  .empty {
    margin-top: var(--space-4);
  }

  .board-wrap {
    --rank-w: 3.25rem;
    position: relative;
    width: fit-content;
    max-width: 100%;
    max-height: max(20rem, calc(100dvh - 15rem));
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface);
    overscroll-behavior: contain;
  }

  .board {
    width: auto;
    border-collapse: separate;
    border-spacing: 0;
    font-size: var(--fs-0);
  }

  .board th,
  .board td {
    height: calc(var(--control-h) + var(--space-2));
    padding: var(--space-1) var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    background: var(--surface);
    text-align: left;
    vertical-align: middle;
    white-space: nowrap;
  }

  .board thead th {
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--surface-2);
    font-weight: 600;
  }

  .board tbody tr:last-child > * {
    border-bottom: 0;
  }

  .board tr.me > * {
    background: var(--act-tint);
  }

  .board tr.me:focus-visible {
    outline: calc(2 * var(--rule)) solid var(--act);
    outline-offset: calc(-2 * var(--rule));
  }

  .rank {
    position: sticky;
    left: 0;
    z-index: 1;
    width: var(--rank-w);
    min-width: var(--rank-w);
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .board td.rank {
    text-align: right;
  }

  .name {
    position: sticky;
    left: var(--rank-w);
    z-index: 1;
    max-width: 16rem;
    overflow: hidden;
    border-right: var(--rule) solid var(--line);
    font-weight: 500;
    text-overflow: ellipsis;
  }

  .board thead .rank,
  .board thead .name {
    z-index: 3;
  }

  .name-link {
    color: var(--ink);
  }

  .you {
    margin-left: var(--space-1);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .total {
    width: 6rem;
    font-variant-numeric: tabular-nums;
    text-align: right;
  }

  .board th.total {
    text-align: right;
  }

  .board td.total {
    text-align: right;
    font-size: var(--fs-1);
  }

  .time {
    display: block;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    line-height: 1;
  }

  /* Problem columns stay narrow and equal; the name column takes the rest. */
  .pcell {
    position: relative;
    width: 5.5rem;
    min-width: 4.5rem;
    text-align: center;
  }

  .board .pcell {
    text-align: center;
  }

  /* S8: the earned share is a 4 px bar along the bottom of the cell on a
     --line track, so a partial reads as progress; the cell stays --surface. */
  .meter {
    position: absolute;
    right: var(--space-2);
    bottom: var(--space-1);
    left: var(--space-2);
    height: var(--space-1);
    background: var(--line);
  }

  .meter-fill {
    display: block;
    height: 100%;
  }

  .tone-ok .meter-fill {
    background: var(--ok);
  }

  .tone-part .meter-fill {
    background: var(--part);
  }

  .tone-zero .ctext {
    color: var(--bad);
  }

  @media (forced-colors: active) {
    .meter {
      outline: var(--rule) solid CanvasText;
    }

    .meter-fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }

  .ctext {
    position: relative;
    font-variant-numeric: tabular-nums;
  }

  .untried {
    color: var(--ink-muted);
  }

  .pending {
    margin-left: var(--space-1);
    color: var(--ink-muted);
  }

  .large .board {
    font-size: var(--fs-3);
  }

  .large .board td.total {
    font-size: var(--fs-3);
  }

  @media (width < 768px) {
    .board-wrap {
      --rank-w: 2.75rem;
    }

    .board .name {
      min-width: 7.5rem;
      max-width: 9rem;
      white-space: normal;
    }

    .name-text {
      display: -webkit-box;
      overflow: hidden;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      overflow-wrap: break-word;
    }

    .board-wrap {
      overscroll-behavior: auto;
      overscroll-behavior-x: contain;
    }

    .board th,
    .board td {
      padding-inline: var(--space-2);
    }
  }
</style>
