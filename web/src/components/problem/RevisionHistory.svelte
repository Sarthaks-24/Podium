<!--
  RevisionHistory: the problem editor's "History" section (v2 F10.1,
  contract C4; a small section until P3 splits the editor). It lists the
  problem's revisions from the problem response, newest first: the number,
  when and by whom, what changed in words, the setter's reason, whether it
  is validated and which contests pin it now. "Compare with revision 2"
  loads GET /api/admin/problems/{id}/revisions/diff (lib/history.ts) and
  shows what differs and the statement's line diff (DiffLines) below the
  row; it never shows test data. Props: `problemId` and `history` (the
  revision fields of the problem).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, getJSON } from '../../lib/api';
  import { formatDateTime } from '../../lib/format';
  import { diffSummary, revisionChangeText, type ProblemHistory, type RevisionDiff } from '../../lib/history';
  import DiffLines from '../DiffLines.svelte';

  interface Props {
    problemId: number;
    history: ProblemHistory;
  }

  let { problemId, history }: Props = $props();

  /** The words for the fields a diff names. */
  const CHANGED: Record<string, string> = {
    title: 'title',
    statement: 'statement',
    limits: 'limits',
    checker: 'checker',
    solutions: 'solutions',
    samples: 'samples',
    open: 'open tests',
    subtasks: 'subtasks',
    tests: 'tests',
  };

  let open = $state<number | null>(null);
  let diff = $state.raw<RevisionDiff | null>(null);
  let diffState = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
  let diffError = $state('');

  const revisions = $derived(history.revisions ?? []);
  const total = $derived(history.revisions_total ?? revisions.length);

  async function compare(rev: number) {
    if (open === rev) {
      open = null;
      return;
    }
    open = rev;
    diffState = 'loading';
    try {
      diff = await getJSON<RevisionDiff>(`/api/admin/problems/${problemId}/revisions/diff?from=${rev - 1}&to=${rev}`);
      diffState = 'ready';
    } catch (e) {
      diffError = e instanceof ApiError ? e.message : 'The comparison could not be loaded. Try again in a moment.';
      diffState = 'error';
    }
  }

  function changedText(d: RevisionDiff): string {
    if (d.changed.length === 0) return 'Nothing differs.';
    const words = d.changed.map((c) => CHANGED[c] ?? c);
    const list = words.length === 1 ? words[0] : `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
    return `Changed: ${list}.${d.statement_only ? ' A contest can move to it without judging again.' : ''}`;
  }
</script>

{#if revisions.length === 0}
  <p class="muted">No revisions are recorded yet. Each save that changes the problem records one.</p>
{:else}
  <p class="muted intro">
    Each save that changes the problem records a revision. Contests keep the revision they pinned until an organiser updates them on
    the console.{#if total > revisions.length}&nbsp;Showing the newest {revisions.length} of {total}.{/if}
  </p>
  <ul class="revisions">
    {#each revisions as r (r.id)}
      <li>
        <div class="head">
          <span class="rev">Revision <span class="mono">{r.rev}</span></span>
          {#if history.revision?.id === r.id}<span class="state-word" data-state="live">Current</span>{/if}
          <span class="muted when"><MonoDigits text={formatDateTime(r.created_at)} />{r.created_by ? `, ${r.created_by.email}` : ', made by the server'}</span>
        </div>
        <p>{revisionChangeText(r)}. {r.validated ? 'Validated.' : 'Not validated.'}</p>
        {#if r.reason}<p class="reason">Reason: {r.reason}</p>{/if}
        {#if r.pinned_in.length > 0}
          <p class="muted">Pinned in {r.pinned_in.map((c) => `${c.slug} (problem ${c.label})`).join(', ')}</p>
        {/if}
        {#if r.rev > 1 && !r.reconstructed}
          <button type="button" class="btn btn-quiet btn-compact compare" aria-expanded={open === r.rev} onclick={() => compare(r.rev)}>
            Compare with revision {r.rev - 1}
          </button>
        {/if}
        {#if open === r.rev}
          <div class="diff" role="region" aria-label="Revision {r.rev} against revision {r.rev - 1}">
            {#if diffState === 'loading'}
              <p class="muted" role="status">Loading the comparison</p>
            {:else if diffState === 'error'}
              <p class="field-error" role="alert">{diffError}</p>
            {:else if diff}
              <p>{changedText(diff)} {diffSummary(diff.lines)}</p>
              {#if diff.title_from !== diff.title_to}<p>The title changed from "{diff.title_from}" to "{diff.title_to}".</p>{/if}
              {#if diff.lines.length > 0}<DiffLines lines={diff.lines} />{/if}
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .intro {
    max-width: var(--measure-prose);
  }

  .revisions {
    display: grid;
    margin-top: var(--space-3);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .revisions li {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-3);
  }

  .rev {
    font-weight: 600;
  }

  .when {
    font-size: var(--fs-0);
  }

  .reason {
    max-width: var(--measure-prose);
  }

  .compare {
    justify-self: start;
    margin-left: calc(-1 * var(--space-3));
    color: var(--act);
  }

  .diff {
    display: grid;
    gap: var(--space-1);
  }
</style>
