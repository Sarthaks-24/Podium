<!--
  OpenCaseDialog: the form that opens an integrity case from chosen signals
  (POST /api/admin/contests/{id}/integrity/cases). A native modal dialog
  with a title (suggested from the signals, at most 120 characters), a
  reason of at least 20 characters with a live counter, and the chosen
  signals listed in words. A refused case shows the server's sentence
  inside the dialog; a created one is handed to `oncreated`. Escape and
  Cancel call `onclose`, and the browser returns focus to the opener.
  With `submission` set it opens a manual case from that submission
  instead (POST /api/admin/submissions/{id}/case, v2 P4 F9.3): the title
  is suggested from the problem, and the dialog says the submission and
  its private notes are copied into the case. Props: `open`, `contestId`,
  `signals` (the chosen ones), `users` and `problems` (the run's maps, for
  names), `submission` ({ id, name, label }), `onclose`, `oncreated`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, errorText, sendJSON } from '../lib/api';
  import {
    MAX_TITLE,
    MIN_NOTE,
    defaultTitle,
    formatScore,
    kindWord,
    noteCounter,
    noteLength,
    problemName,
    signalStudents,
    strengthWord,
    type Case,
    type ReviewProblem,
    type ReviewUser,
    type Signal,
  } from '../lib/review';

  interface Props {
    open: boolean;
    contestId: string | number;
    signals: Signal[];
    users: Record<string, ReviewUser> | undefined;
    problems: Record<string, ReviewProblem> | undefined;
    submission?: { id: number; name: string; label: string } | null;
    onclose: () => void;
    oncreated: (c: Case) => void;
  }

  let { open, contestId, signals, users, problems, submission = null, onclose, oncreated }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();
  let title = $state('');
  let note = $state('');
  let busy = $state(false);
  let error = $state('');
  let titleError = $state('');
  let noteError = $state('');

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      title = submission ? `Submission to problem ${submission.label}` : defaultTitle(signals, users, problems);
      note = '';
      error = '';
      titleError = '';
      noteError = '';
      dialog.showModal();
      tick().then(() => heading?.focus());
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancelEvent(event: Event) {
    event.preventDefault();
    if (!busy) onclose();
  }

  async function focusInvalid() {
    await tick();
    dialog?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    titleError = title.trim() === '' ? 'Give the case a title.' : [...title.trim()].length > MAX_TITLE ? `Keep the title to ${MAX_TITLE} characters.` : '';
    noteError = noteLength(note) < MIN_NOTE ? `Write a reason of at least ${MIN_NOTE} characters.` : '';
    if (titleError || noteError) {
      await focusInvalid();
      return;
    }
    busy = true;
    try {
      const created = submission
        ? await sendJSON<Case>('POST', `/api/admin/submissions/${submission.id}/case`, { title: title.trim(), note: note.trim() })
        : await sendJSON<Case>('POST', `/api/admin/contests/${encodeURIComponent(String(contestId))}/integrity/cases`, {
            title: title.trim(),
            note: note.trim(),
            signal_ids: signals.map((s) => s.id),
          });
      oncreated(created);
    } catch (e) {
      if (e instanceof ApiError) {
        titleError = e.fields.title ?? '';
        noteError = e.fields.note ?? '';
        if (!titleError && !noteError) error = e.message;
        else void focusInvalid();
      } else {
        error = errorText(e);
      }
    } finally {
      busy = false;
    }
  }
</script>

<dialog bind:this={dialog} class="dialog" aria-labelledby="open-case-title" oncancel={onCancelEvent}>
  <h2 id="open-case-title" tabindex="-1" bind:this={heading}>Open a case</h2>
  <p class="muted lead">The students do not see the case until you share it with them.</p>
  <form class="form" novalidate onsubmit={submit}>
    <div class="field">
      <label for="case-title">Title</label>
      <input
        id="case-title"
        class="input"
        maxlength={MAX_TITLE}
        bind:value={title}
        aria-invalid={titleError ? 'true' : undefined}
        aria-describedby={titleError ? 'case-title-error' : undefined}
      />
      {#if titleError}<p id="case-title-error" class="field-error">{titleError}</p>{/if}
    </div>
    <div class="field">
      <label for="case-note">Reason</label>
      <textarea
        id="case-note"
        class="input"
        rows="4"
        bind:value={note}
        aria-invalid={noteError ? 'true' : undefined}
        aria-describedby="case-note-count{noteError ? ' case-note-error' : ''}"
      ></textarea>
      <p id="case-note-count" class="field-help">{noteCounter(note)}</p>
      {#if noteError}<p id="case-note-error" class="field-error">{noteError}</p>{/if}
    </div>
    {#if submission}
      <p class="chosen small">
        From {submission.name}'s submission to problem {submission.label}. The submission and its private notes are copied into the case, so the notes are kept
        with it.
      </p>
    {:else}
    <div class="chosen">
      <h3 id="case-signals">{signals.length === 1 ? 'Signal' : `${signals.length} signals`}</h3>
      <ul aria-labelledby="case-signals">
        {#each signals as s (s.id)}
          <li>
            <span class="where">{problemName(problems, s.problem_id)}:</span>
            {kindWord(s.kind)}, {signalStudents(s, users)}, {strengthWord(s.strength).toLowerCase()} <span class="mono">{formatScore(s.score, s.kind)}</span>
          </li>
        {/each}
      </ul>
    </div>
    {/if}
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Open the case</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
</dialog>

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

  .chosen h3 {
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .chosen ul {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-1);
    padding-left: var(--space-4);
    font-size: var(--fs-0);
  }

  .where {
    font-weight: 500;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
