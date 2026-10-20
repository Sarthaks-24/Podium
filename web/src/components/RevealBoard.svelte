<!--
  RevealBoard: the projector walk through the final standings (design.md
  5.5, lib/reveal.ts). A full-screen modal on --paper with large rows
  (--fs-3), starting from the frozen board; opening it also puts the
  browser in full screen, and closing it leaves full screen. Space, Enter or the right arrow
  (or the Next button) resolves the lowest unresolved row: its pending
  cells fill with their final scores, the row slides to its final place
  over --dur-slow above the rows it passes, and its rank number counts to
  the new one. The left
  arrow (or Back) undoes a step, and "Start over" begins again. Rows still
  to come have muted names and their pending cells sit on --wait-tint, so
  the room can see what is left. Escape (or Exit) leaves and keeps the
  place: opening the board again resumes. With prefers-reduced-motion the
  row moves at once and is outlined for a second instead. A polite live
  region says each move as a sentence. Props: `open`, `frozen` and `full`
  (the two boards), `labels`, `title` and `onclose`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { initialReveal, isDone, nextIndex, revealStep, type RevealState } from '../lib/reveal';
  import { cellFor, cellView, ordinalWord, type Standings } from '../lib/standings';

  interface Props {
    open: boolean;
    frozen: Standings;
    full: Standings;
    labels: string[];
    title: string;
    onclose: () => void;
  }

  let { open, frozen, full, labels, title, onclose }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let list: HTMLOListElement | undefined = $state();
  let reveal = $state.raw<RevealState>({ rows: [], step: 0 });
  // Earlier states, for Back.
  let past = $state.raw<RevealState[]>([]);
  let shownRank = $state<Record<number, number>>({});
  let flash = $state<number | null>(null);
  // The row sliding to its new place, drawn above the rows it passes.
  let moving = $state<number | null>(null);
  let said = $state('');
  let busy = false;

  const done = $derived(isDone(reveal));
  const nextRow = $derived.by(() => {
    const i = nextIndex(reveal);
    return i >= 0 ? reveal.rows[i] : null;
  });
  const left = $derived(reveal.rows.filter((r) => !r.resolved).length);

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      // Opening again resumes where the walk was left.
      if (reveal.rows.length === 0) startOver();
      dialog.showModal();
      // The projector view: the browser refuses without a recent click, and the board still works in a window then.
      document.documentElement.requestFullscreen?.().catch(() => {});
      tick().then(() => {
        scrollToNext();
        dialog?.querySelector<HTMLButtonElement>('.next')?.focus();
      });
    } else if (!open && dialog.open) {
      dialog.close();
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    }
  });

  function startOver() {
    reveal = initialReveal(frozen, full);
    past = [];
    shownRank = {};
    flash = null;
    said = '';
    void keepFocus();
  }

  // A button that disables itself (Next at the end, Back and Start over at
  // the start) drops focus to the page, outside the dialog, and the keys
  // stop working. Focus goes to Next while there is a row left, else to the
  // title, where Space does nothing and the arrows still work.
  async function keepFocus() {
    await tick();
    if (!dialog?.open) return;
    const active = document.activeElement;
    if (active instanceof HTMLButtonElement ? !active.disabled : dialog.contains(active)) return;
    const next = dialog.querySelector<HTMLButtonElement>('.next');
    if (next && !next.disabled) next.focus();
    else dialog.querySelector<HTMLElement>('#reveal-title')?.focus();
  }

  function back() {
    if (busy || past.length === 0) return;
    reveal = past[past.length - 1];
    past = past.slice(0, -1);
    shownRank = {};
    said = 'Stepped back one row.';
    tick().then(scrollToNext);
    void keepFocus();
  }

  function reduced(): boolean {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  }

  function scrollToNext() {
    const i = nextIndex(reveal);
    const el = list?.children[i] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'center', behavior: 'auto' });
  }

  function countRank(userId: number, from: number, to: number) {
    if (from === to || reduced()) return;
    const start = performance.now();
    const ms = 320;
    const frame = (t: number) => {
      const k = Math.min(1, (t - start) / ms);
      shownRank = { ...shownRank, [userId]: Math.round(from + (to - from) * k) };
      if (k < 1) requestAnimationFrame(frame);
      else {
        const next = { ...shownRank };
        delete next[userId];
        shownRank = next;
      }
    };
    shownRank = { ...shownRank, [userId]: from };
    requestAnimationFrame(frame);
  }

  async function step() {
    if (busy || done) return;
    busy = true;
    const before: Record<number, number> = {};
    for (const el of list?.querySelectorAll<HTMLElement>('[data-user]') ?? []) before[Number(el.dataset.user)] = el.getBoundingClientRect().top;

    const result = revealStep(reveal);
    if (!result) {
      busy = false;
      return;
    }
    past = [...past, reveal];
    reveal = result.state;
    const { move } = result;
    const row = reveal.rows[move.to];
    void keepFocus();
    said =
      move.to < move.from
        ? `${row.name} moves from ${ordinalWord(move.rankFrom)} to ${ordinalWord(move.rankTo)} with ${row.total} points.`
        : `${row.name} stays ${ordinalWord(move.rankTo)} with ${row.total} points.`;
    await tick();

    if (reduced()) {
      flash = move.user_id;
      setTimeout(() => {
        if (flash === move.user_id) flash = null;
      }, 1000);
    } else {
      moving = move.user_id;
      // FLIP: start every moved row where it was, then let it slide home.
      for (const el of list?.querySelectorAll<HTMLElement>('[data-user]') ?? []) {
        const old = before[Number(el.dataset.user)];
        if (old === undefined) continue;
        const dy = old - el.getBoundingClientRect().top;
        if (Math.abs(dy) < 1) continue;
        el.style.transition = 'none';
        el.style.transform = `translateY(${dy}px)`;
      }
      requestAnimationFrame(() => {
        for (const el of list?.querySelectorAll<HTMLElement>('[data-user]') ?? []) {
          if (!el.style.transform) continue;
          el.style.transition = 'transform var(--dur-slow) var(--ease)';
          el.style.transform = '';
        }
      });
      countRank(move.user_id, move.rankFrom, move.rankTo);
    }
    setTimeout(
      () => {
        busy = false;
        if (moving === move.user_id) moving = null;
        scrollToNext();
      },
      reduced() ? 0 : 340,
    );
  }

  function onKeydown(event: KeyboardEvent) {
    // Space and Enter on another button (Exit, Back, Start over) do what that button says.
    const target = event.target as HTMLElement;
    if ((event.key === ' ' || event.key === 'Enter') && target.closest('button') && !target.closest('.next')) return;
    if (event.key === ' ' || event.key === 'ArrowRight' || event.key === 'Enter') {
      event.preventDefault();
      void step();
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault();
      back();
    }
  }

  function onCancel(event: Event) {
    event.preventDefault();
    onclose();
  }
