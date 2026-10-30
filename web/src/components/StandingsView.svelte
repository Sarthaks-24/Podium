<!--
  StandingsView: a contest's board for a student, shared by
  the standings page and the panel that opens over the editor during the
  contest. It loads the contest (for its problem letters) and
  GET /api/contests/{slug}/standings and says in one line whether the board
  is live, hiding its newest results ("Results from 13:51 are hidden until
  the end.", never "freeze", design/words.md) or final, and how many
  submissions are still judging. It always asks for the board students
  get (?as=student), so an organiser in Student view sees that board,
  under the line "Student view: the board as students see it now." Then the student's own place as the one
  display figure, StandingsTable with their row tinted and "Jump to you",
  and one legend line, the rest behind "How to read the board". When a
  refetch moves the student's own place or score, the announcer says so.
 A refusal (403) shows the server's sentence. The board refetches when
  the event stream says its version changed, at most once every 3 s.
  Props: `slug` and `oncontest` (given the contest once it loads).
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { polite } from '../lib/announce.svelte';
  import { getJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import { isForContest, type Contest } from '../lib/contests';
  import { loader } from '../lib/loader.svelte';
  import { boardLabels, placeFigure, rankAnnouncement, statusLine, type Standings } from '../lib/standings';
  import { LiveSubscription, type StreamMode } from '../lib/stream';
  import PageState from './PageState.svelte';
  import PlaceFigure from './PlaceFigure.svelte';
  import StaleNote from './StaleNote.svelte';
  import StandingsTable from './StandingsTable.svelte';
  import StreamNotice from './StreamNotice.svelte';

  let { slug, oncontest }: { slug: string; oncontest?: (c: Contest) => void } = $props();

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
      if (!page.live) return;
      serverClock.observe(c.server_now, sent, Date.now());
      contest = c;
      oncontest?.(c);
    } catch {
      // The board says what went wrong.
    }
  }

  // as=student: an organiser here is in Student view and gets the board students get.
  const page = loader(
    () => getJSON<Standings>(`${api}/standings?as=student`),
    (s) => {
      // An older board that arrives late never replaces a newer one.
      if (standings && s.version < standings.version && s.variant === standings.variant) return;
      const said = rankAnnouncement(standings, s);
      if (said !== '') polite(said);
      standings = s;
    },
    () => `${api}/standings?as=student`,
  );
  const loadBoard = (quiet = false) => page.load({ quiet });

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

  let stream: LiveSubscription | null = null;

  onMount(() => {
    void loadContest();
    void loadBoard();
    stream = new LiveSubscription({
      events: ['standings', 'contest'],
      onEvent,
      onReconnect: () => void loadBoard(true),
      poll: () => void loadBoard(true),
      onMode: (m) => (streamMode = m),
    });
    stream.start();
  });

  onDestroy(() => {
    page.stop();
    stream?.stop();
    clearTimeout(pending);
  });
</script>

<StaleNote {page} />
{#if page.state === 'loading'}
  <PageState kind="loading" message="Loading the standings" shape="rows" />
{:else if page.state === 'forbidden'}
  <p class="lead">{page.error}</p>
{:else if page.state === 'missing'}
  <PageState kind="missing" message="There is no contest at this address." parent={{ href: '/', label: 'Go to your contests.' }} />
{:else if page.state === 'error'}
  <PageState kind="error" message={page.error} onretry={() => loadBoard()} />
{:else if standings}
  {#if contest?.organiser}<p class="student-view small">Student view: the board as students see it now.</p>{/if}
  <p class="status" class:hiding={standings.variant === 'frozen'}>{statusLine(standings, ended)}</p>
  {#if place}
    <div class="place-wrap"><PlaceFigure {place} /></div>
  {/if}
  <StreamNotice mode={streamMode} onresume={() => stream?.resume()} />
  <div class="board">
    <StandingsTable {standings} {labels} caption="Standings for {contest?.title ?? 'this contest'}" />
  </div>
  <!-- One line under the board, the rest behind a disclosure, so the board comes first. -->
  <p class="legend">
    The bar under a score is the share of points earned; a dash means not tried. Time is when the total was reached, from the start.
  </p>
  <details class="how-to">
    <summary>How to read the board</summary>
    <ul>
      <li>Each problem cell is that student's score on the problem, with a bar for the share of its points.</li>
      <li>A number after a score, such as +2, counts submissions made while the newest results are hidden. They are shown at the end.</li>
      <li>Equal scores are ordered by the time the total was reached.</li>
    </ul>
  </details>
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-narrow);
  }

  .student-view {
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .status {
    margin-block: var(--space-3) var(--space-4);
    color: var(--ink-muted);
  }

  .status.hiding {
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
