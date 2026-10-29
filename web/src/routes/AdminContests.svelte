<!--
  AdminContests: "Contests", the organiser landing page at /admin/contests
  (GET /api/admin/contests; "/" sends organisers here). From the top:

  - Needs you: unanswered questions in a live contest (one request to its
    questions until the list carries the count) and contest problems that
    changed since they were added, each a link to where it is fixed. It
    waits until a contest has run, because before that there is nothing
    to show, and it is left out when nothing needs action.
  - Live and next: one block for each published live contest and for the
    next published one, with its state word, a countdown in mono and
    "Open live view".
  - New contest: a small form for the title, the start and the length that
    creates the contest unpublished with the default settings (the server
    derives its address from the title) and opens its Setup.
  - Every other contest in one table, drafts and later ones first, then
    past ones newest first: the title (a link to the tab that fits its
    state, lib/contests organiserHome), when it starts, how long it lasts,
    the problem count and the organiser state word ("Draft" while students
    cannot see it). Under 1024 px each row reads as title and state, then
    the start, and the whole row is the link.

  With no contests yet it points to the problem bank and lists the steps to
  a first published contest, with a Done mark on those already taken: an
  allowed email domain and another organiser (owners, each a link to
  Settings), a validated problem, a published contest, sharing the
  address. This is the app's one first-run checklist; Settings has none.
  Students see a short explanation instead. Takes no props.
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
    organiserHome,
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
  import { loader } from '../lib/loader.svelte';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let contests = $state<AdminContestSummary[]>([]);

  const defaults = defaultSettings(Date.now());
  let creating = $state(false);
  let newTitle = $state('');
  let newStart = $state(toLocalInput(defaults.starts_at));
  // A number input binds a number, or null when empty.
  let newLength = $state<number | string | null>(Math.round((defaults.ends_at - defaults.starts_at) / 60_000));
  let createBusy = $state(false);
  let createError = $state('');
  let fields = $state<Record<string, string>>({});
  let titleInput: HTMLInputElement | undefined = $state();
  let newButton: HTMLButtonElement | undefined = $state();

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const isOwner = $derived(session.user?.role === 'owner');
  // First run (no contests): what is already done on the way to a published contest; null while unknown.
  let domainCount = $state<number | null>(null);
  /** Organisers other than the configured owner and the person signed in. */
  let otherOrganisers = $state<number | null>(null);
  let validatedCount = $state<number | null>(null);
  const origin = typeof location === 'undefined' ? '' : location.origin;

  const page = loader(
    async () => {
      const sent = Date.now();
      const data = await getJSON<{ contests: AdminContestSummary[]; server_now?: number }>('/api/admin/contests');
      return { data, sent, got: Date.now() };
    },
    ({ data, sent, got }) => {
      if (typeof data.server_now === 'number') serverClock.observe(data.server_now, sent, got);
      refreshTicker();
      contests = data.contests ?? [];
    },
  );

  async function loadFirstRun() {
    try {
      const data = await getJSON<{ problems: { validated: boolean; archived: boolean }[] }>('/api/admin/problems');
      if (page.live) validatedCount = (data.problems ?? []).filter((p) => p.validated && !p.archived).length;
    } catch {
      validatedCount = null;
    }
    if (!isOwner) return;
    try {
      const data = await getJSON<{ domains: unknown[]; people: { id: number; locked: boolean }[] }>('/api/admin/people');
      if (!page.live) return;
      domainCount = (data.domains ?? []).length;
      otherOrganisers = (data.people ?? []).filter((p) => !p.locked && p.id !== session.user?.id).length;
    } catch {
      domainCount = null;
      otherOrganisers = null;
    }
  }

  function isLive(c: AdminContestSummary): boolean {
    return c.state === 'live' || c.state === 'frozen';
  }

  const sorted = $derived(organiserOrder(contests));
  /** Published live contests, then the next published one. */
  const liveAndNext = $derived.by(() => {
    const next = sorted.find((c) => c.published && c.state === 'upcoming');
    return [...sorted.filter((c) => c.published && isLive(c)), ...(next ? [next] : [])];
  });
  const rest = $derived(sorted.filter((c) => !liveAndNext.includes(c)));
  /** "Needs you" waits until a contest has run (V3-20): before that there is nothing to show. */
  const hasRun = $derived(contests.some((c) => c.published && c.state !== 'upcoming'));

  /** Unanswered questions per live contest, by id. */
  let unanswered = $state<Record<number, number>>({});

  async function loadUnanswered(id: number) {
    try {
      const data = await getJSON<{ clarifications: Clarification[] }>(`/api/admin/contests/${id}/clarifications`);
      if (page.live) unanswered = { ...unanswered, [id]: (data.clarifications ?? []).filter((q) => !q.answer).length };
    } catch {
      // A contest whose questions did not load shows no count.
    }
  }

  /** What needs the organiser now, each with where it is fixed. */
  const needs = $derived.by(() => {
    const out: { text: string; href: string }[] = [];
    for (const c of sorted) {
      const n = unanswered[c.id] ?? 0;
      if (c.published && isLive(c) && n > 0) out.push({ text: `${countOf(n, 'unanswered question')} in ${c.title}`, href: `/admin/contests/${c.id}/console#clar-title` });
    }
    for (const c of sorted) {
      if (c.state === 'ended' || c.state === 'practice') continue;
      const changed = (c.problems ?? []).filter((p) => p.ready === false).map((p) => p.label);
      if (changed.length === 0) continue;
      const which = changed.length === 1 ? `Problem ${changed[0]}` : `Problems ${changed.join(', ')}`;
      const since = changed.length === 1 ? 'it was' : 'they were';
      out.push({ text: `${which} in ${c.title} changed since ${since} added. Validate, then add again.`, href: `/admin/contests/${c.id}` });
    }
    return out;
  });

  async function load() {
    await page.load();
    if (page.state !== 'ready') return;
    if (contests.length === 0) void loadFirstRun();
    for (const c of contests) if (c.published && isLive(c)) void loadUnanswered(c.id);
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
    const text = String(newLength ?? '').trim();
    const minutes = Number(text);
    if (title === '') fields.title = 'Enter a title.';
    if (starts === null) fields.starts_at = 'Enter the start date and time.';
    if (text === '' || !Number.isInteger(minutes) || minutes < 1) fields.ends_at = 'Enter the length in whole minutes.';
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
        ends_at: (starts as number) + minutes * 60_000,
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
    if (!isOrganiser) page.forbid();
    else void load();
    const stopTicker = useTicker();
    return () => {
      stopTicker();
      page.stop();
    };
  });
