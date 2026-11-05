<!--
  Layout: the workspace's grid. From 1024 px three columns fill the window under the
  shell's measured header (--shell-head): the rail, the statement under the
  title, and the code column with the editor over the result, their rows
  set by the splitter (`rows`). A column splitter (`colSplit`) sits between
  the statement and the code on every page, sized by `cols` (the rail keeps
  its own width); with `statementHidden` the code takes the whole width. From
  1024 to 1279 px the rail is narrower, and from 1280 px it is wider; a laptop
  at most 45rem tall (the `short` tier) gives back padding to the editor.
  Under 1024 px the panes stack under the sticky strip and show one tab at a
  time (Problem and Code from 768 px, Problem, Code and Result under it).
  In a contest the `bar` takes the rail's place from 1024 px: one row
  across the top, then the title and the statement beside the code.
  Props: `layout` ('wide', 'tablet' or 'phone'), `rows` (the code column's
  rows from 1024 px), `cols` (the statement, splitter and code columns),
  `showStatement`, `showEditor`, `showResult`, `column` (bindable: the code
  column, which the splitter measures), `grid` (bindable) and the
  snippets `side` (the rail) and `splitter` (from 1024 px), `strip` (under
  1024 px), `title`, `statement`, `code` (the editor pane) and `result`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    layout: 'wide' | 'tablet' | 'phone';
    rows?: string;
    cols?: string;
    statementHidden?: boolean;
    bar?: Snippet;
    colSplit?: Snippet;
    showStatement: boolean;
    showEditor: boolean;
    showResult: boolean;
    column?: HTMLDivElement;
    grid?: HTMLDivElement;
    side?: Snippet;
    title: Snippet;
    strip?: Snippet;
    statement: Snippet;
    code: Snippet;
    splitter?: Snippet;
    result: Snippet;
  }

  let { layout, rows, cols, statementHidden = false, bar, colSplit, showStatement, showEditor, showResult, column = $bindable(), grid = $bindable(), side, title, strip, statement, code, splitter, result }: Props = $props();

  const wide = $derived(layout === 'wide');
  const withBar = $derived(wide && !!bar);
  // The rail keeps its width (--rail-w) beside the columns the splitter sets.
  const template = $derived(!wide || !cols ? undefined : withBar ? cols : `var(--rail-w) ${cols}`);
</script>

