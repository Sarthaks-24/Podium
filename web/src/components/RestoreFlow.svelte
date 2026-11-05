<!--
  RestoreFlow: "Restore" for an archived problem or contest. It asks first
  ("Restore Two sum?"), then POSTs `path` (the item's .../restore route) with
  no slug, which gives the original address back. When another problem or
  contest has taken that address meanwhile, the server answers 409 with a
  free one, and the flow asks for a new address with that one filled in.
  Success shows the toast "Restored <title>" and calls `ondone`.

  Props: `open`, `kind` ('problem' or 'contest'), `title` (its name), `path`
  (the restore route), `onclose` and `ondone`.
-->
<script lang="ts">
  import ConfirmDialog from './ConfirmDialog.svelte';
  import Dialog from './Dialog.svelte';
  import { ApiError, errorText, isSignedOut, sendJSON } from '../lib/api';
  import { showToast } from '../lib/toast.svelte';

  interface Props {
    open: boolean;
    kind: 'problem' | 'contest';
    title: string;
    path: string;
    onclose: () => void;
    ondone: () => void;
  }

  let { open, kind, title, path, onclose, ondone }: Props = $props();

  let step = $state<'confirm' | 'slug'>('confirm');
  let slug = $state('');
  let message = $state('');
  let fieldError = $state('');
  let error = $state('');
  let busy = $state(false);
  let slugInput: HTMLInputElement | undefined = $state();

  // Each opening starts at the question.
  $effect(() => {
    if (!open) return;
    step = 'confirm';
    slug = '';
    message = '';
    fieldError = '';
    error = '';
  });

  async function restore(chosen: string) {
    busy = true;
    error = '';
    fieldError = '';
    try {
      await sendJSON<unknown>('POST', path, { slug: chosen });
      showToast(`Restored ${title}`);
      ondone();
    } catch (e) {
      if (isSignedOut(e)) return;
      if (e instanceof ApiError && e.status === 409 && e.suggestedSlug !== '') {
        step = 'slug';
        slug = e.suggestedSlug;
        message = e.message;
      } else if (e instanceof ApiError && e.fields.slug) {
        fieldError = e.fields.slug;
        slugInput?.focus();
      } else error = errorText(e);
    } finally {
      busy = false;
    }
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (slug.trim() === '') {
      fieldError = 'Enter an address.';
      slugInput?.focus();
      return;
    }
    void restore(slug.trim());
  }

  const what = $derived(kind === 'problem' ? 'It comes back to the list of problems at its old address.' : 'It comes back to the list of contests at its old address. Students can open it again if it is published.');
</script>

<ConfirmDialog
  open={open && step === 'confirm'}
  title="Restore {title}?"
  body={what}
  confirmLabel="Restore"
  {busy}
  {error}
  onconfirm={() => restore('')}
  oncancel={onclose}
/>

<Dialog open={open && step === 'slug'} {onclose} {busy} kind="small" title="Restore {title}" closeLabel="Close" initialFocus={() => slugInput}>
  <form class="form" novalidate onsubmit={submit}>
    <p class="body">{message}</p>
    <div class="field">
      <label for="restore-slug">New address</label>
      <input
        id="restore-slug"
        bind:this={slugInput}
        class="input mono"
        type="text"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        bind:value={slug}
        aria-invalid={fieldError ? 'true' : undefined}
        aria-describedby={fieldError ? 'restore-slug-error restore-slug-help' : 'restore-slug-help'}
      />
      <p id="restore-slug-help" class="field-help">Lowercase letters, digits and single hyphens.</p>
      {#if fieldError}<p id="restore-slug-error" class="field-error">{fieldError}</p>{/if}
    </div>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Restore</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
</Dialog>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-3);
  }

  .body {
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
</style>
