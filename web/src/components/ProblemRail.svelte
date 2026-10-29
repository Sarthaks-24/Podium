<!--
  ProblemRail: the contest's problems in the workspace rail (design.md
  5.3). From 1024 px each row is the letter, the title (up to two lines,
  then cut, whole on hover; R4, audit S6), the score cell in mono and a 2 px bar of earned over
  total, in the partial colour for a partial score; the open problem is
  marked by a border on its inline start (a shape, not only a tint, audit
  06-7). The score cell shows the score, or "not tried" without one
  (S1-09), and in exam mode "not submitted" or "submitted" until scores
  are out.
  Below 1024 px the same links sit in one row of letters with their scores
  ("-" for none), which scrolls sideways when there are many. Under the
  problems come Standings (a panel over the editor during the contest, B3,
  else the standings page), Help (the rules, Run and Submit, the keys) and
  the Messages button with its unread count, all quiet buttons with no
  underline (the control system in styles/controls.css). Each link keeps one sentence for
  screen readers (sentence()).
  With `layout` "strip" it draws the one sticky row under 1024 px (B4,
  V3-14): the problem letters with their scores (scrolling sideways when
  there are many), the contest clock (`clock`), and a Menu holding
  Standings, Messages with its count, Help and Leave, so the site header
  and the link row can go and the statement starts at once.
  Props: `problems`, `slug`, `current` (the open label), `clock` (a
  snippet for the strip, optional), `layout` ("wide",
  "tablet", "phone" or "strip"), `exam`, `unread` (messages not yet seen),
  `onmessages` (opens the messages panel), `onhelp` (optional; opens Help)
  and `onstandings` (optional; opens the standings panel instead of the
  page).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { railStatus, type ContestProblem } from '../lib/contests';
  import { dismissable } from '../lib/dismissable';
  import Icon from './Icon.svelte';

  interface Props {
    problems: ContestProblem[];
    slug: string;
    current: string;
    layout: string;
    exam?: boolean;
    unread?: number;
    onmessages: () => void;
    onhelp?: () => void;
    onstandings?: () => void;
    clock?: Snippet;
  }

  let { problems, slug, current, layout, exam = false, unread = 0, onmessages, onhelp, onstandings, clock }: Props = $props();

  const base = $derived(`/contests/${encodeURIComponent(slug)}`);
  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement | undefined = $state();

  /** Closes the menu, gives focus back to its button, then runs the item (the dialog returns focus there, P8). */
  function pick(fn: (() => void) | undefined) {
    menuOpen = false;
    menuButton?.focus();
    fn?.();
  }

  function href(label: string): string {
    return `${base}/problems/${encodeURIComponent(label)}`;
  }

  function count(p: ContestProblem): number {
    return p.my_submission_count ?? 0;
  }

  function scoreText(p: ContestProblem): string {
    if (p.my_score !== null) return `${p.my_score}/${p.max_score}`;
    if (exam && count(p) > 0) return 'Submitted';
    return '-';
  }

  function sentence(p: ContestProblem): string {
    if (p.my_score !== null) return `Problem ${p.label}, ${p.title}, ${p.my_score} of ${p.max_score} points`;
    if (exam) {
      const n = count(p);
      if (n === 0) return `Problem ${p.label}, ${p.title}, not submitted`;
      return `Problem ${p.label}, ${p.title}, submitted ${n === 1 ? 'once' : `${n} times`}, score after the contest`;
    }
    return `Problem ${p.label}, ${p.title}, not tried`;
  }

  function percent(p: ContestProblem): number {
    return p.my_score !== null && p.max_score > 0 ? Math.round((p.my_score / p.max_score) * 100) : 0;
  }

  function partial(p: ContestProblem): boolean {
    return p.my_score !== null && p.my_score > 0 && p.my_score < p.max_score;
  }
</script>

