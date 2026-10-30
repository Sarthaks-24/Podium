<!--
  TestDetail: what a student may see of one test, under its row in
  RunResults or under the verdict in VerdictPanel. For a wrong answer it
  starts with the first differing line, whitespace made visible
  (lib/diff.ts), then the program's output beside the expected one. When
  the test is revealed (a practice submission's first failing hidden
  test, or a run's first failing example) it is a bordered block that
  adds the input; "Run on this input"
  puts that input in the custom input box and runs it, unless the
  input was cut at 4 KB, when running it would not be the same test.
  Props: `heading` (an h3 for the block, optional), `revealed` (draws
  the bordered block; set only for a revealed test), `input`
  (optional), `expected` (null for a custom input), `output`,
  `outputCut` (only the start of the output is shown), `cut` (any of the
  texts was cut at 4 KB), `verdict` (the test's verdict code, for the
  difference), `stderr` (the end of what the program printed to stderr, a
  run only, optional), `runInput` (the input "Run on this input" uses,
  optional) and `onruninput` (optional; without it the button is left
  out).
-->
<script lang="ts">
  import { firstDifference, showWhitespace } from '../lib/diff';

  interface Props {
    heading?: string;
    revealed?: boolean;
    input?: string | null;
    expected: string | null;
    output: string;
    outputCut?: boolean;
    cut?: boolean;
    verdict: string;
    stderr?: string;
    runInput?: string | null;
    onruninput?: (input: string) => void;
  }

  let { heading = '', revealed = false, input = null, expected, output, outputCut = false, cut = false, verdict, stderr = '', runInput = null, onruninput }: Props = $props();

  const diff = $derived(expected !== null && verdict === 'WA' ? firstDifference(expected, output) : null);
  const canRun = $derived(onruninput !== undefined && runInput !== null && !cut);
</script>

<div class="detail" class:revealed data-testid={revealed ? 'revealed-test' : undefined}>
  {#if heading}<h3 class="heading">{heading}</h3>{/if}

  {#if input !== null}
    <div class="out">
      <p class="label">Input</p>
      <pre translate="no">{input}</pre>
    </div>
  {/if}

  {#if diff}
    <div class="diff">
      <p class="diff-title">First difference on line {diff.line}</p>
      <dl>
        <dt>Expected</dt>
        <dd class="mono">{diff.expected === null ? '(no more lines)' : showWhitespace(diff.expected) || '(empty line)'}</dd>
        <dt>Your output</dt>
        <dd class="mono bad">{diff.actual === null ? '(no more lines)' : showWhitespace(diff.actual) || '(empty line)'}</dd>
      </dl>
      <p class="hint">Spaces show as a middle dot and tabs as an arrow.</p>
    </div>
  {/if}

  <div class="outputs" class:pair={expected !== null}>
    {#if expected !== null}
      <div class="out">
        <p class="label">Expected</p>
        <pre translate="no">{expected}</pre>
      </div>
    {/if}
    <div class="out">
      <p class="label">Your output</p>
      <pre translate="no">{output}</pre>
      {#if outputCut && !cut}<p class="hint">Only the first 4 KB is shown.</p>{/if}
    </div>
  </div>
  {#if stderr}
    <div class="out">
      <p class="label">Error output (stderr), the last lines</p>
      <pre translate="no">{stderr}</pre>
    </div>
  {/if}
  {#if cut}<p class="hint">This test is longer than 4 KB, so only the start of each part is shown and it cannot be run from here.</p>{/if}

  {#if canRun && onruninput}
    <button type="button" class="btn btn-link btn-compact use-input" onclick={() => onruninput(runInput ?? '')}>Run on this input</button>
  {/if}
</div>

<style>
  .detail {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    container: detail / inline-size;
  }

  /* A revealed test is one bordered block: one panel per task area. */
  .revealed {
    padding: var(--space-3);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .heading {
    font-size: var(--fs-1);
  }

  .label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .use-input {
    justify-self: start;
    margin-inline-start: calc(-1 * var(--space-3));
  }

  .diff {
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .revealed .diff {
    background: var(--surface-2);
  }

  .diff-title {
    font-weight: 500;
  }

  .diff dl {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-1) var(--space-3);
    margin-top: var(--space-1);
    font-size: var(--fs-0);
  }

  .diff dt {
    color: var(--ink-muted);
  }

  .diff dd {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .bad {
    color: var(--bad);
  }

  .hint {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .outputs {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .out {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }

  .out pre {
    max-height: calc(10 * var(--lh-body) * var(--fs-0));
    min-height: calc(var(--lh-body) * var(--fs-0) + 2 * var(--space-2));
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    font-size: var(--fs-0);
  }

  @container detail (min-width: 30rem) {
    .outputs.pair {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
</style>
