<!--
  RunResults: the outcome of "Run on samples" or "Run on this input". While
  the judge has it, it says where the run is in the queue. Then, for each
  input, the verdict in words with time and memory and, through
  TestDetail, the program's output and, for a sample, the expected output,
  the checker's comment and for a wrong answer the first differing line;
  a passed sample shows its verdict line only, so the failure is near the
  top.
  Compiler output comes first, verbatim, with clickable line jumps; when
  the code did not compile, the empty input rows are left out (audit
  2026-10-20 S4). After the samples come the problem's open tests (D115),
  "Open test 1" and on: a passed one shows its verdict only, one not run
  says why. The run's first failing sample or open test is revealed
  (`run.revealed`): its row becomes a block with "This test is shown
  because this was a run.", the input, the expected output, the program's
  output and "Run on this input". A failing sample without that offers the
  button too, with the sample's own input (D114). The button puts the
  input in the custom input box, opens it and runs it there. Props: `run`,
  `language` (for the compiler's location shapes), `onjump`, `samples`
  (the workspace's samples, for their inputs) and `onruninput` (optional;
  without it the button is left out).
-->
<script lang="ts">
  import {
    formatKb,
    formatMs,
    isBad,
    isCustomResult,
    revealedReason,
    runCompileFailed,
    runHeadline,
    runVerdictLabel,
    type Run,
    type RunResult,
    type Sample,
  } from '../lib/workspace';
  import CompileOutput from './CompileOutput.svelte';
  import TestDetail from './TestDetail.svelte';

  interface Props {
    run: Run;
    language: string;
    onjump: ((line: number, col: number | null) => void) | null;
    samples?: Sample[];
    onruninput?: (input: string) => void;
  }

  let { run, language, onjump, samples = [], onruninput }: Props = $props();

  const head = $derived(runHeadline(run));

  /** The input of the sample a result row ran, by its "Sample N" label. */
  function sampleInput(label: string): string | null {
    const m = /^Sample (\d+)$/.exec(label);
    if (!m) return null;
    return samples.find((x) => x.idx === Number(m[1]))?.input ?? null;
  }

  function isRevealed(r: RunResult): boolean {
    return !!run.revealed && run.revealed.label === r.label;
  }
</script>

<div class="run">
  <p class="headline tone-{head.tone}">{head.text}</p>

  {#if run.compile_output}
    <div class="block">
      <p class="label">Compiler output</p>
      <CompileOutput output={run.compile_output} {language} {onjump} />
    </div>
  {/if}

  <!-- A run that did not compile ran no input: the empty sample rows stay out (S4). -->
  {#each runCompileFailed(run) ? [] : run.results as r, i (i)}
    <section class="result">
      <div class="result-head">
        <h3 class="name">{r.label}</h3>
        <span class="verdict" class:bad={isBad(r.verdict)} class:ok={!isBad(r.verdict) && r.verdict !== 'SK'}>{runVerdictLabel(r.verdict)}</span>
        {#if r.verdict !== 'SK' && r.verdict !== ''}
          <span class="mono meta">{formatMs(r.time_ms)}</span>
          <span class="mono meta">{formatKb(r.memory_kb)}</span>
        {/if}
      </div>
      {#if r.message}<p class="message">{r.message}</p>{/if}

      {#if r.verdict === 'SK'}
        <p class="meta">An earlier test failed, so this one was not run.</p>
      {:else if run.revealed && isRevealed(r)}
        {@const rv = run.revealed}
        <TestDetail
          reason={revealedReason('run')}
          input={rv.input}
          expected={rv.expected}
          output={rv.output}
          cut={rv.truncated}
          verdict={r.verdict}
          runInput={rv.input}
          {onruninput}
        />
      {:else if isCustomResult(r) || (r.kind !== 'open' && isBad(r.verdict))}
        <TestDetail
          expected={r.expected}
          output={r.output}
          outputCut={r.output_truncated}
          verdict={r.verdict}
          runInput={isBad(r.verdict) ? sampleInput(r.label) : null}
          {onruninput}
        />
      {/if}
    </section>
  {/each}
</div>

<style>
  .run {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
    container: result / inline-size;
  }

  .headline {
    font-size: var(--fs-2);
    font-weight: 600;
    line-height: var(--lh-tight);
  }

  .tone-ok,
  .ok {
    color: var(--ok);
  }

  .tone-bad,
  .bad {
    color: var(--bad);
  }

  .tone-wait {
    color: var(--ink-muted);
  }

  .block {
    display: grid;
    gap: var(--space-1);
  }

  .label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .result {
    display: grid;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: var(--rule) solid var(--line);
    min-width: 0;
  }

  .result-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-3);
  }

  .name {
    font-size: var(--fs-1);
  }

  .verdict {
    font-weight: 500;
  }

  .meta {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .message {
    font-size: var(--fs-0);
  }
</style>
