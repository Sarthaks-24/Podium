<!--
  DeletedProblems: the "Deleted problems" list for admins and the owner, at
  /admin/problems?deleted=1. One row per deleted problem: its title and
  original slug (plain text, nothing to open), when and by whom it was
  deleted, and whether it was ever used. There is no restore. It loads
  GET /api/admin/problems/deleted through lib/loader and shows the usual
  loading, error and empty states.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageState from '../PageState.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { getJSON } from '../../lib/api';
  import { formatDateTime } from '../../lib/format';
  import { loader } from '../../lib/loader.svelte';
  import type { DeletedProblem } from '../../lib/problems';

  let rows = $state<DeletedProblem[]>([]);
  const page = loader(
    () => getJSON<{ problems: DeletedProblem[] }>('/api/admin/problems/deleted'),
    (data) => (rows = data.problems ?? []),
    () => '/api/admin/problems/deleted',
  );

  onMount(() => {
    void page.load();
    return () => page.stop();
  });
</script>

<section class="deleted" aria-labelledby="deleted-title">
  <h2 id="deleted-title">Deleted problems</h2>
  <p class="muted lead">A deleted problem cannot be restored. Ended contests that used it keep their standings and show it as Deleted problem.</p>
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading deleted problems" shape="rows" />
  {:else if page.state === 'error' || page.state === 'missing' || page.state === 'forbidden'}
    <PageState kind="error" message={page.error} onretry={() => page.load()} />
  {:else if rows.length === 0}
    <PageState kind="empty" message="No problem has been deleted." />
  {:else}
    <div class="table-wrap list">
      <table class="table table-stack">
        <thead>
          <tr>
            <th scope="col">Title</th>
            <th scope="col">Deleted</th>
            <th scope="col">By</th>
            <th scope="col">Used</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as r (r.id)}
            <tr>
              <th scope="row" class="title">{r.title.trim() !== '' ? r.title : r.slug}<span class="orig-slug mono">{r.slug}</span></th>
              <td class="when"><MonoDigits text={formatDateTime(r.deleted_at)} /></td>
              <td class="by">{r.deleted_by !== '' ? r.deleted_by : 'An account that no longer exists'}</td>
              <td class="used">{r.used ? 'In contests or submitted to' : 'Never used'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<style>
  .deleted {
    margin-top: var(--space-5);
  }

  .lead {
    margin-top: var(--space-2);
    max-width: var(--measure-narrow);
  }

  .list {
    margin-top: var(--space-4);
  }

  .title {
    font-weight: 500;
    min-width: 9rem;
  }

  .orig-slug {
    margin-left: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .by,
  .used {
    color: var(--ink-muted);
  }

  @media (width < 768px) {
    .list tbody tr {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'title'
        'when'
        'by'
        'used';
    }

    .title {
      grid-area: title;
      min-width: 0;
    }

    .when {
      grid-area: when;
    }

    .by {
      grid-area: by;
    }

    .used {
      grid-area: used;
    }

    .when,
    .by,
    .used {
      font-size: var(--fs-0);
    }
  }
</style>
