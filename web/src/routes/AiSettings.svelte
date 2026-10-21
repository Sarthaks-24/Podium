<!--
  AiSettings: the "AI assistant" screen at /admin/ai (cache/v1/m5-plan.md
  sections 1 and 7, contract C11 of the v2 plan). It says in one paragraph
  where provider keys live (the server's memory, for 2 hours, never
  stored), lists the keys this session holds (provider, "Key active until
  14:32", model, and Forget key for each; the active one is marked as used
  for new drafts), and holds the key form (components/ai/KeyForm, which
  the wizard shows inline too): provider (Anthropic, OpenAI,
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
  import KeyForm from '../components/ai/KeyForm.svelte';
  import { onMount, tick } from 'svelte';
  import { aiPaths, heldKeys, KEY_HOURS, keyExpired, keyLine, modelLabel, needsKey, type KeyStatus, type ProviderOption } from '../lib/ai';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let status = $state<KeyStatus | null>(null);
  let forgetting = $state('');
  let keyForm: ReturnType<typeof KeyForm> | undefined = $state();

  const now = $derived(status?.server_now ?? Date.now());
  const held = $derived(status ? heldKeys(status) : []);
  const hasFake = $derived(status?.providers.some((p) => !needsKey(p.id)) ?? false);
  const expiredAny = $derived(status?.providers.some((p) => keyExpired(p, now)) ?? false);

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
      loadState = 'ready';
    } catch (e) {
      if (handleSessionError(e)) return;
      loadError = errorText(e);
      loadState = 'error';
    }
  }

  onMount(() => {
    setTitle(routeTitle('ai-settings'));
    const role = session.user?.role;
    if (role === 'admin' || role === 'owner') load();
    else loadState = 'forbidden';
  });

  async function forget(p: ProviderOption) {
    forgetting = p.id;
    try {
      await sendJSON<void>('DELETE', aiPaths.keyOf(p.id));
      status = await getJSON<KeyStatus>(aiPaths.key);
      showToast(`${p.name} key forgotten`);
      keyForm?.reset();
      await tick();
      document.getElementById('provider')?.focus();
    } catch (e) {
      if (!handleSessionError(e)) showToast(errorText(e), 'error');
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
              <button type="button" class="btn btn-quiet btn-compact btn-danger-quiet" disabled={forgetting !== ''} onclick={() => forget(p)}>
                Forget key<span class="visually-hidden"> for {p.name}</span>
              </button>
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

    <div class="form-block">
      <KeyForm bind:this={keyForm} bind:status onsessionerror={handleSessionError} primary={!(status.active && status.model !== '')} />
    </div>

    {#if hasFake && needsKey(status.provider)}
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

  .form-block {
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }







  .testing {
    margin-top: var(--space-4);
    font-size: var(--fs-0);
  }

  @media (width < 768px) {
    .state-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
