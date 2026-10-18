<!--
  SubmissionList: the person's own submissions to this problem, newest
  first. Each row reads as when, language, outcome and score, and opens in
  place to show the full verdict panel for that submission. Props:
  `submissions` (newest first), `onjump` (passed to each panel for compile
  error jumps).
-->
<script lang="ts">
  import { formatWhen, headline, languageLabel, type Submission } from '../lib/workspace';
  import VerdictPanel from './VerdictPanel.svelte';

  interface Props {
    submissions: Submission[];
    onjump: ((line: number, col: number | null) => void) | null;
  }

  let { submissions, onjump }: Props = $props();

  let open = $state<Record<number, boolean>>({});
</script>

{#if submissions.length === 0}
  <p class="muted empty">No submissions yet. Submit when your code passes the samples.</p>
{:else}
  <ul class="list">
    {#each submissions as s (s.id)}
      {@const h = headline(s)}
      <li class="item">
        <button
          type="button"
          class="row"
          aria-expanded={open[s.id] === true}
          aria-controls="submission-{s.id}"
          onclick={() => (open[s.id] = !open[s.id])}
        >
          <span class="mono when">{formatWhen(s.created_at)}</span>
          <span class="lang">{languageLabel(s.language)}</span>
          <span class="outcome tone-{h.tone}">{h.text}</span>
          <span class="mono score">{s.state === 'done' ? `${s.score} of ${s.max_score}` : ''}</span>
        </button>
        <div id="submission-{s.id}" class="body" hidden={open[s.id] !== true}>
          {#if open[s.id]}
            <VerdictPanel submission={s} {onjump} idPrefix="submission-{s.id}" />
          {/if}
        </div>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty {
    font-size: var(--fs-0);
  }

  .list {
    container-type: inline-size;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .item {
    border-bottom: var(--rule) solid var(--line);
  }

  .row {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr) auto;
    align-items: baseline;
    gap: var(--space-3);
    width: 100%;
    min-height: var(--control-h);
    padding: var(--space-2) 0;
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--ink);
    font-size: var(--fs-0);
    text-align: left;
  }

  .row:hover .outcome {
    text-decoration: underline;
  }

  .when,
  .lang,
  .score {
    color: var(--ink-muted);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .outcome {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-part {
    color: var(--part);
  }

  .tone-bad {
    color: var(--bad);
  }

  .tone-wait {
    color: var(--ink-muted);
  }

  .body {
    padding-bottom: var(--space-3);
  }

  .body[hidden] {
    display: none;
  }

  @container (max-width: 22rem) {
    .row {
      grid-template-columns: auto minmax(0, 1fr) auto;
    }

    .lang {
      display: none;
    }
  }
</style>
