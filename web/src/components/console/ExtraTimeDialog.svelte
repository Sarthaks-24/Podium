<!--
  ExtraTimeDialog: extra minutes for one student, from the console (stage
  C2): the student, the minutes (0 removes the extra time) and an optional
  reason, saved with PUT .../extensions/{user}. The students given extra
  time so far are listed under the form. After the end it says the
  contest has ended and offers only Close (walk bug B3). A refused field
  is marked and takes focus; any other refusal shows inside the dialog.

  Props: `open`, `base` (the contest's admin API path), `students`
  ({ user_id, name }), `extensions` (from the contest), `ended`, `onclose`
  and `ondone(message)`, given the sentence the page shows as a toast.
-->
<script lang="ts">
  import Dialog from '../Dialog.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import type { AdminContest } from '../../lib/contests';
  import { countOf } from '../../lib/format';

  interface Props {
    open: boolean;
    base: string;
    students: readonly { user_id: number; name: string }[];
    extensions: NonNullable<AdminContest['extensions']>;
    ended: boolean;
    onclose: () => void;
    ondone: (message: string) => Promise<void> | void;
  }

  let { open, base, students, extensions, ended, onclose, ondone }: Props = $props();

  let user = $state('');
  let minutes = $state('15');
  let reason = $state('');
  let error = $state('');
  /** Which field the error is about, so only that one is marked and focused. */
  let field = $state<'user' | 'minutes' | ''>('');
  let busy = $state(false);

  $effect(() => {
    if (!open) return;
    error = '';
    field = '';
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    field = '';
    const n = Math.round(Number(minutes));
    if (user === '') {
      error = 'Choose a student.';
      field = 'user';
      document.getElementById('ext-user')?.focus();
      return;
    }
    if (String(minutes).trim() === '' || !Number.isFinite(n) || n < 0) {
      error = 'Enter the minutes, 0 to remove the extra time.';
      field = 'minutes';
      document.getElementById('ext-minutes')?.focus();
      return;
    }
    busy = true;
    try {
      await sendJSON<void>('PUT', `${base}/extensions/${encodeURIComponent(user)}`, { minutes: n, reason: reason.trim() });
      const who = students.find((s) => String(s.user_id) === user)?.name ?? 'the student';
      reason = '';
      await ondone(n === 0 ? `Removed the extra time for ${who}` : `Gave ${who} ${countOf(n, 'more minute', 'more minutes')}`);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      error = e instanceof ApiError ? (e.fields.minutes ?? e.message) : errorText(e);
      field = e instanceof ApiError && e.fields.minutes ? 'minutes' : e instanceof ApiError && e.code === 'not_participant' ? 'user' : '';
      if (field) document.getElementById(field === 'minutes' ? 'ext-minutes' : 'ext-user')?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} title="Extra time for one student" closeLabel="Close">
  {#if ended}
    <p class="body" role="status">The contest has ended, so extra time can no longer be given.</p>
  {:else}
    <form class="form" novalidate onsubmit={submit}>
      <div class="field">
        <label for="ext-user">Student</label>
        <select
          id="ext-user"
          aria-required="true"
          class="select"
          bind:value={user}
          aria-invalid={error && field === 'user' ? 'true' : undefined}
          aria-describedby={error && field === 'user' ? 'ext-error' : undefined}
        >
          <option value="">Choose a student</option>
          {#each students as s (s.user_id)}<option value={String(s.user_id)}>{s.name}</option>{/each}
        </select>
      </div>
      <div class="field">
        <label for="ext-minutes">Extra minutes</label>
        <input
          id="ext-minutes"
          aria-required="true"
          class="input mono"
          type="number"
          min="0"
          inputmode="numeric"
          bind:value={minutes}
          aria-invalid={error && field === 'minutes' ? 'true' : undefined}
          aria-describedby="ext-minutes-help{error && field === 'minutes' ? ' ext-error' : ''}"
        />
        <p id="ext-minutes-help" class="field-help">Only this student's end moves. 0 removes the extra time.</p>
      </div>
      <div class="field">
        <label for="ext-reason">Reason (optional)</label>
        <input id="ext-reason" class="input" type="text" autocomplete="off" placeholder="Laptop crashed" bind:value={reason} />
      </div>
      {#if error}<p id="ext-error" class="field-error" role={field ? undefined : 'alert'}>{error}</p>{/if}
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={busy}>Give extra time</button>
        <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
      </div>
    </form>
  {/if}
  {#if extensions.length > 0}
    <h3 class="sub">Extra time so far</h3>
    <ul class="extensions small">
      {#each extensions as x (x.user_id)}
        <li>{x.name || x.email}: <span class="mono">{x.minutes}</span> min{x.reason ? `, ${x.reason}` : ''}</li>
      {/each}
    </ul>
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

  #ext-minutes {
    max-width: 9rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }

  .sub {
    margin-top: var(--space-5);
    font-size: var(--fs-sub);
  }

  .extensions {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink-muted);
  }
</style>