{#if layout === 'strip'}
  <div class="row">
    <nav class="chips" aria-label="Problems">
      <ul class="problems">
        {#each problems as p (p.label)}
          <li>
            <a class="problem" href={href(p.label)} aria-current={p.label === current ? 'page' : undefined} aria-label={sentence(p)} title={p.title}>
              <span class="mono letter">{p.label}</span>
              <span class="mono score" class:sent={scoreText(p) === 'Submitted'}>{scoreText(p)}</span>
              <span class="bar" aria-hidden="true"><span class="fill" class:part={partial(p)} style:width="{percent(p)}%"></span></span>
            </a>
          </li>
        {/each}
      </ul>
    </nav>
    {#if clock}<div class="row-clock">{@render clock()}</div>{/if}
    <div class="menu" use:dismissable={{ open: menuOpen, onclose: () => (menuOpen = false), trigger: menuButton }}>
      <button type="button" class="btn btn-quiet btn-compact" aria-expanded={menuOpen} aria-controls="strip-menu" bind:this={menuButton} onclick={() => (menuOpen = !menuOpen)}>
        Menu{#if unread > 0}<span class="mono count">{unread}</span><span class="visually-hidden">&nbsp;unread messages</span>{/if}
      </button>
      <ul id="strip-menu" class="menu-panel" hidden={!menuOpen}>
        <li>
          {#if onstandings}<button type="button" class="item" onclick={() => pick(onstandings)}>Standings</button>{:else}<a class="item" href="{base}/standings?from={encodeURIComponent(current)}">Standings</a>{/if}
        </li>
        <li>
          <button type="button" class="item" onclick={() => pick(onmessages)}>
            Messages{#if unread > 0}<span class="mono count">{unread}</span><span class="visually-hidden">&nbsp;unread</span>{/if}
          </button>
        </li>
        {#if onhelp}<li><button type="button" class="item" onclick={() => pick(onhelp)}>Help</button></li>{/if}
        <li><a class="item" href="/">Leave</a></li>
      </ul>
    </div>
  </div>
{:else}
<nav class="rail-nav" class:wide={layout === 'wide'} aria-label="Problems">
  <ul class="problems">
    {#each problems as p (p.label)}
      <li>
        <a
          class="problem"
          href="{base}/problems/{encodeURIComponent(p.label)}"
          aria-current={p.label === current ? 'page' : undefined}
          aria-label={sentence(p)}
          title={p.title}
        >
          <span class="mono letter">{p.label}</span>
          {#if layout === 'wide'}
            {@const st = railStatus(p, exam)}
            <span class="title">{p.title}</span>
            <span class="cell">
              {#if st.score}<span class="mono score">{st.score}</span>{/if}
              {#if st.word}<span class="word" class:alone={!st.score}>{st.word}</span>{/if}
            </span>
          {:else}
            <span class="mono score" class:sent={scoreText(p) === 'Submitted'}>{scoreText(p)}</span>
          {/if}
          <span class="bar" aria-hidden="true"><span class="fill" class:part={partial(p)} style:width="{percent(p)}%"></span></span>
        </a>
      </li>
    {/each}
  </ul>
  <div class="links">
    {#if onstandings}
      <button type="button" class="btn btn-link btn-compact" onclick={onstandings}>Standings</button>
    {:else}
      <a class="btn btn-link btn-compact" href="{base}/standings{current ? `?from=${encodeURIComponent(current)}` : ''}">Standings</a>
    {/if}
    {#if onhelp}<button type="button" class="btn btn-link btn-compact" onclick={onhelp}>Help</button>{/if}
    <button type="button" class="btn btn-link btn-compact messages" onclick={onmessages}>
      <Icon name="message-square" />
      Messages{#if unread > 0}<span class="mono count">{unread}</span><span class="visually-hidden">&nbsp;unread</span>{/if}
    </button>
  </div>
</nav>
{/if}

<style>
  .rail-nav {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
    margin-top: var(--space-3);
  }

  .problems {
    display: flex;
    gap: var(--space-1);
    min-width: 0;
    max-width: 100%;
    padding: 0;
    overflow-x: auto;
    list-style: none;
  }

  .problem {
    position: relative;
    display: inline-grid;
    grid-template-columns: auto auto;
    align-items: baseline;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding: var(--space-2) var(--space-3) var(--space-3);
    border-radius: var(--radius);
    color: var(--ink);
    text-decoration: none;
    white-space: nowrap;
  }

  .problem:hover {
    background: var(--surface-2);
  }

  /* State is a shape (F9, audit 06-7): the open problem has a border on its
     inline start, which forced colours keep, besides the tint. */
  .problem {
    border-inline-start: calc(2 * var(--rule)) solid transparent;
  }

  .problem[aria-current='page'] {
    border-inline-start-color: var(--act);
    background: var(--act-tint);
    font-weight: 600;
  }

  .letter {
    font-weight: 400;
  }

  .score {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .score.sent {
    font-family: var(--font-ui);
  }

  .bar {
    position: absolute;
    right: var(--space-3);
    bottom: var(--space-2);
    left: var(--space-3);
    height: calc(2 * var(--rule));
    background: var(--line);
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--ok);
  }

  /* A partial score fills in the partial colour, as standings cells do (audit 01-15). */
  .fill.part {
    background: var(--part);
  }

  .cell {
    display: grid;
    justify-items: end;
    line-height: var(--lh-tight);
  }

  /* Words in the interface face; mono is for the digits (design.md amendment). */
  .word {
    color: var(--ink-muted);
    font-family: var(--font-ui);
    font-size: var(--fs-0);
    white-space: nowrap;
  }

  .word:not(.alone) {
    font-size: calc(var(--fs-0) * 0.9);
  }


  .links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .count {
    min-width: 1.5em;
    padding: 0 var(--space-1);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
    font-size: var(--fs-0);
    text-align: center;
  }

  /* The rail from 1024 px: one row per problem, links below. */
  .wide {
    display: grid;
    gap: var(--space-5);
    margin-top: 0;
  }

  .wide .problems {
    display: grid;
    gap: var(--space-1);
    overflow: visible;
  }

  .wide .problem {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    width: 100%;
    min-height: var(--control-h);
  }

  /* R4, audit S6: up to two lines, then cut; the whole title is on hover and in the link's name. */
  .wide .title {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    font-size: var(--fs-0);
    line-height: var(--lh-tight);
    white-space: normal;
    overflow-wrap: break-word;
  }

  /* The rail is 10rem under 1280 px: the status goes under the title, which keeps the width. */
  @media (width < 1280px) {
    .wide .problem {
      grid-template-columns: auto minmax(0, 1fr);
    }

    .wide .cell {
      display: flex;
      grid-column: 2;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 0 var(--space-2);
      justify-items: start;
    }
  }

  .wide .links {
    display: grid;
    justify-items: start;
    gap: var(--space-1);
  }

  /* Quiet buttons line their words up with the rail's edge. */
  .wide .links .btn {
    margin-inline-start: calc(-1 * var(--space-3));
  }

  /* The one row under 1024 px (B4): the letters scroll, the clock and Menu stay. */
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    width: 100%;
  }

  .chips {
    flex: 1;
    min-width: 0;
  }

  .row .problem {
    padding-inline: var(--space-2);
  }

  .row-clock {
    flex: none;
  }

  .menu {
    position: relative;
    flex: none;
  }

  .menu-panel {
    position: absolute;
    inset-inline-end: 0;
    top: calc(100% + var(--space-1));
    z-index: var(--z-bar);
    display: grid;
    min-width: 12rem;
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
  }

  .menu-panel[hidden] {
    display: none;
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: var(--control-h-touch);
    padding-inline: var(--space-3);
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--ink);
    font: inherit;
    text-align: start;
    text-decoration: none;
  }

  .item:hover {
    background: var(--surface-2);
  }

  /* Forced colours drop the tint and the bar fills: the current problem
     gets an outline and the bars keep their track and fill. */
  @media (forced-colors: active) {
    .problem {
      border-inline-start-color: Canvas;
    }

    .problem[aria-current='page'] {
      border-inline-start-color: Highlight;
      outline: calc(2 * var(--rule)) solid Highlight;
    }

    .bar {
      outline: var(--rule) solid CanvasText;
    }

    .fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }
</style>
