<!--
  AiSettings: the "AI assistant" screen at /admin/ai (cache/v1/m5-plan.md
  sections 1 and 7). It says in one paragraph where the provider key lives
  (the server's memory, for 2 hours, never stored), shows the key's state
  ("Key active until 14:32" with the provider and model, and Forget key),
  and holds the key form: provider, model (Claude Opus 5.5 by default) and
  a password field with autocomplete off. Save key sends the form with
  PUT /api/admin/ai/key, which checks the key with the provider; the field
  is cleared whatever the answer, and a refusal is shown under it in the
  server's words, never with the key. In testing mode the fake provider
  needs no key, so the field is hidden for it. Takes no props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    aiPaths,
    checkKeyForm,
    defaultModel,
    initialForm,
    KEY_HOURS,
    keyLine,
    modelLabel,
    needsKey,
    providerLabel,
    saveKey,
    type KeyForm,
    type KeyStatus,
  } from '../lib/ai';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let status = $state<KeyStatus | null>(null);

  let form = $state<KeyForm>({ provider: '', model: '', key: '' });
  let fields = $state<Record<string, string>>({});
  let formError = $state('');
  let saving = $state(false);
  let forgetting = $state(false);
  let keyInput: HTMLInputElement | undefined = $state();

  const provider = $derived(status?.providers.find((p) => p.id === form.provider));
  const keyNeeded = $derived(needsKey(form.provider));
  const hasFake = $derived(status?.providers.some((p) => !needsKey(p.id)) ?? false);
  // Judged by the server's clock when it sent one, so a wrong local clock cannot hide a live key.
  const expired = $derived(
    status?.active === true && status.active_until !== null && status.active_until < (status.server_now ?? Date.now()),
  );

  /** Handles errors every request can hit; returns true when it did. */
  function handleSessionError(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const s = await getJSON<KeyStatus>(aiPaths.key);
      status = s;
      form = initialForm(s);
      loadState = 'ready';
    } catch (e) {
      if (handleSessionError(e)) return;
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      loadState = 'error';
    }
  }

  onMount(() => {
    setTitle('AI assistant');
    const role = session.user?.role;
    if (role === 'admin' || role === 'owner') load();
    else loadState = 'forbidden';
  });

  function onProviderChange() {
    form.model = defaultModel(provider);
    if (!keyNeeded) form.key = '';
    fields = {};
    formError = '';
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    fields = checkKeyForm(form);
    formError = '';
    if (Object.keys(fields).length > 0) {
      await tick();
      document.querySelector<HTMLElement>('.key-form [aria-invalid="true"]')?.focus();
      return;
    }
    saving = true;
    const result = await saveKey((body) => sendJSON<KeyStatus>('PUT', aiPaths.key, body), form);
    saving = false;
    // The key leaves the page's state here, whatever the answer.
    form = result.form;
    if (result.ok) {
      status = result.status;
      showToast(keyNeeded ? 'Key saved' : 'Fake provider on');
      return;
    }
    if (result.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return;
    }
    if (result.code === 'bad_key' && keyNeeded) {
      fields = { key: result.message };
      await tick();
      keyInput?.focus();
    } else {
      formError = result.message;
    }
  }

  async function forget() {
    forgetting = true;
    try {
      await sendJSON<void>('DELETE', aiPaths.key);
      if (status) status = { ...status, active: false, active_until: null };
      showToast('Key forgotten');
      await tick();
      document.getElementById('provider')?.focus();
    } catch (e) {
      if (!handleSessionError(e)) showToast(e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.', 'error');
    } finally {
      forgetting = false;
    }
  }
</script>

<div class="settings">
  <a class="back" href="/admin/problems">Problem bank</a>
  <h1 tabindex="-1">AI assistant</h1>

  {#if loadState === 'forbidden'}
    <p class="lead">Only admins can use the AI assistant. Ask an owner if you need access.</p>
  {:else if loadState === 'loading'}
    <p class="lead muted" role="status">Loading</p>
  {:else if loadState === 'error'}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
  {:else if status}
    <p class="lead">
      The assistant drafts problems from a brief or from pasted text with your own key. The key stays in the server's memory for {KEY_HOURS} hours
      after you save it, tied to this sign-in, and is never stored: not in the database, not in the logs and not in this browser. Signing out,
      a server restart or Forget key ends it sooner.
    </p>

    <section class="state" aria-labelledby="state-heading">
      <h2 id="state-heading">Key</h2>
      {#if status.active && !expired}
        <p class="key-line tone-ok"><span class="dot" aria-hidden="true"></span>{keyLine(status)}</p>
        <p class="muted">{providerLabel(status, status.provider)}, {modelLabel(status, status.provider, status.model)}</p>
        <div class="state-actions">
          <a class="btn btn-primary" href="/admin/ai/new">New problem with AI</a>
          <button type="button" class="btn btn-quiet btn-danger-quiet" disabled={forgetting} onclick={forget}>Forget key</button>
        </div>
      {:else if expired}
        <p class="key-line">The key expired. Paste it again below to keep using the assistant.</p>
      {:else}
        <p class="key-line">No key yet. Add one below to draft problems with AI.</p>
      {/if}
    </section>

    <form class="key-form" novalidate onsubmit={save} aria-labelledby="form-heading">
      <h2 id="form-heading">{status.active && !expired ? 'Replace the key' : 'Add a key'}</h2>

      <div class="row">
        <div class="field">
          <label for="provider">Provider</label>
          <select
            id="provider"
            class="select"
            bind:value={form.provider}
            onchange={onProviderChange}
            aria-invalid={fields.provider ? 'true' : undefined}
            aria-describedby={fields.provider ? 'provider-error' : undefined}
          >
            {#each status.providers as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
          </select>
          {#if fields.provider}<p id="provider-error" class="field-error">{fields.provider}</p>{/if}
        </div>
        <div class="field">
          <label for="model">Model</label>
          <select
            id="model"
            class="select"
            bind:value={form.model}
            aria-invalid={fields.model ? 'true' : undefined}
            aria-describedby={fields.model ? 'model-error' : undefined}
          >
            {#each provider?.models ?? [] as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
          </select>
          {#if fields.model}<p id="model-error" class="field-error">{fields.model}</p>{/if}
        </div>
      </div>

      {#if keyNeeded}
        <div class="field key-field">
          <label for="api-key">API key</label>
          <input
            id="api-key"
            class="input mono"
            type="password"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            data-1p-ignore
            data-lpignore="true"
            bind:this={keyInput}
            bind:value={form.key}
            aria-invalid={fields.key ? 'true' : undefined}
            aria-describedby="api-key-help{fields.key ? ' api-key-error' : ''}"
          />
          <p id="api-key-help" class="field-help">
            From your provider's console, under API keys. Saving checks it with one small request; the field is cleared afterwards.
          </p>
          {#if fields.key}<p id="api-key-error" class="field-error">{fields.key}</p>{/if}
        </div>
      {:else}
        <p class="note">The fake provider needs no key. It answers with canned problems, so you can try drafting a problem in testing mode without a network.</p>
      {/if}

      <div class="actions">
        <!-- With a key active, the way on to the wizard is the primary action. -->
        <button type="submit" class="btn {status.active && !expired ? 'btn-secondary' : 'btn-primary'}" disabled={saving}>
          {saving ? 'Checking key' : keyNeeded ? 'Save key' : 'Use fake provider'}
        </button>
      </div>
      <p class="visually-hidden" role="status">{saving ? 'Checking key' : ''}</p>
      {#if formError !== ''}<p class="field-error form-error" role="alert">{formError}</p>{/if}
    </form>

    {#if hasFake && keyNeeded}
      <p class="testing muted">In testing mode, choose the fake provider to try drafting without a key.</p>
    {/if}
  {/if}
</div>

<style>
  .settings {
    max-width: 48rem;
  }

  .back {
    display: inline-block;
    margin-bottom: var(--space-2);
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 72ch;
  }

  .retry {
    margin-top: var(--space-4);
  }

  h2 {
    font-size: var(--fs-2);
  }

  .state {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .key-line {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-weight: 500;
  }

  .tone-ok {
    color: var(--ok);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: currentColor;
  }

  .state-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }

  .key-form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .row > .field {
    flex: 1 1 14rem;
    min-width: 0;
  }

  .key-field {
    max-width: 32rem;
  }

  .note {
    max-width: 60ch;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .form-error {
    margin-top: calc(-1 * var(--space-2));
  }

  .testing {
    margin-top: var(--space-4);
    font-size: var(--fs-0);
  }

  @media (max-width: 767px) {
    .actions .btn,
    .state-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
