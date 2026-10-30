<!--
  ContestIntegrity: the organiser's similar-code report for one contest
  at /admin/contests/{id}/integrity (docs/INTEGRITY.md), titled and
  trailed "Similar code" like the button that opens it. "Check integrity"
  starts a check (POST .../integrity/run); it is disabled with the reason
  until every student's time is up, and while a check runs the page says
  so and polls GET .../integrity every two seconds until it is done. Under
  the stats line come the signals, filtered by kind and strength and
  grouped by problem in tables (strength in words, with the reason for
  similar code), with the groups of similar code on each problem listed as names,
  and "Download similar pairs (CSV)" for the college's own process. The
  decisions recorded for this contest are listed below, newest first; each
  can be changed (DecisionDialog). When the contest had an exam option on
  (V3-25), a table under the signals gives what the editor recorded per
  student over the contest (GET .../integrity/counts): pasted characters,
  and the focus losses and full-screen exits when those were counted.
  Every view carries IntegrityNote. The
  page has no tab of its own: Results links here with "Similar code" once
  the contest has ended. The contest loads through lib/loader, so an
  answer after the page has gone is dropped. Props: `id`.
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageState from '../components/PageState.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { onMount } from 'svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import DecisionDialog from '../components/DecisionDialog.svelte';
  import SignalTable from '../components/SignalTable.svelte';
  import { ApiError, errorText, getJSON, isSignedOut, sendJSON } from '../lib/api';
  import { anyExamOption, type AdminContest } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import {
    KINDS,
    STRENGTHS,
    clusters,
    decisionWord,
    filterSignals,
    groupByProblem,
    joinWords,
    problemName,
    statsLine,
    userName,
    type Case,
    type Run,
  } from '../lib/review';
  import { session } from '../lib/session.svelte';
  import { ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  /** How often a running check is polled, in ms. */
  const POLL_MS = 2000;

  let contest = $state.raw<AdminContest | null>(null);
  let run = $state.raw<Run | null>(null);
  let runState = $state<'loading' | 'none' | 'ready' | 'error'>('loading');
  let runError = $state('');
  let startError = $state('');
  let starting = $state(false);
  let cases = $state.raw<Case[] | null>(null);
  let casesError = $state('');
  /** What the editor recorded per student, when an exam option is on (V3-25). */
  type Counts = { user_id: number; name: string; pastes: number; pasted_chars: number; focus_losses: number; fullscreen_exits: number };
  let counts = $state.raw<Counts[] | null>(null);
  let countsError = $state('');

  let kind = $state('');
  let strength = $state('');
  let deciding = $state<Case | { user_id: number; name: string } | null>(null);

  let pollTimer: ReturnType<typeof setTimeout> | null = null;
  // Taken as the data arrives, just before the loading view gives way, so the heading keeps focus.
  let restoreFocus: () => Promise<void> = async () => undefined;

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const page = loader(
    () => getJSON<AdminContest>(base),
    (c) => {
      restoreFocus = keepHeadingFocus();
      contest = c;
      setTitle(routeTitle('contest-integrity'), c.title);
    },
  );
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

  /** A side request that failed because the page is gone or signed out says nothing. */
  function quiet(e: unknown): boolean {
    return isSignedOut(e) || !page.live;
  }

  async function load() {
    await page.load();
    void restoreFocus();
    if (page.state === 'missing') setTitle('Contest not found');
    if (page.state !== 'ready') return;
    void loadRun();
    void loadCases();
    if (anyExamOption(contest)) void loadCounts();
  }

  async function loadCounts() {
    countsError = '';
    try {
      const data = await getJSON<{ counts: Counts[] }>(`${base}/integrity/counts`);
      if (page.live) counts = data.counts ?? [];
    } catch (e) {
      if (quiet(e)) return;
      countsError = errorText(e);
    }
  }

  async function loadRun(): Promise<Run | null> {
    try {
      const r = await getJSON<Run>(`${base}/integrity`);
      if (!page.live) return null;
      run = r;
      runState = 'ready';
      if (r.state === 'running') schedulePoll();
      return r;
    } catch (e) {
      if (quiet(e)) return null;
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
      runError = errorText(e);
      runState = 'error';
      return null;
    }
  }

  async function loadCases() {
    casesError = '';
    try {
      const data = await getJSON<{ cases: Case[] }>(`${base}/integrity/cases`);
      if (page.live) cases = data.cases ?? [];
    } catch (e) {
      if (quiet(e)) return;
      casesError = errorText(e);
    }
  }

  function schedulePoll() {
    if (pollTimer !== null || !page.live) return;
    pollTimer = setTimeout(async () => {
      pollTimer = null;
      const r = await loadRun();
      if (r && r.state === 'done') showToast('The check finished');
    }, POLL_MS);
  }

  /** A new check replaces the signals shown, so checking again asks first (C3). */
  let recheckOpen = $state(false);

  async function startCheck() {
    recheckOpen = false;
    if (running) return;
    startError = '';
    starting = true;
    try {
      await sendJSON<{ run_id: number }>('POST', `${base}/integrity/run`);
      await loadRun();
      schedulePoll();
    } catch (e) {
      if (!quiet(e)) startError = errorText(e);
    } finally {
      starting = false;
    }
  }

  function decided() {
    deciding = null;
    showToast('Recorded the decision');
    void loadCases();
  }

  onMount(() => {
    setTitle(routeTitle('contest-integrity'));
    if (!isOrganiser) page.forbid();
    else void load();
    const stop = useTicker();
    return () => {
      page.stop();
      if (pollTimer !== null) clearTimeout(pollTimer);
      stop();
    };
  });
</script>

{#if page.state === 'forbidden'}
  <PageHeader title="Similar code" />
  <PageState kind="forbidden" message="Only organisers can review similar code." />
{:else if page.state !== 'ready' || !contest}
  <PageHeader
    title={page.state === 'missing' ? 'Contest not found' : 'Similar code'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: page.state === 'missing' ? 'Contest not found' : 'Similar code' }]}
  />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={page.error} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="integrity" />

  <div class="section">
    <IntegrityNote />
    <div class="run">
      <button
        type="button"
        class="btn btn-primary"
        disabled={!over}
        aria-disabled={running ? 'true' : undefined}
        aria-describedby={!over ? 'check-help' : undefined}
        onclick={() => (run?.state === 'done' && !running ? (recheckOpen = true) : startCheck())}
        >{running ? 'Checking' : run?.state === 'done' ? 'Check again' : 'Check integrity'}</button
      >
      <div class="run-text">
        {#if !over}
          <p id="check-help" class="muted small">Check integrity after the contest ends for everyone.</p>
        {:else if run?.state === 'done' && !running}
          <p class="muted small">Checking again replaces these signals. Decisions stay.</p>
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
      <p class="stats">{statsLine(run.stats)} <span class="muted">Finished <MonoDigits text={formatDateTime(run.finished_at ?? run.started_at)} />.</span></p>

      {#if (run.signals ?? []).some((x) => x.kind === 'similar')}
        <p><a class="btn btn-secondary" href="{base}/integrity/pairs.csv" download>Download similar pairs (CSV)</a></p>
      {/if}
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

  {#if anyExamOption(contest)}
    <section class="section" aria-labelledby="counts-title">
      <h2 id="counts-title">Recorded during the contest</h2>
      {#if countsError}
        <p class="field-error">{countsError}</p>
      {:else if counts === null}
        <p class="muted" role="status">Loading what was recorded</p>
      {:else if counts.length === 0}
        <p class="muted">Nothing was recorded: no pastes{contest.count_focus ? ', no tab switches' : ''}{contest.full_screen ? ', no exits from full screen' : ''}.</p>
      {:else}
        <div class="table-wrap">
          <table class="table table-stack">
            <caption class="visually-hidden">What the editor recorded per student in {contest.title}</caption>
            <thead>
              <tr>
                <th scope="col">Student</th>
                <th scope="col">Pasted</th>
                {#if contest.count_focus}<th scope="col">Left the page</th>{/if}
                {#if contest.full_screen}<th scope="col">Left full screen</th>{/if}
              </tr>
            </thead>
            <tbody>
              {#each counts as c (c.user_id)}
                <tr>
                  <th scope="row">{c.name}</th>
                  <td><MonoDigits text={`${c.pasted_chars} characters in ${c.pastes === 1 ? '1 paste' : `${c.pastes} pastes`}`} /></td>
                  {#if contest.count_focus}<td><MonoDigits text={c.focus_losses === 1 ? '1 time' : `${c.focus_losses} times`} /></td>{/if}
                  {#if contest.full_screen}<td><MonoDigits text={c.fullscreen_exits === 1 ? '1 time' : `${c.fullscreen_exits} times`} /></td>{/if}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}

  <section class="section" aria-labelledby="cases-title">
    <h2 id="cases-title">Decisions</h2>
    {#if casesError}
      <p class="field-error">{casesError}</p>
    {:else if cases === null}
      <p class="muted" role="status">Loading the decisions</p>
    {:else if cases.length === 0}
      <p class="muted">No decisions yet. Open a signal and record a decision about a student when one deserves it.</p>
    {:else}
      <div class="table-wrap case-list">
        <table class="table table-stack">
          <caption class="visually-hidden">Decisions for {contest.title}</caption>
          <thead>
            <tr>
              <th scope="col">Student</th>
              <th scope="col">Decision</th>
              <th scope="col">Reason</th>
              <th scope="col">Recorded</th>
              <th scope="col"><span class="visually-hidden">Change</span></th>
            </tr>
          </thead>
          <tbody>
            {#each cases as c (c.id)}
              <tr>
                <th scope="row" class="case-who">{c.name}</th>
                <td class="case-state">{decisionWord(c.decision)}</td>
                <td class="case-reason">{c.reason}</td>
                <td class="case-when"><MonoDigits text={formatDateTime(c.decided_at)} /><span class="muted small"> by {c.decided_by || 'a removed account'}</span></td>
                <td class="case-act"><button type="button" class="btn btn-quiet btn-compact" onclick={() => (deciding = c)}>Change<span class="visually-hidden"> the decision about {c.name}</span></button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>

  <DecisionDialog
    open={deciding !== null}
    contestId={id}
    student={deciding ? { id: deciding.user_id, name: deciding.name } : null}
    current={deciding && 'decision' in deciding ? deciding : null}
    onclose={() => (deciding = null)}
    ondecided={decided}
  />
  <ConfirmDialog
    open={recheckOpen}
    title="Check again?"
    body="The new check replaces the signals listed here. Decisions already recorded stay."
    confirmLabel="Check again"
    onconfirm={startCheck}
    oncancel={() => (recheckOpen = false)}
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
    max-width: var(--measure-wide);
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

  .case-state {
    white-space: nowrap;
  }

  .case-reason {
    max-width: var(--measure-form);
    overflow-wrap: anywhere;
  }
</style>
