<!--
  HistoryDialog: the site's history of changes, behind the "History of
  changes" button on the Organisation tab of Settings (GET
  /api/admin/site-audit): every change to people, domains, accounts and
  the server's mode, newest first, with who made it. It filters by the
  kind of change and by an email, and loads older pages on request. It
  loads each time it opens, so it never shows a stale list. Props: `open`
  and `onclose`.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import Dialog from '../Dialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { errorText, getJSON, isSignedOut } from '../../lib/api';
  import { formatDateTime } from '../../lib/format';
  import { SITE_ACTIONS, actorText, feedQuery, siteAuditSummary, type SiteAuditEntry, type SiteAuditPage } from '../../lib/history';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    open: boolean;
    onclose: () => void;
  }

  let { open, onclose }: Props = $props();

  let entries = $state<SiteAuditEntry[]>([]);
  let status = $state<'loading' | 'ready' | 'error'>('loading');
  let page = $state(1);
  let more = $state(false);
  let action = $state('');
  let search = $state('');
  let busy = $state(false);

  /** Loads a page of changes; page 1 replaces the list, later pages add older rows. */
  async function load(next: number) {
    busy = true;
    if (next === 1) status = 'loading';
    try {
      const data = await getJSON<SiteAuditPage>(`/api/admin/site-audit${feedQuery({ page: next, action, q: search.trim() })}`);
      entries = next === 1 ? (data.entries ?? []) : [...entries, ...(data.entries ?? [])];
      page = next;
      more = data.has_more === true;
      status = 'ready';
    } catch (err) {
      if (isSignedOut(err)) return;
      if (next === 1) status = 'error';
      else showToast(errorText(err), 'error');
    } finally {
      busy = false;
    }
  }

  // Opening loads; the filters load when submitted.
  $effect(() => {
    if (open) untrack(() => void load(1));
  });

  function filter(event?: SubmitEvent) {
    event?.preventDefault();
    void load(1);
  }
</script>

<Dialog {open} {onclose} title="History of changes" closeLabel="Close">
  <p class="muted small lead">Every change to people, domains and the site, newest first. Nothing here can be edited or removed.</p>
  <form class="row" novalidate onsubmit={filter}>
    <div class="field">
      <label for="history-action">Kind of change</label>
      <select id="history-action" class="select" bind:value={action} onchange={() => filter()}>
        <option value="">All changes</option>
        {#each SITE_ACTIONS as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
      </select>
    </div>
    <div class="field grow">
      <label for="history-search">Email contains</label>
      <input id="history-search" class="input" type="search" autocomplete="off" spellcheck="false" bind:value={search} />
    </div>
    <button type="submit" class="btn btn-secondary" disabled={busy}>Search</button>
  </form>
  <div role="region" aria-label="Changes" class="list">
    {#if status === 'loading'}
      <p class="muted small" role="status">Loading the history</p>
    {:else if status === 'error'}
      <p class="muted small">
        The history could not be loaded. <button type="button" class="btn btn-quiet btn-compact" onclick={() => load(1)}>Try again</button>
      </p>
    {:else if entries.length === 0}
      <p class="muted small">{action !== '' || search.trim() !== '' ? 'No change matches. Clear the filter to see everything.' : 'No changes yet.'}</p>
    {:else}
      <ul class="items">
        {#each entries as e (e.id)}
          <li>
            <p class="meta small"><MonoDigits text={formatDateTime(e.at)} /> {actorText(e.actor)}</p>
            <p class="entry">{siteAuditSummary(e)}</p>
          </li>
        {/each}
      </ul>
      {#if more}
        <button type="button" class="btn btn-quiet btn-compact" disabled={busy} onclick={() => load(page + 1)}>Show older changes</button>
      {/if}
    {/if}
  </div>
</Dialog>

<style>
  .lead {
    margin-top: var(--space-2);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .grow {
    flex: 1 1 12rem;
  }

  .list {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    margin-top: var(--space-4);
  }

  .items {
    display: grid;
    width: 100%;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .items > li {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }

  .meta {
    color: var(--ink-muted);
  }

  .entry {
    overflow-wrap: anywhere;
  }
</style>
