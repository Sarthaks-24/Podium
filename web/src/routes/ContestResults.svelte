<!--
  ContestResults: the organiser's results for one contest at
  /admin/contests/{id}/results. Under the contest's tabs, one line says
  where the reveal stands, and only for a contest that keeps hidden
  results hidden after the end: before every student's time is up it says
  when the reveal opens and offers no button; then "Reveal" shows students
  the full board (POST .../reveal) after one confirmation, because it
  cannot be undone; after it the line says when it happened.
  Beside it sit "Similar code" (once the contest has ended; it opens the
  integrity page), "Download CSV" and "Download JSON", and a download
  leaves a line naming the file. Below, across the page width
  (the walk measured the board at 560 px of 1440): the standings, full or
  as students saw them (GET .../standings?variant=frozen) when results
  were hidden, with one line saying which board this is
  (lib/standings organiserBoardLine), then the one statistics table:
  solved, attempted, median attempts and how many students reached each
  score, per problem. The animated reveal walk and the editorial are not
  offered. A student an integrity decision excluded is not on the
  board or in the downloads. Students see a short explanation instead.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import StandingsTable from '../components/StandingsTable.svelte';
  import { errorText, getJSON, isSignedOut, sendJSON } from '../lib/api';
  import { type AdminContest, type ProblemStats } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDayTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { boardLabels, organiserBoardLine, type Standings } from '../lib/standings';
  import { ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  let contest = $state.raw<AdminContest | null>(null);
  let variant = $state<'full' | 'frozen'>('full');
  let board = $state.raw<Standings | null>(null);
  let boardError = $state('');
  let stats = $state<ProblemStats[] | null>(null);
  let statsError = $state('');
  let revealError = $state('');
  let downloadLine = $state('');
  // Taken as the data arrives, just before the loading view gives way, so the heading keeps focus.
  let restoreFocus: () => Promise<void> = async () => undefined;

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const page = loader(
    () => getJSON<AdminContest>(base),
    (c) => {
      restoreFocus = keepHeadingFocus();
      contest = c;
      setTitle(routeTitle('contest-results'), c.title);
    },
  );
  // The reveal opens once every student's time is up: the close plus the longest personal extension.
  const revealOpensAt = $derived.by(() => {
    if (!contest) return 0;
    const longest = Math.max(0, ...(contest.extensions ?? []).map((x) => x.minutes));
    return (contest.close_at ?? contest.ends_at) + longest * 60_000;
  });
  const revealReady = $derived(ticker.now >= revealOpensAt);
  const ended = $derived(contest?.state === 'ended' || contest?.state === 'practice');
  // When the board stopped updating for students, and whether they still see it that way.
  const hiddenFrom = $derived(contest && contest.freeze_minutes > 0 ? (contest.close_at ?? contest.ends_at) - contest.freeze_minutes * 60_000 : null);
  const hiding = $derived(
    contest !== null && hiddenFrom !== null && ticker.now >= hiddenFrom && (ticker.now < revealOpensAt || (contest.hold_reveal === true && !contest.revealed_at)),
  );
  // There is something to reveal only when results are hidden and kept hidden after the end.
  const held = $derived(!!contest?.hold_reveal && hiddenFrom !== null);
  const offerReveal = $derived(held && !contest?.revealed_at && revealReady);
  /** The reveal line follows the state: before it opens, when it is open, after it. */
  const revealLine = $derived.by(() => {
    if (!contest || !held || hiddenFrom === null) return '';
    if (contest.revealed_at) return `Revealed ${formatDayTime(contest.revealed_at)}. Students see the full board.`;
    if (!revealReady) {
      return `Results from ${clock(hiddenFrom)} stay hidden from students until you reveal them. The reveal opens at ${clock(revealOpensAt)}, when every student's time is up.`;
    }
    return `Students see the board without the results from ${clock(hiddenFrom)}. Reveal shows them the full board.`;
  });
  const labels = $derived(board ? boardLabels(board, (contest?.problems ?? []).map((p) => p.label)) : []);
  const slug = $derived(contest?.slug ?? 'contest');
  // The browser's zone, so the times in the file read as they do here.
  const tz = (() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
    } catch {
      return '';
    }
  })();

  /** "14:32" in local time. */
  function clock(ms: number): string {
    const d = new Date(ms);
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  }

  async function load() {
    await page.load();
    void restoreFocus();
    if (page.state === 'missing') setTitle('Contest not found');
    if (page.state !== 'ready') return;
    void loadBoard();
    void loadStats();
  }

  async function loadBoard() {
    boardError = '';
    try {
      const b = await getJSON<Standings>(`${base}/standings${variant === 'frozen' ? '?variant=frozen' : ''}`);
      if (page.live) board = b;
    } catch (e) {
      if (isSignedOut(e) || !page.live) return;
      board = null;
      boardError = errorText(e);
    }
  }

  async function loadStats() {
    try {
      const data = await getJSON<{ problems: ProblemStats[] }>(`${base}/stats`);
      if (page.live) stats = data.problems ?? [];
    } catch (e) {
      if (!isSignedOut(e) && page.live) statsError = errorText(e);
    }
  }

  function setVariant(v: 'full' | 'frozen') {
    variant = v;
    void loadBoard();
  }

  function histogramMax(p: ProblemStats): number {
    return Math.max(1, ...p.histogram.map((h) => h.students));
  }

  let revealOpen = $state(false);
  let revealBusy = $state(false);

  /** Shows students the full board, and shows it here too. */
  async function reveal() {
    revealBusy = true;
    revealError = '';
    try {
      await sendJSON<unknown>('POST', `${base}/reveal`);
      revealOpen = false;
      showToast('Revealed the full board');
      const c = await getJSON<AdminContest>(base);
      if (!page.live) return;
      contest = c;
      setVariant('full');
    } catch (e) {
      revealOpen = false;
      if (!isSignedOut(e) && page.live) revealError = errorText(e);
    } finally {
      revealBusy = false;
    }
  }

  onMount(() => {
    setTitle(routeTitle('contest-results'));
    if (!isOrganiser) page.forbid();
    else void load();
    const stop = useTicker();
    return () => {
      page.stop();
      stop();
    };
  });
