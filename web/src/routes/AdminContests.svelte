<!--
  AdminContests: the organiser list of contests at /admin/contests
  (GET /api/admin/contests), soonest first with past ones after. A table
  gives each contest's title (a link to its settings), when it starts, how
  long it lasts, the problem count, the state word, whether it is
  published, and links to its console and results. Under 768 px each row
  reads as title and state, then the start and the links. "New contest"
  opens a small form for a title, a slug (suggested from the title until it
  is edited) and the start and end, creates the contest unpublished with
  the default settings (graded, fixed start, subtask feedback) and goes to
  its settings. Students see a short explanation instead. Takes no props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import {
    defaultSettings,
    fromLocalInput,
    lengthText,
    problemCount,
    stateWord,
    toLocalInput,
    type AdminContest,
    type AdminContestSummary,
  } from '../lib/contests';
  import { formatDateTime } from '../lib/format';
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
  const sorted = $derived.by(() => {
    const now = Date.now();
    const ahead = contests.filter((c) => c.ends_at >= now).sort((a, b) => a.starts_at - b.starts_at);
    const past = contests.filter((c) => c.ends_at < now).sort((a, b) => b.starts_at - a.starts_at);
    return [...ahead, ...past];
  });

  async function load() {
    loadState = 'loading';
    try {
      const data = await getJSON<{ contests: AdminContestSummary[] }>('/api/admin/contests');
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
  });
</script>

<h1 tabindex="-1">Contest admin</h1>

{#if loadState === 'forbidden'}
  <p class="lead">Only admins can manage contests. Ask an owner if you need access.</p>
{:else}
  <div class="toolbar">
    <button
      type="button"
      class="btn btn-primary"
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
    <p class="lead muted" role="status">Loading contests</p>
  {:else if loadState === 'error'}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
  {:else if contests.length === 0}
    <p class="lead muted">No contests yet. Create one, then add validated problems to it.</p>
  {:else}
    <div class="table-wrap list">
      <table class="table">
        <thead>
          <tr>
            <th scope="col">Title</th>
            <th scope="col">Starts</th>
            <th scope="col">Length</th>
            <th scope="col" class="right">Problems</th>
            <th scope="col">State</th>
            <th scope="col">Visibility</th>
            <th scope="col"><span class="visually-hidden">Links</span></th>
          </tr>
        </thead>
        <tbody>
          {#each sorted as c (c.id)}
            <tr>
              <th scope="row" class="title"><a href="/admin/contests/{c.id}">{c.title}</a></th>
              <td class="num when">{formatDateTime(c.starts_at)}</td>
              <td class="length">{lengthText(c)}</td>
              <td class="num right problems">{problemCount(c)}</td>
              <td class="state">{stateWord(c.state)}{c.paused ? ', judging paused' : ''}</td>
              <td class="pub" class:draft={!c.published}>{c.published ? 'Published' : 'Draft'}</td>
              <td class="links">
                <span class="link-row">
                  <a href="/admin/contests/{c.id}/console" aria-label="Console for {c.title}">Console</a>
                  <a href="/admin/contests/{c.id}/results" aria-label="Results for {c.title}">Results</a>
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
  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
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
  .state,
  .pub {
    white-space: nowrap;
  }

  .pub.draft {
    color: var(--ink-muted);
  }

  .links {
    white-space: nowrap;
  }

  .link-row {
    display: inline-flex;
    gap: var(--space-3);
  }

  @media (max-width: 767px) {
    .list thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .list tbody tr {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'title state'
        'when pub'
        'links links';
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

    .pub {
      grid-area: pub;
      font-size: var(--fs-0);
      text-align: right;
    }

    .length,
    .problems {
      display: none;
    }

    .links {
      grid-area: links;
      height: auto;
    }
  }
</style>
