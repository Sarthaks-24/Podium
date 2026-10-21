<!--
  AppealRuling: one appeal as reviewers see it, and the form that rules on
  it (v2 P4 F9.3; POST /api/admin/appeals/{id}/ruling, docs/API.md "C9:
  appeals"). Shows who appealed and when, their text, the ruling deadline
  (with "Overdue" once it has passed), and the ruling once there is one.
  While the appeal is open and `mayRule` is not false, the form asks
  "Uphold the appeal" or "The decision stands", and a reason of at least
  20 characters that the student reads. Upholding reverses the outcome or
  the adjustment, so it asks first and says what changes. Only a reviewer
  who took no part may rule: when the server says so (403
  uninvolved_reviewer) the refusal is shown in place; an owner who is the
  only organiser can rule alone with a written reason. Props: `appeal`,
  `what` (the outcome or adjustment in words), `mayRule` (from the case;
  undefined when the page does not know), `owner` (the viewer is an owner)
  and `onruled` (gets the updated appeal).
-->
<script lang="ts">
  import Badge from '../Badge.svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { formatDateTime } from '../../lib/format';
  import { MIN_REASON, noteCounter, noteLength, type CaseAppeal } from '../../lib/review';
  import { focusFirstInvalid } from '../../lib/focus';
  import { appealState } from '../../lib/standing';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    appeal: CaseAppeal;
    what: string;
    mayRule?: boolean;
    owner?: boolean;
    onruled: (a: CaseAppeal) => void;
  }

  let { appeal, what, mayRule, owner = false, onruled }: Props = $props();

  let upheld = $state<'' | 'yes' | 'no'>('');
  let reason = $state('');
  let sole = $state(false);
  let soleReason = $state('');
  let errors = $state({ upheld: '', reason: '', sole: '' });
  let refusal = $state('');
  let busy = $state(false);
  let confirming = $state(false);

  const uid = $derived(`appeal-${appeal.id}`);
  const badge = $derived(appealState(appeal));
  const who = $derived(appeal.user?.name || appeal.user?.email || 'The student');

  function check(): boolean {
    errors = {
      upheld: upheld === '' ? 'Choose whether the appeal is upheld.' : '',
      reason:
        noteLength(reason) === 0
          ? 'Write the reason first. The student reads it.'
          : noteLength(reason) < MIN_REASON
            ? `Write at least ${MIN_REASON} characters.`
            : '',
      sole: sole && noteLength(soleReason) < MIN_REASON ? `Write at least ${MIN_REASON} characters on why you rule alone.` : '',
    };
    return !errors.upheld && !errors.reason && !errors.sole;
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    refusal = '';
    if (!check()) {
      void focusFirstInvalid(`#${uid}`);
      return;
    }
    if (upheld === 'yes') confirming = true;
    else void send();
  }

  async function send() {
    busy = true;
    try {
      const body: Record<string, unknown> = { upheld: upheld === 'yes', reason: reason.trim() };
      if (sole) body.sole_organiser_reason = soleReason.trim();
      const out = await sendJSON<CaseAppeal>('POST', `/api/admin/appeals/${appeal.id}/ruling`, body);
      showToast(upheld === 'yes' ? 'Upheld the appeal' : 'Ruled that the decision stands');
      onruled(out);
    } catch (e) {
      if (e instanceof ApiError && (e.fields.reason || e.fields.sole_organiser_reason)) {
        errors = { ...errors, reason: e.fields.reason ?? '', sole: e.fields.sole_organiser_reason ?? '' };
        confirming = false;
        await focusFirstInvalid(`#${uid}`);
      } else {
        refusal = errorText(e);
      }
    } finally {
      busy = false;
      confirming = false;
    }
  }
</script>

