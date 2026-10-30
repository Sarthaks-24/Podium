<!--
  MessagesDialog: everything organisers said during a contest, and the
  place to ask them something, in a panel down the right edge over the
  editor column (components/Dialog; Escape closes it, focus returns to
  the opener). The MessageList comes first: announcements newest
  first, the student's own questions with their answers or "Waiting for an
  answer", and answers made public for everyone. Then, while the student
  may still ask (during their window and in practice),
  "Ask a question" about the contest in general or one problem
  (POST /api/contests/{slug}/clarifications); a refused question shows the
  server's sentence under the field, and a sent one is confirmed in a
  status line inside the dialog (a toast would sit behind the modal).
  "About" starts on "The contest in general" and says which problem is
  open. Props: `open`, `contest`, `current`
  (the open problem's letter, or ""), `canAsk`, `onclose` and `onasked`
  (called after a question was sent, with the new clarification when the
  server returns it).
-->
<script lang="ts">
  import { tick } from 'svelte';
  import Dialog from './Dialog.svelte';
  import { ApiError, sendJSON } from '../lib/api';
  import type { Clarification, Contest } from '../lib/contests';
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

  let topic = $state('');
  let question = $state('');
  let busy = $state(false);
  let error = $state('');
  let sent = $state('');

  // Each opening starts clean on the general topic.
  $effect(() => {
    if (open) {
      topic = '';
      sent = '';
    }
  });

  async function ask(event: SubmitEvent) {
    event.preventDefault();
    // While a question is on its way the button stays focusable (aria-disabled) and repeats do nothing.
    if (busy) return;
    error = '';
    sent = '';
    const text = question.trim();
    if (text === '') {
      error = 'Write your question first.';
      // Every other form moves focus to the field it names.
      await tick();
      document.getElementById('ask-question')?.focus();
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
      // Reloading mid-contest would lose the typed question, so the fallback keeps it.
      error = e instanceof ApiError ? (e.fields.question ?? e.message) : 'Not sent. Your question is still here: try again in a moment.';
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} title="Messages" kind="sheet">
  <MessageList {contest} level={3} idPrefix="msg" />

  {#if canAsk}
    <form class="ask" novalidate onsubmit={ask}>
      <h3 class="section-title">Ask a question</h3>
      <div class="ask-fields">
        <div class="field">
          <label for="ask-topic">About</label>
          <select id="ask-topic" class="select" bind:value={topic} aria-describedby={current && topic === '' ? 'ask-topic-help' : undefined}>
            <option value="">The contest in general</option>
            {#each contest.problems ?? [] as p (p.label)}<option value={p.label}>Problem {p.label}: {p.title}</option>{/each}
          </select>
          {#if current && topic === ''}<p id="ask-topic-help" class="field-help">You have problem {current} open. Choose it above if the question is about it.</p>{/if}
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
</Dialog>

<style>
  .ask {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    justify-items: start;
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
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
</style>
