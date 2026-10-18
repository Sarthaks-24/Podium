<!--
  ContestClock: the time a student has left (design.md 5.3), in mono on the
  server's clock (lib/ticker.svelte.ts), with the freeze line under it
  ("Board freezes in 32 min", then "Board frozen") for fixed-start contests
  with a freeze. A polite live region speaks only at 30, 10, 5 and 1
  minutes left and when time is up; the timer itself is never announced.
  In the last 5 minutes the time turns bold (no colour, to stay calm), and
  at zero the clock says "Time is up. Your submissions are saved." on
  screen too. Props: `endsAt` (Unix ms), `freeze` (Unix ms or null),
  `onexpire` (called once when the time reaches zero) and `compact` (one
  line, for the sticky strip under 1024 px).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { clockMilestone, formatCountdown, freezeText } from '../lib/contests';
  import { ticker, useTicker } from '../lib/ticker.svelte';

  interface Props {
    endsAt: number;
    freeze: number | null;
    onexpire?: () => void;
    compact?: boolean;
  }

  let { endsAt, freeze, onexpire, compact = false }: Props = $props();

  const left = $derived(endsAt - ticker.now);
  const freezeLine = $derived(freezeText(freeze, ticker.now));

  let spoken = $state('');
  let prev: number | null = null;
  let expired = false;

  $effect(() => {
    const now = left;
    if (prev !== null) {
      const m = clockMilestone(prev, now);
      if (m) spoken = m;
    }
    prev = now;
    if (now <= 0 && !expired) {
      expired = true;
      onexpire?.();
    } else if (now > 0) {
      expired = false;
    }
  });

  onMount(() => useTicker());
</script>

<div class="clock" class:compact>
  <p class="label" id="clock-label">Time left</p>
  <p class="mono time" class:late={left <= 5 * 60_000} role="timer" aria-labelledby="clock-label">{formatCountdown(left)}</p>
  {#if left <= 0}<p class="up">Time is up. Your submissions are saved.</p>{/if}
  {#if freezeLine}<p class="freeze" class:frozen={freeze !== null && ticker.now >= freeze}>{freezeLine}</p>{/if}
  <p class="visually-hidden" aria-live="polite">{spoken}</p>
</div>

<style>
  .clock {
    display: grid;
    gap: var(--space-1);
  }

  .label {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .time {
    font-size: var(--fs-3);
    line-height: var(--lh-tight);
  }

  .time.late {
    font-weight: 600;
  }

  .up {
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .freeze {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .freeze.frozen {
    color: var(--ink);
    font-weight: 500;
  }

  .compact {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 var(--space-3);
  }

  .compact .time {
    font-size: var(--fs-2);
  }
</style>
