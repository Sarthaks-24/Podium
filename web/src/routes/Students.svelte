<!--
  Students: the organisers' table of students at /admin/students (v2 P4
  F9.4, F9.2b; GET /api/admin/students, docs/API.md "C9: organisers'
  students pages"). The shell loads it lazily.

  One question: who may need a word from an organiser. The table is
  sorted by name and never by score; the two filters ("Missed the last 2
  graded contests", "10 or more submissions, nothing accepted") come from
  the server over final graded contests only, and the line under them
  names the contests they looked at. A search matches a name or an email.
  Columns: the name (opens the student page) with the email under it, graded contests,
  the latest graded percent, the last submission, and the rating (owners
  and admins only, never shown to a student). Name and rating sort with a
  button in their header (aria-sort says the order); the default stays
  by name. Under 768 px each row stacks (.table-stack): name and rating,
  with the email, then the figures with their labels.

  Beside the table from 1280 px, below it on smaller screens: what the
  filters and the rating mean. States: loading, error, forbidden (anyone
  who is not an owner or admin), and an empty table that says why.
  Takes no props.
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import { onMount } from 'svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { bandOf, bandWord, FILTERS, filterHelp, percentText, ratingText, sortStudents, type SortKey, type StudentFilter, type StudentsList } from '../lib/students';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let data = $state.raw<StudentsList | null>(null);
  let filter = $state<StudentFilter>('');
  let query = $state('');
  /** The search and filter the shown rows answer, for the empty line. */
  let shownFor = $state({ filter: '' as StudentFilter, q: '' });
  let busy = $state(false);
  let sortKey = $state<SortKey>('name');
  let sortDir = $state<'asc' | 'desc'>('asc');

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const rows = $derived(data ? sortStudents(data.students, sortKey, sortDir) : []);
  const help = $derived(filterHelp(shownFor.filter, data?.last_graded ?? []));

  async function load(first = false) {
    if (first) loadState = 'loading';
    busy = true;
    const params: string[] = [];
    if (filter !== '') params.push(`filter=${encodeURIComponent(filter)}`);
    if (query.trim() !== '') params.push(`q=${encodeURIComponent(query.trim())}`);
    const q = params.join('&');
    try {
      const d = await getJSON<StudentsList>(`/api/admin/students${q ? `?${q}` : ''}`);
      const restore = first ? keepHeadingFocus() : null;
      data = d;
      shownFor = { filter, q: query.trim() };
      loadState = 'ready';
      void restore?.();
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
    } finally {
      busy = false;
    }
  }

  function search(event: SubmitEvent) {
    event.preventDefault();
    void load();
  }

  function sortBy(key: SortKey) {
    if (sortKey === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    else {
      sortKey = key;
      sortDir = 'asc';
    }
  }

  function ariaSort(key: SortKey): 'ascending' | 'descending' | undefined {
    if (sortKey !== key) return undefined;
    return sortDir === 'asc' ? 'ascending' : 'descending';
  }

  const emptyText = $derived.by(() => {
    if (shownFor.q !== '') return `No student matches "${shownFor.q}". Check the spelling, or clear the search.`;
    if (shownFor.filter !== '') return 'Nobody matches this filter.';
    return 'No students yet. They appear here once they sign in.';
  });

  onMount(() => {
    setTitle(routeTitle('students'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load(true);
  });
</script>

<PageHeader title="Students" lead="Everyone who has signed in as a student, sorted by name. Open a student for their contests and record." />

{#if loadState === 'forbidden'}
  <PageState kind="forbidden" message="Only owners and admins can see the students." />
{:else if loadState === 'loading'}
  <PageState kind="loading" message="Loading the students" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={() => load(true)} />
{:else if data}
  <div class="page-split students-split">
    <div class="page-split-main">
      <section class="panel" aria-labelledby="table-title">
        <div class="panel-head">
          <h2 id="table-title">{shownFor.filter === '' ? 'Every student' : (FILTERS.find((f) => f.id === shownFor.filter)?.label ?? 'Students')}</h2>
          <span class="muted small" aria-live="polite">{rows.length === 1 ? '1 student' : `${rows.length} students`}</span>
        </div>
        <form class="tools" novalidate onsubmit={search}>
          <div class="field">
            <label for="students-filter">Show</label>
            <select id="students-filter" class="select" bind:value={filter} onchange={() => load()}>
              {#each FILTERS as f (f.id)}<option value={f.id}>{f.label}</option>{/each}
            </select>
          </div>
          <div class="field grow">
            <label for="students-q">Name or email</label>
            <input id="students-q" class="input" type="search" autocomplete="off" spellcheck="false" bind:value={query} />
          </div>
          <button type="submit" class="btn btn-secondary" disabled={busy}>Search</button>
        </form>
        <p class="muted small" id="filter-help">{help}</p>

        {#if rows.length === 0}
          <p class="muted empty">{emptyText}</p>
        {:else}
          <div class="table-wrap">
            <table class="table table-stack list" aria-describedby="filter-help">
              <caption class="visually-hidden">Students, sorted by {sortKey === 'name' ? 'name' : 'rating'}, {sortDir === 'asc' ? 'ascending' : 'descending'}</caption>
              <thead>
                <tr>
                  <th scope="col" aria-sort={ariaSort('name')}>
                    <button type="button" class="sort" onclick={() => sortBy('name')}>Name<span class="arrow" aria-hidden="true">{sortKey === 'name' ? (sortDir === 'asc' ? 'A to Z' : 'Z to A') : ''}</span></button>
                  </th>
                  <th scope="col" class="right">Graded contests</th>
                  <th scope="col" class="right">Latest graded</th>
                  <th scope="col">Last submission</th>
                  <th scope="col" class="right" aria-sort={ariaSort('rating')}>
                    <button type="button" class="sort" onclick={() => sortBy('rating')}>Rating<span class="arrow" aria-hidden="true">{sortKey === 'rating' ? (sortDir === 'asc' ? 'low first' : 'high first') : ''}</span></button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {#each rows as r (r.id)}
                  {@const band = bandOf(r.rating_tenths)}
                  <tr>
                    <th scope="row" class="name">
                      <a class="row-link" href="/admin/students/{r.id}">{r.name || r.email}</a>
                      {#if r.name}<span class="email">{r.email}</span>{/if}
                    </th>
                    <td class="num right graded"><span class="cell-label">Graded contests</span>{r.graded_contests}</td>
                    <td class="num right latest">
                      <span class="cell-label">Latest graded</span>{#if r.last_graded_percent !== null}{percentText(r.last_graded_percent)}%{:else}<span class="muted word">None yet</span>{/if}
                    </td>
                    <td class="last"><span class="cell-label">Last submission</span>{#if r.last_submission_at !== null}<MonoDigits text={formatDateTime(r.last_submission_at)} />{:else}<span class="muted">Never</span>{/if}</td>
                    <td class="right rating">
                      <span class="num">{ratingText(r.rating_tenths)}</span>{#if band !== 'good'}<span class="band small">{bandWord(band)}</span>{/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </section>
    </div>

    <aside class="page-split-aside" aria-label="About this table">
      <section class="panel" aria-labelledby="about-title">
        <h2 id="about-title">What the columns mean</h2>
        <dl class="defs">
          <dt>Graded contests</dt>
          <dd>Final graded contests the student took part in. Practice and ungraded contests do not count.</dd>
          <dt>Latest graded</dt>
          <dd>Their score as a percentage in the most recent of those.</dd>
          <dt>Rating</dt>
          <dd>
            From 0 to 10, for owners and admins only. Students see their band, never the number: 9 to 10 is Good standing, 6 to 8.9
            Outcomes on record, below 6 Repeated outcomes.
          </dd>
        </dl>
      </section>
      {#if data.last_graded.length > 0}
        <section class="panel" aria-labelledby="last-title">
          <h2 id="last-title">Contests the filters use</h2>
          <ul class="contests">
            {#each data.last_graded as c (c.contest_id)}
              <li><a href="/admin/contests/{c.contest_id}/results">{c.title}</a></li>
            {/each}
          </ul>
        </section>
      {/if}
    </aside>
  </div>
{/if}

<style>
  .students-split {
    margin-top: var(--space-6);
  }

  /* The table needs more room than a form: the work column takes what the aside leaves. */
  @media (min-width: 1280px) {
    .students-split {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
    }
  }

  .panel h2 {
    font-size: var(--fs-sub);
  }

  .small {
    font-size: var(--fs-0);
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .grow {
    flex: 1 1 14rem;
  }

  .empty {
    padding-block: var(--space-3);
  }

  .right,
  .table .right {
    text-align: right;
  }

  .sort {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-2);
    min-height: var(--control-h);
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .sort:hover {
    color: var(--ink);
  }

  .arrow {
    color: var(--act);
    font-weight: 500;
  }

  .name {
    font-weight: 500;
  }

  .name a {
    color: var(--ink);
  }

  .email {
    display: block;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
    overflow-wrap: anywhere;
  }

  .last {
    white-space: nowrap;
  }

  .rating {
    white-space: nowrap;
  }

  .band {
    display: block;
    color: var(--ink-muted);
  }

  .cell-label {
    display: none;
  }

  .word {
    font-family: var(--font-ui);
  }

  .defs {
    display: grid;
    gap: var(--space-1);
    font-size: var(--fs-0);
  }

  .defs dt {
    margin-top: var(--space-2);
    font-weight: 600;
  }

  .defs dt:first-child {
    margin-top: 0;
  }

  .defs dd {
    margin: 0;
    color: var(--ink-muted);
  }

  .contests {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
  }

  /* Phones: name, email and rating, then the figures with their labels. */
  @media (width < 768px) {
    .list tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'name rating'
        'graded graded'
        'latest latest'
        'last last';
    }

    .name {
      grid-area: name;
    }

    .rating {
      grid-area: rating;
    }

    .graded {
      grid-area: graded;
    }

    .latest {
      grid-area: latest;
    }

    .last {
      grid-area: last;
      white-space: normal;
    }

    .list .graded,
    .list .latest,
    .list .last {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }

    .cell-label {
      display: inline;
      margin-inline-end: var(--space-1);
      font-family: var(--font-ui);
    }
  }
</style>
