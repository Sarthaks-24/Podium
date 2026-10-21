<!--
  KeyForm: the AI provider key form (split out of AiSettings in v2 P3 for
  the wizard's inline form, audit 2026-10-20 O7; contract C11): provider
  (Anthropic, OpenAI, Google Gemini, and the fake provider in testing
  mode), a model picker filled from the provider's live list with a "Type
  a model id" choice that opens a free-text field, and a password field
  with autocomplete off. With a key already held the key field may stay
  empty to change only the model. Save sends PUT /api/admin/ai/key; the
  key field is cleared whatever the answer, and a refusal is shown under
  it in the server's words, never with the key.

  Props: `status` (bindable KeyStatus; replaced by the server's answer
  after a save), `id` (a prefix for the field ids, none by default), `heading` (the form's heading; "" for none), `primary` (whether
  Save is the page's primary action), `onsessionerror` (true when it
  handled a signed-out or forbidden answer) and `onsaved`. `reset()`
  puts the form back on the provider in use, after a key was forgotten.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { aiPaths, checkKeyForm, initialForm, isListed, keyExpired, needsKey, saveKey, type KeyForm, type KeyStatus, type ModelList, type ProviderOption } from '../../lib/ai';
  import { errorText, getJSON, sendJSON } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    status: KeyStatus;
    id?: string;
    heading?: string;
    primary?: boolean;
    onsessionerror?: (e: unknown) => boolean;
    onsaved?: (status: KeyStatus) => void;
  }

  let { status = $bindable(), id = '', heading, primary = true, onsessionerror = () => false, onsaved }: Props = $props();

  /** A field id, with the prefix when the page has more than one form. */
  function fid(name: string): string {
    return id ? `${id}-${name}` : name;
  }

  /** The picker's value for "the model is typed below". */
  const TYPED = '';

  let form = $state<KeyForm>({ provider: '', model: '', key: '' });
  let typing = $state(false);
  let fields = $state<Record<string, string>>({});
  let formError = $state('');
  let saving = $state(false);
  let listing = $state(false);
  let listError = $state('');
  let keyInput: HTMLInputElement | undefined = $state();
  let modelInput: HTMLInputElement | undefined = $state();

  const now = $derived(status.server_now ?? Date.now());
  const provider = $derived<ProviderOption | undefined>(status.providers.find((p) => p.id === form.provider));
  const keyNeeded = $derived(needsKey(form.provider));
  const holdsKey = $derived(!!provider && provider.has_key && !keyExpired(provider, now));
  const models = $derived(provider?.models ?? []);
  const showTyped = $derived(keyNeeded && (typing || models.length === 0));
  const saveLabel = $derived(!keyNeeded ? 'Use fake provider' : holdsKey && form.key.trim() === '' ? 'Save model' : 'Save key');
  const title = $derived(heading ?? (holdsKey ? `Change the ${provider?.name ?? ''} key or model` : 'Add a key'));

  /** Puts the form on a provider: its saved model, typed when the list does not have it. */
  function choose(pid: string) {
    const p = status.providers.find((x) => x.id === pid);
    form = { provider: pid, model: needsKey(pid) ? (p?.model ?? '') : (p?.models[0]?.id ?? ''), key: '' };
    typing = needsKey(pid) && form.model !== '' && !isListed(p, form.model);
    fields = {};
    formError = '';
    listError = '';
    if (p && p.has_key && !keyExpired(p, now) && p.models.length === 0 && needsKey(pid)) void listModels(false);
  }

  /** Back to the provider in use, after the page changed the keys. */
  export function reset() {
    choose(form.provider || initialForm(status).provider);
  }

  onMount(() => choose(initialForm(status).provider));

  /** Fills the picker from the provider's own list; refresh asks the provider again. */
  async function listModels(refresh: boolean) {
    const pid = form.provider;
    listing = true;
    listError = '';
    try {
      const list = await getJSON<ModelList>(aiPaths.models(pid, refresh));
      status = { ...status, providers: status.providers.map((p) => (p.id === pid ? { ...p, models: list.models } : p)) };
      if (refresh) showToast('Model list updated');
    } catch (e) {
      if (!onsessionerror(e)) listError = errorText(e);
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
      document.getElementById(fid('key-form'))?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
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
      onsaved?.(result.status);
      if (form.model === '' && keyNeeded) {
        await tick();
        document.getElementById(fid('model'))?.focus();
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
</script>

<form id={fid('key-form')} class="key-form" novalidate onsubmit={save} aria-labelledby={title ? fid('form-heading') : undefined} aria-label={title ? undefined : 'AI key'}>
  {#if title}<h2 id={fid('form-heading')}>{title}</h2>{/if}

  <div class="field provider-field">
    <label for={fid('provider')}>Provider</label>
    <select
      id={fid('provider')}
      class="select"
      value={form.provider}
      onchange={(e) => choose((e.currentTarget as HTMLSelectElement).value)}
      aria-invalid={fields.provider ? 'true' : undefined}
      aria-describedby={fields.provider ? fid('provider-error') : undefined}
    >
      {#each status.providers as p (p.id)}<option value={p.id}>{p.name}{p.has_key && !keyExpired(p, now) ? ', key active' : ''}</option>{/each}
    </select>
    {#if fields.provider}<p id={fid('provider-error')} class="field-error">{fields.provider}</p>{/if}
  </div>

  {#if keyNeeded}
    {#if models.length > 0}
      <div class="field">
        <label for={fid('model')}>Model</label>
        <div class="model-row">
          <select id={fid('model')} class="select" value={showTyped ? TYPED : form.model} onchange={onPick} aria-describedby={fid('model-help')}>
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
        <p id={fid('model-help')} class="field-help">{provider?.name}'s own list, kept to models that can write a problem. A model that is not listed can still be typed.</p>
        {#if listError}<p class="field-error">{listError}</p>{/if}
      </div>
    {/if}

    {#if showTyped}
      <div class="field model-id">
        <label for={fid('model-id')}>Model id</label>
        <input
          id={fid('model-id')}
          class="input mono"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          maxlength="140"
          bind:this={modelInput}
          bind:value={form.model}
          aria-invalid={fields.model ? 'true' : undefined}
          aria-describedby={fid('model-id-help') + (fields.model ? ` ${fid('model-id-error')}` : '')}
        />
        <p id={fid('model-id-help')} class="field-help">
          {#if models.length > 0}Exactly as {provider?.name} writes it.{:else if listing}Listing {provider?.name}'s models.{:else}Optional. Leave it empty and save the key: {provider?.name}'s models are then listed here to choose from.{/if}
        </p>
        {#if models.length === 0 && listError}<p class="field-error">{listError}</p>{/if}
        {#if fields.model}<p id={fid('model-id-error')} class="field-error">{fields.model}</p>{/if}
      </div>
    {/if}

    <div class="field key-field">
      <label for={fid('api-key')}>API key</label>
      <input
        id={fid('api-key')}
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
        aria-describedby={fid('api-key-help') + (fields.key ? ` ${fid('api-key-error')}` : '')}
      />
      <p id={fid('api-key-help')} class="field-help">
        {#if holdsKey}A key for {provider?.name} is active. Leave this empty to change only the model.{:else}From your provider's console, under API keys. Saving checks it without spending tokens; the field is cleared afterwards.{/if}
      </p>
      {#if fields.key}<p id={fid('api-key-error')} class="field-error">{fields.key}</p>{/if}
    </div>
  {:else}
    <p class="note">The fake provider needs no key. It answers with canned problems, so you can try drafting a problem in testing mode without a network.</p>
  {/if}

  <div class="actions">
    <button type="submit" class="btn {primary ? 'btn-primary' : 'btn-secondary'}" disabled={saving}>
      {saving ? 'Checking key' : saveLabel}
    </button>
  </div>
  <p class="visually-hidden" role="status">{saving ? 'Checking key' : ''}</p>
  {#if formError !== ''}<p class="field-error form-error" role="alert">{formError}</p>{/if}
</form>

<style>
  h2 {
    font-size: var(--fs-sub);
  }

  .key-form {
    display: grid;
    gap: var(--space-4);
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

  @media (width < 768px) {
    .actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
