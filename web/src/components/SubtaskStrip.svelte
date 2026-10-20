<!--
  SubtaskStrip: one bar per subtask, as wide as its points, filling as the
  judge reports tests (design.md 5.3 and 7). A subtask fills in --wait while
  it is being judged, then turns --ok when it passed or --bad when it
  failed. The fill grows over --dur-slow: this is one of the product's two
  orchestrated motions, and it is instant under prefers-reduced-motion.
  The score sits at the end in mono. The bars are drawn for sighted users;
  screen readers get the same facts from the image's label. Props:
  `submission`.
-->
<script lang="ts">
  import { isBad, type Submission } from '../lib/workspace';

  let { submission }: { submission: Submission } = $props();

  const cells = $derived(
    submission.subtasks.map((st) => {
      const total = st.tests.length;
      const judged = st.tests.filter((t) => t.verdict !== '').length;
      const finished = st.verdict !== '' || submission.state === 'done' || submission.state === 'error';
      const failed = isBad(st.verdict) || st.tests.some((t) => isBad(t.verdict));
      // A compile error or a judge failure leaves every subtask unjudged.
      const skipped =
        !failed &&
        (st.verdict === 'SK' ||
          submission.verdict === 'CE' ||
          submission.state === 'error' ||
          (total > 0 && st.tests.every((t) => t.verdict === 'SK')));
      const tone = st.verdict === 'AC' ? 'ok' : failed ? 'bad' : skipped ? 'wait' : finished && st.earned < st.points ? 'bad' : 'wait';
      const fill = finished || failed || skipped ? 1 : total > 0 ? judged / total : 0;
      const word =
        st.verdict === 'AC' ? 'passed' : tone === 'bad' ? 'failed' : skipped ? 'skipped' : judged > 0 ? 'being judged' : 'waiting';
      return { idx: st.idx, points: st.points, tone, fill, word };
    }),
  );

  const label = $derived(
    cells.length === 0
      ? 'No subtasks'
      : cells.map((c) => `Subtask ${c.idx}, ${c.points} points, ${c.word}`).join('; '),
  );
</script>

<div class="strip-row">
  <div class="strip" role="img" aria-label={label}>
    {#each cells as cell (cell.idx)}
      <span class="cell" style:flex-grow={Math.max(1, cell.points)}>
        <span class="fill tone-{cell.tone}" style:width="{Math.round(cell.fill * 100)}%"></span>
      </span>
    {/each}
  </div>
  <span class="score mono">{#if submission.state === 'done'}{submission.score} of {submission.max_score}{/if}</span>
</div>

<style>
  .strip-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .strip {
    display: flex;
    flex: 1 1 auto;
    gap: var(--space-1);
    min-width: 0;
  }

  .cell {
    position: relative;
    flex-basis: 0;
    height: var(--space-3);
    overflow: hidden;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
  }

  .fill {
    display: block;
    height: 100%;
    transition:
      width var(--dur-slow) var(--ease),
      background-color var(--dur) var(--ease);
  }

  .tone-ok {
    background: var(--ok);
  }

  .tone-bad {
    background: var(--bad);
  }

  .tone-wait {
    background: var(--wait);
  }

  .score {
    flex: none;
    font-size: var(--fs-0);
    font-variant-numeric: tabular-nums;
    color: var(--ink-muted);
  }

  @media (prefers-reduced-motion: reduce) {
    .fill {
      transition: none;
    }
  }

  /* Forced colours drop backgrounds: keep each cell's outline and draw the
     fill in the system highlight, so passed subtasks still read as filled. */
  @media (forced-colors: active) {
    .cell {
      border-color: CanvasText;
    }

    .fill {
      background: Highlight;
      forced-color-adjust: none;
    }

    /* A failed subtask stays unfilled, as it would read without colour. */
    .fill.tone-bad {
      background: Canvas;
    }

    .fill.tone-wait {
      background: GrayText;
    }
  }
</style>
