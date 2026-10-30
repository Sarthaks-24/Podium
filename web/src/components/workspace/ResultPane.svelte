<!--
  ResultPane: the result area as three fixed tabs, Run, Submissions and
  Your input (lib/judging.svelte.ts holds which one shows). Run has the
  latest run on the examples; Submissions has the newest verdict, then
  every submission; Your input has its own box and Run. A failure to send
  says so above the tabs with "Submit again" or "Run again". When a tab
  switches by itself under 1024 px, focus moves to its first line, so a
  keyboard or screen reader user lands on the new result.
-->
<script lang="ts">
  import type { Judging } from '../../lib/judging.svelte';
  import { formatWhen, isWaiting, languageLabel, type Sample } from '../../lib/workspace';
  import Icon from '../Icon.svelte';
  import RunResults from '../RunResults.svelte';
  import StreamNotice from '../StreamNotice.svelte';
  import SubmissionList from '../SubmissionList.svelte';
  import Tabs from '../Tabs.svelte';
  import VerdictPanel from '../VerdictPanel.svelte';

  interface Props {
    judging: Judging;
    samples: Sample[];
    language: string;
    problemScore: { score: number | null; max: number } | null;
    locked: string;
    sticky: boolean;
    /** Under 1024 px nothing runs: only Submissions shows, without tabs. */
    narrow?: boolean;
    onjump: (line: number, col: number | null) => void;
    onruninput: (input: string) => void;
  }

  let { judging, samples, language, problemScore, locked, sticky, narrow = false, onjump, onruninput }: Props = $props();

  const run = $derived(judging.state.run);
  const current = $derived(judging.current);
  const runInput = $derived(locked ? undefined : onruninput);
  const tabs = $derived([
    { id: 'run' as const, label: 'Run', busy: run !== null && !judging.customRun && isWaiting(run) },
    { id: 'submissions' as const, label: 'Submissions', count: judging.state.submissions.length, busy: current !== null && isWaiting(current) },
    { id: 'input' as const, label: 'Your input', busy: run !== null && judging.customRun && isWaiting(run) },
  ]);
</script>

{#if judging.notice && judging.notice.kind === 'error'}
  <div class="banner banner-error notice" role="alert" tabindex="-1" data-result-focus>
    <p>{judging.notice.text}</p>
    {#if judging.notice.retry}
      <button type="button" class="btn btn-secondary btn-compact" onclick={() => judging.retry()}>{judging.notice.action === 'submit' ? 'Submit again' : 'Run again'}</button>
    {/if}
  </div>
{/if}
<StreamNotice mode={judging.streamMode} onresume={() => judging.resume()} />

{#if !narrow}
  <Tabs {tabs} selected={judging.tab} onselect={(t) => (judging.tab = t)} label="Result" idPrefix="ws-rtab" controls={(t) => `ws-rpanel-${t}`} />
{/if}

<div class="panel" role="tabpanel" id="ws-rpanel-run" aria-labelledby="ws-rtab-run" hidden={narrow || judging.tab !== 'run'}>
  {#if run && !judging.customRun}
    <h2 class="block-title" tabindex="-1" data-result-focus>Run on the examples</h2>
    <RunResults {run} {language} {onjump} {samples} onruninput={runInput} />
  {:else}
    <p class="muted empty" tabindex="-1" data-result-focus>Run tries your code on the examples and never counts. Its result appears here.</p>
  {/if}
</div>

<div class="panel" role={narrow ? undefined : 'tabpanel'} id="ws-rpanel-submissions" aria-labelledby={narrow ? undefined : 'ws-rtab-submissions'} hidden={!narrow && judging.tab !== 'submissions'}>
  {#if current?.hidden}
    <h2 class="block-title" tabindex="-1" data-result-focus>Latest submission</h2>
    <p class="headline-hidden">Submitted</p>
    <p class="muted small">{formatWhen(current.created_at)}, {languageLabel(current.language)}. Scores appear after the contest.</p>
  {:else if current}
    <h2 class="block-title" tabindex="-1" data-result-focus>Latest submission</h2>
    <VerdictPanel submission={current} {onjump} idPrefix="current" {problemScore} {sticky} onruninput={runInput} />
    {#if judging.exampleFailed}
      <button type="button" class="btn btn-secondary btn-compact" disabled={judging.runBusy || locked !== ''} onclick={() => judging.runExamples()}>
        <Icon name="play" />Run to see your output
      </button>
    {/if}
  {:else}
    <p class="muted empty" tabindex="-1" data-result-focus>Submit grades your code on the hidden tests. Your submissions appear here.</p>
  {/if}
  {#if judging.state.submissions.length > 0}
    <h2 class="block-title">Your submissions</h2>
    <SubmissionList submissions={judging.state.submissions} onjump={null} />
  {/if}
</div>

<div class="panel" role="tabpanel" id="ws-rpanel-input" aria-labelledby="ws-rtab-input" hidden={narrow || judging.tab !== 'input'}>
  <label class="block-title" for="ws-custom" data-result-focus tabindex="-1">Your input</label>
  <textarea id="ws-custom" class="input textarea-code" rows="4" spellcheck="false" bind:value={judging.customInput}></textarea>
  <button type="button" class="btn btn-secondary btn-compact run" disabled={locked !== ''} aria-disabled={judging.runBusy ? 'true' : undefined} onclick={() => judging.runCustom()}>
    <Icon name="play" />Run
  </button>
  {#if run && judging.customRun}
    <h2 class="block-title">Run on your input</h2>
    <RunResults {run} {language} {onjump} {samples} />
  {/if}
</div>

<style>
  .panel {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .panel[hidden] {
    display: none;
  }

  .panel > .btn {
    justify-self: start;
  }

  .block-title {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .block-title:focus {
    outline: none;
  }

  .block-title:focus-visible {
    outline: calc(2 * var(--rule)) solid var(--act);
    outline-offset: var(--space-1);
  }

  .headline-hidden {
    font-size: var(--fs-2);
    font-weight: 600;
  }

  .notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
  }

  .empty {
    font-size: var(--fs-0);
  }

  .input {
    width: 100%;
  }

  /* 16 px on touch screens, so iOS does not zoom into the box. */
  @media (pointer: coarse) {
    .input {
      font-size: var(--fs-1);
    }
  }
</style>
