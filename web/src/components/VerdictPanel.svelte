<!--
  VerdictPanel: everything about one submission (design.md 5.3 and 8). The
  headline names the outcome in sentence case ("Wrong answer on test 5",
  "Partial, 40 of 100", "3rd in queue"), a line of guidance follows for a
  failing verdict, then the subtask strip filling as results arrive, the
  compiler output verbatim with clickable line jumps, and the tests: the
  samples and each subtask in its own disclosure, a failing one open, each
  row giving the verdict in words, the time in mono and the memory. Props:
  `submission`, `onjump` (moves the editor to a compile error's line), and
  `idPrefix` (keeps ids unique when several panels are on the page).
-->
<script lang="ts">
  import { verdictLabel } from '../lib/problems';
  import {
    firstFailure,
    formatKb,
    formatMs,
    guidance,
    headline,
    isBad,
    isWaiting,
    type Submission,
    type TestResult,
  } from '../lib/workspace';
  import CompileOutput from './CompileOutput.svelte';
  import SubtaskStrip from './SubtaskStrip.svelte';

  interface Props {
    submission: Submission;
    onjump: ((line: number, col: number | null) => void) | null;
    idPrefix: string;
  }

  let { submission, onjump, idPrefix }: Props = $props();

  const head = $derived(headline(submission));
  const fail = $derived(firstFailure(submission));
  // The subtask holding the first failure opens by itself.
  const failSubtask = $derived(
    fail && !fail.sample ? (submission.subtasks.find((st) => st.tests.some((t) => t.idx === fail.idx))?.idx ?? null) : null,
  );
  const advice = $derived(
    submission.state !== 'done'
      ? []
      : submission.verdict === 'CE'
        ? guidance('CE', submission.language)
        : fail
          ? guidance(fail.verdict, submission.language, { sample: fail.sample })
          : [],
  );

  function word(t: TestResult): string {
    return t.verdict === '' ? 'Waiting' : verdictLabel(t.verdict);
  }

  function groupWord(tests: TestResult[], verdict: string): string {
    if (verdict !== '') return verdictLabel(verdict);
    if (tests.some((t) => isBad(t.verdict))) return 'Failed';
    if (tests.length > 0 && tests.every((t) => t.verdict !== '')) return 'Passed';
    return isWaiting(submission) ? 'Judging' : 'Not judged';
  }
</script>

<div class="panel">
  <p class="headline tone-{head.tone}">{head.text}</p>
  {#if head.detail}<p class="detail">{head.detail}</p>{/if}
  {#each advice as line, i (i)}<p class="advice">{line}</p>{/each}

  {#if submission.subtasks.length > 0 && submission.verdict !== 'CE'}
    <SubtaskStrip {submission} />
  {/if}

  {#if submission.compile_output}
    <div class="compile">
      <p class="label" id="{idPrefix}-compile">Compiler output</p>
      <CompileOutput output={submission.compile_output} language={submission.language} {onjump} />
    </div>
  {/if}

  {#if submission.verdict !== 'CE' && (submission.samples.length > 0 || submission.subtasks.some((s) => s.tests.length > 0))}
    <div class="groups">
      {#if submission.samples.length > 0}
        {@const bad = submission.samples.some((t) => isBad(t.verdict))}
        <details class="group" open={bad}>
          <summary>
            <span>Samples</span>
            <span class="group-word" class:bad>{groupWord(submission.samples, '')}</span>
          </summary>
          {@render rows(submission.samples)}
        </details>
      {/if}
      {#each submission.subtasks as st (st.idx)}
        {#if st.tests.length > 0}
          {@const bad = isBad(st.verdict) || st.tests.some((t) => isBad(t.verdict))}
          <details class="group" open={bad && st.idx === failSubtask}>
            <summary>
              <span>Subtask {st.idx}</span>
              <span class="mono points">{st.earned} of {st.points}</span>
              <span class="group-word" class:bad class:ok={st.verdict === 'AC'}>{groupWord(st.tests, st.verdict)}</span>
            </summary>
            {@render rows(st.tests)}
          </details>
        {/if}
      {/each}
    </div>
  {/if}
</div>

{#snippet rows(tests: TestResult[])}
  <table class="tests">
    <thead class="visually-hidden">
      <tr><th scope="col">Test</th><th scope="col">Verdict</th><th scope="col">Time</th><th scope="col">Memory</th></tr>
    </thead>
    <tbody>
      {#each tests as t (t.idx)}
        <tr>
          <th scope="row" class="mono idx">{t.idx}</th>
          <td class:bad={isBad(t.verdict)} class:muted={t.verdict === '' || t.verdict === 'SK'}>{word(t)}</td>
          <td class="mono num">{t.verdict === '' || t.verdict === 'SK' ? '' : formatMs(t.time_ms)}</td>
          <td class="mono num">{t.verdict === '' || t.verdict === 'SK' ? '' : formatKb(t.memory_kb)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/snippet}

<style>
  .panel {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .headline {
    font-size: var(--fs-2);
    font-weight: 600;
    line-height: var(--lh-tight);
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-part {
    color: var(--part);
  }

  .tone-bad {
    color: var(--bad);
  }

  .tone-wait {
    color: var(--ink-muted);
  }

  .detail {
    font-weight: 500;
  }

  .advice {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    max-width: 60ch;
  }

  .compile {
    display: grid;
    gap: var(--space-1);
  }

  .label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .groups {
    display: grid;
    border-top: var(--rule) solid var(--line);
  }

  .group {
    border-bottom: var(--rule) solid var(--line);
  }

  .group summary {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-h);
    border-radius: var(--radius);
    font-weight: 500;
    cursor: pointer;
  }

  .points {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .group-word {
    margin-left: auto;
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .group-word.ok {
    color: var(--ok);
  }

  .bad,
  .group-word.bad {
    color: var(--bad);
  }

  .tests {
    width: 100%;
    margin-bottom: var(--space-2);
    border-collapse: collapse;
    font-size: var(--fs-0);
  }

  .tests td,
  .tests th {
    padding: var(--space-1) var(--space-2) var(--space-1) 0;
  }

  .idx {
    width: 3ch;
    color: var(--ink-muted);
    font-weight: 400;
    text-align: left;
  }

  .num {
    text-align: right;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
</style>