</script>

{#if page.state === 'forbidden'}
  <PageHeader title="Results" />
  <PageState kind="forbidden" message="Only organisers can see full results." />
{:else if page.state !== 'ready' || !contest}
  <PageHeader
    title={page.state === 'missing' ? 'Contest not found' : 'Results'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: page.state === 'missing' ? 'Contest not found' : 'Results' }]}
  />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={page.error} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="results" />

  <div class="bar">
    {#if revealLine}<p class="reveal-line">{revealLine}</p>{/if}
    <div class="results-actions">
      {#if offerReveal}
        <button type="button" class="btn btn-primary" onclick={() => (revealOpen = true)}>Reveal</button>
      {/if}
      {#if ended}
        <a class="btn btn-secondary" href="/admin/contests/{encodeURIComponent(id)}/integrity">Similar code</a>
      {/if}
      <a
        class="btn btn-secondary"
        href="{base}/export.csv{tz ? `?tz=${encodeURIComponent(tz)}` : ''}"
        download="{slug}-results.csv"
        onclick={() => (downloadLine = `Downloading ${slug}-results.csv to your browser's downloads.`)}><Icon name="download" /> Download CSV</a
      >
      <a
        class="btn btn-secondary"
        href="{base}/export.json"
        download="{slug}-results.json"
        onclick={() => (downloadLine = `Downloading ${slug}-results.json to your browser's downloads.`)}><Icon name="download" /> Download JSON</a
      >
    </div>
    <p class="muted small download-line" role="status">{downloadLine}</p>
    {#if revealError}<p class="field-error" role="alert">{revealError}</p>{/if}
  </div>

  <section class="section" aria-labelledby="board-title">
    <div class="section-head">
      <h2 id="board-title">Standings</h2>
      {#if hiddenFrom !== null}
        <fieldset class="variant">
          <legend class="visually-hidden">Board</legend>
          <label class="choice"><input type="radio" name="variant" checked={variant === 'full'} onchange={() => setVariant('full')} /> Full</label>
          <label class="choice"
            ><input type="radio" name="variant" checked={variant === 'frozen'} onchange={() => setVariant('frozen')} /> As students {hiding ? 'see' : 'saw'} it</label
          >
        </fieldset>
      {/if}
    </div>
    {#if boardError}
      <div class="banner banner-error" role="alert"><p>{boardError}</p></div>
    {:else if board}
      <p class="muted">{organiserBoardLine(board, { ended, hiddenFrom, hiding })}</p>
      <StandingsTable
        standings={board}
        {labels}
        wide
        caption="Standings for {contest.title}, {variant === 'full' ? 'full board' : 'as students saw it'}"
      />
    {:else}
      <p class="muted" role="status">Loading the standings</p>
    {/if}
  </section>

  <section class="section" aria-labelledby="stats-title">
    <h2 id="stats-title">Statistics</h2>
    {#if statsError}
      <p class="field-error">{statsError}</p>
    {:else if stats === null}
      <p class="muted" role="status">Loading statistics</p>
    {:else if stats.length === 0}
      <p class="muted">No statistics yet. They appear once students submit.</p>
    {:else}
      <div class="table-wrap">
        <table class="table table-dense table-stack stats">
          <caption class="visually-hidden">Statistics for each problem</caption>
          <thead>
            <tr>
              <th scope="col">Problem</th>
              <th scope="col" class="right">Solved</th>
              <th scope="col" class="right">Attempted</th>
              <th scope="col" class="right">Median attempts</th>
              <th scope="col">Students by score</th>
            </tr>
          </thead>
          <tbody>
            {#each stats as p (p.label)}
              <tr>
                <th scope="row" class="pname"><span class="mono letter">{p.label}</span>{p.title ?? ''}</th>
                <td class="num right solved"><span class="cell-label">Solved</span>{p.solved}</td>
                <td class="num right tried"><span class="cell-label">Attempted</span>{p.attempted ?? ''}</td>
                <td class="num right median"><span class="cell-label">Median attempts</span>{p.median_attempts ?? '-'}</td>
                <td class="hist">
                  <ul class="buckets">
                    {#each p.histogram as h (h.score)}
                      <li>
                        <span class="mono bucket-score" aria-hidden="true">{h.score}</span>
                        <span class="bucket-bar" aria-hidden="true"><span style:width="{Math.round((h.students / histogramMax(p)) * 100)}%"></span></span>
                        <span class="mono bucket-count" aria-hidden="true">{h.students}</span>
                        <span class="visually-hidden">{h.students} {h.students === 1 ? 'student' : 'students'} scored {h.score}</span>
                      </li>
                    {/each}
                  </ul>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>

  <ConfirmDialog
    open={revealOpen}
    title="Reveal the full board?"
    body="Students see the final standings at once. This cannot be undone."
    confirmLabel="Reveal"
    danger
    busy={revealBusy}
    onconfirm={reveal}
    oncancel={() => (revealOpen = false)}
  />
{/if}

<style>
  .bar {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-5);
    justify-items: start;
  }

  .reveal-line {
    max-width: var(--measure-wide);
  }

  .results-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  /* Out of the flow while empty, but still rendered so it announces. */
  .download-line:empty {
    position: absolute;
  }

  .section {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
    margin-top: var(--space-6);
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .section-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3) var(--space-5);
  }

  .variant {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-4);
    padding: 0;
    border: 0;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .cell-label {
    display: none;
  }

  /* Phones: each problem is its name, then the counts, then the histogram. */
  @media (width < 768px) {
    .stats tbody tr {
      grid-template-columns: repeat(3, minmax(0, auto));
      grid-template-areas:
        'pname pname pname'
        'solved tried median'
        'hist hist hist';
      justify-content: start;
      gap: var(--space-2) var(--space-4);
    }

    .stats tbody :is(th, td) {
      min-width: 0;
      text-align: left;
      white-space: normal;
      overflow-wrap: anywhere;
    }

    .stats .pname {
      grid-area: pname;
    }

    .stats .solved {
      grid-area: solved;
    }

    .stats .tried {
      grid-area: tried;
    }

    .stats .median {
      grid-area: median;
    }

    .stats .hist {
      grid-area: hist;
      min-width: 0;
    }

    .cell-label {
      display: inline;
      margin-right: var(--space-1);
      color: var(--ink-muted);
      font-family: var(--font-ui);
    }
  }

  .letter {
    margin-right: var(--space-3);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .hist {
    min-width: 14rem;
  }

  .buckets {
    display: grid;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .buckets li {
    display: grid;
    grid-template-columns: 3rem minmax(4rem, 1fr) 2.5rem;
    align-items: center;
    gap: var(--space-2);
  }

  .bucket-score,
  .bucket-count {
    text-align: right;
  }

  .bucket-bar {
    display: block;
    height: var(--space-2);
    background: var(--surface-2);
  }

  .bucket-bar span {
    display: block;
    height: 100%;
    background: var(--act);
  }
</style>
