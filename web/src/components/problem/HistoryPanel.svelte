<!--
  HistoryPanel: "Version history" on the problem editor. It lists the
  problem's versions, newest first, with the day, the reason and a line that
  marks the current one. A version whose files were all kept has "Bring back
  this version"; any other says why it cannot be brought back. The button
  asks first (ConfirmDialog, not danger, focus on Cancel): it writes that
  version back as a new version, and the confirm says what stays as it is.
  The server's refusal (a running contest, students who have code, an
  archived problem) is shown inside the dialog in its own words.

  Props: `problemId`, `dirty` (the editor has unsaved changes: the server
  works on the saved problem, so the buttons wait), `onbrought` (called after
  a bring back, so the editor reloads the problem) and `onclose` (the Close
  button; the editor hides the panel and returns focus to its menu).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { errorText, getJSON, isSignedOut, sendJSON } from '../../lib/api';
  import { showToast } from '../../lib/toast.svelte';
  import { bringBackPath, broughtBackToast, confirmText, historyPath, NOT_KEPT, versionTime, type BroughtBack, type History, type Version } from '../../lib/versions';

  interface Props {
    problemId: number;
    dirty: boolean;
    onbrought: () => void | Promise<void>;
    onclose: () => void;
  }

  let { problemId, dirty, onbrought, onclose }: Props = $props();

  let history = $state<History | null>(null);
  let loadError = $state('');
  let asking = $state<Version | null>(null);
  let busy = $state(false);
  let refusal = $state('');

  const words = $derived(confirmText(asking?.created_at ?? 0));

  async function load() {
    loadError = '';
    try {
      history = await getJSON<History>(historyPath(problemId));
    } catch (e) {
      if (!isSignedOut(e)) loadError = errorText(e);
    }
  }

  onMount(() => void load());

  function ask(version: Version) {
    refusal = '';
    asking = version;
  }

  async function bringBack() {
    if (!asking) return;
    const version = asking;
    busy = true;
    refusal = '';
    try {
      const out = await sendJSON<BroughtBack>('POST', bringBackPath(problemId, version.id));
      asking = null;
      showToast(broughtBackToast(version.created_at, out));
      await onbrought();
    } catch (e) {
      if (!isSignedOut(e)) refusal = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<section class="panel history" aria-labelledby="history-heading">
  <div class="head">
    <h2 id="history-heading" tabindex="-1">Version history</h2>
    <button type="button" class="btn btn-quiet" onclick={onclose}>Close</button>
  </div>
  {#if dirty}<p class="field-help">Save your changes first: a version is brought back onto the saved problem.</p>{/if}
  {#if loadError !== ''}
    <div class="banner banner-error" role="alert">
      <p>{loadError}</p>
      <button type="button" class="btn btn-secondary btn-compact" onclick={load}>Try again</button>
    </div>
  {:else if history === null}
    <p class="muted" role="status">Loading versions</p>
  {:else}
    <ul class="versions">
      {#each history.revisions as v (v.id)}
        <li>
          <div class="what">
            <span class="when">{versionTime(v.created_at)}</span>
            {#if v.current}<span class="muted">Current version</span>{/if}
            {#if v.reason !== ''}<span class="muted">{v.reason}</span>{/if}
          </div>
          {#if !v.current}
            {#if v.can_bring_back}
              <button type="button" class="btn btn-secondary btn-compact" disabled={dirty} onclick={() => ask(v)}>Bring back this version</button>
            {:else}
              <p class="muted">{NOT_KEPT}</p>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
    {#if history.total > history.revisions.length}
      <p class="muted">Showing the newest {history.revisions.length} of {history.total} versions.</p>
    {/if}
  {/if}
</section>

<ConfirmDialog
  open={asking !== null}
  title={words.title}
  body={words.body}
  confirmLabel={words.confirm}
  {busy}
  error={refusal}
  onconfirm={bringBack}
  oncancel={() => (asking = null)}
/>

<style>
  .history {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    justify-items: start;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    width: 100%;
  }

  .versions {
    width: 100%;
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
  }

  .versions li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    border-top: var(--rule) solid var(--line);
  }

  .what {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    min-width: 0;
  }

  .when {
    font-variant-numeric: tabular-nums;
  }
</style>
