<!--
  Layout: the workspace's grid (design.md 5.3, F7.2 to F7.5; split out of
  Workspace in B2). From 1024 px three columns fill the window under the
  shell's measured header (--shell-head): the rail, the statement under the
  title, and the code column with the editor over the result, their rows
  set by the splitter (`rows`). From 1024 to 1279 px the rail is narrower
  and the code column wider, so the toolbar keeps one row; a laptop at most
  45rem tall (the `short` tier) gives back padding to the editor. Under
  1024 px the panes stack under the sticky strip and show one tab at a
  time (Problem and Code from 768 px, Problem, Code and Result under it).
  Props: `layout` ('wide', 'tablet' or 'phone'), `rows` (the code column's
  rows from 1024 px), `showStatement`, `showEditor`, `showResult`, `column`
  (bindable: the code column, which the splitter measures) and the
  snippets `side` (the rail) and `splitter` (from 1024 px), `strip` (under
  1024 px), `title`, `statement`, `code` (the editor pane) and `result`.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    layout: 'wide' | 'tablet' | 'phone';
    rows?: string;
    showStatement: boolean;
    showEditor: boolean;
    showResult: boolean;
    column?: HTMLDivElement;
    side?: Snippet;
    title: Snippet;
    strip?: Snippet;
    statement: Snippet;
    code: Snippet;
    splitter?: Snippet;
    result: Snippet;
  }

  let { layout, rows, showStatement, showEditor, showResult, column = $bindable(), side, title, strip, statement, code, splitter, result }: Props = $props();

  const wide = $derived(layout === 'wide');
</script>

<div class="ws" data-layout={layout}>
  {#if wide && side}<aside class="rail" aria-label="Problem">{@render side()}</aside>{/if}
  <!-- Under 1024 px the sticky row comes first, then the title and the statement at once (B4). -->
  {#if !wide && strip}{@render strip()}{/if}
  <div class="title-cell">{@render title()}</div>

  <section
    id="ws-problem"
    class="pane statement-pane"
    hidden={!showStatement}
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

  /* Three columns: rail, statement, code over verdict, filling the window. */
  @media (min-width: 1024px) {
    .ws {
      grid-template-columns: 10rem minmax(0, 1fr) minmax(22.5rem, 1.15fr);
      grid-template-rows: auto minmax(0, 1fr);
      grid-template-areas:
        'rail title code'
        'rail statement code';
      /* Sized from the shell (S2, F7.3): the header's real height, testing
         banner included, comes from App.svelte as --shell-head. 18rem only
         keeps a zoomed-in window usable. */
      height: calc(100dvh - var(--shell-head, var(--space-7)) - 2 * var(--space-3));
      min-height: 18rem;
      border: var(--rule) solid var(--line);
      background: var(--surface);
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

    /* The rows come from the splitter (F7.5) as an inline style; this is the shape before it. */
    .code-col {
      grid-area: code;
      grid-template-rows: minmax(0, 3fr) auto minmax(0, 2fr);
      border-left: var(--rule) solid var(--line);
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
      padding: var(--sticky-top) var(--space-4) var(--space-4);
      overflow-y: auto;
    }
  }

  /* S6: a wider rail from 1280 px, so most titles fit on their two lines. */
  @media (min-width: 1280px) {
    .ws {
      grid-template-columns: 13rem minmax(0, 1fr) minmax(22.5rem, 1.15fr);
    }
  }

  /* Under 1280 px, at any height (1366 by 768 at 125 percent is 1093 px
     wide; audit S2 and council C1-042): a narrower rail and a wider code
     column, so the toolbar keeps one row. */
  @media (1024px <= width < 1280px) {
    .ws {
      grid-template-columns: 10rem minmax(0, 1fr) minmax(22.5rem, 1.4fr);
    }

    .rail {
      padding-inline: var(--space-3);
    }

    .editor-pane {
      padding-inline: var(--space-2);
    }
  }

  /* The short tier (F7.3): every pane gives back padding to the editor. */
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
