<!--
  AddToContestDialog: "Add to contest" from the problem editor, once the
  problem has passed validation. Each opening lists the contests that can
  still take a problem, drafts and upcoming ones (GET
  /api/admin/contests), leaving out any that already have this problem.
  Adding reads the contest's problems and sends them back with this one
  under the next free letter, through the same PUT .../problems the
  contest's setup uses, so the server makes the same checks. A refusal is
  shown inside the dialog. Props: `open`, `problemId`, `title` (the
  problem's), `onclose` and `ondone(contest title, letter)`.
-->
<script lang="ts">
  import Dialog from '../Dialog.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../../lib/api';
  import { labelFor, type AdminContest, type AdminContestSummary } from '../../lib/contests';
  import { formatDayTime } from '../../lib/format';

  interface Props {
    open: boolean;
    problemId: number;
    title: string;
    onclose: () => void;
    ondone: (contest: string, label: string) => void;
  }

  let { open, problemId, title, onclose, ondone }: Props = $props();

  let contests = $state<AdminContestSummary[] | null>(null);
  let chosen = $state('');
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  // Each opening asks again, so a contest made in another tab shows up.
  $effect(() => {
    if (!open) return;
    contests = null;
    chosen = '';
    error = '';
    loadError = '';
    void load();
  });

  async function load() {
    try {
      const data = await getJSON<{ contests: AdminContestSummary[] }>('/api/admin/contests');
      const takers = (data.contests ?? []).filter(
        (c) => c.state === 'upcoming' && !c.archived && !(c.problems ?? []).some((p) => p.problem_id === problemId),
      );
      takers.sort((a, b) => Number(a.published) - Number(b.published) || a.starts_at - b.starts_at);
      contests = takers;
      if (takers.length > 0) chosen = String(takers[0].id);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      loadError = errorText(e);
      contests = [];
    }
  }

  function describe(c: AdminContestSummary): string {
    return c.published ? `${c.title}, starts ${formatDayTime(c.starts_at)}` : `${c.title}, draft`;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    const target = contests?.find((c) => String(c.id) === chosen);
    if (!target) return;
    error = '';
    busy = true;
    try {
      const base = `/api/admin/contests/${target.id}`;
      const contest = await getJSON<AdminContest>(base);
      const used = new Set(contest.problems.map((p) => p.label));
      let i = 0;
      while (used.has(labelFor(i))) i++;
      const label = labelFor(i);
      await sendJSON<AdminContest>('PUT', `${base}/problems`, [
        ...contest.problems.map((p) => ({ problem_id: p.problem_id, label: p.label })),
        { problem_id: problemId, label },
      ]);
      ondone(target.title, label);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} kind="small" title="Add {title} to a contest" closeLabel="Close">
  {#if contests === null}
    <p class="body muted" role="status">Loading contests</p>
  {:else if loadError}
    <p class="field-error body" role="alert">{loadError}</p>
  {:else if contests.length === 0}
    <p class="body">No draft or upcoming contest can take it. Make a contest first.</p>
    <div class="actions">
      <a class="btn btn-secondary" href="/admin/contests">Go to Contests</a>
    </div>
  {:else}
    <form class="form" novalidate onsubmit={submit}>
      <div class="field">
        <label for="add-contest">Contest</label>
        <select id="add-contest" class="select" bind:value={chosen}>
          {#each contests as c (c.id)}<option value={String(c.id)}>{describe(c)}</option>{/each}
        </select>
        <p class="field-help">It goes in after the contest's other problems, under the next letter.</p>
      </div>
      {#if error}<p class="field-error" role="alert">{error}</p>{/if}
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={busy}>Add to contest</button>
        <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
      </div>
    </form>
  {/if}
</Dialog>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-3);
  }

  .body {
    margin-top: var(--space-3);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
</style>
