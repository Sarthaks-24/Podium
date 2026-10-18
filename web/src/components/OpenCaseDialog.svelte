<!--
  OpenCaseDialog: the form that opens an integrity case from chosen signals
  (POST /api/admin/contests/{id}/integrity/cases). A native modal dialog
  with a title (suggested from the signals, at most 120 characters), a
  reason of at least 20 characters with a live counter, and the chosen
  signals listed in words. A refused case shows the server's sentence
  inside the dialog; a created one is handed to `oncreated`. Escape and
  Cancel call `onclose`, and the browser returns focus to the opener.
  Props: `open`, `contestId`, `signals` (the chosen ones), `users` and
  `problems` (the run's maps, for names), `onclose`, `oncreated`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, sendJSON } from '../lib/api';
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
    onclose: () => void;
    oncreated: (c: Case) => void;
  }

  let { open, contestId, signals, users, problems, onclose, oncreated }: Props = $props();

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
      title = defaultTitle(signals, users, problems);
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

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    titleError = title.trim() === '' ? 'Give the case a title.' : [...title.trim()].length > MAX_TITLE ? `Keep the title to ${MAX_TITLE} characters.` : '';
    noteError = noteLength(note) < MIN_NOTE ? `Write a reason of at least ${MIN_NOTE} characters.` : '';
    if (titleError || noteError) return;
    busy = true;
    try {
      const created = await sendJSON<Case>('POST', `/api/admin/contests/${encodeURIComponent(String(contestId))}/integrity/cases`, {
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
      } else {
        error = 'Something went wrong. Try again in a moment.';
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
    <div class="chosen">
      <h3 id="case-signals">{signals.length === 1 ? 'Signal' : `${signals.length} signals`}</h3>
      <ul aria-labelledby="case-signals">
        {#each signals as s (s.id)}
          <li>
            <span class="where">{problemName(problems, s.problem_id)}:</span>
            {kindWord(s.kind)}, {signalStudents(s, users)}, {strengthWord(s.strength).toLowerCase()} <span class="mono">{formatScore(s.score)}</span>
          </li>
        {/each}
      </ul>
    </div>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Open the case</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
</dialog>

<style>
  .dialog {
    width: min(36rem, calc(100vw - 2 * var(--gutter)));
    max-height: calc(100dvh - 2 * var(--space-5));
    margin: auto;
    padding: var(--space-5);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--ink);
    box-shadow: var(--shadow-menu);
  }

  .dialog::backdrop {
    background: color-mix(in srgb, var(--ink) 40%, transparent);
  }

  h2 {
    font-size: var(--fs-3);
  }

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
