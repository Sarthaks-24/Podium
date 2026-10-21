<!--
  SubmissionNotes: private notes on one contest submission, for reviewers
  (v2 P4 F9.3; GET and POST /api/admin/submissions/{id}/notes, docs/API.md
  "C9: private notes and manual cases"). A native modal dialog on the
  shared shell, opened from the console feed.

  It lists the notes already written (who, when, the lines they point at,
  and when each is deleted: 30 days after it was written unless a case is
  opened from the submission), then a form for a new note: the text (1 to
  2,000 characters, plain text) and, optionally, the lines in question
  ("3-5, 12"). The student is never told about a note, and the dialog
  says so. Below the form, "Open a case" hands over to OpenCaseDialog
  (through `onopencase`), or, when a case already exists, a link to it.
  Opening the dialog is logged for the owner; a reviewer's own submission
  answers 404 and the dialog says why. The program is shown read only in
  CodeView (as text, never HTML), and the lines typed in "Lines in
  question" are highlighted in it as they are typed, so the reviewer picks
  them against the code. Props: `open`,
  `submission` ({ id, name, label }), `onclose`, `onopencase`, and
  `onchange` (gets the new note count and case id, for the feed row).
-->
<script lang="ts">
  import { tick } from 'svelte';
  import CodeView from './CodeView.svelte';
  import MonoDigits from './MonoDigits.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { lineSpans } from '../lib/standing';
  import { linesText, parseLines, type Note, type Notes } from '../lib/students';
  import { showToast } from '../lib/toast.svelte';

  interface Props {
    open: boolean;
    submission: { id: number; name: string; label: string } | null;
    onclose: () => void;
    onopencase: () => void;
    onchange?: (noteCount: number, caseId: number | null) => void;
  }

  let { open, submission, onclose, onopencase, onchange }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();
  let view = $state<'loading' | 'ready' | 'refused' | 'error'>('loading');
  let refusal = $state('');
  let notes = $state<Note[]>([]);
  let caseId = $state<number | null>(null);
  let source = $state('');
  let text = $state('');
  let lines = $state('');
  let textError = $state('');
  let linesError = $state('');
  let sendError = $state('');
  let busy = $state(false);

  const MAX_NOTE = 2000;

  // The typed lines, highlighted in the program while they parse.
  const picked = $derived.by(() => {
    const parsed = parseLines(lines);
    return 'lines' in parsed ? lineSpans(source, parsed.lines) : [];
  });

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      text = '';
      lines = '';
      textError = '';
      linesError = '';
      sendError = '';
      dialog.showModal();
      tick().then(() => heading?.focus());
      void load();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  async function load() {
    if (!submission) return;
    view = 'loading';
    try {
      const d = await getJSON<Notes>(`/api/admin/submissions/${submission.id}/notes`);
      notes = d.notes ?? [];
      caseId = d.case_id ?? null;
      source = d.source ?? '';
      view = 'ready';
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        refusal = 'This is your own submission, or it is not a contest submission. Another organiser reviews it.';
        view = 'refused';
      } else if (e instanceof ApiError && e.status === 403) {
        refusal = 'Only reviewers of this contest can read or write notes.';
        view = 'refused';
      } else {
        refusal = errorText(e);
        view = 'error';
      }
    }
  }

  function onCancelEvent(event: Event) {
    event.preventDefault();
    if (!busy) onclose();
  }

  async function add(event: SubmitEvent) {
    event.preventDefault();
    if (!submission) return;
    sendError = '';
    const n = [...text.trim()].length;
    textError = n === 0 ? 'Write the note first.' : n > MAX_NOTE ? `Keep the note to ${MAX_NOTE} characters.` : '';
    const parsed = parseLines(lines);
    linesError = 'error' in parsed ? parsed.error : '';
    if (textError || linesError) {
      await tick();
      dialog?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
      return;
    }
    busy = true;
    try {
      const note = await sendJSON<Note>('POST', `/api/admin/submissions/${submission.id}/notes`, {
        text: text.trim(),
        lines: 'lines' in parsed ? parsed.lines : [],
      });
      notes = [...notes, note];
      text = '';
      lines = '';
      showToast('Saved the note');
      onchange?.(notes.length, caseId);
    } catch (e) {
      if (e instanceof ApiError && (e.fields.text || e.fields.lines)) {
        textError = e.fields.text ?? '';
        linesError = e.fields.lines ?? '';
        await tick();
        dialog?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
      } else {
        sendError = errorText(e);
      }
    } finally {
      busy = false;
    }
  }
