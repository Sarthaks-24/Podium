<!--
  DecisionDialog: records an organiser's decision about one student in one
  contest (PUT /api/admin/contests/{id}/integrity/cases/{user}): No action,
  Warning or Exclude from this contest, with a reason of at least 10
  characters. A new decision replaces the last one. A native modal dialog;
  Escape and Cancel call `onclose`, and the browser returns focus to the
  opener. The saved case is handed to `ondecided`. Props: `open`,
  `contestId`, `student` ({ id, name }), `current` (the decision now, if
  any), `onclose`, `ondecided`.
-->
<script lang="ts">
  import Dialog from './Dialog.svelte';
  import { focusFirstInvalid } from '../lib/focus';
  import { errorText, sendJSON } from '../lib/api';
  import { DECISIONS, MIN_REASON, noteCounter, noteLength, type Case, type DecisionKind } from '../lib/review';

  interface Props {
    open: boolean;
    contestId: string | number;
    student: { id: number; name: string } | null;
    current?: Case | null;
    onclose: () => void;
    ondecided: (c: Case) => void;
  }

  let { open, contestId, student, current = null, onclose, ondecided }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let decision = $state<DecisionKind>('warning');
  let reason = $state('');
  let busy = $state(false);
  let error = $state('');
  let reasonError = $state('');

  // Each opening starts from the decision recorded now.
  $effect(() => {
    if (!open) return;
    decision = current?.decision ?? 'warning';
    reason = '';
    error = '';
    reasonError = '';
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!student) return;
    error = '';
    reasonError = noteLength(reason) < MIN_REASON ? 'Write the reason in a sentence or two.' : '';
    if (reasonError) {
      await focusFirstInvalid(dialog);
      return;
    }
    busy = true;
    try {
      const out = await sendJSON<{ case: Case }>(
        'PUT',
        `/api/admin/contests/${encodeURIComponent(String(contestId))}/integrity/cases/${student.id}`,
        { decision, reason: reason.trim() },
      );
      ondecided(out.case);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} labelledby="decision-title" bind:dialog>
  <h2 id="decision-title" tabindex="-1">Record a decision about {student?.name ?? 'this student'}</h2>
  {#if current}
    <p class="muted lead">This replaces the decision recorded now: {DECISIONS.find((d) => d.id === current.decision)?.label}.</p>
  {/if}
  <form class="form" novalidate onsubmit={submit}>
    <fieldset class="field">
      <legend>Decision</legend>
      {#each DECISIONS as d (d.id)}
        <label class="choice">
          <input type="radio" name="decision" value={d.id} bind:group={decision} />
          <span><span class="label">{d.label}</span><span class="muted small help">{d.help}</span></span>
        </label>
      {/each}
    </fieldset>
    <div class="field">
      <label for="decision-reason">Reason</label>
      <textarea
        id="decision-reason"
        aria-required="true"
        class="input"
        rows="4"
        bind:value={reason}
        aria-invalid={reasonError ? 'true' : undefined}
        aria-describedby="decision-reason-count{reasonError ? ' decision-reason-error' : ''}"
      ></textarea>
      <p id="decision-reason-count" class="field-help">{noteCounter(reason)}</p>
      {#if reasonError}<p id="decision-reason-error" class="field-error">{reasonError}</p>{/if}
    </div>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Record the decision</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
</Dialog>

<style>
  .lead {
    margin-top: var(--space-2);
    font-size: var(--fs-0);
  }

  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-4);
  }

  fieldset {
    border: 0;
    padding: 0;
    margin: 0;
    display: grid;
    gap: var(--space-2);
  }

  legend {
    font-weight: 500;
    margin-bottom: var(--space-1);
  }

  .choice {
    display: flex;
    align-items: start;
    gap: var(--space-2);
    min-height: var(--control-h);
  }

  .choice input {
    margin-top: var(--space-1);
  }

  .choice .label {
    display: block;
    font-weight: 500;
  }

  .help {
    display: block;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
