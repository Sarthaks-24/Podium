<!--
  ContactInbox: the messages people sent through "Write to the organisers",
  at /admin/contact, for owners and admins (contract C6, P3 FP.2). Newest
  first, 50 at a time with "Show older messages" (GET /api/admin/contact
  ?before=). Each message reads as the topic, the sender's name and email,
  when it was sent and the text, shown as plain text with its line breaks,
  never as HTML. "Reply by email" is a mailto: link to the sender (there is
  no mail server, D71); "Delete" asks first and removes it
  (DELETE /api/admin/contact/{id}, recorded in the site audit). Co-organisers
  whose site role is student get the forbidden state. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import Badge from '../components/Badge.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { replyHref, topicLabel, type InboxMessage, type InboxPage } from '../lib/contact';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let messages = $state<InboxMessage[]>([]);
  let more = $state(false);
  let olderBusy = $state(false);
  let deleting = $state<InboxMessage | null>(null);
  let deleteBusy = $state(false);
  let deleteError = $state('');

  function signedOut(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const page = await getJSON<InboxPage>('/api/admin/contact');
      messages = page.messages ?? [];
      more = page.more === true;
      loadState = 'ready';
    } catch (e) {
      if (signedOut(e)) return;
      if (e instanceof ApiError && e.status === 403) {
        loadError = e.message;
        loadState = 'forbidden';
      } else {
        loadError = e instanceof ApiError ? e.message : 'The messages could not load. Try again in a moment.';
        loadState = 'error';
      }
    }
  }

  async function older() {
    const last = messages.at(-1);
    if (!last || olderBusy) return;
    olderBusy = true;
    try {
      const page = await getJSON<InboxPage>(`/api/admin/contact?before=${encodeURIComponent(String(last.id))}`);
      messages = [...messages, ...(page.messages ?? [])];
      more = page.more === true;
    } catch (e) {
      if (signedOut(e)) return;
      showToast(e instanceof ApiError ? e.message : 'Older messages could not load. Try again in a moment.', 'error');
    } finally {
      olderBusy = false;
    }
  }

  async function confirmDelete() {
    const m = deleting;
    if (!m) return;
    deleteBusy = true;
    deleteError = '';
    try {
      await sendJSON('DELETE', `/api/admin/contact/${encodeURIComponent(String(m.id))}`);
      messages = messages.filter((x) => x.id !== m.id);
      showToast(`Deleted the message from ${m.sender.name || m.sender.email}.`);
      deleting = null;
    } catch (e) {
      if (signedOut(e)) return;
      // Inside the dialog: a toast would sit under the modal backdrop (C1-008).
      deleteError = e instanceof ApiError ? e.message : 'The message could not be deleted. Try again in a moment.';
    } finally {
      deleteBusy = false;
    }
  }

  onMount(() => {
    setTitle(routeTitle('contact-inbox'));
    void load();
  });
</script>

<PageHeader title="Messages to the organisers" lead="What people sent through Write to the organisers. Reply by email; messages are deleted 180 days after they arrive." />

{#if loadState === 'loading'}
  <PageState kind="loading" message="Loading messages" />
{:else if loadState === 'forbidden'}
  <PageState kind="forbidden" message={loadError} />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={load} />
{:else if messages.length === 0}
  <PageState kind="empty" message="No messages yet. When someone writes to the organisers, the message appears here." />
{:else}
  <ul class="panel inbox">
    {#each messages as m (m.id)}
      <li class="message">
        <div class="panel-head">
          <p class="from">
            <span class="name">{m.sender.name || m.sender.email}</span>
            <span class="muted">{m.sender.email}</span>
          </p>
          <Badge text={topicLabel(m.topic)} />
        </div>
        <p class="when muted"><MonoDigits text={formatDateTime(m.created_at)} /></p>
        <p class="text">{m.message}</p>
        <div class="action-row">
          <a class="btn btn-secondary btn-compact" href={replyHref(m)}>Reply by email</a>
          <button type="button" class="btn btn-danger-quiet btn-compact" onclick={() => (deleting = m)}>Delete</button>
        </div>
      </li>
    {/each}
  </ul>
  {#if more}
    <div class="action-row older">
      <button type="button" class="btn btn-secondary" aria-disabled={olderBusy ? 'true' : undefined} onclick={older}>Show older messages</button>
    </div>
  {/if}
{/if}

<ConfirmDialog
  open={deleting !== null}
  title="Delete this message?"
  body={deleting ? `The message from ${deleting.sender.name || deleting.sender.email} is removed for every organiser. This cannot be undone.` : ''}
  confirmLabel="Delete"
  danger
  busy={deleteBusy}
  error={deleteError}
  onconfirm={confirmDelete}
  oncancel={() => {
    deleting = null;
    deleteError = '';
  }}
/>

<style>
  /* One panel, one rule between messages: a list, not a stack of cards. */
  .inbox {
    gap: 0;
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    padding-block: 0;
    list-style: none;
  }

  .message {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-4);
  }

  .message + .message {
    border-top: var(--rule) solid var(--line);
  }

  .from {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-2);
    overflow-wrap: anywhere;
  }

  .name {
    font-weight: 600;
  }

  .when {
    font-size: var(--fs-0);
  }

  .text {
    max-width: var(--measure-prose);
    overflow-wrap: anywhere;
    white-space: pre-line;
  }

  .older {
    margin-top: var(--space-4);
  }
</style>
