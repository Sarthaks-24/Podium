<!--
  DeleteProblemDialog: "Delete" for a problem, for admins and the owner. It
  loads GET .../delete-info and then shows one of three things: the
  server's refusal in its own words (a contest that has not ended holds the
  problem, or an admin asks about a problem that was used), with only Close;
  or what the delete does and the slug to type. For a used problem it also
  offers, without forcing, Download pack and a link to each contest's
  results, and it names the ended contests where practice for the problem
  closes. Delete problem stays off until the slug is typed. POSTs
  .../delete, shows the toast "Deleted <name>" and calls `ondone`.

  Props: `open`, `problemId`, `name`, `slug` (the original slug to type),
  `role` (the signed-in person's role), `onclose` and `ondone`.
-->
<script lang="ts">
  import { tick, untrack } from 'svelte';
  import Dialog from '../Dialog.svelte';
  import { ApiError, errorText, getJSON, isSignedOut, sendJSON } from '../../lib/api';
  import { deleteRefusal, practiceClosing, problemPath, slugConfirmed, type DeleteInfo } from '../../lib/problems';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    open: boolean;
    problemId: number;
    name: string;
    slug: string;
    role: string | undefined;
    onclose: () => void;
    ondone: () => void;
  }

  let { open, problemId, name, slug, role, onclose, ondone }: Props = $props();

  let info = $state<DeleteInfo | null>(null);
  let loadError = $state('');
  let typed = $state('');
  let error = $state('');
  let busy = $state(false);
  let slugInput: HTMLInputElement | undefined = $state();
  let closeButton: HTMLButtonElement | undefined = $state();

  const refusal = $derived(info ? deleteRefusal(info, role) : '');
  const practice = $derived(info ? practiceClosing(info) : '');
  const ended = $derived(info ? info.contests.filter((c) => !info!.unended.some((u) => u.contest_id === c.contest_id)) : []);
  const confirmed = $derived(slugConfirmed(typed, slug));

  // Each opening starts again from the server's answer.
  $effect(() => {
    if (!open) return;
    info = null;
    loadError = '';
    typed = '';
    error = '';
    untrack(() => void load());
  });

  // The slug field (or Close, when the answer is a refusal) exists only after the answer: focus moves there then.
  $effect(() => {
    if (open && info) void tick().then(() => (slugInput ?? closeButton)?.focus());
  });

  async function load() {
    try {
      info = await getJSON<DeleteInfo>(problemPath(problemId, '/delete-info'));
    } catch (e) {
      if (!isSignedOut(e)) loadError = errorText(e);
    }
  }

  async function remove(event: SubmitEvent) {
    event.preventDefault();
    if (!confirmed || busy) return;
    busy = true;
    error = '';
    try {
      await sendJSON<void>('POST', problemPath(problemId, '/delete'));
      showToast(`Deleted ${name}`);
      ondone();
    } catch (e) {
      if (isSignedOut(e)) return;
      // The state moved since the dialog opened: show the new refusal in place.
      if (e instanceof ApiError && (e.status === 403 || e.status === 409)) {
        error = e.message;
        await load();
      } else error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} labelledby="delete-problem-title" describedby="delete-problem-body" initialFocus={() => slugInput ?? closeButton}>
  <h2 id="delete-problem-title">Delete {name}?</h2>
  <div id="delete-problem-body" class="body">
    {#if loadError}
      <p class="field-error" role="alert">{loadError}</p>
    {:else if !info}
      <p class="muted" role="status">Loading</p>
    {:else if refusal}
      <p>{refusal}</p>
      {#if info.unended.length > 0}
        <ul class="list">
          {#each info.unended as c (c.contest_id)}<li>{c.title}</li>{/each}
        </ul>
      {/if}
    {:else}
      <p>
        This removes {name} from the bank for good, with its tests and solutions. It cannot be restored. Ended contests keep their standings and every
        student's submissions, and show it as Deleted problem.
      </p>
      {#if practice}<p>{practice}</p>{/if}
      {#if info.used}
        <p>You can keep a copy first.</p>
        <ul class="list links">
          <li><a class="btn btn-secondary btn-compact" href={problemPath(problemId, '/export')} download>Download pack</a></li>
          {#each ended as c (c.contest_id)}
            <li><a href="/admin/contests/{c.contest_id}/results">Results of {c.title}</a></li>
          {/each}
        </ul>
      {/if}
    {/if}
  </div>
  {#if info && !refusal}
    <form class="form" novalidate onsubmit={remove}>
      <div class="field">
        <label for="delete-slug">Type <code class="slug">{slug}</code> to confirm</label>
        <input
          id="delete-slug"
          bind:this={slugInput}
          class="input mono"
          type="text"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          bind:value={typed}
        />
      </div>
      {#if error}<p class="field-error" role="alert">{error}</p>{/if}
      <div class="actions">
        <button type="submit" class="btn btn-danger" disabled={busy || !confirmed}>Delete problem</button>
        <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
      </div>
    </form>
  {:else}
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="button" class="btn btn-secondary" bind:this={closeButton} onclick={onclose}>Close</button>
    </div>
  {/if}
</Dialog>

<style>
  .body {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .list {
    padding-left: var(--space-5);
  }

  .links {
    display: grid;
    gap: var(--space-2);
    padding-left: 0;
    list-style: none;
  }

  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-4);
  }

  .slug {
    display: inline-block;
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
</style>
