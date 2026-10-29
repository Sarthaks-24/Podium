<!--
  ProblemRail: a contest's problems and its links inside the workspace, in
  one of two shapes (B4, B6, design/student.md).
  - "bar", from 1024 px, in place of the site header: Leave, the contest's
    title, the letters with their scores (scrolling sideways past six, the
    open one kept in view), the clock, then Messages with its unread count,
    Standings and Help.
  - "strip", under 1024 px, the one sticky row: the letters with their
    scores, the clock and a Menu holding Standings, Messages, Help and
    Leave, so the statement starts at once.
  Each letter shows the score or "-" over a 2 px bar, in the partial colour for a partial
  score; the open problem has a border on its inline start (a shape, not
  only a tint, audit 06-7); a letter whose statement changed since the
  student last opened it carries a dot. Each link keeps one sentence for
  screen readers (sentence()). Leave asks first while the contest is live
  (`onleave`), otherwise it is a link home.
  Props: `problems`, `slug`, `current` (the open label), `layout` ("bar"
  or "strip"), `unread` (messages not yet seen), `onmessages`,
  `onhelp`, `onstandings` (a panel instead of the standings page), `clock`
  (a snippet), `contestTitle`, `changed` (letters whose statement changed)
  and `onleave`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ContestProblem } from '../lib/contests';
  import { dismissable } from '../lib/dismissable';
  import Icon from './Icon.svelte';

  interface Props {
    problems: ContestProblem[];
    slug: string;
    current: string;
    layout: string;
    unread?: number;
    onmessages: () => void;
    onhelp?: () => void;
    onstandings?: () => void;
    clock?: Snippet;
    contestTitle?: string;
    changed?: ReadonlySet<string>;
    onleave?: () => void;
  }

  let { problems, slug, current, layout, unread = 0, onmessages, onhelp, onstandings, clock, contestTitle = '', changed = new Set<string>(), onleave }: Props = $props();

  const base = $derived(`/contests/${encodeURIComponent(slug)}`);
  // Past six letters the row scrolls; the open one stays in view (B6).
  function keepInView(node: HTMLElement, isCurrent: boolean) {
    if (isCurrent) node.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    return {
      update(now: boolean) {
        if (now) node.scrollIntoView({ block: 'nearest', inline: 'nearest' });
      },
    };
  }

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

  function scoreText(p: ContestProblem): string {
    if (p.my_score !== null) return `${p.my_score}/${p.max_score}`;
    return '-';
  }

  function sentence(p: ContestProblem): string {
    return baseSentence(p) + (changed.has(p.label) ? ', statement changed' : '');
  }

  function baseSentence(p: ContestProblem): string {
    if (p.my_score !== null) return `Problem ${p.label}, ${p.title}, ${p.my_score} of ${p.max_score} points`;
    return `Problem ${p.label}, ${p.title}, not tried`;
  }

  function percent(p: ContestProblem): number {
    return p.my_score !== null && p.max_score > 0 ? Math.round((p.my_score / p.max_score) * 100) : 0;
  }

  function partial(p: ContestProblem): boolean {
    return p.my_score !== null && p.my_score > 0 && p.my_score < p.max_score;
  }
</script>

{#snippet chips()}
  <nav class="chips" aria-label="Problems">
    <ul class="problems">
      {#each problems as p (p.label)}
        <li>
          <a class="problem" href={href(p.label)} aria-current={p.label === current ? 'page' : undefined} aria-label={sentence(p)} title={p.title} use:keepInView={p.label === current}>
            <span class="mono letter">{p.label}</span>
            <span class="mono score" class:sent={scoreText(p) === 'Submitted'}>{scoreText(p)}</span>
            {#if changed.has(p.label)}<span class="changed" aria-hidden="true"></span>{/if}
            <span class="bar" aria-hidden="true"><span class="fill" class:part={partial(p)} style:width="{percent(p)}%"></span></span>
          </a>
        </li>
      {/each}
    </ul>
  </nav>
{/snippet}

{#snippet leave(cls: string)}
  {#if onleave}<button type="button" class={cls} onclick={() => pick(onleave)}>Leave</button>{:else}<a class={cls} href="/">Leave</a>{/if}
{/snippet}

{#if layout === 'bar'}
  <div class="contest-bar">
    {@render leave('btn btn-quiet btn-compact leave')}
    <p class="contest-title">{contestTitle}</p>
    {@render chips()}
    {#if clock}<div class="row-clock">{@render clock()}</div>{/if}
    <div class="bar-links">
      <button type="button" class="btn btn-quiet btn-compact messages" onclick={onmessages}>
        <Icon name="message-square" />
        Messages{#if unread > 0}<span class="mono count">{unread}</span><span class="visually-hidden">&nbsp;unread</span>{/if}
      </button>
      {#if onstandings}
        <button type="button" class="btn btn-quiet btn-compact" onclick={onstandings}>Standings</button>
      {:else}
        <a class="btn btn-quiet btn-compact" href="{base}/standings?from={encodeURIComponent(current)}">Standings</a>
      {/if}
      {#if onhelp}<button type="button" class="btn btn-quiet btn-compact" aria-keyshortcuts="?" onclick={onhelp}>Help</button>{/if}
    </div>
  </div>
{:else if layout === 'strip'}
  <div class="row">
    {@render chips()}
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
        <li>{@render leave('item')}</li>
      </ul>
    </div>
  </div>
{/if}

<style>

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

  .count {
    min-width: 1.5em;
    padding: 0 var(--space-1);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
    font-size: var(--fs-0);
    text-align: center;
  }

  /* The contest bar from 1024 px (B6): Leave and the title, the letters, the clock, the links. */
  .contest-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
    padding-inline: var(--space-3);
  }

  .contest-title {
    flex: none;
    max-width: 14rem;
    overflow: hidden;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .contest-bar .chips {
    flex: 1;
    min-width: 0;
  }

  .bar-links {
    display: flex;
    flex: none;
    align-items: center;
    gap: var(--space-1);
  }

  .changed {
    position: absolute;
    top: var(--space-1);
    right: var(--space-1);
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: var(--act);
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
