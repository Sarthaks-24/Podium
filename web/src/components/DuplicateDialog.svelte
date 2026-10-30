<!--
  DuplicateDialog: "Duplicate" from a contest's More menu. It asks only for
  the copy's start (a week after this contest's by default); the copy keeps
  the setup, the problems and the length, titled "(copy)", and starts as a draft.
  "Duplicate contest" POSTs .../clone with the new start and the end that
  keeps the length; the server names the copy's address from the title. A
  refusal is shown inside the dialog, under the start when it is about the
  times.

  Props: `open`, `base` (the contest's admin API path), `title`,
  `startsAt` and `endsAt` (this contest's times), `onclose` and
  `ondone(made)` with the new contest.
-->
<script lang="ts">
  import Dialog from './Dialog.svelte';
  import { ApiError, errorText, isSignedOut, sendJSON } from '../lib/api';
  import { fromLocalInput, toLocalInput, type AdminContest } from '../lib/contests';

  interface Props {
    open: boolean;
    base: string;
    title: string;
    startsAt: number;
    endsAt: number;
    onclose: () => void;
    ondone: (made: AdminContest) => void;
  }

  let { open, base, title, startsAt, endsAt, onclose, ondone }: Props = $props();

  let start = $state('');
  let fieldError = $state('');
  let error = $state('');
  let busy = $state(false);

  // Each opening starts clean, a week after this contest.
  $effect(() => {
    if (!open) return;
    start = toLocalInput(startsAt + 7 * 86_400_000);
    fieldError = '';
    error = '';
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    fieldError = '';
    error = '';
    const starts = fromLocalInput(start);
    if (starts === null) {
      fieldError = 'Enter the start date and time.';
      document.getElementById('dup-start')?.focus();
      return;
    }
    busy = true;
    try {
      ondone(await sendJSON<AdminContest>('POST', `${base}/clone`, { starts_at: starts, ends_at: starts + (endsAt - startsAt) }));
    } catch (e) {
      if (isSignedOut(e)) return;
      const timing = e instanceof ApiError ? (e.fields.starts_at ?? e.fields.ends_at) : undefined;
      if (timing) {
        fieldError = timing;
        document.getElementById('dup-start')?.focus();
      } else error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} kind="small" title="Duplicate {title}" closeLabel="Close">
  <form class="form" novalidate onsubmit={submit}>
    <p class="body">A new draft with the same setup, problems and length, titled {title} (copy).</p>
    <div class="field">
      <label for="dup-start">Starts</label>
      <input
        id="dup-start"
        aria-required="true"
        class="input"
        type="datetime-local"
        bind:value={start}
        aria-invalid={fieldError ? 'true' : undefined}
        aria-describedby={fieldError ? 'dup-error' : undefined}
      />
      {#if fieldError}<p id="dup-error" class="field-error">{fieldError}</p>{/if}
    </div>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Duplicate contest</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
</Dialog>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-3);
  }

  .body {
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
</style>
