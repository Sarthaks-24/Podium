<!--
  Home: "Your contests", the student's landing page
  (organisers land on /admin/contests instead). One list from
  GET /api/contests, not cards: live contests first with their countdown,
  then upcoming ones soonest first, then past ones newest first. Each row
  gives the title (a link to the contest; on phones the whole row is the
  link), when it runs, how long it lasts, the problem count, the student's
  score once they have one ("240 of 300", digits in mono) and the state
  word. The first contest (live, else the next, else the latest one the
  student took part in, never one they missed) is pinned above the
  table in a panel with its countdown as mono digits and
  one primary button, "Open contest" in every state, so the
  student never has to choose; the table lists the rest under "Other
  contests" ("Contests" when nothing is pinned). A live row in the table adds an "Open contest" button. A
  frozen contest reads "Live" with "standings frozen" in its countdown.
 Under 768 px each row reads as title and state, then the start,
  then the length and problem count in one muted line beside the score. A
  countdown over a day reads as a span ("Starts in 2 d 23 h"). The app
  shell sends signed-out visitors to /signin first. Takes no props.
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import { getJSON } from '../lib/api';
  import { loader } from '../lib/loader.svelte';
  import { serverClock } from '../lib/clock';
  import {
    formatCountdown,
    formatSpan,
    lengthText,
    listCountdown,
    orderedContests,
    pinnedContest,
    studentState,
    studentStateWord,
    type ContestList,
    type ContestSummary,
  } from '../lib/contests';
  import { countOf, formatDateTime, numberParts, scoreParts } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';

  let contests = $state<ContestSummary[]>([]);

  const page = loader(
    async () => {
      const sent = Date.now();
      const data = await getJSON<ContestList>('/api/contests');
      if (typeof data.server_now === 'number') serverClock.observe(data.server_now, sent, Date.now());
      return data;
    },
    (data) => {
      refreshTicker();
      contests = orderedContests(data);
    },
  );
  const load = () => page.load();

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
    if (page.state === 'ready' && ticker.now >= reloadAt) void load();
  });

  function isLive(c: ContestSummary): boolean {
    return c.state === 'live' || c.state === 'frozen';
  }

  onMount(() => {
    setTitle(routeTitle('home'));
    void load();
    const stopTicker = useTicker();
    return () => {
      page.stop();
      stopTicker();
    };
  });
</script>

<PageHeader title="Your contests" />

{#if page.state === 'loading'}
  <PageState kind="loading" message="Loading contests" shape="rows" />
{:else if page.state !== 'ready'}
  <PageState kind="error" message={page.error} onretry={load} />
{:else if contests.length === 0}
  <PageState kind="empty" message="No contests yet. When an organiser schedules one, it appears here." />
{:else}
  {@const pin = pinnedContest(contests)}
  {@const others = contests.filter((c) => c !== pin)}
  <!-- The live or next contest on top, with its countdown and one button; never one the student missed. -->
  {#if pin}
  {@const pcd = listCountdown(pin, ticker.now)}
  {@const pinHref = `/contests/${encodeURIComponent(pin.slug)}`}
  <section class="panel pinned" aria-labelledby="pinned-title">
    <div class="pinned-head">
      <h2 id="pinned-title"><a href={pinHref} id="contest-{pin.slug}">{pin.title}</a></h2>
      <span class="state-word" data-state={studentState(pin.state)}>{studentStateWord(pin.state)}</span>
    </div>
    {#if pcd && pcd.ms > 0}
      <p class="pinned-count">
        <span class="count-lead">{pcd.lead}</span>
        <span class="mono figure">{pcd.ms >= 86_400_000 ? formatSpan(pcd.ms) : formatCountdown(pcd.ms)}</span>{#if pcd.after}<span class="count-lead">{pcd.after}</span>{/if}
      </p>
    {/if}
    <p class="muted">
      <MonoDigits text={formatDateTime(pin.starts_at)} />, {lengthText(pin)}, {countOf(pin.problem_count, 'problem')}{#if pin.my_score !== null}. Your score <MonoDigits parts={scoreParts(pin.my_score, pin.max_score)} />{/if}.
    </p>
    <div class="action-row">
      <a class="btn btn-primary" href={pinHref} aria-describedby="pinned-title">Open contest</a>
    </div>
  </section>
  {/if}

  {#if others.length > 0}
  <h2 class="others">{pin ? 'Other contests' : 'Contests'}</h2>
  <div class="table-wrap list">
    <table class="table table-stack">
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
        {#each others as c (c.slug)}
          {@const cd = listCountdown(c, ticker.now)}
          {@const href = `/contests/${encodeURIComponent(c.slug)}`}
          <tr class:live={isLive(c)}>
            <th scope="row" class="title">
              <a class="row-link" id="contest-{c.slug}" {href}>{c.title}</a>
              {#if cd && cd.ms > 0}
                <span class="countdown">{cd.lead} <MonoDigits parts={numberParts(cd.ms >= 86_400_000 ? formatSpan(cd.ms) : formatCountdown(cd.ms))} />{cd.after}</span>
              {/if}
              {#if isLive(c)}
                <a class="btn btn-secondary btn-compact open" {href} aria-describedby="contest-{c.slug}">Open contest</a>
              {/if}
            </th>
            <td class="when"><MonoDigits text={formatDateTime(c.starts_at)} /></td>
            <td class="length"><span class="meta">{lengthText(c)}<span class="phone-only">, {countOf(c.problem_count, 'problem')}</span></span></td>
            <td class="num right problems">{c.problem_count}</td>
            <td class="right score">
              {#if c.my_score !== null}<span class="cell-label">Score </span><MonoDigits parts={scoreParts(c.my_score, c.max_score)} />{/if}
            </td>
            <td class="state"><span class="state-word" data-state={studentState(c.state)}>{studentStateWord(c.state)}</span></td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  {/if}
{/if}

<style>
  .pinned {
    max-width: var(--measure-form);
    margin-top: var(--space-5);
  }

  .pinned-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2) var(--space-3);
  }

  .pinned-head a {
    color: var(--ink);
  }

  .pinned-count {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 var(--space-2);
  }

  .count-lead {
    color: var(--ink-muted);
  }

  .pinned-count .figure {
    font-size: var(--fs-4);
    line-height: var(--lh-tight);
  }

  .others {
    margin-top: var(--section-gap);
  }

  .list {
    margin-top: var(--space-4);
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

  .open {
    margin-top: var(--space-2);
  }

  .length {
    white-space: nowrap;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .score {
    white-space: nowrap;
  }

  .cell-label,
  .phone-only {
    display: none;
  }

  .state {
    white-space: nowrap;
  }

  /* Phones: title and state, then the start, then length, problems and score. */
  @media (width < 768px) {
    .list tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title state'
        'when when'
        'meta score';
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
    .score {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }

    .length {
      grid-area: meta;
    }

    .phone-only {
      display: inline;
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
    }
  }
</style>
