<!--
  AiSettings: the "AI assistant" screen at /admin/ai (cache/v1/m5-plan.md
  sections 1 and 7, contract C11 of the v2 plan). It says in one paragraph
  where provider keys live (the server's memory, for 2 hours, never
  stored), lists the keys this session holds (provider, "Key active until
  14:32", model, and Forget key for each; the active one is marked as used
  for new drafts), and holds the key form: provider (Anthropic, OpenAI,
  Google Gemini, and the fake provider in testing mode), a model picker
  filled from the provider's live list with a "Type a model id" choice
  that opens a free-text field, and a password field with autocomplete
  off. There is no default model: before a key is saved the list is
  unknown, so the model id is typed or left empty, and the server then
  checks the key by listing the models, which fill the picker. With a key
  already held, the key field may stay empty to change only the model.
  Save sends PUT /api/admin/ai/key; the key field is cleared whatever the
  answer, and a refusal is shown under it in the server's words, never
  with the key. Takes no props.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick } from 'svelte';
  import {
    aiPaths,
    checkKeyForm,
    heldKeys,
    initialForm,
    isListed,
    KEY_HOURS,
    keyExpired,
    keyLine,
    modelLabel,
    needsKey,
    saveKey,
    type KeyForm,
    type KeyStatus,
    type ModelList,
    type ProviderOption,
  } from '../lib/ai';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  /** The picker's value for "the model is typed below". */
  const TYPED = '';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let status = $state<KeyStatus | null>(null);

  let form = $state<KeyForm>({ provider: '', model: '', key: '' });
  let typing = $state(false);
  let fields = $state<Record<string, string>>({});
  let formError = $state('');
  let saving = $state(false);
  let forgetting = $state('');
  let listing = $state(false);
  let listError = $state('');
  let keyInput: HTMLInputElement | undefined = $state();
  let modelInput: HTMLInputElement | undefined = $state();

  const now = $derived(status?.server_now ?? Date.now());
  const provider = $derived<ProviderOption | undefined>(status?.providers.find((p) => p.id === form.provider));
  const keyNeeded = $derived(needsKey(form.provider));
  const holdsKey = $derived(!!provider && provider.has_key && !keyExpired(provider, now));
  const models = $derived(provider?.models ?? []);
  const showTyped = $derived(keyNeeded && (typing || models.length === 0));
  const held = $derived(status ? heldKeys(status) : []);
  const hasFake = $derived(status?.providers.some((p) => !needsKey(p.id)) ?? false);
  const expiredAny = $derived(status?.providers.some((p) => keyExpired(p, now)) ?? false);
  const saveLabel = $derived(!keyNeeded ? 'Use fake provider' : holdsKey && form.key.trim() === '' ? 'Save model' : 'Save key');

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

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  /** Puts the form on a provider: its saved model, typed when the list does not have it. */
  function choose(id: string) {
    const p = status?.providers.find((x) => x.id === id);
    form = { provider: id, model: needsKey(id) ? (p?.model ?? '') : (p?.models[0]?.id ?? ''), key: '' };
    typing = needsKey(id) && form.model !== '' && !isListed(p, form.model);
    fields = {};
    formError = '';
    listError = '';
    if (p && p.has_key && !keyExpired(p, now) && p.models.length === 0 && needsKey(id)) void listModels(false);
  }

  async function load() {
    loadState = 'loading';
    try {
      const s = await getJSON<KeyStatus>(aiPaths.key);
      status = s;
      const start = initialForm(s);
      loadState = 'ready';
      choose(start.provider);
    } catch (e) {
      if (handleSessionError(e)) return;
      loadError = messageOf(e);
      loadState = 'error';
    }
  }

  onMount(() => {
    setTitle(routeTitle('ai-settings'));
    const role = session.user?.role;
    if (role === 'admin' || role === 'owner') load();
    else loadState = 'forbidden';
  });

  /** Fills the picker from the provider's own list; refresh asks the provider again. */
  async function listModels(refresh: boolean) {
    const id = form.provider;
    listing = true;
    listError = '';
    try {
      const list = await getJSON<ModelList>(aiPaths.models(id, refresh));
      if (status) status = { ...status, providers: status.providers.map((p) => (p.id === id ? { ...p, models: list.models } : p)) };
      if (refresh) showToast('Model list updated');
    } catch (e) {
      if (!handleSessionError(e)) listError = messageOf(e);
    } finally {
      listing = false;
    }
  }

  function onPick(event: Event) {
    const value = (event.currentTarget as HTMLSelectElement).value;
    if (value === TYPED) {
      typing = true;
      form.model = isListed(provider, form.model) ? '' : form.model;
      void tick().then(() => modelInput?.focus());
    } else {
      typing = false;
      form.model = value;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    fields = checkKeyForm(form, holdsKey ? { has_key: true } : undefined);
    formError = '';
    if (Object.keys(fields).length > 0) {
      await tick();
      document.querySelector<HTMLElement>('.key-form [aria-invalid="true"]')?.focus();
      return;
    }
    const savingModelOnly = saveLabel === 'Save model';
    saving = true;
    const result = await saveKey((body) => sendJSON<KeyStatus>('PUT', aiPaths.key, body), form);
    saving = false;
    // The key leaves the page's state here, whatever the answer.
    form = result.form;
    if (result.ok) {
      status = result.status;
      const saved = result.status.providers.find((p) => p.id === form.provider);
      typing = needsKey(form.provider) && form.model !== '' && !isListed(saved, form.model);
      if (!keyNeeded) showToast('Fake provider on');
      else if (form.model === '') showToast('Key saved. Choose a model to start drafting.');
      else showToast(savingModelOnly ? 'Model saved' : 'Key saved');
      if (form.model === '') {
        await tick();
        document.getElementById('model')?.focus();
      }
      return;
    }
    if (result.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return;
    }
    if (result.code === 'bad_key' && keyNeeded) {
      // The sentence says whether the key or the model was refused; it sits under the key field.
      fields = { key: result.message };
      await tick();
      keyInput?.focus();
    } else {
      formError = result.message;
    }
  }

  async function forget(p: ProviderOption) {
    forgetting = p.id;
    try {
      await sendJSON<void>('DELETE', aiPaths.keyOf(p.id));
      status = await getJSON<KeyStatus>(aiPaths.key);
      showToast(`${p.name} key forgotten`);
      choose(form.provider);
      await tick();
      document.getElementById('provider')?.focus();
    } catch (e) {
      if (!handleSessionError(e)) showToast(messageOf(e), 'error');
    } finally {
      forgetting = '';
    }
  }
</script>

<div class="settings">
  <PageHeader title="AI assistant" crumbs={[{ label: 'Problem bank', href: '/admin/problems' }, { label: 'AI assistant' }]} />

  {#if loadState === 'forbidden'}
    <PageState kind="forbidden" message="Only admins can use the AI assistant." />
  {:else if loadState === 'loading'}
    <PageState kind="loading" message="Loading" />
  {:else if loadState === 'error'}
    <PageState kind="error" message={loadError} onretry={load} />
  {:else if status}
    <p class="lead">
      The assistant drafts problems from a brief or from pasted text with your own key from Anthropic, OpenAI or Google. A key stays in the
      server's memory for {KEY_HOURS} hours after you save it, tied to this sign-in, and is never stored: not in the database, not in the logs
      and not in this browser. Signing out, a server restart or Forget key ends it sooner.
    </p>

    <section class="state" aria-labelledby="state-heading">
      <h2 id="state-heading">Keys</h2>
      {#if held.length > 0}
        <ul class="keys">
          {#each held as p (p.id)}
            <li class="held">
              <div class="held-text">
                <p class="key-line tone-ok">
                  <span class="dot" aria-hidden="true"></span>{p.name}: {keyLine({ active: true, provider: p.id, active_until: p.active_until })}
                </p>
                <p class="muted">
                  {#if !needsKey(p.id)}Canned problems{:else if p.model}{modelLabel(status, p.id, p.model)}{:else}No model chosen yet{/if}{#if p.id === status.provider}. Used for new drafts.{/if}
                </p>
              </div>
              <button
                type="button"
                class="btn btn-quiet btn-compact btn-danger-quiet"
                aria-label="Forget the {p.name} key"
                disabled={forgetting !== ''}
                onclick={() => forget(p)}>Forget key</button
              >
            </li>
          {/each}
        </ul>
        {#if status.active && (status.model !== '' || !needsKey(status.provider))}
          <div class="state-actions"><a class="btn btn-primary" href="/admin/ai/new">New problem with AI</a></div>
        {/if}
      {:else if expiredAny}
        <p class="key-line">The key expired. Paste it again below to keep using the assistant.</p>
      {:else}
        <p class="key-line">No key yet. Add one below to draft problems with AI.</p>
      {/if}
    </section>

    <form class="key-form" novalidate onsubmit={save} aria-labelledby="form-heading">
      <h2 id="form-heading">{holdsKey ? `Change the ${provider?.name ?? ''} key or model` : 'Add a key'}</h2>

      <div class="field provider-field">
        <label for="provider">Provider</label>
        <select
          id="provider"
          class="select"
          value={form.provider}
          onchange={(e) => choose((e.currentTarget as HTMLSelectElement).value)}
          aria-invalid={fields.provider ? 'true' : undefined}
          aria-describedby={fields.provider ? 'provider-error' : undefined}
        >
          {#each status.providers as p (p.id)}<option value={p.id}>{p.name}{p.has_key && !keyExpired(p, now) ? ', key active' : ''}</option>{/each}
        </select>
        {#if fields.provider}<p id="provider-error" class="field-error">{fields.provider}</p>{/if}
      </div>

      {#if keyNeeded}
        {#if models.length > 0}
          <div class="field">
            <label for="model">Model</label>
            <div class="model-row">
              <select
                id="model"
                class="select"
                value={showTyped ? TYPED : form.model}
                onchange={onPick}
                aria-describedby="model-help"
              >
                {#if !showTyped && form.model === ''}<option value="" disabled>Choose a model</option>{/if}
                {#each models as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
                <option value={TYPED}>Type a model id</option>
              </select>
              {#if holdsKey}
                <button type="button" class="btn btn-quiet btn-compact" disabled={listing} onclick={() => listModels(true)}>
                  {listing ? 'Listing models' : 'Refresh the list'}
                </button>
              {/if}
            </div>
            <p id="model-help" class="field-help">{provider?.name}'s own list, kept to models that can write a problem. A model that is not listed can still be typed.</p>
            {#if listError}<p class="field-error">{listError}</p>{/if}
          </div>
        {/if}

        {#if showTyped}
          <div class="field model-id">
            <label for="model-id">Model id</label>
            <input
              id="model-id"
              class="input mono"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              maxlength="140"
              bind:this={modelInput}
              bind:value={form.model}
              aria-invalid={fields.model ? 'true' : undefined}
              aria-describedby="model-id-help{fields.model ? ' model-id-error' : ''}"
            />
            <p id="model-id-help" class="field-help">
              {#if models.length > 0}Exactly as {provider?.name} writes it.{:else if listing}Listing {provider?.name}'s models.{:else}Optional. Leave it empty and save the key: {provider?.name}'s models are then listed here to choose from.{/if}
            </p>
            {#if models.length === 0 && listError}<p class="field-error">{listError}</p>{/if}
            {#if fields.model}<p id="model-id-error" class="field-error">{fields.model}</p>{/if}
          </div>
        {/if}

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
            {#if holdsKey}A key for {provider?.name} is active. Leave this empty to change only the model.{:else}From your provider's console, under API keys. Saving checks it without spending tokens; the field is cleared afterwards.{/if}
          </p>
          {#if fields.key}<p id="api-key-error" class="field-error">{fields.key}</p>{/if}
        </div>
      {:else}
        <p class="note">The fake provider needs no key. It answers with canned problems, so you can try drafting a problem in testing mode without a network.</p>
      {/if}

      <div class="actions">
        <!-- With a key and model active, the way on to the wizard is the primary action. -->
        <button type="submit" class="btn {status.active && status.model !== '' ? 'btn-secondary' : 'btn-primary'}" disabled={saving}>
          {saving ? 'Checking key' : saveLabel}
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
    max-width: var(--measure-form);
  }

  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-prose);
  }

  h2 {
    font-size: var(--fs-sub);
  }

  .state {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .keys {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .held {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .held-text {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
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
    flex: none;
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

  .provider-field,
  .model-id,
  .key-field {
    max-width: 32rem;
  }

  .model-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .model-row .select {
    flex: 1 1 16rem;
    width: auto;
    min-width: 0;
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

  @media (width < 768px) {
    .actions .btn,
    .state-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
