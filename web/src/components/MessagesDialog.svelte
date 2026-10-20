<!--
  MessagesDialog: everything organisers said during a contest, and the
  place to ask them something. A native modal dialog (Escape closes it,
  focus returns to the opener) listing announcements newest first, the
  student's own questions with their answers or "Waiting for an answer",
  and answers made public for everyone. While the student's window is open
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
  import { ApiError, sendJSON } from '../lib/api';
  import type { Clarification, Contest } from '../lib/contests';
  import { formatWhen } from '../lib/workspace';
  import Icon from './Icon.svelte';
  import Markdown from './Markdown.svelte';

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

  const announcements = $derived([...(contest.announcements ?? [])].sort((a, b) => b.created_at - a.created_at));
  const mine = $derived([...(contest.my_clarifications ?? [])].sort((a, b) => b.created_at - a.created_at));
  const everyone = $derived(
    [...(contest.public_clarifications ?? [])].filter((c) => !mine.some((m) => m.id === c.id)).sort((a, b) => b.created_at - a.created_at),
  );

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

  function topicText(label: string | null): string {
    return label ? `Problem ${label}` : 'General';
  }

  async function ask(event: SubmitEvent) {
    event.preventDefault();
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
      error = e instanceof ApiError ? (e.fields.question ?? e.message) : 'Something went wrong. Try again in a moment.';
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
      <button type="submit" class="btn btn-primary" disabled={busy}>Send question</button>
      <p class="sent" role="status">{sent}</p>
    </form>
  {:else}
    <p class="muted small closed">Questions close when your time in the contest ends.</p>
  {/if}

  <section class="section" aria-labelledby="msg-ann">
    <h3 id="msg-ann" class="section-title">Announcements</h3>
    {#if announcements.length === 0}
      <p class="muted small">No announcements yet.</p>
    {:else}
      <ul class="items">
        {#each announcements as a (a.id)}
          <li class="item">
            <p class="mono when">{formatWhen(a.created_at)}</p>
            <Markdown source={a.body_md} compact />
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section class="section" aria-labelledby="msg-mine">
    <h3 id="msg-mine" class="section-title">Your questions</h3>
    {#if mine.length === 0}
      <p class="muted small">You have not asked anything.</p>
    {:else}
      <ul class="items">
        {#each mine as c (c.id)}
          <li class="item">
            <p class="meta"><span class="mono when">{formatWhen(c.created_at)}</span> {topicText(c.problem_label)}{c.public ? ', answered for everyone' : ''}</p>
            <p class="q">{c.question}</p>
            {#if c.answer}<p class="a">{c.answer}</p>{:else}<p class="muted small">Waiting for an answer.</p>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  {#if everyone.length > 0}
    <section class="section" aria-labelledby="msg-public">
      <h3 id="msg-public" class="section-title">Answers for everyone</h3>
      <ul class="items">
        {#each everyone as c (c.id)}
          <li class="item">
            <p class="meta"><span class="mono when">{formatWhen(c.answered_at ?? c.created_at)}</span> {topicText(c.problem_label)}</p>
            <p class="q">{c.question}</p>
            <p class="a">{c.answer}</p>
          </li>
        {/each}
      </ul>
    </section>
  {/if}
</dialog>

<style>
  .section,
  .ask {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-5);
  }

  .ask {
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

  .items {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .item {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }

  .when,
  .meta {
    color: var(--ink-muted);
  }

  .q {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .a {
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .small {
    font-size: var(--fs-0);
  }
</style>
