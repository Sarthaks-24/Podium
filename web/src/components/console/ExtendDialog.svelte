<!--
  ExtendDialog: "Extend the contest" for everyone, from the console (stage
  C2). The minutes to add, the new end said before anything is sent ("Every
  student's contest then ends at 15:40"), and "Extend contest", which
  POSTs .../extend. After the end it says the contest has ended and offers only
  the Close button (walk bug B3); the server refuses too, and a refusal is shown
  inside the dialog, where it can be read over the backdrop.

  Props: `open`, `base` (the contest's admin API path), `closeAt` (the end
  now, with time added for everyone), `ended`, `onclose` and
  `ondone(minutes)`.
-->
<script lang="ts">
  import Dialog from '../Dialog.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { countOf } from '../../lib/format';
  import { ticker } from '../../lib/ticker.svelte';
  import { formatWhen } from '../../lib/workspace';

  interface Props {
    open: boolean;
    base: string;
    closeAt: number;
    ended: boolean;
    onclose: () => void;
    ondone: (minutes: number) => Promise<void> | void;
  }

  let { open, base, closeAt, ended, onclose, ondone }: Props = $props();

  let minutes = $state('10');
  let fieldError = $state('');
  let error = $state('');
  let busy = $state(false);

  const n = $derived(Math.round(Number(minutes)));
  const valid = $derived(String(minutes).trim() !== '' && Number.isFinite(n) && n >= 1 && n <= 240);

  // Each opening starts clean.
  $effect(() => {
    if (!open) return;
    fieldError = '';
    error = '';
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    fieldError = '';
    error = '';
    if (!valid) {
      fieldError = 'Enter the minutes to add, from 1 to 240.';
      document.getElementById('extend-minutes')?.focus();
      return;
    }
    busy = true;
    try {
      await sendJSON<unknown>('POST', `${base}/extend`, { minutes: n });
      await ondone(n);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      if (e instanceof ApiError && e.fields.minutes) {
        fieldError = e.fields.minutes;
        document.getElementById('extend-minutes')?.focus();
      } else error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} kind="small" title="Extend the contest" closeLabel="Close">
  {#if ended}
    <p class="body" role="status">The contest has ended. Extending it now would reopen it for every student, so time can no longer be added.</p>
  {:else}
    <form class="form" novalidate onsubmit={submit}>
      <div class="field">
        <label for="extend-minutes">Minutes to add for everyone</label>
        <input
          id="extend-minutes"
          aria-required="true"
          class="input mono"
          type="number"
          min="1"
          max="240"
          inputmode="numeric"
          bind:value={minutes}
          aria-invalid={fieldError ? 'true' : undefined}
          aria-describedby="extend-help{fieldError ? ' extend-error' : ''}"
        />
        <p id="extend-help" class="field-help">
          {valid
            ? `Every student's contest then ends at ${formatWhen(closeAt + n * 60_000, ticker.now)}, ${countOf(n, 'minute')} later, and their clocks update at once.`
            : 'Their clocks update at once. Post an announcement so students know.'}
        </p>
        {#if fieldError}<p id="extend-error" class="field-error">{fieldError}</p>{/if}
      </div>
      {#if error}<p class="field-error" role="alert">{error}</p>{/if}
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={busy}>Extend contest</button>
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
    color: var(--ink-muted);
  }

  .field input {
    max-width: 9rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
</style>