<article class="appeal" id={uid} aria-labelledby="{uid}-title">
  <div class="head">
    <h3 id="{uid}-title">Appeal by {who} against {what}</h3>
    <Badge tone={badge.tone} text={badge.word} />
    {#if appeal.state === 'open' && appeal.overdue}<Badge tone="bad" text="Overdue" />{/if}
  </div>
  <p class="muted small">
    <MonoDigits text={`Appealed ${formatDateTime(appeal.created_at)}`} />{#if appeal.state === 'open'}<MonoDigits
        text={`. Rule by ${formatDateTime(appeal.decide_by)}.`}
      />{/if}
  </p>
  <p class="text">{appeal.text}</p>

  {#if appeal.ruling}
    <div class="ruling">
      <p class="small">
        <strong>{appeal.ruling.upheld ? 'Upheld' : 'The decision stands'}</strong>, by {appeal.ruling.by_name || 'an organiser'},
        <MonoDigits text={formatDateTime(appeal.ruling.created_at)} />
      </p>
      <p class="text">{appeal.ruling.reason}</p>
    </div>
  {:else if mayRule === false}
    <p class="muted small">You took part in this decision, so another owner or admin rules on the appeal.</p>
  {:else}
    <form class="form" novalidate onsubmit={submit}>
      <fieldset class="choice-set" data-invalid={errors.upheld ? 'true' : undefined} aria-describedby={errors.upheld ? `${uid}-upheld-error` : undefined}>
        <legend>Ruling</legend>
        <label class="choice"><input type="radio" name="{uid}-upheld" value="yes" bind:group={upheld} /> Uphold the appeal: reverse {what}</label>
        <label class="choice"><input type="radio" name="{uid}-upheld" value="no" bind:group={upheld} /> The decision stands</label>
        {#if errors.upheld}<p id="{uid}-upheld-error" class="field-error">{errors.upheld}</p>{/if}
      </fieldset>
      <div class="field">
        <label for="{uid}-reason">Reason for the ruling, for the student</label>
        <textarea
          id="{uid}-reason"
          aria-required="true"
          class="input"
          rows="3"
          bind:value={reason}
          aria-invalid={errors.reason ? 'true' : undefined}
          aria-describedby="{uid}-reason-count{errors.reason ? ` ${uid}-reason-error` : ''}"
        ></textarea>
        <p id="{uid}-reason-count" class="field-help">{noteCounter(reason)}</p>
        {#if errors.reason}<p id="{uid}-reason-error" class="field-error">{errors.reason}</p>{/if}
      </div>
      {#if owner}
        <label class="choice"><input type="checkbox" bind:checked={sole} /> I am the only organiser and rule alone</label>
        {#if sole}
          <div class="field">
            <label for="{uid}-sole">Why you rule alone</label>
            <textarea
              id="{uid}-sole"
              aria-required="true"
              class="input"
              rows="2"
              bind:value={soleReason}
              aria-invalid={errors.sole ? 'true' : undefined}
              aria-describedby={errors.sole ? `${uid}-sole-error` : undefined}
            ></textarea>
            {#if errors.sole}<p id="{uid}-sole-error" class="field-error">{errors.sole}</p>{/if}
          </div>
        {/if}
      {/if}
      {#if refusal}<p class="field-error" role="alert">{refusal}</p>{/if}
      <button type="submit" class="btn btn-secondary" disabled={busy}>Rule on the appeal</button>
    </form>
  {/if}
</article>

<ConfirmDialog
  open={confirming}
  title="Uphold the appeal?"
  body="This reverses {what} for {who}. The student is told, and the ruling cannot be changed later."
  items={['Their rating gets back what the decision took, up to 10.', 'A zero on a contest board is removed and the ranks follow.']}
  confirmLabel="Uphold the appeal"
  {busy}
  onconfirm={send}
  oncancel={() => (confirming = false)}
/>

<style>
  .appeal {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-3);
    border-top: var(--rule) solid var(--line);
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .head h3 {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .text {
    max-width: var(--measure-prose);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .ruling {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-3);
    border: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .form {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
  }

  .form .field {
    width: 100%;
  }

  .choice-set {
    display: grid;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .choice-set legend {
    margin-bottom: var(--space-1);
    font-weight: 500;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }
</style>
