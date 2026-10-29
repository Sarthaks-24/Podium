<!--
  VerdictPanel: everything about one submission (design.md 5.3 and 8). The
  headline names the outcome in sentence case ("Wrong answer on test 5",
  "Partial, 40 of 100", "3rd in queue"), a line of guidance follows for a
  failing verdict, then the subtask strip filling as results arrive, the
  compiler output verbatim with clickable line jumps, and the tests: the
  samples and each subtask in its own disclosure, a failing one open, each
  row giving the verdict in words, the time in mono and the memory. A
  waiting submission reads "Submitted, 3rd in queue"; one that scored less
  than the problem already has adds "Your score on this problem stays 60
  of 100" (S4). A practice submission may carry its first failing hidden
  test (`revealed`, D115): it is shown in full under the strip through
  TestDetail, with "This test is shown because this is practice." and
  "Run on this input"; a submission that counts never carries one, so it
  never shows. With `sticky` the headline, its detail line and the strip
  stay at the top of a scrolling result pane while the rest scrolls under
  them (audit 2026-10-20 S5). Props: `submission`, `onjump` (moves the
  editor to a compile error's line), `idPrefix` (keeps ids unique when
  several panels are on the page), `problemScore` (optional, { score, max }
  so far), `sticky` (optional) and `onruninput` (optional; puts an input
  in the custom input box and runs it).
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
    problemScoreLine,
    isWaiting,
    revealedReason,
    revealedTitle,
    type Submission,
    type TestResult,
  } from '../lib/workspace';
  import CompileOutput from './CompileOutput.svelte';
  import SubtaskStrip from './SubtaskStrip.svelte';
  import TestDetail from './TestDetail.svelte';

  interface Props {
    submission: Submission;
    onjump: ((line: number, col: number | null) => void) | null;
    idPrefix: string;
    problemScore?: { score: number | null; max: number } | null;
    sticky?: boolean;
    onruninput?: (input: string) => void;
  }

  let { submission, onjump, idPrefix, problemScore = null, sticky = false, onruninput }: Props = $props();
  // D115: only a finished submission that never counts carries it; the server decides.
  const revealed = $derived(submission.state === 'done' && submission.revealed ? submission.revealed : null);
  const revealedVerdict = $derived(
    revealed ? (submission.subtasks.flatMap((st) => st.tests).find((t) => t.idx === revealed.idx)?.verdict ?? '') : '',
  );
  const stays = $derived(problemScoreLine(submission, problemScore));

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

<div class="verdict">
  <div class="head" class:sticky>
    <p class="headline tone-{head.tone}">{head.text}</p>
    {#if head.detail}<p class="detail">{head.detail}</p>{/if}
    {#if submission.subtasks.length > 0 && submission.verdict !== 'CE'}
      <SubtaskStrip {submission} />
      <!-- V3-27: the word's meaning where a student first meets it. -->
      <p class="detail">A subtask's points count when every hidden test in it passes.</p>
    {/if}
  </div>
  {#if stays}<p class="stays">{stays}</p>{/if}
  {#each advice as line, i (i)}<p class="advice">{line}</p>{/each}

  {#if revealed}
    <TestDetail
      heading={revealedTitle(revealed)}
      reason={revealedReason('submission')}
      input={revealed.input}
      expected={revealed.expected}
      output={revealed.output}
      cut={revealed.truncated}
      verdict={revealedVerdict}
      runInput={revealed.input}
      {onruninput}
    />
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
            <span>Examples</span>
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
          <td class="mono num">{t.verdict === '' || t.verdict === 'SK' ? '' : formatMs(t.time_ms, submission.time_step_ms)}</td>
          <td class="mono num">{t.verdict === '' || t.verdict === 'SK' ? '' : formatKb(t.memory_kb)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/snippet}

<style>
  /* Not .panel: the result pane is the task area; the verdict needs no second box inside it. */
  .verdict {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .head {
    display: grid;
    gap: var(--space-2);
  }

  /* S5: the verdict, its detail and the strip stay in view while the tests scroll. The
     pane says how much padding it has (--sticky-top, --sticky-bleed), so the
     head sticks to the pane's very top and its background covers the sides. */
  .head.sticky {
    position: sticky;
    top: calc(-1 * var(--sticky-top, 0px));
    z-index: var(--z-sticky);
    margin-inline: calc(-1 * var(--sticky-bleed, 0px));
    padding: var(--space-2) var(--sticky-bleed, 0px);
    border-bottom: var(--rule) solid var(--line);
    background: var(--surface);
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

  .stays {
    color: var(--ink-muted);
  }

  .advice {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    max-width: var(--measure-prose);
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
