<!--
  MessagesDialog: everything organisers said during a contest, and the
  place to ask them something. A native modal dialog (Escape closes it,
  focus returns to the opener) with the MessageList: announcements newest
  first, the student's own questions with their answers or "Waiting for an
  answer", and answers made public for everyone. While the student's window is open
  a form asks a question about one problem or the contest in general
  (POST /api/contests/{slug}/clarifications); a refused question shows the
  server's sentence under the field, and a sent one is confirmed in a
  status line inside the dialog (a toast would sit behind the modal).
  "About" starts on the open problem. Props: `open`, `contest`, `current`
  (the open problem's letter, or ""), `canAsk`, `onclose` and `onasked`
  (called after a question was sent, with the new clarification when the
  server returns it).
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, sendJSON, PAGE_ERROR_MESSAGE } from '../lib/api';
  import type { Clarification, Contest } from '../lib/contests';
  import Icon from './Icon.svelte';
  import MessageList from './MessageList.svelte';

  interface Props {
    open: boolean;
    contest: Contest;
    current?: string;
    canAsk: boolean;
    onclose: () => void;
    onasked: (c: Clarification | null) => void;
  }

  let { open, contest, current = '', canAsk, onclose, onasked }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();
  let topic = $state('');
  let question = $state('');
  let busy = $state(false);
  let error = $state('');
  let sent = $state('');

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      topic = current;
      sent = '';
      dialog.showModal();
      tick().then(() => heading?.focus());
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancel(event: Event) {
    event.preventDefault();
    onclose();
  }

  async function ask(event: SubmitEvent) {
    event.preventDefault();
    // While a question is on its way the button stays focusable (aria-disabled) and repeats do nothing.
    if (busy) return;
    error = '';
    sent = '';
    const text = question.trim();
    if (text === '') {
      error = 'Write your question first.';
      return;
    }
    busy = true;
    try {
      const made = await sendJSON<{ clarification?: Clarification } | Clarification | undefined>(
        'POST',
        `/api/contests/${encodeURIComponent(contest.slug)}/clarifications`,
        { problem_label: topic === '' ? null : topic, question: text },
      );
      question = '';
      sent = 'Question sent. The answer appears here and as a banner.';
      // The server answers {clarification, server_now}; a bare clarification is taken too.
      const c = made && typeof made === 'object' ? ('clarification' in made ? made.clarification : made) : undefined;
      onasked(c && typeof c === 'object' && 'id' in c ? c : null);
    } catch (e) {
      error = e instanceof ApiError ? (e.fields.question ?? e.message) : PAGE_ERROR_MESSAGE;
    } finally {
      busy = false;
    }
  }
</script>

<dialog bind:this={dialog} class="dialog" aria-labelledby="messages-title" oncancel={onCancel}>
  <div class="dialog-head">
    <h2 id="messages-title" tabindex="-1" bind:this={heading}>Messages</h2>
    <button type="button" class="btn btn-quiet btn-compact" aria-label="Close messages" title="Close" onclick={onclose}><Icon name="x" /></button>
  </div>

  {#if canAsk}
    <form class="ask" novalidate onsubmit={ask}>
      <h3 class="section-title">Ask the organisers</h3>
      <div class="ask-fields">
        <div class="field">
          <label for="ask-topic">About</label>
          <select id="ask-topic" class="select" bind:value={topic}>
            <option value="">The contest in general</option>
            {#each contest.problems ?? [] as p (p.label)}<option value={p.label}>Problem {p.label}: {p.title}</option>{/each}
          </select>
        </div>
        <div class="field">
          <label for="ask-question">Question</label>
          <textarea
            id="ask-question"
            class="input"
            rows="3"
            bind:value={question}
            aria-invalid={error ? 'true' : undefined}
            aria-describedby="ask-help{error ? ' ask-error' : ''}"
          ></textarea>
          <p id="ask-help" class="field-help">Organisers answer you privately, or for everyone when the answer helps all.</p>
          {#if error}<p id="ask-error" class="field-error" role="alert">{error}</p>{/if}
        </div>
      </div>
      <button type="submit" class="btn btn-primary" aria-disabled={busy ? 'true' : undefined}>{busy ? 'Sending' : 'Send question'}</button>
      <p class="sent" role="status">{sent}</p>
    </form>
  {:else}
    <p class="muted small closed">Questions close when your time in the contest ends.</p>
  {/if}

  <MessageList {contest} level={3} idPrefix="msg" />
</dialog>

<style>
  .ask {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-5);
    justify-items: start;
    padding-bottom: var(--space-5);
    border-bottom: var(--rule) solid var(--line);
  }

  .ask-fields {
    display: grid;
    gap: var(--space-3);
    width: 100%;
  }

  .sent {
    font-size: var(--fs-0);
  }

  /* Kept in the page while empty, so the first "Question sent." is announced. */
  .sent:empty {
    min-height: 0;
  }

  .closed {
    margin-top: var(--space-4);
  }

  .section-title {
    font-size: var(--fs-1);
  }

  .small {
    font-size: var(--fs-0);
  }
</style>
