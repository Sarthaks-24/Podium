<!--
  PreferencesBox: the admin's Preferences box in the Dashboard's AI prompts
  tab. A short text of style choices (at most 2,000 characters) that the
  server adds to every AI-generated problem: the wizard's prompt and the
  prompt for another AI. Shows a character counter, an example, who last
  changed it and when, "Save preferences" and "Reset to default" (which
  empties the box; the history keeps older versions). A save from a stale
  page is refused with a sentence and a Reload button. Props: `data` (the
  preferences kind from GET /api/admin/ai/prompts), `onchange` (gets the
  kind the server sent back) and `onreload` (reloads the whole tab).
-->
<script lang="ts">
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { PREFERENCES_EXAMPLE, STALE_CODE, counter, lastChanged, type PromptKindData } from '../../lib/prompts';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    data: PromptKindData;
    onchange: (kind: PromptKindData) => void;
    onreload: () => Promise<void>;
  }

  let { data, onchange, onreload }: Props = $props();

  let draft = $state('');
  let seen = $state(-1);
  let busy = $state(false);
  let error = $state('');
  let stale = $state(false);
  let confirming = $state(false);

  /** The newest row of the kind, a reset row included: what the server compares base_id with. */
  const baseId = $derived(data.history[0]?.id ?? 0);
  const changed = $derived(draft !== data.active.text);

  // A new version from the server replaces the draft; typing never does.
  $effect(() => {
    if (baseId !== seen) {
      seen = baseId;
      draft = data.active.text;
    }
  });

  async function send<T>(run: () => Promise<T>): Promise<T | undefined> {
    busy = true;
    error = '';
    stale = false;
    try {
      return await run();
    } catch (err) {
      stale = err instanceof ApiError && err.code === STALE_CODE;
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    const r = await send(() => sendJSON<{ kind: PromptKindData }>('PUT', '/api/admin/ai/prompts/preferences', { body: draft, base_id: baseId }));
    if (r) {
      onchange(r.kind);
      showToast('Saved the preferences');
    }
  }

  async function reset() {
    const r = await send(() => sendJSON<{ kind: PromptKindData }>('POST', '/api/admin/ai/prompts/preferences/reset', { base_id: baseId }));
    confirming = false;
    if (r) {
      onchange(r.kind);
      showToast('Reset the preferences to default');
    }
  }

  async function reload() {
    await onreload();
    error = '';
    stale = false;
  }
</script>

<section class="section" aria-labelledby="prefs-heading">
  <h2 id="prefs-heading">Preferences</h2>
  <p class="intro muted">Style choices for the AI. Applied to every AI-generated problem.</p>
  <form class="prefs" novalidate onsubmit={save}>
    <div class="field">
      <label for="prefs-text">Preferences</label>
      <textarea
        id="prefs-text"
        class="input"
        rows="6"
        spellcheck="true"
        bind:value={draft}
        aria-describedby="prefs-help prefs-count{error ? ' prefs-error' : ''}"
        aria-invalid={error && !stale ? 'true' : undefined}
      ></textarea>
      <p id="prefs-count" class="field-help">{counter(draft, data.limit)}</p>
      <p id="prefs-help" class="field-help">For example: {PREFERENCES_EXAMPLE}</p>
      {#if error}<p id="prefs-error" class="field-error" role="alert">{error}</p>{/if}
    </div>
    <p class="muted">{lastChanged(data.history[0])}</p>
    <div class="actions">
      <button type="submit" class="btn btn-secondary" disabled={busy || !changed || draft.trim() === ''}>Save preferences</button>
      <button type="button" class="btn btn-secondary" disabled={busy || data.active.text === ''} onclick={() => (confirming = true)}>Reset to default</button>
      {#if stale}<button type="button" class="btn btn-secondary" onclick={reload}>Reload</button>{/if}
    </div>
  </form>
</section>

<ConfirmDialog
  open={confirming}
  title="Reset the preferences to default?"
  body="The box is emptied. Older versions stay in the history of changes."
  confirmLabel="Reset to default"
  {busy}
  {error}
  onconfirm={reset}
  oncancel={() => (confirming = false)}
/>

<style>
  .prefs {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
    margin-top: var(--space-4);
    max-width: var(--measure-form);
  }

  .field {
    width: 100%;
  }

  textarea {
    width: 100%;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