<div bind:this={grid} class="ws" class:with-bar={withBar} class:no-statement={wide && statementHidden} data-layout={layout} style:grid-template-columns={template}>
  {#if withBar && bar}
    <div class="bar-cell">{@render bar()}</div>
  {:else if wide && side}
    <aside class="rail" aria-label="Problem">{@render side()}</aside>
  {/if}
  <!-- Under 1024 px the sticky row comes first, then the title and the statement at once. -->
  {#if !wide && strip}{@render strip()}{/if}
  <div class="title-cell">{@render title()}</div>

  {#if wide && colSplit && !statementHidden}<div class="col-split">{@render colSplit()}</div>{/if}
  <section
    id="ws-problem"
    class="pane statement-pane"
    hidden={!showStatement || (wide && statementHidden)}
    role={wide ? undefined : 'tabpanel'}
    aria-labelledby={wide ? undefined : 'ws-tab-problem'}
    aria-label={wide ? 'Statement' : undefined}
  >
    {@render statement()}
  </section>

  <div class="code-col" bind:this={column} style:grid-template-rows={rows}>
    <section
      id="ws-code"
      class="pane editor-pane"
      hidden={!showEditor}
      role={wide ? undefined : 'tabpanel'}
      aria-labelledby={wide ? undefined : 'ws-tab-code'}
      aria-label={wide ? 'Code' : undefined}
    >
      {@render code()}
    </section>
    {#if wide && splitter}{@render splitter()}{/if}
    <section
      id="ws-result"
      class="pane result-pane"
      hidden={!showResult}
      role={layout === 'phone' ? 'tabpanel' : undefined}
      aria-labelledby={layout === 'phone' ? 'ws-tab-result' : undefined}
      aria-label={layout === 'phone' ? undefined : 'Result'}
    >
      {@render result()}
    </section>
  </div>
</div>

<style>
  .ws {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-width: 0;
  }

  .ws :global([hidden]) {
    display: none !important;
  }

  .pane,
  .title-cell {
    min-width: 0;
  }

  .statement-pane {
    padding-top: var(--space-5);
  }

  .code-col {
    display: grid;
    min-width: 0;
  }

  .editor-pane {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    align-content: start;
    padding-top: var(--space-4);
  }

  .result-pane {
    display: grid;
    align-content: start;
    gap: var(--space-4);
    padding-block: var(--space-5);
  }

  .rail {
    display: grid;
    align-content: start;
    gap: var(--space-5);
    padding: var(--space-4);
    border-right: var(--rule) solid var(--line);
    background: var(--surface-2);
    overflow-y: auto;
  }

  /* Tablet: the result follows the editor in the Code tab. */
  @media (768px <= width < 1024px) {
    .result-pane {
      border-top: var(--rule) solid var(--line);
      margin-top: var(--space-4);
    }
  }

  /* Rail, statement, splitter and code over verdict, filling the window. The
     columns come from the splitter as an inline style; this is the shape before it. */
  @media (min-width: 1024px) {
    .ws {
      --rail-w: 10rem;
      grid-template-columns: var(--rail-w) minmax(0, 40fr) auto minmax(22.5rem, 60fr);
      grid-template-rows: auto minmax(0, 1fr);
      grid-template-areas:
        'rail title split code'
        'rail statement split code';
      /* Sized from the shell: the header's real height, testing
         banner included, comes from App.svelte as --shell-head. 18rem only
         keeps a zoomed-in window usable. */
      height: calc(100dvh - var(--shell-head, var(--space-7)) - 2 * var(--space-3) - env(safe-area-inset-top) - env(safe-area-inset-bottom));
      min-height: 18rem;
      border: var(--rule) solid var(--line);
      background: var(--surface);
    }

    .ws.no-statement {
      grid-template-areas:
        'rail title'
        'rail code';
    }

    .rail {
      grid-area: rail;
    }

    .title-cell {
      grid-area: title;
      padding: var(--space-4) var(--space-5) var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .statement-pane {
      /* Positioned, so absolutely placed hidden text inside scrolls with it instead of stretching the page. */
      position: relative;
      grid-area: statement;
      padding: var(--space-4) var(--space-5) var(--space-6);
      overflow-y: auto;
    }

    .col-split {
      grid-area: split;
      display: grid;
      border-left: var(--rule) solid var(--line);
    }

    /* The rows come from the splitter as an inline style; this is the shape before it. */
    .code-col {
      grid-area: code;
      grid-template-rows: minmax(0, 65fr) auto minmax(0, 35fr);
      min-height: 0;
    }

    .editor-pane {
      min-height: 0;
      padding: var(--space-2) var(--space-3) var(--space-1);
      container: code / inline-size;
    }

    .result-pane {
      --sticky-top: var(--space-3);
      --sticky-bleed: var(--space-4);
      display: flex;
      flex-direction: column;
      padding: var(--sticky-top) var(--space-4) var(--space-4);
      overflow-y: auto;
    }

    .result-pane > :global(*) {
      flex: none;
    }
  }

  /* In a contest the bar runs across the top. */
  @media (min-width: 1024px) {
    .ws.with-bar {
      grid-template-columns: minmax(0, 40fr) auto minmax(22.5rem, 60fr);
      grid-template-rows: auto auto minmax(0, 1fr);
      grid-template-areas:
        'bar bar bar'
        'title split code'
        'statement split code';
    }

    .ws.with-bar.no-statement {
      grid-template-areas:
        'bar'
        'title'
        'code';
    }

    .bar-cell {
      grid-area: bar;
      padding-block: var(--space-1);
      border-bottom: var(--rule) solid var(--line);
      background: var(--surface-2);
    }
  }

  /* A wider rail from 1280 px, so most titles fit on their two lines. */
  @media (min-width: 1280px) {
    .ws {
      --rail-w: 13rem;
    }
  }

  /* Under 1280 px, at any height (1366 by 768 at 125 percent is 1093 px
     wide): a narrower rail, so the code keeps its width. */
  @media (1024px <= width < 1280px) {
    .rail {
      padding-inline: var(--space-3);
    }

    .editor-pane {
      padding-inline: var(--space-2);
    }
  }

  /* The short tier: every pane gives back padding to the editor. */
  @media (min-width: 1024px) and (max-height: 45rem) {
    .title-cell {
      padding-block: var(--space-2);
    }

    .statement-pane {
      padding-top: var(--space-3);
    }

    .rail {
      gap: var(--space-3);
      padding-block: var(--space-3);
    }

    .editor-pane {
      padding-top: var(--space-1);
    }

    .result-pane {
      --sticky-top: var(--space-2);
    }
  }
</style>
