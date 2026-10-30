<!--
  Overview: how the contest is going, under the console's opening sentence.
  How many students started,
  then one line per problem with a bar and "21 solved of 27 attempted"
  (GET /api/admin/contests/{id}/stats: solved is the full score), and the
  queue ("Queue: 3 waiting, 1 judging"). Each problem line has a quiet
  "Judge again" that the page asks about first.

  Props: `stats` (null while loading), `error` (why the numbers could not
  be loaded), `problems` (the contest's problems, for the letters and
  titles), `queue` (optional) and `onrejudge(label)`.
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import type { AdminContestProblem, ContestStats, QueueHealth } from '../../lib/contests';

  interface Props {
    stats: ContestStats | null;
    error: string;
    problems: readonly AdminContestProblem[];
    queue?: QueueHealth;
    onrejudge: (label: string) => void;
  }

  let { stats, error, problems, queue, onrejudge }: Props = $props();

  const rows = $derived(
    problems.map((p) => {
      const s = stats?.problems.find((x) => x.label === p.label);
      return { label: p.label, title: p.title, solved: s?.solved ?? 0, attempted: s?.attempted ?? 0 };
    }),
  );
  // The bars share one scale: everyone who started, or the most attempts when that is larger.
  const scale = $derived(Math.max(1, stats?.started ?? 0, ...rows.map((r) => r.attempted)));

  function share(n: number): string {
    return `${Math.round((n / scale) * 100)}%`;
  }
</script>

<section class="panel" aria-labelledby="overview-title">
  <h2 id="overview-title">Overview</h2>
  {#if error && stats === null}
    <p class="muted small">{error}</p>
  {:else if stats === null}
    <p class="muted small" role="status">Loading the numbers</p>
  {:else}
    {#if typeof stats.started === 'number'}<p><MonoDigits text={`${stats.started} started`} /></p>{/if}
    <ul class="probs">
      {#each rows as r (r.label)}
        <li>
          <span class="plabel" title={r.title}><span class="mono">{r.label}</span> {r.title}</span>
          <span class="bar" aria-hidden="true">
            <span class="tried" style:width={share(r.attempted)}></span>
            <span class="solved" style:width={share(r.solved)}></span>
          </span>
          <span class="nums small"><MonoDigits text={`${r.solved} solved of ${r.attempted} attempted`} /></span>
          <button type="button" class="btn btn-quiet btn-compact again" aria-label="Judge problem {r.label} again" onclick={() => onrejudge(r.label)}>Judge again</button>
        </li>
      {/each}
    </ul>
  {/if}
  {#if queue}
    <p class="small">
      <MonoDigits text={queue.waiting === 0 && queue.judging === 0 ? 'Queue: empty' : `Queue: ${queue.waiting} waiting, ${queue.judging} judging`} />
    </p>
  {/if}
</section>

<style>
  .panel h2 {
    font-size: var(--fs-section);
  }

  .probs {
    display: grid;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .probs li {
    display: grid;
    grid-template-columns: minmax(0, 12rem) minmax(4rem, 1fr) auto auto;
    align-items: center;
    gap: var(--space-3);
  }

  .plabel {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .plabel .mono {
    font-weight: 600;
  }

  /* Attempted is the faint part of the bar, solved the strong part over it. */
  .bar {
    position: relative;
    height: var(--space-2);
    background: var(--line);
  }

  .bar span {
    position: absolute;
    inset-block: 0;
    left: 0;
  }

  .tried {
    background: var(--line-strong);
  }

  .solved {
    background: var(--ok);
  }

  .nums {
    white-space: nowrap;
  }

  .again {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .probs li:hover .again,
  .again:focus-visible {
    color: var(--ink);
  }

  @media (forced-colors: active) {
    .solved {
      background: CanvasText;
    }

    .tried {
      background: GrayText;
    }
  }

  /* Phones: the numbers and Judge again go under the bar. */
  @media (width < 768px) {
    .probs li {
      grid-template-columns: minmax(0, 8rem) minmax(0, 1fr) auto;
      grid-template-areas:
        'label bar bar'
        '. nums again';
    }

    .plabel {
      grid-area: label;
    }

    .bar {
      grid-area: bar;
    }

    .nums {
      grid-area: nums;
    }

    .again {
      grid-area: again;
    }
  }
</style>
