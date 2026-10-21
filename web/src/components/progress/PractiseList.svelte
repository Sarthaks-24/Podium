<!--
  PractiseList: "Practise next" on "Your progress" (v2 frontend PLAN.md
  6.12, item 6). Up to six problems below full score, newest contest
  first, from the server's `retry` list: "Week 6, B, Interval covering"
  with "40 of 100" (digits in mono) and a "Practise" button to the problem
  when the contest is open for practice, otherwise the muted line
  "Practice is not open for this contest". One list with rules between the
  rows. Props: `rows` (RetryRow[]), `titles` (slug to contest title), and
  `organiser` (an organiser's view: no Practise buttons, only the list).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { scoreParts } from '../../lib/format';
  import { problemHref, shortTitle, type RetryRow } from '../../lib/progress';

  const MAX = 6;

  let { rows, titles, organiser = false }: { rows: RetryRow[]; titles: Record<string, string>; organiser?: boolean } = $props();

  const shown = $derived(rows.slice(0, MAX));
</script>

<ul class="practise">
  {#each shown as r (`${r.slug}/${r.label}`)}
    {@const id = `practise-${r.slug}-${r.label}`}
    <li>
      <p class="what" {id}>
        <span>{shortTitle(titles[r.slug] ?? r.slug)}, <span class="mono">{r.label}</span>, {r.title}</span>
        <span class="score"><MonoDigits parts={scoreParts(r.score, r.max_score)} /></span>
      </p>
      {#if !organiser}
        {#if r.practice_open}
          <a class="btn btn-link btn-compact" href={problemHref(r.slug, r.label)} aria-describedby={id}>Practise</a>
        {:else}
          <p class="muted small">Practice is not open for this contest</p>
        {/if}
      {/if}
    </li>
  {/each}
</ul>
{#if rows.length > MAX}
  <p class="muted small more">{rows.length - MAX} more below full score. They are in the table above.</p>
{/if}

<style>
  .practise {
    display: grid;
    padding: 0;
    list-style: none;
  }

  li {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  li:first-child {
    padding-top: 0;
  }

  li:last-child {
    padding-bottom: 0;
    border-bottom: 0;
  }

  .what {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--space-1) var(--space-3);
    min-width: 0;
  }

  .what > span:first-child {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .score {
    color: var(--ink-muted);
    white-space: nowrap;
  }

  li .btn {
    justify-self: start;
  }

  .more {
    margin-top: var(--space-3);
  }
</style>
