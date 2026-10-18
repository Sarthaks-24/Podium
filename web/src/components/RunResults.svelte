<!--
  RunResults: the outcome of "Run on samples" or "Run on this input". While
  the judge has it, it says where the run is in the queue. Then, for each
  input, the verdict in words with time and memory, the program's output
  and, for a sample, the expected output and the checker's comment. A wrong
  answer on a sample also shows the first differing line with whitespace
  made visible (lib/diff.ts). Compiler output comes first, verbatim, with
  clickable line jumps. Props: `run`, `language` (for the compiler's
  location shapes) and `onjump`.
-->
<script lang="ts">
  import { firstDifference, showWhitespace } from '../lib/diff';
  import { formatKb, formatMs, isBad, isWaiting, runVerdictLabel, waitingText, type Run } from '../lib/workspace';
  import CompileOutput from './CompileOutput.svelte';

  interface Props {
    run: Run;
    language: string;
    onjump: ((line: number, col: number | null) => void) | null;
  }

  let { run, language, onjump }: Props = $props();

  const samples = $derived(run.results.filter((r) => r.expected !== null));
  const passed = $derived(samples.filter((r) => !isBad(r.verdict)).length);

  const head = $derived.by(() => {
    if (isWaiting(run)) return { text: run.state === 'running' ? 'Running' : waitingText(run), tone: 'wait' };
    if (run.state === 'error') return { text: 'The run could not finish. Try again in a moment.', tone: 'bad' };
    if (run.results.length === 0 && run.compile_output) return { text: 'Compile error', tone: 'bad' };
    if (samples.length > 0) {
      return {
        text: `Passed ${passed} of ${samples.length} ${samples.length === 1 ? 'sample' : 'samples'}`,
        tone: passed === samples.length ? 'ok' : 'bad',
      };
    }
    const r = run.results[0];
    return r ? { text: runVerdictLabel(r.verdict), tone: isBad(r.verdict) ? 'bad' : 'ok' } : { text: 'Finished', tone: 'ok' };
  });
</script>

<div class="run">
  <p class="headline tone-{head.tone}">{head.text}</p>

  {#if run.compile_output}
    <div class="block">
      <p class="label">Compiler output</p>
      <CompileOutput output={run.compile_output} {language} {onjump} />
    </div>
  {/if}

  {#each run.results as r, i (i)}
    {@const diff = r.expected !== null && r.verdict === 'WA' ? firstDifference(r.expected, r.output) : null}
    <section class="result">
      <div class="result-head">
        <h3 class="name">{r.label}</h3>
        <span class="verdict" class:bad={isBad(r.verdict)} class:ok={!isBad(r.verdict)}>{runVerdictLabel(r.verdict)}</span>
        <span class="mono meta">{formatMs(r.time_ms)}</span>
        <span class="mono meta">{formatKb(r.memory_kb)}</span>
      </div>
      {#if r.message}<p class="message">{r.message}</p>{/if}

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

      <div class="outputs" class:pair={r.expected !== null}>
        <div class="out">
          <p class="label">Your output</p>
          <pre>{r.output}</pre>
          {#if r.output_truncated}<p class="hint">Only the first 4 KB is shown.</p>{/if}
        </div>
        {#if r.expected !== null}
          <div class="out">
            <p class="label">Expected</p>
            <pre>{r.expected}</pre>
          </div>
        {/if}
      </div>
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

  .diff {
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
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

  .hint {
    margin-top: var(--space-1);
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

  @container result (min-width: 30rem) {
    .outputs.pair {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
</style>
