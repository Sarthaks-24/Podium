<!--
  Contact: "Write to the organisers" at /contact, for anyone signed in
  (contract C6, P3 FP.2; D71 as amended). A topic, a plain-text message of
  up to 2,000 characters with a live count, and "Send message"
  (POST /api/contact; the sender is the session, so the form asks for no
  name or email). Success replaces the form with "Sent. The organisers
  reply by email, usually within two days." and offers another message.
  Errors say what to do and keep the text: field errors under the field,
  the server's rate-limit sentence, being signed out, or an unreachable
  server. Beside the form from 1280 px (page-split), below it on smaller
  screens: the organisers' email and phone for anyone who would rather
  write directly. Signed-out visitors are sent to sign in by the shell.
  Takes no props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { ApiError, sendJSON } from '../lib/api';
  import { MAX_MESSAGE, SENT, TOPICS, messageProblem, sendError, type ContactTopic } from '../lib/contact';
  import { setTitle } from '../lib/head';
  import { loadMode, runMode, telHref } from '../lib/mode.svelte';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';

  let topic = $state<ContactTopic>('contest');
  let message = $state('');
  let busy = $state(false);
  let sent = $state(false);
  let formError = $state('');
  let fieldErrors = $state<{ topic?: string; message?: string }>({});
  let messageField: HTMLTextAreaElement | undefined = $state();
  let sentLine: HTMLParagraphElement | undefined = $state();

  const length = $derived([...message.trim()].length);

  onMount(() => {
    setTitle(routeTitle('contact'));
    void loadMode();
  });

  async function send(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    formError = '';
    fieldErrors = {};
    const problem = messageProblem(message);
    if (problem !== '') {
      fieldErrors = { message: problem };
      messageField?.focus();
      return;
    }
    busy = true;
    try {
      await sendJSON('POST', '/api/contact', { topic, message: message.trim() });
      sent = true;
      message = '';
      await tick();
      sentLine?.focus();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
      }
      const err = sendError(e);
      if (err.field) {
        fieldErrors = { [err.field]: err.text };
        if (err.field === 'message') messageField?.focus();
      } else {
        formError = err.text;
      }
    } finally {
      busy = false;
    }
  }

  async function another() {
    sent = false;
    await tick();
    messageField?.focus();
  }
</script>

<PageHeader title="Write to the organisers" lead="Questions about a contest, your account or your data. The organisers read every message." />

<div class="page-split contact">
  <div class="page-split-main">
    {#if sent}
      <div class="panel" role="status">
        <p class="sent" tabindex="-1" bind:this={sentLine}>{SENT}</p>
        <div class="action-row">
          <button type="button" class="btn btn-secondary" onclick={another}>Write another message</button>
        </div>
      </div>
    {:else}
      <form class="panel form" onsubmit={send} novalidate>
        <div class="field">
          <label for="contact-topic">About</label>
          <select
            id="contact-topic"
            class="select topic"
            bind:value={topic}
            aria-invalid={fieldErrors.topic ? 'true' : undefined}
            aria-describedby={fieldErrors.topic ? 'contact-topic-error' : undefined}
          >
            {#each TOPICS as t (t.id)}<option value={t.id}>{t.label}</option>{/each}
          </select>
          {#if fieldErrors.topic}<p class="field-error" id="contact-topic-error">{fieldErrors.topic}</p>{/if}
        </div>
        <div class="field">
          <label for="contact-message">Message</label>
          <textarea
            id="contact-message"
            class="input"
            rows="8"
            bind:value={message}
            bind:this={messageField}
            aria-invalid={fieldErrors.message ? 'true' : undefined}
            aria-describedby="contact-count{fieldErrors.message ? ' contact-message-error' : ''}"
          ></textarea>
          <p class="field-help" id="contact-count" class:over={length > MAX_MESSAGE}>
            <span class="mono">{length}</span> of <span class="mono">{MAX_MESSAGE}</span> characters. Your name and email go with it from your account.
          </p>
          {#if fieldErrors.message}<p class="field-error" id="contact-message-error">{fieldErrors.message}</p>{/if}
        </div>
        {#if formError}<p class="banner banner-error" role="alert">{formError}</p>{/if}
        <div class="action-row">
          <button type="submit" class="btn btn-primary" aria-disabled={busy ? 'true' : undefined}>{busy ? 'Sending' : 'Send message'}</button>
        </div>
      </form>
    {/if}
  </div>

  {#if runMode.contactEmail !== ''}
    <aside class="page-split-aside" aria-label="Other ways to reach the organisers">
      <div class="panel">
        <h2>Rather write directly?</h2>
        <p>Email <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a>{#if runMode.organisation.phone !== ''}, or call
            <a href={telHref(runMode.organisation.phone)}>{runMode.organisation.phone}</a>{/if}.</p>
        {#if runMode.organisation.privacy_contact !== ''}
          <p class="muted">For a copy or removal of your data you can also write to
            <a href="mailto:{runMode.organisation.privacy_contact}">{runMode.organisation.privacy_contact}</a>.</p>
        {/if}
      </div>
    </aside>
  {/if}
</div>

<style>
  .contact {
    margin-top: var(--space-5);
  }

  .form {
    gap: var(--space-4);
  }

  .topic {
    max-width: 22rem;
  }

  .over {
    color: var(--bad);
  }

  .sent {
    font-size: var(--fs-2);
  }

  .sent:focus {
    outline: none;
    box-shadow: none;
  }

  .panel p {
    overflow-wrap: anywhere;
  }
</style>
