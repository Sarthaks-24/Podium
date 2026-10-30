<!--
  ContestClock: the time a student has left, in mono on the
  server's clock (lib/ticker.svelte.ts). The clock says nothing about the
  standings: what the board hides is said once, on the standings.
  A polite live region speaks only at 30, 10, 5 and 1
  minutes left and when time is up; the timer itself is never announced.
  In the last 5 minutes a sentence under the time says so ("Under 5
  minutes left. Submit what you have."), with no colour, to stay calm; the
  mono face has one weight, so the time is never set in a faked bold. At zero the clock says "Time is up. Your submissions are saved." on
  screen too. Props: `endsAt` (Unix ms),
  `onexpire` (called once when the time reaches zero) and `compact` (for
  the sticky strip under 1024 px: the time alone, the "Time left"
  label kept for screen readers only).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { clockMilestone, formatCountdown } from '../lib/contests';
  import { ticker, useTicker } from '../lib/ticker.svelte';

  interface Props {
    endsAt: number;
    onexpire?: () => void;
    compact?: boolean;
  }

  let { endsAt, onexpire, compact = false }: Props = $props();

  const left = $derived(endsAt - ticker.now);

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

<div class="clock" class:compact data-testid="contest-clock">
  <p class="label" id="clock-label">Time left</p>
  <p class="mono time" role="timer" aria-labelledby="clock-label">{formatCountdown(left)}</p>
  {#if left > 0 && left <= 5 * 60_000}<p class="late">Under 5 minutes left. Submit what you have.</p>{/if}
  {#if left <= 0}<p class="up">Time is up. Your submissions are saved.</p>{/if}
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

  .time {
    font-synthesis: none;
  }

  .late {
    padding-bottom: var(--space-1);
    border-bottom: var(--rule) solid var(--ink);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .up {
    font-size: var(--fs-0);
    font-weight: 600;
  }

  /* The workspace strip under 1024 px: the time beside the previous and next problem; "Time left" stays for
     screen readers as the timer's name. */
  .compact {
    gap: 0;
  }

  .compact .label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .compact .time {
    font-size: var(--fs-2);
  }

  .compact .late,
  .compact .up {
    padding-bottom: 0;
    border-bottom: 0;
  }
</style>
