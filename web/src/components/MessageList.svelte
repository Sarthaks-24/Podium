<!--
  MessageList: what organisers said in a contest, in three sections, each
  newest first: announcements, the student's own questions with their
  answers or "Waiting for an answer.", and answers made public for
  everyone (minus the student's own). MessagesDialog shows it during a
  contest and ContestPage after it, so the copy and the dates never
  differ (council C1-010). Props: `contest` (its announcements,
  my_clarifications and public_clarifications), `level` (2 or 3, the
  heading level of each section), `idPrefix` (keeps heading ids unique on
  the page), and `framed` (each section is a panel and empty sections are
  left out, for a page column; otherwise empty announcements and questions
  say so, for the dialog).
-->
<script lang="ts">
  import MonoDigits from './MonoDigits.svelte';
  import Markdown from './Markdown.svelte';
  import type { Contest } from '../lib/contests';
  import { formatWhen } from '../lib/workspace';

  interface Props {
    contest: Pick<Contest, 'announcements' | 'my_clarifications' | 'public_clarifications'>;
    level?: 2 | 3;
    idPrefix: string;
    framed?: boolean;
  }

  let { contest, level = 3, idPrefix, framed = false }: Props = $props();

  const heading = $derived(`h${level}`);
  const announcements = $derived([...(contest.announcements ?? [])].sort((a, b) => b.created_at - a.created_at));
  const mine = $derived([...(contest.my_clarifications ?? [])].sort((a, b) => b.created_at - a.created_at));
  const everyone = $derived(
    [...(contest.public_clarifications ?? [])].filter((c) => !mine.some((m) => m.id === c.id)).sort((a, b) => b.created_at - a.created_at),
  );

  function topicText(label: string | null): string {
    return label ? `Problem ${label}` : 'General';
  }
</script>

{#if !framed || announcements.length > 0}
  <section class={framed ? 'panel' : 'section'} aria-labelledby="{idPrefix}-ann">
    <svelte:element this={heading} id="{idPrefix}-ann" class="section-title">Announcements</svelte:element>
    {#if announcements.length === 0}
      <p class="muted small">No announcements yet.</p>
    {:else}
      <ul class="items">
        {#each announcements as a (a.id)}
          <li class="item">
            <p class="when"><MonoDigits text={formatWhen(a.created_at)} /></p>
            <Markdown source={a.body_md} compact />
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

{#if !framed || mine.length > 0}
  <section class={framed ? 'panel' : 'section'} aria-labelledby="{idPrefix}-mine">
    <svelte:element this={heading} id="{idPrefix}-mine" class="section-title">Your questions</svelte:element>
    {#if mine.length === 0}
      <p class="muted small">You have not asked anything.</p>
    {:else}
      <ul class="items">
        {#each mine as c (c.id)}
          <li class="item">
            <p class="meta"><span class="when"><MonoDigits text={formatWhen(c.created_at)} /></span> {topicText(c.problem_label)}{c.public ? ', answered for everyone' : ''}</p>
            <p class="q">{c.question}</p>
            {#if c.answer}<p class="a">{c.answer}</p>{:else}<p class="muted small">Waiting for an answer.</p>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

{#if everyone.length > 0}
  <section class={framed ? 'panel' : 'section'} aria-labelledby="{idPrefix}-public">
    <svelte:element this={heading} id="{idPrefix}-public" class="section-title">Answers for everyone</svelte:element>
    <ul class="items">
      {#each everyone as c (c.id)}
        <li class="item">
          <p class="meta"><span class="when"><MonoDigits text={formatWhen(c.answered_at ?? c.created_at)} /></span> {topicText(c.problem_label)}</p>
          <p class="q">{c.question}</p>
          <p class="a">{c.answer}</p>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-5);
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
