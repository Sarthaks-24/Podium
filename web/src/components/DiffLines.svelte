<!--
  DiffLines: a line diff of two statements as plain text (lib/history.ts
  DiffLine), shared by the student's StatementDiff and the organiser's
  revision history. Removed lines are marked "-" and added lines "+" in
  the gutter, with "Removed:" and "Added:" in hidden text, so colour is
  never the only cue; long unchanged stretches fold into "12 unchanged
  lines". Text is never rendered as Markdown or HTML. Props: `lines`.
-->
<script lang="ts">
  import { diffRuns, type DiffLine } from '../lib/history';

  let { lines }: { lines: DiffLine[] } = $props();

  /** Unchanged runs longer than this fold, keeping a line of context each side. */
  const FOLD = 6;

  const runs = $derived(diffRuns(lines));
</script>

<div class="lines" role="group" aria-label="Statement lines">
  {#each runs as run, i (i)}
    {#if run.op === 'same' && run.lines.length > FOLD}
      <p class="line same"><span class="mark" aria-hidden="true"></span><span class="text">{run.lines[0]}</span></p>
      <details class="fold">
        <summary>{run.lines.length - 2} unchanged lines</summary>
        {#each run.lines.slice(1, -1) as text, j (j)}<p class="line same"><span class="mark" aria-hidden="true"></span><span class="text">{text}</span></p>{/each}
      </details>
      <p class="line same"><span class="mark" aria-hidden="true"></span><span class="text">{run.lines[run.lines.length - 1]}</span></p>
    {:else}
      {#each run.lines as text, j (j)}
        <p class="line {run.op}">
          <span class="mark mono" aria-hidden="true">{run.op === 'add' ? '+' : run.op === 'del' ? '-' : ''}</span>
          {#if run.op !== 'same'}<span class="visually-hidden">{run.op === 'add' ? 'Added: ' : 'Removed: '}</span>{/if}
          <span class="text">{text === '' ? ' ' : text}</span>
        </p>
      {/each}
    {/if}
  {/each}
</div>

<style>
  .lines {
    margin-top: var(--space-4);
    border: var(--rule) solid var(--line);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
  }

  .line {
    display: grid;
    grid-template-columns: 2ch minmax(0, 1fr);
    gap: var(--space-2);
    padding: 0 var(--space-2);
  }

  .text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .mark {
    color: var(--ink-muted);
    text-align: center;
  }

  .add {
    background: var(--ok-tint);
  }

  .add .mark {
    color: var(--ok);
  }

  .del {
    background: var(--bad-tint);
  }

  .del .mark {
    color: var(--bad);
  }

  .del .text {
    text-decoration: line-through;
    text-decoration-color: color-mix(in srgb, currentColor 45%, transparent);
  }

  .fold summary {
    padding: var(--space-1) var(--space-2);
    color: var(--ink-muted);
    font-family: var(--font-ui);
    cursor: pointer;
  }

  @media (forced-colors: active) {
    .add,
    .del {
      border-inline-start: calc(2 * var(--rule)) solid CanvasText;
    }
  }
</style>
