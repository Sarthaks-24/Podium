<!--
  ProgressGrid: "Contests and problems" on "Your progress" (v2 frontend
  PLAN.md 6.12, item 3). One row per contest, newest first: the contest
  (a link to it), the score ("240 of 300", digits in mono), the final
  place as a small muted fact ("12 of 140", D95), then one cell per
  problem drawn like a standings cell (lib/standings.ts): the letter, the
  score and a 4 px bar along the bottom with the earned share in --ok or
  --part on a --line track, zero in --bad. Each cell carries a hidden
  sentence ("Problem B, 40 of 100, 3 attempts, best result wrong answer.")
  so the bar never speaks alone. An exam-mode contest shows its total only.
  Problems a review decision set to zero are listed under the table, each
  with "See your case" for the student (or a plain line for organisers).
  Under 768 px each row stacks: the contest, then score and place, then
  the cells, which wrap. Props: `contests`, `organiser` (the view is an
  organiser's, so contest links go to the results page and the case line
  is worded for them).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { formatDayTime, scoreParts } from '../../lib/format';
  import { cellSentence, isFull, problemTone, rankText, zeroedProblems, type ProgressContest } from '../../lib/progress';

  let { contests, organiser = false }: { contests: ProgressContest[]; organiser?: boolean } = $props();

  const zeroed = $derived(zeroedProblems(contests));

  function contestHref(c: ProgressContest): string {
    if (organiser && typeof c.contest_id === 'number') return `/admin/contests/${c.contest_id}/results`;
    return `/contests/${encodeURIComponent(c.slug)}`;
  }

  function fraction(score: number | null, max: number): number {
    if (score === null || !(max > 0)) return 0;
    return Math.min(1, Math.max(0, score / max));
  }
</script>

<div class="table-wrap">
  <table class="table table-stack grid">
    <caption class="visually-hidden">Your score in each contest and on each problem, newest first</caption>
    <thead>
      <tr>
        <th scope="col">Contest</th>
        <th scope="col" class="right">Score</th>
        <th scope="col" class="right">Place</th>
        <th scope="col">Problems</th>
      </tr>
    </thead>
    <tbody>
      {#each contests as c (c.slug)}
        {@const rank = rankText(c)}
        <tr>
          <th scope="row" class="title">
            <a href={contestHref(c)}>{c.title}</a>
            <span class="when"><MonoDigits text={formatDayTime(c.ended_at)} /></span>
          </th>
          <td class="right score"><MonoDigits parts={scoreParts(c.score, c.max_score)} /></td>
          <td class="right place">
            {#if rank !== ''}<span class="cell-label">Place&nbsp;</span><MonoDigits text={rank} />{/if}
          </td>
          <td class="cells">
            {#if isFull(c)}
              <ul class="pcells">
                {#each c.problems as p (p.label)}
                  {@const tone = problemTone(p)}
                  <li class="pcell tone-{tone}" class:decided={p.zero_by_decision}>
                    <span class="letter" aria-hidden="true">{p.label}</span>
                    <span class="ctext mono" aria-hidden="true">{p.score ?? ''}</span>
                    {#if tone !== 'none'}
                      <span class="meter" aria-hidden="true"><span class="meter-fill" style:width="{Math.round(fraction(p.score, p.max_score) * 100)}%"></span></span>
                    {/if}
                    <span class="visually-hidden">{cellSentence(p)}</span>
                  </li>
                {/each}
              </ul>
            {:else}
              <span class="muted small">Totals only: this contest hid per-problem results.</span>
            {/if}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

{#if zeroed.length > 0}
  <ul class="zeroed">
    {#each zeroed as z (`${z.contest.slug}/${z.problem.label}`)}
      <li>
        {z.contest.title}, problem {z.problem.label}: zero on this problem by decision.
        {#if !organiser}<a href="/integrity">See your case</a>{/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .grid {
    min-width: 0;
  }

  .title a {
    font-weight: 500;
  }

  .when {
    display: block;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .score,
  .place {
    white-space: nowrap;
  }

  .place {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .cell-label {
    display: none;
  }

  .pcells {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  /* A standings cell (StandingsTable, audit S8): --surface with the earned
     share as a 4 px bar along the bottom on a --line track. */
  .pcell {
    position: relative;
    display: grid;
    grid-template-columns: auto auto;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2);
    min-width: 4.5rem;
    padding: var(--space-1) var(--space-2) var(--space-3);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .letter {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .ctext {
    font-variant-numeric: tabular-nums;
  }

  .tone-zero .ctext {
    color: var(--bad);
  }

  .meter {
    position: absolute;
    right: var(--space-2);
    bottom: var(--space-1);
    left: var(--space-2);
    height: var(--space-1);
    background: var(--line);
  }

  .meter-fill {
    display: block;
    height: 100%;
  }

  .tone-ok .meter-fill {
    background: var(--ok);
  }

  .tone-part .meter-fill {
    background: var(--part);
  }

  .zeroed {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-3);
    padding: 0;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    list-style: none;
  }

  @media (forced-colors: active) {
    .meter {
      outline: var(--rule) solid CanvasText;
    }

    .meter-fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }

  /* Phones: the contest, then score and place, then the cells. */
  @media (width < 768px) {
    .grid tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title title'
        'score place'
        'cells cells';
    }

    .title {
      grid-area: title;
    }

    .score,
    .table .score {
      grid-area: score;
      text-align: left;
    }

    .place {
      grid-area: place;
    }

    .cells {
      grid-area: cells;
      padding-top: var(--space-1);
    }

    .cell-label {
      display: inline;
    }
  }
</style>