</script>

<dialog bind:this={dialog} class="reveal" aria-labelledby="reveal-title" oncancel={onCancel} onkeydown={onKeydown}>
  <header class="top">
    <div>
      <h2 id="reveal-title" tabindex="-1">{title}</h2>
      <p class="hint">
        {#if done}All rows resolved. These are the final standings.{:else}Press Space or the right arrow to resolve the next row, the left
          arrow to go back. {left}
          {left === 1 ? 'row' : 'rows'} left.{/if}
      </p>
    </div>
    <div class="buttons">
      <button type="button" class="btn btn-primary next" disabled={done} onclick={step}>Next</button>
      <button type="button" class="btn btn-secondary back" disabled={past.length === 0} onclick={back}>Back</button>
      <button type="button" class="btn btn-quiet" disabled={past.length === 0} onclick={startOver}>Start over</button>
      <button type="button" class="btn btn-secondary exit" onclick={onclose}>Exit</button>
    </div>
  </header>
  <p class="visually-hidden" aria-live="polite">{said}</p>

  <div class="board">
    <div class="row head" aria-hidden="true">
      <span class="rank">#</span>
      <span class="name">Name</span>
      <span class="total">Score</span>
      {#each labels as l (l)}<span class="cell">{l}</span>{/each}
    </div>
    <ol class="rows" bind:this={list}>
      {#each reveal.rows as r (r.user_id)}
        <li
          class="row"
          class:next-row={nextRow?.user_id === r.user_id}
          class:resolved={r.resolved}
          class:flash={flash === r.user_id}
          class:moving={moving === r.user_id}
          data-user={r.user_id}
        >
          <span class="visually-hidden">Rank</span>
          <span class="rank mono">{shownRank[r.user_id] ?? r.rank}</span>
          <span class="visually-hidden">,</span>
          <span class="name">{r.name}</span>
          <span class="visually-hidden">,</span>
          <span class="total mono">{r.total}</span>
          <span class="visually-hidden">points,</span>
          {#each labels as l (l)}
            {@const v = cellView(cellFor(r, l), l)}
            <span class="cell tone-{v.tone}" class:pend={v.pending !== ''}>
              <span class="fill" style:width="{Math.round(v.fraction * 100)}%"></span>
              <span class="ctext mono" aria-hidden="true">{v.text}{#if v.pending}<span class="pending">{v.pending}</span>{/if}</span>
              <span class="visually-hidden">{v.sentence}</span>
            </span>
          {/each}
        </li>
      {/each}
    </ol>
  </div>
</dialog>

<style>
  .reveal {
    width: 100vw;
    max-width: none;
    height: 100dvh;
    max-height: none;
    margin: 0;
    padding: var(--space-5) var(--gutter);
    border: 0;
    background: var(--paper);
    color: var(--ink);
  }

  .reveal[open] {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    gap: var(--space-4);
  }

  .reveal::backdrop {
    background: var(--paper);
  }

  .top {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3) var(--space-5);
  }

  .top h2 {
    font-size: var(--fs-4);
  }

  .hint {
    color: var(--ink-muted);
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .board {
    min-height: 0;
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .rows {
    padding: 0;
    list-style: none;
  }

  .row {
    position: relative;
    display: grid;
    grid-template-columns: 4.5rem minmax(10rem, 3fr) 6rem;
    grid-auto-columns: minmax(5rem, 1fr);
    grid-auto-flow: column;
    align-items: center;
    min-width: max-content;
    border-bottom: var(--rule) solid var(--line);
    border-left: var(--space-1) solid transparent;
    background: var(--surface);
    font-size: var(--fs-3);
  }

  .row.head {
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--surface-2);
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .row > span:not(.visually-hidden) {
    padding: var(--space-2) var(--space-3);
  }

  .rank,
  .total {
    font-variant-numeric: tabular-nums;
    text-align: right;
  }

  .name {
    overflow: hidden;
    font-weight: 500;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cell {
    position: relative;
    align-self: stretch;
    display: grid;
    place-items: center;
  }

  .fill {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    padding: 0 !important;
  }

  .tone-ok .fill {
    background: var(--ok-tint);
  }

  .tone-part .fill {
    background: var(--part-tint);
  }

  .ctext {
    position: relative;
  }

  .pending {
    margin-left: var(--space-1);
    color: var(--ink-muted);
  }

  /* The next row to reveal is marked by a border, not an inset shadow (F9),
     so the mark survives forced colours. Every row carries the border,
     transparent, so the columns line up. */
  .row.next-row {
    border-left-color: var(--act);
  }

  @media (forced-colors: active) {
    .row {
      border-left-color: Canvas;
    }

    .row.next-row {
      border-left-color: Highlight;
    }
  }

  /* Still to come: muted names, pending cells on the waiting tint. */
  .row:not(.resolved):not(.head) .name {
    color: var(--ink-muted);
  }

  .cell.pend {
    background: var(--wait-tint);
  }

  /* Below the sticky head (2), above the rows the climbing row slides past. */
  .row.moving {
    z-index: 1;
  }

  /* Reduced motion: the resolved row is outlined above the cell fills. */
  .row.flash::after {
    position: absolute;
    inset: 0;
    z-index: 3;
    outline: 3px solid var(--act);
    outline-offset: -3px;
    content: '';
    pointer-events: none;
  }
</style>
