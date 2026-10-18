<!--
  Home: "Your contests" (design.md 5.2), the signed-in landing page. One
  list from GET /api/contests, not cards: live contests first with their
  countdown in mono, then upcoming ones soonest first, then past ones
  newest first. Each row gives the title (a link to the contest), when it
  runs, how long it lasts, the problem count, the student's score once
  they have one, and a state word (Upcoming, Live, Frozen, Ended,
  Practice). From 768 to 1023 px the length and problem count are left
  out; under 768 px each row reads as title and state, then the start,
  then the length and the student's score. A countdown over a day reads
  as a span ("Starts in 2 d 23 h"). Organisers also get a link to the contest
  admin. The app shell sends signed-out visitors to /signin first. Takes no
  props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import { formatCountdown, formatSpan, lengthText, listCountdown, orderedContests, stateWord, type ContestList, type ContestSummary } from '../lib/contests';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { session } from '../lib/session.svelte';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';

  let loadState = $state<'loading' | 'ready' | 'error'>('loading');
  let loadError = $state('');
  let contests = $state<ContestSummary[]>([]);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');

  async function load() {
    loadState = 'loading';
    try {
      const sent = Date.now();
      const data = await getJSON<ContestList>('/api/contests');
      if (typeof data.server_now === 'number') serverClock.observe(data.server_now, sent, Date.now());
      refreshTicker();
      contests = orderedContests(data);
      loadState = 'ready';
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      loadState = 'error';
    }
  }

  // Reload when a countdown runs out, so the state words move on.
  let reloadAt = $derived.by(() => {
    let next = Number.POSITIVE_INFINITY;
    for (const c of contests) {
      const cd = listCountdown(c, ticker.now);
      if (cd && cd.ms > 0) next = Math.min(next, ticker.now + cd.ms);
    }
    return next;
  });

  $effect(() => {
    if (loadState === 'ready' && ticker.now >= reloadAt) void load();
  });

  function scoreText(c: ContestSummary): string {
    if (c.my_score === null) return '';
    return c.max_score !== undefined ? `${c.my_score} of ${c.max_score}` : String(c.my_score);
  }

  onMount(() => {
    setTitle('Your contests');
    void load();
    return useTicker();
  });
</script>

<h1 tabindex="-1">Your contests</h1>

{#if isOrganiser}
  <p class="org muted">To create a contest or follow one live, open <a href="/admin/contests">Contest admin</a>.</p>
{/if}

{#if loadState === 'loading'}
  <p class="lead muted" role="status">Loading contests</p>
{:else if loadState === 'error'}
  <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
  <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
{:else if contests.length === 0}
  <p class="lead muted">No contests yet. When an organiser schedules one, it appears here.</p>
{:else}
  <div class="table-wrap list">
    <table class="table">
      <thead>
        <tr>
          <th scope="col">Contest</th>
          <th scope="col">When</th>
          <th scope="col" class="length">Length</th>
          <th scope="col" class="right problems">Problems</th>
          <th scope="col" class="right">Your score</th>
          <th scope="col">State</th>
        </tr>
      </thead>
      <tbody>
        {#each contests as c (c.slug)}
          {@const cd = listCountdown(c, ticker.now)}
          <tr class:live={c.state === 'live' || c.state === 'frozen'}>
            <th scope="row" class="title">
              <a href="/contests/{encodeURIComponent(c.slug)}">{c.title}</a>
              {#if cd && cd.ms > 0}
                <span class="countdown">{cd.lead} <span class="mono">{cd.ms >= 86_400_000 ? formatSpan(cd.ms) : formatCountdown(cd.ms)}</span></span>
              {/if}
            </th>
            <td class="num when">{formatDateTime(c.starts_at)}</td>
            <td class="length">{lengthText(c)}</td>
            <td class="num right problems"><span class="cell-label">Problems</span>{c.problem_count}</td>
            <td class="num right score">{#if scoreText(c)}<span class="cell-label">Score</span>{scoreText(c)}{/if}</td>
            <td class="state state-{c.state}">{stateWord(c.state)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .org {
    margin-top: var(--space-3);
    font-size: var(--fs-0);
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .list {
    margin-top: var(--space-5);
  }

  .title {
    min-width: 12rem;
    font-weight: 500;
  }

  .countdown {
    display: block;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .live .countdown {
    color: var(--ink);
  }

  .length {
    white-space: nowrap;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .cell-label {
    display: none;
  }

  .state {
    color: var(--ink-muted);
    white-space: nowrap;
  }

  .state-live,
  .state-frozen {
    color: var(--act);
    font-weight: 600;
  }

  .state-practice {
    color: var(--ink);
  }

  /* Tablets: the length and problem count make room for the state. */
  @media (min-width: 768px) and (max-width: 1023px) {
    .length,
    .problems {
      display: none;
    }
  }

  /* Phones: title and state, then the details in one muted line. */
  @media (max-width: 767px) {
    .list thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .list tbody tr {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title state'
        'when when'
        'meta score';
      gap: var(--space-1) var(--space-3);
      padding: var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .list tbody tr:last-child {
      border-bottom: 0;
    }

    .list tbody :is(th, td) {
      height: auto;
      padding: 0;
      border: 0;
    }

    .title {
      grid-area: title;
      min-width: 0;
    }

    .state {
      grid-area: state;
      text-align: right;
    }

    .when {
      grid-area: when;
    }

    .when,
    .length,
    .problems,
    .score {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }

    .length {
      grid-area: meta;
    }

    .problems {
      display: none;
    }

    .score {
      grid-area: score;
      text-align: right;
    }

    .cell-label {
      display: inline;
      margin-right: var(--space-1);
      font-family: var(--font-ui);
    }
  }
</style>
