<!--
  Inbox: "Notices" at /notices, the signed-in person's notices (contract
  C9, v2 P4 F9.1), newest first, from GET /api/me/notifications. Each
  notice is one row of a single list: what happened (the server's
  sentence), what to do, by when ("Reply by 26 Oct, 18:00"), on a shared
  case or a reminder the line "Nothing affects your marks until a person
  decides.", and one button that names the action ("Reply on the case
  page", "Read the decision", "See your result"). Case notices open the
  student case page (/integrity/cases/{case}). Unread notices carry a
  "New" badge for this visit; the page marks the ones it shows as read
  (POST /api/me/notifications/read), so the count in the header and the
  account menu drops. "Show older notices" loads the next page.

  While the person is in a live contest the server holds every notice
  (`held`), and the page says they wait until the contest ends. Empty:
  "No notices yet." with what will arrive here. Takes no props.
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import { onMount } from 'svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { markRead, notices } from '../lib/notices.svelte';
  import { dueLine, noticeAction, noticeHref, type Notice, type NoticeList } from '../lib/notifications';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';

  let loadState = $state<'loading' | 'ready' | 'error'>('loading');
  let loadError = $state('');
  let list = $state.raw<Notice[]>([]);
  let held = $state(false);
  let hasMore = $state(false);
  let older = $state<'idle' | 'loading' | 'error'>('idle');
  /** Ids that were unread when this visit loaded them; they keep "New" until the page closes. */
  let fresh = $state<Set<number>>(new Set());

  function signedOut(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  function adopt(page: NoticeList, append: boolean) {
    const items = page.notifications ?? [];
    list = append ? [...list, ...items] : items;
    held = page.held === true;
    hasMore = page.has_more === true;
    fresh = new Set([...fresh, ...items.filter((n) => n.read_at === null).map((n) => n.id)]);
    notices.unread = typeof page.unread === 'number' ? page.unread : notices.unread;
    void markRead(items).catch(() => undefined);
  }

  async function load() {
    loadState = 'loading';
    try {
      const page = await getJSON<NoticeList>('/api/me/notifications');
      const restore = keepHeadingFocus();
      adopt(page, false);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (signedOut(e)) return;
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      loadState = 'error';
    }
  }

  async function loadOlder() {
    const lastId = list[list.length - 1]?.id;
    if (lastId === undefined) return;
    older = 'loading';
    try {
      adopt(await getJSON<NoticeList>(`/api/me/notifications?before=${lastId}`), true);
      older = 'idle';
    } catch (e) {
      if (signedOut(e)) return;
      older = 'error';
    }
  }

  onMount(() => {
    setTitle(routeTitle('inbox'));
    void load();
  });
</script>

<PageHeader title="Notices" back={{ href: '/', label: 'Your contests' }} lead="Results that are final and questions from the organisers about your submissions." />

{#if loadState === 'loading'}
  <PageState kind="loading" message="Loading notices" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={load} />
{:else if held}
  <p class="state-line">You are in a contest now. Notices wait until it ends, so nothing interrupts you. They appear here afterwards.</p>
{:else if list.length === 0}
  <PageState kind="empty" message="No notices yet. When a contest's results are final, or an organiser asks you about a submission, it appears here." />
{:else}
  <ul class="notices panel">
    {#each list as n (n.id)}
      {@const href = noticeHref(n)}
      {@const due = dueLine(n)}
      <li class="notice">
        <div class="notice-head">
          <p class="happened" id="notice-{n.id}">{n.happened}</p>
          {#if fresh.has(n.id)}<Badge tone="act" text="New" />{/if}
        </div>
        {#if n.todo}<p>{n.todo}</p>{/if}
        {#if due}<p class="due"><MonoDigits text={due} /></p>{/if}
        {#if n.reassurance}<p class="muted">{n.reassurance}</p>{/if}
        <div class="notice-foot">
          {#if href}<a class="btn btn-secondary btn-compact" {href} aria-describedby="notice-{n.id}">{noticeAction(n.kind)}</a>{/if}
          <span class="muted small"><MonoDigits text={formatDateTime(n.created_at)} /></span>
        </div>
      </li>
    {/each}
  </ul>
  {#if hasMore}
    <div class="action-row more">
      <button type="button" class="btn btn-secondary" disabled={older === 'loading'} onclick={loadOlder}>Show older notices</button>
      {#if older === 'error'}<p class="field-error" role="alert">Older notices could not load. Try again.</p>{/if}
    </div>
  {/if}
{/if}

<style>
  .state-line {
    max-width: var(--measure-narrow);
    margin-top: var(--space-4);
  }

  .notices {
    gap: 0;
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    padding-block: 0;
    list-style: none;
  }

  .notice {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .notice:last-child {
    border-bottom: 0;
  }

  .notice-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-1) var(--space-3);
  }

  .happened {
    min-width: 0;
    font-weight: 600;
  }

  .due {
    font-weight: 500;
  }

  .notice-foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    margin-top: var(--space-1);
  }

  .small {
    font-size: var(--fs-0);
  }

  .more {
    margin-top: var(--space-4);
  }
</style>
