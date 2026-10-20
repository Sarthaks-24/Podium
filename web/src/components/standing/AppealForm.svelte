<!--
  AppealForm: the student's appeal against an outcome or a standing
  adjustment (contract C9 "appeals", POST /api/me/appeals; v2 P4 F9.2).
  Closed it is one secondary button, "Appeal this decision"; open, a text
  box for why the decision is wrong (20 to 4,000 characters, counted as
  the server counts them), the deadline, and "Send appeal". The text is
  checked before it is sent and the server's refusal ("The 14 days to
  appeal have passed.") is shown under the box. On success the parent
  gets the new appeal and the form closes. The text goes to an organiser
  who took no part in the decision. Props: `target` ({ case_id } or
  { adjustment_id }), `appealBy` (the deadline, Unix ms, or null), `id`
  (a unique prefix for the field ids), and `onsent` (the created appeal).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { tick } from 'svelte';
  import { ApiError, sendJSON } from '../../lib/api';
  import { formatDayTime } from '../../lib/format';
  import { session } from '../../lib/session.svelte';
  import { appealLength, appealProblem, MAX_APPEAL, MIN_APPEAL, type StudentAppeal } from '../../lib/standing';

  interface Props {
    target: { case_id: number } | { adjustment_id: number };
    appealBy: number | null;
    id: string;
    onsent: (appeal: StudentAppeal) => void;
  }

  let { target, appealBy, id, onsent }: Props = $props();

  let open = $state(false);
  let text = $state('');
  let error = $state('');
  let busy = $state(false);
  let box: HTMLTextAreaElement | undefined = $state();

  async function start() {
    open = true;
    await tick();
    box?.focus();
  }

  async function send(event: SubmitEvent) {
    event.preventDefault();
    error = appealProblem(text);
    if (error) {
      box?.focus();
      return;
    }
    busy = true;
    try {
      const appeal = await sendJSON<StudentAppeal>('POST', '/api/me/appeals', { ...target, text: text.trim() });
      open = false;
      text = '';
      onsent(appeal ?? { state: 'open', created_at: Date.now() });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      error = e instanceof ApiError ? (e.fields.text ?? e.message) : 'Your appeal could not be sent. Try again in a moment.';
      box?.focus();
    } finally {
      busy = false;
    }
  }
</script>

{#if !open}
  <div class="action-row">
    <button type="button" class="btn btn-secondary" onclick={start}>Appeal this decision</button>
    {#if appealBy !== null}<span class="muted small">Until <MonoDigits text={formatDayTime(appealBy)} /></span>{/if}
  </div>
{:else}
  <form class="appeal" novalidate onsubmit={send}>
    <div class="field">
      <label for="{id}-text">Why do you think the decision is wrong?</label>
      <textarea
        id="{id}-text"
        name="appeal"
        autocomplete="off"
        class="input"
        rows="5"
        maxlength={MAX_APPEAL}
        bind:this={box}
        bind:value={text}
        aria-invalid={error ? 'true' : undefined}
        aria-describedby="{id}-help{error ? ` ${id}-error` : ''}"
      ></textarea>
      <p id="{id}-help" class="field-help">
        An organiser who took no part in the decision reads it and replies within 14 days. At least {MIN_APPEAL} characters; <MonoDigits text="{appealLength(text)} of {MAX_APPEAL}" /> so far.{#if appealBy !== null}
          You can appeal until <MonoDigits text={formatDayTime(appealBy)} />.{/if}
      </p>
      {#if error}<p id="{id}-error" class="field-error" role="alert">{error}</p>{/if}
    </div>
    <div class="action-row">
      <button type="submit" class="btn btn-primary" disabled={busy}>Send appeal</button>
      <button type="button" class="btn btn-quiet" onclick={() => ((open = false), (error = ''))}>Cancel</button>
    </div>
  </form>
{/if}

<style>
  .appeal {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-form);
  }

  .small {
    font-size: var(--fs-0);
  }
</style>
