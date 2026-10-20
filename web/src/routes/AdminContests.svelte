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
  the problem bank (audit O6). "New contest"
  opens a small form for a title, a slug (suggested from the title until it
  is edited) and the start and end, creates the contest unpublished with
  the default settings (graded, fixed start, subtask feedback) and goes to
  its settings. Students see a short explanation instead. Takes no props.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick } from 'svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
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
  import { slugify } from '../lib/problems';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let contests = $state<AdminContestSummary[]>([]);

  const defaults = defaultSettings(Date.now());
  let creating = $state(false);
  let newTitle = $state('');
  let newSlug = $state('');
  let slugEdited = $state(false);
  let newStart = $state(toLocalInput(defaults.starts_at));
  let newEnd = $state(toLocalInput(defaults.ends_at));
  let createBusy = $state(false);
  let createError = $state('');
  let fields = $state<Record<string, string>>({});
  let titleInput: HTMLInputElement | undefined = $state();
  let newButton: HTMLButtonElement | undefined = $state();

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
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
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      if (e instanceof ApiError && e.status === 403) {
        loadState = 'forbidden';
        return;
      }
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
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
    newSlug = '';
    slugEdited = false;
    await tick();
    newButton?.focus();
  }

  async function create(event: SubmitEvent) {
    event.preventDefault();
    createError = '';
    fields = {};
    const title = newTitle.trim();
    const slug = newSlug.trim();
    const starts = fromLocalInput(newStart);
    const ends = fromLocalInput(newEnd);
    if (title === '') fields.title = 'Enter a title.';
    if (slug === '') fields.slug = 'Enter a slug, for example week-6.';
    if (starts === null) fields.starts_at = 'Enter the start date and time.';
    if (ends === null) fields.ends_at = 'Enter the end date and time.';
    else if (starts !== null && ends <= starts) fields.ends_at = 'The contest must end after it starts.';
    if (Object.keys(fields).length > 0) {
      await tick();
      document.querySelector<HTMLElement>('.create [aria-invalid="true"]')?.focus();
      return;
    }
    createBusy = true;
    try {
      const made = await sendJSON<AdminContest>('POST', '/api/admin/contests', {
        ...defaults,
        title,
        slug,
        starts_at: starts,
        ends_at: ends,
        duration_minutes: Math.round(((ends ?? 0) - (starts ?? 0)) / 60_000),
      });
      showToast(`Created ${made.title}`);
      navigate(`/admin/contests/${made.id}`);
    } catch (e) {
      if (e instanceof ApiError && Object.keys(e.fields).length > 0) {
        fields = { ...e.fields };
        if (Object.keys(e.fields).some((k) => !['title', 'slug', 'starts_at', 'ends_at'].includes(k))) createError = e.message;
      } else {
        createError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
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
  <PageState kind="forbidden" message="Only admins can manage contests." />
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
        {#if left > 0}
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
        <a class="btn btn-primary" href="{fbase}/console">Console</a>
        <a class="btn btn-quiet" href={fbase}>Settings</a>
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
      <div class="grid">
        <div class="field">
          <label for="new-title">Title</label>
          <input
            id="new-title"
            class="input"
            type="text"
            autocomplete="off"
            bind:this={titleInput}
            bind:value={newTitle}
            oninput={() => {
              if (!slugEdited) newSlug = slugify(newTitle);
            }}
            aria-invalid={fields.title ? 'true' : undefined}
            aria-describedby={fields.title ? 'new-title-error' : undefined}
          />
          {#if fields.title}<p id="new-title-error" class="field-error">{fields.title}</p>{/if}
        </div>
        <div class="field">
          <label for="new-slug">Slug</label>
          <input
            id="new-slug"
            class="input mono"
            type="text"
            autocomplete="off"
            spellcheck="false"
            bind:value={newSlug}
            oninput={() => (slugEdited = newSlug !== '')}
            aria-invalid={fields.slug ? 'true' : undefined}
            aria-describedby="new-slug-help{fields.slug ? ' new-slug-error' : ''}"
          />
          <p id="new-slug-help" class="field-help">Part of the contest address, like /contests/week-6.</p>
          {#if fields.slug}<p id="new-slug-error" class="field-error">{fields.slug}</p>{/if}
        </div>
        <div class="field">
          <label for="new-start">Starts</label>
          <input
            id="new-start"
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
    max-width: 48rem;
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