</script>

<dialog bind:this={dialog} class="dialog" aria-labelledby="notes-title" oncancel={onCancelEvent}>
  <div class="dialog-head">
    <h2 id="notes-title" tabindex="-1" bind:this={heading}>Notes on {submission?.name ?? 'a student'}'s submission to problem {submission?.label ?? ''}</h2>
    <button type="button" class="btn btn-quiet btn-compact" onclick={onclose}>Close</button>
  </div>
  <p class="muted small lead">Private to organisers. The student is never told about a note; they see a question only if you open a case.</p>

  {#if view === 'loading'}
    <p class="muted small" role="status">Loading the notes</p>
  {:else if view === 'refused' || view === 'error'}
    <p class="field-error" role="alert">{refusal}</p>
  {:else}
    {#if notes.length === 0}
      <p class="muted small">No notes yet.</p>
    {:else}
      <ul class="notes" aria-label="Notes, oldest first">
        {#each notes as n (n.id)}
          <li>
            <p class="muted small">
              <MonoDigits text={formatDateTime(n.created_at)} />, {n.by_name || 'an organiser'}{n.lines.length > 0 ? `, ${linesText(n.lines)}` : ''}
            </p>
            <p class="text">{n.text}</p>
            <p class="muted small">{#if n.expires_at === null}Kept with the case.{:else}<MonoDigits text={`Deleted on ${formatDateTime(n.expires_at)} unless a case is opened.`} />{/if}</p>
          </li>
        {/each}
      </ul>
    {/if}

    {#if source !== ''}
      <div class="program">
        <CodeView {source} spans={picked} label="{submission?.name ?? 'The student'}'s program for problem {submission?.label ?? ''}" />
        <p class="muted small">The lines you type under "Lines in question" are highlighted here.</p>
      </div>
    {/if}

    <form class="form" novalidate onsubmit={add}>
      <div class="field">
        <label for="note-text">New note</label>
        <textarea
          id="note-text"
          aria-required="true"
          class="input"
          rows="3"
          maxlength={MAX_NOTE}
          bind:value={text}
          aria-invalid={textError ? 'true' : undefined}
          aria-describedby={textError ? 'note-text-error' : undefined}
        ></textarea>
        {#if textError}<p id="note-text-error" class="field-error">{textError}</p>{/if}
      </div>
      <div class="field">
        <label for="note-lines">Lines in question (optional)</label>
        <input
          id="note-lines"
          class="input lines-input"
          autocomplete="off"
          spellcheck="false"
          bind:value={lines}
          aria-invalid={linesError ? 'true' : undefined}
          aria-describedby="note-lines-help{linesError ? ' note-lines-error' : ''}"
        />
        <p id="note-lines-help" class="field-help">Line numbers of the program, such as 3-5, 12. If you open a case, the student sees these lines highlighted in their own code.</p>
        {#if linesError}<p id="note-lines-error" class="field-error">{linesError}</p>{/if}
      </div>
      {#if sendError}<p class="field-error" role="alert">{sendError}</p>{/if}
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={busy}>Save note</button>
        {#if caseId !== null}
          <a class="btn btn-quiet btn-link" href="/admin/integrity/cases/{caseId}">Go to the case</a>
        {:else}
          <button type="button" class="btn btn-secondary" disabled={busy} onclick={onopencase}>Open a case</button>
        {/if}
      </div>
    </form>
  {/if}
</dialog>

<style>
  .dialog-head h2 {
    font-size: var(--fs-sub);
  }

  .lead {
    margin-top: var(--space-2);
  }

  .notes {
    display: grid;
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .notes li {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .program {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }

  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-4);
  }

  .lines-input {
    max-width: 16rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
