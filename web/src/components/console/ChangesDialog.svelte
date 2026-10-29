<!--
  ChangesDialog: the contest's audit log, behind the console's "History of
  changes" link (stage C2): every change that can affect results, newest
  first, with who made it (GET .../audit). It filters by the kind of
  change and by who made it, shows the latest 10 with "Show all", and
  loads older pages on request. It loads when it opens, and again when
  `refresh` changes while it is open (the page raises it after each
  action). Extra time says whether a reason was written, never the reason.

  Props: `open`, `base` (the contest's admin API path), `names` (student
  names by user id, for rows that name a student by id), `refresh` and
  `onclose`.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import Dialog from '../Dialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, getJSON } from '../../lib/api';
  import { AUDIT_ACTIONS, auditSummary, type AuditEntry } from '../../lib/contests';
  import { feedQuery } from '../../lib/history';
  import { ticker } from '../../lib/ticker.svelte';
  import { formatWhen } from '../../lib/workspace';

  interface Props {
    open: boolean;
    base: string;
    names: ReadonlyMap<number, string>;
    refresh: number;
    onclose: () => void;
  }

  let { open, base, names, refresh, onclose }: Props = $props();

  const SHOWN = 10;

  let audit = $state<AuditEntry[]>([]);
  let auditState = $state<'loading' | 'ready' | 'error'>('loading');
  let showAll = $state(false);
  let page = $state(1);
  let more = $state(false);
  let action = $state('');
  let actor = $state('');

  /** Loads a page of changes; page 1 replaces the list, later pages add older rows. */
  async function load(next = 1) {
    try {
      const data = await getJSON<{ entries: AuditEntry[]; has_more?: boolean }>(`${base}/audit${feedQuery({ page: next, action, actor: actor.trim() })}`);
      audit = next === 1 ? (data.entries ?? []) : [...audit, ...(data.entries ?? [])];
      page = next;
      more = data.has_more === true;
      auditState = 'ready';
    } catch (e) {
      if (!(e instanceof ApiError && e.status === 401)) auditState = 'error';
    }
  }

  // Only opening and `refresh` reload; the filters load when submitted.
  $effect(() => {
    void refresh;
    if (open) untrack(() => void load(1));
  });

  function filter(event?: SubmitEvent) {
    event?.preventDefault();
    showAll = false;
    void load(1);
  }
</script>

<Dialog {open} {onclose} title="History of changes" closeLabel="Close">
  <p class="muted small lead">Every change that can affect results, with who made it. Nothing here can be edited or deleted.</p>
  <form class="row" novalidate onsubmit={filter}>
    <div class="field">
      <label for="audit-action">Kind of change</label>
      <select id="audit-action" class="select" bind:value={action} onchange={() => filter()}>
        <option value="">All changes</option>
        {#each AUDIT_ACTIONS as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
      </select>
    </div>
    <div class="field grow">
      <label for="audit-actor">Made by</label>
      <!-- An example, not a placeholder that reads as a value already there (C2-068). -->
      <p id="audit-actor-help" class="field-help">An email, for example ta@college.edu</p>
      <input id="audit-actor" class="input" type="search" autocomplete="off" spellcheck="false" aria-describedby="audit-actor-help" bind:value={actor} />
    </div>
    <button type="submit" class="btn btn-secondary">Search</button>
  </form>
  <div role="region" aria-label="Changes" class="list">
    {#if auditState === 'loading'}
      <p class="muted small" role="status">Loading the changes</p>
    {:else if auditState === 'error'}
      <p class="muted small">The changes could not be loaded. Close this and open it again to retry.</p>
    {:else if audit.length === 0}
      <p class="muted small">{action !== '' || actor.trim() !== '' ? 'No change matches. Clear the filter to see everything.' : 'Nothing recorded yet.'}</p>
    {:else}
      <ul class="items">
        {#each showAll ? audit : audit.slice(0, SHOWN) as e (e.id)}
          <li>
            <p class="meta small"><MonoDigits text={formatWhen(e.created_at, ticker.now)} /> {e.actor?.email ?? 'The server'}</p>
            <p class="entry">{auditSummary(e, names)}</p>
          </li>
        {/each}
      </ul>
      {#if audit.length > SHOWN}
        <button type="button" class="btn btn-quiet btn-compact" aria-expanded={showAll} onclick={() => (showAll = !showAll)}>
          {showAll ? 'Show the latest 10' : `Show all ${audit.length}`}
        </button>
      {/if}
      {#if more && (showAll || audit.length <= SHOWN)}
        <button type="button" class="btn btn-quiet btn-compact" onclick={() => load(page + 1)}>Show older changes</button>
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