</script>

<PageHeader title="Contests" />

{#if page.state === 'forbidden'}
  <PageState kind="forbidden" message="Only organisers can manage contests." />
{:else}
  {#if page.state === 'ready' && hasRun && needs.length > 0}
    <section class="needs" aria-labelledby="needs-title">
      <h2 id="needs-title">Needs you</h2>
      <ul class="needs-list">
        {#each needs as n (n.text)}<li><a href={n.href}>{n.text}</a></li>{/each}
      </ul>
    </section>
  {/if}

  {#if page.state === 'ready' && liveAndNext.length > 0}
    <div class="next-list">
      {#each liveAndNext as f, i (f.id)}
        {@const left = isLive(f) ? (f.close_at ?? f.ends_at) - ticker.now : f.starts_at - ticker.now}
        {@const fs = organiserState(f)}
        <section class="next" aria-labelledby="next-{f.id}">
          <div class="next-head">
            <h2 id="next-{f.id}">{f.title}</h2>
            <span class="state-word" data-state={fs.kind}>{fs.word}</span>
          </div>
          <p class="next-line">
            {#if left > 0}
              {isLive(f) ? 'Ends in' : 'Starts in'} <MonoDigits parts={numberParts(left >= 86_400_000 ? formatSpan(left) : formatCountdown(left))} />{#if f.paused}, judging paused{/if}
            {:else}
              <MonoDigits text={formatDateTime(f.starts_at)} />
            {/if}
          </p>
          <p class="action-row">
            <a class="btn {i === 0 ? 'btn-primary' : 'btn-secondary'}" href="/admin/contests/{f.id}/console">Open live view</a>
          </p>
        </section>
      {/each}
    </div>
  {/if}

  <div class="toolbar">
    <button
      type="button"
      class="btn {liveAndNext.length > 0 ? 'btn-secondary' : 'btn-primary'}"
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
          <label for="new-length">Length in minutes</label>
          <input
            id="new-length"
            aria-required="true"
            class="input mono"
            type="number"
            min="1"
            inputmode="numeric"
            bind:value={newLength}
            aria-invalid={fields.ends_at ? 'true' : undefined}
            aria-describedby={fields.ends_at ? 'new-length-error' : undefined}
          />
          {#if fields.ends_at}<p id="new-length-error" class="field-error">{fields.ends_at}</p>{/if}
        </div>
      </div>
      <p class="field-help">The contest starts unpublished. Add problems in its Setup before you publish it.</p>
      <div class="actions">
        <button type="submit" class="btn btn-primary" disabled={createBusy}>Create contest</button>
        <button type="button" class="btn btn-quiet" onclick={closeCreate}>Cancel</button>
      </div>
      {#if createError}<p class="field-error" role="alert">{createError}</p>{/if}
    </form>
  {/if}

  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading contests" />
  {:else if page.state === 'error'}
    <PageState kind="error" message={page.error} onretry={load} />
  {:else if contests.length === 0}
    <PageState kind="empty" message="No contests yet. Contests use validated problems from the problem bank.">
      {#snippet action()}<a class="btn btn-secondary" href="/admin/problems">Go to the problem bank</a>{/snippet}
    </PageState>
    <section class="first-run" aria-labelledby="first-run-heading">
      <h2 id="first-run-heading">Your first contest</h2>
      <ul class="steps">
        {#if isOwner}
          <li>
            <span class="step">Allow your college's email domain in <a href="/admin/people?tab=sign-in">Settings</a>, so students can sign in.</span>
            {#if domainCount !== null}<Badge tone={domainCount > 0 ? 'ok' : 'neutral'} text={domainCount > 0 ? 'Done' : 'To do'} />{/if}
          </li>
          <li>
            <span class="step">Add another organiser in <a href="/admin/people">Settings</a>, so someone else can run a contest.</span>
            {#if otherOrganisers !== null}<Badge tone={otherOrganisers > 0 ? 'ok' : 'neutral'} text={otherOrganisers > 0 ? 'Done' : 'To do'} />{/if}
          </li>
        {/if}
        <li>
          <span class="step">Add or import a problem in the <a href="/admin/problems">problem bank</a>, then validate it.</span>
          {#if validatedCount !== null}<Badge tone={validatedCount > 0 ? 'ok' : 'neutral'} text={validatedCount > 0 ? 'Done' : 'To do'} />{/if}
        </li>
        <li>
          <span class="step">Create a contest with New contest, add problems, then publish it from its Setup.</span>
          <Badge tone="neutral" text="To do" />
        </li>
        <li>
          <span class="step">Share the address with your students: <span class="address">{origin}</span></span>
        </li>
      </ul>
    </section>
  {:else if rest.length > 0}
    <div class="table-wrap list">
      <table class="table table-stack">
        <thead>
          <tr>
            <th scope="col">Title</th>
            <th scope="col">Starts</th>
            <th scope="col">Length</th>
            <th scope="col" class="right">Problems</th>
            <th scope="col">State</th>
          </tr>
        </thead>
        <tbody>
          {#each rest as c (c.id)}
            {@const st = organiserState(c)}
            <tr>
              <th scope="row" class="title"><a class="row-link" href={organiserHome(c)}>{c.title}</a></th>
              <td class="when"><MonoDigits text={formatDateTime(c.starts_at)} /></td>
              <td class="length">{lengthText(c)}</td>
              <td class="num right problems">{problemCount(c)}</td>
              <td class="state"><span class="state-word" data-state={st.kind}>{st.word}</span>{#if c.paused}<span class="paused">Judging paused</span>{/if}</td>
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

  .first-run h2,
  .needs h2 {
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

  .needs {
    max-width: var(--measure-form);
    margin-top: var(--space-5);
  }

  .needs-list {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-3);
    padding-left: var(--space-5);
  }

  .needs-list a {
    overflow-wrap: anywhere;
  }

  .next-list {
    display: grid;
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .next {
    display: grid;
    gap: var(--space-2);
    max-width: var(--measure-form);
    padding-block: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .next-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-3);
  }

  .next-head h2 {
    font-size: var(--fs-section);
    overflow-wrap: anywhere;
  }

  .next-line {
    color: var(--ink-muted);
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

  /* Rows stack below 1024 px here (R1), wider than the phone tier of
     .table-stack in controls.css: title and state, then the start; the
     title link covers the row. */
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
        'when when';
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
  }
</style>
