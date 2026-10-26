<!--
  Students: the organisers' table of everyone who signed in as a student
  (GET /api/admin/students), sorted by name, with how many contests each
  took part in and their last submission. A search by name or email
  narrows it; the address keeps the search, so a reload shows the same
  rows. Results per contest are in each contest's standings and export.
  Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import { ApiError, errorText, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { navigate, router } from '../lib/router.svelte';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';

  interface StudentRow {
    id: number;
    name: string;
    email: string;
    contests: number;
    last_submission_at: number | null;
    disabled?: boolean;
  }

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let rows = $state.raw<StudentRow[]>([]);
  let query = $state('');
  let shownFor = $state('');
  let busy = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');

  // Only the newest request is shown, and a late answer never navigates back here.
  let loadSeq = 0;
  let alive = true;

  async function load(first = false) {
    if (first) loadState = 'loading';
    busy = true;
    const seq = ++loadSeq;
    const q = query.trim();
    const params = q ? `?q=${encodeURIComponent(q)}` : '';
    try {
      const d = await getJSON<{ students: StudentRow[] }>(`/api/admin/students${params}`);
      if (seq !== loadSeq || !alive) return;
      const restore = first ? keepHeadingFocus() : null;
      rows = d.students ?? [];
      shownFor = q;
      navigate(`/admin/students${params}`, { replace: true });
      loadState = 'ready';
      void restore?.();
    } catch (e) {
      if (seq !== loadSeq || !alive) return;
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
    } finally {
      if (seq === loadSeq) busy = false;
    }
  }

  function search(event: SubmitEvent) {
    event.preventDefault();
    void load();
  }

  onMount(() => {
    setTitle(routeTitle('students'));
    query = router.query.get('q') ?? '';
    if (!isOrganiser) loadState = 'forbidden';
    else void load(true);
    return () => {
      alive = false;
    };
  });
</script>

<PageHeader title="Students" lead="Everyone who has signed in as a student, sorted by name." />

{#if loadState === 'forbidden'}
  <PageState kind="forbidden" message="Only organisers can see the students." />
{:else if loadState === 'loading'}
  <PageState kind="loading" message="Loading the students" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={() => load(true)} />
{:else}
  <section class="panel list-panel" aria-labelledby="table-title">
    <div class="panel-head">
      <h2 id="table-title">Every student</h2>
      <span class="muted small" aria-live="polite">{rows.length === 1 ? '1 student' : `${rows.length} students`}</span>
    </div>
    <form class="tools" novalidate onsubmit={search}>
      <div class="field grow">
        <label for="students-q">Name or email</label>
        <input id="students-q" class="input" type="search" autocomplete="off" spellcheck="false" bind:value={query} />
      </div>
      <button type="submit" class="btn btn-secondary" disabled={busy}>Search</button>
    </form>

    {#if rows.length === 0}
      {#if shownFor !== ''}
        <p class="muted empty">No student matches "{shownFor}". Check the spelling, or clear the search.</p>
      {:else}
        <p class="muted empty">
          No students yet. A student appears here after signing in with an allowed email domain (an owner sets these on
          <a href="/admin/people">People</a>).
        </p>
      {/if}
    {:else}
      <div class="table-wrap">
        <table class="table table-stack list">
          <caption class="visually-hidden">Students, sorted by name</caption>
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col" class="right">Contests</th>
              <th scope="col">Last submission</th>
            </tr>
          </thead>
          <tbody>
            {#each rows as r (r.id)}
              <tr>
                <th scope="row" class="name">
                  {r.name || r.email}
                  {#if r.name && r.name !== r.email}<span class="email">{r.email}</span>{/if}
                  {#if r.disabled}<span class="email">Account disabled</span>{/if}
                </th>
                <td class="num right taken"><span class="cell-label">Contests</span>{r.contests}</td>
                <td class="last">
                  <span class="cell-label">Last submission</span>{#if r.last_submission_at !== null}<MonoDigits text={formatDateTime(r.last_submission_at)} />{:else}<span class="muted">Never</span>{/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
{/if}

<style>
  .list-panel {
    margin-top: var(--space-6);
  }

  .panel h2 {
    font-size: var(--fs-sub);
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

  .name {
    font-weight: 500;
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

  .cell-label {
    display: none;
  }

  /* Phones: the name, then the figures with their labels. */
  @media (width < 768px) {
    .list tbody tr {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'name'
        'taken'
        'last';
    }

    .name {
      grid-area: name;
    }

    .taken {
      grid-area: taken;
    }

    .last {
      grid-area: last;
      white-space: normal;
    }

    .list .taken,
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
