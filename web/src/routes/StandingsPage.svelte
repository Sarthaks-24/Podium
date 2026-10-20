<!--
  StandingsPage: a contest's board for students, at
  /contests/{slug}/standings (design.md 5.4, cache/v1/m4-plan.md section 3).
  It loads the contest (for its title and problem letters) and
  GET /api/contests/{slug}/standings, says whether the board is live,
  frozen ("Frozen at 20:30. Updates when the contest ends.", or after the
  end, held for the reveal, "The final standings appear when the organisers
  release them.") or final and how many submissions are still judging, with
  one legend line under the board and a "How to read the board"
  disclosure for "+2" and the time under each score (audit 01-11 and
  2026-10-20 S8), the student's own place as the page's one display figure
  ("7 of 24" at --fs-6, D80), and StandingsTable with the student's own row
  tinted and "Jump to you". When a refetch moves the student's own place or
  score, the announcer says so as a sentence (S6, X1). A window-mode contest answers
  403 until the student's window closes; the page then says "The board
  opens when the window closes." The board refetches when the event stream
  says its version changed, at most once every 3 s. Props: `slug`.
-->
<script lang="ts">
  import PlaceFigure from '../components/PlaceFigure.svelte';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onDestroy, onMount } from 'svelte';
  import StandingsTable from '../components/StandingsTable.svelte';
  import StreamNotice from '../components/StreamNotice.svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import { isForContest, type Contest } from '../lib/contests';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { polite } from '../lib/announce.svelte';
  import { boardLabels, placeFigure, rankAnnouncement, statusLine, type Standings } from '../lib/standings';
  import { EventStream, type StreamMode } from '../lib/stream';

  let { slug }: { slug: string } = $props();

  const CLOSED = 'The standings open when the contest closes.';

  let loadState = $state<'loading' | 'ready' | 'closed' | 'forbidden' | 'missing' | 'error'>('loading');
  let message = $state('');
  let contest = $state.raw<Contest | null>(null);
  let standings = $state.raw<Standings | null>(null);
  let streamMode = $state<StreamMode>('connecting');

  const api = $derived(`/api/contests/${encodeURIComponent(slug)}`);
  const ended = $derived(contest?.state === 'ended' || contest?.state === 'practice');
  const labels = $derived(standings ? boardLabels(standings, (contest?.problems ?? []).map((p) => p.label)) : []);
  const place = $derived(standings ? placeFigure(standings) : null);

  async function loadContest() {
    try {
      const sent = Date.now();
      const c = await getJSON<Contest>(api);
      serverClock.observe(c.server_now, sent, Date.now());
      contest = c;
      setTitle(routeTitle('standings'), c.title);
    } catch {
      // The board says what went wrong.
    }
  }

  async function loadBoard(quiet = false) {
    if (!quiet) loadState = 'loading';
    try {
      const s = await getJSON<Standings>(`${api}/standings`);
      if (standings && s.version < standings.version && s.variant === standings.variant) return;
      const said = rankAnnouncement(standings, s);
      if (said !== '') polite(said);
      standings = s;
      loadState = 'ready';
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      if (quiet && loadState === 'ready') return;
      if (e instanceof ApiError && e.status === 403) {
        const closed = e.code === 'board_closed' || e.message === CLOSED || /window closes|contest closes/i.test(e.message);
        loadState = closed ? 'closed' : 'forbidden';
        message = closed ? CLOSED : e.message;
      } else if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
      } else {
        loadState = 'error';
        message = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      }
    }
  }

  // Version events from everyone's submissions arrive often; refetch at most every 3 s.
  let lastFetch = 0;
  let pending: ReturnType<typeof setTimeout> | undefined;
  function refetchThrottled() {
    if (pending) return;
    const wait = Math.max(0, lastFetch + 3000 - Date.now());
    pending = setTimeout(() => {
      pending = undefined;
      lastFetch = Date.now();
      void loadBoard(true);
    }, wait);
  }

  function onEvent(type: string, data: unknown) {
    if (!isForContest(data, contest ?? { slug })) return;
    if (type === 'standings') {
      const v = (data as { version?: unknown }).version;
      if (typeof v === 'number' && standings && v <= standings.version) return;
      refetchThrottled();
    } else if (type === 'contest') {
      void loadContest();
      refetchThrottled();
    }
  }

  let stream: EventStream | null = null;

  onMount(() => {
    setTitle(routeTitle('standings'));
    void loadContest();
    void loadBoard();
    stream = new EventStream({
      url: '/api/events',
      events: ['standings', 'contest'],
      onEvent,
      onReconnect: () => void loadBoard(true),
      poll: () => void loadBoard(true),
      onMode: (m) => (streamMode = m),
    });
    stream.start();
  });

  onDestroy(() => {
    stream?.stop();
    clearTimeout(pending);
  });
</script>

<PageHeader title="Standings" back={{ href: `/contests/${encodeURIComponent(slug)}`, label: contest?.title ?? 'Contest' }} />

{#if loadState === 'loading'}
  <PageState kind="loading" message="Loading the standings" />
{:else if loadState === 'closed'}
  <p class="lead">{CLOSED}</p>
  <p class="muted small">Everyone starts at a different time in this contest, so the standings stay hidden until everyone has had their time.</p>
{:else if loadState === 'forbidden'}
  <p class="lead">{message}</p>
{:else if loadState === 'missing'}
  <PageState kind="missing" message="There is no contest at this address." parent={{ href: '/', label: 'Go to your contests.' }} />
{:else if loadState === 'error'}
  <PageState kind="error" message={message} onretry={() => loadBoard()} />
{:else if standings}
  <p class="status" class:frozen={standings.variant === 'frozen'}>{statusLine(standings, ended)}</p>
  {#if place}
    <div class="place-wrap"><PlaceFigure {place} /></div>
  {/if}
  <StreamNotice mode={streamMode} onresume={() => stream?.resume()} />
  <div class="board">
    <StandingsTable {standings} {labels} caption="Standings for {contest?.title ?? 'this contest'}" />
  </div>
  <!-- S8: one line under the board, the rest behind a disclosure. -->
  <p class="legend">The bar under a score is the share of points earned. A dash means not tried.</p>
  <details class="how-to">
    <summary>How to read the board</summary>
    <ul>
      <li>Each problem cell is that student's score on the problem, with a bar for the share of its points.</li>
      <li>A number after a score, such as +2, counts submissions made after the freeze. They are shown when the contest ends.</li>
      <li>The time under each total is when it was reached, counted from the start. Equal scores are ordered by that time.</li>
    </ul>
  </details>
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .small {
    margin-top: var(--space-2);
    max-width: 40rem;
    font-size: var(--fs-0);
  }

  .status {
    margin-block: var(--space-3) var(--space-4);
    color: var(--ink-muted);
  }

  .status.frozen {
    color: var(--ink);
  }

  .legend {
    max-width: var(--measure-prose);
    margin-top: var(--space-3);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .how-to {
    max-width: var(--measure-prose);
    margin-top: var(--space-2);
    font-size: var(--fs-0);
  }

  .how-to summary {
    width: fit-content;
    min-height: var(--control-h);
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    line-height: var(--control-h);
    cursor: pointer;
  }

  .how-to ul {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
    color: var(--ink-muted);
  }

  .place-wrap {
    margin-bottom: var(--space-6);
  }

  .board {
    margin-top: var(--space-2);
  }
</style>
