<!--
  AdminContests: "Contests", the organiser landing page at /admin/contests
  (GET /api/admin/contests; "/" sends organisers here, v2 N1 and N6). The
  live contest, or else the next one, comes first on its own: its state
  word, a countdown in mono, the unanswered questions of a live contest
  (one request to its clarifications until the contest response carries
  the count, C10 in P3), and its links with Console first. Then every
  contest: live and frozen ones first (an extended contest stays among
  them), upcoming ones soonest first, past ones newest first. A table
  gives each contest's title (a link to its settings), when it starts, how
  long it lasts, the problem count, the organiser state word ("Draft"
  while students cannot see it, lib/contests organiserState) and its
  Console and Results. Under 1024 px each row reads as title and state,
  then the start and the links (R1). With no contests yet it points to
  the problem bank, and lists the steps to a first published contest
  with a Done mark on those already taken: an allowed email domain
  (owners), a validated problem, a published contest, sharing the address
  (C2-024). A featured contest that is not published says so and puts
  Settings first instead of a countdown. "New contest"
  opens a small form for a title and the start and end, creates the
  contest unpublished with the default settings (the server derives its
  address from the title) and goes to its settings. Students see a short explanation instead. Takes no props.
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick } from 'svelte';
  import { focusFirstInvalid } from '../lib/focus';
  import { ApiError, getJSON, sendJSON, errorText } from '../lib/api';
  import {
    defaultSettings,
    fromLocalInput,
    lengthText,
    organiserOrder,
    organiserState,
    problemCount,
    toLocalInput,
    type AdminContest,
    type AdminContestSummary,
  } from '../lib/contests';
  import MonoDigits from '../components/MonoDigits.svelte';
  import { serverClock } from '../lib/clock';
  import { countOf, formatDateTime, numberParts } from '../lib/format';
  import { formatCountdown, formatSpan, type Clarification } from '../lib/contests';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let contests = $state<AdminContestSummary[]>([]);

  const defaults = defaultSettings(Date.now());
  let creating = $state(false);
  let newTitle = $state('');
  let newStart = $state(toLocalInput(defaults.starts_at));
  let newEnd = $state(toLocalInput(defaults.ends_at));
  let createBusy = $state(false);
  let createError = $state('');
  let fields = $state<Record<string, string>>({});
  let titleInput: HTMLInputElement | undefined = $state();
  let newButton: HTMLButtonElement | undefined = $state();

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const isOwner = $derived(session.user?.role === 'owner');
  // First run (no contests): what is already done on the way to a published contest; null while unknown.
  let domainCount = $state<number | null>(null);
  let validatedCount = $state<number | null>(null);
  const origin = typeof location === 'undefined' ? '' : location.origin;

  async function loadFirstRun() {
    try {
      const data = await getJSON<{ problems: { validated: boolean; archived: boolean }[] }>('/api/admin/problems');
      validatedCount = (data.problems ?? []).filter((p) => p.validated && !p.archived).length;
    } catch {
      validatedCount = null;
    }
    if (!isOwner) return;
    try {
      const data = await getJSON<{ domains: unknown[] }>('/api/admin/people');
      domainCount = (data.domains ?? []).length;
    } catch {
      domainCount = null;
    }
  }
  const sorted = $derived(organiserOrder(contests));
  // The contest an organiser most likely came for: the live one, else the next.
  const featured = $derived(sorted.find((c) => isLive(c)) ?? sorted.find((c) => c.state === 'upcoming') ?? null);
  let unanswered = $state<number | null>(null);

  function isLive(c: AdminContestSummary): boolean {
    return c.state === 'live' || c.state === 'frozen';
  }

  async function loadUnanswered(id: number) {
    try {
      const data = await getJSON<{ clarifications: Clarification[] }>(`/api/admin/contests/${id}/clarifications`);
      unanswered = (data.clarifications ?? []).filter((q) => !q.answer).length;
    } catch {
      unanswered = null;
    }
  }

  $effect(() => {
    const f = featured;
    if (f && isLive(f)) void loadUnanswered(f.id);
    else unanswered = null;
  });

  async function load() {
    loadState = 'loading';
    try {
      const sent = Date.now();
      const data = await getJSON<{ contests: AdminContestSummary[]; server_now?: number }>('/api/admin/contests');
      if (typeof data.server_now === 'number') serverClock.observe(data.server_now, sent, Date.now());
      refreshTicker();
      contests = data.contests ?? [];
      loadState = 'ready';
      if (contests.length === 0) void loadFirstRun();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      if (e instanceof ApiError && e.status === 403) {
        loadState = 'forbidden';
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
    }
  }

  async function openCreate() {
    creating = true;
    createError = '';
    fields = {};
    await tick();
    titleInput?.focus();
  }

  async function closeCreate() {
    creating = false;
    newTitle = '';
    await tick();
    newButton?.focus();
  }

  async function create(event: SubmitEvent) {
    event.preventDefault();
    createError = '';
    fields = {};
    const title = newTitle.trim();
    const starts = fromLocalInput(newStart);
    const ends = fromLocalInput(newEnd);
    if (title === '') fields.title = 'Enter a title.';
    if (starts === null) fields.starts_at = 'Enter the start date and time.';
    if (ends === null) fields.ends_at = 'Enter the end date and time.';
    else if (starts !== null && ends <= starts) fields.ends_at = 'The contest must end after it starts.';
    if (Object.keys(fields).length > 0) {
      await focusFirstInvalid('.create');
      return;
    }
    createBusy = true;
    try {
      const made = await sendJSON<AdminContest>('POST', '/api/admin/contests', {
        ...defaults,
        title,
        starts_at: starts,
        ends_at: ends,
      });
      showToast(`Created ${made.title}`);
      navigate(`/admin/contests/${made.id}`);
    } catch (e) {
      if (e instanceof ApiError && Object.keys(e.fields).length > 0) {
        fields = { ...e.fields };
        if (Object.keys(e.fields).some((k) => !['title', 'starts_at', 'ends_at'].includes(k))) createError = e.message;
        await focusFirstInvalid('.create');
      } else {
        createError = errorText(e);
      }
    } finally {
      createBusy = false;
    }
  }

  onMount(() => {
    setTitle(routeTitle('admin-contests'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    return useTicker();
  });
</script>

<PageHeader title="Contests" />

{#if loadState === 'forbidden'}
  <PageState kind="forbidden" message="Only organisers can manage contests." />
{:else}
  {#if loadState === 'ready' && featured}
    {@const f = featured}
    {@const left = isLive(f) ? (f.close_at ?? f.ends_at) - ticker.now : f.starts_at - ticker.now}
    {@const fbase = `/admin/contests/${f.id}`}
    {@const fs = organiserState(f)}
    <section class="featured" aria-labelledby="featured-title">
      <div class="featured-head">
        <h2 id="featured-title">{f.title}</h2>
        <span class="state-word" data-state={fs.kind}>{fs.word}</span>
      </div>
      <p class="featured-line">
        {#if !f.published}
          Not published. Add problems and publish it in Settings.
        {:else if left > 0}
          {isLive(f) ? 'Ends in' : 'Starts in'} <MonoDigits parts={numberParts(left >= 86_400_000 ? formatSpan(left) : formatCountdown(left))} />{#if f.paused}, judging paused{/if}
        {:else}
          <MonoDigits text={formatDateTime(f.starts_at)} />
        {/if}
      </p>
      {#if isLive(f) && unanswered !== null}
        <p class="featured-line">
          {#if unanswered > 0}<a href="{fbase}/console#clar-title">{countOf(unanswered, 'unanswered question')}</a>{:else}No unanswered questions{/if}
        </p>
      {/if}
      <p class="featured-links action-row">
        {#if f.published}
          <a class="btn btn-primary" href="{fbase}/console">Console</a>
          <a class="btn btn-quiet" href={fbase}>Settings</a>
        {:else}
          <a class="btn btn-primary" href={fbase}>Settings</a>
          <a class="btn btn-quiet" href="{fbase}/console">Console</a>
        {/if}
        <a class="btn btn-quiet" href="{fbase}/results">Results</a>
        <a class="btn btn-quiet" href="/contests/{encodeURIComponent(f.slug)}">Student view</a>
      </p>
    </section>
  {/if}

  <div class="toolbar">
    <button
      type="button"
      class="btn {featured ? 'btn-secondary' : 'btn-primary'}"
      aria-expanded={creating}
      aria-controls="create-contest"
      bind:this={newButton}
      onclick={() => (creating ? closeCreate() : openCreate())}>New contest</button
    >
  </div>

  {#if creating}
    <form id="create-contest" class="create" novalidate onsubmit={create} aria-labelledby="create-heading">
      <h2 id="create-heading" class="create-heading">New contest</h2>
      <p class="field-help">All fields are required.</p>
      <div class="grid">
        <div class="field">
          <label for="new-title">Title</label>
          <input
            id="new-title"
            aria-required="true"
            class="input"
            type="text"
            autocomplete="off"
            bind:this={titleInput}
            bind:value={newTitle}
            aria-invalid={fields.title ? 'true' : undefined}
            aria-describedby={fields.title ? 'new-title-error' : undefined}
          />
          {#if fields.title}<p id="new-title-error" class="field-error">{fields.title}</p>{/if}
        </div>
        <div class="field">
          <label for="new-start">Starts</label>
          <input
            id="new-start"
            aria-required="true"
            class="input"
            type="datetime-local"
            bind:value={newStart}
            aria-invalid={fields.starts_at ? 'true' : undefined}
            aria-describedby={fields.starts_at ? 'new-start-error' : undefined}
          />
          {#if fields.starts_at}<p id="new-start-error" class="field-error">{fields.starts_at}</p>{/if}
        </div>
        <div class="field">
          <label for="new-end">Ends</label>
          <input
            id="new-end"
            aria-required="true"
            class="input"
            type="datetime-local"
            bind:value={newEnd}
            aria-invalid={fields.ends_at ? 'true' : undefined}
            aria-describedby={fields.ends_at ? 'new-end-error' : undefined}
          />
          {#if fields.ends_at}<p id="new-end-error" class="field-error">{fields.ends_at}</p>{/if}
        </div>
      </div>
      <p class="field-help">The contest starts unpublished. Add problems and check the settings before you publish it.</p>
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={createBusy}>Create contest</button>
        <button type="button" class="btn btn-quiet" onclick={closeCreate}>Cancel</button>
      </div>
      {#if createError}<p class="field-error" role="alert">{createError}</p>{/if}
    </form>
  {/if}

  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading contests" />
  {:else if loadState === 'error'}
    <PageState kind="error" message={loadError} onretry={load} />
  {:else if contests.length === 0}
    <PageState kind="empty" message="No contests yet. Contests use validated problems from the problem bank.">
      {#snippet action()}<a class="btn btn-secondary" href="/admin/problems">Go to the problem bank</a>{/snippet}
    </PageState>
    <section class="first-run" aria-labelledby="first-run-heading">
      <h2 id="first-run-heading">Your first contest</h2>
      <ul class="steps">
        {#if isOwner}
          <li>
            <span class="step">Allow your college's email domain on <a href="/admin/people">People</a>, so students can sign in.</span>
            {#if domainCount !== null}<Badge tone={domainCount > 0 ? 'ok' : 'neutral'} text={domainCount > 0 ? 'Done' : 'To do'} />{/if}
          </li>
        {/if}
        <li>
          <span class="step">Add or import a problem in the <a href="/admin/problems">problem bank</a>, then validate it.</span>
          {#if validatedCount !== null}<Badge tone={validatedCount > 0 ? 'ok' : 'neutral'} text={validatedCount > 0 ? 'Done' : 'To do'} />{/if}
        </li>
        <li>
          <span class="step">Create a contest with New contest, add problems, then publish it from its settings.</span>
          <Badge tone="neutral" text="To do" />
        </li>
        <li>
          <span class="step">Share the address with your students: <span class="address">{origin}</span></span>
        </li>
      </ul>
    </section>
  {:else}
    <div class="table-wrap list">
      <table class="table table-stack">
        <thead>
          <tr>
            <th scope="col">Title</th>
            <th scope="col">Starts</th>
            <th scope="col">Length</th>
            <th scope="col" class="right">Problems</th>
            <th scope="col">State</th>
            <th scope="col"><span class="visually-hidden">Links</span></th>
          </tr>
        </thead>
        <tbody>
          {#each sorted as c (c.id)}
            {@const st = organiserState(c)}
            <tr>
              <th scope="row" class="title"><a class="row-link" href="/admin/contests/{c.id}">{c.title}</a></th>
              <td class="when"><MonoDigits text={formatDateTime(c.starts_at)} /></td>
              <td class="length">{lengthText(c)}</td>
              <td class="num right problems">{problemCount(c)}</td>
              <td class="state"><span class="state-word" data-state={st.kind}>{st.word}</span>{#if c.paused}<span class="paused">Judging paused</span>{/if}</td>
              <td class="links">
                <span class="link-row">
                  <a class="btn btn-quiet btn-compact" href="/admin/contests/{c.id}/console" aria-label="Console for {c.title}">Console</a>
                  <a class="btn btn-quiet btn-compact" href="/admin/contests/{c.id}/results" aria-label="Results for {c.title}">Results</a>
                </span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
{/if}

<style>
  .first-run {
    max-width: var(--measure-form);
    margin-top: var(--section-gap);
  }

  .first-run h2 {
    font-size: var(--fs-section);
  }

  .steps {
    display: grid;
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .steps li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    min-height: var(--control-h-touch);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .step {
    flex: 1 1 20rem;
    min-width: 0;
  }

  .address {
    overflow-wrap: anywhere;
    font-weight: 500;
  }

  .featured {
    display: grid;
    gap: var(--space-2);
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    padding-bottom: var(--space-5);
    border-bottom: var(--rule) solid var(--line);
  }

  .featured-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-3);
  }

  .featured-head h2 {
    font-size: var(--fs-section);
    overflow-wrap: anywhere;
  }

  .featured-line {
    color: var(--ink-muted);
  }

  .featured-links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
    margin-top: var(--space-1);
  }

  .featured-links a:not(.btn) {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
  }

  .toolbar {
    margin-top: var(--space-5);
  }

  .create {
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .create-heading {
    font-size: var(--fs-2);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
    gap: var(--space-3) var(--space-4);
    margin-block: var(--space-3);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .create .field-error[role='alert'] {
    margin-top: var(--space-2);
  }

  .list {
    margin-top: var(--space-5);
  }

  .title {
    min-width: 10rem;
    font-weight: 500;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .length,
  .state {
    white-space: nowrap;
  }

  .paused {
    margin-left: var(--space-2);
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .links {
    white-space: nowrap;
  }

  .link-row {
    display: inline-flex;
    gap: var(--space-1);
  }

  /* Rows stack below 1024 px here (R1), wider than the phone tier of
     .table-stack in controls.css: title and state, then the start and the
     links. */
  @media (width < 1024px) {
    .list thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .list tbody tr {
      position: relative;
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title state'
        'when links';
      align-items: center;
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

    .list .row-link::after {
      content: '';
      position: absolute;
      inset: 0;
    }

    .list .link-row a {
      position: relative;
      z-index: 1;
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
      color: var(--ink-muted);
      font-size: var(--fs-0);
    }

    .length,
    .problems {
      display: none;
    }

    .links {
      grid-area: links;
      justify-self: end;
    }
  }

  @media (width < 768px) {
    .list tbody tr {
      grid-template-areas:
        'title state'
        'when when'
        'links links';
    }

    .links {
      justify-self: start;
    }
  }
</style>
