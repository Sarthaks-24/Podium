<!--
  ContestIntegrity: the organiser's integrity check for one contest at
  /admin/contests/{id}/integrity (cache/v1/integrity-plan.md sections 5
  and 7, docs/INTEGRITY.md). "Check integrity" starts a check
  (POST .../integrity/run); it is disabled with the reason until every
  student's time is up, and while a check runs the page says so and polls
  GET .../integrity every two seconds until it is done. Under the stats
  line come the signals, filtered by kind and strength and grouped by
  problem in tables (strength in words with the number beside it), with
  the groups of similar code on each problem listed as names. Signals are
  chosen with checkboxes to open a case (OpenCaseDialog); the contest's
  cases are listed below, newest first. Window-mode contests get a note
  that they leak by design. Every view carries IntegrityNote. Props: `id`.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import OpenCaseDialog from '../components/OpenCaseDialog.svelte';
  import SignalTable from '../components/SignalTable.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { type AdminContest } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    KINDS,
    STRENGTHS,
    caseStateWord,
    clusters,
    filterSignals,
    groupByProblem,
    joinWords,
    problemName,
    statsLine,
    userName,
    type Case,
    type Run,
  } from '../lib/review';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  /** How often a running check is polled, in ms. */
  const POLL_MS = 2000;

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<AdminContest | null>(null);
  let run = $state.raw<Run | null>(null);
  let runState = $state<'loading' | 'none' | 'ready' | 'error'>('loading');
  let runError = $state('');
  let startError = $state('');
  let starting = $state(false);
  let cases = $state.raw<Case[] | null>(null);
  let casesError = $state('');

  let kind = $state('');
  let strength = $state('');
  const selected = new SvelteSet<number>();
  let dialogOpen = $state(false);

  let pollTimer: ReturnType<typeof setTimeout> | null = null;
  let destroyed = false;

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  // A check may run once every student's time is up: the close plus the longest personal extension.
  const closesAt = $derived.by(() => {
    if (!contest) return 0;
    const longest = Math.max(0, ...(contest.extensions ?? []).map((x) => x.minutes));
    return (contest.close_at ?? contest.ends_at) + longest * 60_000;
  });
  const over = $derived(!!contest && (contest.state === 'ended' || contest.state === 'practice') && ticker.now >= closesAt);
  const running = $derived(starting || run?.state === 'running');
  const shown = $derived(run && run.state === 'done' ? filterSignals(run.signals ?? [], kind, strength) : []);
  const groups = $derived(groupByProblem(shown, run?.problems));
  const chosen = $derived((run?.signals ?? []).filter((s) => selected.has(s.id)));

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

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  async function load() {
    loadState = 'loading';
    try {
      const c = await getJSON<AdminContest>(base);
      const restore = keepHeadingFocus();
      contest = c;
      setTitle(routeTitle('contest-integrity'), c.title);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
      return;
    }
    void loadRun();
    void loadCases();
  }

  async function loadRun(): Promise<Run | null> {
    try {
      const r = await getJSON<Run>(`${base}/integrity`);
      run = r;
      runState = 'ready';
      // Choices from an earlier check no longer exist.
      const ids = new Set((r.signals ?? []).map((s) => s.id));
      for (const s of [...selected]) if (!ids.has(s)) selected.delete(s);
      if (r.state === 'running') schedulePoll();
      return r;
    } catch (e) {
      if (handleAuth(e)) return null;
      // A refused poll while a check runs just waits for the next one.
      if (e instanceof ApiError && e.status === 429 && run?.state === 'running') {
        schedulePoll();
        return run;
      }
      if (e instanceof ApiError && e.status === 404) {
        run = null;
        runState = 'none';
        return null;
      }
      runError = messageOf(e);
      runState = 'error';
      return null;
    }
  }

  async function loadCases() {
    casesError = '';
    try {
      const data = await getJSON<{ cases: Case[] }>(`${base}/integrity/cases`);
      cases = data.cases ?? [];
    } catch (e) {
      if (handleAuth(e)) return;
      casesError = messageOf(e);
    }
  }

  function schedulePoll() {
    if (pollTimer !== null || destroyed) return;
    pollTimer = setTimeout(async () => {
      pollTimer = null;
      const r = await loadRun();
      if (r && r.state === 'done') showToast('The check finished');
    }, POLL_MS);
  }

  async function startCheck() {
    if (running) return;
    startError = '';
    starting = true;
    try {
      await sendJSON<{ run_id: number }>('POST', `${base}/integrity/run`);
      selected.clear();
      await loadRun();
      schedulePoll();
    } catch (e) {
      if (!handleAuth(e)) startError = messageOf(e);
    } finally {
      starting = false;
    }
  }

  function toggle(signalId: number, on: boolean) {
    if (on) selected.add(signalId);
    else selected.delete(signalId);
  }

  function created(c: Case) {
    dialogOpen = false;
    showToast('Opened the case');
    navigate(`/admin/integrity/cases/${c.id}`);
  }

  function caseStudents(c: Case): string {
    return joinWords((c.users ?? []).map((u) => u.name));
  }

  onMount(() => {
    setTitle(routeTitle('contest-integrity'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    const stop = useTicker();
    return () => {
      destroyed = true;
      if (pollTimer !== null) clearTimeout(pollTimer);
      stop();
    };
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Integrity" />
  <PageState kind="forbidden" message="Only owners and admins can review integrity." />
{:else if loadState !== 'ready' || !contest}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Integrity'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Integrity' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} current="integrity" />

  <div class="section">
    <IntegrityNote />
    {#if contest.start_mode === 'window'}
      <p class="muted small window">
        This contest let students start at different times, so it leaks by design: a student who finished can tell a later one about the problems. Weigh signals between early and late starters with that in mind.
      </p>
    {/if}
    <div class="run">
      <button
        type="button"
        class="btn btn-primary"
        disabled={!over}
        aria-disabled={running ? 'true' : undefined}
        aria-describedby={!over ? 'check-help' : undefined}
        onclick={startCheck}>{running ? 'Checking' : 'Check integrity'}</button
      >
      <div class="run-text">
        {#if !over}
          <p id="check-help" class="muted small">Check integrity after the contest ends for everyone.</p>
        {:else if run?.state === 'done' && !running}
          <p class="muted small">Checking again replaces these signals. Cases keep their own copy of the evidence.</p>
        {/if}
      </div>
    </div>
    {#if startError}<p class="field-error" role="alert">{startError}</p>{/if}
    <div role="status" class="status">
      {#if running}
        <p>Checking the contest. This takes under a minute; the page updates when it is done.</p>
      {/if}
    </div>
  </div>

  <section class="section" aria-labelledby="signals-title">
    <h2 id="signals-title">Signals</h2>
    {#if runState === 'loading'}
      <p class="muted" role="status">Loading the last check</p>
    {:else if runState === 'error'}
      <div class="banner banner-error" role="alert"><p>{runError}</p></div>
    {:else if runState === 'none' || !run}
      <p class="muted">Not checked yet. {over ? 'Press Check integrity to look for copied code and other reasons to look closer.' : 'The check opens once the contest has ended for everyone.'}</p>
    {:else if run.state === 'error'}
      <div class="banner banner-error" role="alert"><p>The check stopped with an error: {run.error || 'no reason given'}. Try again in a minute.</p></div>
    {:else if run.state === 'running'}
      <p class="muted">The signals appear when the check is done.</p>
    {:else}
      <p class="stats">{statsLine(run.stats)} <span class="muted">Finished {formatDateTime(run.finished_at ?? run.started_at)}.</span></p>

      {#if (run.signals ?? []).length > 0}
        <div class="filters">
          <div class="field">
            <label for="filter-kind">Kind</label>
            <select id="filter-kind" class="select" bind:value={kind}>
              <option value="">All kinds</option>
              {#each KINDS as k (k.id)}<option value={k.id}>{k.label}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="filter-strength">Strength</label>
            <select id="filter-strength" class="select" bind:value={strength}>
              <option value="">Any strength</option>
              {#each STRENGTHS as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
            </select>
          </div>
          <div class="open">
            <button
              type="button"
              class="btn btn-secondary"
              disabled={chosen.length === 0}
              aria-describedby="open-help"
              onclick={() => (dialogOpen = true)}>Open a case</button
            >
            <p id="open-help" class="muted small" aria-live="polite">
              {chosen.length === 0 ? 'Choose one or more signals first.' : `${chosen.length} ${chosen.length === 1 ? 'signal' : 'signals'} chosen.`}
            </p>
          </div>
        </div>
      {/if}

      {#if (run.signals ?? []).length === 0}
        <p class="muted">Nothing stood out. That is not a guarantee: read the timelines of any student you have doubts about.</p>
      {:else if shown.length === 0}
        <p class="muted">No signals match these filters.</p>
      {:else}
        {#each groups as g (g.problemId)}
          {@const name = problemName(run.problems, g.problemId)}
          {@const groupsOf = clusters(g.signals)}
          <section class="problem" aria-labelledby="problem-{g.problemId}">
            <h3 id="problem-{g.problemId}">{name}</h3>
            <SignalTable
              signals={g.signals}
              users={run.users}
              problems={run.problems}
              contestId={id}
              caption="Signals on {name}"
              selectable
              {selected}
              ontoggle={toggle}
            />
            {#if groupsOf.length > 0}
              <div class="clusters">
                <h4>Groups of similar code</h4>
                <ul>
                  {#each groupsOf as cl (cl.join('-'))}
                    <li>{joinWords(cl.map((u) => userName(run?.users, u)))}</li>
                  {/each}
                </ul>
              </div>
            {/if}
          </section>
        {/each}
      {/if}
    {/if}
  </section>

  <section class="section" aria-labelledby="cases-title">
    <h2 id="cases-title">Cases</h2>
    {#if casesError}
      <p class="field-error">{casesError}</p>
    {:else if cases === null}
      <p class="muted" role="status">Loading the cases</p>
    {:else if cases.length === 0}
      <p class="muted">No cases yet. Choose signals above and open a case when one deserves a closer look.</p>
    {:else}
      <div class="table-wrap case-list">
        <table class="table table-stack">
          <caption class="visually-hidden">Cases for {contest.title}</caption>
          <thead>
            <tr>
              <th scope="col">Case</th>
              <th scope="col">Students</th>
              <th scope="col">State</th>
              <th scope="col">Updated</th>
            </tr>
          </thead>
          <tbody>
            {#each cases as c (c.id)}
              <tr>
                <th scope="row" class="case-title"><a class="row-link" href="/admin/integrity/cases/{c.id}">{c.title}</a></th>
                <td class="case-who">{caseStudents(c)}</td>
                <td class="case-state">{caseStateWord(c.state)}</td>
                <td class="num case-when">{formatDateTime(c.updated_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>

  <OpenCaseDialog
    open={dialogOpen}
    contestId={id}
    signals={chosen}
    users={run?.users}
    problems={run?.problems}
    onclose={() => (dialogOpen = false)}
    oncreated={created}
  />
{/if}

<style>
  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    min-width: 0;
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .small {
    font-size: var(--fs-0);
  }

  .window {
    max-width: 48rem;
  }

  .run {
    margin-top: var(--space-2);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
  }

  /* Out of the flow while empty, but still rendered so it announces. */
  .status:empty {
    position: absolute;
  }

  .stats {
    max-width: 56rem;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-3) var(--space-4);
  }

  .filters .field {
    width: min(14rem, 100%);
  }

  .open {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .problem {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    margin-top: var(--space-3);
  }

  .problem h3 {
    font-size: var(--fs-2);
  }

  .clusters h4 {
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .clusters ul {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-1);
    padding-left: var(--space-4);
    font-size: var(--fs-0);
  }

  .case-title {
    font-weight: 500;
  }

  .case-state {
    white-space: nowrap;
  }

  /* Phones: the case title, then the students, then state and time. */
  @media (width < 768px) {
    .case-list tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title title'
        'who who'
        'state when';
    }

    .case-title {
      grid-area: title;
    }

    .case-who {
      grid-area: who;
    }

    .case-state {
      grid-area: state;
    }

    .case-when {
      grid-area: when;
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: right;
    }
  }
</style>
