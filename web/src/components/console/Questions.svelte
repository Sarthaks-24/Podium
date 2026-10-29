<!--
  Questions: the console's questions from students (stage C2, pilot P9 and
  P12). Unanswered questions come first, the longest waiting at the top,
  each with who asked, the problem, when and how long it has waited. An
  answer is written in its own box; "Standard answer" fills the box with
  one of the standard answers to edit before sending, never sends it as
  is. "Who sees the answer" is the asker only or everyone, and says what
  students will see. Answered questions are folded under "Answered (3)",
  each with "Make public" or "Make private".

  Props: `base` (the contest's admin API path), `clarifications` (every
  question of the contest) and `onanswered`, called after an answer is
  saved so the page loads the questions again.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import Badge from '../Badge.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { askerName, CANNED_REPLIES, formatSpan, type Clarification } from '../../lib/contests';
  import { countOf } from '../../lib/format';
  import { ticker } from '../../lib/ticker.svelte';
  import { showToast } from '../../lib/toast.svelte';
  import { formatWhen } from '../../lib/workspace';

  interface Props {
    base: string;
    clarifications: readonly Clarification[];
    onanswered: () => Promise<void> | void;
  }

  let { base, clarifications, onanswered }: Props = $props();

  type Reply = { text: string; public: boolean; error: string; busy: boolean };
  let replies = $state<Record<number, Reply>>({});

  // Each question gets its reply draft here, never while rendering; a draft survives a reload of the list.
  $effect.pre(() => {
    const list = clarifications;
    untrack(() => {
      const next: Record<number, Reply> = {};
      for (const q of list) next[q.id] = replies[q.id] ?? { text: '', public: q.public, error: '', busy: false };
      replies = next;
    });
  });

  const waiting = $derived([...clarifications].filter((c) => !c.answer).sort((a, b) => a.created_at - b.created_at));
  const answered = $derived([...clarifications].filter((c) => c.answer).sort((a, b) => b.created_at - a.created_at));

  const EMPTY: Reply = { text: '', public: false, error: '', busy: false };

  function reply(c: Clarification): Reply {
    return replies[c.id] ?? EMPTY;
  }

  /** Puts a standard answer in the box to edit (P12); it is never sent by itself. */
  function insertStandard(c: Clarification, event: Event) {
    const select = event.currentTarget as HTMLSelectElement;
    const r = reply(c);
    if (select.value === '') return;
    r.text = CANNED_REPLIES[Number(select.value)] ?? r.text;
    select.value = '';
    document.getElementById(`reply-${c.id}`)?.focus();
  }

  async function answer(c: Clarification, overridePublic?: boolean) {
    const r = reply(c);
    r.error = '';
    const text = r.text.trim() || (c.answer ?? '');
    if (text === '') {
      r.error = 'Write an answer, or start from a standard one.';
      document.getElementById(`reply-${c.id}`)?.focus();
      return;
    }
    r.busy = true;
    const pub = overridePublic ?? r.public;
    try {
      await sendJSON<unknown>('PUT', `${base}/clarifications/${c.id}`, { answer: text, public: pub });
      showToast(c.answer ? (pub ? 'Made the answer public' : 'Made the answer private') : pub ? 'Sent the answer to everyone' : 'Sent the answer');
      replies[c.id] = { text: '', public: pub, error: '', busy: false };
      await onanswered();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      r.error = e instanceof ApiError ? (e.fields.answer ?? e.message) : errorText(e);
      document.getElementById(`reply-${c.id}`)?.focus();
    } finally {
      r.busy = false;
    }
  }
</script>

