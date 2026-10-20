<!--
  StatementDiff: "What changed in problem A", a native dialog with the line
  diff of a contest problem's statement between two pinned revisions (v2
  F10.1, contract C3; GET /api/contests/{slug}/problems/{label}/revisions
  through lib/history.ts), drawn by DiffLines. It loads when opened. A title change is said in words above the lines. When
  more than two revisions were pinned, a select picks the older side. The
  statement is shown as plain text, never rendered, and the route carries
  no test data (D47). Escape and the close button shut it; focus starts on
  the heading. Props: `open`, `slug`, `label` and `onclose`.
-->
<script lang="ts">
  import { ApiError, getJSON } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { diffSummary, statementHistoryPath, type StatementHistory } from '../lib/history';
  import DiffLines from './DiffLines.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    slug: string;
    label: string;
    onclose: () => void;
  }

  let { open, slug, label, onclose }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();
  let loadState = $state<'loading' | 'ready' | 'error'>('loading');
  let error = $state('');
  let history = $state.raw<StatementHistory | null>(null);
  let from = $state<number | null>(null);

  const diff = $derived(history?.diff ?? null);
  const older = $derived((history?.revisions ?? []).filter((r) => r.revision_id !== history?.current_revision_id));

  async function load(fromId: number | null) {
    loadState = 'loading';
    try {
      const base = statementHistoryPath(slug, label);
      const current = history?.current_revision_id;
      const q = fromId !== null && current !== undefined ? `?from=${fromId}&to=${current}` : '';
      history = await getJSON<StatementHistory>(base + q);
      from = history.diff?.from_revision_id ?? null;
      loadState = 'ready';
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'The changes could not be loaded. Close this and try again.';
      loadState = 'error';
    }
  }

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      heading?.focus();
      history = null;
      void load(null);
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancel(event: Event) {
    event.preventDefault();
    onclose();
  }

  function pick(event: Event) {
    const id = Number((event.currentTarget as HTMLSelectElement).value);
    if (Number.isFinite(id)) void load(id);
  }
</script>

<dialog bind:this={dialog} class="dialog statement-diff" aria-labelledby="diff-title" oncancel={onCancel}>
  <div class="dialog-head">
    <h2 id="diff-title" tabindex="-1" bind:this={heading}>What changed in problem {label}</h2>
    <button type="button" class="btn btn-quiet btn-compact" aria-label="Close what changed" title="Close" onclick={onclose}>
      <Icon name="x" />
    </button>
  </div>

  {#if loadState === 'loading'}
    <p class="muted" role="status">Loading the changes</p>
  {:else if loadState === 'error'}
    <p class="banner banner-error" role="alert">{error}</p>
  {:else if !diff}
    <p class="muted">The statement has not changed since the contest started.</p>
  {:else}
    <p class="lead">
      Revision <span class="mono">{diff.to_rev}</span> against revision <span class="mono">{diff.from_rev}</span>.
      {diffSummary(diff.lines)}
    </p>
    {#if older.length > 1}
      <div class="field compare">
        <label for="diff-from">Compare with</label>
        <select id="diff-from" class="select" value={from ?? undefined} onchange={pick}>
          {#each older as r (r.revision_id)}
            <option value={r.revision_id}>Revision {r.rev}, pinned {formatDateTime(r.pinned_at)}</option>
          {/each}
        </select>
      </div>
    {/if}
    {#if diff.title_from !== diff.title_to}
      <p class="title-change">The title changed from "{diff.title_from}" to "{diff.title_to}".</p>
    {/if}
    {#if diff.coarse}
      <p class="muted small">The statement changed too much to compare line by line, so the old text and then the new text are shown.</p>
    {/if}
    <DiffLines lines={diff.lines} />
  {/if}
</dialog>

<style>
  .statement-diff {
    width: min(var(--measure-wide), calc(100vw - 2 * var(--gutter)));
  }

  .lead,
  .title-change {
    margin-top: var(--space-2);
  }

  .compare {
    margin-top: var(--space-3);
    max-width: 24rem;
  }

  .small {
    font-size: var(--fs-0);
  }
</style>
