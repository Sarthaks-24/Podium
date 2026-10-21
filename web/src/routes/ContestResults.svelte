<!--
  ContestResults: the organiser's results for one contest at
  /admin/contests/{id}/results (cache/v1/m4-plan.md sections 3 and 4).
  The board, full or as students saw it frozen
  (GET .../standings?variant=frozen); "Start the reveal" loads both boards
  from GET .../reveal and opens RevealBoard full screen for the projector
  (it opens once every student's time is up; leaving keeps the walk, and
  "Resume the reveal" picks it up again); a held board is released to
  students only after a confirmation, because it cannot be undone;
  links download the results as CSV or JSON; per-problem statistics
  (solved, attempted, median attempts and how many students reached each
  score, as a second line on phones); and the editorial, written in
  Markdown with a preview, saved as a draft or published with
  PUT .../editorial. Students see a short
  explanation instead. A last section links to the integrity check.

  v2 P4: the page uses the working layout. The board, the class view
  (components/students/ClassView: per problem how many tried and reached
  full marks, the median and each subtask's solve rate, totals only), the
  problem statistics and the editorial are the work; beside them from
  1280 px (first below that) the reveal, the downloads (owners and
  admins: both files carry a "zero by decision" column that the board
  never shows) and the integrity check. Each name on the board opens that
  student's progress page (web-quality #16). A board that a review
  decision changed says so in one line, "Board updated on <date> after a
  review decision", without saying whose; no cell carries a mark.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import Markdown from '../components/Markdown.svelte';
  import RevealBoard from '../components/RevealBoard.svelte';
  import StandingsTable from '../components/StandingsTable.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import ClassView from '../components/students/ClassView.svelte';
  import { formatDateTime } from '../lib/format';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { type AdminContest, type ProblemStats } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { boardLabels, boardUpdatedLine, statusLine, type Standings } from '../lib/standings';
  import { ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<AdminContest | null>(null);
  let variant = $state<'full' | 'frozen'>('full');
  let board = $state.raw<Standings | null>(null);
  let boardError = $state('');
  let stats = $state<ProblemStats[] | null>(null);
  let statsError = $state('');

  let revealData = $state.raw<{ frozen: Standings; full: Standings } | null>(null);
  let revealOpen = $state(false);
  let revealError = $state('');
  let revealButton: HTMLButtonElement | undefined = $state();

  let editorial = $state('');
  let editorialPublished = $state(false);
  let editorialBusy = $state(false);
  let editorialError = $state('');
  let preview = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  // The reveal opens once every student's time is up: the close plus the longest personal extension.
  const revealOpensAt = $derived.by(() => {
    if (!contest) return 0;
    const longest = Math.max(0, ...(contest.extensions ?? []).map((x) => x.minutes));
    return (contest.close_at ?? contest.ends_at) + longest * 60_000;
  });
  const revealReady = $derived(ticker.now >= revealOpensAt);
  const labels = $derived(board ? boardLabels(board, (contest?.problems ?? []).map((p) => p.label)) : []);

  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const c = await getJSON<AdminContest>(base);
      const restore = keepHeadingFocus();
      contest = c;
      editorial = c.editorial_md ?? '';
      editorialPublished = c.editorial_published ?? false;
      setTitle(routeTitle('contest-results'), c.title);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
      return;
    }
    void loadBoard();
    void loadStats();
  }

  async function loadBoard() {
    boardError = '';
    try {
      board = await getJSON<Standings>(`${base}/standings${variant === 'frozen' ? '?variant=frozen' : ''}`);
    } catch (e) {
      if (handleAuth(e)) return;
      board = null;
      boardError = errorText(e);
    }
  }

  async function loadStats() {
    try {
      const data = await getJSON<{ problems: ProblemStats[] }>(`${base}/stats`);
      stats = data.problems ?? [];
    } catch (e) {
      if (handleAuth(e)) return;
      statsError = errorText(e);
    }
  }

  function setVariant(v: 'full' | 'frozen') {
    variant = v;
    void loadBoard();
  }

  async function startReveal() {
    revealError = '';
    // Leaving the reveal keeps its place; opening it again resumes.
    if (revealData) {
      revealOpen = true;
      return;
    }
    try {
      revealData = await getJSON<{ frozen: Standings; full: Standings }>(`${base}/reveal`);
      revealOpen = true;
    } catch (e) {
      if (!handleAuth(e)) revealError = errorText(e);
    }
  }

  function closeReveal() {
    revealOpen = false;
    revealButton?.focus();
  }

  async function saveEditorial(published: boolean, done: string) {
    editorialError = '';
    editorialBusy = true;
    try {
      await sendJSON<unknown>('PUT', `${base}/editorial`, { editorial_md: editorial, published });
      editorialPublished = published;
      showToast(done);
    } catch (e) {
      if (handleAuth(e)) return;
      editorialError = e instanceof ApiError ? (e.fields.editorial_md ?? e.message) : errorText(e);
    } finally {
      editorialBusy = false;
    }
  }

  function students(h: ProblemStats['histogram'][number]): number {
    return h.students ?? h.count ?? 0;
  }

  function histogramMax(p: ProblemStats): number {
    return Math.max(1, ...p.histogram.map(students));
  }

  let releaseOpen = $state(false);
  let releaseBusy = $state(false);

  async function release() {
    releaseBusy = true;
    try {
      await sendJSON<unknown>('POST', `${base}/reveal`);
      releaseOpen = false;
      showToast('Released the full board');
      contest = await getJSON<AdminContest>(base);
    } catch (e) {
      releaseOpen = false;
      if (!handleAuth(e)) revealError = errorText(e);
    } finally {
      releaseBusy = false;
    }
  }

  onMount(() => {
    setTitle(routeTitle('contest-results'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    return useTicker();
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Results" />
  <PageState kind="forbidden" message="Only admins can see full results." />
{:else if loadState !== 'ready' || !contest}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Results'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Results' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="results" />

  <div class="page-split results-split">
    <aside class="page-split-aside" aria-label="Reveal, downloads and integrity">
      <section class="panel" aria-labelledby="reveal-panel-title">
        <h2 id="reveal-panel-title">Reveal</h2>
        <p class="muted small">The board on the projector, resolved row by row from the frozen standings.</p>
        <button
          type="button"
          class="btn btn-primary reveal-btn"
          bind:this={revealButton}
          disabled={!revealReady}
          aria-describedby={revealReady ? undefined : 'reveal-help'}
          onclick={startReveal}><Icon name="maximize" /> {revealData ? 'Resume the reveal' : 'Start the reveal'}</button
        >
        {#if !revealReady}<p id="reveal-help" class="muted small">The reveal opens when every student's time is up.</p>{/if}
        {#if revealError}<p class="field-error" role="alert">{revealError}</p>{/if}
        {#if contest.hold_reveal && !contest.revealed_at}
          <div class="hold">
            <p class="small">Students still see the frozen board. Release the full board when the reveal is over.</p>
            <button type="button" class="btn btn-secondary btn-compact" onclick={() => (releaseOpen = true)}>Release the full board</button>
          </div>
        {/if}
      </section>

      <section class="panel" aria-labelledby="downloads-title">
        <h2 id="downloads-title">Downloads</h2>
        <p class="exports">
          <a class="btn btn-secondary" href="{base}/export.csv" download><Icon name="download" /> Download CSV</a>
          <a class="btn btn-secondary" href="{base}/export.json" download><Icon name="download" /> Download JSON</a>
        </p>
        <p class="muted small">Both files have a "zero by decision" column naming any problem a review decision set to zero for a student. The board itself never shows it.</p>
      </section>

      <section class="panel" aria-labelledby="integrity-title">
        <h2 id="integrity-title">Integrity</h2>
        <p class="muted small">Look for copied code, shared wrong outputs, hard-coded answers and probing once the contest has ended for everyone. Signals are reasons to look, not proof.</p>
        <p><a class="btn btn-secondary" href="/admin/contests/{encodeURIComponent(id)}/integrity">Check integrity</a></p>
      </section>
    </aside>

    <div class="page-split-main">
  <section class="section" aria-labelledby="board-title">
    <div class="section-head">
      <h2 id="board-title">Standings</h2>
      <fieldset class="variant">
        <legend class="visually-hidden">Board</legend>
        <label class="choice"><input type="radio" name="variant" checked={variant === 'full'} onchange={() => setVariant('full')} /> Full</label>
        <label class="choice"><input type="radio" name="variant" checked={variant === 'frozen'} onchange={() => setVariant('frozen')} /> As students saw it frozen</label>
      </fieldset>
    </div>
    {#if boardError}
      <div class="banner banner-error" role="alert"><p>{boardError}</p></div>
    {:else if board}
      <p class="muted">{statusLine(board, contest.state === 'ended' || contest.state === 'practice')}</p>
      {#if boardUpdatedLine(board, formatDateTime)}
        <p class="muted small board-updated"><MonoDigits text={boardUpdatedLine(board, formatDateTime)} /></p>
      {/if}
      <StandingsTable
        standings={board}
        {labels}
        caption="Standings for {contest.title}, {variant === 'full' ? 'full' : 'frozen'}"
        nameHref={(uid) => `/admin/users/${uid}/progress`}
      />
    {:else}
      <p class="muted" role="status">Loading the standings</p>
    {/if}
  </section>

  <section class="section" aria-labelledby="class-title">
    <h2 id="class-title">Class view</h2>
    <ClassView contestId={id} />
  </section>

  <section class="section" aria-labelledby="stats-title">
    <h2 id="stats-title">Problems</h2>
    {#if statsError}
      <p class="field-error">{statsError}</p>
    {:else if stats === null}
      <p class="muted" role="status">Loading statistics</p>
    {:else if stats.length === 0}
      <p class="muted">No statistics yet. They appear once students submit.</p>
    {:else}
      <div class="table-wrap">
        <table class="table table-dense table-stack stats">
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
                        <span class="bucket-bar" aria-hidden="true"><span style:width="{Math.round((students(h) / histogramMax(p)) * 100)}%"></span></span>
                        <span class="mono bucket-count" aria-hidden="true">{students(h)}</span>
                        <span class="visually-hidden">{students(h)} {students(h) === 1 ? 'student' : 'students'} scored {h.score}</span>
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

  <section class="section" aria-labelledby="editorial-title">
    <h2 id="editorial-title">Editorial</h2>
    <form
      class="editorial"
      novalidate
      onsubmit={(e) => {
        e.preventDefault();
        void saveEditorial(editorialPublished, editorialPublished ? 'Saved the editorial' : 'Saved the draft');
      }}
    >
      <div class="field">
        <label for="editorial-md">Editorial (Markdown)</label>
        <textarea id="editorial-md" class="input textarea-code" rows="10" bind:value={editorial} aria-invalid={editorialError ? 'true' : undefined} aria-describedby={editorialError ? 'editorial-error' : undefined}></textarea>
        {#if editorialError}<p id="editorial-error" class="field-error">{editorialError}</p>{/if}
      </div>
      <p class="muted small">{editorialPublished ? 'Published: students see it after the contest.' : 'Not published: students do not see it yet.'}</p>
      <div class="actions">
        {#if editorialPublished}
          <button type="submit" class="btn btn-secondary" disabled={editorialBusy}>Save editorial</button>
          <button type="button" class="btn btn-secondary" disabled={editorialBusy} onclick={() => saveEditorial(false, 'Unpublished the editorial')}>Unpublish editorial</button>
        {:else}
          <button type="button" class="btn btn-secondary" disabled={editorialBusy} onclick={() => saveEditorial(true, 'Published the editorial')}>Publish editorial</button>
          <button type="submit" class="btn btn-secondary" disabled={editorialBusy}>Save draft</button>
        {/if}
        <button type="button" class="btn btn-quiet" aria-expanded={preview} aria-controls="editorial-preview" onclick={() => (preview = !preview)}>
          {preview ? 'Hide preview' : 'Show preview'}
        </button>
      </div>
    </form>
    {#if preview}
      <div id="editorial-preview" class="preview">
        {#if editorial.trim() === ''}<p class="muted">Nothing to preview yet.</p>{:else}<Markdown source={editorial} />{/if}
      </div>
    {/if}
  </section>

    </div>
  </div>

  <ConfirmDialog
    open={releaseOpen}
    title="Release the full board?"
    body="Students see the final standings at once. This cannot be undone."
    confirmLabel="Release the full board"
    danger
    busy={releaseBusy}
    onconfirm={release}
    oncancel={() => (releaseOpen = false)}
  />

  {#if revealData}
    <RevealBoard
      open={revealOpen}
      frozen={revealData.frozen}
      full={revealData.full}
      labels={boardLabels(revealData.full, labels)}
      title={contest.title}
      held={!!contest.hold_reveal && !contest.revealed_at}
      onrelease={() => (releaseOpen = true)}
      onclose={closeReveal}
    />
  {/if}
{/if}

<style>
  .results-split {
    margin-top: var(--space-6);
  }

  /* The board needs the room: the work column takes what the aside leaves. */
  @media (min-width: 1280px) {
    .results-split {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
    }
  }

  .panel h2 {
    font-size: var(--fs-sub);
  }

  .panel {
    justify-items: start;
  }

  .section {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .section + .section {
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

  .small {
    font-size: var(--fs-0);
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

  .hold {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    padding: var(--space-3);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
  }

  .exports {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
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

  .editorial {
    display: grid;
    gap: var(--space-3);
    max-width: 56rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .preview {
    max-width: 56rem;
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }
</style>