<section class="panel" aria-labelledby="clar-title">
  <div class="panel-head">
    <h2 id="clar-title">Questions</h2>
    {#if waiting.length > 0}<Badge tone="bad">{countOf(waiting.length, 'unanswered', 'unanswered')}</Badge>{/if}
  </div>
  {#if waiting.length === 0}
    <p class="muted small">{answered.length === 0 ? 'No questions yet. They appear here the moment a student asks.' : 'Every question is answered.'}</p>
  {:else}
    <ul class="items">
      {#each waiting as c (c.id)}
        {@const r = reply(c)}
        {@const who = askerName(c)}
        <li class="clar">
          <p class="meta small">
            {who}, {c.problem_label ? `problem ${c.problem_label}` : 'general'}, asked <MonoDigits text={formatWhen(c.created_at, ticker.now)} />, waiting
            <MonoDigits text={formatSpan(Math.max(0, ticker.now - c.created_at))} />
          </p>
          <p class="q">{c.question}</p>
          <div class="field">
            <label for="std-{c.id}">Standard answer</label>
            <select id="std-{c.id}" class="select" aria-describedby="std-{c.id}-help" onchange={(e) => insertStandard(c, e)}>
              <option value="">Choose one to edit</option>
              {#each CANNED_REPLIES as text, i (i)}<option value={String(i)}>{text}</option>{/each}
            </select>
            <p id="std-{c.id}-help" class="field-help">Choosing one fills the answer box, where you can change it.</p>
          </div>
          <div class="field">
            <label for="reply-{c.id}">Answer</label>
            <textarea
              id="reply-{c.id}"
              aria-required="true"
              class="input"
              rows="2"
              bind:value={r.text}
              aria-invalid={r.error ? 'true' : undefined}
              aria-describedby={r.error ? `reply-${c.id}-error` : undefined}
            ></textarea>
            {#if r.error}<p id="reply-{c.id}-error" class="field-error">{r.error}</p>{/if}
          </div>
          <fieldset class="audience" aria-describedby="who-{c.id}-help">
            <legend class="small">Who sees the answer</legend>
            <label class="choice"><input type="radio" name="who-{c.id}" checked={!r.public} onchange={() => (r.public = false)} /> Only {who}</label>
            <label class="choice"><input type="radio" name="who-{c.id}" checked={r.public} onchange={() => (r.public = true)} /> Everyone</label>
            <p id="who-{c.id}-help" class="field-help">
              {r.public
                ? 'Every student sees the question and this answer in Messages, without the name of who asked.'
                : `Only ${who} sees the answer, in Messages.`}
            </p>
          </fieldset>
          <button type="button" class="btn btn-secondary" disabled={r.busy} onclick={() => answer(c)}>
            {r.public ? 'Send to everyone' : 'Send answer'}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
  {#if answered.length > 0}
    <details class="answered">
      <summary>Answered ({answered.length})</summary>
      <ul class="items">
        {#each answered as c (c.id)}
          {@const r = reply(c)}
          <li>
            <p class="meta small">
              <MonoDigits text={formatWhen(c.created_at, ticker.now)} />
              {askerName(c)}, {c.problem_label ? `problem ${c.problem_label}` : 'general'}{c.public ? ', for everyone' : ''}
            </p>
            <p class="q">{c.question}</p>
            <p class="a">{c.answer}</p>
            <button type="button" class="btn btn-quiet btn-compact" disabled={r.busy} onclick={() => answer(c, !c.public)}>
              {c.public ? 'Make private' : 'Make public'}
            </button>
          </li>
        {/each}
      </ul>
    </details>
  {/if}
</section>

<style>
  .panel h2 {
    font-size: var(--fs-section);
  }

  .items {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .items > li {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }

  .items .field,
  .audience {
    width: 100%;
  }

  /* A waiting question is the one boxed thing in the column. */
  .clar {
    padding-inline: var(--space-3);
    border: var(--rule) solid var(--line-strong);
    background: var(--paper);
  }

  .meta {
    color: var(--ink-muted);
  }

  .q {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .a {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .audience {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-4);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .audience legend {
    padding: 0;
    color: var(--ink-muted);
  }

  .audience .field-help {
    flex-basis: 100%;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-size: var(--fs-0);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .answered > summary {
    width: fit-content;
    min-height: var(--control-h);
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    line-height: var(--control-h);
    cursor: pointer;
  }

  .answered[open] > summary {
    margin-bottom: var(--space-2);
  }
</style>
