<!--
  ProblemRail: the contest's problems in the workspace rail (design.md
  5.3). From 1024 px each row is the letter, the title (cut to one line,
  whole on hover), the score in mono and a 2 px bar of earned over total;
  the open problem is tinted. Below 1024 px the same links sit in one
  row of letters with their scores, which scrolls sideways when there are
  many. Under the problems: the standings link and the Messages button with
  its unread count. In exam mode, until scores are out, a problem the
  student submitted reads "Submitted" (my_submission_count, read as 0 when
  the server leaves it out).
  Props: `problems`, `slug`, `current` (the open label), `layout` ("wide",
  "tablet" or "phone"), `exam`, `unread` (messages not yet seen) and
  `onmessages` (opens the messages dialog).
-->
<script lang="ts">
  import type { ContestProblem } from '../lib/contests';
  import Icon from './Icon.svelte';

  interface Props {
    problems: ContestProblem[];
    slug: string;
    current: string;
    layout: string;
    exam?: boolean;
    unread?: number;
    onmessages: () => void;
  }

  let { problems, slug, current, layout, exam = false, unread = 0, onmessages }: Props = $props();

  const base = $derived(`/contests/${encodeURIComponent(slug)}`);

  function count(p: ContestProblem): number {
    return p.my_submission_count ?? 0;
  }

  function scoreText(p: ContestProblem): string {
    if (p.my_score !== null) return `${p.my_score}/${p.max_score}`;
    if (exam && count(p) > 0) return 'Submitted';
    return '-';
  }

  function sentence(p: ContestProblem): string {
    if (p.my_score !== null) return `Problem ${p.label}, ${p.title}, ${p.my_score} of ${p.max_score} points`;
    if (exam) {
      const n = count(p);
      if (n === 0) return `Problem ${p.label}, ${p.title}, not submitted`;
      return `Problem ${p.label}, ${p.title}, submitted ${n === 1 ? 'once' : `${n} times`}, score after the contest`;
    }
    return `Problem ${p.label}, ${p.title}, not tried`;
  }

  function percent(p: ContestProblem): number {
    return p.my_score !== null && p.max_score > 0 ? Math.round((p.my_score / p.max_score) * 100) : 0;
  }
</script>

<nav class="rail-nav" class:wide={layout === 'wide'} aria-label="Problems">
  <ul class="problems">
    {#each problems as p (p.label)}
      <li>
        <a
          class="problem"
          href="{base}/problems/{encodeURIComponent(p.label)}"
          aria-current={p.label === current ? 'page' : undefined}
          aria-label={sentence(p)}
          title={p.title}
        >
          <span class="mono letter">{p.label}</span>
          {#if layout === 'wide'}<span class="title">{p.title}</span>{/if}
          <span class="mono score" class:sent={scoreText(p) === 'Submitted'}>{scoreText(p)}</span>
          <span class="bar" aria-hidden="true"><span class="fill" style:width="{percent(p)}%"></span></span>
        </a>
      </li>
    {/each}
  </ul>
  <div class="links">
    <a class="link" href="{base}/standings">Standings</a>
    <button type="button" class="btn btn-quiet btn-compact messages" onclick={onmessages}>
      <Icon name="message-square" />
      Messages{#if unread > 0}<span class="mono count">{unread}</span><span class="visually-hidden">&nbsp;unread</span>{/if}
    </button>
  </div>
</nav>

<style>
  .rail-nav {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
    margin-top: var(--space-3);
  }

  .problems {
    display: flex;
    gap: var(--space-1);
    min-width: 0;
    max-width: 100%;
    padding: 0;
    overflow-x: auto;
    list-style: none;
  }

  .problem {
    position: relative;
    display: inline-grid;
    grid-template-columns: auto auto;
    align-items: baseline;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding: var(--space-2) var(--space-3) var(--space-3);
    border-radius: var(--radius);
    color: var(--ink);
    text-decoration: none;
    white-space: nowrap;
  }

  .problem:hover {
    background: var(--surface-2);
  }

  .problem[aria-current='page'] {
    background: var(--act-tint);
  }

  .letter {
    font-weight: 400;
  }

  .score {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .score.sent {
    font-family: var(--font-ui);
  }

  .bar {
    position: absolute;
    right: var(--space-3);
    bottom: var(--space-2);
    left: var(--space-3);
    height: calc(2 * var(--rule));
    background: var(--line);
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--ok);
  }

  .links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .link {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .messages {
    color: var(--act);
  }

  .count {
    min-width: 1.5em;
    padding: 0 var(--space-1);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
    font-size: var(--fs-0);
    text-align: center;
  }

  /* The rail from 1024 px: one row per problem, links below. */
  .wide {
    display: grid;
    gap: var(--space-5);
    margin-top: 0;
  }

  .wide .problems {
    display: grid;
    gap: var(--space-1);
    overflow: visible;
  }

  .wide .problem {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    width: 100%;
    min-height: var(--control-h);
  }

  .wide .title {
    overflow: hidden;
    font-size: var(--fs-0);
    text-overflow: ellipsis;
  }

  .wide .links {
    display: grid;
    justify-items: start;
    gap: var(--space-1);
  }

  .wide .messages {
    margin-left: calc(-1 * var(--space-3));
  }

  /* Forced colours drop the tint and the bar fills: the current problem
     gets an outline and the bars keep their track and fill. */
  @media (forced-colors: active) {
    .problem[aria-current='page'] {
      outline: calc(2 * var(--rule)) solid Highlight;
    }

    .bar {
      outline: var(--rule) solid CanvasText;
    }

    .fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }
</style>
